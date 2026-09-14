// SPDX-License-Identifier: MIT

//go:build !unix

package pluginhost

import "os/exec"

// isolate is a no-op where process groups are not available. Releases target
// linux and darwin (ADR-0026); this exists so the package still builds elsewhere.
func isolate(*exec.Cmd) {}

// killGroup falls back to killing only the immediate child, which may leave
// grandchildren running.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
