package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestExperimentDiagnosticReaderBindsCallerBranchSourceAndAttempt(t *testing.T) {
	for _, change := range []string{"valid", "branch", "attempt", "source", "caller", "windows-valid", "windows-missing-job", "windows-extra-job", "windows-request-platforms", "windows-one-platform", "windows-duplicate-platform", "windows-other-platform"} {
		t.Run(change, func(t *testing.T) {
			x := fixture()
			x.Registration.Concern = "canary"
			x.Registration.Caller.Repository = "keeldock-platform/platform-ci"
			x.Registration.Caller.ID = 1430960557
			x.Registration.Caller.WorkflowPath = ".github/workflows/devlane-synthetic.yml"
			x.Registration.Caller.Actor = "Moh-Bakr"
			x.Registration.Platforms = append([]string{}, experimentPlatforms...)
			if strings.HasPrefix(change, "windows-") {
				x.Registration.Platforms = []string{"windows-x64", "windows-arm64"}
			}
			selected := append([]string{}, x.Registration.Platforms...)
			slices.Sort(selected)
			x.Dispatch.Inputs["platforms"] = strings.Join(selected, ",")
			x.Dispatch.Inputs["concern"] = "canary"
			x.Dispatch.Inputs["purpose"] = "feedback"
			branch := "experiment/ci-diagnostics-fixture"
			receipt := ExperimentDiagnosticReceipt{Branch: branch, Expectation: x}
			run := fixtureRun(x)
			run.Repository.ID = x.Registration.Caller.ID
			run.Repository.FullName = x.Registration.Caller.Repository
			run.HTMLURL = "https://github.com/" + x.Registration.Caller.Repository + "/actions/runs/4"
			run.HeadBranch = branch
			run.Status = "completed"
			run.Conclusion = "failure"
			request := map[string]string{}
			for k, v := range x.Dispatch.Inputs {
				request[k] = v
			}
			if change == "source" {
				request["source_sha"] = strings.Repeat("f", 40)
			}
			if change == "windows-request-platforms" {
				request["platforms"] = "windows-x64"
			}
			requestRaw, _ := json.Marshal(request)
			workflow, _ := json.Marshal(map[string]any{"jobs": map[string]any{"matrix": map[string]any{"uses": "keeldock-platform/ci-kit/.github/workflows/experiment-matrix.yml@" + x.Registration.KitSHA, "with": map[string]any{"kit_sha": x.Registration.KitSHA, "request": string(requestRaw)}}}})
			if change == "branch" {
				run.HeadBranch = "main"
			}
			if change == "attempt" {
				run.RunAttempt = 2
			}
			if change == "caller" {
				run.Repository.ID++
			}
			jobs := []any{map[string]any{"run_id": 4, "run_attempt": 1, "id": 1, "name": "matrix / bootstrap", "status": "completed", "conclusion": "success"}}
			for i, p := range x.Registration.Platforms {
				conclusion := "success"
				if i == 0 {
					conclusion = "failure"
				}
				jobs = append(jobs, map[string]any{"run_id": 4, "run_attempt": 1, "id": i + 2, "name": "matrix / pack-" + p, "status": "completed", "conclusion": conclusion, "steps": []any{map[string]any{"name": "Execute", "conclusion": conclusion}}})
			}
			switch change {
			case "windows-missing-job":
				jobs = jobs[:len(jobs)-1]
			case "windows-extra-job":
				jobs = append(jobs, map[string]any{"run_id": 4, "run_attempt": 1, "id": 9, "name": "matrix / pack-ios-simulator", "status": "completed", "conclusion": "success"})
			case "windows-one-platform":
				receipt.Expectation.Registration.Platforms = []string{"windows-x64"}
			case "windows-duplicate-platform":
				receipt.Expectation.Registration.Platforms = []string{"windows-x64", "windows-x64"}
			case "windows-other-platform":
				receipt.Expectation.Registration.Platforms = []string{"windows-x64", "ios-simulator"}
			}
			var archive bytes.Buffer
			z := zip.NewWriter(&archive)
			f, _ := z.Create("failure-windows-x64-1.age")
			f.Write([]byte("recorded ciphertext"))
			z.Close()
			reads := 0
			c, e := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
				reads++
				if r.Method != "GET" {
					t.Fatal("experiment diagnostics performed mutation")
				}
				switch {
				case strings.Contains(r.URL.Path, "/contents/"):
					return reply(200, map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(workflow)}), nil
				case strings.HasSuffix(r.URL.Path, "/jobs"):
					return reply(200, map[string]any{"total_count": len(jobs), "jobs": jobs}), nil
				case strings.HasSuffix(r.URL.Path, "/zip"):
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://fixture.blob.core.windows.net/file"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				case strings.HasSuffix(r.URL.Path, "/artifacts"):
					return reply(200, map[string]any{"total_count": 1, "artifacts": []any{map[string]any{"id": 12, "name": "diagnostics-windows-x64-1", "size_in_bytes": archive.Len(), "expired": false, "workflow_run": map[string]any{"id": 4, "head_sha": x.Registration.ControlSHA}}}}), nil
				default:
					return reply(200, run), nil
				}
			}))
			if e != nil {
				t.Fatal(e)
			}
			c.ArtifactTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" {
					t.Fatal("credential forwarded")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(archive.Bytes()))}, nil
			})
			visits := 0
			e = c.ExperimentDiagnostics(context.Background(), receipt, func(slot string, cipher []byte) error {
				visits++
				if slot != "windows-x64" || string(cipher) != "recorded ciphertext" {
					t.Fatal("wrong evidence")
				}
				return nil
			})
			if (change == "valid" || change == "windows-valid") && (e != nil || visits != 1 || reads != 6) {
				t.Fatalf("valid experiment evidence refused: %v reads=%d visits=%d", e, reads, visits)
			}
			if change != "valid" && change != "windows-valid" && (e == nil || visits != 0) {
				t.Fatal("unbound experiment evidence accepted")
			}
		})
	}
}
