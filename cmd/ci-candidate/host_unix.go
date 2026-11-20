//go:build !windows

package main

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func nativeArchitecture() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if runtime.GOOS == "darwin" {
		out, e := exec.CommandContext(ctx, "/usr/sbin/sysctl", "-n", "hw.optional.arm64").Output()
		if e != nil {
			return "", errCandidate
		}
		switch strings.TrimSpace(string(out)) {
		case "1":
			return "arm64", nil
		case "0":
			return "amd64", nil
		default:
			return "", errCandidate
		}
	}
	if runtime.GOOS == "linux" {
		out, e := exec.CommandContext(ctx, "/usr/bin/uname", "-m").Output()
		if e != nil {
			return "", errCandidate
		}
		switch strings.TrimSpace(string(out)) {
		case "x86_64":
			return "amd64", nil
		case "aarch64", "arm64":
			return "arm64", nil
		default:
			return "", errCandidate
		}
	}
	return "", errCandidate
}
