// SPDX-License-Identifier: MIT

package reporters

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/pkg/finding"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// sample is the one input both reporters render, so the golden files can be read
// side by side to compare what each format keeps and what it drops.
func sample() Run {
	return Run{
		Root: "/repo",
		Base: "main",
		Findings: []finding.Finding{
			{
				Fingerprint: "a3f9c2d1e4b7", RuleID: "arch/no-domain-to-infra",
				Lane: finding.LaneDeterministic, Confidence: finding.ConfidenceHigh,
				Severity: finding.SeverityError, File: "src/domain/order.ts", Line: 3,
				Message: "domain/ must not import infra/ — inject a port instead",
			},
			{
				Fingerprint: "c7d0a3f6b9e2", RuleID: "llm/missing-abstraction",
				Lane: finding.LaneLLM, Confidence: finding.ConfidenceMedium,
				Severity: finding.SeverityInfo, File: "src/domain/order.ts", Line: 42,
				Message:  "Four call sites repeat the same retry-and-log wrapper",
				Evidence: "order.ts:42, order.ts:61, invoice.ts:18 and invoice.ts:44 wrap a call in the same try/catch with identical backoff constants.",
			},
			{
				Fingerprint: "b1c4e7a0d3f6", RuleID: "logic/no-floating-promises",
				Lane: finding.LaneDeterministic, Confidence: finding.ConfidenceHigh,
				Severity: finding.SeverityError, File: "src/infra/http.ts", Line: 9, EndLine: 11,
				Message:    "Promise returned from send() is not awaited",
				Suggestion: "  await send(request)",
			},
		},
		Warnings: []string{"plugin typescript: knip timed out after 2m0s"},
		Skipped:  []string{"eslint: no flat config found"},
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "golden", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("golden file updated")
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (rerun with -update to create it)", err)
	}
	if got != string(want) {
		t.Fatalf("output differs from %s; rerun with -update if intended\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestTextGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := (Text{Out: &buf}).Report(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}
	golden(t, "text.txt", buf.String())
}

func TestRDJSONGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := (RDJSON{Out: &buf}).Report(context.Background(), sample()); err != nil {
		t.Fatal(err)
	}
	golden(t, "rdjson.json", buf.String())
}

func TestRDJSONIsValidJSONWithOneDiagnosticPerFinding(t *testing.T) {
	var buf bytes.Buffer
	run := sample()
	if err := (RDJSON{Out: &buf}).Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	var doc rdDiagnosticResult
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(doc.Diagnostics) != len(run.Findings) {
		t.Fatalf("want %d diagnostics, got %d", len(run.Findings), len(doc.Diagnostics))
	}
	if doc.Source.Name != "reviewer" {
		t.Fatalf("want the source named, got %q", doc.Source.Name)
	}
	// A multi-line finding must keep its span; collapsing it would misplace a
	// suggested change.
	last := doc.Diagnostics[2]
	if last.Location.Range.Start.Line != 9 || last.Location.Range.End.Line != 11 {
		t.Fatalf("want the 9-11 span preserved, got %+v", last.Location.Range)
	}
	if len(last.Suggestions) != 1 {
		t.Fatalf("want the suggestion carried through, got %+v", last.Suggestions)
	}
}

func TestTextReportsAnEmptyRunPlainly(t *testing.T) {
	var buf bytes.Buffer
	if err := (Text{Out: &buf}).Report(context.Background(), Run{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "reviewer: no findings" {
		t.Fatalf("want a plain empty-run line, got %q", got)
	}
}

// A run where analyzers failed is not a clean run, and must not read like one.
func TestTextShowsWarningsBeforeFindings(t *testing.T) {
	var buf bytes.Buffer
	run := Run{Warnings: []string{"plugin typescript: did not start"}}
	if err := (Text{Out: &buf}).Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "warning: plugin typescript: did not start") {
		t.Fatalf("want the warning first, got %q", out)
	}
	if !strings.Contains(out, "no findings") {
		t.Fatalf("want the empty-run line as well, got %q", out)
	}
}

func TestReporterNames(t *testing.T) {
	for _, r := range []Reporter{Text{}, RDJSON{}} {
		if r.Name() == "" {
			t.Fatalf("%T has no name", r)
		}
	}
}

// rdjson has nowhere in its schema for warnings, so they go to a side channel.
// Dropping them would make a run where every plugin failed serialise identically
// to a clean review.
func TestRDJSONSendsWarningsToTheLog(t *testing.T) {
	var out, log bytes.Buffer
	run := Run{Warnings: []string{"plugin typescript: did not start"}, Skipped: []string{"knip: no config"}}
	if err := (RDJSON{Out: &out, Log: &log}).Report(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "plugin typescript: did not start") {
		t.Fatalf("want the warning on the log channel, got %q", log.String())
	}
	if !strings.Contains(log.String(), "knip: no config") {
		t.Fatalf("want the skip on the log channel, got %q", log.String())
	}
	// The document itself stays schema-clean.
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := doc["warnings"]; ok {
		t.Fatal("warnings must not be smuggled into the reviewdog document")
	}
}

func TestRDJSONWithoutALogChannelStillWorks(t *testing.T) {
	var out bytes.Buffer
	if err := (RDJSON{Out: &out}).Report(context.Background(), Run{Warnings: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
}
