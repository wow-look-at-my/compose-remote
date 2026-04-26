package reconcile

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wow-look-at-my/compose-remote/internal/compose"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

// fakeCompose simulates the parts of docker compose Apply uses.
type fakeCompose struct {
	mu	sync.Mutex

	pullCalls		[][]string
	pullErr			error
	upCalls			int
	upErr			error
	forceCalls		[]string
	forceErrPerSvc		map[string]error
	psQueue			[][]compose.Container	// each Ps() call pops one
	defaultPsResponse	[]compose.Container

	// upBehavior decides what state the next Ps() call returns after up.
	// If set, replaces psQueue's entry.
	afterUp	[]compose.Container
}

func (f *fakeCompose) Pull(_ context.Context, services ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]string(nil), services...)
	f.pullCalls = append(f.pullCalls, cp)
	return f.pullErr
}

func (f *fakeCompose) Up(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upCalls++
	if f.afterUp != nil {
		f.psQueue = append(f.psQueue, f.afterUp)
	}
	return f.upErr
}

func (f *fakeCompose) ForceRecreate(_ context.Context, service string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forceCalls = append(f.forceCalls, service)
	if f.forceErrPerSvc != nil {
		if err, ok := f.forceErrPerSvc[service]; ok {
			return err
		}
	}
	return nil
}

func (f *fakeCompose) Ps(_ context.Context) ([]compose.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.psQueue) > 0 {
		head := f.psQueue[0]
		f.psQueue = f.psQueue[1:]
		return head, nil
	}
	return f.defaultPsResponse, nil
}

func (f *fakeCompose) ImageID(_ context.Context, _ string) (string, error) {
	// Apply doesn't need ImageID; runner.Tick calls it before Diff. The
	// fake returns empty so any test that does end up touching it
	// behaves like "image not yet pulled locally".
	return "", nil
}

func TestApplyEmptyItemsIsNoOp(t *testing.T) {
	f := &fakeCompose{}
	require.NoError(t, Apply(context.Background(), f, nil))

	assert.Equal(t, 0, f.upCalls)

	assert.Equal(t, 0, len(f.pullCalls))

}

func TestApplyHappyPath(t *testing.T) {
	// One drifted-config service that compose recreates correctly.
	f := &fakeCompose{
		afterUp: []compose.Container{
			{ID: "new", Service: "web", CreatedAt: time.Now().Add(time.Second)},
		},
	}
	items := []Item{{Service: "web", Reason: DriftedConfig, PriorContainerID: "old"}}
	require.NoError(t, Apply(context.Background(), f, items))

	assert.Equal(t, 1, f.upCalls)

	assert.Equal(t, 0, len(f.forceCalls))

	assert.Equal(t, 0, len(f.pullCalls))

}

func TestApplyImageDriftPulls(t *testing.T) {
	f := &fakeCompose{
		afterUp: []compose.Container{
			{ID: "new", Service: "web", CreatedAt: time.Now().Add(time.Second)},
		},
	}
	items := []Item{{Service: "web", Reason: DriftedImage, PriorContainerID: "old"}}
	require.NoError(t, Apply(context.Background(), f, items))

	assert.False(t, len(f.pullCalls) != 1 || len(f.pullCalls[0]) != 1 || f.pullCalls[0][0] != "web")

}

func TestApplyBugFixForceRecreates(t *testing.T) {
	// Compose returns the SAME container ID after up: this is the bug.
	// The bug-fix pass must force-recreate.
	f := &fakeCompose{
		afterUp: []compose.Container{
			{ID: "old", Service: "web", CreatedAt: time.Now().Add(-time.Hour)},
		},
	}
	items := []Item{{Service: "web", Reason: DriftedConfig, PriorContainerID: "old"}}
	require.NoError(t, Apply(context.Background(), f, items))

	assert.False(t, len(f.forceCalls) != 1 || f.forceCalls[0] != "web")

}

func TestApplyBugFixHandlesMissingService(t *testing.T) {
	// Compose's up didn't bring up `web` at all (not in ps result).
	// We should force-recreate it.
	f := &fakeCompose{afterUp: nil}	// no containers after up
	items := []Item{{Service: "web", Reason: Missing}}
	_ = Apply(context.Background(), f, items)
	assert.False(t, len(f.forceCalls) != 1 || f.forceCalls[0] != "web")

}

func TestApplyUpFailureStillRunsBugFix(t *testing.T) {
	f := &fakeCompose{
		upErr:		errors.New("boom"),
		afterUp:	[]compose.Container{{ID: "old", Service: "web", CreatedAt: time.Now().Add(-time.Hour)}},
	}
	items := []Item{{Service: "web", Reason: DriftedConfig, PriorContainerID: "old"}}
	err := Apply(context.Background(), f, items)
	assert.NotNil(t, err)

	assert.Equal(t, 1, len(f.forceCalls))

}

func TestApplyForceRecreateError(t *testing.T) {
	f := &fakeCompose{
		afterUp:	[]compose.Container{{ID: "old", Service: "web", CreatedAt: time.Now().Add(-time.Hour)}},
		forceErrPerSvc:	map[string]error{"web": errors.New("nope")},
	}
	items := []Item{{Service: "web", Reason: DriftedConfig, PriorContainerID: "old"}}
	err := Apply(context.Background(), f, items)
	assert.NotNil(t, err)

}

func TestApplyPullErrorIsTolerated(t *testing.T) {
	// A pull failure must not stop us from running up + bug-fix.
	f := &fakeCompose{
		pullErr:	errors.New("registry down"),
		afterUp:	[]compose.Container{{ID: "new", Service: "web", CreatedAt: time.Now().Add(time.Second)}},
	}
	items := []Item{{Service: "web", Reason: DriftedImage, PriorContainerID: "old"}}
	require.NoError(t, Apply(context.Background(), f, items))

	assert.Equal(t, 1, f.upCalls)

}

func TestJoinComma(t *testing.T) {
	cases := []struct {
		in	[]string
		want	string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b", "c"}, "a,b,c"},
	}
	for _, c := range cases {
		got := joinComma(c.in)
		assert.Equal(t, c.want, got)

	}
}
