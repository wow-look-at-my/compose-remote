package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHTTPSourceETagFlow(t *testing.T) {
	body := []byte("services: {}\n")
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("ETag", `"v1"`)
		if inm := r.Header.Get("If-None-Match"); inm == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	s := NewHTTP(srv.URL, nil)
	r1, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(r1.Content) != string(body) {
		t.Errorf("first fetch content = %q", r1.Content)
	}
	if r1.Rev != `"v1"` {
		t.Errorf("first fetch rev = %q", r1.Rev)
	}
	if r1.NotModified {
		t.Error("first fetch should not be NotModified")
	}

	r2, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r2.NotModified {
		t.Error("second fetch should be NotModified (304 via ETag)")
	}
	if r2.Rev != `"v1"` {
		t.Errorf("second fetch rev = %q", r2.Rev)
	}
}

func TestHTTPSourceLastModifiedFallback(t *testing.T) {
	body := []byte("services: {}\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	s := NewHTTP(srv.URL, nil)
	r, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Rev != "Wed, 21 Oct 2015 07:28:00 GMT" {
		t.Errorf("rev = %q", r.Rev)
	}
}

func TestHTTPSourceErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := NewHTTP(srv.URL, nil)
	if _, err := s.Fetch(context.Background()); err == nil {
		t.Error("expected error for 500")
	}
}

func TestHTTPSourceName(t *testing.T) {
	if NewHTTP("https://x", nil).Name() != "http:https://x" {
		t.Error("Name() format")
	}
}
