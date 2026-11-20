// Package config validates bounded, non-executable local configuration.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid configuration")

// ValidateJSON checks the bounded JSON token structure without closing provider-specific fields.
func ValidateJSON(data []byte, max int) error {
	if len(data) > max || !utf8.Valid(data) || !validEscapes(data) {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if value(dec, 0) != nil {
		return ErrInvalid
	}
	if _, e := dec.Token(); e != io.EOF {
		return ErrInvalid
	}
	return nil
}

// Decode rejects ambiguous JSON before decoding with unknown-field checking.
func Decode(data []byte, dst any, max int) error {
	if ValidateJSON(data, max) != nil {
		return ErrInvalid
	}
	var raw any
	check := json.NewDecoder(bytes.NewReader(data))
	check.UseNumber()
	if check.Decode(&raw) != nil || !exactFields(raw, reflect.TypeOf(dst)) {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return ErrInvalid
	}
	return nil
}
func value(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrInvalid
	}
	t, e := d.Token()
	if e != nil {
		return e
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := k.(string)
			if !ok || seen[s] {
				return ErrInvalid
			}
			seen[s] = true
			if e = value(d, depth+1); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	case '[':
		for d.More() {
			if e = value(d, depth+1); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	return ErrInvalid
}

// encoding/json deliberately replaces invalid surrogate escapes; configuration cannot.
func validEscapes(b []byte) bool {
	in := false
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			in = !in
			continue
		}
		if !in || b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		n, ok := hex4(b, i+1)
		if !ok {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			lo, ok := hex4(b, i+3)
			if !ok || lo < 0xdc00 || lo > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
func hex4(b []byte, i int) (int, bool) {
	if i+4 > len(b) {
		return 0, false
	}
	n := 0
	for _, c := range b[i : i+4] {
		n *= 16
		switch {
		case c >= '0' && c <= '9':
			n += int(c - '0')
		case c >= 'a' && c <= 'f':
			n += int(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			n += int(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return n, true
}

// SafePath resolves a repository-relative path without following symlinks.
func SafePath(root, rel string) (string, error) {
	if rel == "" || !utf8.ValidString(rel) || filepath.IsAbs(rel) || strings.ContainsAny(rel, "\\:") || strings.HasPrefix(rel, "/") {
		return "", ErrInvalid
	}
	for _, c := range rel {
		if unicode.IsControl(c) {
			return "", ErrInvalid
		}
	}
	if rel != "." && filepath.ToSlash(filepath.Clean(rel)) != rel {
		return "", ErrInvalid
	}
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if p == ".." || p == "" || strings.HasSuffix(p, ".") && p != "." || strings.HasSuffix(p, " ") {
			return "", ErrInvalid
		}
		base := strings.ToUpper(strings.Split(p, ".")[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return "", ErrInvalid
		}
	}
	abs, e := filepath.Abs(root)
	if e != nil {
		return "", ErrInvalid
	}
	cur := abs
	st, e := os.Lstat(cur)
	if e != nil || !st.IsDir() || unsafeLink(st) || !safeRoot(abs) {
		return "", ErrInvalid
	}
	for i, p := range parts {
		if p == "." {
			continue
		}
		cur = filepath.Join(cur, p)
		st, e = os.Lstat(cur)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil || unsafeLink(st) || i < len(parts)-1 && !st.IsDir() {
			return "", ErrInvalid
		}
	}
	return cur, nil
}

// Read decodes a regular non-symlink JSON file with a hard size limit.
func Read(path string, dst any, max int) error {
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Size() > int64(max) {
		return ErrInvalid
	}
	f, e := os.Open(path)
	if e != nil {
		return ErrInvalid
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if e != nil {
		return ErrInvalid
	}
	return Decode(b, dst, max)
}

func exactFields(v any, t reflect.Type) bool {
	if t == nil {
		return false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if v == nil {
		return true
	}
	if t == reflect.TypeOf(json.RawMessage{}) {
		return true
	}
	if t == reflect.TypeOf(time.Time{}) {
		s, ok := v.(string)
		if !ok {
			return false
		}
		_, e := time.Parse(time.RFC3339Nano, s)
		return e == nil
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if tag == "" {
				tag = f.Name
			}
			fields[tag] = f.Type
		}
		for k, x := range m {
			ft, ok := fields[k]
			if !ok || !exactFields(x, ft) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		a, ok := v.([]any)
		if !ok {
			return false
		}
		for _, x := range a {
			if !exactFields(x, t.Elem()) {
				return false
			}
		}
	case reflect.Map:
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		for _, x := range m {
			if !exactFields(x, t.Elem()) {
				return false
			}
		}
	}
	return true
}

// NoLinks rejects symbolic links and native reparse points in a fixed policy path.
func NoLinks(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	for p := path; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || unsafeLink(st) {
			return false
		}
		if filepath.Dir(p) == p {
			return true
		}
	}
}
