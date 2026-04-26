package reconcile

import (
	"testing"
	"time"

	"github.com/wow-look-at-my/compose-remote/internal/compose"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func mkDesired() map[string]compose.Service {
	return map[string]compose.Service{
		"web":		{Name: "web", Hash: "h1", Image: "nginx:1.25"},
		"cache":	{Name: "cache", Hash: "h2", Image: "redis:7"},
	}
}

func TestDiffMissing(t *testing.T) {
	got := Diff(mkDesired(), nil)
	require.Equal(t, 2, len(got))

	for _, it := range got {
		assert.Equal(t, Missing, it.Reason)

	}
}

func TestDiffInSync(t *testing.T) {
	actual := []compose.Container{
		{ID: "1", Service: "web", Image: "nginx:1.25", ConfigHash: "h1", State: "running"},
		{ID: "2", Service: "cache", Image: "redis:7", ConfigHash: "h2", State: "running"},
	}
	got := Diff(mkDesired(), actual)
	assert.Equal(t, 0, len(got))

}

func TestDiffDriftedConfig(t *testing.T) {
	actual := []compose.Container{
		{ID: "1", Service: "web", Image: "nginx:1.25", ConfigHash: "stale", State: "running"},
		{ID: "2", Service: "cache", Image: "redis:7", ConfigHash: "h2", State: "running"},
	}
	got := Diff(mkDesired(), actual)
	assert.False(t, len(got) != 1 || got[0].Service != "web" || got[0].Reason != DriftedConfig)

	assert.Equal(t, "1", got[0].PriorContainerID)

}

func TestDiffDriftedImage(t *testing.T) {
	actual := []compose.Container{
		{ID: "1", Service: "web", Image: "nginx:1.24", ConfigHash: "h1", State: "running"},
		{ID: "2", Service: "cache", Image: "redis:7", ConfigHash: "h2", State: "running"},
	}
	got := Diff(mkDesired(), actual)
	assert.False(t, len(got) != 1 || got[0].Reason != DriftedImage)

}

func TestDiffUnhealthy(t *testing.T) {
	actual := []compose.Container{
		{ID: "1", Service: "web", Image: "nginx:1.25", ConfigHash: "h1", State: "running", Health: "unhealthy"},
		{ID: "2", Service: "cache", Image: "redis:7", ConfigHash: "h2", State: "exited", ExitCode: 1},
	}
	got := Diff(mkDesired(), actual)
	require.Equal(t, 2, len(got))

	for _, it := range got {
		assert.Equal(t, Unhealthy, it.Reason)

	}
}

func TestDiffPicksMostRecentDuplicate(t *testing.T) {
	now := time.Now()
	actual := []compose.Container{
		{ID: "old", Service: "web", Image: "nginx:1.25", ConfigHash: "stale", CreatedAt: now.Add(-time.Hour)},
		{ID: "new", Service: "web", Image: "nginx:1.25", ConfigHash: "h1", CreatedAt: now},
	}
	desired := map[string]compose.Service{
		"web": {Name: "web", Hash: "h1", Image: "nginx:1.25"},
	}
	got := Diff(desired, actual)
	assert.Equal(t, 0, len(got))

}

func TestDiffDeterministicOrder(t *testing.T) {
	// Two missing services: order must be alphabetical.
	desired := map[string]compose.Service{
		"zeta":		{Name: "zeta"},
		"alpha":	{Name: "alpha"},
	}
	got := Diff(desired, nil)
	assert.False(t, got[0].Service != "alpha" || got[1].Service != "zeta")

}

func TestPullSet(t *testing.T) {
	items := []Item{
		{Service: "a", Reason: DriftedImage},
		{Service: "b", Reason: DriftedConfig},
		{Service: "a", Reason: DriftedImage},	// dup
		{Service: "c", Reason: Missing},
	}
	got := PullSet(items)
	assert.False(t, len(got) != 1 || got[0] != "a")

}
