package main

import (
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
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
