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

func syntheticFailureClass(result process.Result) int {
	for _, path := range []string{result.StderrPath, result.StdoutPath} {
		raw, e := readRegular(path, 1<<20)
		if e != nil {
			continue
		}
		for i, pattern := range []string{"npm error code EACCES", "npm error code EUSAGE", "npm error code EINTEGRITY", "npm error code ENOTFOUND", "npm error code ETIMEDOUT", "npm error code EBADPLATFORM", "curl: (", "sha256sum: WARNING"} {
			if bytes.Contains(raw, []byte(pattern)) {
				return i + 1
			}
		}
	}
	return 0
}

func isSyntheticCanary(cmd catalog.Command) bool {
	return len(cmd.Argv) >= 2 && cmd.Argv[0] == "node" && cmd.Argv[1] == "ci/canary.mjs"
}
