package github

import (
	"context"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostedRegisteredSignature(t *testing.T) {
	if os.Getenv("CI_REGISTERED_SIGNATURE_SMOKE") != "1" {
		t.Skip("explicit public hosted smoke")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal("root")
	}

	temp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("temporary root")
	}
	sink := filepath.Join(temp, "public-diagnostics")
	if process.MakePrivateDir(sink) != nil {
		t.Fatal("public capture")
	}
	ctx := process.WithCaptureSink(context.Background(), sink)
	defer func() {
		if !t.Failed() {
			return
		}
		_ = filepath.WalkDir(sink, func(path string, d os.DirEntry, e error) error {
			if e != nil || d.IsDir() {
				return e
			}
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			raw, e := io.ReadAll(io.LimitReader(f, 65537))
			f.Close()
			if e == nil {
				t.Logf("PUBLIC_GIT_DIAGNOSTIC %s %s", filepath.Base(path), raw)
			}
			return e
		})
	}()
	head, err := Git(ctx, root, 1024, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal("head")
	}
	tip := strings.TrimSpace(string(head))
	fixture := filepath.Join(temp, "public-fixture")
	if _, err := Git(ctx, temp, 65536, "clone", "--no-hardlinks", "--no-checkout", root, fixture); err != nil {
		t.Fatal("public fixture clone")
	}
	// This opt-in smoke clones only the pinned public control tree and runs
	// before source secrets. Its native verifier errors are public-tool data.
	git, err := TrustedExecutable("git", fixture)
	if err != nil {
		t.Fatal("PUBLIC_VERIFY git authority")
	}
	ssh, err := TrustedExecutable("ssh-keygen", fixture)
	if err != nil {
		t.Fatal("PUBLIC_VERIFY ssh authority")
	}
	allowed, err := Git(ctx, fixture, 1<<20, "show", tip+":docs/commit-signers")
	if err != nil {
		t.Fatal("PUBLIC_VERIFY signers blob")
	}
	allowedPath := filepath.Join(temp, "public-signers")
	f, err := process.CreatePrivateFile(allowedPath)
	if err != nil {
		t.Fatal("PUBLIC_VERIFY signers file")
	}
	_, err = f.Write(allowed)
	ce := f.Close()
	if err != nil || ce != nil {
		t.Fatal("PUBLIC_VERIFY signers write")
	}
	args := []string{git, "--no-replace-objects", "-c", "trace2.eventTarget=1", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "diff.external=", "-C", fixture, "-c", "gpg.format=ssh", "-c", "gpg.ssh.program=" + ssh, "-c", "gpg.program=false", "-c", "gpg.x509.program=false", "-c", "gpg.ssh.allowedSignersFile=" + allowedPath, "-c", "gpg.ssh.revocationFile=", "-c", "gpg.minTrustLevel=fully", "verify-commit", tip}
	result, runErr := process.Run(ctx, process.Spec{Argv: args, Root: fixture, Cwd: ".", ResultDir: filepath.Join(temp, "public-verify"), Timeout: 30 * time.Second, OutputLimit: 65536, Env: map[string]string{"PATH": filepath.Dir(git)}})
	if runErr != nil || result.ExitCode != 0 {
		for _, stream := range []struct{ name, path string }{{"STDOUT", result.StdoutPath}, {"STDERR", result.StderrPath}} {
			capture, openErr := os.Open(stream.path)
			if openErr != nil {
				t.Fatal("PUBLIC_VERIFY capture unavailable")
			}
			raw, readErr := io.ReadAll(io.LimitReader(capture, 65537))
			capture.Close()
			if readErr != nil {
				t.Fatal("PUBLIC_VERIFY capture unreadable")
			}
			if len(raw) > 65536 {
				t.Fatal("PUBLIC_VERIFY output bound")
			}
			t.Logf("PUBLIC_VERIFY_%s %s", stream.name, raw)
		}
		t.Fatalf("PUBLIC_VERIFY native result code=%d timeout=%t cleanup=%t", result.ExitCode, result.TimedOut, result.CleanupComplete)
	}
	t.Log("PUBLIC_VERIFY native PASS")
	if err := VerifySignaturesWithAuthority(ctx, fixture, tip, tip, SignerAuthority{root, tip}); err != nil {
		t.Fatal("public signed fixture verification with current revocation authority")
	}
}
