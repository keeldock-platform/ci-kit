package collector

import (
	"bytes"
	"encoding/xml"
	"errors"
	junit "github.com/joshdk/go-junit"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrReports = errors.New("required report census incomplete")

const MaxReport = 16 << 20

type ReportExpectation struct {
	Unit, Check, Platform, Variant string
	Shard, Shards                  int
	Assertions, Tests, Reports     []string
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
	Assertions                     []string
}

func reportKey(unit, check, platform, variant string, shard, shards int) string {
	return strings.Join([]string{unit, check, platform, variant, number(shard), number(shards)}, "\x00")
}
func number(n int) string {
	if n < 0 {
		return "-"
	}
	return strconv.Itoa(n)
}
func exact(want, got []string) bool {
	if len(want) != len(got) {
		return false
	}
	set := map[string]bool{}
	for _, v := range want {
		if v == "" || set[v] {
			return false
		}
		set[v] = true
	}
	for _, v := range got {
		if !set[v] {
			return false
		}
		delete(set, v)
	}
	return len(set) == 0
}

// ValidateReports reconciles each unit/check/platform/variant/shard and its
// complete tests/assertions/report paths. Parsing is delegated to go-junit.
// Tests and assertions are private identifiers and never included in errors.
func ValidateReports(c Census, reports []Report) error {
	if len(c.Entries) == 0 || len(c.Entries) > 100000 || len(reports) == 0 || len(reports) > 100000 {
		return ErrReports
	}
	expectations := map[string]ReportExpectation{}
	paths := map[string]bool{}
	actual := map[string][]string{}
	assertions := map[string][]string{}
	reportPaths := map[string][]string{}
	shardTests := map[string]map[string]bool{}
	shards := map[string]map[int]bool{}
	shardCounts := map[string]int{}
	groupAssertions := map[string]int{}
	for _, e := range c.Entries {
		key := reportKey(e.Unit, e.Check, e.Platform, e.Variant, e.Shard, e.Shards)
		if e.Unit == "" || e.Check == "" || e.Platform == "" || e.Shards < 1 || e.Shard < 1 || e.Shard > e.Shards || len(e.Tests) == 0 || len(e.Reports) == 0 {
			return ErrReports
		}
		if _, ok := expectations[key]; ok {
			return ErrReports
		}
		expectations[key] = e
		group := reportKey(e.Unit, e.Check, e.Platform, e.Variant, 0, 0)
		if shards[group] == nil {
			shards[group] = map[int]bool{}
			shardTests[group] = map[string]bool{}
			shardCounts[group] = e.Shards
		}
		if shardCounts[group] != e.Shards || shards[group][e.Shard] {
			return ErrReports
		}
		shards[group][e.Shard] = true
		groupAssertions[group] += len(e.Assertions)
		for _, test := range e.Tests {
			if test == "" || shardTests[group][test] {
				return ErrReports
			}
			shardTests[group][test] = true
		}
	}
	for group, seen := range shards {
		if !c.Feedback && (len(seen) != shardCounts[group] || groupAssertions[group] == 0) {
			return ErrReports
		}
	}
	for _, r := range reports {
		key := reportKey(r.Unit, r.Check, r.Platform, r.Variant, r.Shard, r.Shards)
		if _, ok := expectations[key]; !ok {
			return ErrReports
		}
		if r.Path == "" || filepath.IsAbs(r.Path) || filepath.Clean(r.Path) != r.Path || r.Path == ".." || strings.HasPrefix(r.Path, ".."+string(filepath.Separator)) || paths[key+"\x00"+r.Path] || len(r.Data) == 0 || len(r.Data) > MaxReport || !utf8.Valid(r.Data) {
			return ErrReports
		}
		paths[key+"\x00"+r.Path] = true
		reportPaths[key] = append(reportPaths[key], r.Path)
		assertions[key] = append(assertions[key], r.Assertions...)
		if r.Format != "junit" {
			return ErrReports
		}
		tests, e := PassedTests(r.Data)
		if e != nil {
			return ErrReports
		}
		proven := map[string]bool{}
		for _, test := range tests {
			proven[test] = true
		}
		for _, assertion := range r.Assertions {
			if !proven[assertion] {
				return ErrReports
			}
		}
		actual[key] = append(actual[key], tests...)
	}
	for key, e := range expectations {
		if !exact(e.Tests, actual[key]) || !exact(e.Assertions, assertions[key]) || !exact(e.Reports, reportPaths[key]) {
			return ErrReports
		}
	}
	return nil
}
func passedTests(suites []junit.Suite) ([]string, error) {
	tests := []string{}
	for _, suite := range suites {
		for _, test := range suite.Tests {
			if test.Status != junit.StatusPassed || test.Name == "" {
				return nil, ErrReports
			}
			id := test.Name
			if test.Classname != "" {
				id = test.Classname + "/" + id
			}
			tests = append(tests, id)
		}
		nested, e := passedTests(suite.Suites)
		if e != nil {
			return nil, e
		}
		tests = append(tests, nested...)
	}
	return tests, nil
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

// PassedTests returns private canonical classname/name IDs from a bounded,
// nonempty report only when every observed test passed without skip or retry.
func PassedTests(data []byte) ([]string, error) {
	if len(data) == 0 || len(data) > MaxReport || !utf8.Valid(data) || safeXML(data) != nil {
		return nil, ErrReports
	}
	suites, e := junit.Ingest(data)
	if e != nil {
		return nil, ErrReports
	}
	tests, e := passedTests(suites)
	if e != nil || len(tests) == 0 {
		return nil, ErrReports
	}
	seen := map[string]bool{}
	for _, test := range tests {
		if seen[test] {
			return nil, ErrReports
		}
		seen[test] = true
	}
	return tests, nil
}

// ReportAssertions proves each expected assertion names an actual passed test.
func ReportAssertions(data []byte, want []string) ([]string, error) {
	tests, e := PassedTests(data)
	if e != nil {
		return nil, e
	}
	set := map[string]bool{}
	for _, test := range tests {
		set[test] = true
	}
	out := []string{}
	seen := map[string]bool{}
	for _, w := range want {
		if !set[w] || seen[w] {
			return nil, ErrReports
		}
		seen[w] = true
		out = append(out, w)
	}
	return out, nil
}

// Failure is private diagnostic data, never a public result field.
type Failure struct {
	Test  string `json:"test"`
	Error string `json:"error"`
}

func Failures(data []byte) ([]Failure, error) {
	if len(data) == 0 || len(data) > MaxReport || !utf8.Valid(data) || safeXML(data) != nil {
		return nil, ErrReports
	}
	suites, e := junit.Ingest(data)
	if e != nil {
		return nil, ErrReports
	}
	failures := []Failure{}
	var walk func([]junit.Suite)
	walk = func(suites []junit.Suite) {
		for _, s := range suites {
			for _, t := range s.Tests {
				if t.Status == junit.StatusFailed || t.Status == junit.StatusError {
					message := t.Message
					if t.Error != nil {
						message = strings.TrimSpace(t.Message + "\n" + t.Error.Error())
					}
					name := t.Name
					if t.Classname != "" {
						name = t.Classname + "/" + name
					}
					failures = append(failures, Failure{name, message})
				}
			}
			walk(s.Suites)
		}
	}
	walk(suites)
	return failures, nil
}
