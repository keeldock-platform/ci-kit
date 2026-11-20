package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"regexp"
	"strings"
)

// Experiment authority is never read from the production admission variable.
// Only the separately reviewed branch entry may opt into diagnostic behaviour.
func readExperimentControl(get func(string) string) (control, error) {
	ref := get("CI_REF")
	if !regexp.MustCompile(`^refs/heads/experiment/ci-[a-z0-9][a-z0-9-]{0,63}$`).MatchString(ref) || get("CI_WORKFLOW_REF") != get("CI_REPOSITORY")+"/.github/workflows/experiment-ci.yml@"+ref {
		return control{}, refused
	}
	shim := func(k string) string {
		switch k {
		case "CI_ADMISSION_POLICY":
			return get("CI_EXPERIMENT_POLICY")
		case "CI_REF":
			return "refs/heads/main"
		case "CI_WORKFLOW_REF":
			return get("CI_REPOSITORY") + "/.github/workflows/canary-ci.yml@refs/heads/main"
		}
		return get(k)
	}
	c, err := readProductionControl(shim)
	if err != nil || len(c.Policy.Concerns) != 1 || c.Policy.AffectedQualified || c.Request.Inputs["concern"] != "canary" || c.Request.Inputs["purpose"] != "feedback" {
		return control{}, refused
	}
	c.Request.Ref = strings.TrimPrefix(ref, "refs/heads/")
	if c.Request.ValidateExperiment() != nil {
		return control{}, refused
	}
	return c, nil
}

type diagnosticKey struct{}
type diagnosticStage struct {
	Slot    int  `json:"slot"`
	Code    int  `json:"code"`
	Blocked bool `json:"blocked"`
}

func diagnostic(ctx context.Context, s diagnosticStage) {
	if report, ok := ctx.Value(diagnosticKey{}).(func(diagnosticStage)); ok {
		report(s)
	}
}
func diagnosticContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, diagnosticKey{}, func(s diagnosticStage) { b, _ := json.Marshal(s); fmt.Println(string(b)) })
}
func experimentRequest(d protocol.Dispatch) bool { return d.ValidateExperiment() == nil }
