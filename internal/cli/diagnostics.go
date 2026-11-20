package cli

import (
	"bytes"
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/keeldock-platform/ci-kit/internal/collector"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/diagnostics"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

func diagnosticsLocal() bool {
	for _, k := range []string{"CI", "GITHUB_ACTIONS", "GITHUB_RUN_ID", "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"} {
		if os.Getenv(k) != "" {
			return false
		}
	}
	return true
}
func diagnosticKeyPath(path string) string {
	if path != "" {
		return path
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return ""
	}
	return filepath.Join(home, ".config", "keeldock-ci", "diag-age.key")
}
func (a *app) decryptFailureFiles(files map[string][]byte, key, output string, expected *diagnostics.Identity) error {
	if !diagnosticsLocal() || output == "" {
		return gh.Fail(3, "policy_refused")
	}
	output, e := filepath.Abs(output)
	if e != nil || !config.NoLinks(filepath.Dir(output)) || !process.UserOnly(filepath.Dir(output), true) {
		return gh.Fail(3, "policy_refused")
	}
	safe, e := config.SafePath(filepath.Dir(output), filepath.Base(output))
	if e != nil || safe != output {
		return gh.Fail(3, "policy_refused")
	}
	type evidence struct {
		Identity       diagnostics.Identity `json:"identity"`
		ExitCode       int                  `json:"exit_code"`
		Failures       []collector.Failure  `json:"failures"`
		Commands       []json.RawMessage    `json:"commands"`
		FailedCommands []json.RawMessage    `json:"failed_commands"`
		Tails          map[string]string    `json:"log_tails"`
		ParseErrors    []string             `json:"parse_errors"`
	}
	reports := []evidence{}
	count := 0
	slots := make([]string, 0, len(files))
	for slot := range files {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	for _, slot := range slots {
		b, e := diagnostics.Open(bytes.NewReader(files[slot]), diagnosticKeyPath(key))
		if e != nil {
			return gh.Fail(6, "missing_evidence")
		}
		if expected != nil {
			want := *expected
			want.Platform = slot
			if b.Identity != want {
				return gh.Fail(3, "policy_refused")
			}
		}
		report := evidence{Identity: b.Identity, ExitCode: b.ExitCode, Tails: map[string]string{}}
		names := make([]string, 0, len(b.Files))
		for name := range b.Files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			data := b.Files[name]
			switch filepath.Base(name) {
			case "junit.xml":
				failures, e := collector.Failures(data)
				if e != nil {
					report.ParseErrors = append(report.ParseErrors, "Malformed JUnit: "+name)
					continue
				}
				report.Failures = append(report.Failures, failures...)
				count += len(failures)
			case "command.json":
				if !json.Valid(data) {
					return gh.Fail(6, "missing_evidence")
				}
				record, _ := json.Marshal(struct {
					Capture string          `json:"capture"`
					Command json.RawMessage `json:"command"`
				}{filepath.Dir(name), json.RawMessage(data)})
				report.Commands = append(report.Commands, json.RawMessage(record))
				var command process.CommandDiagnostic
				if json.Unmarshal(data, &command) != nil {
					return gh.Fail(6, "missing_evidence")
				}
				if command.Error != "" || command.Result.ExitCode != 0 || command.Result.TimedOut || command.Incomplete {
					report.FailedCommands = append(report.FailedCommands, json.RawMessage(record))
				}
			case "incomplete.txt":
				report.ParseErrors = append(report.ParseErrors, string(data))
			case "stdout", "stderr":
				if len(data) > 16384 {
					data = data[len(data)-16384:]
				}
				report.Tails[name] = strings.ToValidUTF8(string(data), "�")
			default:
				return gh.Fail(6, "missing_evidence")
			}
		}
		reports = append(reports, report)
	}
	if len(reports) == 0 {
		return gh.Fail(6, "missing_evidence")
	}
	raw, e := json.MarshalIndent(reports, "", "  ")
	if e != nil {
		return gh.Fail(6, "missing_evidence")
	}
	f, e := process.CreatePrivateFile(output)
	if e != nil {
		return gh.Fail(3, "policy_refused")
	}
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || !process.UserOnly(output, false) {
		f.Close()
		return gh.Fail(3, "policy_refused")
	}
	_, writeErr := f.Write(append(raw, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		os.Remove(output)
		return gh.Fail(6, "missing_evidence")
	}
	// Never attach private contents or paths to the public JSON/stdout projection.
	if a.result.Local != nil {
		a.result.Local.Data = map[string]any{"failure_count": count}
	}
	return nil
}
func (a *app) privateBundle(path, key, output string) error {
	a.begin("failures", false)
	if !diagnosticsLocal() || !config.NoLinks(path) || !process.UserOnly(path, false) {
		return gh.Fail(3, "policy_refused")
	}
	st, e := os.Stat(path)
	if e != nil || st.Size() > 192<<20 {
		return gh.Fail(6, "missing_evidence")
	}
	f, e := os.Open(path)
	if e != nil {
		return gh.Fail(6, "missing_evidence")
	}
	raw, e := io.ReadAll(io.LimitReader(f, (192<<20)+1))
	f.Close()
	if len(raw) > 192<<20 {
		return gh.Fail(6, "missing_evidence")
	}
	if e != nil {
		return gh.Fail(6, "missing_evidence")
	}
	return a.decryptFailureFiles(map[string][]byte{"local": raw}, key, output, nil)
}

func (a *app) experimentFailureFiles(run, path, key, output string, attempt int) error {
	a.begin("failures", false)
	if a.offline || !diagnosticsLocal() || !protocol.Numeric(run) || !config.NoLinks(path) || !process.UserOnly(path, false) {
		return gh.Fail(3, "policy_refused")
	}
	var receipt gh.ExperimentDiagnosticReceipt
	if config.Read(path, &receipt, 1<<20) != nil || receipt.Expectation.RunID != run || attempt < 0 || attempt > 0 && attempt != receipt.Expectation.Attempt {
		return gh.Fail(3, "policy_refused")
	}
	x := receipt.Expectation
	client, e := a.client(filepath.Dir(path))
	if e != nil {
		return e
	}
	if client.VerifyActor(a.ctx, "Moh-Bakr") != nil {
		return gh.Fail(3, "auth_refused")
	}
	if output == "" {
		output = filepath.Join(filepath.Dir(path), "failures-"+run+"-"+strconv.Itoa(x.Attempt)+".json")
	}
	expected := diagnostics.Identity{Run: run, Attempt: strconv.Itoa(x.Attempt), KitSHA: x.Registration.KitSHA, SourceSHA: x.Dispatch.Inputs["source_sha"]}
	a.result.Local.Data = map[string]any{"failure_count": 0}
	return a.diagnosticReports(output, key, &expected, func(visit func(string, []byte) error) error {
		return client.ExperimentDiagnostics(a.ctx, receipt, visit)
	})
}

// diagnosticReports validates a whole attempt before making any report visible
// at its final name. Partial transport/parse errors remove only this invocation's
// temporary or newly linked files, never an existing user report.
func (a *app) diagnosticReports(output, key string, expected *diagnostics.Identity, walk func(func(string, []byte) error) error) error {
	output, e := filepath.Abs(output)
	if e != nil || !config.NoLinks(filepath.Dir(output)) || !process.UserOnly(filepath.Dir(output), true) {
		return gh.Fail(3, "policy_refused")
	}
	safe, e := config.SafePath(filepath.Dir(output), filepath.Base(output))
	if e != nil || safe != output {
		return gh.Fail(3, "policy_refused")
	}
	dir, e := os.MkdirTemp(filepath.Dir(output), ".diagnostics-")
	if e != nil {
		return gh.Fail(6, "missing_evidence")
	}
	if os.Remove(dir) != nil || process.MakePrivateDir(dir) != nil {
		return gh.Fail(3, "policy_refused")
	}
	defer os.RemoveAll(dir)
	type reportFile struct{ staged, target string }
	files := []reportFile{}
	count := 0
	err := walk(func(slot string, cipher []byte) error {
		// Slot strings have already been bound to the fixed provider vocabulary.
		if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`).MatchString(slot) {
			return gh.Fail(3, "policy_refused")
		}
		staged := filepath.Join(dir, slot+".json")
		if e := a.decryptFailureFiles(map[string][]byte{slot: cipher}, key, staged, expected); e != nil {
			return e
		}
		if a.result.Local != nil {
			var data struct {
				Count int `json:"failure_count"`
			}
			raw, _ := json.Marshal(a.result.Local.Data)
			json.Unmarshal(raw, &data)
			count += data.Count
		}
		target := strings.TrimSuffix(output, filepath.Ext(output)) + "-" + slot + ".json"
		if _, e := os.Lstat(target); !os.IsNotExist(e) {
			return gh.Fail(3, "policy_refused")
		}
		files = append(files, reportFile{staged, target})
		return nil
	})
	if err != nil {
		if a.result.Local != nil {
			a.result.Local.Data = map[string]any{}
		}
		return err
	}
	linked := []string{}
	for _, file := range files {
		// Link is atomic and refuses preexisting targets on each supported native OS.
		if os.Link(file.staged, file.target) != nil {
			for _, path := range linked {
				os.Remove(path)
			}
			return gh.Fail(3, "policy_refused")
		}
		linked = append(linked, file.target)
	}
	if a.result.Local != nil {
		a.result.Local.Data = map[string]any{"failure_count": count}
	}
	return nil
}
