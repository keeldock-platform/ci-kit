//go:build windows

package github

import (
	"encoding/json"
	"fmt"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"os"
	"testing"
)

func TestHostedSignatureAssessment(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"git", "ssh-keygen"} {
		path := signatureTool(name)
		source := "installed"
		if machine, err := config.LoadMachine(); err == nil {
			if registered, ok := machine.Tools[name]; ok {
				path = registered
				source = "registered"
			}
		}
		_, statErr := os.Stat(path)
		trusted, phase := trustedPathAssessment(path)
		_, resolveErr := TrustedExecutable(name, root)
		raw, _ := json.Marshal(map[string]any{"tool": name, "source": source, "exists": statErr == nil, "no_links": config.NoLinks(path), "trusted_path": trusted, "phase": phase, "resolved": resolveErr == nil})
		fmt.Printf("PUBLIC_SIGNATURE_DIAGNOSTIC %s\n", raw)
	}
}
