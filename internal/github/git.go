package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Git runs a fixed read-only argv under a credential-free environment and bounded output.
func Git(ctx context.Context, root string, limit int, args ...string) ([]byte, error) {
	p, e := TrustedExecutable("git", root)
	if e != nil {
		return nil, e
	}
	if limit < 1 || limit > 16<<20 {
		return nil, Fail(3, "policy_refused")
	}
	dir, e := os.MkdirTemp("", "kd-ci-git-")
	if e != nil {
		return nil, Fail(3, "policy_refused")
	}
	defer os.RemoveAll(dir)
	dir, e = filepath.EvalSymlinks(dir)
	if e != nil {
		return nil, Fail(3, "policy_refused")
	}
	argv := append([]string{p, "--no-replace-objects", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "diff.external=", "-C", root}, args...)
	res, e := process.Run(ctx, process.Spec{Root: root, Cwd: ".", Argv: argv, ResultDir: filepath.Join(dir, "capture"), Timeout: 30 * time.Second, OutputLimit: int64(limit), Env: map[string]string{"PATH": filepath.Dir(p)}})
	if e != nil || res.ExitCode != 0 {
		return nil, Fail(3, "policy_refused")
	}
	f, e := os.Open(res.StdoutPath)
	if e != nil {
		return nil, Fail(3, "policy_refused")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if e != nil || len(b) > limit {
		return nil, Fail(3, "policy_refused")
	}
	return b, nil
}

type capped struct {
	bytes.Buffer
	limit int
	over  bool
}

func (c *capped) Write(b []byte) (int, error) {
	if c.Len()+len(b) > c.limit {
		c.over = true
		return 0, io.ErrShortBuffer
	}
	return c.Buffer.Write(b)
}
func decodeContent(v, encoding string) ([]byte, error) {
	if encoding != "base64" || len(v) > 2<<20 {
		return nil, Fail(6, "missing_evidence")
	}
	b, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(v, "\n", ""))
	if e != nil {
		return nil, Fail(6, "missing_evidence")
	}
	return b, nil
}

// SignerAuthority is supplied by the independently admitted installation/control policy.
type SignerAuthority struct{ KitRoot, KitSHA string }

// VerifySignatures resolves the installed current kit, never a candidate revocation policy.
func VerifySignatures(ctx context.Context, root, source, base string) error {
	machine, e := config.LoadMachine()
	if e != nil || machine.KitRoot == "" {
		return Fail(3, "policy_refused")
	}
	registration, e := RegistrationAt(ctx, root, base)
	if e != nil {
		return e
	}
	return VerifySignaturesWithAuthority(ctx, root, source, base, SignerAuthority{machine.KitRoot, registration.KitSHA})
}

// VerifySignaturesWithAuthority consumes the canonical current kit fingerprint ledger.
func VerifySignaturesWithAuthority(ctx context.Context, root, source, base string, authority SignerAuthority) error {
	if !protocol.SHA(source) || !protocol.SHA(base) || !protocol.SHA(authority.KitSHA) || !filepath.IsAbs(authority.KitRoot) {
		return Fail(3, "policy_refused")
	}
	kit, e := filepath.EvalSymlinks(authority.KitRoot)
	if e != nil {
		return Fail(3, "policy_refused")
	}
	sourceRoot, e := filepath.EvalSymlinks(root)
	if e != nil {
		return Fail(3, "policy_refused")
	}
	if !outsideRoot(sourceRoot, kit) {
		return Fail(3, "policy_refused")
	}
	head, e := Git(ctx, kit, 1024, "rev-parse", "HEAD")
	if e != nil || strings.TrimSpace(string(head)) != authority.KitSHA {
		return Fail(3, "policy_refused")
	}
	ledger, e := Git(ctx, kit, 64<<10, "show", authority.KitSHA+":ci/revoked-signer-fingerprints")
	if e != nil {
		return e
	}
	path, e := config.SafePath(kit, "ci/revoked-signer-fingerprints")
	if e != nil {
		return Fail(3, "policy_refused")
	}
	ledgerFile, e := os.Open(path)
	if e != nil {
		return Fail(3, "policy_refused")
	}
	live, e := io.ReadAll(io.LimitReader(ledgerFile, (64<<10)+1))
	ledgerFile.Close()
	if e != nil || len(live) > 64<<10 || !bytes.Equal(live, ledger) {
		return Fail(3, "policy_refused")
	}
	revoked, e := revokedFingerprints(ledger)
	if e != nil {
		return e
	}
	allowed, e := Git(ctx, root, 1<<20, "show", base+":docs/commit-signers")
	if e != nil {
		return e
	}
	roles, e := signerRoles(allowed)
	if e != nil {
		return e
	}
	dir, e := os.MkdirTemp("", "kd-ci-signers-")
	if e != nil {
		return Fail(3, "policy_refused")
	}
	defer os.RemoveAll(dir)
	allowedPath := filepath.Join(dir, "allowed")
	f, e := process.CreatePrivateFile(allowedPath)
	if e != nil {
		return Fail(3, "policy_refused")
	}
	_, e = f.Write(allowed)
	ce := f.Close()
	if e != nil || ce != nil {
		return Fail(3, "policy_refused")
	}
	sshKeygen, e := TrustedExecutable("ssh-keygen", root)
	if e != nil {
		return e
	}
	verifyArgs := []string{"-c", "gpg.format=ssh", "-c", "gpg.ssh.program=" + sshKeygen, "-c", "gpg.program=false", "-c", "gpg.x509.program=false", "-c", "gpg.ssh.allowedSignersFile=" + allowedPath, "-c", "gpg.ssh.revocationFile=", "-c", "gpg.minTrustLevel=fully"}
	commits, e := Git(ctx, root, 1<<20, "rev-list", "--max-count=10001", base+".."+source)
	if e != nil {
		return e
	}
	ids := strings.Fields(string(commits))
	if len(ids) > 10000 {
		return Fail(3, "policy_refused")
	}
	ids = append(ids, base)
	for _, id := range ids {
		if !protocol.SHA(id) {
			return Fail(3, "policy_refused")
		}
		raw, e := Git(ctx, root, 16<<20, "cat-file", "commit", id)
		if e != nil || !sshSignatureObject(raw) {
			return Fail(3, "policy_refused")
		}
		if _, e = Git(ctx, root, 1<<20, append(append([]string{}, verifyArgs...), "verify-commit", id)...); e != nil {
			return Fail(3, "policy_refused")
		}
		format := append(append([]string{}, verifyArgs...), "log", "-1", "--format=%GF%x00%GS", id)
		signer, e := Git(ctx, root, 1<<20, format...)
		if e != nil {
			return e
		}
		fp, principal, ok := strings.Cut(strings.TrimSpace(string(signer)), "\x00")
		role, approved := roles[fp]
		if !ok || !fingerprintRE.MatchString(fp) || !approved || revoked[fp] || !role.Principals[principal] {
			return Fail(3, "policy_refused")
		}
		changed, e := Git(ctx, root, 1<<20, "diff-tree", "--root", "--no-commit-id", "--name-only", "-z", "-m", "-r", id)
		if e != nil {
			return e
		}
		sensitive := false
		for _, p := range strings.Split(string(changed), "\x00") {
			if p == "docs/commit-signers" || p == ".kd-ci.json" || p == "OWNERS" || p == "OWNERS.json" || p == "ownership.json" || strings.Contains(p, "CODEOWNERS") || strings.HasPrefix(p, "ci/") || strings.HasPrefix(p, "editions/") || p == "entitlements/policy.toml" || strings.HasSuffix(p, "ci-unit.json") {
				sensitive = true
			}
		}
		if sensitive && (!role.Person || role.Lane) {
			return Fail(3, "policy_refused")
		}
	}
	return nil
}
