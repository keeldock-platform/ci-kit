package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// CatalogueDigest binds the exact ordered descriptor, variant and shard catalogue.
func CatalogueDigest(units []Unit) (string, error) {
	entries, e := Entries(units)
	if e != nil {
		return "", e
	}
	type identity struct {
		ID         string            `json:"id"`
		Platform   string            `json:"platform"`
		Descriptor string            `json:"descriptor_sha256"`
		Variant    string            `json:"variant"`
		Axes       map[string]string `json:"axes"`
		Shard      string            `json:"shard"`
	}
	bound := make([]identity, 0, len(entries))
	for _, entry := range entries {
		axes := map[string]string{}
		for _, u := range units {
			if u.ID != entry.Unit {
				continue
			}
			for _, variant := range u.Checks[entry.Check].Variants {
				if variant.ID == entry.Variant {
					axes = variant.Axes
				}
			}
		}
		bound = append(bound, identity{entry.ID, entry.Platform, entry.Digest, entry.Variant, axes, entry.Shard})
	}
	b, e := json.Marshal(bound)
	if e != nil {
		return "", ErrInvalid
	}
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:]), nil
}
func entryKey(e Entry) string { return e.ID + "\x00" + e.Variant + "\x00" + e.Shard }

// Selector encodes opaque zero-based catalogue indices for bounded feedback only.
func Selector(units []Unit, selected []Entry) (string, error) {
	if len(selected) == 0 || len(selected) > 256 {
		return "", ErrInvalid
	}
	entries, e := Entries(units)
	if e != nil {
		return "", e
	}
	indices := map[string]int{}
	for i, entry := range entries {
		indices[entryKey(entry)] = i
	}
	seen := map[int]bool{}
	ids := []int{}
	for _, entry := range selected {
		i, ok := indices[entryKey(entry)]
		if !ok || seen[i] {
			return "", ErrInvalid
		}
		seen[i] = true
		ids = append(ids, i)
	}
	sort.Ints(ids)
	encoded := make([]string, len(ids))
	for i, id := range ids {
		encoded[i] = strconv.Itoa(id)
	}
	digest, e := CatalogueDigest(units)
	if e != nil {
		return "", e
	}
	value := "v1:" + digest + ":" + strings.Join(encoded, ",")
	if len(value) > 8192 {
		return "", ErrInvalid
	}
	return value, nil
}

// ResolveSelector refuses stale identity, duplicate indices and noncanonical encodings.
func ResolveSelector(units []Unit, selector string) ([]Entry, error) {
	if len(selector) > 8192 {
		return nil, ErrInvalid
	}
	parts := strings.Split(selector, ":")
	if len(parts) != 3 || parts[0] != "v1" || len(parts[1]) != 64 || parts[2] == "" {
		return nil, ErrInvalid
	}
	digest, e := CatalogueDigest(units)
	if e != nil || parts[1] != digest {
		return nil, ErrInvalid
	}
	entries, e := Entries(units)
	if e != nil {
		return nil, e
	}
	ids := strings.Split(parts[2], ",")
	if len(ids) > 256 {
		return nil, ErrInvalid
	}
	out := make([]Entry, 0, len(ids))
	last := -1
	for _, encoded := range ids {
		id, e := strconv.Atoi(encoded)
		if e != nil || strconv.Itoa(id) != encoded || id <= last || id < 0 || id >= len(entries) {
			return nil, ErrInvalid
		}
		last = id
		out = append(out, entries[id])
	}
	return out, nil
}
