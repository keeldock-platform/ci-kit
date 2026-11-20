package main

import (
	"bytes"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"path/filepath"
)

// The synthetic observer proves private marker bytes reached both captured streams.
// It emits no marker, path, digest, size or raw command output.
func syntheticCapture(source string, result process.Result) error {
	marker, e := readRegular(filepath.Join(source, "private-test-content.txt"), 16384)
	if e != nil || len(marker) == 0 {
		return refused
	}
	for _, p := range []string{result.StdoutPath, result.StderrPath} {
		out, e := readRegular(p, 1<<20)
		if e != nil || !bytes.Contains(out, marker) {
			return refused
		}
	}
	return nil
}

func isSyntheticCanary(cmd catalog.Command) bool {
	return len(cmd.Argv) >= 2 && cmd.Argv[0] == "node" && cmd.Argv[1] == "ci/canary.mjs"
}
