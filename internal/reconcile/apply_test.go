package reconcile

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wow-look-at-my/compose-remote/internal/compose"
)

// fakeCompose simulates the parts of docker compose Apply uses.
type fakeCompose struct {
	mu sync.Mutex

	pullCalls         [][]string
	pullErr           error
	upCalls           int
	upErr             error
	forceCalls        []string
	forceErrPerSvc    map[string]error
	psQueue           [][]compose.Container // each Ps() call pops one
	defaultPsResponse []compose.Container

	// upBehavior decides what state the next Ps() call returns after up.
	// If set, replaces psQueue's entry.
	afterUp []compose.Container
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

func TestApplyEmptyItemsIsNoOp(t *testing.T) {
	f := &fakeCompose{}
	if err := Apply(context.Background(), f, nil); err != nil {
		t.Fatal(err)
	}
	if f.upCalls != 0 {
		t.Errorf("up was called: %d", f.upCalls)
	}
	if len(f.pullCalls) != 0 {
		t.Errorf("pull was called: %v", f.pullCalls)
	}
}

func TestApplyHappyPath(t *testing.T) {
	// One drifted-config service that compose recreates correctly.
	f := &fakeCompose{
		afterUp: []compose.Container{
			{ID: "new", Service: "web", CreatedAt: time.Now().Add(time.Second)},
		},
	}
	items := []Item{{Service: "web", Reason: DriftedConfig, PriorContainerID: "old"}}
	if err := Apply(context.Background(), f, items); err != nil {
		t.Fatal(err)
	}
	if f.upCalls != 1 {
		t.Errorf("upCalls = %d", f.upCalls)
	}
	if len(f.forceCalls) != 0 {
		t.Errorf("force-recreate should not be needed: %v", f.forceCalls)
	}
	if len(f.pullCalls) != 0 {
		t.Errorf("pull should not be called for config drift: %v", f.pullCalls)
	}
}

func TestApplyImageDriftPulls(t *testing.T) {
	f := &fakeCompose{
		afterUp: []compose.Container{
			{ID: "new", Service: "web", CreatedAt: time.Now().Add(time.Second)},
		},
	}
	items := []Item{{Service: "web", Reason: DriftedImage, PriorContainerID: "old"}}
	if err := Apply(context.Background(), f, items); err != nil {
		t.Fatal(err)
	}
	if len(f.pullCalls) != 1 || len(f.pullCalls[0]) != 1 || f.pullCalls[0][0] != "web" {
		t.Errorf("pullCalls = %v", f.pullCalls)
	}
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
	if err := Apply(context.Background(), f, items); err != nil {
		t.Fatal(err)
	}
	if len(f.forceCalls) != 1 || f.forceCalls[0] != "web" {
		t.Errorf("expected force-recreate on web, got %v", f.forceCalls)
	}
}

func TestApplyBugFixHandlesMissingService(t *testing.T) {
	// Compose's up didn't bring up `web` at all (not in ps result).
	// We should force-recreate it.
	f := &fakeCompose{afterUp: nil} // no containers after up
	items := []Item{{Service: "web", Reason: Missing}}
	_ = Apply(context.Background(), f, items)
	if len(f.forceCalls) != 1 || f.forceCalls[0] != "web" {
		t.Errorf("expected force-recreate, got %v", f.forceCalls)
	}
}

func TestApplyUpFailureStillRunsBugFix(t *testing.T) {
	f := &fakeCompose{
		upErr:   errors.New("boom"),
		afterUp: []compose.Container{{ID: "old", Service: "web", CreatedAt: time.Now().Add(-time.Hour)}},
	}
	items := []Item{{Service: "web", Reason: DriftedConfig, PriorContainerID: "old"}}
	err := Apply(context.Background(), f, items)
	if err == nil {
		t.Error("expected error from up to surface")
	}
	if len(f.forceCalls) != 1 {
		t.Errorf("bug-fix pass should still run despite up error; got %v", f.forceCalls)
	}
}

func TestApplyForceRecreateError(t *testing.T) {
	f := &fakeCompose{
		afterUp:        []compose.Container{{ID: "old", Service: "web", CreatedAt: time.Now().Add(-time.Hour)}},
		forceErrPerSvc: map[string]error{"web": errors.New("nope")},
	}
	items := []Item{{Service: "web", Reason: DriftedConfig, PriorContainerID: "old"}}
	err := Apply(context.Background(), f, items)
	if err == nil {
		t.Error("expected error to surface")
	}
}

func TestApplyPullErrorIsTolerated(t *testing.T) {
	// A pull failure must not stop us from running up + bug-fix.
	f := &fakeCompose{
		pullErr: errors.New("registry down"),
		afterUp: []compose.Container{{ID: "new", Service: "web", CreatedAt: time.Now().Add(time.Second)}},
	}
	items := []Item{{Service: "web", Reason: DriftedImage, PriorContainerID: "old"}}
	if err := Apply(context.Background(), f, items); err != nil {
		t.Fatal(err)
	}
	if f.upCalls != 1 {
		t.Errorf("up should still run after pull fail")
	}
}

func TestJoinComma(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b", "c"}, "a,b,c"},
	}
	for _, c := range cases {
		got := joinComma(c.in)
		if got != c.want {
			t.Errorf("joinComma(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
