// Package diagnostics transports private failure evidence using maintained age.
package diagnostics

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"filippo.io/age"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

const MaxBundle = 128 << 20

var ErrInvalid = errors.New("diagnostics unavailable")

type Identity struct {
	Run       string `json:"run"`
	Attempt   string `json:"attempt"`
	Platform  string `json:"platform"`
	KitSHA    string `json:"kit_sha"`
	SourceSHA string `json:"source_sha"`
}
type Bundle struct {
	SchemaVersion int               `json:"schema_version"`
	Identity      Identity          `json:"identity"`
	ExitCode      int               `json:"exit_code"`
	Files         map[string][]byte `json:"files"`
}

// Seal publishes atomically only a complete authenticated ciphertext. Source
// trees and environment variables are never enumerated by this collector.
func Seal(sink, output, recipient string, identity Identity, code int) error {
	r, e := age.ParseX25519Recipient(recipient)
	if e != nil || !config.NoLinks(sink) || !process.UserOnly(sink, true) {
		return ErrInvalid
	}
	b := Bundle{1, identity, code, map[string][]byte{}}
	total := 0
	e = filepath.WalkDir(sink, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return ErrInvalid
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return ErrInvalid
		}
		name, e := filepath.Rel(sink, path)
		if e != nil {
			return ErrInvalid
		}
		switch filepath.Base(name) {
		case "command.json", "stdout", "stderr", "junit.xml", "incomplete.txt":
		default:
			return ErrInvalid
		}
		st, e := d.Info()
		if e != nil || !st.Mode().IsRegular() || st.Size() > process.MaxOutput || total+int(st.Size()) > MaxBundle || len(b.Files) >= 4096 {
			return ErrInvalid
		}
		f, e := os.Open(path)
		if e != nil {
			return ErrInvalid
		}
		raw, e := io.ReadAll(io.LimitReader(f, int64(MaxBundle-total)+1))
		f.Close()
		if len(raw) > MaxBundle-total {
			return ErrInvalid
		}
		if e != nil {
			return ErrInvalid
		}
		total += len(raw)
		b.Files[filepath.ToSlash(name)] = raw
		return nil
	})
	if e != nil || Validate(b) != nil {
		return ErrInvalid
	}
	if !config.NoLinks(filepath.Dir(output)) {
		return ErrInvalid
	}
	f, e := process.CreatePrivateFile(output + ".partial")
	if e != nil {
		return ErrInvalid
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(output + ".partial")
		}
	}()
	w, e := age.Encrypt(f, r)
	if e != nil {
		return ErrInvalid
	}
	if json.NewEncoder(w).Encode(b) != nil || w.Close() != nil || f.Sync() != nil || f.Close() != nil {
		return ErrInvalid
	}
	if _, e = os.Lstat(output); !os.IsNotExist(e) {
		return ErrInvalid
	}
	if os.Rename(output+".partial", output) != nil {
		return ErrInvalid
	}
	ok = true
	return nil
}

// Open authenticates the entire ciphertext before returning data. It never
// extracts archive paths or executes commands from a bundle.
func Open(ciphertext io.Reader, keyPath string) (Bundle, error) {
	var b Bundle
	if !config.NoLinks(keyPath) || !process.UserOnly(keyPath, false) {
		return b, ErrInvalid
	}
	key, e := os.Open(keyPath)
	if e != nil {
		return b, ErrInvalid
	}
	ids, e := age.ParseIdentities(io.LimitReader(key, 65537))
	key.Close()
	if e != nil || len(ids) != 1 {
		return b, ErrInvalid
	}
	r, e := age.Decrypt(ciphertext, ids...)
	if e != nil {
		return b, ErrInvalid
	}
	raw, e := io.ReadAll(io.LimitReader(r, 2*MaxBundle+1))
	if e != nil || len(raw) > 2*MaxBundle {
		return b, ErrInvalid
	}

	var wire struct {
		SchemaVersion int               `json:"schema_version"`
		Identity      Identity          `json:"identity"`
		ExitCode      int               `json:"exit_code"`
		Files         map[string]string `json:"files"`
	}
	if config.Decode(raw, &wire, 2*MaxBundle) != nil || len(wire.Files) > 4096 {
		return b, ErrInvalid
	}
	b = Bundle{SchemaVersion: wire.SchemaVersion, Identity: wire.Identity, ExitCode: wire.ExitCode, Files: map[string][]byte{}}
	for name, encoded := range wire.Files {
		data, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil {
			return Bundle{}, ErrInvalid
		}
		b.Files[name] = data
	}

	total := 0
	for name, data := range b.Files {
		if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return Bundle{}, ErrInvalid
		}
		total += len(data)
	}
	if total > MaxBundle || Validate(b) != nil {
		return Bundle{}, ErrInvalid
	}
	return b, nil
}

// Validate checks a closed wire format; encryption is not proof of the sender.
func Validate(b Bundle) error {
	numeric := regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	sha := regexp.MustCompile(`^[a-f0-9]{40}$`)
	slot := regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)
	if b.SchemaVersion != 1 || !numeric.MatchString(b.Identity.Run) || !numeric.MatchString(b.Identity.Attempt) || !sha.MatchString(b.Identity.KitSHA) || !sha.MatchString(b.Identity.SourceSHA) || !slot.MatchString(b.Identity.Platform) || b.ExitCode == 0 || b.ExitCode < -1 || b.ExitCode > 255 || len(b.Files) == 0 {
		return ErrInvalid
	}
	commandCount := 0
	groups := map[string]map[string]bool{}
	for name, data := range b.Files {
		parts := strings.Split(name, "/")
		if len(parts) != 2 || !regexp.MustCompile(`^(?:command|report)-[A-Za-z0-9]+$`).MatchString(parts[0]) {
			return ErrInvalid
		}
		if groups[parts[0]] == nil {
			groups[parts[0]] = map[string]bool{}
		}
		groups[parts[0]][parts[1]] = true
		if parts[1] == "command.json" {
			var required map[string]json.RawMessage
			if config.Decode(data, &required, 1<<20) != nil {
				return ErrInvalid
			}
			for _, key := range []string{"argv", "root", "cwd", "result", "incomplete", "capture_errors"} {
				if _, ok := required[key]; !ok {
					return ErrInvalid
				}
			}
			var command process.CommandDiagnostic
			if config.Decode(data, &command, 1<<20) != nil || len(command.Argv) == 0 || len(command.Argv) > 4096 || command.Root == "" || command.Cwd == "" {
				return ErrInvalid
			}
			commandCount++
		}
	}
	for dir, files := range groups {
		if strings.HasPrefix(dir, "command-") {
			if len(files) != 3 || !files["command.json"] || !files["stdout"] || !files["stderr"] {
				return ErrInvalid
			}
		} else if len(files) != 1 || !files["junit.xml"] && !files["incomplete.txt"] {
			return ErrInvalid
		}
	}
	if commandCount == 0 {
		return ErrInvalid
	}
	return nil
}
