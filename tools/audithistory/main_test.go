// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

// TestEveryPatternCatchesItsCase is the test that makes "0 findings" mean
// something.
//
// A scanner whose patterns match nothing reports a clean history exactly as a
// clean history does. That failure has happened repeatedly in this project — an
// analyzer reporting correctly into a void — and an audit is the worst place for
// it, because the output is trusted precisely when nobody is looking closely.
func TestEveryPatternCatchesItsCase(t *testing.T) {
	cases := []struct {
		pattern string
		line    string
	}{
		{"internal-host", `API_BASE = "https://reviews.build.internal/api/v3"`},
		{"internal-host", `  host: metrics.corp:8080`},
		{"internal-host", `ssh deploy@runner-07.intranet`},
		{"internal-host", `PRINTER = "http://label-01.local:631/jobs"`},
		{"private-address", `proxy = 10.4.19.22:8443`},
		{"private-address", `  - 192.168.1.10:5432`},
		{"home-directory", `cache: /Users/dana/.config/reviewer`},
		{"home-directory", `RUN cp /home/buildbot/creds .`},
		{"internal-tooling", `see acme-eng.slack.com for context`},
		{"internal-tooling", `tracked at wombat.atlassian.net`},
		{"placeholder-scope", `npm i @team/review`},
		{"copyleft-header", `// SPDX-License-Identifier: GPL-3.0-only`},
		{"copyleft-header", `		GNU GENERAL PUBLIC LICENSE`},
	}

	for _, tc := range cases {
		t.Run(tc.pattern+": "+tc.line, func(t *testing.T) {
			got := scan(blob{hash: "0000000000", path: "example.txt"}, tc.line)
			if len(got) == 0 {
				t.Fatalf("no pattern matched %q; %s is not doing anything", tc.line, tc.pattern)
			}
			var names []string
			for _, f := range got {
				names = append(names, f.pattern)
			}
			if !contains(names, tc.pattern) {
				t.Errorf("matched %v, want %s to match", names, tc.pattern)
			}
		})
	}
}

// TestOrdinaryCodeIsNotAFinding is the other half. The first draft of the
// internal-host pattern reported 45 findings across this repository and every one
// was a filename ending in `.test.ts` or a call to `RegExp.test`. An audit that
// cries wolf is switched off, and then it is not an audit.
func TestOrdinaryCodeIsNotAFinding(t *testing.T) {
	for _, line := range []string{
		`write(root, "src/order.test.ts", body);`,
		`const sources = req.changed.filter((c) => SOURCE.test(c.path));`,
		`import { thing } from "./analyzers/knip.test.js";`,
		`if (config.local) { return config.local; }`,
		`// the version is 1.2.3, released 10.4.2025`,
		`const scoped = "@adamtait/reviewer-plugin-typescript";`,
		`// SPDX-License-Identifier: MIT`,
		`func TestLocal(t *testing.T) {`,
	} {
		t.Run(line, func(t *testing.T) {
			if got := scan(blob{hash: "0000000000", path: "example.ts"}, line); len(got) > 0 {
				t.Errorf("%s reported ordinary code: %q", got[0].pattern, line)
			}
		})
	}
}

// A compiled binary embeds the build machine's paths, so it matches the
// home-directory pattern constantly — and none of it was written by anyone.
func TestBinaryContentIsSkipped(t *testing.T) {
	body := "ELF\x00\x00/home/builder/go/src/thing.go\x00"
	if got := scan(blob{hash: "0000000000", path: "bin/tool"}, body); len(got) > 0 {
		t.Errorf("binary content produced %d findings; it should be skipped", len(got))
	}
}

// The skip list is where an audit quietly stops looking, so it is worth a test
// saying exactly how short it is.
func TestSkipListIsNarrow(t *testing.T) {
	for _, path := range []string{
		"internal/config/config.go",
		"docs/architecture.md",
		"README.md",
		".github/workflows/release.yml",
		"examples/config.minimal.yaml",
	} {
		if skipPath(path) {
			t.Errorf("%s is skipped by the audit, and should not be", path)
		}
	}
	for _, path := range []string{
		"plugins/typescript/node_modules/knip/package.json",
		"tools/audithistory/main.go",
		// This file. Its fixture table below is a worked example of every pattern
		// the audit refuses, so an audit that reads it reports fourteen findings
		// against itself — which is how the audit job failed on the pull request
		// that added it.
		"tools/audithistory/main_test.go",
	} {
		if !skipPath(path) {
			t.Errorf("%s should be skipped", path)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.EqualFold(s, needle) {
			return true
		}
	}
	return false
}
