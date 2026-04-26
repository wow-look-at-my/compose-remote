package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/wow-look-at-my/compose-remote/internal/compose"
	"github.com/wow-look-at-my/compose-remote/internal/log"
)

// Composer is the subset of compose.Client behaviour that runner.Tick
// and Apply use. Defined here (rather than in compose) so apply and
// runner can be tested with a fake without dragging in the real
// Client. ImageID is consumed by runner.Tick before Diff (not by Apply
// itself) but lives here because Tick takes the same Composer.
type Composer interface {
	Pull(ctx context.Context, services ...string) error
	Up(ctx context.Context) error
	ForceRecreate(ctx context.Context, service string) error
	Ps(ctx context.Context) ([]compose.Container, error)
	ImageID(ctx context.Context, image string) (string, error)
}

// Apply resolves the diff set against the live host. It:
//  1. Pulls any drifted-image services (and only those).
//  2. Runs `docker compose up -d --remove-orphans --wait`.
//  3. Runs the bug-fix pass: any service that was in the diff set whose
//     container is unchanged after `up` is force-recreated with --wait.
//
// The function does NOT compute the diff itself; pass it in. Returning a
// nil error means the host is reconciled (in sync). A non-nil error means
// the apply ran but at least one operation failed; the runner logs and
// loops on the next tick.
func Apply(ctx context.Context, c Composer, items []Item) error {
	if len(items) == 0 {
		return nil
	}

	t0 := time.Now()

	// 1. Pull only image-drifted services.
	if pulls := PullSet(items); len(pulls) > 0 {
		log.Info("compose pull", log.KV{K: "services", V: joinComma(pulls)})
		if err := c.Pull(ctx, pulls...); err != nil {
			// Don't bail; an image-pull failure may be transient. We'll
			// still try the up; if the local image is sufficient the up
			// can succeed, and if not the up will fail loudly.
			log.Warn("compose pull failed", log.KV{K: "err", V: err.Error()})
		}
	}

	// 2. up --remove-orphans --wait. --wait is non-negotiable.
	upErr := c.Up(ctx)
	if upErr != nil {
		log.Warn("compose up failed (continuing to bug-fix pass)",
			log.KV{K: "err", V: upErr.Error()})
	}

	// 3. Bug-fix pass: re-inspect and force-recreate any service that
	// compose wrongly skipped.
	if err := bugFixPass(ctx, c, items, t0); err != nil {
		return err
	}
	if upErr != nil {
		return upErr
	}
	return nil
}

func bugFixPass(ctx context.Context, c Composer, items []Item, t0 time.Time) error {
	post, err := c.Ps(ctx)
	if err != nil {
		return fmt.Errorf("ps after up: %w", err)
	}
	postByService := map[string]compose.Container{}
	for _, ct := range post {
		// Pick the most recent if duplicates remain.
		if e, ok := postByService[ct.Service]; ok {
			if ct.CreatedAt.After(e.CreatedAt) {
				postByService[ct.Service] = ct
			}
			continue
		}
		postByService[ct.Service] = ct
	}

	var firstErr error
	for _, it := range items {
		got, ok := postByService[it.Service]
		recreated := ok && (it.PriorContainerID == "" || got.ID != it.PriorContainerID) && !got.CreatedAt.Before(t0)
		if recreated {
			continue
		}
		log.Warn("docker compose skipped recreation; forcing",
			log.KV{K: "service", V: it.Service},
			log.KV{K: "reason", V: string(it.Reason)},
			log.KV{K: "prior_container", V: it.PriorContainerID},
		)
		if err := c.ForceRecreate(ctx, it.Service); err != nil {
			log.Error("force-recreate failed",
				log.KV{K: "service", V: it.Service},
				log.KV{K: "err", V: err.Error()})
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
