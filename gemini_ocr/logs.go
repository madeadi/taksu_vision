// In-memory ring buffer of recent log lines, exposed via GET /logs for the
// web app's Logs tab (see web/src/components/gemini-logs.tsx). Not
// persisted across restarts — scripts/run_pipeline.sh already redirects
// stdout/stderr to a log file on disk for that.
package main

import (
	"strings"
	"sync"
)

const logBufferCapacity = 1000

// logRing backs GET /logs. Set once in main() before any logging happens.
var logRing *logRingWriter

type logRingWriter struct {
	mu     sync.Mutex
	lines  []string
	next   int
	filled bool
}

func newLogRingWriter(capacity int) *logRingWriter {
	return &logRingWriter{lines: make([]string, capacity)}
}

// Write implements io.Writer, so it can be used as one of log.SetOutput's
// io.MultiWriter targets. Each call is treated as one log line.
func (w *logRingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines[w.next] = strings.TrimRight(string(p), "\n")
	w.next = (w.next + 1) % len(w.lines)
	if w.next == 0 {
		w.filled = true
	}
	return len(p), nil
}

// Lines returns buffered lines oldest-first.
func (w *logRingWriter) Lines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.filled {
		out := make([]string, w.next)
		copy(out, w.lines[:w.next])
		return out
	}
	out := make([]string, len(w.lines))
	copy(out, w.lines[w.next:])
	copy(out[len(w.lines)-w.next:], w.lines[:w.next])
	return out
}
