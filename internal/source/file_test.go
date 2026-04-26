package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSourceFetch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(path, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewFile(path)
	if s.Name() == "" {
		t.Error("Name() is empty")
	}
	r, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Content) != "services: {}\n" {
		t.Errorf("Content = %q", r.Content)
	}
	if r.Rev == "" {
		t.Error("Rev is empty")
	}
	if r.NotModified {
		t.Error("NotModified should be false")
	}
}

func TestFileSourceMissing(t *testing.T) {
	s := NewFile(filepath.Join(t.TempDir(), "missing"))
	if _, err := s.Fetch(context.Background()); err == nil {
		t.Error("expected error for missing file")
	}
}
