package protocol

import (
	"encoding/json"
	"fmt"
	"github.com/keeldock-platform/ci-kit/schemas"
	"strings"
	"testing"
)

func TestDispatchRoundTripOpaqueAndClosed(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	d, e := NewDispatch("example", strings.Repeat("a", 40), strings.Repeat("b", 40), "unit", "feedback", "windows-arm64", "v1:"+strings.Repeat("c", 64)+":0,2", id)
	if e != nil {
		t.Fatal(e)
	}
	if d.Validate() != nil || len(d.Inputs) != 10 || len(d.RunName()) > 160 {
		t.Fatal("bad request")
	}
	b, _ := json.Marshal(d)
	if strings.Contains(string(b), "private-feature") {
		t.Fatal("disclosure")
	}
	d.Inputs["extra"] = "x"
	if d.Validate() == nil {
		t.Fatal("extra accepted")
	}
}
func TestDispatchRejectsInvalidSelections(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	tests := []struct{ mode, purpose, platforms, selector string }{{"quick", "admission", "all", "all"}, {"full", "admission", "windows-arm64", "all"}, {"affected", "feedback", "windows-arm64,linux-debian", "all"}, {"unit", "feedback", "all", "v1:" + strings.Repeat("c", 64) + ":2,1"}, {"unit", "feedback", "all", "v1:" + strings.Repeat("c", 64) + ":00"}}
	for i, x := range tests {
		if _, e := NewDispatch("example", strings.Repeat("a", 40), strings.Repeat("b", 40), x.mode, x.purpose, x.platforms, x.selector, id); e == nil {
			t.Fatalf("case%d accepted", i)
		}
	}
	if _, e := NewDispatch(strings.Repeat("a", 96), strings.Repeat("a", 40), strings.Repeat("b", 40), "full", "feedback", "all", "all", id); e == nil {
		t.Fatal("long run name accepted")
	}
}
func TestAllExitEnvelopesValidate(t *testing.T) {
	for code := 0; code <= 8; code++ {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			r := New("status", "0.1.0-dev", true)
			if code != 0 {
				r.Fail(code, map[int]string{1: "check_failed", 2: "usage", 3: "auth_refused", 4: "deadline", 5: "capacity", 6: "missing_evidence", 7: "interrupted", 8: "pending"}[code])
			}
			b, _ := r.Bytes()
			var v any
			json.Unmarshal(b, &v)
			if e := schemas.Validate("urn:keeldock:kd-ci:result:1", v); e != nil {
				t.Fatal(e)
			}
			if e := r.ValidateSemantics(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestEligibilityRequiresExactCompleteEvidence(t *testing.T) {
	r := New("status", "0.1.0-dev", true)
	r.State = "passed"
	r.AdmissionEligible = true
	r.Coverage = Coverage{[]string{"windows-arm64", "linux-debian"}, []string{"windows-arm64", "windows-arm64"}}
	if r.ValidateSemantics() == nil {
		t.Fatal("duplicate substituted coverage accepted")
	}
	r.Coverage = Coverage{[]string{"windows-arm64"}, []string{"windows-arm64"}}
	if r.ValidateSemantics() == nil {
		t.Fatal("empty stages and tuple accepted")
	}
}
func TestURLAndPlatformSemantics(t *testing.T) {
	for _, url := range []string{"https://example.invalid/run", "http://github.com/run", "https://attacker@github.com/run"} {
		r := New("status", "0.1.0-dev", true)
		r.Identity.URL = Ptr(url)
		if r.ValidateSemantics() == nil {
			t.Fatal("unsafe url")
		}
	}
	r := New("status", "0.1.0-dev", true)
	r.Stages = []Stage{{Slot: "private-name", State: "failed"}}
	if r.ValidateSemantics() == nil {
		t.Fatal("unknown slot accepted")
	}
}
func FuzzDispatch(f *testing.F) {
	f.Add("example", "all", "all")
	f.Fuzz(func(t *testing.T, concern, platforms, selector string) {
		_, _ = NewDispatch(concern, strings.Repeat("a", 40), strings.Repeat("b", 40), "unit", "feedback", platforms, selector, "11111111-1111-4111-8111-111111111111")
	})
}
func TestNativeReusableJobNamesClosed(t *testing.T) {
	for _, name := range []string{"preflight", "plan", "collect", "validate / preflight", "validate / plan", "validate / collect", "pack-windows-arm64", "pack-windows-arm64 / pack-windows-arm64", "validate / pack-windows-arm64", "validate / pack-windows-arm64 / pack-windows-arm64"} {
		if _, _, ok := ParseJobName(name); !ok {
			t.Fatalf("fixed name refused%s", name)
		}
	}
	for _, name := range []string{"private-prefix / plan", "validate / private-test", "pack-linux-debian / pack-windows-arm64", "validate / plan / collect", "validate / pack-linux-debian / pack-windows-arm64", "validate / validate / collect"} {
		if _, _, ok := ParseJobName(name); ok {
			t.Fatalf("unknown chain accepted%s", name)
		}
	}
}
