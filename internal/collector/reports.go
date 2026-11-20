package collector

import (
	"bytes"
	"encoding/xml"
	"errors"
	junit "github.com/joshdk/go-junit"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrReports = errors.New("required report census incomplete")

const MaxReport = 16 << 20

type ReportExpectation struct {
	Unit, Check, Platform, Variant string
	Shard, Shards                  int
	Assertions, Reports            []string
}
type Census struct {
	Entries  []ReportExpectation
	Feedback bool
}
type Report struct {
	Unit, Check, Platform, Variant string
	Shard, Shards                  int
	Path, Format                   string
	Data                           []byte
}

func reportKey(unit, check, platform, variant string, shard, shards int) string {
	return strings.Join([]string{unit, check, platform, variant, strconv.Itoa(shard), strconv.Itoa(shards)}, "\x00")
}
func exact(want, got []string) bool {
	want = slices.Clone(want)
	got = slices.Clone(got)
	slices.Sort(want)
	slices.Sort(got)
	return len(want) > 0 && !slices.Contains(want, "") && len(slices.Compact(slices.Clone(want))) == len(want) && slices.Equal(want, got)
}

// ValidateReports requires every planned suite/variant/shard and fresh report.
// Maintained runners own bulk discovery, retries and skips. Assertions are only
// explicit mandatory security/privacy invariants, never a bulk test-name census.
// Private identifiers and report contents never appear in errors.
func ValidateReports(c Census, reports []Report) error {
	if len(c.Entries) == 0 || len(c.Entries) > 100000 || len(reports) == 0 || len(reports) > 100000 {
		return ErrReports
	}
	expected := map[string]ReportExpectation{}
	paths := map[string][]string{}
	passed := map[string][]string{}
	shards := map[string]map[int]bool{}
	counts := map[string]int{}
	for _, e := range c.Entries {
		key := reportKey(e.Unit, e.Check, e.Platform, e.Variant, e.Shard, e.Shards)
		group := reportKey(e.Unit, e.Check, e.Platform, e.Variant, 0, 0)
		if e.Unit == "" || e.Check == "" || e.Platform == "" || e.Shards < 1 || e.Shard < 1 || e.Shard > e.Shards || len(e.Reports) == 0 {
			return ErrReports
		}
		if _, ok := expected[key]; ok {
			return ErrReports
		}
		if shards[group] == nil {
			shards[group] = map[int]bool{}
			counts[group] = e.Shards
		}
		if counts[group] != e.Shards || shards[group][e.Shard] {
			return ErrReports
		}
		shards[group][e.Shard] = true
		expected[key] = e
	}
	for group, seen := range shards {
		if !c.Feedback && len(seen) != counts[group] {
			return ErrReports
		}
	}
	for _, r := range reports {
		key := reportKey(r.Unit, r.Check, r.Platform, r.Variant, r.Shard, r.Shards)
		if _, ok := expected[key]; !ok {
			return ErrReports
		}
		if r.Path == "" || filepath.IsAbs(r.Path) || filepath.Clean(r.Path) != r.Path || r.Path == ".." || strings.HasPrefix(r.Path, ".."+string(filepath.Separator)) || r.Format != "junit" {
			return ErrReports
		}
		observed, e := reportTests(r.Data)
		if e != nil {
			return ErrReports
		}
		tests, e := passedTests(observed)
		if e != nil {
			return ErrReports
		}
		for _, test := range observed {
			if test.Status == junit.StatusSkipped {
				for _, name := range expected[key].Assertions {
					if testID(test) == name {
						return ErrReports
					}
				}
			}
		}
		passed[key] = append(passed[key], tests...)
		paths[key] = append(paths[key], r.Path)
	}
	for key, e := range expected {
		if !exact(e.Reports, paths[key]) {
			return ErrReports
		}
		// An invariant must appear exactly once and pass; bulk names are ignored.
		seen := map[string]bool{}
		for _, name := range e.Assertions {
			if name == "" || seen[name] {
				return ErrReports
			}
			seen[name] = true
			n := 0
			for _, actual := range passed[key] {
				if actual == name {
					n++
				}
			}
			if n != 1 {
				return ErrReports
			}
		}
	}
	return nil
}

func reportTests(data []byte) ([]junit.Test, error) {
	if len(data) == 0 || len(data) > MaxReport || !utf8.Valid(data) || safeXML(data) != nil {
		return nil, ErrReports
	}
	suites, e := junit.Ingest(data)
	if e != nil {
		return nil, ErrReports
	}
	tests := []junit.Test{}
	var visit func([]junit.Suite)
	visit = func(suites []junit.Suite) {
		for _, suite := range suites {
			tests = append(tests, suite.Tests...)
			visit(suite.Suites)
		}
	}
	visit(suites)
	if len(tests) == 0 || len(tests) > 100000 {
		return nil, ErrReports
	}
	return tests, nil
}
func testID(t junit.Test) string {
	if t.Classname != "" {
		return t.Classname + "/" + t.Name
	}
	return t.Name
}

// safeXML checks only document safety and resource limits. The maintained JUnit
// parser owns test interpretation. encoding/xml never fetches external entities.
func safeXML(data []byte) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	depth, roots := 0, 0
	for {
		token, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return ErrReports
		}
		switch t := token.(type) {
		case xml.Directive:
			return ErrReports
		case xml.ProcInst:
			if t.Target != "xml" || roots != 0 {
				return ErrReports
			}
		case xml.StartElement:
			depth++
			if depth > 32 {
				return ErrReports
			}
			if depth == 1 {
				roots++
				if roots != 1 || t.Name.Space != "" || (t.Name.Local != "testsuite" && t.Name.Local != "testsuites") {
					return ErrReports
				}
			}
			attrs := map[xml.Name]bool{}
			for _, a := range t.Attr {
				if attrs[a.Name] {
					return ErrReports
				}
				attrs[a.Name] = true
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(t)) > 0 {
				return ErrReports
			}
		}
	}
	if roots != 1 || depth != 0 {
		return ErrReports
	}
	return nil
}

// PassedTests requires real nonzero passed results, rejects runner failures and
// permits ordinary bulk skips/retries. Required invariants are checked separately.
func PassedTests(data []byte) ([]string, error) {
	tests, e := reportTests(data)
	if e != nil {
		return nil, e
	}
	return passedTests(tests)
}
func passedTests(tests []junit.Test) ([]string, error) {
	passed := []string{}
	for _, t := range tests {
		switch t.Status {
		case junit.StatusPassed:
			if t.Name == "" {
				return nil, ErrReports
			}
			passed = append(passed, testID(t))
		case junit.StatusSkipped:
		default:
			return nil, ErrReports
		}
	}
	if len(passed) == 0 {
		return nil, ErrReports
	}
	return passed, nil
}

// Failure is private diagnostic data, never a public result field.
type Failure struct {
	Test  string `json:"test"`
	Error string `json:"error"`
}

func Failures(data []byte) ([]Failure, error) {
	tests, e := reportTests(data)
	if e != nil {
		return nil, e
	}
	failures := []Failure{}
	for _, t := range tests {
		if t.Status == junit.StatusFailed || t.Status == junit.StatusError {
			message := t.Message
			if t.Error != nil {
				message = strings.TrimSpace(t.Message + "\n" + t.Error.Error())
			}
			failures = append(failures, Failure{testID(t), message})
		}
	}
	return failures, nil
}
