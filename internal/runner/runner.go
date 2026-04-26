package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wow-look-at-my/compose-remote/internal/compose"
	"github.com/wow-look-at-my/compose-remote/internal/log"
	"github.com/wow-look-at-my/compose-remote/internal/reconcile"
	"github.com/wow-look-at-my/compose-remote/internal/source"
	"github.com/wow-look-at-my/compose-remote/internal/state"
)

// Config holds the inputs for one reconcile loop.
type Config struct {
	Source   source.Source
	State    *state.Dir
	Project  string
	Interval time.Duration
}

// Run starts the reconcile loop. It returns when ctx is cancelled or an
// unrecoverable error occurs (e.g. docker is unavailable).
//
// Transient errors (source fetch failure, compose command failure) are
// logged and the loop keeps ticking. The function only returns an error
// for startup-time failures.
func Run(ctx context.Context, cfg Config) error {
	if err := compose.EnsureAvailable(ctx); err != nil {
		return fmt.Errorf("docker compose unavailable: %w", err)
	}
	client := compose.New(cfg.State.ComposeFile(), cfg.Project)
	log.Info("started",
		log.KV{K: "project", V: cfg.Project},
		log.KV{K: "source", V: cfg.Source.Name()},
		log.KV{K: "interval", V: cfg.Interval.String()},
		log.KV{K: "state_dir", V: cfg.State.Path()},
	)

	if err := Tick(ctx, cfg, client); err != nil {
		log.Warn("first tick failed", log.KV{K: "err", V: err.Error()})
	}

	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown")
			return nil
		case <-t.C:
			if err := Tick(ctx, cfg, client); err != nil {
				log.Warn("tick failed", log.KV{K: "err", V: err.Error()})
			}
		}
	}
}

// RunOnce performs a single reconcile pass and returns. Used by the
// `apply` subcommand and by `run --once`.
func RunOnce(ctx context.Context, cfg Config) error {
	if err := compose.EnsureAvailable(ctx); err != nil {
		return fmt.Errorf("docker compose unavailable: %w", err)
	}
	client := compose.New(cfg.State.ComposeFile(), cfg.Project)
	return Tick(ctx, cfg, client)
}

// Tick performs one reconcile cycle: fetch -> parse -> diff -> apply.
// Exposed so tests can drive a single tick with a fake Composer.
func Tick(ctx context.Context, cfg Config, client reconcile.Composer) error {
	// Fetch.
	res, err := cfg.Source.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", cfg.Source.Name(), err)
	}
	content := res.Content
	if res.NotModified {
		// Re-use last-written compose file. This still proceeds to diff:
		// the actual host state may have drifted even if the source
		// hasn't changed.
		c, rerr := cfg.State.ReadCompose()
		if rerr != nil {
			return fmt.Errorf("source 304 but no cached compose file: %w", rerr)
		}
		content = c
	}

	// Parse + inject hash labels.
	parsed, err := compose.Parse(content, cfg.State.Path())
	if err != nil {
		return fmt.Errorf("parse compose: %w", err)
	}
	rendered, err := parsed.Marshal()
	if err != nil {
		return fmt.Errorf("render compose: %w", err)
	}
	changed, err := cfg.State.WriteCompose(rendered)
	if err != nil {
		return fmt.Errorf("write compose: %w", err)
	}
	if changed {
		log.Info("compose file updated",
			log.KV{K: "rev", V: res.Rev},
			log.KV{K: "services", V: len(parsed.Services())},
		)
	}

	// Inspect actual.
	actual, err := client.Ps(ctx)
	if err != nil {
		return fmt.Errorf("compose ps: %w", err)
	}

	// Diff.
	items := reconcile.Diff(parsed.Services(), actual)
	if len(items) == 0 {
		log.Info("in sync",
			log.KV{K: "services", V: len(parsed.Services())},
			log.KV{K: "rev", V: res.Rev},
		)
		return nil
	}
	for _, it := range items {
		log.Info("diff",
			log.KV{K: "service", V: it.Service},
			log.KV{K: "reason", V: string(it.Reason)},
		)
	}

	// Apply.
	if err := reconcile.Apply(ctx, client, items); err != nil {
		// Surface as a tick error but don't kill the loop.
		return fmt.Errorf("apply: %w", err)
	}
	log.Info("apply complete", log.KV{K: "diffs", V: len(items)})
	return nil
}

// ErrShutdown is returned when the context is cancelled cleanly.
var ErrShutdown = errors.New("shutdown")
