package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/keeldock-platform/ci-kit/internal/diagnostics"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

func TestPrivateEarlyFailureSealsBeforeReadyAndRejectsPlantedPath(t *testing.T) {
	env := validEnvironment(t)
	c, e := readControl(func(k string) string { return env[k] })
	if e != nil {
		t.Fatal("control")
	}
	base, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	work := filepath.Join(base, "work")
	if process.MakePrivateDir(work) != nil {
		t.Fatal("work")
	}
	if os.MkdirAll(filepath.Join(work, "public-control", ".github"), 0700) != nil {
		t.Fatal("public config")
	}
	identity, _ := age.GenerateX25519Identity()
	raw, _ := json.Marshal(map[string]any{"schema_version": 1, "recipient": identity.Recipient().String()})
	if os.WriteFile(filepath.Join(work, "public-control", ".github", "diagnostics.json"), raw, 0600) != nil {
		t.Fatal("config")
	}
	key := filepath.Join(work, "identity")
	f, _ := process.CreatePrivateFile(key)
	f.WriteString(identity.String())
	f.Close()
	t.Chdir(work)
	for _, planted := range []bool{false, true} {
		temp := filepath.Join(work, map[bool]string{false: "ordinary", true: "planted"}[planted])
		if process.MakePrivateDir(temp) != nil {
			t.Fatal("temp")
		}
		outputFile := filepath.Join(temp, "github-output")
		f, _ := process.CreatePrivateFile(outputFile)
		f.Close()
		t.Setenv("GITHUB_OUTPUT", outputFile)
		t.Setenv("RUNNER_TEMP", temp)
		t.Setenv("CI_RUN_ID", "123")
		t.Setenv("CI_ATTEMPT", "1")
		t.Setenv("CI_PLATFORM", "windows-x64")
		t.Setenv("CI_KIT_OPERATION", "pack")
		t.Setenv("SOURCE_MAP", "")
		artifact := filepath.Join(temp, "ci-diagnostics", "failure-windows-x64-1.age")
		if planted {
			if process.MakePrivateDir(filepath.Dir(artifact)) != nil {
				t.Fatal("out")
			}
			f, _ := process.CreatePrivateFile(artifact)
			f.WriteString("benign planted plaintext")
			f.Close()
		}
		if code := privateWork(context.Background(), c); code != 3 {
			t.Fatalf("original failure lost: %d", code)
		}
		ready, _ := os.ReadFile(outputFile)
		if planted {
			if len(ready) != 0 {
				t.Fatal("unsealed publication became ready")
			}
			continue
		}
		if string(ready) != "diagnostics=ready\n" {
			t.Fatal("sealed artifact not ready")
		}
		cipher, e := os.Open(artifact)
		if e != nil {
			t.Fatal("artifact")
		}
		bundle, e := diagnostics.Open(cipher, key)
		cipher.Close()
		if e != nil || bundle.Identity.Run != "123" || bundle.ExitCode != 3 {
			t.Fatal("early failure evidence lost")
		}
		found := false
		for _, data := range bundle.Files {
			if strings.Contains(string(data), "source map and tool resolution") {
				found = true
			}
		}
		if !found {
			t.Fatal("early failure stage missing")
		}
	}
}
