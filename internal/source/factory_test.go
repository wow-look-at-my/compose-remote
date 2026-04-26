package source

import (
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestFactoryNoFlags(t *testing.T) {
	_, err := New(Flags{})
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestFactoryConflictingFlags(t *testing.T) {
	_, err := New(Flags{File: "/a", URL: "https://b"})
	assert.NotNil(t, err)

}

func TestFactoryFile(t *testing.T) {
	s, err := New(Flags{File: "compose.yml"})
	require.Nil(t, err)

	_, ok := s.(*FileSource)
	assert.True(t, ok)

}

func TestFactoryHTTP(t *testing.T) {
	s, err := New(Flags{URL: "https://example.com/compose.yml"})
	require.Nil(t, err)

	_, ok := s.(*HTTPSource)
	assert.True(t, ok)

}

func TestFactoryHTTPBadScheme(t *testing.T) {
	_, err := New(Flags{URL: "ftp://example.com"})
	assert.NotNil(t, err)

}

func TestFactoryGit(t *testing.T) {
	s, err := New(Flags{Git: "https://example.com/x.git", StateDir: "/tmp/x"})
	require.Nil(t, err)

	_, ok := s.(*GitSource)
	assert.True(t, ok)

}

func TestFactoryGitNoStateDir(t *testing.T) {
	_, err := New(Flags{Git: "https://example.com/x.git"})
	assert.NotNil(t, err)

}
