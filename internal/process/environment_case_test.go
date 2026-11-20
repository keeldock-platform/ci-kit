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
