//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const (
	detachedProcess       = 0x00000008 // DETACHED_PROCESS
	createNewProcessGroup = 0x00000200 // CREATE_NEW_PROCESS_GROUP
)

// detachCmd starts the child detached from this wrapper's console and process
// group so it survives the wrapper's exit.
func detachCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}
