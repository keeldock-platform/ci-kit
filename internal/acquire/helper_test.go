package acquire

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The native Git test reuses the exact test executable as a non-persisting
// credential protocol helper, avoiding a separate local binary build/install.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "get":
			if ServeCredential(os.Stdin, os.Stdout, os.Getenv) != nil {
				os.Exit(1)
			}
			os.Exit(0)
		case "store", "erase":
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestGitCredentialProtocolSupportsSpaceAndUnicodeHelperPath(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	helper := filepath.Join(root, "credential helper é")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	src, e := os.Open(executable)
	if e != nil {
		t.Fatal(e)
	}
	defer src.Close()
	dst, e := os.OpenFile(helper, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if e != nil {
		t.Fatal(e)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal("helper copy failed")
	}
	command, e := credentialHelperCommand(helper)
	if e != nil {
		t.Fatal("trusted helper path refused")
	}
	git := "/usr/bin/git"
	if runtime.GOOS == "windows" {
		git = filepath.Join(os.Getenv("ProgramFiles"), "Git", "cmd", "git.exe")
	}
	env := credentialEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, "-c", "credential.helper=", "-c", "credential.helper="+command, "-c", "credential.useHttpPath=true", "credential", "fill")
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "TEMP", "TMP", "TMPDIR"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\npath=sample-owner/sample-source.git\n\n")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		t.Fatal("native Git credential helper refused")
	}
	if !strings.Contains(out.String(), "password="+env["SOURCE_READ_01"]) {
		t.Fatal("native credential protocol did not preserve runtime marker")
	}
}
func TestGitCredentialHelperRejectsCommandTextAndRelativePaths(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "trusted")
	if runtime.GOOS == "windows" {
		root = `C:\trusted`
	}
	for _, path := range []string{"relative-helper", filepath.Join(root, "helper\"; unsafe"), filepath.Join(root, "helper$(unsafe)"), filepath.Join(root, "helper\nunsafe")} {
		if _, e := credentialHelperCommand(path); e == nil {
			t.Fatal("helper command text accepted")
		}
	}
}
