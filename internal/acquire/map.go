// Package acquire fetches registered immutable source through captured Git.
// The caller owns trusted workflow/actor admission and current signer authority.
package acquire

import (
	"errors"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"regexp"
	"strings"
	"unicode"
)

var ErrRefused = errors.New("source acquisition refused")
var aliasRE = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,95}$`)
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
var slotRE = regexp.MustCompile(`^SOURCE_READ_(0[1-9]|[12][0-9]|3[0-2])$`)

type Source struct {
	Alias        string `json:"alias"`
	Owner        string `json:"owner"`
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repository_id"`
	Branch       string `json:"branch"`
	SecretSlot   string `json:"secret_slot"`
}
type SourceMap struct {
	SchemaVersion int      `json:"schema_version"`
	Concern       string   `json:"concern"`
	Root          string   `json:"root"`
	Sources       []Source `json:"sources"`
}

// ParseMap bounds SOURCE_MAP before any private routing or network operation.
func ParseMap(data []byte, concern string) (SourceMap, error) {
	var m SourceMap
	if config.Decode(data, &m, 16<<10) != nil {
		return m, ErrRefused
	}
	if m.SchemaVersion != 1 || m.Concern != concern || !aliasRE.MatchString(m.Concern) || !aliasRE.MatchString(m.Root) || len(m.Sources) == 0 || len(m.Sources) > 32 {
		return m, ErrRefused
	}
	aliases, ids, slots := map[string]bool{}, map[int64]bool{}, map[string]bool{}
	root := false
	for _, s := range m.Sources {
		if !aliasRE.MatchString(s.Alias) || !nameRE.MatchString(s.Owner) || !nameRE.MatchString(s.Repository) || s.RepositoryID <= 0 || !slotRE.MatchString(s.SecretSlot) || !validBranch(s.Branch) || aliases[s.Alias] || ids[s.RepositoryID] || slots[s.SecretSlot] {
			return m, ErrRefused
		}
		if s.Alias == m.Root {
			root = true
			if s.SecretSlot != "SOURCE_READ_01" {
				return m, ErrRefused
			}
		} else if s.SecretSlot == "SOURCE_READ_01" {
			return m, ErrRefused
		}
		aliases[s.Alias] = true
		ids[s.RepositoryID] = true
		slots[s.SecretSlot] = true
	}
	if !root {
		return m, ErrRefused
	}
	return m, nil
}
func validBranch(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, part := range strings.Split(s, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return s != "" && len(s) <= 200 && !strings.ContainsAny(s, " ~^:?*[\\\x00\r\n") && !strings.Contains(s, "..") && !strings.Contains(s, "@{") && !strings.HasPrefix(s, "-") && !strings.HasPrefix(s, "/") && !strings.HasSuffix(s, "/") && !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, ".lock") && !strings.Contains(s, "//")
}
