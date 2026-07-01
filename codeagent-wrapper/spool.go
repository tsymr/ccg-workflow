package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Live output spooling.
//
// Each running task appends its content stream as JSON Lines to
// <liveDir>/<session-id>.jsonl. The `--view` aggregator (viewer.go) tails
// every spool file and renders all concurrent sessions in a single
// multi-panel web page. Because wrappers write files instead of each
// hosting their own HTTP server, N wrappers running in parallel never
// contend for a port, and the viewer needs only one SSH-forwarded port.

// liveDir returns the directory where per-session spool files are written.
// Override with CODEAGENT_LIVE_DIR; defaults to ~/.claude/.ccg/live.
func liveDir() string {
	if d := strings.TrimSpace(os.Getenv("CODEAGENT_LIVE_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "ccg-live")
	}
	return filepath.Join(home, ".claude", ".ccg", "live")
}

// spoolEvent is one JSON Lines record in a session spool file.
type spoolEvent struct {
	Type    string `json:"type"` // meta | reasoning | command | message | text | done
	Content string `json:"content,omitempty"`
	Backend string `json:"backend,omitempty"` // meta only
	Task    string `json:"task,omitempty"`    // meta only
	PID     int    `json:"pid,omitempty"`     // meta only
	TS      string `json:"ts,omitempty"`
}

// SpoolWriter appends a single session's live output to its spool file.
type SpoolWriter struct {
	mu   sync.Mutex
	f    *os.File
	path string
	done bool
}

// NewSpoolWriter creates the session spool file and writes its meta header.
func NewSpoolWriter(sessionID, backend, task string) (*SpoolWriter, error) {
	dir := liveDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, sanitizeSessionID(sessionID)+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	sw := &SpoolWriter{f: f, path: path}
	sw.write(spoolEvent{
		Type:    "meta",
		Backend: backend,
		Task:    task,
		PID:     os.Getpid(),
		TS:      time.Now().Format(time.RFC3339),
	})
	return sw, nil
}

func (sw *SpoolWriter) write(ev spoolEvent) {
	if sw == nil {
		return
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.f == nil {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	// A viewer on the same host reads from the shared page cache, so a plain
	// Write is immediately visible to it without an fsync.
	_, _ = sw.f.Write(append(data, '\n'))
}

// Content records one streamed chunk with its content type
// (reasoning | command | message | text).
func (sw *SpoolWriter) Content(content, contentType string) {
	if contentType == "" {
		contentType = "message"
	}
	sw.write(spoolEvent{Type: contentType, Content: content})
}

// Done marks the session complete. Idempotent: only the first call writes.
func (sw *SpoolWriter) Done() {
	if sw == nil {
		return
	}
	sw.mu.Lock()
	if sw.done || sw.f == nil {
		sw.mu.Unlock()
		return
	}
	sw.done = true
	sw.mu.Unlock()
	sw.write(spoolEvent{Type: "done", TS: time.Now().Format(time.RFC3339)})
}

// Close closes the spool file. Safe to call multiple times.
func (sw *SpoolWriter) Close() error {
	if sw == nil {
		return nil
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.f == nil {
		return nil
	}
	err := sw.f.Close()
	sw.f = nil
	return err
}

// sanitizeSessionID strips path separators so a session id can't escape liveDir.
func sanitizeSessionID(id string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", "..", "_", string(os.PathSeparator), "_")
	cleaned := strings.TrimSpace(repl.Replace(id))
	if cleaned == "" {
		return "session"
	}
	return cleaned
}

// readSpoolPID returns the PID recorded in a spool file's meta header line.
func readSpoolPID(path string) (int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	line, err := bufio.NewReader(f).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return 0, false
	}
	var ev spoolEvent
	if json.Unmarshal(bytes.TrimSpace(line), &ev) != nil || ev.PID <= 0 {
		return 0, false
	}
	return ev.PID, true
}

// cleanupOldLiveFiles removes spool files whose owning process is gone and
// that have been idle longer than grace. Files owned by a running process are
// always kept (an active task may still be writing); recently finished files
// are kept for a while so the viewer can still replay them. Best-effort.
func cleanupOldLiveFiles(grace time.Duration) {
	dir := liveDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if pid, ok := readSpoolPID(path); ok && processRunningCheck(pid) {
			continue // owning process still alive
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) < grace {
			continue // recently finished; keep for replay
		}
		_ = os.Remove(path)
	}
}
