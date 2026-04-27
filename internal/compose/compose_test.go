package compose

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

type fakeRunner struct {
	composeOut	string
	composeErr	error
	composeLog	[][]string

	inspectOut	map[string]string
	inspectErr	error
	inspectLog	[]string

	imageInspectOut	map[string]string
	imageInspectErr	map[string]error
	imageInspectLog	[]string

	versionErr	error

	networkInspectOut	map[string]string
	networkInspectErr	map[string]error
	networkCreateErr	error
	networksCreated		[]string

	volumeInspectOut	map[string]string
	volumeInspectErr	map[string]error
	volumeCreateErr		error
	volumesCreated		[]string
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

func (f *fakeRunner) imageInspect(_ context.Context, image string) (string, error) {
	f.imageInspectLog = append(f.imageInspectLog, image)
	if f.imageInspectErr != nil {
		if err, ok := f.imageInspectErr[image]; ok {
			return "", err
		}
	}
	if v, ok := f.imageInspectOut[image]; ok {
		return v, nil
	}
	return "", nil
}

func (f *fakeRunner) version(_ context.Context) (string, error) {
	if f.versionErr != nil {
		return "", f.versionErr
	}
	return "Docker Compose version v2.30.0", nil
}

func (f *fakeRunner) networkInspect(_ context.Context, name string) (string, error) {
	if v, ok := f.networkInspectOut[name]; ok {
		return v, nil
	}
	if err, ok := f.networkInspectErr[name]; ok {
		return "", err
	}
	return "", nil
}

func (f *fakeRunner) networkCreate(_ context.Context, name string) error {
	f.networksCreated = append(f.networksCreated, name)
	return f.networkCreateErr
}

func (f *fakeRunner) volumeInspect(_ context.Context, name string) (string, error) {
	if v, ok := f.volumeInspectOut[name]; ok {
		return v, nil
	}
	if err, ok := f.volumeInspectErr[name]; ok {
		return "", err
	}
	return "", nil
}

func (f *fakeRunner) volumeCreate(_ context.Context, name string) error {
	f.volumesCreated = append(f.volumesCreated, name)
	return f.volumeCreateErr
}

func TestParsePsArrayForm(t *testing.T) {
	in := `[{"ID":"abc","Name":"p-web-1","Service":"web","Image":"nginx","State":"running","Health":"healthy","ExitCode":0}]`
	got, err := parsePs(in)
	require.Nil(t, err)

	assert.False(t, len(got) != 1 || got[0].Service != "web")

}

func TestParsePsLineForm(t *testing.T) {
	in := `{"ID":"a","Name":"p-web-1","Service":"web","Image":"nginx","State":"running","Health":"","ExitCode":0}
{"ID":"b","Name":"p-cache-1","Service":"cache","Image":"redis","State":"running","Health":"","ExitCode":0}`
	got, err := parsePs(in)
	require.Nil(t, err)

	require.Equal(t, 2, len(got))

	assert.False(t, got[0].Service != "web" || got[1].Service != "cache")

}

func TestParsePsEmpty(t *testing.T) {
	got, err := parsePs("")
	require.Nil(t, err)

	assert.Nil(t, got)

}

func TestParsePsBadArrayJSON(t *testing.T) {
	_, err := parsePs("[not json")
	assert.NotNil(t, err)

}

func TestParsePsBadLineJSON(t *testing.T) {
	_, err := parsePs("not json")
	assert.NotNil(t, err)

}

func TestNewClient(t *testing.T) {
	c := New("/tmp/c.yml", "proj")
	assert.False(t, c.File != "/tmp/c.yml" || c.Project != "proj")

	assert.NotNil(t, c.r)

}

func TestPullPassesServices(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	require.NoError(t, c.Pull(context.Background(), "web", "cache"))

	require.Equal(t, 1, len(r.composeLog))

	args := r.composeLog[0]
	want := []string{"f", "p", "pull", "web", "cache"}
	for i, w := range want {
		assert.False(t, i >= len(args) || args[i] != w)

	}
}

func TestPullNoArgs(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	require.NoError(t, c.Pull(context.Background()))

	args := r.composeLog[0]
	// args = [f, p, pull]
	assert.Equal(t, "pull", args[len(args)-1])

}

func TestUpIncludesWait(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	require.NoError(t, c.Up(context.Background()))

	got := strings.Join(r.composeLog[0], " ")
	for _, want := range []string{"up", "-d", "--remove-orphans", "--wait"} {
		assert.Contains(t, got, want)

	}
}

func TestForceRecreateIncludesWaitAndNoDeps(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	require.NoError(t, c.ForceRecreate(context.Background(), "web"))

	got := strings.Join(r.composeLog[0], " ")
	for _, want := range []string{"--force-recreate", "--no-deps", "--wait", "web"} {
		assert.Contains(t, got, want)

	}
}

func TestPsParsesAndEnriches(t *testing.T) {
	r := &fakeRunner{
		composeOut:	`[{"ID":"abc","Service":"web","Image":"nginx","State":"running"}]`,
		inspectOut: map[string]string{
			"abc": mustJSON([]map[string]any{
				{
					"Created":	"2024-01-02T03:04:05Z",
					"Image":	"sha256:imgid",
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
	require.Nil(t, err)

	require.Equal(t, 1, len(got))

	assert.Equal(t, "deadbeef", got[0].ConfigHash)

	assert.Equal(t, "sha256:imgid", got[0].ImageID)

	assert.False(t, got[0].CreatedAt.IsZero())

}

func TestPsInspectError(t *testing.T) {
	r := &fakeRunner{
		composeOut:	`[{"ID":"abc","Service":"web"}]`,
		inspectErr:	errors.New("inspect boom"),
	}
	c := &Client{File: "f", Project: "p", r: r}
	_, err := c.Ps(context.Background())
	assert.NotNil(t, err)

}

func TestPsComposeError(t *testing.T) {
	r := &fakeRunner{composeErr: errors.New("compose boom")}
	c := &Client{File: "f", Project: "p", r: r}
	_, err := c.Ps(context.Background())
	assert.NotNil(t, err)

}

func TestEnrichEmptyResult(t *testing.T) {
	r := &fakeRunner{
		composeOut:	`[{"ID":"abc","Service":"web"}]`,
		inspectOut:	map[string]string{"abc": "[]"},
	}
	c := &Client{File: "f", Project: "p", r: r}
	_, err := c.Ps(context.Background())
	assert.NotNil(t, err)

}

func TestEnrichBadInspectJSON(t *testing.T) {
	r := &fakeRunner{
		composeOut:	`[{"ID":"abc","Service":"web"}]`,
		inspectOut:	map[string]string{"abc": "not json"},
	}
	c := &Client{File: "f", Project: "p", r: r}
	_, err := c.Ps(context.Background())
	assert.NotNil(t, err)

}

func TestImageIDReturnsDigest(t *testing.T) {
	r := &fakeRunner{
		imageInspectOut: map[string]string{
			"traefik:v3": "sha256:deadbeef\n",
		},
	}
	c := &Client{File: "f", Project: "p", r: r}
	id, err := c.ImageID(context.Background(), "traefik:v3")
	require.NoError(t, err)

	assert.Equal(t, "sha256:deadbeef", id)

	require.Equal(t, 1, len(r.imageInspectLog))

	assert.Equal(t, "traefik:v3", r.imageInspectLog[0])

}

func TestImageIDReturnsEmptyForMissingImage(t *testing.T) {
	r := &fakeRunner{
		imageInspectErr: map[string]error{
			"missing:tag": errors.New("Error: No such image: missing:tag"),
		},
	}
	c := &Client{File: "f", Project: "p", r: r}
	id, err := c.ImageID(context.Background(), "missing:tag")
	require.NoError(t, err)

	assert.Equal(t, "", id)

}

func TestImageIDPropagatesOtherErrors(t *testing.T) {
	r := &fakeRunner{
		imageInspectErr: map[string]error{
			"img:tag": errors.New("docker daemon unreachable"),
		},
	}
	c := &Client{File: "f", Project: "p", r: r}
	_, err := c.ImageID(context.Background(), "img:tag")
	assert.NotNil(t, err)

}

func TestRunDockerNotFound(t *testing.T) {
	// runDocker is exercised by EnsureAvailable on real systems; here we
	// at least check the wrapper returns an error for an obviously bad
	// invocation by overriding PATH.
	t.Setenv("PATH", "/nonexistent")
	err := EnsureAvailable(context.Background())
	assert.NotNil(t, err)

}

// mustJSON is a tiny helper for fixtures.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
