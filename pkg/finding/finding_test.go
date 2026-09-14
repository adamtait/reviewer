// SPDX-License-Identifier: MIT

package finding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func valid() Finding {
	return Finding{
		Fingerprint: "a3f9c2d1e4b7",
		RuleID:      "arch/no-domain-to-infra",
		Lane:        LaneDeterministic,
		Confidence:  ConfidenceHigh,
		Severity:    SeverityError,
		File:        "src/domain/order.ts",
		Line:        3,
		Message:     "domain/ must not import infra/",
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Finding)
		want string // substring of the expected error; "" means valid
	}{
		{"valid", func(*Finding) {}, ""},
		{"no rule id", func(f *Finding) { f.RuleID = "" }, "ruleId is required"},
		{"no file", func(f *Finding) { f.File = "" }, "file is required"},
		{"absolute file", func(f *Finding) { f.File = "/tmp/x.ts" }, "must be relative"},
		{"no message", func(f *Finding) { f.Message = "" }, "message is required"},
		{"line zero", func(f *Finding) { f.Line = 0 }, "line is 0"},
		{"endLine before line", func(f *Finding) { f.Line, f.EndLine = 10, 4 }, "endLine 4 is before line 10"},
		{"unknown lane", func(f *Finding) { f.Lane = "guesswork" }, `lane is "guesswork"`},
		{"unknown confidence", func(f *Finding) { f.Confidence = "certain" }, `confidence is "certain"`},
		{"unknown severity", func(f *Finding) { f.Severity = "fatal" }, `severity is "fatal"`},
		{
			"llm without evidence",
			func(f *Finding) { f.Lane, f.Evidence = LaneLLM, "" },
			"evidence is required",
		},
		{
			"llm with blank evidence",
			func(f *Finding) { f.Lane, f.Evidence = LaneLLM, "   \n" },
			"evidence is required",
		},
		{
			"llm with evidence",
			func(f *Finding) { f.Lane, f.Evidence = LaneLLM, "four call sites repeat the wrapper" },
			"",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := valid()
			tc.mut(&f)
			err := f.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	err := Finding{}.Validate()
	if err == nil {
		t.Fatal("want errors for an empty finding")
	}
	// One message per plugin round-trip is worth more than the first failure.
	if n := strings.Count(err.Error(), "\n") + 1; n < 6 {
		t.Fatalf("want every problem reported, got %d:\n%v", n, err)
	}
}

func TestSpan(t *testing.T) {
	f := valid()
	if s, e := f.Span(); s != 3 || e != 3 {
		t.Fatalf("want a single-line span 3-3, got %d-%d", s, e)
	}
	f.EndLine = 7
	if s, e := f.Span(); s != 3 || e != 7 {
		t.Fatalf("want 3-7, got %d-%d", s, e)
	}
}

func TestSortIsStableAndTotal(t *testing.T) {
	fs := []Finding{
		{File: "b.ts", Line: 1, RuleID: "z"},
		{File: "a.ts", Line: 9, RuleID: "a"},
		{File: "a.ts", Line: 2, RuleID: "b"},
		{File: "a.ts", Line: 2, RuleID: "a"},
	}
	Sort(fs)
	got := make([]string, len(fs))
	for i, f := range fs {
		got[i] = f.File + ":" + f.RuleID
	}
	want := []string{"a.ts:a", "a.ts:b", "a.ts:a", "b.ts:z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestGoldenRoundTrip(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "golden", "findings.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fs []Finding
	if err := json.Unmarshal(raw, &fs); err != nil {
		t.Fatalf("golden file does not unmarshal into Finding: %v", err)
	}
	for _, f := range fs {
		if err := f.Validate(); err != nil {
			t.Fatalf("golden finding %s is invalid: %v", f.RuleID, err)
		}
	}
	out, err := json.MarshalIndent(fs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(out)), strings.TrimSpace(string(raw)); got != want {
		if os.Getenv("UPDATE_GOLDEN") != "" {
			if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Skip("golden file updated")
		}
		t.Fatalf("round-trip differs from the golden file; rerun with UPDATE_GOLDEN=1 if intended\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestSchemaMatchesTheStruct is the drift guard. The schema is authored by hand
// rather than generated, so nothing but this test stops a new Go field from being
// invisible to plugin authors, or a schema property from having no Go home.
func TestSchemaMatchesTheStruct(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schema", "finding.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	var structFields, requiredFields []string
	rt := reflect.TypeOf(Finding{})
	for i := range rt.NumField() {
		tag := rt.Field(i).Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		structFields = append(structFields, name)
		if !strings.Contains(opts, "omitempty") {
			requiredFields = append(requiredFields, name)
		}
	}

	var schemaFields []string
	for name := range doc.Properties {
		schemaFields = append(schemaFields, name)
	}
	sort.Strings(structFields)
	sort.Strings(schemaFields)
	if !reflect.DeepEqual(structFields, schemaFields) {
		t.Fatalf("schema properties and struct fields disagree:\n  struct: %v\n  schema: %v", structFields, schemaFields)
	}

	sort.Strings(requiredFields)
	schemaRequired := append([]string(nil), doc.Required...)
	sort.Strings(schemaRequired)
	if !reflect.DeepEqual(requiredFields, schemaRequired) {
		t.Fatalf("schema `required` and the struct's non-omitempty fields disagree:\n  struct: %v\n  schema: %v", requiredFields, schemaRequired)
	}
}

// A rule id that cannot survive the comment marker is refused here, at the only
// point where every finding passes through. The alternative is a comment reposted
// on every push whose thread is never resolved, discovered weeks later.
func TestARuleIDThatCannotSurviveTheMarkerIsRejected(t *testing.T) {
	for _, ruleID := range []string{
		"conventions/no console",
		"arch/no-domain->infra",
		"has\ttab",
		"has\nnewline",
	} {
		f := Finding{
			RuleID: ruleID, Lane: LaneDeterministic,
			Confidence: ConfidenceHigh, Severity: SeverityError,
			File: "a.ts", Line: 1, Message: "m",
		}
		err := f.Validate()
		if err == nil {
			t.Errorf("ruleId %q was accepted", ruleID)
			continue
		}
		if !strings.Contains(err.Error(), "comment marker") {
			t.Errorf("ruleId %q: want the reason stated, got %v", ruleID, err)
		}
	}

	// And the ordinary ones are still fine.
	for _, ruleID := range []string{"conventions/no-console", "types/TS2322", "deps/npm"} {
		f := Finding{
			RuleID: ruleID, Lane: LaneDeterministic,
			Confidence: ConfidenceHigh, Severity: SeverityError,
			File: "a.ts", Line: 1, Message: "m",
		}
		if err := f.Validate(); err != nil {
			t.Errorf("ruleId %q was rejected: %v", ruleID, err)
		}
	}
}
