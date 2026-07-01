package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpoolWriterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAGENT_LIVE_DIR", dir)

	sw, err := NewSpoolWriter("codex-123-abcd", "codex", "do the thing")
	if err != nil {
		t.Fatalf("NewSpoolWriter: %v", err)
	}
	sw.Content("reason here", "reasoning")
	sw.Content("go build", "command")
	sw.Content("plain", "") // empty type defaults to message
	sw.Done()
	if err := sw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "codex-123-abcd.jsonl"))
	if err != nil {
		t.Fatalf("read spool: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 JSONL lines, got %d: %q", len(lines), lines)
	}

	var meta spoolEvent
	if err := json.Unmarshal([]byte(lines[0]), &meta); err != nil {
		t.Fatalf("meta unmarshal: %v", err)
	}
	if meta.Type != "meta" || meta.Backend != "codex" || meta.Task != "do the thing" {
		t.Errorf("bad meta: %+v", meta)
	}
	if meta.PID != os.Getpid() {
		t.Errorf("meta PID = %d, want %d", meta.PID, os.Getpid())
	}

	wantTypes := []string{"meta", "reasoning", "command", "message", "done"}
	for i, want := range wantTypes {
		var ev spoolEvent
		if err := json.Unmarshal([]byte(lines[i]), &ev); err != nil {
			t.Fatalf("line %d unmarshal: %v", i, err)
		}
		if ev.Type != want {
			t.Errorf("line %d type = %q, want %q", i, ev.Type, want)
		}
	}
}

func TestSpoolWriterNilSafe(t *testing.T) {
	var sw *SpoolWriter
	// None of these should panic on a nil writer.
	sw.Content("x", "message")
	sw.Done()
	if err := sw.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
}

func TestSanitizeSessionID(t *testing.T) {
	cases := map[string]string{
		"codex-1-ab":    "codex-1-ab",
		"../../etc/pwd": "____etc_pwd",
		"a/b\\c":        "a_b_c",
		"  ":            "session",
		"":              "session",
	}
	for in, want := range cases {
		if got := sanitizeSessionID(in); got != want {
			t.Errorf("sanitizeSessionID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadSpoolSummary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAGENT_LIVE_DIR", dir)

	// Active session (no done record yet).
	active := filepath.Join(dir, "active.jsonl")
	os.WriteFile(active, []byte(
		`{"type":"meta","backend":"gemini","task":"live"}`+"\n"+
			`{"type":"message","content":"streaming"}`+"\n"), 0o644)
	meta, done := readSpoolSummary(active)
	if meta.Backend != "gemini" || meta.Task != "live" {
		t.Errorf("active meta = %+v", meta)
	}
	if done {
		t.Errorf("active session reported done")
	}

	// Finished session.
	finished := filepath.Join(dir, "finished.jsonl")
	os.WriteFile(finished, []byte(
		`{"type":"meta","backend":"codex","task":"x"}`+"\n"+
			`{"type":"done"}`+"\n"), 0o644)
	if _, done := readSpoolSummary(finished); !done {
		t.Errorf("finished session not reported done")
	}
}

func TestCleanupOldLiveFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAGENT_LIVE_DIR", dir)

	// Owned by this (alive) process — must be kept regardless of age.
	alive := filepath.Join(dir, "alive.jsonl")
	writeMeta(t, alive, os.Getpid())
	makeOld(t, alive)

	// Dead PID + old mtime — should be removed.
	deadOld := filepath.Join(dir, "dead-old.jsonl")
	writeMeta(t, deadOld, 2147483646)
	makeOld(t, deadOld)

	// Dead PID but fresh — kept for replay grace period.
	deadFresh := filepath.Join(dir, "dead-fresh.jsonl")
	writeMeta(t, deadFresh, 2147483646)

	cleanupOldLiveFiles(30 * time.Minute)

	if _, err := os.Stat(alive); err != nil {
		t.Errorf("alive-owned file was removed: %v", err)
	}
	if _, err := os.Stat(deadOld); !os.IsNotExist(err) {
		t.Errorf("dead+old file should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(deadFresh); err != nil {
		t.Errorf("dead+fresh file should be kept for grace: %v", err)
	}
}

func writeMeta(t *testing.T, path string, pid int) {
	t.Helper()
	ev := spoolEvent{Type: "meta", Backend: "codex", Task: "t", PID: pid}
	data, _ := json.Marshal(ev)
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("writeMeta: %v", err)
	}
}

func makeOld(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}
