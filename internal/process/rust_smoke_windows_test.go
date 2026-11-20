package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This uses only the public fixture, before any source map or credential step.
func TestHostedWindowsRustSmoke(t *testing.T) {
	if os.Getenv("CI_PUBLIC_WINDOWS_RUST_SMOKE") != "1" {
		t.Skip("source-free hosted Windows probe")
	}
	if err := checkNativeMSVCLinker(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, rel := range []string{"Cargo.toml", "Cargo.lock", "src/lib.rs"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "synthetic", "rust", rel))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "rust", rel)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cargo, err := exec.LookPath("cargo")
	if err != nil || !filepath.IsAbs(cargo) {
		t.Fatal("public Cargo executable unavailable")
	}
	result, err := Run(context.Background(), Spec{Argv: []string{cargo, "test", "--locked", "--offline"}, Root: root, Cwd: "rust", ResultDir: filepath.Join(root, "capture"), Timeout: 120 * time.Second, Env: map[string]string{"CARGO_NET_OFFLINE": "true", "RUSTUP_AUTO_INSTALL": "0"}})
	// The streams contain only a checked-in public fixture and public tool facts.
	for _, p := range []string{result.StdoutPath, result.StderrPath} {
		if p != "" {
			if raw, readErr := os.ReadFile(p); readErr == nil {
				t.Log(string(raw))
			}
		}
	}
	if err != nil || result.ExitCode != 0 || !result.CleanupComplete {
		t.Fatalf("public native Cargo failed: %v exit=%d timeout=%t", err, result.ExitCode, result.TimedOut)
	}
}

// Resolve the actual child PATH and compare it with Cargo's explicit native
// linker. This rejects GNU link even when Cargo could bypass it via its override.
func checkNativeMSVCLinker() error {
	target := map[string]string{"amd64": "X86_64", "arm64": "AARCH64"}[runtime.GOARCH]
	if target == "" {
		return fmt.Errorf("unsupported native MSVC architecture")
	}
	env, err := Environment(nil)
	if err != nil {
		return err
	}
	values := map[string]string{}
	for _, pair := range env {
		k, v, _ := strings.Cut(pair, "=")
		values[strings.ToUpper(k)] = v
	}
	want := values["CARGO_TARGET_"+target+"_PC_WINDOWS_MSVC_LINKER"]
	got, err := exec.LookPath("link.exe")
	if err != nil || want == "" || !filepath.IsAbs(want) || !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		return fmt.Errorf("link resolves outside explicit native MSVC linker: resolved=%q expected=%q error=%v", got, want, err)
	}
	return nil
}

func TestWindowsLinkerRejectsGitCoreutils(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "Git", "usr", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "link.exe"), []byte("public fake GNU link fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	target := map[string]string{"amd64": "X86_64", "arm64": "AARCH64"}[runtime.GOARCH]
	t.Setenv("CARGO_TARGET_"+target+"_PC_WINDOWS_MSVC_LINKER", filepath.Join(root, "VS", "link.exe"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := checkNativeMSVCLinker(); err == nil {
		t.Fatal("Git coreutils link was accepted")
	}
}
