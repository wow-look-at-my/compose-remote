package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func gitOK(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// initBareWithFile creates a bare repo seeded with a single file at path.
// Returns the bare repo URL (filesystem path) and the resolved commit SHA.
func initBareWithFile(t *testing.T, path, content string) (repoURL, sha string) {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "bare.git")
	work := t.TempDir()

	run := func(dir string, args ...string) string {
		// Disable any host-level signing config so commits in tests
		// don't depend on signing infrastructure.
		full := append([]string{
			"-c", "commit.gpgsign=false",
			"-c", "tag.gpgsign=false",
			"-c", "gpg.format=openpgp",
		}, args...)
		cmd := exec.Command("git", full...)
		if dir != "" {
			cmd.Dir = dir
		}
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		require.Nil(t, err)

		return strings.TrimSpace(string(out))
	}
	run("", "init", "--bare", "-b", "main", bare)
	run("", "clone", bare, work)
	run(work, "checkout", "-b", "main")
	abs := filepath.Join(work, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))

	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))

	run(work, "add", path)
	run(work, "commit", "-m", "init")
	run(work, "push", "-u", "origin", "main")
	sha = run(work, "rev-parse", "HEAD")
	return bare, sha
}

func TestGitSourceFetch(t *testing.T) {
	gitOK(t)
	repo, sha := initBareWithFile(t, "compose.yml", "services: {}\n")
	dst := filepath.Join(t.TempDir(), "clone")

	g := NewGit(repo, "main", "compose.yml", dst, "")
	assert.NotEqual(t, "", g.Name())

	r, err := g.Fetch(context.Background())
	require.Nil(t, err)

	assert.Equal(t, "services: {}\n", string(r.Content))

	assert.Equal(t, sha, r.Rev)

	// Second fetch on the same dir reuses the existing clone.
	r2, err := g.Fetch(context.Background())
	require.Nil(t, err)

	assert.Equal(t, sha, r2.Rev)

}

func TestGitSourceDefaultPath(t *testing.T) {
	g := NewGit("repo", "", "", "/tmp/x", "")
	assert.Equal(t, "docker-compose.yml", g.GitPath)

}

func TestLooksLikeSHA(t *testing.T) {
	cases := map[string]bool{
		"abc1234":	true,
		"abcdef1234567890abcdef1234567890abcdef12":	true,
		"main":		false,
		"":		false,
		"abc":		false,	// too short
		"xyz1234":	false,
	}
	for in, want := range cases {
		got := looksLikeSHA(in)
		assert.Equal(t, want, got)

	}
}
