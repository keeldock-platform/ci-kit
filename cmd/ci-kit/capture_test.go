package main

import (
	"context"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSyntheticCaptureRequiresMarkerInBothStreams(t *testing.T) {
	root := t.TempDir()
	marker := []byte("synthetic-capture-positive-control")
	paths := []string{filepath.Join(root, "private-test-content.txt"), filepath.Join(root, "stdout"), filepath.Join(root, "stderr")}
	for _, p := range paths {
		if os.WriteFile(p, marker, 0600) != nil {
			t.Fatal("fixture")
		}
	}
	result := process.Result{StdoutPath: paths[1], StderrPath: paths[2]}
	if syntheticCapture(root, result) != nil {
		t.Fatal("positive capture refused")
	}
	if os.WriteFile(paths[2], []byte("missing"), 0600) != nil {
		t.Fatal("fixture")
	}
	if syntheticCapture(root, result) == nil {
		t.Fatal("absent stderr marker accepted")
	}
	if os.Remove(paths[0]) != nil {
		t.Fatal("fixture")
	}
	if syntheticCapture(root, result) == nil {
		t.Fatal("absent marker accepted")
	}
}

func TestAndroidCommandRunnerRequiresBothPrivateStreams(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node required for public tooling fixture")
	}
	for _, negative := range []bool{false, true} {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if os.Mkdir(filepath.Join(root, "ci"), 0700) != nil {
			t.Fatal("mkdir")
		}
		marker := []byte("public-tooling-fixture-control")
		if os.WriteFile(filepath.Join(root, "private-test-content.txt"), marker, 0600) != nil {
			t.Fatal("marker")
		}
		script := `import fs from 'node:fs';const marker=fs.readFileSync('private-test-content.txt');process.stdout.write(marker);`
		if !negative {
			script += `process.stderr.write(marker);`
		}
		if os.WriteFile(filepath.Join(root, "ci", "canary.mjs"), []byte(script), 0600) != nil {
			t.Fatal("script")
		}
		index, controls := 0, 0
		capture, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		state := androidState{Diagnostic: true, CaptureDirectory: capture}
		run := androidCommandRunner(state, &index, &controls)
		got := run(context.Background(), catalog.Command{Cwd: ".", Argv: []string{"node", "ci/canary.mjs"}, TimeoutSeconds: 10}, root)
		if negative {
			if got != 6 || controls != 0 {
				t.Fatal("absent stderr control accepted", got, controls)
			}
		} else if got != 0 || controls != 1 {
			t.Fatal("positive native callback refused", got, controls)
		}
	}
}
