package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Dir is the state directory for a single stack.
//
// Layout:
//
//	<root>/<name>/
//	  compose.yml      latest fetched compose content
//	  git/             persistent git clone (only for --git sources)
type Dir struct {
	Root string
	Name string
}

// New constructs a Dir, creating it on disk.
func New(root, name string) (*Dir, error) {
	if root == "" {
		return nil, fmt.Errorf("state root is empty")
	}
	if name == "" {
		return nil, fmt.Errorf("stack name is empty")
	}
	d := &Dir{Root: root, Name: name}
	if err := os.MkdirAll(d.Path(), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", d.Path(), err)
	}
	return d, nil
}

// Path is the directory for this stack.
func (d *Dir) Path() string { return filepath.Join(d.Root, d.Name) }

// ComposeFile is the absolute path to the compose file used as the
// `-f` argument to docker compose.
func (d *Dir) ComposeFile() string { return filepath.Join(d.Path(), "compose.yml") }

// GitDir is the persistent git clone path for git sources.
func (d *Dir) GitDir() string { return filepath.Join(d.Path(), "git") }

// WriteCompose writes the compose content atomically and returns true if
// the on-disk file changed (content sha differs from the existing file).
func (d *Dir) WriteCompose(content []byte) (changed bool, err error) {
	path := d.ComposeFile()
	prev, _ := os.ReadFile(path)
	if sha(prev) == sha(content) && len(prev) > 0 {
		return false, nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return true, nil
}

// ReadCompose returns the last-written compose content, or os.ErrNotExist.
func (d *Dir) ReadCompose() ([]byte, error) { return os.ReadFile(d.ComposeFile()) }

func sha(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
