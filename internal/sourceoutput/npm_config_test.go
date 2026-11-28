//go:build !windows

package sourceoutput

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keeldock-platform/ci-kit/internal/process"
)

func TestNpmConfigFilesAreDistinctEmptyPrivateAndExclusive(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "private-home")
	if process.MakePrivateDir(home) != nil {
		t.Fatal("private home")
	}
	user, global, err := npmConfigFiles(home)
	if err != nil || user == global {
		t.Fatalf("user=%q global=%q err=%v", user, global, err)
	}
	for _, path := range []string{user, global} {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 {
			t.Fatalf("config file %s: info=%v err=%v", path, info, e)
		}
		data, e := os.ReadFile(path)
		if e != nil || len(data) != 0 {
			t.Fatalf("config content %s: %q err=%v", path, data, e)
		}
	}
}

func TestNpmConfigFilesRefuseCreationFailureCollisionAndSymlink(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "not-a-home")
	if os.WriteFile(file, []byte("x"), 0600) != nil {
		t.Fatal("file fixture")
	}
	if _, _, err = npmConfigFiles(file); err == nil {
		t.Fatal("accepted non-directory parent")
	}
	home := filepath.Join(root, "collision")
	if process.MakePrivateDir(home) != nil {
		t.Fatal("collision home")
	}
	user := filepath.Join(home, "npm-userconfig")
	if os.WriteFile(user, []byte("preserve"), 0600) != nil {
		t.Fatal("collision fixture")
	}
	if _, _, err = npmConfigFiles(home); err == nil {
		t.Fatal("accepted existing user config")
	}
	if got, e := os.ReadFile(user); e != nil || string(got) != "preserve" {
		t.Fatalf("collision changed: %q %v", got, e)
	}
	if _, e := os.Lstat(filepath.Join(home, "npm-globalconfig")); !os.IsNotExist(e) {
		t.Fatalf("created global after user collision: %v", e)
	}
	symlinkHome := filepath.Join(root, "symlink-home")
	if process.MakePrivateDir(symlinkHome) != nil {
		t.Fatal("symlink home")
	}
	target := filepath.Join(root, "target")
	if os.WriteFile(target, []byte("keep"), 0600) != nil {
		t.Fatal("target")
	}
	if os.Symlink(target, filepath.Join(symlinkHome, "npm-globalconfig")) != nil {
		t.Fatal("symlink fixture")
	}
	if _, _, err = npmConfigFiles(symlinkHome); err == nil {
		t.Fatal("accepted global config symlink")
	}
	if _, e := os.Lstat(filepath.Join(symlinkHome, "npm-userconfig")); !os.IsNotExist(e) {
		t.Fatalf("partial user config not removed: %v", e)
	}
	if got, e := os.ReadFile(target); e != nil || string(got) != "keep" {
		t.Fatalf("symlink target changed: %q %v", got, e)
	}
}

func TestNpmGenerateAndCIUseDistinctPrivateConfigsAndClosedEnvironment(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	captureRoot := filepath.Join(root, "captures")
	externalHome := filepath.Join(root, "external-home")
	for _, p := range []string{source, captureRoot, externalHome} {
		if process.MakePrivateDir(p) != nil {
			t.Fatalf("private fixture %s", p)
		}
	}
	if os.WriteFile(filepath.Join(externalHome, ".npmrc"), []byte("registry=https://private.invalid/\n"), 0600) != nil {
		t.Fatal("external config")
	}
	if os.WriteFile(filepath.Join(source, ".npmrc"), []byte("registry=https://public.invalid/\n"), 0600) != nil {
		t.Fatal("project config")
	}
	t.Setenv("HOME", externalHome)
	t.Setenv("NPM_CONFIG_USERCONFIG", filepath.Join(externalHome, ".npmrc"))
	t.Setenv("NPM_CONFIG_GLOBALCONFIG", filepath.Join(externalHome, ".npmrc"))
	t.Setenv("OPENSSL_CONF", filepath.Join(externalHome, ".npmrc"))
	for _, install := range []bool{false, true} {
		name := "generate"
		if install {
			name = "ci"
		}
		capture := filepath.Join(captureRoot, name)
		node := filepath.Join(root, "fake-node")
		script := "#!/bin/sh\n" +
			"printf 'ARG=%s\\n' \"$@\"\n" +
			"for arg in \"$@\"; do case \"$arg\" in --userconfig=*) p=${arg#*=}; test -f \"$p\" && test ! -s \"$p\" || exit 51; printf 'USER=%s\\n' \"$p\";; --globalconfig=*) p=${arg#*=}; test -f \"$p\" && test ! -s \"$p\" || exit 52; printf 'GLOBAL=%s\\n' \"$p\";; esac; done\n" +
			"printf 'HOME=%s\\n' \"$HOME\"\n" +
			"printf 'EXTERNAL=%s|%s|%s\\n' \"${OPENSSL_CONF-unset}\" \"${NPM_CONFIG_USERCONFIG-unset}\" \"${NPM_CONFIG_GLOBALCONFIG-unset}\"\n"
		if os.WriteFile(node, []byte(script), 0700) != nil {
			t.Fatal("fake node")
		}
		if err := npm(context.Background(), source, capture, ".", Tool{Node: node, NpmRoot: filepath.Join(root, "fake-npm")}, install); err != nil {
			out, _ := os.ReadFile(filepath.Join(capture, "stdout"))
			stderr, _ := os.ReadFile(filepath.Join(capture, "stderr"))
			t.Fatalf("npm install=%v stdout=%q stderr=%q", install, out, stderr)
		}
		out, err := os.ReadFile(filepath.Join(capture, "stdout"))
		if err != nil {
			t.Fatalf("capture stdout: %v", err)
		}
		text := string(out)
		mode := "install"
		if install {
			mode = "ci"
		}
		for _, want := range []string{"ARG=" + mode, "ARG=--ignore-scripts", "ARG=--offline=false", "ARG=--registry=https://registry.npmjs.org", "ARG=--no-audit", "ARG=--no-fund", "HOME=" + capture + "-home", "EXTERNAL=unset|unset|unset"} {
			if !strings.Contains(text, want) {
				t.Fatalf("install=%v missing %q in %s", install, want, text)
			}
		}
		lines := strings.Split(text, "\n")
		var user, global string
		for _, line := range lines {
			if strings.HasPrefix(line, "USER=") {
				user = strings.TrimPrefix(line, "USER=")
			}
			if strings.HasPrefix(line, "GLOBAL=") {
				global = strings.TrimPrefix(line, "GLOBAL=")
			}
		}
		if user == "" || global == "" || user == global || !strings.HasPrefix(user, capture+"-home/") || !strings.HasPrefix(global, capture+"-home/") {
			t.Fatalf("unsafe config args: %s", text)
		}
		for _, path := range []string{user, global} {
			info, err := os.Lstat(path)
			content, readErr := os.ReadFile(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 || readErr != nil || len(content) != 0 {
				t.Fatalf("config file not retained as private empty regular file: %s info=%v content=%q err=%v", path, info, content, err)
			}
		}
	}
}
