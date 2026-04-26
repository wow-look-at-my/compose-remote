package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wow-look-at-my/compose-remote/internal/compose"
	"github.com/wow-look-at-my/compose-remote/internal/source"
	"github.com/wow-look-at-my/compose-remote/internal/state"
)

// fakeSource always returns the configured content/rev/error.
type fakeSource struct {
	content     []byte
	rev         string
	notModified bool
	err         error
	calls       int
	mu          sync.Mutex
}

func (f *fakeSource) Name() string { return "fake" }
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
	psResult   []compose.Container
	upCalled   int
	upErr      error
	pulled     [][]string
	forceCalls []string
}

func (f *fakeComposer) Pull(_ context.Context, services ...string) error {
	cp := append([]string(nil), services...)
	f.pulled = append(f.pulled, cp)
	return nil
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

const oneServiceCompose = `services:
  web:
    image: nginx:1.25
`

func newDir(t *testing.T) *state.Dir {
	d, err := state.New(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTickFetchesAndApplies(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{content: []byte(oneServiceCompose), rev: "v1"}
	cmp := &fakeComposer{psResult: nil} // missing -> apply
	cfg := Config{Source: src, State: dir, Project: "test"}
	if err := Tick(context.Background(), cfg, cmp); err != nil {
		t.Fatal(err)
	}
	if cmp.upCalled != 1 {
		t.Errorf("up calls = %d, want 1", cmp.upCalled)
	}
	// state dir should now have a compose.yml
	if _, err := dir.ReadCompose(); err != nil {
		t.Errorf("compose.yml not written: %v", err)
	}
}

func TestTickInSyncIsNoOp(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{content: []byte(oneServiceCompose), rev: "v1"}
	// First tick: writes compose.yml and brings up.
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	if err := Tick(context.Background(), cfg, cmp); err != nil {
		t.Fatal(err)
	}

	// Now read the compose.yml to find the injected hash and synthesize
	// an "actual" container that matches.
	body, err := dir.ReadCompose()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := compose.Parse(body, dir.Path())
	if err != nil {
		t.Fatal(err)
	}
	hash := parsed.Services()["web"].Hash
	cmp.psResult = []compose.Container{
		{ID: "x", Service: "web", Image: "nginx:1.25", ConfigHash: hash, State: "running"},
	}
	cmp.upCalled = 0
	if err := Tick(context.Background(), cfg, cmp); err != nil {
		t.Fatal(err)
	}
	if cmp.upCalled != 0 {
		t.Errorf("in-sync should not call up; got %d", cmp.upCalled)
	}
}

func TestTickHandlesNotModified(t *testing.T) {
	dir := newDir(t)
	// Seed a previous compose file so NotModified can fall back.
	if _, err := dir.WriteCompose([]byte(oneServiceCompose)); err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{notModified: true, rev: "v1"}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	if err := Tick(context.Background(), cfg, cmp); err != nil {
		t.Fatal(err)
	}
}

func TestTickNotModifiedNoCacheError(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{notModified: true, rev: "v1"}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	if err := Tick(context.Background(), cfg, cmp); err == nil {
		t.Error("expected error: 304 with no cached compose")
	}
}

func TestTickFetchError(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{err: errors.New("network")}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	if err := Tick(context.Background(), cfg, cmp); err == nil {
		t.Error("expected fetch error")
	}
}

func TestTickParseError(t *testing.T) {
	dir := newDir(t)
	src := &fakeSource{content: []byte("not yaml: : :")}
	cmp := &fakeComposer{}
	cfg := Config{Source: src, State: dir, Project: "test"}
	if err := Tick(context.Background(), cfg, cmp); err == nil {
		t.Error("expected parse error")
	}
}
