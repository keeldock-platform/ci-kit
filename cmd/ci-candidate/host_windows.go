//go:build windows

package main

import (
	"debug/pe"
	"golang.org/x/sys/windows"
)

func nativeArchitecture() (string, error) {
	var processMachine, nativeMachine uint16
	if windows.IsWow64Process2(windows.CurrentProcess(), &processMachine, &nativeMachine) != nil || processMachine != pe.IMAGE_FILE_MACHINE_UNKNOWN {
		return "", errCandidate
	}
	switch nativeMachine {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "amd64", nil
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "arm64", nil
	default:
		return "", errCandidate
	}
}
