package process

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func physicalTemp(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func fixtureSpec(t *testing.T, mode string, args ...string) Spec {
	t.Helper()
	root := physicalTemp(t)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	return Spec{Argv: append([]string{exe, "-test.run=^TestProcessFixture$", "--", mode}, args...), Root: root, Cwd: ".", ResultDir: filepath.Join(root, "capture"), Timeout: 3 * time.Second, Grace: 50 * time.Millisecond, ReapTimeout: time.Second}
}
func TestProcessFixture(t *testing.T) {
	idx := -1
	for i, a := range os.Args {
		if a == "--" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	args := os.Args[idx+1:]
	if len(args) == 0 {
		os.Exit(90)
	}
	switch args[0] {
	case "ok":
		fmt.Print(args[1])
		fmt.Fprint(os.Stderr, args[1])
		os.Exit(0)
	case "exit":
		os.Exit(23)
	case "crash":
		panic("synthetic fixture crash")
	case "overflow":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 65536))
		os.Exit(0)
	case "hang":
		if os.WriteFile("fixture-hang-ready", nil, 0600) != nil {
			os.Exit(96)
		}
		for {
			time.Sleep(time.Hour)
		}
	case "env":
		for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "ACTIONS_RUNTIME_TOKEN", "GITHUB_OUTPUT", "GITHUB_ENV", "GITHUB_STEP_SUMMARY", "NODE_OPTIONS", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "SSH_AUTH_SOCK", "GIT_CONFIG_COUNT", "GH_DEBUG"} {
			if os.Getenv(key) != "" {
				os.Exit(91)
			}
		}
		os.Exit(0)
	case "tree":
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessFixture$", "--", "grandchild")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if e := cmd.Start(); e != nil {
			os.Exit(92)
		}
		for {
			if _, e := os.Stat("fixture-ready"); e == nil {
				break
			}
			runtime.Gosched()
		}
		fmt.Fprintln(os.Stdout, cmd.Process.Pid)
		os.Exit(0)
	case "grandchild":
		fmt.Fprintln(os.Stderr, "child-started")
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessFixture$", "--", "hang")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if e := cmd.Start(); e != nil {
			os.Exit(94)
		}
		if e := os.WriteFile("fixture-ready", nil, 0600); e != nil {
			os.Exit(95)
		}
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(93)
	}
	os.Exit(0)
}
func TestCapturePrivateBeforeLaunchAndExactBytes(t *testing.T) {
	marker := make([]byte, 32)
	if _, e := rand.Read(marker); e != nil {
		t.Fatal(e)
	}
	value := hex.EncodeToString(marker) + " é spaces\n::error::fixture"
	s := fixtureSpec(t, "ok", value)
	r, e := Run(context.Background(), s)
	if e != nil || r.ExitCode != 0 || !r.CleanupComplete {
		t.Fatalf("capture failed: %+v %v", r, e)
	}
	for _, p := range []string{r.StdoutPath, r.StderrPath} {
		b, e := os.ReadFile(p)
		if e != nil || string(b) != value {
			t.Fatal("captured bytes mismatch")
		}
		st, e := os.Stat(p)
		if e != nil {
			t.Fatal(e)
		}
		if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
			t.Fatal("nonprivate output")
		}
	}
	if _, e := os.Stat(filepath.Join(s.ResultDir, "active")); !os.IsNotExist(e) {
		t.Fatal("active capture retained")
	}
}
func TestOriginalExitAndCrashRemainFailures(t *testing.T) {
	for _, mode := range []string{"exit", "crash"} {
		t.Run(mode, func(t *testing.T) {
			r, e := Run(context.Background(), fixtureSpec(t, mode))
			if e == nil || r.ExitCode == 0 || !r.CleanupComplete {
				t.Fatalf("failure lost: %+v %v", r, e)
			}
			if !r.ExitObserved || r.Interrupted {
				t.Fatal("first actual child exit cause lost")
			}
			if mode == "exit" && r.ExitCode != 23 {
				t.Fatal("original exit lost")
			}
		})
	}
}
func TestTimeoutKillsAndReaps(t *testing.T) {
	s := fixtureSpec(t, "hang")
	s.Timeout = 100 * time.Millisecond
	started := time.Now()
	r, e := Run(context.Background(), s)
	if e == nil || !r.TimedOut || !r.CleanupComplete || time.Since(started) > 4*time.Second {
		t.Fatalf("timeout cleanup failed: %+v %v", r, e)
	}
}
func TestOutputOverflowIsBoundedFailure(t *testing.T) {
	s := fixtureSpec(t, "overflow")
	s.OutputLimit = 1024
	r, e := Run(context.Background(), s)
	if e == nil || !r.Overflow || !r.CleanupComplete {
		t.Fatalf("overflow lost: %+v %v", r, e)
	}
	st, e := os.Stat(r.StdoutPath)
	if e != nil || st.Size() != 1024 {
		t.Fatal("output limit not enforced")
	}
}
func TestCredentialAndRunnerChannelsAreScrubbed(t *testing.T) {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "ACTIONS_RUNTIME_TOKEN", "GITHUB_OUTPUT", "GITHUB_ENV", "GITHUB_STEP_SUMMARY", "NODE_OPTIONS", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "SSH_AUTH_SOCK", "GIT_CONFIG_COUNT", "GH_DEBUG"} {
		t.Setenv(key, "synthetic")
	}
	r, e := Run(context.Background(), fixtureSpec(t, "env"))
	if e != nil || r.ExitCode != 0 {
		t.Fatalf("environment leak: %+v %v", r, e)
	}
	if _, e := Environment(map[string]string{"GH_TOKEN": "synthetic"}); e == nil {
		t.Fatal("authority override accepted")
	}
}
func TestGrandchildrenAndInheritedOutputAreCleaned(t *testing.T) {
	s := fixtureSpec(t, "tree")
	r, e := Run(context.Background(), s)
	if e != nil || r.ExitCode != 0 || !r.CleanupComplete {
		t.Fatalf("descendant cleanup failed: %+v %v", r, e)
	}
}
func TestCancellationRemainsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, e := Run(ctx, fixtureSpec(t, "hang"))
	if e == nil || r.ExitCode == 0 {
		t.Fatal("cancellation passed")
	}
}
func TestUnsafeCwdAndShellWrappersRefused(t *testing.T) {
	s := fixtureSpec(t, "ok", "x")
	for _, cwd := range []string{"../outside", "a/../b", filepath.Dir(s.Root)} {
		s.Cwd = cwd
		if _, e := Run(context.Background(), s); e == nil {
			t.Fatal("unsafe cwd accepted")
		}
	}
	s.Cwd = "."
	s.Argv[0] = filepath.Join(s.Root, "tool.cmd")
	if _, e := Run(context.Background(), s); e == nil {
		t.Fatal("batch wrapper accepted")
	}
}
func TestSymlinkCwdRejected(t *testing.T) {
	root := physicalTemp(t)
	target := physicalTemp(t)
	if e := os.Symlink(target, filepath.Join(root, "linked")); e != nil {
		t.Fatal(e)
	}
	if _, e := SafePath(root, "linked"); e == nil {
		t.Fatal("symlink accepted")
	}
}
func TestPurgeRetainsUnknownAndActive(t *testing.T) {
	root := physicalTemp(t)
	old := time.Now().Add(-48 * time.Hour)
	for _, name := range []string{"old", "active", "unknown"} {
		p := filepath.Join(root, name)
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
		for _, file := range []string{"stdout", "stderr"} {
			f := filepath.Join(p, file)
			os.WriteFile(f, []byte("x"), 0600)
			os.Chtimes(f, old, old)
		}
	}
	os.WriteFile(filepath.Join(root, "active", "active"), nil, 0600)
	os.WriteFile(filepath.Join(root, "unknown", "other"), nil, 0600)
	deleted, e := Purge(root, 24*time.Hour, time.Now())
	if e != nil || strings.Join(deleted, ",") != "old" {
		t.Fatalf("unsafe purge: %v %v", deleted, e)
	}
}

func TestMetadataEnvironmentAcceptsOnlyFixedControlValues(t *testing.T) {
	fixed := map[string]string{"RUSTUP_AUTO_INSTALL": "0", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_TERMINAL_PROMPT": "0", "NX_DAEMON": "false", "NX_CACHE_PROJECT_GRAPH": "false", "NX_NO_CLOUD": "true", "NX_SKIP_NX_CACHE": "true", "NX_INTERACTIVE": "false", "NX_NATIVE_COMMAND_RUNNER": "false", "NX_LOAD_DOT_ENV_FILES": "false"}
	if _, e := Environment(fixed); e != nil {
		t.Fatal("trusted fixed metadata controls refused")
	}
	for _, extra := range []map[string]string{{"RUSTUP_AUTO_INSTALL": "1"}, {"NODE_PATH": "source/hooks"}, {"NX_PROJECTS_PATH": "source/hooks"}, {"NX_NO_CLOUD": "false"}, {"GIT_CONFIG_GLOBAL": "source/config"}} {
		if _, e := Environment(extra); e == nil {
			t.Fatal("unreviewed metadata authority accepted")
		}
	}
}

func TestActiveCancellationRetainsItsFirstCause(t *testing.T) {
	s := fixtureSpec(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, e := Run(ctx, s); done <- outcome{r, e} }()
	ready := filepath.Join(s.Root, "fixture-hang-ready")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, e := os.Stat(ready); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture failed to become active")
		}
		runtime.Gosched()
	}
	cancel()
	o := <-done
	if o.err == nil || !o.result.Interrupted || o.result.TimedOut || o.result.ExitObserved || !o.result.CleanupComplete {
		t.Fatalf("first cancellation cause lost: %+v %v", o.result, o.err)
	}
}
