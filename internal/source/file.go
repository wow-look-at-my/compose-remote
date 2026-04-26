package source

import (
	"context"
	"fmt"
	"os"
)

// FileSource reads a compose file from the local filesystem.
type FileSource struct {
	Path string
}

// NewFile constructs a FileSource.
func NewFile(path string) *FileSource { return &FileSource{Path: path} }

// Name returns a short identifier for logs.
func (f *FileSource) Name() string { return "file:" + f.Path }

// Fetch reads the file fresh on every call. The Rev is mtime+size, which
// is sufficient for the cheap short-circuit and survives process restarts.
func (f *FileSource) Fetch(_ context.Context) (Result, error) {
	st, err := os.Stat(f.Path)
	if err != nil {
		return Result{}, fmt.Errorf("stat %s: %w", f.Path, err)
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", f.Path, err)
	}
	rev := fmt.Sprintf("%d:%d", st.ModTime().UnixNano(), st.Size())
	return Result{Content: b, Rev: rev}, nil
}
