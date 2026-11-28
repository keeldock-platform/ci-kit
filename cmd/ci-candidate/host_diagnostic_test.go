package main

import (
	"bytes"
	"errors"
	"runtime"
	"testing"
)

func TestCandidateHostPredicatesKeepFixedReasons(t *testing.T) {
	cases := []struct {
		name, os, arch, runtimeOS, runtimeArch, version, native, reason string
		err                                                             error
	}{
		{"native Intel", "darwin", "amd64", "darwin", "amd64", expectedGoVersion, "amd64", "", nil},
		{"native ARM", "darwin", "arm64", "darwin", "arm64", expectedGoVersion, "arm64", "", nil},
		{"invalid OS", "unknown", "amd64", "darwin", "amd64", expectedGoVersion, "amd64", "target", nil},
		{"invalid arch", "darwin", "unknown", "darwin", "amd64", expectedGoVersion, "amd64", "target", nil},
		{"foreign target", "darwin", "amd64", "darwin", "arm64", expectedGoVersion, "arm64", "runtime", nil},
		{"wrong toolchain", "darwin", "amd64", "darwin", "amd64", "go0.invalid", "amd64", "toolchain", nil},
		{"native mismatch", "darwin", "amd64", "darwin", "amd64", expectedGoVersion, "arm64", "native-mismatch", nil},
		{"query error", "darwin", "amd64", "darwin", "amd64", expectedGoVersion, "", "native-query", errors.New("private raw query error")},
		{"translation", "darwin", "amd64", "darwin", "amd64", expectedGoVersion, "", "translation", hostCheckError("translation")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			err := validateHostIdentity(tc.os, tc.arch, tc.runtimeOS, tc.runtimeArch, tc.version, func() (string, error) {
				called = true
				return tc.native, tc.err
			})
			if tc.reason == "" {
				if err != nil || !called {
					t.Fatalf("native host refused: %v", err)
				}
			} else if candidateFailureLine(err) != "FAIL: candidate host "+tc.reason {
				t.Fatalf("wrong reason: %v", err)
			}
			if (tc.reason == "target" || tc.reason == "runtime" || tc.reason == "toolchain") && called {
				t.Fatal("query before rejecting invalid runtime")
			}
		})
	}
}

func TestCandidateCommandPropagatesOnlyFixedHostFailures(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if candidateCommand([]string{"-host-only", "-os", "unknown", "-arch", "amd64"}, &stdout, &stderr) != 1 || stdout.Len() != 0 || stderr.String() != "FAIL: candidate host target\n" {
		t.Fatalf("actual command lost host reason: stdout %q, stderr %q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if candidateCommand([]string{"-host-only", "-os", runtime.GOOS, "-arch", runtime.GOARCH}, &stdout, &stderr) != 0 || stderr.Len() != 0 || stdout.String() != "PASS: candidate check\n" {
		t.Fatalf("actual native command failed: stdout %q, stderr %q", stdout.String(), stderr.String())
	}
	for _, err := range []error{errCandidate, errors.New("::error::raw\n##[warning]"), hostCheckError("unknown\n##[warning]")} {
		if candidateFailureLine(err) != "FAIL: candidate check" {
			t.Fatal("unapproved error text entered public output")
		}
	}
}
