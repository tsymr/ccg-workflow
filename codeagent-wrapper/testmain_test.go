package main

import (
	"os"
	"testing"
)

// TestMain isolates the live-output spool directory to a throwaway temp dir for
// the entire test binary. Many tests drive runCodexTaskWithContext with mock
// backends, which spool live output; without this they would litter the real
// ~/.claude/.ccg/live directory. Individual tests may still override
// CODEAGENT_LIVE_DIR via t.Setenv.
func TestMain(m *testing.M) {
	// Never auto-spawn a real background viewer from the test suite.
	_ = os.Unsetenv("CODEAGENT_AUTO_VIEW")
	dir, err := os.MkdirTemp("", "ccg-live-test-")
	if err == nil {
		_ = os.Setenv("CODEAGENT_LIVE_DIR", dir)
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
