package main

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
)

var refused = errors.New("hosted policy refused")
var repositoryRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

const canaryConcern = "canary"

var environmentRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,95}$`)

type concernPolicy struct {
	Environment     string            `json:"environment"`
	SourceMapSHA256 string            `json:"source_map_sha256"`
	Environments    map[string]string `json:"environments,omitempty"`
}
type admissionPolicy struct {
	SchemaVersion       int                      `json:"schema_version"`
	CallerRepository    string                   `json:"caller_repository"`
	CallerRepositoryID  int64                    `json:"caller_repository_id"`
	ControlSHA          string                   `json:"control_sha"`
	KitSHA              string                   `json:"kit_sha"`
	Actors              []string                 `json:"actors"`
	Concerns            map[string]concernPolicy `json:"concerns"`
	AcquisitionReviewed bool                     `json:"acquisition_reviewed"`
	AffectedQualified   bool                     `json:"affected_qualified"`
	Incident            bool                     `json:"incident"`
}
type control struct {
	Request protocol.Dispatch
	Policy  admissionPolicy
	Concern concernPolicy
}

func readControl(get func(string) string) (control, error) {
	if get("CI_ROUTE") == "experiment" {
		return readExperimentControl(get)
	}
	if get("CI_ROUTE") != "" {
		return control{}, refused
	}
	return readProductionControl(get)
}
func readProductionControl(get func(string) string) (control, error) {
	var c control
	var fields map[string]json.RawMessage
	if config.Decode([]byte(get("CI_REQUEST")), &fields, 12288) != nil {
		return c, refused
	}
	c.Request = protocol.Dispatch{Ref: "main", Inputs: map[string]string{}}
	for k, v := range fields {
		if k == "kit_sha" || k == "environment" {
			continue
		}
		var value string
		if json.Unmarshal(v, &value) != nil {
			return c, refused
		}
		c.Request.Inputs[k] = value
	}
	if c.Request.Validate() != nil || config.Decode([]byte(get("CI_ADMISSION_POLICY")), &c.Policy, 16384) != nil {
		return c, refused
	}
	var policyKeys map[string]json.RawMessage
	if config.Decode([]byte(get("CI_ADMISSION_POLICY")), &policyKeys, 16384) != nil || len(policyKeys) != 10 {
		return c, refused
	}
	p := c.Policy
	if p.SchemaVersion != 1 || !protocol.SHA(p.ControlSHA) || !protocol.SHA(p.KitSHA) || !repositoryRE.MatchString(p.CallerRepository) || p.CallerRepositoryID <= 0 || len(p.Actors) == 0 || len(p.Actors) > 32 || len(p.Concerns) == 0 || len(p.Concerns) > 32 || p.Incident {
		return c, refused
	}
	id, e := strconv.ParseInt(get("CI_REPOSITORY_ID"), 10, 64)
	if e != nil || id != p.CallerRepositoryID || get("CI_REPOSITORY") != p.CallerRepository || get("CI_WORKFLOW_SHA") != p.ControlSHA || get("CI_KIT_SHA") != p.KitSHA {
		return c, refused
	}
	validEntrypoint := false
	canaryEntrypoint := false
	for _, path := range []string{"source-ci.yml", "canary-ci.yml", "dispatch.yml"} {
		if get("CI_WORKFLOW_REF") == p.CallerRepository+"/.github/workflows/"+path+"@refs/heads/main" {
			validEntrypoint = true
			if path == "canary-ci.yml" {
				canaryEntrypoint = true
			}
		}
	}
	if !validEntrypoint || canaryEntrypoint && c.Request.Inputs["concern"] != canaryConcern {
		return c, refused
	}
	if get("CI_EVENT_NAME") != "workflow_dispatch" || get("CI_REF") != "refs/heads/main" || get("CI_REF_TYPE") != "branch" || get("CI_FORK") != "false" || !slices.Contains(p.Actors, get("CI_ACTOR")) || !slices.Contains(p.Actors, get("CI_TRIGGERING_ACTOR")) {
		return c, refused
	}
	if get("RUNNER_DEBUG") != "" || strings.EqualFold(get("ACTIONS_RUNNER_DEBUG"), "true") || strings.EqualFold(get("ACTIONS_STEP_DEBUG"), "true") {
		return c, refused
	}
	var rawConcerns map[string]map[string]json.RawMessage
	if json.Unmarshal(policyKeys["concerns"], &rawConcerns) != nil {
		return c, refused
	}
	for alias, v := range p.Concerns {
		if !config.Alias(alias) || !environmentRE.MatchString(v.Environment) || !protocol.Digest(v.SourceMapSHA256) {
			return c, refused
		}
		_, explicit := rawConcerns[alias]["environments"]
		if alias == canaryConcern && !explicit {
			return c, refused
		}
		if explicit {
			if len(v.Environments) != len(protocol.Platforms)+1 {
				return c, refused
			}
			seen := map[string]bool{}
			for _, slot := range append([]string{"plan"}, protocol.Platforms...) {
				value := v.Environments[slot]
				if !environmentRE.MatchString(value) || seen[value] {
					return c, refused
				}
				seen[value] = true
			}
		}
	}
	var ok bool
	c.Concern, ok = p.Concerns[c.Request.Inputs["concern"]]
	if !ok {
		return c, refused
	}
	if get("CI_KIT_OPERATION") != "collect" && !p.AcquisitionReviewed {
		return c, refused
	}
	return c, nil
}
func output(values map[string]string) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return refused
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return refused
	}
	defer f.Close()
	for k, v := range values {
		if !environmentRE.MatchString(k) || strings.ContainsAny(v, "\r\n") {
			return refused
		}
		if _, e = f.WriteString(k + "=" + v + "\n"); e != nil {
			return refused
		}
	}
	return nil
}

func (p concernPolicy) environment(slot string) string {
	if len(p.Environments) > 0 {
		return p.Environments[slot]
	}
	return p.Environment
}
