package compose

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	composeOut string
	composeErr error
	composeLog [][]string

	inspectOut map[string]string
	inspectErr error
	inspectLog []string

	versionErr error
}

func (f *fakeRunner) composeArgs(_ context.Context, file, project string, args ...string) (string, error) {
	full := append([]string{file, project}, args...)
	f.composeLog = append(f.composeLog, full)
	if f.composeErr != nil {
		return "", f.composeErr
	}
	return f.composeOut, nil
}

func (f *fakeRunner) inspect(_ context.Context, id string) (string, error) {
	f.inspectLog = append(f.inspectLog, id)
	if f.inspectErr != nil {
		return "", f.inspectErr
	}
	if v, ok := f.inspectOut[id]; ok {
		return v, nil
	}
	return "[]", nil
}

func (f *fakeRunner) version(_ context.Context) (string, error) {
	if f.versionErr != nil {
		return "", f.versionErr
	}
	return "Docker Compose version v2.30.0", nil
}

func TestParsePsArrayForm(t *testing.T) {
	in := `[{"ID":"abc","Name":"p-web-1","Service":"web","Image":"nginx","State":"running","Health":"healthy","ExitCode":0}]`
	got, err := parsePs(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Service != "web" {
		t.Errorf("parsed = %#v", got)
	}
}

func TestParsePsLineForm(t *testing.T) {
	in := `{"ID":"a","Name":"p-web-1","Service":"web","Image":"nginx","State":"running","Health":"","ExitCode":0}
{"ID":"b","Name":"p-cache-1","Service":"cache","Image":"redis","State":"running","Health":"","ExitCode":0}`
	got, err := parsePs(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Service != "web" || got[1].Service != "cache" {
		t.Errorf("parsed = %#v", got)
	}
}

func TestParsePsEmpty(t *testing.T) {
	got, err := parsePs("")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %#v", got)
	}
}

func TestParsePsBadArrayJSON(t *testing.T) {
	if _, err := parsePs("[not json"); err == nil {
		t.Error("expected error")
	}
}

func TestParsePsBadLineJSON(t *testing.T) {
	if _, err := parsePs("not json"); err == nil {
		t.Error("expected error")
	}
}

func TestNewClient(t *testing.T) {
	c := New("/tmp/c.yml", "proj")
	if c.File != "/tmp/c.yml" || c.Project != "proj" {
		t.Errorf("client = %+v", c)
	}
	if c.r == nil {
		t.Error("runner not initialised")
	}
}

func TestPullPassesServices(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	if err := c.Pull(context.Background(), "web", "cache"); err != nil {
		t.Fatal(err)
	}
	if len(r.composeLog) != 1 {
		t.Fatalf("expected 1 call, got %d", len(r.composeLog))
	}
	args := r.composeLog[0]
	want := []string{"f", "p", "pull", "web", "cache"}
	for i, w := range want {
		if i >= len(args) || args[i] != w {
			t.Errorf("arg[%d] = %q, want %q (full: %v)", i, args[i], w, args)
		}
	}
}

func TestPullNoArgs(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	if err := c.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	args := r.composeLog[0]
	// args = [f, p, pull]
	if args[len(args)-1] != "pull" {
		t.Errorf("expected last arg 'pull', got %v", args)
	}
}

func TestUpIncludesWait(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	if err := c.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.composeLog[0], " ")
	for _, want := range []string{"up", "-d", "--remove-orphans", "--wait"} {
		if !strings.Contains(got, want) {
			t.Errorf("Up missing %q in %q", want, got)
		}
	}
}

func TestForceRecreateIncludesWaitAndNoDeps(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	if err := c.ForceRecreate(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.composeLog[0], " ")
	for _, want := range []string{"--force-recreate", "--no-deps", "--wait", "web"} {
		if !strings.Contains(got, want) {
			t.Errorf("ForceRecreate missing %q in %q", want, got)
		}
	}
}

func TestPsParsesAndEnriches(t *testing.T) {
	r := &fakeRunner{
		composeOut: `[{"ID":"abc","Service":"web","Image":"nginx","State":"running"}]`,
		inspectOut: map[string]string{
			"abc": mustJSON([]map[string]any{
				{
					"Created": "2024-01-02T03:04:05Z",
					"Image":   "sha256:imgid",
					"Config": map[string]any{
						"Labels": map[string]string{
							"com.docker.compose.config-hash": "deadbeef",
						},
					},
				},
			}),
		},
	}
	c := &Client{File: "f", Project: "p", r: r}
	got, err := c.Ps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].ConfigHash != "deadbeef" {
		t.Errorf("ConfigHash = %q", got[0].ConfigHash)
	}
	if got[0].ImageID != "sha256:imgid" {
		t.Errorf("ImageID = %q", got[0].ImageID)
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("CreatedAt unset")
	}
}

func TestPsInspectError(t *testing.T) {
	r := &fakeRunner{
		composeOut: `[{"ID":"abc","Service":"web"}]`,
		inspectErr: errors.New("inspect boom"),
	}
	c := &Client{File: "f", Project: "p", r: r}
	if _, err := c.Ps(context.Background()); err == nil {
		t.Error("expected inspect error")
	}
}

func TestPsComposeError(t *testing.T) {
	r := &fakeRunner{composeErr: errors.New("compose boom")}
	c := &Client{File: "f", Project: "p", r: r}
	if _, err := c.Ps(context.Background()); err == nil {
		t.Error("expected compose error")
	}
}

func TestEnrichEmptyResult(t *testing.T) {
	r := &fakeRunner{
		composeOut: `[{"ID":"abc","Service":"web"}]`,
		inspectOut: map[string]string{"abc": "[]"},
	}
	c := &Client{File: "f", Project: "p", r: r}
	if _, err := c.Ps(context.Background()); err == nil {
		t.Error("expected error for empty inspect result")
	}
}

func TestEnrichBadInspectJSON(t *testing.T) {
	r := &fakeRunner{
		composeOut: `[{"ID":"abc","Service":"web"}]`,
		inspectOut: map[string]string{"abc": "not json"},
	}
	c := &Client{File: "f", Project: "p", r: r}
	if _, err := c.Ps(context.Background()); err == nil {
		t.Error("expected json parse error")
	}
}

func TestRunDockerNotFound(t *testing.T) {
	// runDocker is exercised by EnsureAvailable on real systems; here we
	// at least check the wrapper returns an error for an obviously bad
	// invocation by overriding PATH.
	t.Setenv("PATH", "/nonexistent")
	if err := EnsureAvailable(context.Background()); err == nil {
		t.Error("expected error when docker not on PATH")
	}
}

// mustJSON is a tiny helper for fixtures.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
