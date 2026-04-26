package log

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var mu sync.Mutex

// KV is a key/value pair for structured logging.
type KV struct {
	K string
	V any
}

func emit(w *os.File, level, msg string, kv ...KV) {
	mu.Lock()
	defer mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "ts=%s level=%s msg=%q",
		time.Now().UTC().Format(time.RFC3339Nano), level, msg)
	for _, p := range kv {
		fmt.Fprintf(&b, " %s=%s", p.K, formatValue(p.V))
	}
	b.WriteByte('\n')
	_, _ = w.WriteString(b.String())
}

func formatValue(v any) string {
	s := fmt.Sprintf("%v", v)
	if strings.ContainsAny(s, " \t\"\\") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// Info logs at info level to stdout.
func Info(msg string, kv ...KV) { emit(os.Stdout, "info", msg, kv...) }

// Warn logs at warn level to stderr.
func Warn(msg string, kv ...KV) { emit(os.Stderr, "warn", msg, kv...) }

// Error logs at error level to stderr.
func Error(msg string, kv ...KV) { emit(os.Stderr, "error", msg, kv...) }

// Debug logs at debug level to stdout when COMPOSE_REMOTE_DEBUG is set.
func Debug(msg string, kv ...KV) {
	if os.Getenv("COMPOSE_REMOTE_DEBUG") == "" {
		return
	}
	emit(os.Stdout, "debug", msg, kv...)
}
