package reconcile

import (
	"sort"

	"github.com/wow-look-at-my/compose-remote/internal/compose"
)

// Reason classifies why a service is in the diff set.
type Reason string

const (
	// Missing: no container exists for this desired service.
	Missing Reason = "missing"
	// DriftedConfig: container exists but its config-hash label doesn't
	// match the desired hash.
	DriftedConfig Reason = "drifted-config"
	// DriftedImage: the image string in the desired file changed; we need
	// to pull before recreating.
	DriftedImage Reason = "drifted-image"
	// Unhealthy: container exists, has a healthcheck, and is unhealthy
	// or has exited unsuccessfully.
	Unhealthy Reason = "unhealthy"
)

// Item is one entry in a diff set: a desired service that is not in sync.
type Item struct {
	Service string
	Reason  Reason
	// PriorContainerID is the id of the container that exists for this
	// service before we apply (empty for Missing). The bug-fix pass uses
	// this to detect "compose said up-to-date but the container wasn't
	// recreated".
	PriorContainerID string
}

// Diff computes the set of differences between desired and actual.
//
// Orphans (actual containers whose service is not in desired) are NOT
// returned here; they are handled by `docker compose up --remove-orphans`.
func Diff(desired map[string]compose.Service, actual []compose.Container) []Item {
	byService := map[string]compose.Container{}
	for _, c := range actual {
		// If multiple containers exist for one service (scale > 1), pick
		// the most recently created one for diff purposes; an orphaned
		// duplicate gets cleaned up on up --remove-orphans.
		if existing, ok := byService[c.Service]; ok {
			if c.CreatedAt.After(existing.CreatedAt) {
				byService[c.Service] = c
			}
			continue
		}
		byService[c.Service] = c
	}

	items := make([]Item, 0)
	names := make([]string, 0, len(desired))
	for n := range desired {
		names = append(names, n)
	}
	sort.Strings(names) // determinism for logs and tests

	for _, name := range names {
		want := desired[name]
		got, ok := byService[name]
		if !ok {
			items = append(items, Item{Service: name, Reason: Missing})
			continue
		}
		// Image-string drift => pull then recreate.
		if got.Image != "" && want.Image != "" && got.Image != want.Image {
			items = append(items, Item{
				Service:          name,
				Reason:           DriftedImage,
				PriorContainerID: got.ID,
			})
			continue
		}
		if got.ConfigHash != want.Hash {
			items = append(items, Item{
				Service:          name,
				Reason:           DriftedConfig,
				PriorContainerID: got.ID,
			})
			continue
		}
		// Health check.
		if got.Health == "unhealthy" {
			items = append(items, Item{
				Service:          name,
				Reason:           Unhealthy,
				PriorContainerID: got.ID,
			})
			continue
		}
		if got.State == "exited" && got.ExitCode != 0 {
			items = append(items, Item{
				Service:          name,
				Reason:           Unhealthy,
				PriorContainerID: got.ID,
			})
			continue
		}
	}
	return items
}

// PullSet returns the unique services that need a `docker compose pull`
// because their image string changed.
func PullSet(items []Item) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, it := range items {
		if it.Reason != DriftedImage {
			continue
		}
		if _, ok := seen[it.Service]; ok {
			continue
		}
		seen[it.Service] = struct{}{}
		out = append(out, it.Service)
	}
	return out
}
