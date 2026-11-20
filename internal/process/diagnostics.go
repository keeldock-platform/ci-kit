package process

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

type captureSinkKey struct{}

const diagnosticBudget int64 = 128 << 20

// CommandDiagnostic contains private argv and capture metadata. Environment
// values, including acquisition credentials, are deliberately excluded.
type CommandDiagnostic struct {
	Argv          []string `json:"argv"`
	Root          string   `json:"root"`
	Cwd           string   `json:"cwd"`
	Result        Result   `json:"result"`
	Error         string   `json:"error,omitempty"`
	Incomplete    bool     `json:"incomplete"`
	CaptureErrors []string `json:"capture_errors"`
}

// WithCaptureSink retains diagnostics outside ephemeral checkout trees. Nothing
// from this sink is public: the hosted caller must encrypt before publication.
func WithCaptureSink(ctx context.Context, directory string) context.Context {
	return context.WithValue(ctx, captureSinkKey{}, directory)
}

// RetainAttempt records resolution errors that occur before a child can start.
func RetainAttempt(ctx context.Context, s Spec, err error) {
	retainCapture(ctx, s, Result{ExitCode: -1, CleanupComplete: true}, err)
}

func captureDirectory(sink, prefix string) (string, error) {
	if !UserOnly(sink, true) {
		if e := MakePrivateDir(sink); e != nil {
			return "", e
		}
	}
	dir, e := os.MkdirTemp(sink, prefix)
	if e != nil {
		return "", e
	}
	if e = os.Remove(dir); e != nil {
		return "", e
	}
	if e = MakePrivateDir(dir); e != nil {
		return "", e
	}
	return dir, nil
}
func captureRemaining(sink string) int64 {
	remaining := diagnosticBudget
	_ = filepath.WalkDir(sink, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			remaining = 0
			return e
		}
		if !d.IsDir() {
			st, e := d.Info()
			if e != nil || !st.Mode().IsRegular() {
				remaining = 0
				return ErrCapture
			}
			remaining -= st.Size()
		}
		return nil
	})
	if remaining < 0 {
		return 0
	}
	return remaining
}
func readDiagnosticFile(path string, limit int64) ([]byte, error) {
	if _, e := SafePath(filepath.Dir(path), "."); e != nil {
		return nil, e
	}
	before, e := os.Lstat(path)
	if e != nil || !before.Mode().IsRegular() {
		return nil, ErrCapture
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || !os.SameFile(before, st) {
		return nil, ErrCapture
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(raw)) > limit {
		return raw[:limit], ErrOverflow
	}
	return raw, nil
}
func retainCapture(ctx context.Context, s Spec, result Result, err error) {
	sink, _ := ctx.Value(captureSinkKey{}).(string)
	if sink == "" {
		return
	}
	dir, e := captureDirectory(sink, "command-")
	if e != nil {
		return
	}
	record := CommandDiagnostic{Argv: s.Argv, Root: s.Root, Cwd: s.Cwd, Result: result, CaptureErrors: []string{}}
	if err != nil {
		record.Error = err.Error()
	}
	remaining := captureRemaining(sink) - (64 << 10)
	if remaining < 0 {
		remaining = 0
	}
	for _, stream := range []struct{ name, path string }{{"stdout", result.StdoutPath}, {"stderr", result.StderrPath}} {
		raw := []byte{}
		if stream.path != "" {
			raw, e = readDiagnosticFile(stream.path, remaining)
			if e != nil {
				record.Incomplete = true
				record.CaptureErrors = append(record.CaptureErrors, stream.name+": capture unavailable or budget exceeded")
			}
		}
		remaining -= int64(len(raw))
		if remaining < 0 {
			remaining = 0
		}
		if writeCaptureFile(filepath.Join(dir, stream.name), raw) != nil {
			record.Incomplete = true
			record.CaptureErrors = append(record.CaptureErrors, stream.name+": capture write failed")
		}
	}
	raw, e := json.Marshal(record)
	if e != nil {
		return
	}
	_ = writeCaptureFile(filepath.Join(dir, "command.json"), raw)
}
func writeCaptureFile(path string, raw []byte) error {
	f, e := CreatePrivateFile(path)
	if e != nil {
		return e
	}
	_, e = f.Write(raw)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}

// RetainReport copies fresh JUnit even when the runner returns before ordinary
// report validation. Missing reports remain missing; malformed XML is retained.
func RetainReport(ctx context.Context, path string) {
	sink, _ := ctx.Value(captureSinkKey{}).(string)
	if sink == "" {
		return
	}
	if _, e := os.Lstat(path); os.IsNotExist(e) {
		return
	}
	dir, e := captureDirectory(sink, "report-")
	if e != nil {
		return
	}
	limit := captureRemaining(sink)
	if limit > 16<<20 {
		limit = 16 << 20
	}
	raw, e := readDiagnosticFile(path, limit)
	if e != nil {
		_ = writeCaptureFile(filepath.Join(dir, "incomplete.txt"), []byte("JUnit capture unavailable or budget exceeded"))
		return
	}
	_ = writeCaptureFile(filepath.Join(dir, "junit.xml"), raw)
}
