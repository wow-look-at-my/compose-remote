package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestFileSourceFetch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	require.NoError(t, os.WriteFile(path, []byte("services: {}\n"), 0o644))

	s := NewFile(path)
	assert.NotEqual(t, "", s.Name())

	r, err := s.Fetch(context.Background())
	require.Nil(t, err)

	assert.Equal(t, "services: {}\n", string(r.Content))

	assert.NotEqual(t, "", r.Rev)

	assert.False(t, r.NotModified)

}

func TestFileSourceMissing(t *testing.T) {
	s := NewFile(filepath.Join(t.TempDir(), "missing"))
	_, err := s.Fetch(context.Background())
	assert.NotNil(t, err)

}
