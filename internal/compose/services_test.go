package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseEmitsTwoServices(t *testing.T) {
	p := parseOK(t, baseCompose, t.TempDir())
	if len(p.Services()) != 2 {
		t.Fatalf("want 2 services, got %d", len(p.Services()))
	}
	if _, ok := p.Services()["web"]; !ok {
		t.Error("missing web")
	}
	if _, ok := p.Services()["cache"]; !ok {
		t.Error("missing cache")
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse([]byte(""), ""); err == nil {
		t.Error("expected error for empty input")
	}
	if _, err := Parse([]byte("foo: bar\n"), ""); err == nil {
		t.Error("expected error when services: missing")
	}
}

func TestHashStableAcrossParses(t *testing.T) {
	dir := t.TempDir()
	a := parseOK(t, baseCompose, dir)
	b := parseOK(t, baseCompose, dir)
	if a.Services()["web"].Hash != b.Services()["web"].Hash {
		t.Error("hash should be stable across parses of the same content")
	}
}

func TestHashChangesPerService(t *testing.T) {
	dir := t.TempDir()
	a := parseOK(t, baseCompose, dir)

	mutated := strings.Replace(baseCompose, "FOO: bar", "FOO: baz", 1)
	b := parseOK(t, mutated, dir)

	if a.Services()["web"].Hash == b.Services()["web"].Hash {
		t.Error("web hash should change when its env changed")
	}
	if a.Services()["cache"].Hash != b.Services()["cache"].Hash {
		t.Error("cache hash should not change when only web changed")
	}
}

func TestHashIncludesEnvFileContent(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "web.env")
	if err := os.WriteFile(envPath, []byte("X=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yml := `services:
  web:
    image: nginx
    env_file: web.env
`
	a := parseOK(t, yml, dir)
	if err := os.WriteFile(envPath, []byte("X=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := parseOK(t, yml, dir)
	if a.Services()["web"].Hash == b.Services()["web"].Hash {
		t.Error("hash must change when env_file content changes (the docker compose bug case)")
	}
}

func TestMarshalInjectsLabel(t *testing.T) {
	p := parseOK(t, baseCompose, t.TempDir())
	out, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, LabelHash) {
		t.Errorf("marshalled output missing %s label:\n%s", LabelHash, s)
	}
	// The hash for web should appear in the rendered file.
	if !strings.Contains(s, p.Services()["web"].Hash) {
		t.Error("web hash not in rendered file")
	}
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
	if !strings.Contains(s, LabelHash+"="+p.Services()["web"].Hash) {
		t.Errorf("list-form labels: missing entry %q in:\n%s", LabelHash, s)
	}
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
	if strings.Contains(s, "stale") {
		t.Errorf("expected stale value to be overwritten, got:\n%s", s)
	}
}

func TestEnvFileSequenceForms(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.env"), []byte("A=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.env"), []byte("B=2"), 0o644); err != nil {
		t.Fatal(err)
	}
	yml := `services:
  web:
    image: nginx
    env_file:
      - a.env
      - path: b.env
`
	p, err := Parse([]byte(yml), dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Services()["web"].Hash == "" {
		t.Error("hash unexpectedly empty")
	}
}
