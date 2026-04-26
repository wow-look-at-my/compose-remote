package source

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// GitSource clones (once) and fetches+checks-out a ref on each Fetch.
// The compose file at GitPath inside the working tree is returned as
// Result.Content; the resolved commit SHA is the Rev.
type GitSource struct {
	Repo    string // e.g. https://github.com/foo/bar.git
	Ref     string // branch / tag / sha; empty means HEAD of default branch
	GitPath string // path inside the repo to the compose file
	WorkDir string // local clone location (managed by state pkg, persistent)
	SSHKey  string // optional path passed via GIT_SSH_COMMAND

	mu sync.Mutex
}

// NewGit constructs a GitSource.
func NewGit(repo, ref, gitPath, workDir, sshKey string) *GitSource {
	if gitPath == "" {
		gitPath = "docker-compose.yml"
	}
	return &GitSource{
		Repo:    repo,
		Ref:     ref,
		GitPath: gitPath,
		WorkDir: workDir,
		SSHKey:  sshKey,
	}
}

// Name returns a short identifier for logs.
func (g *GitSource) Name() string {
	if g.Ref == "" {
		return "git:" + g.Repo + "#" + g.GitPath
	}
	return "git:" + g.Repo + "@" + g.Ref + "#" + g.GitPath
}

// Fetch ensures the local clone exists and is at the requested ref, then
// reads the compose file at GitPath. Rev is the resolved commit SHA.
func (g *GitSource) Fetch(ctx context.Context) (Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(g.WorkDir), 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir parent: %w", err)
	}

	if !g.cloned() {
		if err := g.clone(ctx); err != nil {
			return Result{}, err
		}
	}
	if err := g.fetchAndCheckout(ctx); err != nil {
		return Result{}, err
	}

	sha, err := g.resolveHead(ctx)
	if err != nil {
		return Result{}, err
	}
	composePath := filepath.Join(g.WorkDir, g.GitPath)
	b, err := os.ReadFile(composePath)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", composePath, err)
	}
	return Result{Content: b, Rev: sha}, nil
}

func (g *GitSource) cloned() bool {
	st, err := os.Stat(filepath.Join(g.WorkDir, ".git"))
	return err == nil && st.IsDir()
}

func (g *GitSource) clone(ctx context.Context) error {
	args := []string{"clone", "--depth=1"}
	if g.Ref != "" && !looksLikeSHA(g.Ref) {
		args = append(args, "--branch", g.Ref)
	}
	args = append(args, g.Repo, g.WorkDir)
	if _, err := g.runGit(ctx, "", args...); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}
	return nil
}

func (g *GitSource) fetchAndCheckout(ctx context.Context) error {
	if _, err := g.runGit(ctx, g.WorkDir, "fetch", "--depth=1", "origin", g.fetchRef()); err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	target := "FETCH_HEAD"
	if looksLikeSHA(g.Ref) {
		target = g.Ref
	}
	if _, err := g.runGit(ctx, g.WorkDir, "reset", "--hard", target); err != nil {
		return fmt.Errorf("git reset: %w", err)
	}
	return nil
}

func (g *GitSource) fetchRef() string {
	if g.Ref == "" {
		return "HEAD"
	}
	return g.Ref
}

func (g *GitSource) resolveHead(ctx context.Context) (string, error) {
	out, err := g.runGit(ctx, g.WorkDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (g *GitSource) runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if g.SSHKey != "" {
		cmd.Env = append(os.Environ(),
			"GIT_SSH_COMMAND=ssh -i "+g.SSHKey+" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new",
		)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func looksLikeSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
