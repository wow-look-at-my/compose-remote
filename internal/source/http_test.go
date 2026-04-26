package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
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
	require.Nil(t, err)

	assert.Equal(t, string(body), string(r1.Content))

	assert.Equal(t, `"v1"`, r1.Rev)

	assert.False(t, r1.NotModified)

	r2, err := s.Fetch(context.Background())
	require.Nil(t, err)

	assert.True(t, r2.NotModified)

	assert.Equal(t, `"v1"`, r2.Rev)

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
	require.Nil(t, err)

	assert.Equal(t, "Wed, 21 Oct 2015 07:28:00 GMT", r.Rev)

}

func TestHTTPSourceErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := NewHTTP(srv.URL, nil)
	_, err := s.Fetch(context.Background())
	assert.NotNil(t, err)

}

func TestHTTPSourceName(t *testing.T) {
	assert.Equal(t, "http:https://x", NewHTTP("https://x", nil).Name())

}
