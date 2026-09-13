// SPDX-License-Identifier: MIT

package testfixture

import (
	"os"
	"os/exec"
	"testing"
)

// RequireToolsVar names the environment variable that turns a skipped test into a
// failing one.
const RequireToolsVar = "REVIEWER_REQUIRE_TOOLS"

// RequireTool skips when a third-party binary is missing, unless the environment
// says the binary was supposed to be there.
//
// A test that skips is a test that did not run, and in CI the two are told apart
// only by reading the log. That is how the secrets-gate test — the one asserting
// that no diff reaches a model provider after a credential is found — sat in this
// repository having never once executed on a runner: CI installed no gitleaks, the
// test skipped, and the job was green.
//
// So CI sets REVIEWER_REQUIRE_TOOLS after installing the pinned binaries, and a
// missing one fails there. Locally it still skips, because a contributor who has
// not installed three third-party binaries should get a green suite and a note,
// not a wall of failures.
func RequireTool(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}
	if os.Getenv(RequireToolsVar) != "" {
		t.Fatalf("%s is not installed, but %s is set: CI installs it by pinned version "+
			"and checksum, so this means the install step did not run or did not work. "+
			"Skipping here would hide that.", name, RequireToolsVar)
	}
	t.Skipf("%s is not installed; run .github/scripts/install-tools.sh to run this test", name)
	return ""
}
