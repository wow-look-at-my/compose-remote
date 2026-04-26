package runner

import (
	"context"
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
	// PullInterval, if > 0, runs `docker compose pull` (all services)
	// on its own ticker. Zero (the default) disables periodic pulls;
	// the only pulls then are on YAML image-string changes via
	// reconcile.PullSet. Image-SHA drift detection in Diff still works
	// regardless -- it just relies on whatever's in the local cache.
	PullInterval time.Duration
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
	return runLoop(ctx, cfg, compose.New(cfg.State.ComposeFile(), cfg.Project))
}

// RunOnce performs a single reconcile pass and returns. Used by the
// `apply` subcommand and by `run --once`.
func RunOnce(ctx context.Context, cfg Config) error {
	if err := compose.EnsureAvailable(ctx); err != nil {
		return fmt.Errorf("docker compose unavailable: %w", err)
	}
	return Tick(ctx, cfg, compose.New(cfg.State.ComposeFile(), cfg.Project))
}

// runLoop is the actual loop body. Split out so tests can drive it with
// a fake Composer and a pre-cancelled or short-lived ctx.
//
// Reconcile and pull tickers are merged into a single select so that all
// docker compose operations run sequentially. Two goroutines hitting
// `docker compose` for the same project at the same time (e.g. a
// background `pull` overlapping with a foreground `up`) is racy and
// causes intermittent failures, so we serialise here.
func runLoop(ctx context.Context, cfg Config, client reconcile.Composer) error {
	log.Info("started",
		log.KV{K: "project", V: cfg.Project},
		log.KV{K: "source", V: cfg.Source.Name()},
		log.KV{K: "interval", V: cfg.Interval.String()},
		log.KV{K: "pull_interval", V: cfg.PullInterval.String()},
		log.KV{K: "state_dir", V: cfg.State.Path()},
	)

	if err := Tick(ctx, cfg, client); err != nil {
		log.Warn("first tick failed", log.KV{K: "err", V: err.Error()})
	}

	t := time.NewTicker(cfg.Interval)
	defer t.Stop()

	// pullTickC is nil when --pull-interval is unset. A nil channel in a
	// select is never selected, so the pull case effectively turns off.
	var pullTickC <-chan time.Time
	if cfg.PullInterval > 0 {
		pt := time.NewTicker(cfg.PullInterval)
		defer pt.Stop()
		pullTickC = pt.C
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown")
			return nil
		case <-t.C:
			if err := Tick(ctx, cfg, client); err != nil {
				log.Warn("tick failed", log.KV{K: "err", V: err.Error()})
			}
		case <-pullTickC:
			periodicPull(ctx, client)
		}
	}
}

// periodicPull runs `docker compose pull` for the whole project. Called
// from runLoop's select on the --pull-interval ticker, so it cannot
// overlap with a Tick on the same project. The pull does NOT trigger an
// immediate reconcile -- the next regular Tick (within cfg.Interval)
// will spot image-SHA drift via Diff and recreate any stale containers
// via the existing apply path.
//
// Routing image-pull through compose (rather than `docker pull` +
// `docker run` like watchtower does) is what keeps configs:, secrets:,
// volumes:, and networks: re-applied correctly on the new container.
func periodicPull(ctx context.Context, client reconcile.Composer) {
	if err := client.Pull(ctx); err != nil {
		log.Warn("periodic pull failed", log.KV{K: "err", V: err.Error()})
		return
	}
	log.Info("periodic pull complete")
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

	// Look up the local cache's SHA for each desired image, so Diff can
	// spot SHA drift even when the YAML image string is unchanged. We
	// dedupe by image string -- many services can share a tag and there's
	// no reason to inspect twice.
	localImageIDs := map[string]string{}
	for _, svc := range parsed.Services() {
		if svc.Image == "" {
			continue
		}
		if _, seen := localImageIDs[svc.Image]; seen {
			continue
		}
		id, ierr := client.ImageID(ctx, svc.Image)
		if ierr != nil {
			log.Warn("image inspect failed",
				log.KV{K: "image", V: svc.Image},
				log.KV{K: "err", V: ierr.Error()})
			continue
		}
		localImageIDs[svc.Image] = id
	}

	// Diff.
	items := reconcile.Diff(parsed.Services(), actual, localImageIDs)
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

