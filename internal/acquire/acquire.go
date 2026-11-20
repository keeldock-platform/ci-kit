package acquire

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type LockedSource struct{ Alias, SHA, BaseSHA string }
type Request struct {
	Map                              SourceMap
	RootSHA, BaseSHA                 string
	Workspace, Git, CredentialHelper string
	// These callbacks are trusted control-plane readers, not candidate hooks.
	VerifyRepository func(context.Context, Source) error
	VerifySignatures func(context.Context, string, string, string) error
	ReadComposition  func(context.Context, string) ([]LockedSource, error)
}
type Checkout struct {
	Root      string
	Sources   map[string]string
	ownedRoot string
}

var ErrCleanup = errors.New("source acquisition cleanup incomplete")

// Acquire acquires the root before reading its immutable composition. All trees
// and raw captures are removed on any failure. Close is mandatory on success.
func Acquire(ctx context.Context, r Request) (checkout *Checkout, ret error) {
	if !shaRE.MatchString(r.RootSHA) || !shaRE.MatchString(r.BaseSHA) || !filepath.IsAbs(r.Workspace) || !filepath.IsAbs(r.Git) || !filepath.IsAbs(r.CredentialHelper) || r.VerifyRepository == nil || r.VerifySignatures == nil || r.ReadComposition == nil {
		return nil, ErrRefused
	}
	if _, e := credentialHelperCommand(r.CredentialHelper); e != nil {
		return nil, ErrRefused
	}
	// Revalidate typed maps as well as environment-decoded maps.
	encoded, e := json.Marshal(r.Map)
	if e != nil {
		return nil, ErrRefused
	}
	validated, e := ParseMap(encoded, r.Map.Concern)
	if e != nil {
		return nil, e
	}
	r.Map = validated
	byAlias := map[string]Source{}
	for _, s := range r.Map.Sources {
		if !aliasRE.MatchString(s.Alias) || !nameRE.MatchString(s.Owner) || !nameRE.MatchString(s.Repository) || !slotRE.MatchString(s.SecretSlot) || !validBranch(s.Branch) || s.RepositoryID <= 0 {
			return nil, ErrRefused
		}
		if _, ok := byAlias[s.Alias]; ok {
			return nil, ErrRefused
		}
		byAlias[s.Alias] = s
	}
	root, ok := byAlias[r.Map.Root]
	if !ok || root.SecretSlot != "SOURCE_READ_01" {
		return nil, ErrRefused
	}
	if debugEnabled(os.Getenv("ACTIONS_RUNNER_DEBUG")) || debugEnabled(os.Getenv("ACTIONS_STEP_DEBUG")) || debugEnabled(os.Getenv("RUNNER_DEBUG")) {
		return nil, ErrRefused
	}
	if _, e := process.SafePath(filepath.Dir(r.Workspace), "."); e != nil {
		return nil, ErrRefused
	}
	if e := process.MakePrivateDir(r.Workspace); e != nil {
		return nil, ErrRefused
	}
	checkout = &Checkout{Root: r.Workspace, Sources: map[string]string{}, ownedRoot: r.Workspace}
	success := false
	owned := checkout
	defer func() {
		if !success {
			if e := owned.Close(); e != nil {
				ret = errors.Join(ret, ErrCleanup)
			}
		}
	}()
	for _, name := range []string{"sources", "captures", "empty"} {
		if process.MakePrivateDir(filepath.Join(checkout.Root, name)) != nil {
			return nil, ErrRefused
		}
	}
	if e := checkout.fetch(ctx, r, root, r.RootSHA, r.BaseSHA); e != nil {
		return nil, ErrRefused
	}
	locked, e := r.ReadComposition(ctx, checkout.Sources[root.Alias])
	if e != nil || len(locked) > 31 {
		return nil, ErrRefused
	}
	sort.Slice(locked, func(i, j int) bool { return locked[i].Alias < locked[j].Alias })
	seen := map[string]bool{root.Alias: true}
	for _, l := range locked {
		s, ok := byAlias[l.Alias]
		if !ok || seen[l.Alias] || !shaRE.MatchString(l.SHA) || !shaRE.MatchString(l.BaseSHA) {
			return nil, ErrRefused
		}
		seen[l.Alias] = true
		if e = checkout.fetch(ctx, r, s, l.SHA, l.BaseSHA); e != nil {
			return nil, ErrRefused
		}
	}
	success = true
	return checkout, nil
}
func (c *Checkout) fetch(ctx context.Context, r Request, s Source, sha, base string) error {
	token := os.Getenv(s.SecretSlot)
	if token == "" || strings.ContainsAny(token, "\x00\r\n") || len(token) > 64<<10 {
		return ErrRefused
	}
	if e := r.VerifyRepository(ctx, s); e != nil {
		return ErrRefused
	}
	dest := filepath.Join(c.Root, "sources", s.Alias)
	if e := process.MakePrivateDir(dest); e != nil {
		return ErrRefused
	}
	empty := filepath.Join(c.Root, "empty")
	captureRoot := filepath.Join(c.Root, "captures", s.Alias)
	if process.MakePrivateDir(captureRoot) != nil {
		return ErrRefused
	}
	authority := map[string]string{"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": "", "GIT_SSH_COMMAND": "", "GIT_LFS_SKIP_SMUDGE": "1", "KD_CI_CREDENTIAL_SLOT": s.SecretSlot, "KD_CI_CREDENTIAL_PATH": s.Owner + "/" + s.Repository + ".git", s.SecretSlot: token}
	helper, e := credentialHelperCommand(r.CredentialHelper)
	if e != nil {
		return ErrRefused
	}
	sequence := 0
	run := func(args ...string) (string, error) {
		sequence++
		capture := filepath.Join(captureRoot, smallInt(sequence))
		prefix := []string{r.Git, "-c", "credential.helper=", "-c", "credential.helper=" + helper, "-c", "credential.useHttpPath=true", "-c", "http.extraHeader=", "-c", "http.followRedirects=false", "-c", "http.proxy=", "-c", "core.hooksPath=" + empty, "-c", "core.fsmonitor=false", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "filter.lfs.smudge=", "-c", "filter.lfs.required=false"}
		result, e := process.RunAcquisition(ctx, process.Spec{Argv: append(prefix, args...), Root: dest, Cwd: ".", ResultDir: capture, Timeout: 2 * time.Minute}, authority)
		if e != nil || result.ExitCode != 0 {
			return "", ErrRefused
		}
		data, e := os.ReadFile(result.StdoutPath)
		if e != nil || len(data) > 1<<20 {
			return "", ErrRefused
		}
		return strings.TrimSpace(string(data)), nil
	}
	if _, e := run("init", "--quiet", "--template="+empty, "."); e != nil {
		return e
	}
	remote := "https://github.com/" + s.Owner + "/" + s.Repository + ".git"
	if _, e := run("remote", "add", "origin", remote); e != nil {
		return e
	}
	if _, e := run("fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--depth=1024", "origin", sha, base, "refs/heads/"+s.Branch+":refs/remotes/origin/admitted"); e != nil {
		return e
	}
	if tip, e := run("rev-parse", "refs/remotes/origin/admitted"); e != nil || tip != base {
		return ErrRefused
	}
	if got, e := run("remote", "get-url", "origin"); e != nil || got != remote {
		return ErrRefused
	}
	if got, e := run("rev-parse", sha+"^{commit}"); e != nil || got != sha {
		return ErrRefused
	}
	if _, e := run("merge-base", "--is-ancestor", base, sha); e != nil {
		return ErrRefused
	}
	if e := r.VerifySignatures(ctx, dest, sha, base); e != nil {
		return ErrRefused
	}
	if _, e := run("-c", "advice.detachedHead=false", "checkout", "--quiet", "--detach", sha); e != nil {
		return e
	}
	if got, e := run("rev-parse", "HEAD"); e != nil || got != sha {
		return ErrRefused
	}
	c.Sources[s.Alias] = dest
	return nil
}
func (c *Checkout) Close() error {
	if c == nil {
		return nil
	}
	if c.Root == "" && c.ownedRoot == "" {
		return nil
	}
	if c.ownedRoot == "" || c.Root != c.ownedRoot {
		return ErrRefused
	}
	if _, e := process.SafePath(c.Root, "."); e != nil {
		return ErrRefused
	}
	if e := os.RemoveAll(c.Root); e != nil {
		return ErrRefused
	}
	c.Root = ""
	c.ownedRoot = ""
	c.Sources = nil
	return nil
}
func smallInt(n int) string {
	if n == 0 {
		return "0"
	}
	b := [20]byte{}
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func debugEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false", "0":
		return false
	default:
		return true
	}
}
