package process

import (
	"os"
	"strings"
	"testing"
)

func TestGitTraceTargetsOnlyCapturedStderr(t *testing.T) {
	t.Setenv("GIT_TRACE2_EVENT", "/untrusted/output")
	env, err := Environment(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range env {
		if strings.HasPrefix(pair, "GIT_TRACE2_EVENT=") {
			t.Fatal("ambient trace inherited")
		}
	}
	env, err = Environment(map[string]string{"GIT_TRACE2_EVENT": "1"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, pair := range env {
		found = found || pair == "GIT_TRACE2_EVENT=1"
	}
	if !found {
		t.Fatal("captured stderr trace missing")
	}
	for _, value := range []string{"0", "2", "9", "/tmp/trace", `C:\trace`, "af_unix:stream:/tmp/socket", os.DevNull} {
		if _, err := Environment(map[string]string{"GIT_TRACE2_EVENT": value}); err == nil {
			t.Fatalf("trace target admitted: %q", value)
		}
	}
}
