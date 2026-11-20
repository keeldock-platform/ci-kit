package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/keeldock-platform/ci-kit/internal/collector"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(status int, v any) *http.Response {
	b, _ := json.Marshal(v)
	request, _ := http.NewRequest("GET", "https://api.github.com/synthetic", nil)
	return &http.Response{Request: request, StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(b)))}
}
func fixture() Expectation {
	d, _ := protocol.NewDispatch("example", strings.Repeat("a", 40), strings.Repeat("b", 40), "full", "feedback", "all", "all", "11111111-1111-4111-8111-111111111111")
	return Expectation{CompositionSHA256: strings.Repeat("e", 64), SchemaVersion: 1, Registration: Registration{SchemaVersion: 1, Concern: "example", Source: RegisteredRepository{"example/source", 1, "main"}, Caller: RegisteredCaller{"example/checks", 2, "3", ".github/workflows/dispatch.yml", "example-admin"}, ControlSHA: strings.Repeat("c", 40), KitSHA: strings.Repeat("d", 40), KitAPI: 1, CapacityAvailable: true, Platforms: []string{"windows-arm64"}, CompositionSHA256: strings.Repeat("e", 64), SourceMapSHA256: strings.Repeat("f", 64), SigningPolicy: "ssh-v1", Slots: []string{"checks"}}, Dispatch: d, RunID: "4", Attempt: 1, CreatedAt: time.Now()}
}
func fixtureRun(x Expectation) Run {
	r := Run{ID: 4, RunAttempt: 1, WorkflowID: 3, Path: x.Registration.Caller.WorkflowPath, HeadBranch: "main", HeadSHA: x.Registration.ControlSHA, Event: "workflow_dispatch", DisplayTitle: x.Dispatch.RunName(), Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/example/checks/actions/runs/4"}
	r.Actor.Login = x.Registration.Caller.Actor
	r.TriggeringActor.Login = x.Registration.Caller.Actor
	r.Repository.ID = 2
	r.Repository.FullName = "example/checks"
	return r
}
func TestExplicitGoGHTransportHeadersAndSafeError(t *testing.T) {
	called := 0
	c, e := New("synthetic-token-marker", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
		called++
		if r.URL.Host != "api.github.com" || r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" || !strings.Contains(r.Header.Get("Authorization"), "synthetic-token-marker") {
			t.Fatal("missing explicit options")
		}
		return reply(403, map[string]string{"message": "private-upstream-marker"}), nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.Request(context.Background(), "GET", "user", nil, new(any))
	var f *Failure
	if !errors.As(e, &f) || f.Exit != 3 || strings.Contains(e.Error(), "private-upstream-marker") || called != 1 {
		t.Fatal("unsafe error or retry")
	}
}
func TestHostAndInjectionOverridesRefused(t *testing.T) {
	for _, k := range []string{"GH_PATH", "GH_HOST", "GH_API_HOST", "GH_DEBUG", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NODE_OPTIONS", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "GIT_CONFIG_COUNT"} {
		if EnvironmentAllowed([]string{k + "=synthetic-marker"}) {
			t.Fatalf("accepted%s", k)
		}
	}
	if !EnvironmentAllowed([]string{"GH_HOST=github.com", "GH_TOKEN=synthetic-marker"}) {
		t.Fatal("valid headless convention refused")
	}
}
func TestDispatch200UsesReturnedIDWithoutRunSearch(t *testing.T) {
	x := fixture()
	run := fixtureRun(x)
	posts, searches := 0, 0
	c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts++
			return reply(200, map[string]any{"workflow_run_id": 4, "run_url": "https://api.github.com/repos/example/checks/actions/runs/4", "html_url": run.HTMLURL}), nil
		}
		if strings.Contains(r.URL.Path, "/workflows/") {
			searches++
		}
		return reply(200, run), nil
	}))
	got, _, e := c.DispatchRun(context.Background(), x.Registration, x.Dispatch, x.CompositionSHA256)
	if e != nil || got.RunID != "4" || posts != 1 || searches != 0 {
		t.Fatalf("wrong normal path%v", e)
	}
}
func TestAmbiguousDispatchNeverRetriesPOST(t *testing.T) {
	for _, status := range []int{204, 503} {
		x := fixture()
		posts := 0
		c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == "POST" {
				posts++
				if status == 204 {
					return reply(204, nil), nil
				}
				return reply(503, map[string]string{"message": "private-upstream-marker"}), nil
			}
			return reply(200, map[string]any{"workflow_runs": []Run{}}), nil
		}))
		_, _, e := c.DispatchRun(context.Background(), x.Registration, x.Dispatch, x.CompositionSHA256)
		var f *Failure
		if !errors.As(e, &f) || f.Code != "dispatch_ambiguous" || posts != 1 {
			t.Fatal("ambiguous write retried or accepted")
		}
	}
}
func TestDispatchAmbiguityResolvesUniqueUUID(t *testing.T) {
	x := fixture()
	run := fixtureRun(x)
	c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			return reply(204, nil), nil
		}
		return reply(200, map[string]any{"workflow_runs": []Run{run}}), nil
	}))
	got, _, e := c.DispatchRun(context.Background(), x.Registration, x.Dispatch, x.CompositionSHA256)
	if e != nil || got.RunID != "4" {
		t.Fatal(e)
	}
}
func TestImmutableRunBindingRejectsEveryMismatch(t *testing.T) {
	x := fixture()
	tests := []func(*Run){func(r *Run) { r.HeadSHA = strings.Repeat("e", 40) }, func(r *Run) { r.HeadBranch = "tag" }, func(r *Run) { r.RunAttempt = 2 }, func(r *Run) { r.Actor.Login = "other-admin" }, func(r *Run) { r.Repository.ID = 99 }, func(r *Run) { r.WorkflowID = 99 }, func(r *Run) { r.DisplayTitle = "unrelated" }, func(r *Run) { r.Event = "pull_request" }, func(r *Run) { r.HTMLURL = "https://example.invalid/run" }}
	for i, f := range tests {
		r := fixtureRun(x)
		f(&r)
		if bind(x, r) == nil {
			t.Fatalf("mismatch%d accepted", i)
		}
	}
}
func TestAttemptSpecificJobsAndPublicSuccess(t *testing.T) {
	x := fixture()
	run := fixtureRun(x)
	jobs := []apiJob{{ID: 1, RunID: 4, RunAttempt: 1, Name: "preflight", Status: "completed", Conclusion: "success"}, {ID: 2, RunID: 4, RunAttempt: 1, Name: "plan", Status: "completed", Conclusion: "success"}, {ID: 3, RunID: 4, RunAttempt: 1, Name: "pack-windows-arm64", Status: "completed", Conclusion: "success"}, {ID: 4, RunID: 4, RunAttempt: 1, Name: "collect", Status: "completed", Conclusion: "success"}}
	c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/jobs") {
			if !strings.Contains(r.URL.Path, "/attempts/1/") {
				t.Fatal("attempt not pinned")
			}
			return reply(200, map[string]any{"total_count": len(jobs), "jobs": jobs}), nil
		}
		return reply(200, run), nil
	}))
	r, e := c.Status(context.Background(), x, "status", "0.1.0-dev")
	if e != nil || r.ExitCode != 0 || r.State != "passed" || r.AdmissionEligible {
		t.Fatalf("%v %+v", e, r)
	}
	b, _ := json.Marshal(r)
	for _, private := range []string{"Argv", "Census", "private-test-marker"} {
		if strings.Contains(string(b), private) {
			t.Fatal("private disclosure")
		}
	}
	jobs = jobs[:3]
	r, e = c.Status(context.Background(), x, "status", "0.1.0-dev")
	if e != nil || r.ExitCode != 6 {
		t.Fatal("missing finalizer admitted")
	}
}
func TestCollectorGlobalSlotIntegration(t *testing.T) {
	x := fixture()
	expected := slots(x)
	obs := collector.Observed{Needs: map[string]string{}}
	for i, s := range expected {
		obs.Jobs = append(obs.Jobs, collector.Job{ID: int64(i + 1), RunID: 4, Attempt: 1, Platform: s.Platform, Slot: s.Slot, Status: "completed", Conclusion: "success"})
		obs.Needs[s.Platform+"/"+s.Slot] = "success"
	}
	obs.Jobs = append(obs.Jobs, collector.Job{ID: 100, RunID: 4, Attempt: 1, Slot: "finalizer", Status: "completed", Conclusion: "success"})
	v := collector.Evaluate(collector.Expected{RunID: 4, Attempt: 1, Slots: expected, Finalizer: "finalizer"}, obs)
	if !v.Passed || !v.Complete {
		t.Fatal(v)
	}
}
func TestCancellationAttemptDriftPreservesRequest(t *testing.T) {
	x := fixture()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "records")
	if e = process.MakePrivateDir(dir); e != nil {
		t.Fatal(e)
	}
	recorder, e := CancellationRecorder(dir, x)
	if e != nil {
		t.Fatal(e)
	}
	run := fixtureRun(x)
	run.Status = "in_progress"
	posts := 0
	c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts++
			return reply(202, nil), nil
		}
		if posts > 0 {
			run.RunAttempt = 2
		}
		return reply(200, run), nil
	}))
	r, e := c.Cancel(context.Background(), x, "0.1.0-dev", recorder)
	var f *Failure
	if !errors.As(e, &f) || f.Code != "cancel_attempt_drift" || !r.Operation.CancelRequested || r.Operation.CancelConfirmed || posts != 1 || r.Identity.Attempt == nil || *r.Identity.Attempt != 1 {
		t.Fatal("cancel race lost")
	}
	files, e := os.ReadDir(dir)
	if e != nil || len(files) != 3 {
		t.Fatal("missing private cancellation events", e)
	}
	var operationID string
	for i, file := range files {
		p := filepath.Join(dir, file.Name())
		if !process.UserOnly(p, false) {
			t.Fatal("cancellation record is not user-only")
		}
		b, e := os.ReadFile(p)
		var event struct {
			CancellationRecord
			OperationID   string `json:"operation_id"`
			RequestID     string `json:"request_id"`
			RequestDigest string `json:"request_digest"`
		}
		if e != nil || json.Unmarshal(b, &event) != nil || event.OriginalAttempt != 1 || event.RequestedAttempt != 1 || event.RequestID != x.Dispatch.Inputs["request_id"] || event.RequestDigest != x.Dispatch.Inputs["request_digest"] || !protocol.UUID(event.OperationID) {
			t.Fatal("private cancellation tuple lost")
		}
		if i == 0 {
			operationID = event.OperationID
			if event.PostAttempted || event.CancelRequested || event.ObservedAttempt != 1 {
				t.Fatal("write intent confused with acknowledgement")
			}
		} else if event.OperationID != operationID || !event.PostAttempted || !event.CancelRequested {
			t.Fatal("cancellation acknowledgement lost")
		}
		if i == 2 && (event.ObservedAttempt != 2 || event.ErrorCode != "cancel_attempt_drift" || event.CancelConfirmed) {
			t.Fatal("observed rerun attempt was discarded")
		}
	}
	public, _ := json.Marshal(r)
	if strings.Contains(string(public), "observed_attempt") || strings.Contains(string(public), "operation_id") {
		t.Fatal("private cancellation record reached public result")
	}
}

func TestCancellationRecordFailurePreventsWrite(t *testing.T) {
	x := fixture()
	run := fixtureRun(x)
	run.Status = "in_progress"
	posts := 0
	c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts++
		}
		return reply(200, run), nil
	}))
	r, e := c.Cancel(context.Background(), x, "0.1.0-dev", func(CancellationRecord) error {
		return errors.New("synthetic private disk failure")
	})
	var failure *Failure
	if !errors.As(e, &failure) || failure.Code != "missing_evidence" || posts != 0 || r.Operation.CancelRequested || r.Operation.CancelConfirmed {
		t.Fatal("cancellation proceeded without durable private evidence")
	}
}
func TestPrivateStateRejectsCorruption(t *testing.T) {
	root := t.TempDir()
	dir, e := StateDir(root, "results")
	if e != nil {
		t.Fatal(e)
	}
	x := fixture()
	if SaveExpectation(dir, x) != nil {
		t.Fatal("save")
	}
	if _, e = ReadExpectation(dir, x.RunID); e != nil {
		t.Fatal(e)
	}
	if SaveExpectation(dir, x) == nil {
		t.Fatal("overwrite accepted")
	}
	if _, e = ReadExpectation(dir, "../escape"); e == nil {
		t.Fatal("escape accepted")
	}
}
func TestSignatureFormatAndPositiveRolePolicy(t *testing.T) {
	ssh := []byte("tree " + strings.Repeat("a", 40) + "\ngpgsig -----BEGIN SSH SIGNATURE-----\n data\n -----END SSH SIGNATURE-----\n\nmessage\n")
	if !sshSignatureObject(ssh) {
		t.Fatal("SSH signature refused")
	}
	for _, marker := range []string{"-----BEGIN PGP SIGNATURE-----", "-----BEGIN SIGNED MESSAGE-----"} {
		if sshSignatureObject([]byte("tree x\ngpgsig " + marker + "\n data\n\nmessage")) {
			t.Fatal("other format accepted")
		}
	}
	blob := make([]byte, 51)
	binary.BigEndian.PutUint32(blob[:4], 11)
	copy(blob[4:15], "ssh-ed25519")
	binary.BigEndian.PutUint32(blob[15:19], 32)
	encoded := base64.StdEncoding.EncodeToString(blob)
	roles, e := signerRoles([]byte("approved-person ssh-ed25519 " + encoded + "\nlane-worker ssh-ed25519 " + encoded + "\n"))
	if e != nil {
		t.Fatal(e)
	}
	for _, role := range roles {
		if !role.Person || !role.Lane || !role.Principals["approved-person"] {
			t.Fatal("key lane classification lost")
		}
	}
	if _, e = signerRoles([]byte("* ssh-ed25519 " + encoded + "\n")); e == nil {
		t.Fatal("wildcard person authority")
	}
}
func TestCanonicalRevocationLedgerClosed(t *testing.T) {
	if _, e := revokedFingerprints(nil); e != nil {
		t.Fatal("empty current ledger refused")
	}
	fp := "SHA256:" + strings.Repeat("a", 43)
	if got, e := revokedFingerprints([]byte(fp + "\n")); e != nil || !got[fp] {
		t.Fatal("canonical ledger refused")
	}
	for _, s := range []string{fp, fp + "\r\n", fp + "\n" + fp + "\n", "not-a-fingerprint\n"} {
		if _, e := revokedFingerprints([]byte(s)); e == nil {
			t.Fatal("invalid ledger accepted")
		}
	}
}
func TestProviderConfirmedUnassignedQueueIsCapacity(t *testing.T) {
	x := fixture()
	run := fixtureRun(x)
	run.Status = "queued"
	run.Conclusion = ""
	created := time.Now().Add(-21 * time.Minute).UTC().Format(time.RFC3339)
	job := apiJob{ID: 1, RunID: 4, RunAttempt: 1, Name: "pack-windows-arm64", Status: "queued", CreatedAt: &created}
	c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/jobs") {
			return reply(200, map[string]any{"total_count": 1, "jobs": []apiJob{job}}), nil
		}
		return reply(200, run), nil
	}))
	r, e := c.Status(context.Background(), x, "status", "0.1.0-dev")
	if e != nil || r.ExitCode != 5 || r.AdmissionEligible {
		t.Fatal("provider capacity was not classified")
	}
	job.RunnerID = 123
	r, e = c.Status(context.Background(), x, "status", "0.1.0-dev")
	if e != nil || r.ExitCode != 8 {
		t.Fatal("assigned job classified as unassigned capacity")
	}
}
func TestAuthMemoryBoundNeverReturnsOverflow(t *testing.T) {
	var b boundedMemory
	if _, e := b.Write(make([]byte, 65536)); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Write([]byte("synthetic-overflow-marker")); e == nil || !b.overflow || len(b.b) != 65536 {
		t.Fatal("auth capture unbounded")
	}
}
func TestSafeToolVersionTokens(t *testing.T) {
	for _, s := range []string{"git version 2.50.1 (vendor)", "gh version 2.105.0 (date)", "node 26.8.1", "tool 1.2.3-beta.1"} {
		v := toolVersionRE.FindString(s)
		if v == "" || strings.Contains(v, "vendor") || strings.Contains(v, "date") {
			t.Fatal("version token parser")
		}
	}
	if toolVersionRE.FindString("synthetic-private-marker") != "" {
		t.Fatal("unstructured tool output accepted")
	}
}
func TestResultBindsSelectedCompositionNotRegistrationBase(t *testing.T) {
	x := fixture()
	x.CompositionSHA256 = strings.Repeat("1", 64)
	run := fixtureRun(x)
	got := ResultIdentity(x, run)
	if got.CompositionSHA256 == nil || *got.CompositionSHA256 != x.CompositionSHA256 || *got.CompositionSHA256 == x.Registration.CompositionSHA256 {
		t.Fatal("stale base composition reported")
	}
	posts := 0
	c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts++
		}
		return reply(200, run), nil
	}))
	if _, _, e := c.DispatchRun(context.Background(), x.Registration, x.Dispatch, ""); e == nil || posts != 0 {
		t.Fatal("dispatch without composition identity")
	}
}
func TestRemoteActorAuthorityIsExact(t *testing.T) {
	c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
		return reply(200, map[string]string{"login": "approved-admin"}), nil
	}))
	if c.VerifyActor(context.Background(), "approved-admin") != nil {
		t.Fatal("approved actor refused")
	}
	if c.VerifyActor(context.Background(), "other-admin") == nil || c.VerifyActor(context.Background(), "") == nil {
		t.Fatal("wrong or missing actor accepted")
	}
}
func TestProviderJSONRejectsDuplicatesAndMalformedUnicode(t *testing.T) {
	for _, body := range []string{`{"id":1,"id":2}`, `{"title":"\ud800"}`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})} {
		c, _ := New("synthetic", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
			response := reply(200, nil)
			response.Body = io.NopCloser(strings.NewReader(body))
			return response, nil
		}))
		if _, e := c.Request(context.Background(), "GET", "synthetic", nil, new(any)); e == nil {
			t.Fatal("ambiguous provider JSON accepted")
		}
	}
}

func TestPublishedKitIncludesCurrentRevocationAuthority(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docs", "revoked-signer-fingerprints")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("current kit revocation ledger missing")
	}
	if _, err := revokedFingerprints(raw); err != nil {
		t.Fatal("current kit revocation ledger invalid")
	}
	head, err := Git(context.Background(), root, 1024, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal("kit head unavailable")
	}
	blob, err := Git(context.Background(), root, 65536, "show", strings.TrimSpace(string(head))+":docs/revoked-signer-fingerprints")
	if err != nil || !bytes.Equal(raw, blob) {
		t.Fatal("revocation authority must be committed at the current kit revision")
	}
}

func TestSignatureToolsIgnoreAmbientPathOrder(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	for _, name := range []string{"git", "ssh-keygen"} {
		got, err := TrustedExecutable(name, root)
		expected := signatureTool(name)
		if machine, loadErr := config.LoadMachine(); loadErr == nil {
			if registered, ok := machine.Tools[name]; ok {
				expected = registered
			}
		}
		want, linkErr := filepath.EvalSymlinks(expected)
		if err != nil || linkErr != nil || got != want {
			t.Fatal("OS signature tool unavailable or PATH-selected")
		}
	}
	if _, err := TrustedExecutable("unregistered-tool", root); err == nil {
		t.Fatal("unregistered PATH tool accepted")
	}
}
