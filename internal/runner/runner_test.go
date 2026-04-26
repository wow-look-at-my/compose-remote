package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wow-look-at-my/compose-remote/internal/compose"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
	"github.com/wow-look-at-my/compose-remote/internal/source"
	"github.com/wow-look-at-my/compose-remote/internal/state"
)

// fakeSource always returns the configured content/rev/error.
type fakeSource struct {
	content		[]byte
	rev		string
	notModified	bool
	err		error
	calls		int
	mu		sync.Mutex
}

func (f *fakeSource) Name() string	{ return "fake" }
func (f *fakeSource) Fetch(_ context.Context) (source.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return source.Result{}, f.err
	}
	return source.Result{Content: f.content, Rev: f.rev, NotModified: f.notModified}, nil
}

// fakeComposer mirrors reconcile's fake but lives here to drive Tick.
type fakeComposer struct {
	psResult	[]compose.Container
	upCalled	int
	upErr		error
	pulled		[][]string
	pullErr		error
	forceCalls	[]string
	imageIDs	map[string]string
	mu		sync.Mutex
}

func (f *fakeComposer) Pull(_ context.Context, services ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]string(nil), services...)
	f.pulled = append(f.pulled, cp)
	return f.pullErr
}

func (f *fakeComposer) Up(_ context.Context) error {
	f.upCalled++
	return f.upErr
}

func (f *fakeComposer) ForceRecreate(_ context.Context, service string) error {
	f.forceCalls = append(f.forceCalls, service)
	// After force-recreate, pretend ps now reports a new container so a
	// subsequent Tick wouldn't see drift.
	for i := range f.psResult {
		if f.psResult[i].Service == service {
			f.psResult[i].ID = "new-" + service
			f.psResult[i].CreatedAt = time.Now()
		}
	}
	return nil
}

func (f *fakeComposer) Ps(_ context.Context) ([]compose.Container, error) {
	return f.psResult, nil
}

func (f *fakeComposer) ImageID(_ context.Context, image string) (string, error) {
	if f.imageIDs == nil {
		return "", nil
	}
	return f.imageIDs[image], nil
}

const oneServiceCompose = `services:
  web:
    image: nginx:1.25
`

func newDir(t *testing.T) *state.Dir {
	d, err := state.New(t.TempDir(), "test")
	require.Nil(t, err)

	return d
}

func TestTickFetchesAndApplies(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{content: []byte(oneServiceCompose), rev: "v1"}
	cmp := &fakeComposer{psResult: nil}	// missing -> apply
	cfg := Config{Source: src, State: dir, Project: "test"}
	require.NoError(t, Tick(context.Background(), cfg, cmp))

	assert.Equal(t, 1, cmp.upCalled)

	// state dir should now have a compose.yml
	_, err := dir.ReadCompose()
	assert.Nil(t, err)

}

func TestTickInSyncIsNoOp(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{content: []byte(oneServiceCompose), rev: "v1"}
	// First tick: writes compose.yml and brings up.
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	require.NoError(t, Tick(context.Background(), cfg, cmp))

	// Now read the compose.yml to find the injected hash and synthesize
	// an "actual" container that matches.
	body, err := dir.ReadCompose()
	require.Nil(t, err)

	parsed, err := compose.Parse(body, dir.Path())
	require.Nil(t, err)

	hash := parsed.Services()["web"].Hash
	cmp.psResult = []compose.Container{
		{ID: "x", Service: "web", Image: "nginx:1.25", ConfigHash: hash, State: "running"},
	}
	cmp.upCalled = 0
	require.NoError(t, Tick(context.Background(), cfg, cmp))

	assert.Equal(t, 0, cmp.upCalled)

}

func TestTickHandlesNotModified(t *testing.T) {
	dir := newDir(t)
	// Seed a previous compose file so NotModified can fall back.
	_, err := dir.WriteCompose([]byte(oneServiceCompose))
	require.Nil(t, err)

	src := &fakeSource{notModified: true, rev: "v1"}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	require.NoError(t, Tick(context.Background(), cfg, cmp))

}

func TestTickNotModifiedNoCacheError(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{notModified: true, rev: "v1"}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	err := Tick(context.Background(), cfg, cmp)
	assert.NotNil(t, err)

}

func TestTickFetchError(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{err: errors.New("network")}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	err := Tick(context.Background(), cfg, cmp)
	assert.NotNil(t, err)

}

func TestTickParseError(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{content: []byte("not yaml: : :")}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	err := Tick(context.Background(), cfg, cmp)
	assert.NotNil(t, err)
}

// TestRunBailsWhenDockerMissing covers the Run() startup-error path.
func TestRunBailsWhenDockerMissing(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	dir := newDir(t)
	src := &fakeSource{content: []byte(oneServiceCompose), rev: "v1"}
	cfg := Config{Source: src, State: dir, Project: "test", Interval: time.Millisecond}
	err := Run(context.Background(), cfg)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "docker compose unavailable")
}

// TestRunOnceBailsWhenDockerMissing covers the RunOnce() startup-error path.
func TestRunOnceBailsWhenDockerMissing(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	dir := newDir(t)
	src := &fakeSource{content: []byte(oneServiceCompose), rev: "v1"}
	cfg := Config{Source: src, State: dir, Project: "test"}
	err := RunOnce(context.Background(), cfg)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "docker compose unavailable")
}

// TestRunLoopExitsOnContextCancel exercises the loop body and verifies
// it returns cleanly when ctx is cancelled, after at least one tick.
func TestRunLoopExitsOnContextCancel(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{content: []byte(oneServiceCompose), rev: "v1"}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test", Interval: time.Hour}

	// Cancel almost immediately — the first Tick will run synchronously,
	// then the select must observe ctx.Done() and return.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runLoop(ctx, cfg, cmp)
	require.NoError(t, err)
	assert.Equal(t, 1, cmp.upCalled) // first-tick happened
}

// TestPeriodicPullCallsPullAll verifies the periodic-pull helper calls
// Composer.Pull with no service args (i.e. "pull every service").
func TestPeriodicPullCallsPullAll(t *testing.T) {
	cmp := &fakeComposer{}
	periodicPull(context.Background(), cmp)

	cmp.mu.Lock()
	defer cmp.mu.Unlock()
	require.Equal(t, 1, len(cmp.pulled))

	assert.Equal(t, 0, len(cmp.pulled[0])) // no service args

}

// TestPeriodicPullSwallowsErrors verifies a failing pull is logged
// without panicking (so the runLoop select-case can keep going).
func TestPeriodicPullSwallowsErrors(t *testing.T) {
	cmp := &fakeComposer{pullErr: errors.New("registry unreachable")}
	periodicPull(context.Background(), cmp) // should not panic

	cmp.mu.Lock()
	defer cmp.mu.Unlock()
	assert.Equal(t, 1, len(cmp.pulled))

}

// TestRunLoopFiresPeriodicPullWhenIntervalSet smokes the wiring: when
// PullInterval > 0, runLoop's select fires periodicPull on the same
// goroutine as Tick (so docker compose calls cannot overlap).
func TestRunLoopFiresPeriodicPullWhenIntervalSet(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{content: []byte(oneServiceCompose), rev: "v1"}
	cmp := &fakeComposer{}
	cfg := Config{
		Source:       src,
		State:        dir,
		Project:      "test",
		Interval:     time.Hour,
		PullInterval: time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Give the pull-tick a chance to fire at least once.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			cmp.mu.Lock()
			got := len(cmp.pulled)
			cmp.mu.Unlock()
			if got >= 1 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	err := runLoop(ctx, cfg, cmp)
	require.NoError(t, err)

	cmp.mu.Lock()
	defer cmp.mu.Unlock()
	assert.GreaterOrEqual(t, len(cmp.pulled), 1)

}

// TestRunLoopSkipsPullWhenIntervalZero verifies the pull-tick channel
// is nil when --pull-interval is unset, so runLoop never selects it.
func TestRunLoopSkipsPullWhenIntervalZero(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{content: []byte(oneServiceCompose), rev: "v1"}
	cmp := &fakeComposer{}
	cfg := Config{
		Source:   src,
		State:    dir,
		Project:  "test",
		Interval: time.Hour,
		// PullInterval intentionally zero.
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runLoop(ctx, cfg, cmp)
	require.NoError(t, err)

	cmp.mu.Lock()
	defer cmp.mu.Unlock()
	// Tick may have called Pull through reconcile.Apply for missing
	// services; what matters is that no *periodic* pull happened.
	// First-tick path with empty psResult treats the service as missing
	// and falls into Apply, which only pulls on DriftedImage. Since
	// PriorContainerID is empty for missing, no pull there either.
	assert.Equal(t, 0, len(cmp.pulled))

}

// TestRunLoopRecoversFromTickError verifies a tick error is logged but
// doesn't abort the loop.
func TestRunLoopRecoversFromTickError(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{err: errors.New("transient")}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test", Interval: time.Hour}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runLoop(ctx, cfg, cmp)
	require.NoError(t, err) // graceful shutdown, despite the tick error
}
