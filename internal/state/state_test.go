package state

import (
	"os"
	"path/filepath"
	"testing"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestNewCreatesDir(t *testing.T) {
	root := t.TempDir()
	d, err := New(root, "stack1")
	require.Nil(t, err)

	assert.Equal(t, filepath.Join(root, "stack1"), d.Path())

	st, err := os.Stat(d.Path())
	require.Nil(t, err)

	assert.True(t, st.IsDir())

}

func TestNewRequiresArgs(t *testing.T) {
	_, err := New("", "x")
	assert.NotNil(t, err)

	_, err = New("/tmp", "")
	assert.NotNil(t, err)
}

func TestComposeFilePath(t *testing.T) {
	d, err := New(t.TempDir(), "s")
	require.Nil(t, err)

	assert.Equal(t, "compose.yml", filepath.Base(d.ComposeFile()))

	assert.Equal(t, "git", filepath.Base(d.GitDir()))

}

func TestWriteComposeAtomicAndIdempotent(t *testing.T) {
	d, err := New(t.TempDir(), "s")
	require.Nil(t, err)

	changed, err := d.WriteCompose([]byte("a: 1\n"))
	require.Nil(t, err)

	assert.True(t, changed)

	// Same content -> not changed.
	changed, err = d.WriteCompose([]byte("a: 1\n"))
	require.Nil(t, err)

	assert.False(t, changed)

	// Different content -> changed.
	changed, err = d.WriteCompose([]byte("a: 2\n"))
	require.Nil(t, err)

	assert.True(t, changed)

	got, err := d.ReadCompose()
	require.Nil(t, err)

	assert.Equal(t, "a: 2\n", string(got))

}

func TestReadComposeMissing(t *testing.T) {
	d, err := New(t.TempDir(), "s")
	require.Nil(t, err)

	_, err = d.ReadCompose()
	assert.True(t, os.IsNotExist(err))
}
