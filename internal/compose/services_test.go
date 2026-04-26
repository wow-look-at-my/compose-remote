package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

const baseCompose = `services:
  web:
    image: nginx:1.25-alpine
    labels:
      app: web
    environment:
      FOO: bar
  cache:
    image: redis:7-alpine
`

func parseOK(t *testing.T, content, dir string) *Parsed {
	t.Helper()
	p, err := Parse([]byte(content), dir)
	require.Nil(t, err)

	return p
}

func TestParseEmitsTwoServices(t *testing.T) {
	p := parseOK(t, baseCompose, t.TempDir())
	require.Equal(t, 2, len(p.Services()))

	_, ok := p.Services()["web"]
	assert.True(t, ok)

	_, ok = p.Services()["cache"]
	assert.True(t, ok)
}

func TestParseRejectsEmpty(t *testing.T) {
	_, err := Parse([]byte(""), "")
	assert.NotNil(t, err)

	_, err = Parse([]byte("foo: bar\n"), "")
	assert.NotNil(t, err)
}

func TestHashStableAcrossParses(t *testing.T) {
	dir := t.TempDir()
	a := parseOK(t, baseCompose, dir)
	b := parseOK(t, baseCompose, dir)
	assert.Equal(t, b.Services()["web"].Hash, a.Services()["web"].Hash)

}

func TestHashChangesPerService(t *testing.T) {
	dir := t.TempDir()
	a := parseOK(t, baseCompose, dir)

	mutated := strings.Replace(baseCompose, "FOO: bar", "FOO: baz", 1)
	b := parseOK(t, mutated, dir)

	assert.NotEqual(t, b.Services()["web"].Hash, a.Services()["web"].Hash)

	assert.Equal(t, b.Services()["cache"].Hash, a.Services()["cache"].Hash)

}

func TestHashIncludesEnvFileContent(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "web.env")
	require.NoError(t, os.WriteFile(envPath, []byte("X=1\n"), 0o644))

	yml := `services:
  web:
    image: nginx
    env_file: web.env
`
	a := parseOK(t, yml, dir)
	require.NoError(t, os.WriteFile(envPath, []byte("X=2\n"), 0o644))

	b := parseOK(t, yml, dir)
	assert.NotEqual(t, b.Services()["web"].Hash, a.Services()["web"].Hash)

}

func TestMarshalInjectsLabel(t *testing.T) {
	p := parseOK(t, baseCompose, t.TempDir())
	out, err := p.Marshal()
	require.Nil(t, err)

	s := string(out)
	assert.Contains(t, s, LabelHash)

	// The hash for web should appear in the rendered file.
	assert.Contains(t, s, p.Services()["web"].Hash)

}

func TestInjectLabelListForm(t *testing.T) {
	yml := `services:
  web:
    image: nginx
    labels:
      - app=web
      - tier=front
`
	p := parseOK(t, yml, t.TempDir())
	out, _ := p.Marshal()
	s := string(out)
	assert.Contains(t, s, LabelHash+"="+p.Services()["web"].Hash)

}

func TestInjectLabelOverwrite(t *testing.T) {
	yml := `services:
  web:
    image: nginx
    labels:
      ` + LabelHash + `: stale
`
	p := parseOK(t, yml, t.TempDir())
	out, _ := p.Marshal()
	s := string(out)
	assert.NotContains(t, s, "stale")

}

func TestRoundTripStableWithListLabels(t *testing.T) {
	yml := `services:
  web:
    image: nginx
    labels:
      - app=web
`
	dir := t.TempDir()
	a := parseOK(t, yml, dir)
	rendered, err := a.Marshal()
	require.NoError(t, err)
	b, err := Parse(rendered, dir)
	require.NoError(t, err)
	// Hashes must match across a Parse->Marshal->Parse round-trip.
	assert.Equal(t, a.Services()["web"].Hash, b.Services()["web"].Hash)
}

func TestRoundTripStableWithMapLabels(t *testing.T) {
	yml := `services:
  web:
    image: nginx
    labels:
      app: web
`
	dir := t.TempDir()
	a := parseOK(t, yml, dir)
	rendered, err := a.Marshal()
	require.NoError(t, err)
	b, err := Parse(rendered, dir)
	require.NoError(t, err)
	assert.Equal(t, a.Services()["web"].Hash, b.Services()["web"].Hash)
}

func TestRoundTripStableWithNoLabels(t *testing.T) {
	yml := `services:
  web:
    image: nginx
`
	dir := t.TempDir()
	a := parseOK(t, yml, dir)
	rendered, err := a.Marshal()
	require.NoError(t, err)
	b, err := Parse(rendered, dir)
	require.NoError(t, err)
	assert.Equal(t, a.Services()["web"].Hash, b.Services()["web"].Hash)
}

func TestParseMissingEnvFile(t *testing.T) {
	// env_file pointing at a missing file should not crash; the hash
	// just bakes in the "missing" marker.
	yml := `services:
  web:
    image: nginx
    env_file: nonexistent.env
`
	p := parseOK(t, yml, t.TempDir())
	assert.NotEqual(t, "", p.Services()["web"].Hash)
}

func TestEnvFileSequenceForms(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.env"), []byte("A=1"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.env"), []byte("B=2"), 0o644))

	yml := `services:
  web:
    image: nginx
    env_file:
      - a.env
      - path: b.env
`
	p, err := Parse([]byte(yml), dir)
	require.Nil(t, err)

	assert.NotEqual(t, "", p.Services()["web"].Hash)

}
