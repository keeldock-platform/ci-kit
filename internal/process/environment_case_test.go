package process

import (
	"strings"
	"testing"
)

func TestWindowsExplicitPathOverridesInheritedCase(t *testing.T) {
	t.Setenv("Path", "inherited-path")
	t.Setenv("PATH", "other-inherited-path")
	env, e := environmentFor(map[string]string{"PATH": "trusted-path"}, true)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, pair := range env {
		key, value, _ := strings.Cut(pair, "=")
		if strings.EqualFold(key, "PATH") {
			count++
			if value != "trusted-path" {
				t.Fatal("inherited PATH replaced explicit value")
			}
		}
	}
	if count != 1 {
		t.Fatal("case-folded environment contains duplicate PATH")
	}
}

func TestMSVCBuildEnvironmentSurvivesOnlyOnWindows(t *testing.T) {
	values := map[string]string{
		"LIB": `C:\SDK\Lib`, "LIBPATH": `C:\VS\Lib`, "INCLUDE": `C:\SDK\Include`,
		"CARGO_TARGET_X86_64_PC_WINDOWS_MSVC_LINKER":  `C:\VS\bin\Hostx64\x64\link.exe`,
		"CARGO_TARGET_AARCH64_PC_WINDOWS_MSVC_LINKER": `C:\VS\bin\Hostarm64\arm64\link.exe`,
	}
	for k, v := range values {
		t.Setenv(k, v)
	}
	for _, k := range []string{"SOURCE_READ_01", "GITHUB_OUTPUT", "ACTIONS_RUNTIME_TOKEN", "RUSTFLAGS", "RUSTC_WRAPPER", "CARGO_TARGET_UNREVIEWED_LINKER"} {
		t.Setenv(k, "must-disappear")
	}
	env, err := environmentFor(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, pair := range env {
		k, v, _ := strings.Cut(pair, "=")
		found[strings.ToUpper(k)] = v
		if v == "must-disappear" {
			t.Fatal("authority or build hook inherited", k)
		}
	}
	for k, v := range values {
		if found[k] != v {
			t.Fatal("MSVC setup lost", k)
		}
	}
	if _, err := environmentFor(values, true); err != nil {
		t.Fatal("explicit MSVC values refused", err)
	}
	for k := range values {
		if _, err := environmentFor(map[string]string{k: values[k]}, false); err == nil {
			t.Fatal("Windows input admitted on another OS", k)
		}
	}
	for _, k := range []string{"SOURCE_READ_01", "GITHUB_OUTPUT", "ACTIONS_RUNTIME_TOKEN", "RUSTFLAGS", "RUSTC_WRAPPER", "CARGO_TARGET_UNREVIEWED_LINKER"} {
		if _, err := environmentFor(map[string]string{k: "value"}, true); err == nil {
			t.Fatal("unreviewed input accepted", k)
		}
	}
}
