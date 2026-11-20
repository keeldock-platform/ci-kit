// Package protocol defines the closed, public command result contract.
package protocol

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
)

type Identity struct {
	Concern           *string `json:"concern"`
	SourceSHA         *string `json:"source_sha"`
	BaseSHA           *string `json:"base_sha"`
	ControlSHA        *string `json:"control_sha"`
	KitSHA            *string `json:"kit_sha"`
	CompositionSHA256 *string `json:"composition_sha256"`
	SelectorSHA256    *string `json:"selector_sha256"`
	RequestID         *string `json:"request_id"`
	CIRepo            *string `json:"ci_repo"`
	WorkflowID        *string `json:"workflow_id"`
	RunID             *string `json:"run_id"`
	Attempt           *int    `json:"attempt"`
	URL               *string `json:"url"`
	Purpose           *string `json:"purpose"`
}
type Coverage struct {
	Expected  []string `json:"expected_platforms"`
	Completed []string `json:"completed_platforms"`
}
type Stage struct {
	Slot         string  `json:"slot"`
	Platform     *string `json:"platform"`
	State        string  `json:"state"`
	FailureClass *string `json:"failure_class"`
	StartedAt    *string `json:"started_at"`
	CompletedAt  *string `json:"completed_at"`
}
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
type Operation struct {
	DispatchAccepted bool `json:"dispatch_accepted"`
	CancelRequested  bool `json:"cancel_requested"`
	CancelConfirmed  bool `json:"cancel_confirmed"`
}
type Local struct {
	Dirty       bool     `json:"dirty"`
	Units       []string `json:"units"`
	Explanation []string `json:"explanation"`
	ChildExit   *int     `json:"child_exit"`
	ChildSignal *string  `json:"child_signal"`
	LogDir      *string  `json:"log_dir"`
	Data        any      `json:"data"`
}
type Result struct {
	SchemaVersion     int        `json:"schema_version"`
	CLIVersion        string     `json:"cli_version"`
	Command           string     `json:"command"`
	Scope             string     `json:"scope"`
	State             string     `json:"state"`
	OK                bool       `json:"ok"`
	ExitCode          int        `json:"exit_code"`
	AdmissionEligible bool       `json:"admission_eligible"`
	Identity          Identity   `json:"identity"`
	Coverage          Coverage   `json:"coverage"`
	Stages            []Stage    `json:"stages"`
	Error             *Error     `json:"error"`
	Operation         *Operation `json:"operation"`
	Local             *Local     `json:"local"`
}

func New(command, version string, hosted bool) Result {
	r := Result{SchemaVersion: 1, CLIVersion: version, Command: command, Scope: "local", State: "ready", OK: true, Coverage: Coverage{[]string{}, []string{}}, Stages: []Stage{}, Local: &Local{Units: []string{}, Explanation: []string{}, Data: map[string]any{}}}
	if hosted {
		r.Scope = "hosted"
		r.Local = nil
	}
	return r
}
func (r *Result) Fail(exit int, code string) {
	r.OK = false
	r.ExitCode = exit
	r.AdmissionEligible = false
	r.State = "blocked"
	if exit == 1 {
		r.State = "failed"
	}
	if exit == 4 {
		r.State = "deadline"
	}
	if exit == 6 {
		r.State = "unknown"
	}
	if exit == 7 {
		r.State = "cancelled"
	}
	r.Error = &Error{code, Message(code), false}
	if r.Local != nil {
		r.Local.Data = map[string]any{}
	}
}

// Message exposes fixed diagnostics only; provider/config/parser errors never cross this boundary.
func Message(code string) string {
	switch code {
	case "selection_too_large":
		return "Selection is too large; request full feedback."
	case "usage":
		return "Invalid command or arguments. See --help."
	case "configuration":
		return "Configuration or schema validation failed."
	case "offline_refused":
		return "This command requires network access."
	case "auth_refused":
		return "Existing authentication or authority could not be verified."
	case "policy_refused":
		return "The reviewed policy does not authorise this operation."
	case "deadline":
		return "The deadline expired without complete evidence."
	case "capacity":
		return "The provider confirmed no runner was acquired."
	case "check_failed":
		return "A required check or cleanup failed."
	case "interrupted":
		return "The operation was interrupted."
	case "dispatch_ambiguous":
		return "Dispatch outcome is ambiguous. Preserve the request ID; do not redispatch."
	case "cancel_attempt_drift":
		return "The run attempt changed during cancellation; the run-scoped request may have affected it."
	case "pending":
		return "The run has no final verdict."
	case "missing_evidence":
		return "Required evidence is missing or inconsistent."
	case "capture_failed":
		return "The local process could not be captured safely."
	}
	return "The operation could not be completed safely."
}

var sha = regexp.MustCompile(`^[a-f0-9]{40}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var numeric = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var uuid = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
var Platforms = []string{"linux-debian", "linux-arch", "macos-arm64", "macos-x64", "windows-x64", "windows-arm64", "ios-simulator", "android-emulator", "web-linux", "server-linux-x64", "server-linux-arm64"}
var Slots = []string{"preflight", "acquire", "plan", "setup", "checks", "cleanup", "finalizer"}

func SHA(s string) bool     { return sha.MatchString(s) }
func Digest(s string) bool  { return digest.MatchString(s) }
func Numeric(s string) bool { return numeric.MatchString(s) }
func UUID(s string) bool    { return uuid.MatchString(s) }
func Ptr[T any](v T) *T     { return &v }

// ValidateSemantics rejects combinations that cannot represent complete admission.
func (r Result) ValidateSemantics() error {
	bad := errors.New("invalid result")
	if r.ExitCode < 0 || r.ExitCode > 8 || r.OK != (r.ExitCode == 0) {
		return bad
	}
	if len(unique(r.Coverage.Expected)) != len(r.Coverage.Expected) || len(unique(r.Coverage.Completed)) != len(r.Coverage.Completed) {
		return bad
	}
	for _, p := range r.Coverage.Expected {
		if !slices.Contains(Platforms, p) {
			return bad
		}
	}
	for _, p := range r.Coverage.Completed {
		if !slices.Contains(r.Coverage.Expected, p) {
			return bad
		}
	}
	i := r.Identity
	for _, v := range []*string{i.SourceSHA, i.BaseSHA, i.ControlSHA, i.KitSHA} {
		if v != nil && !SHA(*v) {
			return bad
		}
	}
	for _, v := range []*string{i.CompositionSHA256, i.SelectorSHA256} {
		if v != nil && !Digest(*v) {
			return bad
		}
	}
	if i.RunID != nil && !Numeric(*i.RunID) || i.WorkflowID != nil && !Numeric(*i.WorkflowID) || i.RequestID != nil && !UUID(*i.RequestID) || i.Attempt != nil && *i.Attempt < 1 {
		return bad
	}
	if r.State == "passed" && (r.Error != nil || !r.OK || r.ExitCode != 0) {
		return bad
	}
	if r.Operation != nil && r.Operation.CancelConfirmed && !r.Operation.CancelRequested {
		return bad
	}
	if r.Scope == "hosted" && r.Local != nil {
		return bad
	}
	if r.Scope == "local" && (r.Local == nil || r.AdmissionEligible) {
		return bad
	}
	if r.Identity.URL != nil {
		u, e := url.Parse(*r.Identity.URL)
		if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
			return bad
		}
	}
	for _, s := range r.Stages {
		if !slices.Contains(Slots, s.Slot) || s.Platform != nil && !slices.Contains(Platforms, *s.Platform) {
			return bad
		}
	}
	if r.AdmissionEligible {
		i := r.Identity
		if r.Scope != "hosted" || r.State != "passed" || !r.OK || r.Error != nil || len(r.Coverage.Expected) == 0 || len(r.Stages) == 0 || len(r.Coverage.Completed) != len(r.Coverage.Expected) || i.Purpose == nil || *i.Purpose != "admission" {
			return bad
		}
		for _, v := range []*string{i.SourceSHA, i.BaseSHA, i.ControlSHA, i.KitSHA} {
			if v == nil || !SHA(*v) {
				return bad
			}
		}
		for _, v := range []*string{i.CompositionSHA256, i.SelectorSHA256} {
			if v == nil || !Digest(*v) {
				return bad
			}
		}
		if i.RequestID == nil || !UUID(*i.RequestID) || i.RunID == nil || !Numeric(*i.RunID) || i.WorkflowID == nil || !Numeric(*i.WorkflowID) || i.Attempt == nil || *i.Attempt < 1 || i.Concern == nil || i.CIRepo == nil || i.URL == nil {
			return bad
		}
		for _, s := range r.Stages {
			if s.State != "passed" {
				return bad
			}
		}
	}
	return nil
}
func (r Result) Bytes() ([]byte, error) { return json.Marshal(r) }

func unique(ss []string) []string {
	m := map[string]bool{}
	for _, s := range ss {
		m[s] = true
	}
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	return out
}
