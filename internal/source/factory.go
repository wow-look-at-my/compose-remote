package source

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Flags is the user-provided source configuration parsed by cobra.
type Flags struct {
	File    string
	URL     string
	Git     string
	GitRef  string
	GitPath string
	GitSSH  string

	// StateDir is where persistent caches (the git clone) live.
	StateDir string
}

// New picks a Source backend based on which mutually-exclusive flag was set.
func New(f Flags) (Source, error) {
	chosen := 0
	if f.File != "" {
		chosen++
	}
	if f.URL != "" {
		chosen++
	}
	if f.Git != "" {
		chosen++
	}
	switch chosen {
	case 0:
		return nil, errors.New("one of --file, --url, or --git is required")
	case 1:
	default:
		return nil, errors.New("--file, --url, and --git are mutually exclusive")
	}

	switch {
	case f.File != "":
		abs, err := filepath.Abs(f.File)
		if err != nil {
			return nil, fmt.Errorf("resolve --file: %w", err)
		}
		return NewFile(abs), nil
	case f.URL != "":
		if !strings.HasPrefix(f.URL, "http://") && !strings.HasPrefix(f.URL, "https://") {
			return nil, fmt.Errorf("--url must be http(s)")
		}
		return NewHTTP(f.URL, nil), nil
	case f.Git != "":
		if f.StateDir == "" {
			return nil, errors.New("git source requires a state dir")
		}
		workDir := filepath.Join(f.StateDir, "git")
		return NewGit(f.Git, f.GitRef, f.GitPath, workDir, f.GitSSH), nil
	}
	return nil, errors.New("unreachable")
}
