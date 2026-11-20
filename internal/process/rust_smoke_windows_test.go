package process

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This uses only the public fixture, before any source map or credential step.
func TestHostedWindowsRustSmoke(t *testing.T) {
	if os.Getenv("CI_PUBLIC_WINDOWS_RUST_SMOKE") != "1" {
		t.Skip("source-free hosted Windows probe")
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
