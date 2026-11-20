// Command ci-candidate checks and records public unsigned tooling bytes.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
)

const noticesFile = "THIRD_PARTY_NOTICES"
const expectedGoVersion = "go1.27.4"

var errCandidate = errors.New("candidate check failed")
var controlPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var runPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

type options struct {
	OS, Arch, Control, RunID, Attempt, Dist, Out string
	HostOnly                                     bool
}
type manifest struct {
	SchemaVersion     int             `json:"schema_version"`
	Kind              string          `json:"kind"`
	ControlSHA        string          `json:"control_sha"`
	RunID             string          `json:"run_id"`
	RunAttempt        int             `json:"run_attempt"`
	OS                string          `json:"os"`
	Architecture      string          `json:"architecture"`
	Binary            string          `json:"binary"`
	SHA256            string          `json:"binary_sha256"`
	Notices           string          `json:"notices"`
	NoticesSHA256     string          `json:"notices_sha256"`
	GoVersion         string          `json:"go_version"`
	Builder           string          `json:"builder"`
	BuilderVersion    string          `json:"builder_version"`
	NativeSmoke       map[string]bool `json:"native_smoke"`
	RuntimeQualified  bool            `json:"runtime_qualified"`
	AdmissionEligible bool            `json:"admission_eligible"`
}

func main() {
	if run(os.Args[1:]) != nil {
		fmt.Fprintln(os.Stderr, "FAIL: candidate check")
		os.Exit(1)
	}
	fmt.Println("PASS: candidate check")
}
func run(args []string) error {
	var o options
	flags := flag.NewFlagSet("ci-candidate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&o.OS, "os", "", "native operating system")
	flags.StringVar(&o.Arch, "arch", "", "native architecture")
	flags.StringVar(&o.Control, "control-sha", "", "producing public commit")
	flags.StringVar(&o.RunID, "run-id", "", "producing run")
	flags.StringVar(&o.Attempt, "attempt", "", "producing attempt")
	flags.StringVar(&o.Dist, "dist", "dist", "snapshot directory")
	flags.StringVar(&o.Out, "out", "candidate-output", "new output directory")
	flags.BoolVar(&o.HostOnly, "host-only", false, "check native host only")
	if flags.Parse(args) != nil || flags.NArg() != 0 || validateHost(o.OS, o.Arch) != nil {
		return errCandidate
	}
	if o.HostOnly {
		return nil
	}
	attempt, e := strconv.Atoi(o.Attempt)
	if e != nil || attempt < 1 || strconv.Itoa(attempt) != o.Attempt || !runPattern.MatchString(o.RunID) || !controlPattern.MatchString(o.Control) {
		return errCandidate
	}
	binary, e := findBinary(o.Dist, o.OS)
	if e != nil {
		return e
	}
	if inspectBinary(binary, o.OS, o.Arch) != nil {
		return errCandidate
	}
	info, e := buildinfo.ReadFile(binary)
	if e != nil || info.GoVersion != expectedGoVersion {
		return errCandidate
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	if settings["CGO_ENABLED"] != "0" || settings["GOOS"] != o.OS || settings["GOARCH"] != o.Arch || settings["-trimpath"] != "true" {
		return errCandidate
	}
	before, e := hashFile(binary)
	if e != nil {
		return e
	}
	if smoke(binary, o) != nil {
		return errCandidate
	}
	after, e := hashFile(binary)
	if e != nil || after != before {
		return errCandidate
	}
	output, e := filepath.Abs(o.Out)
	if e != nil || process.MakePrivateDir(output) != nil {
		return errCandidate
	}
	name := "kd-ci"
	if o.OS == "windows" {
		name += ".exe"
	}
	destination := filepath.Join(output, name)
	in, e := os.Open(binary)
	if e != nil {
		return errCandidate
	}
	defer in.Close()
	out, e := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if e != nil {
		return errCandidate
	}
	_, copyErr := io.Copy(out, io.LimitReader(in, (128<<20)+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		return errCandidate
	}
	copied, e := hashFile(destination)
	if e != nil || copied != before {
		return errCandidate
	}
	controlDirectory, e := os.Getwd()
	if e != nil {
		return errCandidate
	}
	noticesHash, e := copyReviewedNotices(controlDirectory, output)
	if e != nil {
		return errCandidate
	}
	m := manifest{SchemaVersion: 1, Kind: "unsigned-cli-candidate", ControlSHA: o.Control, RunID: o.RunID, RunAttempt: attempt, OS: o.OS, Architecture: o.Arch, Binary: name, SHA256: before, Notices: noticesFile, NoticesSHA256: noticesHash, GoVersion: info.GoVersion, Builder: "goreleaser-oss", BuilderVersion: "2.20.2", NativeSmoke: map[string]bool{"version": true, "help": true, "offline_refusals": true}}
	encoded, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return errCandidate
	}
	if os.WriteFile(filepath.Join(output, "candidate.json"), append(encoded, '\n'), 0600) != nil || os.WriteFile(filepath.Join(output, "SHA256SUMS"), []byte(before+"  "+name+"\n"+noticesHash+"  "+noticesFile+"\n"), 0600) != nil {
		return errCandidate
	}
	return nil
}
func validateHost(goos, arch string) error {
	if goos != "darwin" && goos != "linux" && goos != "windows" || arch != "amd64" && arch != "arm64" || runtime.GOOS != goos || runtime.GOARCH != arch || runtime.Version() != expectedGoVersion {
		return errCandidate
	}
	actual, e := nativeArchitecture()
	if e != nil || actual != arch {
		return errCandidate
	}
	return nil
}
func findBinary(dist, goos string) (string, error) {
	root, e := filepath.Abs(dist)
	if e != nil {
		return "", errCandidate
	}
	name := "kd-ci"
	if goos == "windows" {
		name += ".exe"
	}
	found := ""
	count := 0
	e = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil || d.Type()&os.ModeSymlink != 0 {
			return errCandidate
		}
		count++
		if count > 10000 {
			return errCandidate
		}
		if !d.IsDir() && d.Name() == name {
			st, e := d.Info()
			if e != nil || !st.Mode().IsRegular() || found != "" {
				return errCandidate
			}
			found = path
		}
		return nil
	})
	if e != nil || found == "" {
		return "", errCandidate
	}
	return found, nil
}
func hashFile(path string) (string, error) {
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > 128<<20 {
		return "", errCandidate
	}
	f, e := os.Open(path)
	if e != nil {
		return "", errCandidate
	}
	defer f.Close()
	hash := sha256.New()
	if _, e = io.Copy(hash, io.LimitReader(f, (128<<20)+1)); e != nil {
		return "", errCandidate
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func inspectBinary(path, goos, arch string) error {
	switch goos {
	case "linux":
		f, e := elf.Open(path)
		if e != nil {
			return errCandidate
		}
		defer f.Close()
		expected := elf.EM_X86_64
		if arch == "arm64" {
			expected = elf.EM_AARCH64
		}
		if f.Machine != expected || f.Class != elf.ELFCLASS64 {
			return errCandidate
		}
	case "darwin":
		f, e := macho.Open(path)
		if e != nil {
			return errCandidate
		}
		defer f.Close()
		expected := macho.CpuAmd64
		if arch == "arm64" {
			expected = macho.CpuArm64
		}
		if f.Cpu != expected {
			return errCandidate
		}
	case "windows":
		f, e := pe.Open(path)
		if e != nil {
			return errCandidate
		}
		defer f.Close()
		expected := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
		if arch == "arm64" {
			expected = pe.IMAGE_FILE_MACHINE_ARM64
		}
		if f.Machine != expected {
			return errCandidate
		}
	default:
		return errCandidate
	}
	return nil
}
func smoke(binary string, o options) error {
	parent, e := os.MkdirTemp("", "ci-candidate-")
	if e != nil {
		return errCandidate
	}
	defer os.RemoveAll(parent)
	parent, e = filepath.EvalSymlinks(parent)
	if e != nil {
		return errCandidate
	}
	private := filepath.Join(parent, "private")
	if process.MakePrivateDir(private) != nil {
		return errCandidate
	}
	home := filepath.Join(private, "home")
	if process.MakePrivateDir(home) != nil {
		return errCandidate
	}
	checks := [][]string{{"version", "--json"}, {"--help"}, {"dispatch", "--offline", "--json"}, {"status", "1", "--offline", "--json"}, {"wait", "1", "--offline", "--json"}, {"failures", "1", "--offline", "--json"}, {"cancel", "1", "--offline", "--json"}}
	for i, args := range checks {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		result, e := process.Run(ctx, process.Spec{Root: private, Cwd: ".", Argv: append([]string{binary}, args...), ResultDir: filepath.Join(private, fmt.Sprintf("smoke-%d", i)), Timeout: 20 * time.Second, OutputLimit: 64 << 10, Env: map[string]string{"HOME": home, "USERPROFILE": home, "NO_COLOR": "1"}})
		cancel()
		if e != nil && !(i > 1 && errors.Is(e, process.ErrCapture) && result.ExitCode == 3) || result.TimedOut || result.Overflow || !result.CleanupComplete || result.Signal != "" {
			return errCandidate
		}
		raw, e := os.ReadFile(result.StdoutPath)
		if e != nil || len(raw) > 64<<10 {
			return errCandidate
		}
		if i == 1 {
			if result.ExitCode != 0 || !bytes.Contains(raw, []byte("kd-ci")) {
				return errCandidate
			}
			continue
		}
		var response protocol.Result
		if config.Decode(raw, &response, 64<<10) != nil || response.ValidateSemantics() != nil {
			return errCandidate
		}
		if i == 0 {
			if result.ExitCode != 0 || !response.OK || response.Local == nil {
				return errCandidate
			}
			data := response.Local.Data
			version, ok := data.(map[string]any)
			if !ok || version["commit"] != o.Control || version["os"] != o.OS || version["arch"] != o.Arch || version["go_version"] != expectedGoVersion || version["production_qualified"] != false {
				return errCandidate
			}
		}
		if i > 1 {
			if result.ExitCode != 3 || response.ExitCode != 3 || response.OK || response.Error == nil || response.Error.Code != "offline_refused" || response.AdmissionEligible {
				return errCandidate
			}
		}
	}
	return nil
}

func copyReviewedNotices(controlDirectory, output string) (string, error) {
	source, e := config.SafePath(controlDirectory, noticesFile)
	if e != nil {
		return "", errCandidate
	}
	st, e := os.Lstat(source)
	if e != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > 4<<20 {
		return "", errCandidate
	}
	file, e := os.Open(source)
	if e != nil {
		return "", errCandidate
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if e != nil || len(data) > 4<<20 || !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return "", errCandidate
	}
	destination := filepath.Join(output, noticesFile)
	out, e := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", errCandidate
	}
	_, writeErr := out.Write(data)
	closeErr := out.Close()
	if writeErr != nil || closeErr != nil {
		return "", errCandidate
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	copied, e := hashFile(destination)
	if e != nil || copied != digest {
		return "", errCandidate
	}
	return digest, nil
}
