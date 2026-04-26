package source

import "context"

// Source is anything that can produce a docker-compose.yml on demand.
//
// Fetch is called every reconcile tick. Implementations should be cheap on
// the no-change path (HTTP ETag, git fetch on a shallow clone, file stat).
// The returned Rev is an opaque revision identifier whose semantics are
// up to the implementation; callers may log it but must not rely on its
// format. NotModified must be returned (with empty Content/Rev) when the
// implementation can prove the source hasn't changed since the last fetch
// AND it has no cheap way to re-emit the same content; callers fall back
// to the previously cached content in that case.
type Source interface {
	// Name returns a short identifier used in logs.
	Name() string
	// Fetch returns the current desired compose content.
	Fetch(ctx context.Context) (Result, error)
}

// Result is the output of Source.Fetch.
type Result struct {
	// Content is the raw compose YAML bytes.
	Content []byte
	// Rev is an opaque per-source revision identifier (e.g. git SHA, HTTP
	// ETag, file mtime+size). Empty if the source has no notion of a rev.
	Rev string
	// NotModified is true when the source confirmed the content is the
	// same as the last fetch (e.g. HTTP 304). When true, Content may be
	// empty and the caller should reuse its cached copy.
	NotModified bool
}
