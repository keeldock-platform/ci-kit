package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/keeldock-platform/ci-kit/internal/collector"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Run struct {
	ID           int64  `json:"id"`
	RunAttempt   int    `json:"run_attempt"`
	WorkflowID   int64  `json:"workflow_id"`
	Path         string `json:"path"`
	HeadBranch   string `json:"head_branch"`
	HeadSHA      string `json:"head_sha"`
	Event        string `json:"event"`
	DisplayTitle string `json:"display_title"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	HTMLURL      string `json:"html_url"`
	Actor        struct {
		Login string `json:"login"`
	} `json:"actor"`
	TriggeringActor struct {
		Login string `json:"login"`
	} `json:"triggering_actor"`
	Repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func itoa(n int) string    { return strconv.Itoa(n) }
func runID(n int64) string { return strconv.FormatInt(n, 10) }
func (c *Client) ReadRun(ctx context.Context, r Registration, id string) (Run, error) {
	var run Run
	if !protocol.Numeric(id) {
		return run, Fail(2, "usage")
	}
	_, e := c.Request(ctx, "GET", "repos/"+r.Caller.Repository+"/actions/runs/"+id, nil, &run)
	return run, e
}
func bind(x Expectation, run Run) error {
	r := x.Registration
	wid, _ := strconv.ParseInt(r.Caller.WorkflowID, 10, 64)
	if runID(run.ID) != x.RunID || run.RunAttempt != x.Attempt || run.WorkflowID != wid || run.Repository.ID != r.Caller.ID || run.Repository.FullName != r.Caller.Repository || run.Path != r.Caller.WorkflowPath || run.HeadBranch != "main" || run.HeadSHA != r.ControlSHA || run.Event != "workflow_dispatch" || run.DisplayTitle != x.Dispatch.RunName() || run.Actor.Login != r.Caller.Actor || run.TriggeringActor.Login != r.Caller.Actor {
		return Fail(3, "policy_refused")
	}
	u, e := url.Parse(run.HTMLURL)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/"+r.Caller.Repository+"/actions/runs/"+x.RunID {
		return Fail(6, "missing_evidence")
	}
	return nil
}
func identity(x Expectation, run Run) protocol.Identity {
	i := x.Dispatch.Inputs
	r := x.Registration
	selectorDigest := sha256.Sum256([]byte(i["selector"]))
	return protocol.Identity{Concern: protocol.Ptr(r.Concern), SourceSHA: protocol.Ptr(i["source_sha"]), BaseSHA: protocol.Ptr(i["base_sha"]), ControlSHA: protocol.Ptr(r.ControlSHA), KitSHA: protocol.Ptr(r.KitSHA), CompositionSHA256: protocol.Ptr(x.CompositionSHA256), SelectorSHA256: protocol.Ptr(hex.EncodeToString(selectorDigest[:])), RequestID: protocol.Ptr(i["request_id"]), CIRepo: protocol.Ptr(r.Caller.Repository), WorkflowID: protocol.Ptr(r.Caller.WorkflowID), RunID: optional(x.RunID), Attempt: optionalAttempt(x.Attempt), URL: optional(run.HTMLURL), Purpose: protocol.Ptr(i["purpose"])}
}
func (c *Client) DispatchRun(ctx context.Context, r Registration, d protocol.Dispatch, composition string) (Expectation, Run, error) {
	if !protocol.Digest(composition) {
		return Expectation{}, Run{}, Fail(2, "configuration")
	}
	x := Expectation{CompositionSHA256: composition, SchemaVersion: 1, Registration: r, Dispatch: d, CreatedAt: time.Now().UTC()}
	var response struct {
		WorkflowRunID int64  `json:"workflow_run_id"`
		RunURL        string `json:"run_url"`
		HTMLURL       string `json:"html_url"`
	}
	status, e := c.Request(ctx, "POST", "repos/"+r.Caller.Repository+"/actions/workflows/"+r.Caller.WorkflowID+"/dispatches", d, &response)
	var run Run
	if e == nil && status == 200 && response.WorkflowRunID > 0 {
		x.RunID = runID(response.WorkflowRunID)
		run, e = c.ReadRun(ctx, r, x.RunID)
		if e != nil {
			return x, run, e
		}
	} else {
		var recErr error
		run, recErr = c.reconcile(ctx, r, d)
		if recErr != nil {
			return x, run, Fail(6, "dispatch_ambiguous")
		}
		x.RunID = runID(run.ID)
	}
	x.Attempt = run.RunAttempt
	if x.Attempt != 1 || bind(x, run) != nil {
		return x, run, Fail(6, "missing_evidence")
	}
	return x, run, nil
}
func (c *Client) reconcile(ctx context.Context, r Registration, d protocol.Dispatch) (Run, error) {
	var found []Run
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for page := 1; page <= 10; page++ {
		var rs struct {
			Runs []Run `json:"workflow_runs"`
		}
		_, e := c.Request(ctx, "GET", "repos/"+r.Caller.Repository+"/actions/workflows/"+r.Caller.WorkflowID+"/runs?event=workflow_dispatch&per_page=100&page="+itoa(page), nil, &rs)
		if e != nil {
			return Run{}, e
		}
		for _, run := range rs.Runs {
			if run.DisplayTitle == d.RunName() {
				found = append(found, run)
			}
		}
		if len(rs.Runs) < 100 {
			break
		}
		if page == 10 {
			return Run{}, Fail(6, "dispatch_ambiguous")
		}
	}
	if len(found) != 1 {
		return Run{}, Fail(6, "dispatch_ambiguous")
	}
	return found[0], nil
}

type apiJob struct {
	CreatedAt   *string `json:"created_at"`
	RunnerID    int64   `json:"runner_id"`
	ID          int64   `json:"id"`
	RunID       int64   `json:"run_id"`
	RunAttempt  int     `json:"run_attempt"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	Conclusion  string  `json:"conclusion"`
	StartedAt   *string `json:"started_at"`
	CompletedAt *string `json:"completed_at"`
}

func slots(x Expectation) []collector.Slot {
	out := []collector.Slot{{Slot: "preflight"}, {Slot: "plan"}}
	ps := x.Registration.Platforms
	if x.Dispatch.Inputs["platforms"] != "all" {
		ps = strings.Split(x.Dispatch.Inputs["platforms"], ",")
	}
	for _, p := range ps {
		out = append(out, collector.Slot{Platform: p, Slot: "checks"})
	}
	return out
}
func publicJob(name string) (string, string, bool) { return protocol.ParseJobName(name) }

func (c *Client) Status(ctx context.Context, x Expectation, command, version string) (protocol.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out := protocol.New(command, version, true)
	run, e := c.ReadRun(ctx, x.Registration, x.RunID)
	if e != nil {
		return out, e
	}
	if e = bind(x, run); e != nil {
		return out, e
	}
	out.Identity = identity(x, run)
	for _, s := range slots(x) {
		if s.Platform != "" {
			out.Coverage.Expected = append(out.Coverage.Expected, s.Platform)
		}
	}
	jobs := []apiJob{}
	seen := map[int64]bool{}
	for page := 1; page <= 100; page++ {
		var data struct {
			TotalCount int      `json:"total_count"`
			Jobs       []apiJob `json:"jobs"`
		}
		path := fmt.Sprintf("repos/%s/actions/runs/%s/attempts/%d/jobs?per_page=100&page=%d", x.Registration.Caller.Repository, x.RunID, x.Attempt, page)
		if _, e = c.Request(ctx, "GET", path, nil, &data); e != nil {
			return out, e
		}
		for _, job := range data.Jobs {
			if seen[job.ID] {
				return out, Fail(6, "missing_evidence")
			}
			seen[job.ID] = true
			jobs = append(jobs, job)
		}
		if len(data.Jobs) < 100 {
			if len(jobs) != data.TotalCount {
				return out, Fail(6, "missing_evidence")
			}
			break
		}
		if page == 100 {
			return out, Fail(6, "missing_evidence")
		}
	}
	obs := collector.Observed{Needs: map[string]string{}}
	terminal := run.Status == "completed"
	failed := false
	cancelled := false
	for _, j := range jobs {
		s, p, ok := publicJob(j.Name)
		if !ok {
			return out, Fail(6, "missing_evidence")
		}
		if j.RunID != run.ID || j.RunAttempt != x.Attempt {
			return out, Fail(6, "missing_evidence")
		}
		if p != "" && !containsExpectedPlatform(x, p) {
			if j.Status == "completed" && j.Conclusion == "skipped" {
				continue
			}
			return out, Fail(6, "missing_evidence")
		}
		obs.Jobs = append(obs.Jobs, collector.Job{ID: j.ID, RunID: j.RunID, Attempt: j.RunAttempt, Platform: p, Slot: s, Status: j.Status, Conclusion: j.Conclusion})
		if s != "finalizer" {
			obs.Needs[p+"/"+s] = j.Conclusion
		}
		state := "unknown"
		switch j.Status {
		case "queued", "waiting", "pending":
			state = "queued"
		case "in_progress":
			state = "running"
		case "completed":
			switch j.Conclusion {
			case "success":
				state = "passed"
				if p != "" {
					out.Coverage.Completed = append(out.Coverage.Completed, p)
				}
			case "failure":
				state = "failed"
				failed = true
			case "cancelled":
				state = "cancelled"
				cancelled = true
			case "skipped":
				state = "skipped"
			}
		}
		out.Stages = append(out.Stages, protocol.Stage{Slot: s, Platform: optional(p), State: state, StartedAt: j.StartedAt, CompletedAt: j.CompletedAt})
	}
	out.Coverage.Expected = canonicalStrings(out.Coverage.Expected)
	out.Coverage.Completed = canonicalStrings(out.Coverage.Completed)
	sort.SliceStable(out.Stages, func(i, j int) bool { return stageOrder(out.Stages[i]) < stageOrder(out.Stages[j]) })
	verdict := collector.Evaluate(collector.Expected{RunID: run.ID, Attempt: x.Attempt, Slots: slots(x), Finalizer: "finalizer"}, obs)
	if failed {
		out.Fail(1, "check_failed")
		return out, nil
	}
	if !terminal {
		for _, job := range jobs {
			slot, platform, ok := publicJob(job.Name)
			if !ok || slot != "checks" || !containsExpectedPlatform(x, platform) || job.Status != "queued" || job.RunnerID != 0 || job.StartedAt != nil || job.CreatedAt == nil {
				continue
			}
			created, e := time.Parse(time.RFC3339, *job.CreatedAt)
			if e == nil && time.Since(created) >= 20*time.Minute {
				out.Fail(5, "capacity")
				return out, nil
			}
		}
		out.Fail(8, "pending")
		out.State = "queued"
		if run.Status == "in_progress" {
			out.State = "running"
		}
		return out, nil
	}
	if cancelled || run.Conclusion == "cancelled" {
		out.Fail(7, "interrupted")
		return out, nil
	}
	if !verdict.Passed || !verdict.Complete || run.Conclusion != "success" {
		out.Fail(6, "missing_evidence")
		return out, nil
	}
	out.State = "passed"
	out.OK = true
	out.ExitCode = 0
	// Production qualification is an exact released-payload gate; prerelease tools cannot admit.
	out.AdmissionEligible = false
	return out, nil
}
func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
func (c *Client) Wait(ctx context.Context, x Expectation, command, version string) (protocol.Result, error) {
	for {
		r, e := c.Status(ctx, x, command, version)
		if e != nil || r.ExitCode != 8 {
			return r, e
		}
		if e = c.Sleep(ctx, 120*time.Second); e != nil {
			return r, Fail(4, "deadline")
		}
	}
}
func (c *Client) Cancel(ctx context.Context, x Expectation, version string, observers ...func(CancellationRecord) error) (r protocol.Result, err error) {
	r = protocol.New("cancel", version, true)
	if len(observers) > 1 || len(observers) == 1 && observers[0] == nil {
		return r, Fail(6, "missing_evidence")
	}
	record := CancellationRecord{SchemaVersion: 1, RunID: x.RunID, OriginalAttempt: x.Attempt, ObservedState: "unknown"}
	persist := func() error {
		if len(observers) == 0 {
			return nil
		}
		if observers[0](record) != nil {
			return Fail(6, "missing_evidence")
		}
		return nil
	}
	observe := func(run Run) {
		record.ObservedAttempt = run.RunAttempt
		record.ObservedState = "unknown"
		if run.Status == "queued" || run.Status == "in_progress" {
			record.ObservedState = run.Status
		} else if run.Status == "completed" {
			record.ObservedState = terminalState(run.Conclusion)
		}
	}
	defer func() {
		if err != nil {
			record.ErrorCode = "missing_evidence"
			var failure *Failure
			if errors.As(err, &failure) {
				switch failure.Code {
				case "cancel_attempt_drift", "policy_refused", "deadline", "interrupted":
					record.ErrorCode = failure.Code
				}
			}
		}
		if persist() != nil {
			err = Fail(6, "missing_evidence")
		}
	}()
	r.Operation = &protocol.Operation{}
	run, e := c.ReadRun(ctx, x.Registration, x.RunID)
	if e != nil {
		return r, e
	}
	observe(run)
	if e = bind(x, run); e != nil {
		return r, e
	}
	r.Identity = identity(x, run)
	if run.Status == "completed" {
		r.State = terminalState(run.Conclusion)
		return r, nil
	}
	// Immediately recheck the provider attempt before the run-scoped write.
	run, e = c.ReadRun(ctx, x.Registration, x.RunID)
	if e != nil {
		return r, e
	}
	observe(run)
	if e = bind(x, run); e != nil {
		return r, e
	}
	if run.Status == "completed" {
		r.State = terminalState(run.Conclusion)
		return r, nil
	}
	record.RequestedAttempt = x.Attempt
	if e = persist(); e != nil {
		return r, e
	}
	status, e := c.Request(ctx, "POST", "repos/"+x.Registration.Caller.Repository+"/actions/runs/"+x.RunID+"/cancel", nil, nil)
	record.PostAttempted = true
	if e != nil || status != 202 {
		return r, Fail(6, "missing_evidence")
	}
	r.Operation.CancelRequested = true
	record.CancelRequested = true
	if e = persist(); e != nil {
		return r, e
	}
	for {
		current, e := c.ReadRun(ctx, x.Registration, x.RunID)
		if e != nil {
			return r, e
		}
		observe(current)
		if current.RunAttempt != x.Attempt {
			return r, Fail(6, "cancel_attempt_drift")
		}
		if current.Status == "completed" {
			if bind(x, current) != nil {
				return r, Fail(3, "policy_refused")
			}
			r.State = terminalState(current.Conclusion)
			r.Operation.CancelConfirmed = current.Conclusion == "cancelled"
			record.CancelConfirmed = r.Operation.CancelConfirmed
			return r, nil
		}
		if e = c.Sleep(ctx, 120*time.Second); e != nil {
			return r, Fail(4, "deadline")
		}
	}
}
func terminalState(conclusion string) string {
	switch conclusion {
	case "success":
		return "passed"
	case "failure":
		return "failed"
	case "cancelled":
		return "cancelled"
	}
	return "unknown"
}

// ResultIdentity exposes only the already public immutable request tuple.
func ResultIdentity(x Expectation, run Run) protocol.Identity { return identity(x, run) }

func optionalAttempt(n int) *int {
	if n < 1 {
		return nil
	}
	return &n
}

func containsExpectedPlatform(x Expectation, p string) bool {
	for _, s := range slots(x) {
		if s.Platform == p {
			return true
		}
	}
	return false
}

func canonicalStrings(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return slices.Compact(out)
}
func stageOrder(s protocol.Stage) string {
	platform := ""
	started := ""
	completed := ""
	if s.Platform != nil {
		platform = *s.Platform
	}
	if s.StartedAt != nil {
		started = *s.StartedAt
	}
	if s.CompletedAt != nil {
		completed = *s.CompletedAt
	}
	return fmt.Sprintf("%02d/%s/%s/%s/%s", slices.Index(protocol.Slots, s.Slot), platform, s.State, started, completed)
}
