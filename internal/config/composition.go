package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/schemas"
	"regexp"
	"sort"
)

type LockedSource struct {
	Alias   string `json:"alias"`
	SHA     string `json:"sha"`
	BaseSHA string `json:"base_sha"`
}
type Composition struct {
	SchemaVersion int            `json:"schema_version"`
	Sources       []LockedSource `json:"sources"`
}

var fullSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)

// DecodeComposition validates the immutable closure without fetching any object.
func DecodeComposition(data []byte) (Composition, error) {
	var c Composition
	if Decode(data, &c, 1<<20) != nil {
		return c, ErrInvalid
	}
	var raw any
	if json.Unmarshal(data, &raw) != nil || schemas.Validate("urn:keeldock:kd-ci:composition:1", raw) != nil {
		return c, ErrInvalid
	}
	seen := map[string]bool{}
	for _, s := range c.Sources {
		if !Alias(s.Alias) || seen[s.Alias] || !fullSHA.MatchString(s.SHA) || !fullSHA.MatchString(s.BaseSHA) {
			return c, ErrInvalid
		}
		seen[s.Alias] = true
	}
	return c, nil
}

// SingleSourceComposition is the deterministic identity for an omitted closure lock.
func SingleSourceComposition() Composition {
	return Composition{SchemaVersion: 1, Sources: []LockedSource{}}
}

// CompositionDigest binds the complete selected immutable closure in canonical alias order.
func CompositionDigest(c Composition) (string, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return "", ErrInvalid
	}
	checked, e := DecodeComposition(b)
	if e != nil {
		return "", e
	}
	sort.Slice(checked.Sources, func(i, j int) bool { return checked.Sources[i].Alias < checked.Sources[j].Alias })
	b, e = json.Marshal(checked)
	if e != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
