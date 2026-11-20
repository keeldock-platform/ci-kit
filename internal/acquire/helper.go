package acquire

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Git classifies a leading quoted path as a named helper. The fixed ! form
// instead invokes exactly one quoted trusted absolute executable; Git alone
// supplies the get/store/erase operation. No source command text is accepted.
func credentialHelperCommand(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 4096 || !utf8.ValidString(path) {
		return "", ErrRefused
	}
	normalized := filepath.ToSlash(path)
	if strings.ContainsAny(normalized, "\\\"'`$;|&<>*?[]{}") {
		return "", ErrRefused
	}
	for _, r := range normalized {
		if unicode.IsControl(r) {
			return "", ErrRefused
		}
	}
	return `!"` + normalized + `"`, nil
}
