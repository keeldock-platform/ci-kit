package packs

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// The official 1575 roll fixes the Firefox ARIA reflection crash (Playwright
// #43967/#44135). Keep the 1.65.1 driver/image and test its protocol compatibility.
const firefoxURL = "https://cdn.playwright.dev/dbazure/download/playwright/builds/firefox/1575/firefox-ubuntu-24.04.zip"
const firefoxSHA256 = "026c3861bbbb863848cb6ac3eb7ffdd6443dde790038c2832441e3edb71a2c74"
const maxFirefoxArchive = 192 << 20
const maxFirefoxExpanded = 512 << 20

// prepareFirefox runs before any product command. All inputs are kit-owned;
// source configuration cannot choose a URL, digest, revision or extraction path.
func prepareFirefox(ctx context.Context, parent string, imageEntries []string) (string, error) {
	destination := filepath.Join(parent, ".public-browsers")
	if err := os.Mkdir(destination, 0700); err != nil {
		return "", ErrSupport
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(destination)
		}
	}()
	archive, err := os.CreateTemp(destination, "download-")
	if err != nil {
		return "", ErrSupport
	}
	defer os.Remove(archive.Name())
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) > 5 {
			return ErrSupport
		}
		return nil
	}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, firefoxURL, nil)
	if err != nil {
		archive.Close()
		return "", ErrSupport
	}
	response, err := client.Do(request)
	if err != nil {
		archive.Close()
		return "", ErrSupport
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maxFirefoxArchive {
		archive.Close()
		return "", ErrSupport
	}
	receiveErr := receiveFirefox(archive, response.Body)
	closeErr := archive.Close()
	if receiveErr != nil || closeErr != nil {
		return "", ErrSupport
	}
	fixed := filepath.Join(destination, "firefox-1575")
	if err := extractFirefox(ctx, archive.Name(), fixed); err != nil {
		return "", err
	}
	// Playwright 1.65.1 resolves its expected directory name; its target is the
	// explicit, verified 1575 tree, never a silent fallback to the old executable.
	if err := os.Symlink("firefox-1575", filepath.Join(destination, "firefox-1572")); err != nil {
		return "", ErrSupport
	}
	seen := map[string]bool{}
	for _, name := range imageEntries {
		if name == "" || seen[name] || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00\r\n") {
			return "", ErrSupport
		}
		seen[name] = true
		if strings.HasPrefix(name, "firefox-") || name == ".links" {
			continue
		}
		if err := os.Symlink("/ms-playwright/"+name, filepath.Join(destination, name)); err != nil {
			return "", ErrSupport
		}
	}
	ok = true
	return destination, nil
}

func receiveFirefox(output io.Writer, input io.Reader) error {
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(output, digest), io.LimitReader(input, maxFirefoxArchive+1))
	if err != nil || n > maxFirefoxArchive || hex.EncodeToString(digest.Sum(nil)) != firefoxSHA256 {
		return ErrSupport
	}
	return nil
}

type firefoxReader struct {
	context context.Context
	reader  io.Reader
}

func (r firefoxReader) Read(b []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(b)
}

func extractFirefox(ctx context.Context, archive, destination string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return ErrSupport
	}
	defer z.Close()
	if len(z.File) == 0 || len(z.File) > 256 {
		return ErrSupport
	}
	var total uint64
	seen := map[string]bool{}
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" || len(name) > 512 || path.Clean(name) != name || path.IsAbs(name) || (name != "firefox" && !strings.HasPrefix(name, "firefox/")) || strings.ContainsAny(name, "\\:\x00\r\n") || seen[name] || f.Mode()&os.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) || f.UncompressedSize64 > maxFirefoxExpanded {
			return ErrSupport
		}
		total += f.UncompressedSize64
		if total > maxFirefoxExpanded {
			return ErrSupport
		}
		if name == "firefox/firefox" && f.Mode()&0111 == 0 {
			return ErrSupport
		}
		seen[name] = true
	}
	if !seen["firefox/firefox"] {
		return ErrSupport
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return ErrSupport
	}
	for _, f := range z.File {
		if ctx.Err() != nil {
			return ErrSupport
		}
		target := filepath.Join(destination, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if os.MkdirAll(target, 0755) != nil {
				return ErrSupport
			}
			continue
		}
		if os.MkdirAll(filepath.Dir(target), 0755) != nil {
			return ErrSupport
		}
		input, err := f.Open()
		if err != nil {
			return ErrSupport
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			input.Close()
			return ErrSupport
		}
		n, copyErr := io.Copy(output, io.LimitReader(firefoxReader{ctx, input}, int64(f.UncompressedSize64)+1))
		inErr, outErr := input.Close(), output.Close()
		if copyErr != nil || inErr != nil || outErr != nil || n != int64(f.UncompressedSize64) {
			return ErrSupport
		}
		mode := os.FileMode(0644)
		if f.Mode()&0111 != 0 {
			mode = 0755
		}
		if os.Chmod(target, mode) != nil {
			return ErrSupport
		}
	}
	executable := filepath.Join(destination, "firefox", "firefox")
	if st, err := os.Stat(executable); err != nil || !st.Mode().IsRegular() {
		return ErrSupport
	}
	return nil
}
