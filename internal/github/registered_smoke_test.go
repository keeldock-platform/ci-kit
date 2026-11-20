package github

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostedRegisteredSignature(t *testing.T) {
	if os.Getenv("CI_REGISTERED_SIGNATURE_SMOKE") != "1" {
		t.Skip("explicit public hosted smoke")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal("root")
	}
	head, err := Git(context.Background(), root, 1024, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal("head")
	}
	tip := strings.TrimSpace(string(head))
	temp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("temporary root")
	}
	fixture := filepath.Join(temp, "public-fixture")
	if _, err := Git(context.Background(), temp, 65536, "clone", "--no-hardlinks", "--no-checkout", root, fixture); err != nil {
		t.Fatal("public fixture clone")
	}
	if err := VerifySignaturesWithAuthority(context.Background(), fixture, tip, tip, SignerAuthority{root, tip}); err != nil {
		t.Fatal("public signed fixture verification with current revocation authority")
	}
}
