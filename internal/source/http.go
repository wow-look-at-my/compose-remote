package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// HTTPSource fetches a compose file over HTTP(S). It uses ETag / Last-Modified
// caching so repeated polls of an unchanged URL return NotModified=true with
// no body transfer (HTTP 304).
type HTTPSource struct {
	URL    string
	Client *http.Client

	mu           sync.Mutex
	etag         string
	lastModified string
}

// NewHTTP constructs an HTTPSource. If client is nil a default with a
// reasonable timeout is used.
func NewHTTP(url string, client *http.Client) *HTTPSource {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPSource{URL: url, Client: client}
}

// Name returns a short identifier for logs.
func (h *HTTPSource) Name() string { return "http:" + h.URL }

// Fetch performs a conditional GET and returns the body or NotModified=true.
func (h *HTTPSource) Fetch(ctx context.Context) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return Result{}, err
	}
	h.mu.Lock()
	if h.etag != "" {
		req.Header.Set("If-None-Match", h.etag)
	}
	if h.lastModified != "" {
		req.Header.Set("If-Modified-Since", h.lastModified)
	}
	h.mu.Unlock()

	resp, err := h.Client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("http get %s: %w", h.URL, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return Result{NotModified: true, Rev: h.currentRev()}, nil
	case http.StatusOK:
	default:
		return Result{}, fmt.Errorf("http get %s: status %d", h.URL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("read body %s: %w", h.URL, err)
	}

	h.mu.Lock()
	h.etag = resp.Header.Get("ETag")
	h.lastModified = resp.Header.Get("Last-Modified")
	rev := h.currentRevLocked()
	h.mu.Unlock()
	return Result{Content: body, Rev: rev}, nil
}

func (h *HTTPSource) currentRev() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.currentRevLocked()
}

func (h *HTTPSource) currentRevLocked() string {
	if h.etag != "" {
		return h.etag
	}
	return h.lastModified
}
