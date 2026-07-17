package main

import (
	"net/http"
	"sync"
)

// ringBuffer is an io.Writer that keeps only the most recent N writes.
// log.SetOutput writes one line per log call, so each Write here is one
// log entry. Used to make logs visible on an in-app page, since a
// windowsgui build has no console to show them in.
type ringBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func (b *ringBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, string(p))
	if len(b.lines) > b.max {
		b.lines = b.lines[len(b.lines)-b.max:]
	}
	return len(p), nil
}

// Snapshot returns the buffered log lines, most recent first.
func (b *ringBuffer) Snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	for i, line := range b.lines {
		out[len(b.lines)-1-i] = line
	}
	return out
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	render(w, "logs", logRing.Snapshot())
}
