package main

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// viewerHost returns the viewer bind host (CODEAGENT_WEB_HOST, default loopback).
func viewerHost() string {
	if h := strings.TrimSpace(os.Getenv("CODEAGENT_WEB_HOST")); h != "" {
		return h
	}
	return "127.0.0.1"
}

// viewerPort returns the viewer port (CODEAGENT_WEB_PORT, default 8899).
func viewerPort() int {
	if p := strings.TrimSpace(os.Getenv("CODEAGENT_WEB_PORT")); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			return v
		}
	}
	return defaultViewerPort
}

// autoViewEnabled reports whether tasks should auto-start the live viewer.
// Opt-in via CODEAGENT_AUTO_VIEW=1 so no viewer is ever spawned unexpectedly.
func autoViewEnabled() bool {
	v := strings.TrimSpace(os.Getenv("CODEAGENT_AUTO_VIEW"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// viewerReachable reports whether a viewer already listens on host:port.
func viewerReachable(host string, port int) bool {
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

var ensureViewerOnce sync.Once

// maybeAutoStartViewer starts one background viewer if CODEAGENT_AUTO_VIEW is set
// and none is already listening. Runs at most once per wrapper process. The
// spawned viewer is detached (outlives this wrapper), never writes to our stderr,
// and never opens a browser — safe on headless servers. If several parallel
// wrappers race, only one wins the port; the losers exit silently (see runViewer).
func maybeAutoStartViewer() {
	if liteMode || !autoViewEnabled() {
		return
	}
	ensureViewerOnce.Do(func() {
		host := viewerHost()
		port := viewerPort()
		if viewerReachable(host, port) {
			return // a viewer is already up
		}
		self, err := os.Executable()
		if err != nil {
			return
		}
		cmd := exec.Command(self, "--view", "--host", host, "--port", strconv.Itoa(port))
		cmd.Env = append(os.Environ(), "CODEAGENT_VIEW_QUIET=1")
		cmd.Stdin = nil
		if devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
			cmd.Stdout = devnull
			cmd.Stderr = devnull
		}
		detachCmd(cmd)
		if err := cmd.Start(); err != nil {
			return
		}
		go func() { _ = cmd.Wait() }() // reap if it exits early (e.g. lost the port race)
	})
}
