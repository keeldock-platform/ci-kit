package github

import (
	"context"
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type RegisteredRepository struct {
	Repository   string `json:"repository"`
	ID           int64  `json:"id"`
	TargetBranch string `json:"target_branch"`
}
type RegisteredCaller struct {
	Repository   string `json:"repository"`
	ID           int64  `json:"id"`
	WorkflowID   string `json:"workflow_id"`
	WorkflowPath string `json:"workflow_path"`
	Actor        string `json:"actor"`
}

// Registration is reviewed private authority read only from the current admitted base.
type Registration struct {
	SchemaVersion     int                  `json:"schema_version"`
	Concern           string               `json:"concern"`
	Source            RegisteredRepository `json:"source"`
	Caller            RegisteredCaller     `json:"caller"`
	ControlSHA        string               `json:"control_sha"`
	KitSHA            string               `json:"kit_sha"`
	KitAPI            int                  `json:"kit_api"`
	Qualified         bool                 `json:"qualified"`
	Incident          bool                 `json:"incident"`
	CapacityAvailable bool                 `json:"capacity_available"`
	Platforms         []string             `json:"platforms"`
	CompositionSHA256 string               `json:"composition_sha256"`
	SourceMapSHA256   string               `json:"source_map_sha256"`
	SigningPolicy     string               `json:"signing_policy"`
	Slots             []string             `json:"slots"`
}

var repoRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func (r Registration) Validate() error {
	if r.SchemaVersion != 1 || r.KitAPI != 1 || !config.Alias(r.Concern) || !repoRE.MatchString(r.Source.Repository) || !repoRE.MatchString(r.Caller.Repository) || r.Source.ID <= 0 || r.Caller.ID <= 0 || !protocol.Numeric(r.Caller.WorkflowID) || !slices.Contains([]string{".github/workflows/dispatch.yml", ".github/workflows/source-ci.yml", ".github/workflows/canary-ci.yml"}, r.Caller.WorkflowPath) || !config.Alias(strings.ToLower(r.Caller.Actor)) || !protocol.SHA(r.ControlSHA) || !protocol.SHA(r.KitSHA) || !protocol.Digest(r.CompositionSHA256) || !protocol.Digest(r.SourceMapSHA256) || r.SigningPolicy != "ssh-v1" || len(r.Platforms) == 0 || len(r.Slots) == 0 || r.Source.TargetBranch == "" || strings.ContainsAny(r.Source.TargetBranch, " ~^:?*[\\\x00\r\n") || strings.Contains(r.Source.TargetBranch, "..") {
		return Fail(3, "policy_refused")
	}
	seen := map[string]bool{}
	for _, p := range r.Platforms {
		if !slices.Contains(protocol.Platforms, p) || seen[p] {
			return Fail(3, "policy_refused")
		}
		seen[p] = true
	}
	seen = map[string]bool{}
	for _, s := range r.Slots {
		if !slices.Contains(protocol.Slots, s) || s == "finalizer" || seen[s] {
			return Fail(3, "policy_refused")
		}
		seen[s] = true
	}
	return nil
}
func RegistrationAt(ctx context.Context, root, base string) (Registration, error) {
	var r Registration
	if !protocol.SHA(base) {
		return r, Fail(2, "usage")
	}
	b, e := Git(ctx, root, 16<<10, "show", base+":ci/registration.json")
	if e != nil || config.Decode(b, &r, 16<<10) != nil || r.Validate() != nil {
		return r, Fail(3, "policy_refused")
	}
	return r, nil
}

type repoMetadata struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Fork          bool   `json:"fork"`
	Private       bool   `json:"private"`
}
type commitMetadata struct {
	SHA    string `json:"sha"`
	Commit struct {
		Verification struct {
			Verified bool `json:"verified"`
		} `json:"verification"`
	} `json:"commit"`
}

// Preflight compares immutable registration with live provider identity before any write.
func (c *Client) Preflight(ctx context.Context, r Registration, root, source, base string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if r.Validate() != nil || r.Incident || !r.CapacityAvailable {
		return Fail(3, "policy_refused")
	}
	var actor struct {
		Login string `json:"login"`
	}
	if _, e := c.Request(ctx, "GET", "user", nil, &actor); e != nil {
		return e
	}
	if actor.Login != r.Caller.Actor {
		return Fail(3, "auth_refused")
	}
	for _, want := range []RegisteredRepository{r.Source, {Repository: r.Caller.Repository, ID: r.Caller.ID, TargetBranch: "main"}} {
		var meta repoMetadata
		if _, e := c.Request(ctx, "GET", "repos/"+want.Repository, nil, &meta); e != nil {
			return e
		}
		if meta.ID != want.ID || meta.FullName != want.Repository || meta.Fork || want.Repository == r.Caller.Repository && meta.DefaultBranch != "main" || want.Repository == r.Source.Repository && !meta.Private || want.Repository == r.Caller.Repository && meta.Private {
			return Fail(3, "policy_refused")
		}
	}
	var target struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if _, e := c.Request(ctx, "GET", "repos/"+r.Source.Repository+"/git/ref/heads/"+url.PathEscape(r.Source.TargetBranch), nil, &target); e != nil {
		return e
	}
	if target.Object.SHA != base {
		return Fail(3, "policy_refused")
	}
	var commit commitMetadata
	if _, e := c.Request(ctx, "GET", "repos/"+r.Source.Repository+"/commits/"+source, nil, &commit); e != nil {
		return e
	}
	if commit.SHA != source {
		return Fail(3, "policy_refused")
	}
	if _, e := Git(ctx, root, 1024, "merge-base", "--is-ancestor", base, source); e != nil {
		return Fail(3, "policy_refused")
	}
	if e := VerifySignatures(ctx, root, source, base); e != nil {
		return e
	}
	target = struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}{}
	if _, e := c.Request(ctx, "GET", "repos/"+r.Caller.Repository+"/git/ref/heads/main", nil, &target); e != nil {
		return e
	}
	if target.Object.SHA != r.ControlSHA {
		return Fail(3, "policy_refused")
	}
	var workflow struct {
		ID          int64 `json:"id"`
		Path, State string
	}
	if _, e := c.Request(ctx, "GET", "repos/"+r.Caller.Repository+"/actions/workflows/"+r.Caller.WorkflowID, nil, &workflow); e != nil {
		return e
	}
	if workflow.Path != r.Caller.WorkflowPath || workflow.State != "active" || strconv.FormatInt(workflow.ID, 10) != r.Caller.WorkflowID {
		return Fail(3, "policy_refused")
	}
	// Owner-maintained provider policy is outside the candidate source authority.
	var variable struct {
		Value string `json:"value"`
	}
	if _, e := c.Request(ctx, "GET", "repos/"+r.Caller.Repository+"/actions/variables/CI_ADMISSION_POLICY", nil, &variable); e != nil {
		return e
	}
	var policy AdmissionPolicy
	if config.Decode([]byte(variable.Value), &policy, 1<<20) != nil || policy.Validate(r) != nil {
		return Fail(3, "policy_refused")
	}
	// Any existing active workflow could consume capacity; a same-source tuple is never duplicated.
	for _, status := range []string{"queued", "in_progress", "waiting", "requested", "pending"} {
		for page := 1; page <= 10; page++ {
			var list struct {
				Total int   `json:"total_count"`
				Runs  []Run `json:"workflow_runs"`
			}
			path := "repos/" + r.Caller.Repository + "/actions/workflows/" + r.Caller.WorkflowID + "/runs?per_page=100&page=" + itoa(page) + "&status=" + status
			if _, e := c.Request(ctx, "GET", path, nil, &list); e != nil {
				return e
			}
			for _, run := range list.Runs {
				if strings.HasPrefix(run.DisplayTitle, "kd/"+r.Concern+"/") {
					return Fail(3, "policy_refused")
				}
			}
			if len(list.Runs) < 100 {
				break
			}
			if page == 10 {
				return Fail(6, "missing_evidence")
			}
		}
	}
	return nil
}
func MarshalRegistration(r Registration) ([]byte, error) { return json.Marshal(r) }

type ConcernPolicy struct {
	Environment     string            `json:"environment"`
	SourceMapSHA256 string            `json:"source_map_sha256"`
	Environments    map[string]string `json:"environments,omitempty"`
}
type AdmissionPolicy struct {
	SchemaVersion       int                      `json:"schema_version"`
	CallerRepository    string                   `json:"caller_repository"`
	CallerRepositoryID  int64                    `json:"caller_repository_id"`
	ControlSHA          string                   `json:"control_sha"`
	KitSHA              string                   `json:"kit_sha"`
	Actors              []string                 `json:"actors"`
	Concerns            map[string]ConcernPolicy `json:"concerns"`
	AcquisitionReviewed bool                     `json:"acquisition_reviewed"`
	AffectedQualified   bool                     `json:"affected_qualified"`
	Incident            bool                     `json:"incident"`
}

func (p AdmissionPolicy) Validate(r Registration) error {
	concern, ok := p.Concerns[r.Concern]
	if len(concern.Environments) > 0 {
		if len(concern.Environments) != len(protocol.Platforms)+1 {
			return Fail(3, "policy_refused")
		}
		for _, slot := range append([]string{"plan"}, protocol.Platforms...) {
			env, exists := concern.Environments[slot]
			if !exists || !regexp.MustCompile(`^[a-z][a-z0-9-]{0,95}$`).MatchString(env) {
				return Fail(3, "policy_refused")
			}
		}
	}
	if !ok || p.SchemaVersion != 1 || p.CallerRepository != r.Caller.Repository || p.CallerRepositoryID != r.Caller.ID || p.ControlSHA != r.ControlSHA || p.KitSHA != r.KitSHA || !slices.Contains(p.Actors, r.Caller.Actor) || !p.AcquisitionReviewed || p.Incident || concern.SourceMapSHA256 != r.SourceMapSHA256 || !config.Alias(concern.Environment) || r.Qualified && !p.AffectedQualified {
		return Fail(3, "policy_refused")
	}
	return nil
}
