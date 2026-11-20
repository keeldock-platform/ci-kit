package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func localFixture(t *testing.T) (string, string, string) {
	t.Helper()
	packageDir, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	packageDir, e = filepath.EvalSymlinks(packageDir)
	if e != nil {
		t.Fatal(e)
	}
	id, e := protocol.NewUUID()
	if e != nil {
		t.Fatal(e)
	}
	helperDir := filepath.Join(packageDir, ".test-helper-"+id)
	if e = process.MakePrivateDir(helperDir); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(helperDir) })
	helper := filepath.Join(helperDir, "fixture-helper")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	current, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	src, e := os.Open(current)
	if e != nil {
		t.Fatal(e)
	}
	dst, e := process.CreatePrivateFile(helper)
	if e != nil {
		src.Close()
		t.Fatal(e)
	}
	_, e = io.Copy(dst, src)
	src.Close()
	ce := dst.Close()
	if e != nil || ce != nil {
		t.Fatal("helper copy")
	}
	if runtime.GOOS != "windows" {
		if os.Chmod(helper, 0700) != nil {
			t.Fatal("helper mode")
		}
	}
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "source")
	if os.Mkdir(root, 0700) != nil {
		t.Fatal("source mkdir")
	}
	home := filepath.Join(parent, "home")
	os.MkdirAll(home, 0700)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("APPDATA", filepath.Join(home, "config"))
	git := "/usr/bin/git"
	if runtime.GOOS == "windows" {
		git = filepath.Join(os.Getenv("ProgramFiles"), "Git", "cmd", "git.exe")
	}
	if !filepath.IsAbs(git) {
		t.Fatal("native Git registry path")
	}
	policy := config.Machine{SchemaVersion: 1, LocalPolicy: "developer-machine", AllowedLocalConcerns: []string{}, SourceRoots: map[string]string{}, Tools: map[string]string{"git": git, "fixture-helper": helper}, PlannerDirectory: ""}
	userDir, e := os.UserConfigDir()
	if e != nil {
		t.Fatal(e)
	}
	policyDir := filepath.Join(userDir, "KeelDock")
	if os.MkdirAll(policyDir, 0700) != nil {
		t.Fatal("policy mkdir")
	}
	writePrivateJSON(t, filepath.Join(policyDir, "kd-ci-machine.json"), policy)
	git, e = gh.TrustedExecutable("git", root)
	if e != nil {
		t.Fatal("registered native Git was refused")
	}

	platform := ""
	for _, p := range protocol.Platforms {
		if hostSupports(p) {
			platform = p
			break
		}
	}
	if platform == "" {
		t.Fatal("supported native venue mapping is required")
	}
	check := catalog.Check{Census: []string{}, Paths: []string{"sample.txt"}, TestPaths: []string{}, Inputs: []string{}, Platforms: []string{platform}, Commands: map[string][]catalog.Command{"default": {{Cwd: ".", Argv: []string{"fixture-helper", "-test.run=^TestCLILocalHelper$", "--", "emit"}, TimeoutSeconds: 10}}}, Needs: []string{}, DependsOn: []string{}, Variants: []catalog.Variant{}, Assertions: []string{}, Reports: []string{}}
	unit := catalog.Unit{Schema: 1, ID: "sample", Owner: "test-maintainers", Paths: []string{"sample.txt"}, TestPaths: []string{}, Inputs: []string{}, Platforms: []string{platform}, RequiredIn: []string{"affected", "full", "candidate"}, TimeoutSeconds: 30, Resources: catalog.Resources{Weight: 1}, Needs: []string{}, DependsOn: []string{}, OptionalContracts: []string{}, Cache: catalog.Cache{Policy: "none"}, Assertions: []string{}, Outputs: catalog.Outputs{Visibility: "private-runner-only"}, Pack: "workspace", CostClass: "normal", Checks: map[string]catalog.Check{"format": check}}
	os.Mkdir(filepath.Join(root, "ci"), 0700)
	writePrivateJSON(t, filepath.Join(root, "ci-unit.json"), unit)
	writePrivateJSON(t, filepath.Join(root, "ci", "units.json"), map[string]any{"schema": 1, "units": []any{}, "packs": map[string]any{}, "aliases": map[string][]string{"all-format": {"sample"}}})
	writePrivateJSON(t, filepath.Join(root, ".kd-ci.json"), map[string]any{"schema_version": 1, "concern": "synthetic", "kit_api": 1, "unit_roots": []string{"."}, "shared_units": "ci/units.json", "local_policy": "developer-machine", "results_dir": ".ci-local/results", "retention_days": 7})
	os.WriteFile(filepath.Join(root, "sample.txt"), []byte("sample\n"), 0600)
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".ci-local/\n"), 0600)
	gitFixture(t, git, root, "init", "-b", "main")
	gitFixture(t, git, root, "add", ".")
	gitFixture(t, git, root, "-c", "user.name=Test Maintainer", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--no-gpg-sign", "-m", "Add synthetic fixture")
	sha := strings.TrimSpace(gitFixture(t, git, root, "rev-parse", "HEAD"))
	t.Chdir(root)
	return root, platform, sha
}
func writePrivateJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	f, e := process.CreatePrivateFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write(b); e != nil {
		t.Fatal(e)
	}
	if f.Close() != nil {
		t.Fatal("close")
	}
}
func gitFixture(t *testing.T, git, root string, args ...string) string {
	t.Helper()
	argv := append([]string{"-c", "core.hooksPath=" + os.DevNull, "-C", root}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, argv...)
	cmd.Env = []string{"PATH=" + filepath.Dir(git), "HOME=" + os.Getenv("HOME"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal("synthetic Git setup failed")
	}
	return string(out)
}
func TestLocalFeedbackCommandCaptureAndAliases(t *testing.T) {
	root, platform, sha := localFixture(t)
	code, out, err := invoke(t, "run", "all-format", "--offline", "--json")
	if code != 0 {
		t.Fatalf("local feedback code%d %s %s", code, out, err)
	}
	if strings.Contains(out, "synthetic-private-marker") || strings.Contains(err, "synthetic-private-marker") {
		t.Fatal("private output escaped")
	}
	var result protocol.Result
	if json.Unmarshal([]byte(out), &result) != nil || result.Local == nil || result.Local.LogDir == nil || result.AdmissionEligible {
		t.Fatal("invalid local result")
	}
	data, e := os.ReadFile(filepath.Join(*result.Local.LogDir, "0000-0000", "stdout"))
	if e != nil || !bytes.Contains(data, []byte("synthetic-private-marker")) {
		t.Fatal("private output was not captured")
	}
	for _, args := range [][]string{{"doctor", "--offline", "--json"}, {"list", "--json"}, {"explain", "--base", sha, "--sha", sha, "--unit", "all-format", "--json"}, {"results", "purge", "--older-than", "24h", "--json"}} {
		code, out, err = invoke(t, args...)
		if code != 0 {
			t.Fatalf("local %s command failed%d %s %s", args[0], code, out, err)
		}
	}
	os.WriteFile(filepath.Join(root, "sample.txt"), []byte("changed\n"), 0600)
	code, _, _ = invoke(t, "run", "sample-format@"+platform, "--quick", "--base", sha, "--sha", sha, "--json")
	if code != 2 {
		t.Fatal("dirty quick checkout accepted")
	}
}

// TestCLILocalHelper runs only as an explicitly selected synthetic captured child.
func TestCLILocalHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) && os.Args[i+1] == "emit" {
			os.Stdout.Write([]byte("synthetic-private-marker"))
			os.Exit(0)
		}
	}
}
