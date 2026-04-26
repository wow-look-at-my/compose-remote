package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewCreatesDir(t *testing.T) {
	root := t.TempDir()
	d, err := New(root, "stack1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Path() != filepath.Join(root, "stack1") {
		t.Errorf("Path() = %q", d.Path())
	}
	st, err := os.Stat(d.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsDir() {
		t.Error("expected dir")
	}
}

func TestNewRequiresArgs(t *testing.T) {
	if _, err := New("", "x"); err == nil {
		t.Error("empty root should error")
	}
	if _, err := New("/tmp", ""); err == nil {
		t.Error("empty name should error")
	}
}

func TestComposeFilePath(t *testing.T) {
	d, err := New(t.TempDir(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(d.ComposeFile()) != "compose.yml" {
		t.Errorf("ComposeFile() = %q", d.ComposeFile())
	}
	if filepath.Base(d.GitDir()) != "git" {
		t.Errorf("GitDir() = %q", d.GitDir())
	}
}

func TestWriteComposeAtomicAndIdempotent(t *testing.T) {
	d, err := New(t.TempDir(), "s")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := d.WriteCompose([]byte("a: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("first write should be a change")
	}
	// Same content -> not changed.
	changed, err = d.WriteCompose([]byte("a: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("rewriting same content should not be a change")
	}
	// Different content -> changed.
	changed, err = d.WriteCompose([]byte("a: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("different content should be a change")
	}
	got, err := d.ReadCompose()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a: 2\n" {
		t.Errorf("ReadCompose = %q", got)
	}
}

func TestReadComposeMissing(t *testing.T) {
	d, err := New(t.TempDir(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReadCompose(); !os.IsNotExist(err) {
		t.Errorf("expected ErrNotExist, got %v", err)
	}
}
