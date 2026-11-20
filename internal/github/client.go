// Package github confines provider access to explicit credentials and registered identities.
package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

type Failure struct {
	Exit int
	Code string
}

func (f *Failure) Error() string       { return f.Code }
func Fail(exit int, code string) error { return &Failure{exit, code} }

type REST interface {
	RequestWithContext(context.Context, string, string, io.Reader) (*http.Response, error)
}
type Client struct {
	REST              REST
	ArtifactTransport http.RoundTripper
	Timeout           time.Duration
	Sleep             func(context.Context, time.Duration) error
}

func EnvironmentAllowed(env []string) bool {
	for _, v := range env {
		k, x, _ := strings.Cut(v, "=")
		u := strings.ToUpper(k)
		if x == "" {
			continue
		}
		if u == "GH_HOST" && x == "github.com" {
			continue
		}
		switch u {
		case "GH_PATH", "GH_HOST", "GH_API_HOST", "GITHUB_API_URL", "GH_DEBUG", "GH_HTTP_UNIX_SOCKET", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "NODE_OPTIONS", "NODE_PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS":
			return false
		}
	}
	return true
}

// TrustedExecutable rejects checkout executables and mutable or writable tool locations.
func TrustedExecutable(name, root string) (string, error) {
	p, e := exec.LookPath(name)
	// Signature authority uses OS-installed tools, independently of mutable PATH ordering.
	if fixed := signatureTool(name); fixed != "" {
		p, e = fixed, nil
	}
	registered := false
	if machine, err := config.LoadMachine(); err == nil {
		if fixed, ok := machine.Tools[name]; ok {
			p = fixed
			e = nil
			registered = true
		}
	}
	if e != nil || !filepath.IsAbs(p) {
		return "", Fail(3, "auth_refused")
	}
	approved := []string{"/usr/bin/" + name, "/usr/local/bin/" + name, "/opt/homebrew/bin/" + name}
	if name == "gh" {
		approved = append(approved, `C:\Program Files\GitHub CLI\gh.exe`)
	}
	if name == "git" {
		approved = append(approved, `C:\Program Files\Git\cmd\git.exe`)
	}
	if name == "ssh-keygen" {
		approved = append(approved, `C:\Windows\System32\OpenSSH\ssh-keygen.exe`)
	}
	matched := registered
	for _, candidate := range approved {
		if filepath.Clean(p) == filepath.Clean(candidate) {
			matched = true
		}
	}
	if !matched {
		return "", Fail(3, "auth_refused")
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return "", Fail(3, "auth_refused")
	}
	abs, _ := filepath.Abs(root)
	if !outsideRoot(abs, p) {
		return "", Fail(3, "auth_refused")
	}
	if !config.NoLinks(p) || !trustedPath(p) {
		return "", Fail(3, "auth_refused")
	}
	return p, nil
}

type boundedMemory struct {
	mu       sync.Mutex
	b        []byte
	overflow bool
}

func (w *boundedMemory) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.b)+len(p) > 65536 {
		w.overflow = true
		return 0, errors.New("auth output limit")
	}
	w.b = append(w.b, p...)
	return len(p), nil
}
func Token(ctx context.Context, root string, timeout time.Duration) (string, error) {
	env := os.Environ()
	if !EnvironmentAllowed(env) {
		return "", Fail(3, "auth_refused")
	}
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if t := os.Getenv(key); t != "" {
			if len(t) > 65536 || strings.ContainsAny(t, "\r\n\x00") {
				return "", Fail(3, "auth_refused")
			}
			return t, nil
		}
	}
	p, e := TrustedExecutable("gh", root)
	if e != nil {
		return "", e
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, "auth", "token", "--secure-storage", "--hostname", "github.com")
	var b boundedMemory
	cmd.Stdout = &b
	cmd.Stderr = io.Discard
	cmd.Stdin = nil
	cmd.Env = []string{"GH_PROMPT_DISABLED=1", "GH_HOST=github.com", "GH_DEBUG="}
	for _, k := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "SYSTEMROOT", "TMPDIR", "TEMP", "TMP"} {
		if v := os.Getenv(k); v != "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	helperPath := filepath.Dir(p)
	if runtime.GOOS == "windows" {
		helperPath += string(os.PathListSeparator) + `C:\Windows\System32`
	} else {
		helperPath += string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin"
	}
	cmd.Env = append(cmd.Env, "PATH="+helperPath)
	cmd.WaitDelay = 2 * time.Second
	e = process.RunHelper(ctx, cmd)
	if ctx.Err() != nil {
		return "", Fail(4, "deadline")
	}
	if e != nil || b.overflow {
		return "", Fail(3, "auth_refused")
	}
	t := strings.TrimSpace(string(b.b))
	for i := range b.b {
		b.b[i] = 0
	}
	if t == "" || strings.ContainsAny(t, "\r\n\x00") {
		return "", Fail(3, "auth_refused")
	}
	return t, nil
}
func New(token string, timeout time.Duration, transport http.RoundTripper) (*Client, error) {
	if token == "" {
		return nil, Fail(3, "auth_refused")
	}
	if transport == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		tr.DisableCompression = true
		transport = tr
	}
	rest, e := api.NewRESTClient(api.ClientOptions{AuthToken: token, Host: "github.com", APIHost: "api.github.com", Transport: guardTransport{transport}, Timeout: timeout, EnableCache: false, Log: io.Discard, LogIgnoreEnv: true, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Headers: map[string]string{"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2026-03-10"}})
	if e != nil {
		return nil, Fail(3, "auth_refused")
	}
	return &Client{REST: rest, Timeout: timeout, Sleep: sleep}, nil
}

type guardTransport struct{ inner http.RoundTripper }

func (t guardTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" || r.URL.User != nil {
		return nil, Fail(3, "auth_refused")
	}
	resp, e := t.inner.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	if resp.ContentLength > 8<<20 {
		resp.Body.Close()
		return nil, Fail(6, "missing_evidence")
	}
	resp.Body = &limitedBody{ReadCloser: resp.Body, left: 8 << 20}
	return resp, nil
}

type limitedBody struct {
	io.ReadCloser
	left int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		var x [1]byte
		n, e := b.ReadCloser.Read(x[:])
		if n > 0 {
			return 0, Fail(6, "missing_evidence")
		}
		return 0, e
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, e := b.ReadCloser.Read(p)
	b.left -= int64(n)
	return n, e
}
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// TrustedToolPath verifies a fixed machine-registered executable outside the checkout.
func TrustedToolPath(p, root string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	resolved, e := filepath.EvalSymlinks(p)
	if e != nil {
		return false
	}
	if !outsideRoot(root, resolved) {
		return false
	}
	return config.NoLinks(resolved) && trustedPath(resolved)
}

// VerifyActor binds an existing login to the approved dispatcher before a remote command.
func (c *Client) VerifyActor(ctx context.Context, want string) error {
	var user struct {
		Login string `json:"login"`
	}
	if _, e := c.Request(ctx, "GET", "user", nil, &user); e != nil {
		return e
	}
	if want == "" || user.Login != want {
		return Fail(3, "auth_refused")
	}
	return nil
}

func outsideRoot(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	if e != nil {
		return runtime.GOOS == "windows" && filepath.IsAbs(root) && filepath.IsAbs(path) && !strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(path))
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func signatureTool(name string) string {
	if name != "git" && name != "ssh-keygen" {
		return ""
	}
	if runtime.GOOS == "windows" {
		if name == "git" {
			return `C:\Program Files\Git\cmd\git.exe`
		}
		return `C:\Windows\System32\OpenSSH\ssh-keygen.exe`
	}
	return "/usr/bin/" + name
}
