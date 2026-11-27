package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/process"
)

func TestParseVerifyFindingsKeepsOnlyAllowlistedCounts(t *testing.T) {
	raw := []byte(`{"cli_version":"0.5.1","lockfile_version":"1","findings":[{"workflow":"PRIVATE_PATH_SENTINEL","category":"container-image","severity":"warning","detail":"PRIVATE_DETAIL_SENTINEL","doc_url":"PRIVATE_URL_SENTINEL"},{"workflow":"PRIVATE_PATH_SENTINEL","category":"ref-moved","severity":"warning","retryable":true,"dependency":"PRIVATE_DEP_SENTINEL"}]}`)
	counts, ok := parseVerifyFindings(raw)
	if !ok || counts.unknown != 0 {
		t.Fatal("known finding schema rejected")
	}
	if got := counts.values["container-image"]["warning"]; got != 1 {
		t.Fatalf("container warning count = %d", got)
	}
	if got := counts.values["ref-moved"]["warning"]; got != 1 {
		t.Fatalf("ref warning count = %d", got)
	}
	d := verifyDiagnostic{result: "tool_exit_1_findings", counts: counts}
	var output bytes.Buffer
	if err := writeVerifyDiagnostic(&output, d); err != nil {
		t.Fatal("fixed diagnostic write failed")
	}
	for _, forbidden := range []string{"PRIVATE_PATH_SENTINEL", "PRIVATE_DETAIL_SENTINEL", "PRIVATE_URL_SENTINEL", "PRIVATE_DEP_SENTINEL", "cli_version", "lockfile_version"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("private JSON field escaped: %s", forbidden)
		}
	}
	if got, want := output.String(), "VERIFY_DIAGNOSTIC result=tool_exit_1_findings\nVERIFY_FINDING category=ref-moved severity=warning count=1\nVERIFY_FINDING category=container-image severity=warning count=1\n"; got != want {
		t.Fatalf("sanitized output mismatch: %q", got)
	}
}

func TestParseVerifyFindingsUnknownAndInvalidCases(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  []byte
		valid bool
		unknown int
	}{
		{"unknown-category", []byte(`{"cli_version":"v","lockfile_version":"1","findings":[{"category":"private-category","severity":"warning"}]}`), true, 1},
		{"unknown-severity", []byte(`{"cli_version":"v","lockfile_version":"1","findings":[{"category":"stale","severity":"private-severity"}]}`), true, 1},
		{"missing-field-selection", []byte(`{"cli_version":"v","lockfile_version":"1"}`), false, 0},
		{"unknown-envelope-schema", []byte(`{"cli_version":"v","lockfile_version":"1","findings":[],"unexpected":"private"}`), false, 0},
		{"bad-json", []byte(`PRIVATE_RAW_JSON_SENTINEL`), false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, valid := parseVerifyFindings(c.raw)
			if valid != c.valid || got.unknown != c.unknown {
				t.Fatalf("parse result valid=%v unknown=%d", valid, got.unknown)
			}
			if !valid {
				d := verifyDiagnostic{result: "json_invalid", counts: got}
				var out bytes.Buffer
				if err := writeVerifyDiagnostic(&out, d); err != nil { t.Fatal("fixed invalid result rejected") }
				if strings.Contains(out.String(), "PRIVATE") { t.Fatal("raw JSON escaped") }
			}
		})
	}
}

func TestUnknownVerifyCategoryUsesOnlyFixedUnknownCount(t *testing.T) {
 raw := []byte(`{"cli_version":"v","lockfile_version":"1","findings":[{"category":"private-category-sentinel","severity":"warning","detail":"private-detail-sentinel"}]}`)
 counts, valid := parseVerifyFindings(raw)
 if !valid || counts.unknown != 1 { t.Fatal("unknown finding did not fail closed") }
 var output bytes.Buffer
 if err := writeVerifyDiagnostic(&output, verifyDiagnostic{result:"unknown_category", counts:counts}); err != nil { t.Fatal("fixed unknown diagnostic rejected") }
 if got, want := output.String(), "VERIFY_DIAGNOSTIC result=unknown_category\nVERIFY_FINDING category=unknown severity=unknown count=1\n"; got != want { t.Fatalf("unknown output mismatch: %q", got) }
}

func TestParseVerifyFindingsBounds(t *testing.T) {
	tooMany := []byte(fmt.Sprintf(`{"cli_version":"v","lockfile_version":"1","findings":[%s]}`, strings.TrimSuffix(strings.Repeat(`{"category":"stale","severity":"error"},`, maxVerifyFindings+1), ",")))
	if _, ok := parseVerifyFindings(tooMany); ok {
		t.Fatal("over-bound finding array accepted")
	}
	if _, ok := parseVerifyFindings(make([]byte, (2<<20)+1)); ok {
		t.Fatal("over-bound JSON bytes accepted")
	}
}

func TestVerifyFailureClassificationPreservesFirstFailureAndCleanup(t *testing.T) {
	for _, c := range []struct {
		name string
		result process.Result
		err error
		want string
		condition string
	}{
		{"request-rejected", process.Result{ExitCode: -1, CleanupComplete: false}, process.ErrInvalid, "request_rejected", ""},
		{"capture-or-start-no-spawn-claim", process.Result{ExitCode: -1, Started: time.Now(), CleanupComplete: true}, process.ErrCapture, "capture_or_start_failure", ""},
		{"tool-exit-one", process.Result{ExitCode: 1, ExitObserved: true, CleanupComplete: true}, process.ErrCapture, "tool_exit_1_findings", ""},
		{"tool-exit-two", process.Result{ExitCode: 2, ExitObserved: true, CleanupComplete: true}, process.ErrCapture, "tool_exit_2", ""},
		{"unobserved-exit-one", process.Result{ExitCode: 1, CleanupComplete: true}, process.ErrCapture, "other_tool_exit", ""},
		{"timeout", process.Result{ExitCode: -1, TimedOut: true, CleanupComplete: true}, process.ErrCapture, "capture_or_start_failure", "timed_out"},
		{"overflow", process.Result{ExitCode: -1, Overflow: true, CleanupComplete: true}, process.ErrOverflow, "capture_or_start_failure", "capture_overflow"},
		{"cleanup-preserves-refusal", process.Result{ExitCode: 1, ExitObserved: true, CleanupComplete: false}, process.ErrCleanup, "tool_exit_1_findings", "cleanup_incomplete"},
		{"other-exit", process.Result{ExitCode: 7, CleanupComplete: true}, errors.New("private process error"), "other_tool_exit", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := verifyFailureDiagnostic(c.result, c.err)
			if got.result != c.want { t.Fatalf("classification = %q, want %q", got.result, c.want) }
			if c.condition != "" && !got.conditions[c.condition] { t.Fatalf("missing fixed condition %q", c.condition) }
			if c.name == "unobserved-exit-one" && verifyFindingsEligible(got) { t.Fatal("unobserved status reached JSON finding parser") }
			var out bytes.Buffer
			if err := writeVerifyDiagnostic(&out, got); err != nil { t.Fatal("fixed classification rejected") }
			if strings.Contains(out.String(), "private") { t.Fatal("error text escaped") }
			if c.name == "unobserved-exit-one" && strings.Contains(out.String(), "VERIFY_FINDING") { t.Fatal("unobserved exit emitted finding counts") }
		})
	}
}

func TestWriteVerifyDiagnosticRejectsUnboundedOrUnknownValues(t *testing.T) {
	for _, d := range []verifyDiagnostic{
		{result: "private-error"},
		{result: "tool_exit_1_findings", counts: verifyCounts{values: map[string]map[string]int{"private-category": {"warning": 1}}}},
		{result: "tool_exit_1_findings", counts: verifyCounts{values: map[string]map[string]int{"stale": {"warning": maxVerifyCount + 1}}}},
		{result: "tool_exit_1_findings", counts: verifyCounts{values: map[string]map[string]int{}, unknown: maxVerifyFindings + 1}},
	} {
		var out bytes.Buffer
		if err := writeVerifyDiagnostic(&out, d); err == nil {
			t.Fatal("unreviewed or unbounded diagnostic admitted")
		}
		if strings.Contains(out.String(), "private-error") || strings.Contains(out.String(), "private-category") {
			t.Fatal("unreviewed value escaped")
		}
	}
}
