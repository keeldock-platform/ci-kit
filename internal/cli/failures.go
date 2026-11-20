package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/keeldock-platform/ci-kit/internal/collector"
	"github.com/keeldock-platform/ci-kit/internal/config"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

// privateFailures never writes names or errors to the CLI result or its streams.
// Reports already held locally can be inspected without a hosted publication path.
func (a *app) privateFailures(report, output string) error {
	a.begin("failures", false)
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITHUB_RUN_ID", "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"} {
		if os.Getenv(key) != "" {
			return gh.Fail(3, "policy_refused")
		}
	}
	if report == "" || output == "" {
		return gh.Fail(2, "usage")
	}
	report, e := filepath.Abs(report)
	if e != nil {
		return gh.Fail(3, "policy_refused")
	}
	output, e = filepath.Abs(output)
	if e != nil {
		return gh.Fail(3, "policy_refused")
	}
	if !config.NoLinks(report) || !config.NoLinks(filepath.Dir(output)) || !process.UserOnly(report, false) || !process.UserOnly(filepath.Dir(output), true) {
		return gh.Fail(3, "policy_refused")
	}
	f, e := os.Open(report)
	if e != nil {
		return gh.Fail(6, "missing_evidence")
	}
	data, e := io.ReadAll(io.LimitReader(f, collector.MaxReport+1))
	f.Close()
	if e != nil {
		return gh.Fail(6, "missing_evidence")
	}
	failures, e := collector.Failures(data)
	if e != nil {
		return gh.Fail(6, "missing_evidence")
	}
	data, e = json.MarshalIndent(failures, "", "  ")
	if e != nil || len(data) > collector.MaxReport {
		return gh.Fail(6, "missing_evidence")
	}
	f, e = process.CreatePrivateFile(output)
	if e != nil {
		return gh.Fail(3, "policy_refused")
	}
	_, writeErr := f.Write(append(data, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return gh.Fail(6, "missing_evidence")
	}
	a.result.Local.Data = map[string]any{"failure_count": len(failures)}
	return nil
}
