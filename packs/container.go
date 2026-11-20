package packs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/config"
)

type containerAdapter struct {
	run              Execute
	docker, id, user string
}

// Container uses Docker's maintained lifecycle and exec commands. It owns one
// exact container ID, without a daemon, VM manager, image builder or shell DSL.
func Container(ctx context.Context, platform string, s ContainerSupport, run Execute, docker, kit, sourceParent string) (Adapter, error) {
	if !Native(platform) {
		return nil, ErrSupport
	}
	return containerWithExecutor(ctx, platform, s, run, docker, kit, sourceParent)
}
func containerWithExecutor(ctx context.Context, platform string, s ContainerSupport, run Execute, docker, kit, sourceParent string) (adapter Adapter, err error) {
	if docker == "" || s.Image != images[platform] || !filepath.IsAbs(kit) || !filepath.IsAbs(sourceParent) || strings.ContainsAny(kit+sourceParent, ",\r\n") {
		return nil, ErrSupport
	}
	out, e := run(ctx, []string{"id", "-u"}, ".", 30*time.Second, nil)
	if e != nil {
		return nil, ErrSupport
	}
	uid := strings.TrimSpace(string(out))
	if n, e := strconv.Atoi(uid); e != nil || n < 1 {
		return nil, ErrSupport
	}
	out, e = run(ctx, []string{"id", "-g"}, ".", 30*time.Second, nil)
	if e != nil {
		return nil, ErrSupport
	}
	gid := strings.TrimSpace(string(out))
	if n, e := strconv.Atoi(gid); e != nil || n < 1 {
		return nil, ErrSupport
	}
	if _, e = run(ctx, []string{docker, "pull", s.Image}, ".", 10*time.Minute, nil); e != nil {
		return nil, ErrSupport
	}
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return nil, ErrSupport
	}
	name := "ci-" + hex.EncodeToString(nonce[:])
	cidfile := filepath.Join(filepath.Dir(sourceParent), "container.cid")
	if _, e := os.Lstat(cidfile); !os.IsNotExist(e) {
		return nil, ErrSupport
	}
	a := &containerAdapter{run: run, docker: docker, user: uid + ":" + gid}
	success := false
	defer func() {
		if !success && a.id != "" {
			if cleanup := a.Close(context.Background()); cleanup != nil {
				err = ErrSupport
			}
		}
	}()
	out, e = run(ctx, []string{docker, "run", "--detach", "--rm", "--name", name, "--label", "ci.owner=" + name, "--cidfile", cidfile, "--platform", "linux/amd64", "--mount", "type=bind,src=" + sourceParent + ",dst=" + sourceParent, "--mount", "type=bind,src=" + kit + ",dst=/public-control,readonly", "--workdir", "/", "--entrypoint", "/bin/sleep", s.Image, "infinity"}, ".", time.Minute, nil)
	// Docker owns the cidfile write, in a host-owned private directory. Recover
	// identity even if its client is cancelled before it returns stdout.
	if st, statErr := os.Lstat(cidfile); statErr == nil && st.Mode().IsRegular() && st.Size() <= 128 {
		data, readErr := os.ReadFile(cidfile)
		if readErr == nil {
			candidate := strings.TrimSpace(string(data))
			if regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(candidate) {
				a.id = candidate
			}
		}
	}
	if a.id == "" {
		recovered, inspectErr := run(context.Background(), []string{docker, "inspect", name}, ".", 30*time.Second, nil)
		var result []struct {
			ID     string `json:"Id"`
			Config struct {
				Labels map[string]string `json:"Labels"`
			} `json:"Config"`
		}
		if inspectErr == nil && json.Unmarshal(recovered, &result) == nil && len(result) == 1 && result[0].Config.Labels["ci.owner"] == name && regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(result[0].ID) {
			a.id = result[0].ID
		}
	}
	if e != nil || a.id == "" || strings.TrimSpace(string(out)) != a.id {
		return nil, ErrSupport
	}
	id := a.id
	out, e = run(ctx, []string{docker, "exec", id, "/bin/cat", "/etc/os-release"}, ".", 30*time.Second, nil)
	if e != nil {
		return nil, ErrSupport
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = strings.Trim(v, "\"'")
		}
	}
	if platform == "linux-debian" && (values["ID"] != "debian" || values["VERSION_ID"] != "13") || platform == "linux-arch" && values["ID"] != "arch" || platform == "web-linux" && (values["ID"] != "ubuntu" || values["VERSION_ID"] != "24.04") {
		return nil, ErrSupport
	}
	out, e = run(ctx, []string{docker, "exec", id, "uname", "-m"}, ".", 30*time.Second, nil)
	if e != nil || strings.TrimSpace(string(out)) != "x86_64" {
		return nil, ErrSupport
	}
	if platform != "web-linux" {
		argv := []string{docker, "exec", "--user", "0", "--env", "CI_SNAPSHOT=" + s.Snapshot, id, "/bin/sh", "/public-control/packs/" + platform + "/setup.sh"}
		argv = append(argv, s.Packages...)
		if _, e = run(ctx, argv, ".", 10*time.Minute, nil); e != nil {
			return nil, ErrSupport
		}
	}
	if _, e = run(ctx, []string{docker, "exec", "--user", "0", id, "install", "-d", "-o", uid, "-g", gid, "/tmp/ci-home"}, ".", 30*time.Second, nil); e != nil {
		return nil, ErrSupport
	}
	success = true
	return a, nil
}
func (a *containerAdapter) Wrap(argv []string, source, cwd string) ([]string, string, map[string]string, error) {
	work, e := config.SafePath(source, filepath.ToSlash(cwd))
	if e != nil || len(argv) == 0 {
		return nil, "", nil, ErrSupport
	}
	args := []string{a.docker, "exec", "--user", a.user, "--workdir", work, "--env", "HOME=/tmp/ci-home", "--env", "PATH=/tmp/ci-home/.cargo/bin:/tmp/ci-home/.local/bin:/usr/local/bin:/usr/bin:/bin", "--env", "CI=true", "--env", "PLAYWRIGHT_BROWSERS_PATH=/ms-playwright", "--env", "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", a.id}
	args = append(args, argv...)
	return args, ".", nil, nil
}
func (a *containerAdapter) Before(context.Context, string) error { return nil }
func (a *containerAdapter) Close(ctx context.Context) error {
	if a.id == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, e := a.run(ctx, []string{a.docker, "rm", "--force", a.id}, ".", 30*time.Second, nil)
	if e != nil {
		return ErrSupport
	}
	a.id = ""
	return nil
}
