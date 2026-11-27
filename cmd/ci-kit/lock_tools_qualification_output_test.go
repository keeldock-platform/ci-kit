package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadQualificationCaptureBoundsAndTruncation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "capture")
	if err := os.WriteFile(p, []byte(strings.Repeat("x", 9000)), 0600); err != nil {
		t.Fatal(err)
	}
	b, truncated, err := readQualificationCapture(p)
	if err != nil || !truncated || len(b) != 8192 {
		t.Fatalf("len=%d truncated=%v err=%v", len(b), truncated, err)
	}
	if err := os.WriteFile(p, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	b, truncated, err = readQualificationCapture(p)
	if err != nil || truncated || string(b) != "short" {
		t.Fatalf("data=%q truncated=%v err=%v", b, truncated, err)
	}
}

func TestReadQualificationCaptureRefusesMissingSymlinkAndDirectory(t *testing.T) {
	dir := t.TempDir()
	for name, path := range map[string]string{"missing": filepath.Join(dir, "missing"), "directory": dir} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := readQualificationCapture(path); err == nil {
				t.Fatal("accepted unsafe path")
			}
		})
	}
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readQualificationCapture(link); err == nil {
		t.Fatal("accepted symlink")
	}
}

func TestQualificationFailureLineEscapesControlAndWorkflowCommands(t *testing.T) {
	line := qualificationFailureLine("stderr", []byte("警告 bad\n::warning::modern ##[warning]legacy ##[error]bad ##[save-state]x\r\x1b[31m"), false)
	if strings.ContainsAny(line, "\n\r\x1b") || strings.Contains(line, "##[") || !strings.Contains(line, "警告") || !strings.Contains(line, "::warning::modern") || !strings.Contains(line, `\u0023#[warning]legacy`) || !strings.Contains(line, `\u0023#[save-state]x`) {
		t.Fatalf("unsafe/unreadable rendering: %q", line)
	}
	if got := neutralizeLegacyCommands("oops ##[error]injected"); strings.Contains(got, "##[") || !strings.Contains(got, `\u0023#[error]`) {
		t.Fatalf("unsafe error rendering: %q", got)
	}
}
