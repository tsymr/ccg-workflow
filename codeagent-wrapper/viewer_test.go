package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpool(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("writeSpool: %v", err)
	}
}

func TestHandleViewerSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAGENT_LIVE_DIR", dir)

	writeSpool(t, dir, "codex-1.jsonl",
		`{"type":"meta","backend":"codex","task":"build"}`,
		`{"type":"message","content":"hi"}`)
	writeSpool(t, dir, "gemini-2.jsonl",
		`{"type":"meta","backend":"gemini","task":"review"}`,
		`{"type":"done"}`)
	// Non-spool files must be ignored.
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	handleViewerSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got []viewerSession
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d: %+v", len(got), got)
	}

	byID := map[string]viewerSession{}
	for _, s := range got {
		byID[s.ID] = s
	}
	if s := byID["codex-1"]; s.Backend != "codex" || s.Task != "build" || s.Done {
		t.Errorf("codex-1 = %+v", s)
	}
	if s := byID["gemini-2"]; s.Backend != "gemini" || !s.Done {
		t.Errorf("gemini-2 = %+v", s)
	}
}

func TestHandleViewerStreamReplaysAndCloses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAGENT_LIVE_DIR", dir)
	writeSpool(t, dir, "codex-1.jsonl",
		`{"type":"meta","backend":"codex","task":"build"}`,
		`{"type":"message","content":"hello"}`,
		`{"type":"done"}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stream/codex-1", nil)
	// Handler must return (not block) because the file already contains a done record.
	handleViewerStream(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{`"type":"meta"`, `"content":"hello"`, `"type":"done"`} {
		if !strings.Contains(body, want) {
			t.Errorf("stream body missing %q; got:\n%s", want, body)
		}
	}
	if strings.Count(body, "data: ") != 3 {
		t.Errorf("expected 3 SSE frames, got %d:\n%s", strings.Count(body, "data: "), body)
	}
}

func TestHandleViewerStreamNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAGENT_LIVE_DIR", dir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stream/missing", nil)
	handleViewerStream(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleViewerIndexServesHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handleViewerIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "CCG Live Viewer") {
		t.Errorf("index missing title")
	}

	// Unknown paths must 404 (the "/" handler is a catch-all).
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/nope", nil)
	handleViewerIndex(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("/nope status = %d, want 404", rec2.Code)
	}
}
