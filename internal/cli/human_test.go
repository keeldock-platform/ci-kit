package cli

import (
	"bytes"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"strings"
	"testing"
)

func TestHostedHumanShowsPublicFailureMissingAndEligibility(t *testing.T) {
	var out, err bytes.Buffer
	r := protocol.New("failures", Version, true)
	r.Fail(1, "check_failed")
	r.Error.Message = "synthetic-private-marker"
	r.Coverage.Expected = []string{"windows-arm64", "linux-debian"}
	r.Stages = []protocol.Stage{{Slot: "checks", Platform: protocol.Ptr("linux-debian"), State: "failed"}, {Slot: "plan", State: "passed"}}
	a := &app{out: &out, err: &err, result: r}
	if a.write() != 1 {
		t.Fatal("wrong exit")
	}
	want := "failures: failed\nchecks linux-debian: failed\nchecks windows-arm64: missing\nadmission eligible: false\nnext: kd-ci list\n"
	if out.String() != want || strings.Contains(err.String(), "synthetic-private-marker") {
		t.Fatalf("unexpected safe human result %q", out.String())
	}
}
func TestHumanQueuedNextCommandAndLocalFailureCapture(t *testing.T) {
	var out, err bytes.Buffer
	r := protocol.New("dispatch", Version, true)
	r.State = "queued"
	r.Identity.RunID = protocol.Ptr("123")
	r.Identity.Concern = protocol.Ptr("example")
	r.Identity.Attempt = protocol.Ptr(1)
	r.Operation = &protocol.Operation{DispatchAccepted: true}
	a := &app{out: &out, err: &err, result: r}
	if a.write() != 0 {
		t.Fatal("dispatch rendering")
	}
	want := "dispatch: queued\nadmission eligible: false\ndispatch accepted: true\nnext: kd-ci wait 123 --concern example --attempt 1\n"
	if out.String() != want {
		t.Fatalf("queued human golden %q", out.String())
	}
	out.Reset()
	err.Reset()
	r = protocol.New("run", Version, false)
	r.Fail(1, "check_failed")
	r.Local.ChildExit = protocol.Ptr(9)
	r.Local.LogDir = protocol.Ptr("private results")
	a.result = r
	if a.write() != 1 || out.String() != "run: failed\nadmission eligible: false\nchild exit: 9\nlogs: \"private results\"\n" {
		t.Fatalf("local capture golden %q", out.String())
	}
}
