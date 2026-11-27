package github

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"github.com/cli/go-gh/v2/pkg/api"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// WalkSourceOutput requires the existing immutable successful provider receipt.
// It downloads an explicit SOURCE artifact, never failure diagnostics.
func (c *Client) WalkSourceOutput(ctx context.Context, x Expectation, visit func(string, []byte) error) error {
	if x.Dispatch.Inputs["purpose"] != "source-output" || x.Dispatch.Validate() != nil || visit == nil {
		return Fail(6, "missing_evidence")
	}
	status,err := c.Status(ctx,x,"status","source-output-proposal")
	if err != nil || !status.OK || status.State != "passed" || status.AdmissionEligible { return Fail(6,"missing_evidence") }
	failed := []string{"linux-debian"}
	var listing struct {
		Total     int `json:"total_count"`
		Artifacts []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Size    int64  `json:"size_in_bytes"`
			Expired bool   `json:"expired"`
			Run     struct {
				ID      int64  `json:"id"`
				HeadSHA string `json:"head_sha"`
			} `json:"workflow_run"`
		} `json:"artifacts"`
	}
	base := "repos/" + x.Registration.Caller.Repository + "/actions/"
	if _, e := c.Request(ctx, "GET", base+"runs/"+x.RunID+"/artifacts?per_page=100", nil, &listing); e != nil {
		return e
	}
	if listing.Total != len(listing.Artifacts) || listing.Total > 100 {
		return Fail(6, "missing_evidence")
	}
	required := map[string]bool{}
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []struct {
			RunID   int64  `json:"run_id"`
			Attempt int    `json:"run_attempt"`
			Name    string `json:"name"`
			Steps   []struct {
				Name       string `json:"name"`
				Conclusion string `json:"conclusion"`
			} `json:"steps"`
		} `json:"jobs"`
	}
	if _, e := c.Request(ctx, "GET", base+"runs/"+x.RunID+"/attempts/"+strconv.Itoa(x.Attempt)+"/jobs?per_page=100", nil, &jobs); e != nil {
		return e
	}
	if jobs.Total != len(jobs.Jobs) || jobs.Total > 100 {
		return Fail(6, "missing_evidence")
	}
	for _, slot := range failed {
		matched := false
		for _, job := range jobs.Jobs {
			kind, platform, ok := diagnosticJobName(ctx, job.Name)
			if !ok {
				return Fail(6, "missing_evidence")
			}
			name := kind
			if platform != "" {
				name = platform
			}
			if name != slot {
				continue
			}
			if matched || runID(job.RunID) != x.RunID || job.Attempt != x.Attempt {
				return Fail(6, "missing_evidence")
			}
			matched = true
			executionFound := false
			for _, step := range job.Steps {
				if step.Name != "Execute" && step.Name != "Plan" {
					continue
				}
				if executionFound {
					return Fail(6, "missing_evidence")
				}
				executionFound = true
				switch step.Conclusion {
				case "success":
					required[slot] = true
				default:
					return Fail(6, "missing_evidence")
				}
			}
			if !executionFound {
				return Fail(6, "missing_evidence")
			}
		}
		if !matched {
			return Fail(6, "missing_evidence")
		}
	}
	found := map[string]bool{}
	attempt := strconv.Itoa(x.Attempt)
	pattern := regexp.MustCompile(`^source-locks-([a-z][a-z0-9-]{0,40})-` + attempt + `$`)
	for _, a := range listing.Artifacts {
		m := pattern.FindStringSubmatch(a.Name)
		if m == nil {
			continue
		}
		slot := m[1]
		if !required[slot] || slot != "plan" && !containsExpectedPlatform(x, slot) {
			return Fail(6, "missing_evidence")
		}
		if a.ID <= 0 || a.Expired || a.Size < 1 || a.Size > maxDiagnosticDownload || runID(a.Run.ID) != x.RunID || a.Run.HeadSHA != x.Registration.ControlSHA || found[slot] {
			return Fail(6, "missing_evidence")
		}
		requestCtx, cancel := context.WithTimeout(ctx, c.Timeout)
		response, e := c.REST.RequestWithContext(requestCtx, "GET", base+"artifacts/"+strconv.FormatInt(a.ID, 10)+"/zip", nil)
		location := ""
		if e != nil {
			var redirect *api.HTTPError
			if !errors.As(e, &redirect) || redirect.StatusCode != 302 {
				cancel()
				return Fail(6, "missing_evidence")
			}
			location = redirect.Headers.Get("Location")
		} else {
			if response.StatusCode != 302 {
				response.Body.Close()
				cancel()
				return Fail(6, "missing_evidence")
			}
			location = response.Header.Get("Location")
			response.Body.Close()
		}
		cancel()
		u, e := url.Parse(location)
		if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !(strings.HasSuffix(u.Hostname(), ".blob.core.windows.net") || strings.HasSuffix(u.Hostname(), ".actions.githubusercontent.com")) {
			return Fail(6, "missing_evidence")
		}
		requestCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		req, e := http.NewRequestWithContext(requestCtx, "GET", u.String(), nil)
		if e != nil {
			cancel()
			return Fail(6, "missing_evidence")
		}
		transport := c.ArtifactTransport
		if transport == nil {
			transport = &http.Transport{Proxy: nil, DisableCompression: true}
		}
		download := http.Client{Transport: transport, Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		r, e := download.Do(req)
		if e != nil {
			cancel()
			return Fail(6, "missing_evidence")
		}
		raw, e := io.ReadAll(io.LimitReader(r.Body, maxDiagnosticDownload+1))
		r.Body.Close()
		cancel()
		if e != nil || r.StatusCode != 200 || len(raw) > maxDiagnosticDownload {
			return Fail(6, "missing_evidence")
		}
		if !boundedDiagnosticZip(raw) {
			return Fail(6, "missing_evidence")
		}
		z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if e != nil || len(z.File) != 1 {
			return Fail(6, "missing_evidence")
		}
		f := z.File[0]
		if f.Name != "locks-"+slot+"-"+attempt+".age" || !f.Mode().IsRegular() || f.UncompressedSize64 > maxDiagnosticDownload {
			return Fail(6, "missing_evidence")
		}
		stream, e := f.Open()
		if e != nil {
			return Fail(6, "missing_evidence")
		}
		cipher, e := io.ReadAll(io.LimitReader(stream, maxDiagnosticDownload+1))
		stream.Close()
		if e != nil || len(cipher) > maxDiagnosticDownload {
			return Fail(6, "missing_evidence")
		}
		if e = visit(slot, cipher); e != nil {
			return e
		}
		found[slot] = true
	}
	if len(found) != len(required) {
		return Fail(6, "missing_evidence")
	}
	return nil
}
