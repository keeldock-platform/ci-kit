package github

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"gopkg.in/yaml.v3"
)

type ExperimentDiagnosticReceipt struct {
	Branch      string      `json:"branch"`
	Expectation Expectation `json:"expectation"`
}
type experimentDiagnosticsKey struct{}

var experimentPlatforms = []string{"windows-x64", "windows-arm64", "ios-simulator", "android-emulator"}

// ExperimentDiagnostics is an isolated, read-only synthetic evidence route. It
// neither dispatches nor relaxes production's main-only receipt validation.
func (c *Client) ExperimentDiagnostics(ctx context.Context, receipt ExperimentDiagnosticReceipt, visit func(string, []byte) error) error {
	x := receipt.Expectation
	r := x.Registration
	if !regexp.MustCompile(`^experiment/ci-[a-z0-9][a-z0-9-]{0,63}$`).MatchString(receipt.Branch) || x.SchemaVersion != 1 || r.Concern != "canary" || r.Caller.Repository != "keeldock-platform/platform-ci" || r.Caller.ID != 1430960557 || r.Caller.WorkflowPath != ".github/workflows/devlane-synthetic.yml" || r.Caller.Actor != "Moh-Bakr" || !protocol.Numeric(r.Caller.WorkflowID) || !protocol.Numeric(x.RunID) || x.Attempt < 1 || !protocol.SHA(r.ControlSHA) || !protocol.SHA(r.KitSHA) || !protocol.SHA(x.Dispatch.Inputs["source_sha"]) || x.Dispatch.Inputs["concern"] != "canary" || x.Dispatch.Inputs["purpose"] != "feedback" || (len(r.Platforms) != 4 && len(r.Platforms) != 2) {
		return Fail(3, "policy_refused")
	}
	seen := map[string]bool{}
	for _, p := range r.Platforms {
		if !slices.Contains(experimentPlatforms, p) || seen[p] {
			return Fail(3, "policy_refused")
		}
		seen[p] = true
	}
	// This experiment has two reviewed matrix shapes: all native arms or the
	// Windows pair. Bind the selected shape to the immutable caller request.
	if len(r.Platforms) == 2 && (!seen["windows-x64"] || !seen["windows-arm64"]) {
		return Fail(3, "policy_refused")
	}
	selected := append([]string{}, r.Platforms...)
	slices.Sort(selected)
	if x.Dispatch.Inputs["platforms"] != strings.Join(selected, ",") {
		return Fail(3, "policy_refused")
	}
	run, e := c.ReadRun(ctx, r, x.RunID)
	if e != nil {
		return e
	}
	if runID(run.ID) != x.RunID || run.RunAttempt != x.Attempt || runID(run.WorkflowID) != r.Caller.WorkflowID || run.Path != r.Caller.WorkflowPath || run.HeadBranch != receipt.Branch || run.HeadSHA != r.ControlSHA || run.Repository.ID != r.Caller.ID || run.Repository.FullName != r.Caller.Repository || run.Actor.Login != r.Caller.Actor || run.TriggeringActor.Login != r.Caller.Actor || run.Event != "workflow_dispatch" || run.Status != "completed" {
		return Fail(3, "policy_refused")
	}
	// Bind the expected source/kit request to the actual immutable caller bytes.
	var file struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if _, e = c.Request(ctx, "GET", "repos/"+r.Caller.Repository+"/contents/"+r.Caller.WorkflowPath+"?ref="+r.ControlSHA, nil, &file); e != nil {
		return e
	}
	raw, e := decodeContent(file.Content, file.Encoding)
	if e != nil || len(raw) > 1<<20 {
		return Fail(6, "missing_evidence")
	}
	var caller struct {
		Jobs map[string]struct {
			Uses string `yaml:"uses"`
			With struct {
				KitSHA  string `yaml:"kit_sha"`
				Request string `yaml:"request"`
			} `yaml:"with"`
		} `yaml:"jobs"`
	}
	if yaml.Unmarshal(raw, &caller) != nil || len(caller.Jobs) != 1 {
		return Fail(3, "policy_refused")
	}
	job, ok := caller.Jobs["matrix"]
	if !ok || job.Uses != "keeldock-platform/ci-kit/.github/workflows/experiment-matrix.yml@"+r.KitSHA || job.With.KitSHA != r.KitSHA {
		return Fail(3, "policy_refused")
	}
	var request map[string]string
	if config.Decode([]byte(job.With.Request), &request, 16384) != nil {
		return Fail(3, "policy_refused")
	}
	expected, _ := json.Marshal(x.Dispatch.Inputs)
	actual, _ := json.Marshal(request)
	if string(expected) != string(actual) {
		return Fail(3, "policy_refused")
	}
	// Bind job names, run and attempt independently of production's parser.
	var jobs struct {
		Total int      `json:"total_count"`
		Jobs  []apiJob `json:"jobs"`
	}
	path := "repos/" + r.Caller.Repository + "/actions/runs/" + x.RunID + "/attempts/" + itoa(x.Attempt) + "/jobs?per_page=100"
	if _, e = c.Request(ctx, "GET", path, nil, &jobs); e != nil {
		return e
	}
	if jobs.Total != len(r.Platforms)+1 || len(jobs.Jobs) != len(r.Platforms)+1 {
		return Fail(6, "missing_evidence")
	}
	observed := map[string]bool{}
	failed := []string{}
	for _, job := range jobs.Jobs {
		leaf := strings.TrimPrefix(job.Name, "matrix / ")
		if job.Name != "matrix / "+leaf || observed[leaf] || job.RunID != run.ID || job.RunAttempt != x.Attempt || job.Status != "completed" {
			return Fail(6, "missing_evidence")
		}
		observed[leaf] = true
		if leaf == "bootstrap" {
			continue
		}
		platform := strings.TrimPrefix(leaf, "pack-")
		if leaf != "pack-"+platform || !seen[platform] {
			return Fail(6, "missing_evidence")
		}
		if job.Conclusion == "failure" {
			failed = append(failed, platform)
		}
	}
	u, e := url.Parse(run.HTMLURL)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.Path != "/"+r.Caller.Repository+"/actions/runs/"+x.RunID || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Fail(3, "policy_refused")
	}
	return c.WalkDiagnosticArtifacts(context.WithValue(ctx, experimentDiagnosticsKey{}, true), x, failed, visit)
}
func diagnosticJobName(ctx context.Context, name string) (string, string, bool) {
	if enabled, _ := ctx.Value(experimentDiagnosticsKey{}).(bool); enabled {
		leaf := strings.TrimPrefix(name, "matrix / ")
		if name != "matrix / "+leaf {
			return "", "", false
		}
		if leaf == "bootstrap" {
			return "bootstrap", "", true
		}
		platform := strings.TrimPrefix(leaf, "pack-")
		return "checks", platform, leaf == "pack-"+platform && slices.Contains(experimentPlatforms, platform)
	}
	return publicJob(name)
}
