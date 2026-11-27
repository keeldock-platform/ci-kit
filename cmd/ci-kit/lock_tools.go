package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
)

// Source-free bootstrap companion only. There is no source acquisition, secret
// mapping, arbitrary executable, output path, version or command input here.
func lockTools(ctx context.Context, mode string) (code int) {
	if !lockToolsModeAllowed(mode, runtime.GOOS, runtime.GOARCH, os.Getenv) {
		return 3
	}
	stageMode := mode
	if mode == "qualify-candidate" {
		stageMode = "qualify"
	}
	parent := os.Getenv("RUNNER_TEMP")
	if !filepath.IsAbs(parent) {
		return 3
	}
	if _, e := process.SafePath(parent, "."); e != nil {
		return 3
	}
	wd, e := os.Getwd()
	if e != nil {
		return 6
	}
	root := filepath.Join(wd, "public-control")
	if _, e := process.SafePath(root, "."); e != nil {
		return 3
	}
	private, e := os.MkdirTemp(parent, "ci-lock-stage-")
	if e != nil {
		return 6
	}
	defer func() {
		if os.RemoveAll(private) != nil && code == 0 {
			code = 6
		}
	}()
	result, e := process.Run(ctx, process.Spec{Argv: []string{"/usr/bin/python3", "-I", "-B", filepath.Join(root, "packs", "linux-debian", "lock-source-tools", "stage.py"), stageMode, private, parent}, Root: root, Cwd: ".", ResultDir: filepath.Join(private, "capture"), Timeout: 10 * time.Minute, OutputLimit: 1 << 20, Env: map[string]string{"HOME": private, "USERPROFILE": private, "PATH": "/usr/bin:/bin", "CI": "true"}})
	if !result.CleanupComplete {
		if mode == "qualify" || mode == "qualify-candidate" {
			emitQualificationFailure(result)
		}
		return 6
	}
	if result.TimedOut {
		if mode == "qualify" || mode == "qualify-candidate" {
			emitQualificationFailure(result)
		}
		return 4
	}
	if state := contextExit(ctx); state != 0 {
		return state
	}
	if e != nil || result.ExitCode != 0 || result.Overflow {
		if mode == "qualify" || mode == "qualify-candidate" {
			emitQualificationFailure(result)
		}
		return 6
	}
	if mode == "qualify" || mode == "qualify-candidate" {
		raw, e := readRegular(filepath.Join(private, "qualification.json"), 65536)
		if e != nil {
			if isQualificationMode(mode) {
				reportQualificationError("qualification-record-read", e)
			}
			return 6
		}
		if e = writeQualificationRecord(parent, mode, raw, os.Getenv, func(p string) (receiptFile, error) { return process.CreatePrivateFile(p) }, os.Remove); e != nil {
			if isQualificationMode(mode) {
				reportQualificationError("qualification-record-write", e)
			}
			return 6
		}
	}
	return 0
}

// Captures are private temporary files owned by process.Run. Render only the
// bounded source-free qualification streams; stage output stays private.
func emitQualificationFailure(r process.Result) {
	for _, item := range []struct{ name, path string }{{"stdout", r.StdoutPath}, {"stderr", r.StderrPath}} {
		b, truncated, e := readQualificationCapture(item.path)
		if e == nil {
			fmt.Println(qualificationFailureLine(item.name, b, truncated))
		}
	}
}

func isQualificationMode(mode string) bool { return mode == "qualify" || mode == "qualify-candidate" }
func reportQualificationError(phase string, err error) {
	fmt.Printf("qualification phase=%s error=%q\n", phase, neutralizeLegacyCommands(err.Error()))
}

func neutralizeLegacyCommands(s string) string { return strings.ReplaceAll(s, "##[", `\u0023#[`) }

func readQualificationCapture(path string) ([]byte, bool, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, false, os.ErrInvalid
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() {
		return nil, false, os.ErrInvalid
	}
	b := make([]byte, 8193)
	n, err := io.ReadFull(f, b)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, false, err
	}
	return b[:min(n, 8192)], n > 8192, nil
}

func qualificationFailureLine(label string, data []byte, truncated bool) string {
	safe := make([]rune, 0, len(data))
	for _, c := range string(data) {
		if c >= 32 && c != 127 {
			safe = append(safe, c)
		} else {
			safe = append(safe, ' ')
		}
	}
	return fmt.Sprintf("qualification %s (truncated=%t): %s", label, truncated, neutralizeLegacyCommands(string(safe)))
}

func lockToolsModeAllowed(mode, goos, goarch string, getenv func(string) string) bool {
	if (mode != "qualify" && mode != "qualify-candidate" && mode != "stage") || goos != "linux" || goarch != "amd64" || getenv("CI") != "true" || !protocol.SHA(getenv("CI_KIT_SHA")) {
		return false
	}
	if mode == "qualify" {
		return getenv("GITHUB_REPOSITORY") == "keeldock-platform/ci-kit" && getenv("GITHUB_EVENT_NAME") == "workflow_dispatch" && getenv("GITHUB_REF") == "refs/heads/main" && protocol.Numeric(getenv("GITHUB_RUN_ID")) && protocol.Numeric(getenv("GITHUB_RUN_ATTEMPT"))
	}
	if mode == "qualify-candidate" {
		return getenv("GITHUB_REPOSITORY") == "keeldock-platform/ci-kit" && getenv("GITHUB_EVENT_NAME") == "workflow_dispatch" && getenv("GITHUB_REF") == "refs/heads/ci/action-lock-generation-1127" && protocol.Numeric(getenv("GITHUB_RUN_ID")) && protocol.Numeric(getenv("GITHUB_RUN_ATTEMPT")) && getenv("CI_PLATFORM") == "linux-debian" && getenv("CI_KIT_OPERATION") == "lock-qualification"
	}
	return getenv("CI_PLATFORM") == "linux-debian" && getenv("CI_KIT_OPERATION") == "pack"
}
func candidateQualificationAllowed(getenv func(string) string) bool {
	return lockToolsModeAllowed("qualify-candidate", runtime.GOOS, runtime.GOARCH, getenv)
}

type receiptFile interface {
	Write([]byte) (int, error)
	Close() error
}

func writeQualificationRecord(parent, mode string, raw []byte, getenv func(string) string, create func(string) (receiptFile, error), remove func(string) error) error {
	var receipt map[string]any
	if json.Unmarshal(raw, &receipt) != nil {
		return os.ErrInvalid
	}
	if mode == "qualify-candidate" {
		receipt["qualification_class"] = "candidate-only"
		receipt["approved"] = false
	}
	receipt["provider_binding"] = map[string]string{"run_id": getenv("GITHUB_RUN_ID"), "attempt": getenv("GITHUB_RUN_ATTEMPT"), "kit_sha": getenv("CI_KIT_SHA"), "repository": getenv("GITHUB_REPOSITORY")}
	encoded, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(parent, qualificationRecordName(mode))
	f, err := create(path)
	if err != nil {
		return err
	}
	n, writeErr := f.Write(append(encoded, '\n'))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil || n != len(encoded)+1 {
		removeErr := remove(path)
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		if removeErr != nil {
			return removeErr
		}
		return os.ErrInvalid
	}
	return nil
}
func qualificationRecordName(mode string) string {
	if mode == "qualify-candidate" {
		return "ci-lock-tools-candidate-qualification.json"
	}
	return "ci-lock-tools-qualification.json"
}
