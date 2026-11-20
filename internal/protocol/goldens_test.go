package protocol

import (
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/schemas"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClosedCommandAndExitGoldens(t *testing.T) {
	paths, e := filepath.Glob("../../tests/golden/cli/*.json")
	if e != nil || len(paths) != 20 {
		t.Fatalf("expected20JSONexamples got%d", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			var raw any
			if json.Unmarshal(b, &raw) != nil || schemas.Validate("urn:keeldock:kd-ci:result:1", raw) != nil {
				t.Fatal("invalid envelope")
			}
			var r Result
			if json.Unmarshal(b, &r) != nil || r.ValidateSemantics() != nil {
				t.Fatal("invalid semantics")
			}
			if r.Local != nil {
				if r.OK {
					cmd := strings.ReplaceAll(r.Command, "results purge", "purge")
					if schemas.Validate("urn:keeldock:kd-ci:local-data:1#/$defs/"+cmd, r.Local.Data) != nil {
						t.Fatal("invalid command data")
					}
				} else {
					b, _ := json.Marshal(r.Local.Data)
					if string(b) != "{}" {
						t.Fatal("error data")
					}
				}
			}
		})
	}
}
func TestAddedEnvelopeFieldAndUnknownMajorRefused(t *testing.T) {
	r := New("status", "0.1.0-dev", true)
	b, _ := r.Bytes()
	var v map[string]any
	json.Unmarshal(b, &v)
	v["new_private_field"] = "synthetic"
	if schemas.Validate("urn:keeldock:kd-ci:result:1", v) == nil {
		t.Fatal("unknown field accepted")
	}
	delete(v, "new_private_field")
	v["schema_version"] = 2
	if schemas.Validate("urn:keeldock:kd-ci:result:1", v) == nil {
		t.Fatal("unknown schema major accepted")
	}
	v["schema_version"] = 1
	v["state"] = "new_state"
	if schemas.Validate("urn:keeldock:kd-ci:result:1", v) == nil {
		t.Fatal("unknown state accepted")
	}
}
