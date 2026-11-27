package process_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

func TestCaptureCompleteIsInMemoryOnlyAndLegacyResultWireDecodes(t *testing.T) {
	raw, err := json.Marshal(process.CommandDiagnostic{
		Argv: []string{"/fixed/tool"}, Root: "/private/root", Cwd: ".",
		Result: process.Result{ExitCode: 1, ExitObserved: true, CaptureComplete: true, Signal: "", StdoutPath: "/private/stdout", StderrPath: "/private/stderr", CleanupComplete: true},
		CaptureErrors: []string{},
	})
	if err != nil {
		t.Fatal("diagnostic marshal failed")
	}
	var outer map[string]json.RawMessage
	if json.Unmarshal(raw, &outer) != nil {
		t.Fatal("diagnostic JSON malformed")
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(outer["result"], &result) != nil {
		t.Fatal("result JSON malformed")
	}
	want := []string{"ExitCode", "Interrupted", "ExitObserved", "Signal", "TimedOut", "Overflow", "StdoutPath", "StderrPath", "Started", "Ended", "CleanupComplete"}
	if len(result) != len(want) {
		t.Fatal("Result wire key set changed")
	}
	for _, key := range want {
		if _, ok := result[key]; !ok {
			t.Fatal("legacy Result key missing")
		}
	}
	if _, ok := result["CaptureComplete"]; ok {
		t.Fatal("in-memory capture flag escaped onto wire")
	}
	type legacyResult struct {
		ExitCode                  int
		Interrupted, ExitObserved bool
		Signal                    string
		TimedOut, Overflow        bool
		StdoutPath, StderrPath    string
		Started, Ended            time.Time
		CleanupComplete           bool
	}
	type legacyCommandDiagnostic struct {
		Argv          []string     `json:"argv"`
		Root          string       `json:"root"`
		Cwd           string       `json:"cwd"`
		Result        legacyResult `json:"result"`
		Error         string       `json:"error,omitempty"`
		Incomplete    bool         `json:"incomplete"`
		CaptureErrors []string     `json:"capture_errors"`
	}
	var legacy legacyCommandDiagnostic
	if config.Decode(raw, &legacy, 1<<20) != nil || legacy.Result.ExitCode != 1 || !legacy.Result.ExitObserved {
		t.Fatal("existing strict Result decoder rejected unchanged wire format")
	}
}
