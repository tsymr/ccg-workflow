package main

import (
	"net"
	"testing"
)

func TestViewerHostPortDefaults(t *testing.T) {
	t.Setenv("CODEAGENT_WEB_HOST", "")
	t.Setenv("CODEAGENT_WEB_PORT", "")
	if h := viewerHost(); h != "127.0.0.1" {
		t.Errorf("default host = %q, want 127.0.0.1", h)
	}
	if p := viewerPort(); p != defaultViewerPort {
		t.Errorf("default port = %d, want %d", p, defaultViewerPort)
	}

	t.Setenv("CODEAGENT_WEB_HOST", "0.0.0.0")
	t.Setenv("CODEAGENT_WEB_PORT", "9123")
	if h := viewerHost(); h != "0.0.0.0" {
		t.Errorf("host = %q, want 0.0.0.0", h)
	}
	if p := viewerPort(); p != 9123 {
		t.Errorf("port = %d, want 9123", p)
	}

	// Invalid port falls back to default.
	t.Setenv("CODEAGENT_WEB_PORT", "notanumber")
	if p := viewerPort(); p != defaultViewerPort {
		t.Errorf("invalid port = %d, want default %d", p, defaultViewerPort)
	}
}

func TestAutoViewEnabled(t *testing.T) {
	on := []string{"1", "true", "TRUE", "yes", "Yes"}
	off := []string{"", "0", "false", "no", "off"}
	for _, v := range on {
		t.Setenv("CODEAGENT_AUTO_VIEW", v)
		if !autoViewEnabled() {
			t.Errorf("autoViewEnabled(%q) = false, want true", v)
		}
	}
	for _, v := range off {
		t.Setenv("CODEAGENT_AUTO_VIEW", v)
		if autoViewEnabled() {
			t.Errorf("autoViewEnabled(%q) = true, want false", v)
		}
	}
}

func TestViewerReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if !viewerReachable("127.0.0.1", port) {
		t.Errorf("reachable on open port = false, want true")
	}
	_ = ln.Close()
	if viewerReachable("127.0.0.1", port) {
		t.Errorf("reachable on closed port = true, want false")
	}
}

func TestRunViewerQuietBindFailure(t *testing.T) {
	// Occupy the port so the viewer cannot bind.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	// Quiet: the loser of a port race must exit 0 without blocking.
	t.Setenv("CODEAGENT_VIEW_QUIET", "1")
	if rc := runViewer("127.0.0.1", port, false); rc != 0 {
		t.Errorf("quiet bind failure rc = %d, want 0", rc)
	}

	// Non-quiet: a real bind failure is an error.
	t.Setenv("CODEAGENT_VIEW_QUIET", "")
	if rc := runViewer("127.0.0.1", port, false); rc != 1 {
		t.Errorf("bind failure rc = %d, want 1", rc)
	}
}
