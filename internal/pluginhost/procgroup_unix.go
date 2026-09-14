// SPDX-License-Identifier: MIT

//go:build unix

package pluginhost

import (
	"os/exec"
	"syscall"
)

// isolate puts the child in its own process group so that killing it kills
// anything it spawned. A plugin that shells out to `tsc` leaves an orphan
// otherwise, and an orphaned type-checker holds a CPU for minutes.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup kills the whole process group, not just the immediate child.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// The negative pid addresses the group; isolate made the child its leader.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
