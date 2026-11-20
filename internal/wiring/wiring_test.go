package wiring

import (
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"os"
	"strings"
	"testing"
)

func TestSyntheticWiringRefusals(t *testing.T) {
	k, e := os.ReadFile("../../.github/workflows/experiment-matrix.yml")
	if e != nil {
		t.Fatal(e)
	}
	sha, tip := strings.Repeat("1", 40), strings.Repeat("2", 40)
	d, e := protocol.NewDispatch("canary", tip, tip, "full", "feedback", "all", "all", "11111111-1111-4111-8111-111111111111")
	if e != nil {
		t.Fatal(e)
	}
	request, _ := json.Marshal(d.Inputs)
	quoted, _ := json.Marshal(string(request))
	c := []byte("on:\n  workflow_dispatch:\npermissions: {}\njobs:\n  matrix:\n    uses: keeldock-platform/ci-kit/.github/workflows/experiment-matrix.yml@" + sha + "\n    with:\n      kit_sha: " + sha + "\n      experiment_policy: '${{ vars.CI_B07B_EXPERIMENT_POLICY }}'\n      request: " + string(quoted) + "\n    secrets:\n      CI_SYNTHETIC_SOURCE_MAP: ${{ secrets.CI_SYNTHETIC_SOURCE_MAP }}\n      SYNTHETIC_SOURCE_TOKEN: ${{ secrets.SYNTHETIC_SOURCE_TOKEN }}\n")
	if Check(c, k, sha, tip) != nil {
		t.Fatal("positive wiring refused")
	}
	cases := []struct {
		name string
		c, k []byte
	}{
		{"unforwarded-secret", []byte(strings.Replace(string(c), "      SYNTHETIC_SOURCE_TOKEN: ${{ secrets.SYNTHETIC_SOURCE_TOKEN }}\n", "", 1)), k},
		{"undeclared-input", c, []byte(strings.Replace(string(k), "inputs.kit_sha", "inputs.missing", 1))},
		{"secret-scope", c, []byte(strings.Replace(string(k), "  bootstrap:\n", "  bootstrap:\n    environment: ci-experiment\n", 1))},
		{"bootstrap-dependency", c, []byte(strings.Replace(string(k), "needs: bootstrap", "needs: absent", 1))},
		{"empty-check", c, []byte(strings.Replace(string(k), `test -n "$SOURCE_READ_01"`, `true`, 1))},
	}
	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			if Check(v.c, v.k, sha, tip) == nil {
				t.Fatal("unsafe wiring accepted")
			}
		})
	}
	if Check(c, k, sha, strings.Repeat("3", 40)) == nil {
		t.Fatal("stale tip accepted")
	}
}
