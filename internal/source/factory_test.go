package source

import (
	"strings"
	"testing"
)

func TestFactoryNoFlags(t *testing.T) {
	if _, err := New(Flags{}); err == nil {
		t.Error("expected error when no flag is set")
	} else if !strings.Contains(err.Error(), "required") {
		t.Errorf("expected 'required' error, got %v", err)
	}
}

func TestFactoryConflictingFlags(t *testing.T) {
	if _, err := New(Flags{File: "/a", URL: "https://b"}); err == nil {
		t.Error("expected mutually-exclusive error")
	}
}

func TestFactoryFile(t *testing.T) {
	s, err := New(Flags{File: "compose.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*FileSource); !ok {
		t.Errorf("got %T, want *FileSource", s)
	}
}

func TestFactoryHTTP(t *testing.T) {
	s, err := New(Flags{URL: "https://example.com/compose.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*HTTPSource); !ok {
		t.Errorf("got %T, want *HTTPSource", s)
	}
}

func TestFactoryHTTPBadScheme(t *testing.T) {
	if _, err := New(Flags{URL: "ftp://example.com"}); err == nil {
		t.Error("expected error for non-http scheme")
	}
}

func TestFactoryGit(t *testing.T) {
	s, err := New(Flags{Git: "https://example.com/x.git", StateDir: "/tmp/x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*GitSource); !ok {
		t.Errorf("got %T, want *GitSource", s)
	}
}

func TestFactoryGitNoStateDir(t *testing.T) {
	if _, err := New(Flags{Git: "https://example.com/x.git"}); err == nil {
		t.Error("expected error without state dir")
	}
}
