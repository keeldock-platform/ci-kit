package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func nativeArchitecture() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return darwinNativeArchitecture(ctx, func(key string) (uint32, error) {
		return unix.SysctlUint32(key)
	}, unix.Sysctl)
}

// Apple's translation probe treats only ENOENT as native when the key is absent:
// https://developer.apple.com/documentation/apple-silicon/about-the-rosetta-translation-environment
// Integer sysctls require SysctlUint32; Sysctl returns raw bytes as a string.
func darwinNativeArchitecture(ctx context.Context, integer func(string) (uint32, error), text func(string) (string, error)) (string, error) {
	if ctx.Err() != nil {
		return "", hostCheckError("native-query")
	}
	translated, err := integer("sysctl.proc_translated")
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return "", hostCheckError("translation-query")
	}
	if err == nil {
		switch translated {
		case 0:
		case 1:
			return "", hostCheckError("translation")
		default:
			return "", hostCheckError("translation-value")
		}
	}
	if ctx.Err() != nil {
		return "", hostCheckError("native-query")
	}
	machine, err := text("hw.machine")
	if err != nil || ctx.Err() != nil {
		return "", hostCheckError("native-query")
	}
	switch strings.TrimSpace(machine) {
	case "x86_64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", hostCheckError("native-value")
	}
}
