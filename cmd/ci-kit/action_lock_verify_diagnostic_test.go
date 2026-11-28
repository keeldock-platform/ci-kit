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
	raw := []byte(`{"cli_version":"0.5.1","lockfile_version":"1","valid":true,"findings":[{"workflow":"PRIVATE_PATH_SENTINEL","category":"container-image","severity":"warning","detail":"PRIVATE_DETAIL_SENTINEL","doc_url":"PRIVATE_URL_SENTINEL"},{"workflow":"PRIVATE_PATH_SENTINEL","category":"ref-moved","severity":"warning","retryable":true,"dependency":"PRIVATE_DEP_SENTINEL"}]}`)
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

// The pinned v0.5.1 format.WriteJSON contract emits the string versions and
// the valid/findings fields selected by --json=valid,findings.
func TestParseVerifyFindingsMatchesPinnedV051WriterEnvelope(t *testing.T) {
	raw := []byte(`{"cli_version":"0.5.1","lockfile_version":"1","valid":true,"findings":[]}`)
	if _, ok, reason := parseVerifyFindingsReason(raw); !ok || reason != "" {
		t.Fatalf("pinned writer envelope rejected as %s", reason)
	}
}

func TestParseVerifyFindingsReportsOnlyFixedRejectionReason(t *testing.T) {
	tooDeep := []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[],"x":` + strings.Repeat("[", 34) + strings.Repeat("]", 34) + `}`)
	for _, c := range []struct{name string; raw []byte; want string}{
		{"empty", nil, "empty_or_oversized"},
		{"invalid-json", []byte(`{`), "malformed_json"},
		{"extra-top-level-field", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[],"extra":"private"}`), "envelope_shape"},
		{"numeric-version", []byte(`{"cli_version":1,"lockfile_version":"1","valid":true,"findings":[]}`), "version_field"},
		{"missing-valid", []byte(`{"cli_version":"v","lockfile_version":"1","findings":[],"extra":true}`), "valid_field"},
		{"wrong-valid-type", []byte(`{"cli_version":"v","lockfile_version":"1","valid":"true","findings":[]}`), "valid_field"},
		{"missing-findings", []byte(`{"cli_version":"v","lockfile_version":"1"}`), "envelope_shape"},
		{"wrong-findings-type", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":{}}`), "findings_field"},
		{"malformed-finding", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[1]}`), "findings_shape"},
		{"null-finding", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[null]}`), "findings_shape"},
		{"missing-finding-severity", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"sha-as-ref","dependency":"owner/repo@0123456789abcdef0123456789abcdef01234567"}]}`), "findings_shape"},
		{"wrong-finding-severity-type", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"sha-as-ref","severity":true,"dependency":"owner/repo@0123456789abcdef0123456789abcdef01234567"}]}`), "findings_shape"},
		{"null-valid", []byte(`{"cli_version":"v","lockfile_version":"1","valid":null,"findings":[]}`), "valid_field"},
		{"null-version", []byte(`{"cli_version":null,"lockfile_version":"1","valid":true,"findings":[]}`), "version_field"},
		{"case-alias-envelope", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"Valid":false,"findings":[]}`), "policy_key_alias"},
		{"conflicting-severity-alias", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"sha-as-ref","severity":"error","Severity":"info","dependency":"owner/repo@0123456789abcdef0123456789abcdef01234567"}]}`), "policy_key_alias"},
		{"conflicting-category-alias", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"stale","Category":"sha-as-ref","severity":"warning","dependency":"owner/repo@0123456789abcdef0123456789abcdef01234567"}]}`), "policy_key_alias"},
		{"conflicting-dependency-alias", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"sha-as-ref","severity":"warning","dependency":"owner/repo@0123456789abcdef0123456789abcdef01234567","Dependency":"owner/repo@0123456789abcdef0123456789abcdef01234567"}]}`), "policy_key_alias"},
		{"duplicate-valid", []byte(`{"cli_version":"v","lockfile_version":"1","valid":false,"valid":true,"findings":[]}`), "malformed_json"},
		{"invalid-utf8", append([]byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[],"x":"`), append([]byte{0xff}, []byte(`"}`)... )...), "malformed_json"},
		{"wrong-category-type", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":1,"severity":"warning","dependency":"owner/repo@0123456789abcdef0123456789abcdef01234567"}]}`), "findings_shape"},
		{"wrong-dependency-type", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"sha-as-ref","severity":"warning","dependency":1}]}`), "findings_shape"},
		{"duplicate-finding-severity", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"sha-as-ref","severity":"error","severity":"info","dependency":"owner/repo@0123456789abcdef0123456789abcdef01234567"}]}`), "malformed_json"},
		{"excessive-depth", tooDeep, "malformed_json"},
	} { t.Run(c.name, func(t *testing.T) {
		_, ok, got := parseVerifyFindingsReason(c.raw)
		if ok || got != c.want { t.Fatalf("fixed reason mismatch: got %s", got) }
		d := verifyDiagnostic{result:"json_invalid", parseRejection:got}
		var out bytes.Buffer
		if err := writeVerifyDiagnostic(&out, d); err != nil { t.Fatal("diagnostic rejected") }
		if strings.Contains(out.String(), "private") || !strings.Contains(out.String(), "parse_rejection="+c.want) { t.Fatal("unsafe or incomplete diagnostic") }
	}) }
}

func TestKWorkflowLockCompatibilityRequiresExactAllowedFindings(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	base := `{"cli_version":"0.5.1","lockfile_version":"1","valid":true,"findings":[{"category":"sha-as-ref","severity":"warning","dependency":"owner/repo@` + sha + `"}]}`
	for _, c := range []struct{name, raw string; compatible bool}{
		{"full-lowercase-sha", base, true},
		{"info-is-allowed", strings.Replace(base, `}]}`, `},{"category":"future-info","severity":"info"}]}`, 1), true},
		{"info-severity-controls-policy", strings.Replace(base, `}]}`, `},{"category":null,"severity":"info"}]}`, 1), true},
		{"no-sha-warning", strings.Replace(base, `[{"category":"sha-as-ref","severity":"warning","dependency":"owner/repo@`+sha+`"}]`, `[]`, 1), false},
		{"valid-false", strings.Replace(base, `"valid":true`, `"valid":false`, 1), false},
		{"mutable-ref", strings.Replace(base, sha, "main", 1), false},
		{"partial-sha", strings.Replace(base, sha, sha[:12], 1), false},
		{"uppercase-sha", strings.Replace(base, sha, strings.ToUpper(sha), 1), false},
		{"tag-ref", strings.Replace(base, sha, "v1.2.3", 1), false},
		{"wrong-repository-shape", strings.Replace(base, "owner/repo@", "owner@", 1), false},
		{"unknown-warning", strings.Replace(base, `"category":"sha-as-ref"`, `"category":"private-category"`, 1), false},
		{"error", strings.Replace(base, `"severity":"warning"`, `"severity":"error"`, 1), false},
		{"mixed-warning", strings.Replace(base, `}]}`, `},{"category":"stale","severity":"warning"}]}`, 1), false},
		{"mixed-error", strings.Replace(base, `}]}`, `},{"category":"stale","severity":"error"}]}`, 1), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			counts, ok, _ := parseVerifyFindingsReason([]byte(c.raw))
			if !ok { t.Fatal("well-formed policy fixture rejected by parser") }
			if got := kWorkflowLockCompatibility(counts, true); got != c.compatible { t.Fatalf("compatibility = %v, want %v", got, c.compatible) }
		})
	}
}

func TestKWorkflowLockCompatibilityRequiresObservedCleanExitAndIndependentRefs(t *testing.T) {
	raw := []byte(`{"cli_version":"0.5.1","lockfile_version":"1","valid":true,"findings":[{"category":"sha-as-ref","severity":"warning","dependency":"owner/repo@0123456789abcdef0123456789abcdef01234567"}]}`)
	counts, ok, _ := parseVerifyFindingsReason(raw)
	if !ok { t.Fatal("positive policy fixture rejected") }
	if !immutableActionPolicyPassed([]byte("PASS immutable_actions\n")) || immutableActionPolicyPassed([]byte("FAIL immutable_actions\n")) || immutableActionPolicyPassed([]byte("PASS immutable_actions\nextra")) {
		t.Fatal("independent immutable-ref result must match the exact fixed pass")
	}
	clean := process.Result{ExitCode:1, ExitObserved:true, CaptureComplete:true, CleanupComplete:true}
	for _, c := range []struct{name string; result process.Result; err error; immutable bool; want bool}{
		{"observed-clean-exit-one-and-independent-pass", clean, process.ErrCapture, true, true},
		{"unobserved-exit", process.Result{ExitCode:1, CaptureComplete:true, CleanupComplete:true}, process.ErrCapture, true, false},
		{"unexpected-error", clean, errors.New("private process failure"), true, false},
		{"timeout", process.Result{ExitCode:1, ExitObserved:true, TimedOut:true, CaptureComplete:true, CleanupComplete:true}, process.ErrCapture, true, false},
		{"interrupted", process.Result{ExitCode:1, ExitObserved:true, Interrupted:true, CaptureComplete:true, CleanupComplete:true}, process.ErrCapture, true, false},
		{"overflow", process.Result{ExitCode:1, ExitObserved:true, Overflow:true, CaptureComplete:true, CleanupComplete:true}, process.ErrOverflow, true, false},
		{"signal-terminated", process.Result{ExitCode:1, ExitObserved:true, Signal:"TERM", CaptureComplete:true, CleanupComplete:true}, process.ErrCapture, true, false},
		{"incomplete-cleanup", process.Result{ExitCode:1, ExitObserved:true, CaptureComplete:true}, process.ErrCleanup, true, false},
		{"incomplete-capture", process.Result{ExitCode:1, ExitObserved:true, CleanupComplete:true}, process.ErrCapture, true, false},
		{"immutable-policy-failed", clean, process.ErrCapture, false, false},
		{"immutable-checker-process-error", clean, process.ErrCapture, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := verifyFailureDiagnostic(c.result, c.err)
			d.counts = counts
			immutableOutput, immutableErr := []byte("PASS immutable_actions\n"), error(nil)
			if c.name == "immutable-policy-failed" { immutableOutput, immutableErr = []byte("FAIL immutable_actions\n"), nil }
			if c.name == "immutable-checker-process-error" { immutableOutput, immutableErr = []byte("PASS immutable_actions\n"), errors.New("private checker failure") }
			got := admitKWorkflowLockCompatibility(d, immutableOutput, immutableErr)
			if got != c.want { t.Fatalf("compatibility admission = %v, want %v", got, c.want) }
		})
	}
	readFailure := parseVerifyFindingsOutput(verifyFailureDiagnostic(clean, process.ErrCapture), nil, errors.New("private read failure"))
	if admitKWorkflowLockCompatibility(readFailure, []byte("PASS immutable_actions\n"), nil) { t.Fatal("capture read failure admitted") }
	parsed := parseVerifyFindingsOutput(verifyFailureDiagnostic(clean, process.ErrCapture), raw, nil)
	if !admitKWorkflowLockCompatibility(parsed, []byte("PASS immutable_actions\n"), nil) { t.Fatal("production parser and admission predicate rejected canonical success") }
	for _, content := range [][]byte{[]byte("FAIL immutable_actions\n"), []byte("PASS immutable_actions\nextra")} {
		if admitKWorkflowLockCompatibility(parsed, content, nil) { t.Fatal("noncanonical checker content admitted") }
	}
}

func TestParseVerifyFindingsUnknownAndInvalidCases(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  []byte
		valid bool
		unknown int
	}{
		{"unknown-category", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"private-category","severity":"warning"}]}`), true, 1},
		{"unknown-severity", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"stale","severity":"private-severity"}]}`), true, 1},
		{"missing-field-selection", []byte(`{"cli_version":"v","lockfile_version":"1"}`), false, 0},
		{"unknown-envelope-schema", []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[],"unexpected":"private"}`), false, 0},
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
	raw := []byte(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[{"category":"private-category-sentinel","severity":"warning","detail":"private-detail-sentinel"}]}`)
 counts, valid := parseVerifyFindings(raw)
 if !valid || counts.unknown != 1 { t.Fatal("unknown finding did not fail closed") }
 var output bytes.Buffer
 if err := writeVerifyDiagnostic(&output, verifyDiagnostic{result:"unknown_category", counts:counts}); err != nil { t.Fatal("fixed unknown diagnostic rejected") }
 if got, want := output.String(), "VERIFY_DIAGNOSTIC result=unknown_category\nVERIFY_FINDING category=unknown severity=unknown count=1\n"; got != want { t.Fatalf("unknown output mismatch: %q", got) }
}

func TestParseVerifyFindingsBounds(t *testing.T) {
	tooMany := []byte(fmt.Sprintf(`{"cli_version":"v","lockfile_version":"1","valid":true,"findings":[%s]}`, strings.TrimSuffix(strings.Repeat(`{"category":"stale","severity":"error"},`, maxVerifyFindings+1), ",")))
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
		{"tool-exit-one", process.Result{ExitCode: 1, ExitObserved: true, CaptureComplete: true, CleanupComplete: true}, process.ErrCapture, "tool_exit_1_findings", ""},
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
