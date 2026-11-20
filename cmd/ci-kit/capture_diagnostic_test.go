package main

import (
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
	"path/filepath"
	"testing"
)

func TestSyntheticFailureClassPublishesOnlyFixedClass(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "out")
	errout := filepath.Join(root, "err")
	if e := os.WriteFile(out, []byte("arbitrary captured text"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, sample := range []struct {
		text  string
		class int
	}{{"npm error code EACCES\nprivate payload", 1}, {"npm error code EBADPLATFORM", 6}, {"curl: (22) failed", 7}, {"unclassified captured payload", 0}} {
		if e := os.WriteFile(errout, []byte(sample.text), 0600); e != nil {
			t.Fatal(e)
		}
		if got := syntheticFailureClass(process.Result{StdoutPath: out, StderrPath: errout}); got != sample.class {
			t.Fatal("wrong bounded failure class")
		}
	}
}
