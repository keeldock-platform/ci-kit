// Package wiring validates a synthetic caller without reading credentials.
package wiring

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"gopkg.in/yaml.v3"
)

type field struct {
	Required bool   `yaml:"required"`
	Type     string `yaml:"type"`
}
type step struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	With map[string]string `yaml:"with"`
}
type job struct {
	Uses        string            `yaml:"uses"`
	Needs       any               `yaml:"needs"`
	Environment string            `yaml:"environment"`
	Secrets     map[string]string `yaml:"secrets"`
	With        map[string]string `yaml:"with"`
	Env         map[string]string `yaml:"env"`
	Steps       []step            `yaml:"steps"`
}
type workflow struct {
	On map[string]struct {
		Inputs  map[string]field `yaml:"inputs"`
		Secrets map[string]field `yaml:"secrets"`
	} `yaml:"on"`
	Jobs map[string]job `yaml:"jobs"`
}

var bad = errors.New("synthetic wiring refused")

// Check requires named forwarding, immutable bindings and source-free bootstrap.
func Check(caller, kit []byte, sha, tip string) error {
	var c, k workflow
	if yaml.Unmarshal(caller, &c) != nil || yaml.Unmarshal(kit, &k) != nil || !protocol.SHA(sha) || !protocol.SHA(tip) {
		return bad
	}
	call, ok := c.Jobs["matrix"]
	if !ok || call.Uses != "keeldock-platform/ci-kit/.github/workflows/experiment-matrix.yml@"+sha || call.With["kit_sha"] != sha {
		return bad
	}
	decl := k.On["workflow_call"]
	if len(decl.Secrets) != 2 || len(call.Secrets) != 2 {
		return bad
	}
	for _, name := range []string{"CI_SYNTHETIC_SOURCE_MAP", "SYNTHETIC_SOURCE_TOKEN"} {
		if !decl.Secrets[name].Required || call.Secrets[name] != "${{ secrets."+name+" }}" {
			return bad
		}
	}
	if len(call.With) != len(decl.Inputs) {
		return bad
	}
	for name, d := range decl.Inputs {
		if !d.Required || d.Type != "string" || call.With[name] == "" {
			return bad
		}
	}
	for name := range call.With {
		if _, ok := decl.Inputs[name]; !ok {
			return bad
		}
	}
	for _, m := range regexp.MustCompile(`inputs\.([A-Za-z_][A-Za-z0-9_-]*)`).FindAllSubmatch(kit, -1) {
		if _, ok := decl.Inputs[string(m[1])]; !ok {
			return bad
		}
	}
	for _, m := range regexp.MustCompile(`secrets\.([A-Za-z_][A-Za-z0-9_-]*)`).FindAllSubmatch(kit, -1) {
		if _, ok := decl.Secrets[string(m[1])]; !ok {
			return bad
		}
	}
	boot, ok := k.Jobs["bootstrap"]
	if !ok || boot.Environment != "" || len(boot.Secrets) != 0 || len(boot.Steps) == 0 {
		return bad
	}
	raw, _ := json.Marshal(boot)
	if strings.Contains(string(raw), "secrets.") || strings.Contains(string(raw), "SOURCE_READ_") {
		return bad
	}
	pack, ok := k.Jobs["pack"]
	if !ok || pack.Needs != "bootstrap" || pack.Environment != "ci-experiment" {
		return bad
	}
	gate, execute := -1, -1
	for i, s := range pack.Steps {
		if s.Name == "Synthetic secrets ready" {
			gate = i
			if s.Env["SOURCE_MAP"] != "${{ secrets.CI_SYNTHETIC_SOURCE_MAP }}" || s.Env["SOURCE_READ_01"] != "${{ secrets.SYNTHETIC_SOURCE_TOKEN }}" || !strings.Contains(s.Run, `test -n "$SOURCE_MAP"`) || !strings.Contains(s.Run, `test -n "$SOURCE_READ_01"`) {
				return bad
			}
		}
		if s.Name == "Execute" {
			execute = i
			if s.Env["SOURCE_MAP"] != "${{ secrets.CI_SYNTHETIC_SOURCE_MAP }}" || s.Env["SOURCE_READ_01"] != "${{ secrets.SYNTHETIC_SOURCE_TOKEN }}" {
				return bad
			}
		}
	}
	if gate < 0 || execute <= gate {
		return bad
	}
	var inputs map[string]string
	if json.Unmarshal([]byte(call.With["request"]), &inputs) != nil || inputs["source_sha"] != tip || inputs["base_sha"] != tip {
		return bad
	}
	d := protocol.Dispatch{Ref: "experiment/ci-wiring", Inputs: inputs}
	if d.ValidateExperiment() != nil {
		return bad
	}
	return nil
}
