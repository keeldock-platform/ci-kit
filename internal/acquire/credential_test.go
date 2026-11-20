package acquire

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func credentialEnv(t *testing.T) map[string]string {
	t.Helper()
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		t.Fatal(e)
	}
	return map[string]string{"KD_CI_CREDENTIAL_SLOT": "SOURCE_READ_01", "KD_CI_CREDENTIAL_PATH": "sample-owner/sample-source.git", "SOURCE_READ_01": hex.EncodeToString(b)}
}
func TestCredentialProtocolProvidesOnlySelectedHostAndPath(t *testing.T) {
	env := credentialEnv(t)
	var out bytes.Buffer
	e := ServeCredential(strings.NewReader("protocol=https\nhost=github.com\npath=sample-owner/sample-source.git\n\n"), &out, func(k string) string { return env[k] })
	if e != nil || !strings.Contains(out.String(), "password="+env["SOURCE_READ_01"]) {
		t.Fatal("selected credential missing")
	}
}
func TestCredentialProtocolRejectsWrongOriginPathMalformedAndEmpty(t *testing.T) {
	for name, input := range map[string]string{"host": "protocol=https\nhost=elsewhere\npath=sample-owner/sample-source.git\n\n", "path": "protocol=https\nhost=github.com\npath=other/source.git\n\n", "http": "protocol=http\nhost=github.com\npath=sample-owner/sample-source.git\n\n", "duplicate": "protocol=https\nprotocol=http\nhost=github.com\npath=sample-owner/sample-source.git\n\n", "oversize": strings.Repeat("x", 8194)} {
		t.Run(name, func(t *testing.T) {
			env := credentialEnv(t)
			var out bytes.Buffer
			if ServeCredential(strings.NewReader(input), &out, func(k string) string { return env[k] }) == nil || out.Len() != 0 {
				t.Fatal("credential disclosed")
			}
		})
	}
}
