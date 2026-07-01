//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachCmd puts the child in its own session so it survives this wrapper's
// exit and is not hit by signals sent to the wrapper's process group.
func detachCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
