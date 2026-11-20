package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keeldock-platform/ci-kit/internal/process"
)

func TestAndroidReadinessRetainsNativeFailure(t *testing.T) {
	for _, kind := range []string{"missing", "regular"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			sink := filepath.Join(root, "sink")
			if err := process.MakePrivateDir(sink); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "kvm")
			if kind == "regular" {
				if err := os.WriteFile(path, []byte("not a device"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := androidReadyAt(process.WithCaptureSink(context.Background(), sink), path)
			if err == nil {
				t.Fatal("invalid KVM accepted")
			}
			files, readErr := filepath.Glob(filepath.Join(sink, "*", "command.json"))
			if readErr != nil || len(files) != 1 {
				t.Fatal("native error capture missing", readErr)
			}
			raw, readErr := os.ReadFile(files[0])
			if readErr != nil {
				t.Fatal(readErr)
			}
			var diagnostic process.CommandDiagnostic
			if json.Unmarshal(raw, &diagnostic) != nil || diagnostic.Error != err.Error() || len(diagnostic.Argv) != 2 || diagnostic.Argv[0] != "android-kvm-readiness" || diagnostic.Result.ExitObserved {
				t.Fatal("native failure identity lost")
			}
			if kind == "missing" && !strings.Contains(diagnostic.Error, "stat:") {
				t.Fatal("stat error lost")
			}
			if kind == "regular" && !strings.Contains(diagnostic.Error, "not a device:") {
				t.Fatal("mode error lost")
			}
		})
	}
}
