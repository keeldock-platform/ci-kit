// Package process captures trusted commands without publishing their output.
// It is process ownership and accidental-output hygiene, not a sandbox.
package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid captured process specification")
	ErrCapture  = errors.New("captured process failed")
	ErrOverflow = errors.New("captured output limit exceeded")
	ErrCleanup  = errors.New("owned process cleanup incomplete")
)

const MaxOutput int64 = 64 << 20

type Spec struct {
	Argv                        []string
	Root, Cwd, ResultDir        string
	Env                         map[string]string
	Timeout, Grace, ReapTimeout time.Duration
	OutputLimit                 int64
}
type Result struct {
	ExitCode                  int
	Interrupted, ExitObserved bool
	Signal                    string
	TimedOut, Overflow        bool
	StdoutPath, StderrPath    string
	Started, Ended            time.Time
	CleanupComplete           bool
}

// SafePath rejects traversal and every symlink component, including the root.
// Callers must retain exclusive ownership of the tree against concurrent mutation.
func SafePath(root, rel string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.IsAbs(rel) {
		return "", ErrInvalid
	}
	if rel == "" {
		rel = "."
	}
	clean := filepath.Clean(rel)
	if clean != rel || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", ErrInvalid
	}
	target := filepath.Join(root, clean)
	for p := target; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", ErrInvalid
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return target, nil
}

// Environment admits only explicit, ordinary tool/build variables. Inherited
// credentials, workflow channels, preload/debug hooks and agent sockets disappear.
func Environment(extra map[string]string) ([]string, error) {
	return environmentFor(extra, runtime.GOOS == "windows")
}
func environmentFor(extra map[string]string, windows bool) ([]string, error) {
	key := func(k string) string {
		if windows {
			return strings.ToUpper(k)
		}
		return k
	}
	allowed := map[string]bool{"PATH": true, "HOME": true, "USERPROFILE": true, "SYSTEMROOT": true, "WINDIR": true, "TMP": true, "TEMP": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "TZ": true, "CARGO_HOME": true, "RUSTUP_HOME": true, "CARGO_TARGET_DIR": true, "GOCACHE": true, "GOMODCACHE": true, "GOPATH": true, "JAVA_HOME": true, "ANDROID_HOME": true, "ANDROID_SDK_ROOT": true, "DEVELOPER_DIR": true, "SDKROOT": true, "CI": true, "NO_COLOR": true, "TERM": true}
	// MSVC setup values are ordinary build inputs, scoped to Windows. Keep
	// linker selection explicit so Git's coreutils link cannot shadow MSVC.
	if windows {
		for _, name := range []string{"LIB", "LIBPATH", "INCLUDE", "CARGO_TARGET_X86_64_PC_WINDOWS_MSVC_LINKER", "CARGO_TARGET_AARCH64_PC_WINDOWS_MSVC_LINKER"} {
			allowed[name] = true
		}
	}
	env := map[string]string{"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_TERMINAL_PROMPT": "0"}
	for _, pair := range os.Environ() {
		k, v, _ := strings.Cut(pair, "=")
		if allowed[strings.ToUpper(k)] {
			env[key(k)] = v
		}
	}
	fixed := map[string]string{"CARGO_NET_OFFLINE": "true", "RUSTUP_AUTO_INSTALL": "0", "npm_config_offline": "true", "npm_config_update_notifier": "false", "NPM_CONFIG_AUDIT": "false", "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD": "1", "NX_DAEMON": "false", "NX_CACHE_PROJECT_GRAPH": "false", "NX_NO_CLOUD": "true", "NX_SKIP_NX_CACHE": "true", "NX_INTERACTIVE": "false", "NX_NATIVE_COMMAND_RUNNER": "false", "NX_LOAD_DOT_ENV_FILES": "false", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_TERMINAL_PROMPT": "0"}
	for k, v := range extra {
		if k == "KD_CI_SIMULATOR_UDID" || k == "ANDROID_SERIAL" {
			if !validDevice(k, v) {
				return nil, ErrInvalid
			}
			env[key(k)] = v
			continue
		}
		if want, ok := fixed[k]; ok {
			if v != want {
				return nil, ErrInvalid
			}
			env[key(k)] = v
			continue
		}
		if !allowed[strings.ToUpper(k)] || strings.ContainsAny(k+v, "\x00\r\n") {
			return nil, ErrInvalid
		}
		env[key(k)] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(env))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out, nil
}

func Run(parent context.Context, s Spec) (Result, error) { return run(parent, s, nil) }

// RunAcquisition is reserved for trusted Git acquisition, never project commands.
// Only fixed Git isolation controls and the one selected inherited token are admitted.
func RunAcquisition(ctx context.Context, s Spec, authority map[string]string) (Result, error) {
	allowed := map[string]bool{"GIT_CONFIG_NOSYSTEM": true, "GIT_CONFIG_GLOBAL": true, "GIT_TERMINAL_PROMPT": true, "GIT_ASKPASS": true, "GIT_SSH_COMMAND": true, "GIT_LFS_SKIP_SMUDGE": true, "KD_CI_CREDENTIAL_SLOT": true, "KD_CI_CREDENTIAL_PATH": true}
	slot := authority["KD_CI_CREDENTIAL_SLOT"]
	for k, v := range authority {
		if !allowed[k] && k != slot {
			return Result{ExitCode: -1}, ErrInvalid
		}
		if strings.ContainsRune(k+v, 0) {
			return Result{ExitCode: -1}, ErrInvalid
		}
	}
	if len(slot) != 14 || !strings.HasPrefix(slot, "SOURCE_READ_") || slot[12] < '0' || slot[12] > '3' || slot[13] < '0' || slot[13] > '9' || slot[12:] == "00" || slot[12:] > "32" {
		return Result{ExitCode: -1}, ErrInvalid
	}
	return run(ctx, s, authority)
}

func run(parent context.Context, s Spec, authority map[string]string) (res Result, ret error) {
	res.ExitCode = -1
	defer func() { retainCapture(parent, s, res, ret) }()
	res.CleanupComplete = true
	if validateArgv(s.Argv) != nil || s.Timeout <= 0 || s.Timeout > 6*time.Hour {
		return res, ErrInvalid
	}
	cwd, e := SafePath(s.Root, s.Cwd)
	if e != nil {
		return res, e
	}
	env, e := Environment(s.Env)
	if e != nil {
		return res, e
	}
	for k, v := range authority {
		env = append(env, k+"="+v)
	}
	if s.OutputLimit == 0 {
		s.OutputLimit = MaxOutput
	}
	if s.OutputLimit < 1 || s.OutputLimit > MaxOutput {
		return res, ErrInvalid
	}
	if s.Grace == 0 {
		s.Grace = 10 * time.Second
	}
	if s.ReapTimeout == 0 {
		s.ReapTimeout = 10 * time.Second
	}
	if s.Grace < 0 || s.Grace > 10*time.Second || s.ReapTimeout < 0 || s.ReapTimeout > 10*time.Second {
		return res, ErrInvalid
	}
	if !filepath.IsAbs(s.ResultDir) || filepath.Clean(s.ResultDir) != s.ResultDir {
		return res, ErrInvalid
	}
	if _, e = SafePath(filepath.Dir(s.ResultDir), "."); e != nil {
		return res, e
	}
	if e = makePrivateDir(s.ResultDir); e != nil {
		return res, ErrCapture
	}
	active := filepath.Join(s.ResultDir, "active")
	af, e := privateFile(active)
	if e != nil {
		return res, ErrCapture
	}
	af.Close()
	defer func() {
		res.Ended = time.Now().UTC()
		if res.CleanupComplete {
			_ = os.Remove(active)
		}
	}()
	res.StdoutPath = filepath.Join(s.ResultDir, "stdout")
	res.StderrPath = filepath.Join(s.ResultDir, "stderr")
	stdout, e := privateFile(res.StdoutPath)
	if e != nil {
		return res, ErrCapture
	}
	defer stdout.Close()
	stderr, e := privateFile(res.StderrPath)
	if e != nil {
		return res, ErrCapture
	}
	defer stderr.Close()
	outR, outW, e := os.Pipe()
	if e != nil {
		return res, ErrCapture
	}
	defer outR.Close()
	defer outW.Close()
	errR, errW, e := os.Pipe()
	if e != nil {
		return res, ErrCapture
	}
	defer errR.Close()
	defer errW.Close()
	nul, e := os.Open(os.DevNull)
	if e != nil {
		return res, ErrCapture
	}
	defer nul.Close()
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stdin = nul
	cmd.Stdout = outW
	cmd.Stderr = errW
	owner, e := prepare(cmd)
	if e != nil {
		return res, ErrCapture
	}
	defer owner.close()
	if e = parent.Err(); e != nil {
		return res, e
	}
	res.Started = time.Now().UTC()
	if e = cmd.Start(); e != nil {
		_, _ = stderr.WriteString(e.Error())
		return res, ErrCapture
	}
	res.CleanupComplete = false
	// Windows launch is suspended: admission to its kill-on-close job precedes resume.
	if e = owner.started(cmd); e != nil {
		res.CleanupComplete = stopUnadmitted(cmd)
		return res, ErrCapture
	}
	outW.Close()
	errW.Close()
	overflow := make(chan struct{}, 1)
	copyErrors := make(chan error, 2)
	var copies sync.WaitGroup
	copyStream := func(dst *os.File, src *os.File) {
		defer copies.Done()
		n, e := io.Copy(dst, io.LimitReader(src, s.OutputLimit))
		if e == nil && n == s.OutputLimit {
			var b [1]byte
			var m int
			m, e = src.Read(b[:])
			if m > 0 {
				select {
				case overflow <- struct{}{}:
				default:
				}
				e = ErrOverflow
			}
		}
		if e != nil && e != io.EOF {
			copyErrors <- e
		}
	}
	copies.Add(2)
	go copyStream(stdout, outR)
	go copyStream(stderr, errR)
	exited := observeExit(cmd)
	ctx, cancel := context.WithTimeout(parent, s.Timeout)
	defer cancel()
	var waitErr error
	ended := false
	mustStop := false
	select {
	case waitErr = <-exited:
		ended = true
		res.ExitObserved = waitErr == nil
	case <-overflow:
		res.Overflow = true
		mustStop = true
	case <-ctx.Done():
		res.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		res.Interrupted = errors.Is(ctx.Err(), context.Canceled)
		mustStop = true
	}
	// Even a successful parent can leave grandchildren holding capture descriptors.
	// Stop the owned tree on every path; retain the parent's exact original status.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()
	_ = owner.stop(false)
	if !ended {
		select {
		case waitErr = <-exited:
			ended = true
		case <-time.After(s.Grace):
		case <-cleanupCtx.Done():
		}
	}
	stopErr := owner.stop(true)
	// The leader is reaped only after the last group signal, so its ID cannot be
	// recycled into an unrelated group while cleanup sends signals.
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	ended = false
	if !ended {
		select {
		case waitErr = <-waited:
			ended = true
		case <-time.After(s.ReapTimeout):
		case <-cleanupCtx.Done():
		}
	}
	drained := make(chan struct{})
	go func() { copies.Wait(); close(drained) }()
	select {
	case <-drained:
		res.CleanupComplete = ended
	case <-time.After(s.ReapTimeout):
		outR.Close()
		errR.Close()
		res.CleanupComplete = false
	case <-cleanupCtx.Done():
		outR.Close()
		errR.Close()
		res.CleanupComplete = false
	}
	for res.CleanupComplete {
		empty, probeErr := owner.empty()
		if probeErr != nil {
			res.CleanupComplete = false
			break
		}
		if empty {
			res.CleanupComplete = terminationConfirmed(stopErr, true)
			break
		}
		select {
		case <-cleanupCtx.Done():
			res.CleanupComplete = false
		case <-time.After(10 * time.Millisecond):
		}
	}
	if ended {
		res.ExitCode = cmd.ProcessState.ExitCode()
		res.Signal = exitSignal(cmd.ProcessState)
	}
	select {
	case <-overflow:
		res.Overflow = true
	default:
	}
	if waitErr != nil {
		ret = ErrCapture
	}
	if mustStop && ret == nil {
		ret = ErrCapture
	}
	if res.Overflow && ret == nil {
		ret = ErrOverflow
	}
	select {
	case <-copyErrors:
		if ret == nil {
			ret = ErrCapture
		}
	default:
	}
	if !res.CleanupComplete && ret == nil {
		ret = ErrCleanup
	}
	if e = stdout.Close(); e != nil && ret == nil {
		ret = ErrCapture
	}
	if e = stderr.Close(); e != nil && ret == nil {
		ret = ErrCapture
	}
	return res, ret
}
