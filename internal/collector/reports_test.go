package collector

import (
	"strings"
	"testing"
)

func reportFixture() (Census, []Report) {
	e := ReportExpectation{Unit: "unit-a", Check: "unit", Platform: "linux", Variant: "default", Shard: 1, Shards: 1, Assertions: []string{"suite/case-a"}, Tests: []string{"suite/case-a"}, Reports: []string{"report.xml"}}
	r := Report{Unit: e.Unit, Check: e.Check, Platform: e.Platform, Variant: e.Variant, Shard: 1, Shards: 1, Assertions: e.Assertions, Path: "report.xml", Format: "junit", Data: []byte(`<testsuites><testsuite name="suite"><testcase classname="suite" name="case-a"/></testsuite></testsuites>`)}
	return Census{Entries: []ReportExpectation{e}}, []Report{r}
}
func TestMaintainedJUnitParserRequiresExactCensus(t *testing.T) {
	c, r := reportFixture()
	if e := ValidateReports(c, r); e != nil {
		t.Fatal(e)
	}
}
func TestReportsRejectMalformedZeroSkippedFailedAndExternalEntities(t *testing.T) {
	cases := map[string]string{"empty": "", "zero": `<testsuite name="suite" tests="9"/>`, "malformed": `<testsuite><testcase name="a"></testsuite>`, "skipped": `<testsuite><testcase classname="suite" name="case-a"><skipped/></testcase></testsuite>`, "failed": `<testsuite><testcase classname="suite" name="case-a"><failure/></testcase></testsuite>`, "doctype": `<!DOCTYPE testsuite SYSTEM "file:///private"><testsuite/>`, "entity": `<!DOCTYPE testsuite [<!ENTITY x SYSTEM "file:///private">]><testsuite>&x;</testsuite>`, "duplicate": `<testsuite><testcase classname="suite" name="case-a"/><testcase classname="suite" name="case-a"/></testsuite>`, "roots": `<testsuite/><testsuite/>`, "unknown": `<unknown><testsuite/></unknown>`, "trailing": `<testsuite/>unexpected`}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			c, r := reportFixture()
			r[0].Data = []byte(data)
			if ValidateReports(c, r) == nil {
				t.Fatal("incomplete report accepted")
			}
		})
	}
}
func TestReportsRejectMissingExtraPathsAssertionsVariantAndShard(t *testing.T) {
	tests := map[string]func(*Census, *[]Report){"missing": func(c *Census, r *[]Report) { *r = nil }, "duplicate-report": func(c *Census, r *[]Report) { *r = append(*r, (*r)[0]) }, "path": func(c *Census, r *[]Report) { (*r)[0].Path = "../report.xml" }, "assertion": func(c *Census, r *[]Report) { (*r)[0].Assertions = nil }, "variant": func(c *Census, r *[]Report) { (*r)[0].Variant = "other" }, "missing-shard": func(c *Census, r *[]Report) { c.Entries[0].Shards = 2; (*r)[0].Shards = 2 }, "oversize": func(c *Census, r *[]Report) { (*r)[0].Data = []byte(strings.Repeat("x", MaxReport+1)) }}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c, r := reportFixture()
			mutate(&c, &r)
			if ValidateReports(c, r) == nil {
				t.Fatal("incomplete census accepted")
			}
		})
	}
}
func TestShardUnionMustBeCompleteAndDisjoint(t *testing.T) {
	c, r := reportFixture()
	c.Entries[0].Shards = 2
	r[0].Shards = 2
	e := c.Entries[0]
	e.Shard = 2
	e.Tests = []string{"suite/case-b"}
	e.Assertions = []string{"suite/case-b"}
	c.Entries = append(c.Entries, e)
	other := r[0]
	other.Shard = 2
	other.Assertions = e.Assertions
	other.Data = []byte(`<testsuite><testcase classname="suite" name="case-b"/></testsuite>`)
	r = append(r, other)
	if e := ValidateReports(c, r); e != nil {
		t.Fatal(e)
	}
	c.Entries[1].Tests = []string{"suite/case-a"}
	if ValidateReports(c, r) == nil {
		t.Fatal("overlapping shard accepted")
	}
}

func TestFeedbackExplicitlyAllowsSelectedShardOnly(t *testing.T) {
	c, r := reportFixture()
	c.Feedback = true
	c.Entries[0].Shards = 2
	r[0].Shards = 2
	if e := ValidateReports(c, r); e != nil {
		t.Fatal(e)
	}
	c.Feedback = false
	if ValidateReports(c, r) == nil {
		t.Fatal("partial admission census accepted")
	}
}
func TestAssertionsMustBeActualPassedTestIDs(t *testing.T) {
	c, r := reportFixture()
	c.Entries[0].Assertions = []string{"invented"}
	r[0].Assertions = []string{"invented"}
	if ValidateReports(c, r) == nil {
		t.Fatal("self-reported assertion accepted")
	}
}

func TestFeedbackSubsetMayOmitGlobalAssertionTests(t *testing.T) {
	c, r := reportFixture()
	c.Feedback = true
	c.Entries[0].Assertions = nil
	r[0].Assertions = nil
	if e := ValidateReports(c, r); e != nil {
		t.Fatal("exact feedback subset with no global assertion refused")
	}
	c.Feedback = false
	if ValidateReports(c, r) == nil {
		t.Fatal("admission with zero assertion union accepted")
	}
}

// Match the census too, so invalid paths cannot pass merely by being expected.
func TestReportsUseCanonicalSlashPathsOnEveryHost(t *testing.T) {
	for _, platform := range []string{"windows-x64", "windows-arm64", "linux"} {
		for _, name := range []string{"report.xml", "plugin-alpha/reports/unit.xml", "folder with spaces/reports/unit.xml", strings.Repeat("segment/", 50) + "report.xml"} {
			t.Run(platform+"/"+name, func(t *testing.T) {
				c, r := reportFixture()
				c.Entries[0].Platform, r[0].Platform = platform, platform
				c.Entries[0].Reports, r[0].Path = []string{name}, name
				if err := ValidateReports(c, r); err != nil {
					t.Fatal("canonical report refused:", err)
				}
			})
		}
	}
}
func TestReportsRejectPathAliasesAndTraversal(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../report.xml", "../../report.xml", "a/../../report.xml", "a/../report.xml", "./report.xml", "a/./report.xml", "a//report.xml", "report.xml/", "/report.xml", "//server/share/report.xml", `\report.xml`, `a\report.xml`, `..\report.xml`, `C:/report.xml`, `C:report.xml`, `C:\report.xml`, `\\server\share\report.xml`, `\\?\C:\report.xml`, `//?/C:/report.xml`, `report.xml:stream`, "a\x00/report.xml", "a\r/report.xml", "a\n/report.xml", "a\t/report.xml"} {
		t.Run(name, func(t *testing.T) {
			c, r := reportFixture()
			c.Entries[0].Reports, r[0].Path = []string{name}, name
			if ValidateReports(c, r) == nil {
				t.Fatal("noncanonical report accepted")
			}
		})
	}
}
func TestReportsRejectCanonicalDuplicatesAndSubstitution(t *testing.T) {
	for _, mutation := range []string{"duplicate", "substitution", "format", "extra-test"} {
		t.Run(mutation, func(t *testing.T) {
			c, r := reportFixture()
			c.Entries[0].Reports, r[0].Path = []string{"plugin-alpha/reports/unit.xml"}, "plugin-alpha/reports/unit.xml"
			switch mutation {
			case "duplicate":
				r = append(r, r[0])
				c.Entries[0].Reports = append(c.Entries[0].Reports, r[0].Path)
			case "substitution":
				r[0].Path = "plugin-beta/reports/unit.xml"
			case "format":
				r[0].Format = "other"
			case "extra-test":
				r[0].Data = []byte(`<testsuite><testcase classname="suite" name="case-a"/><testcase name="extra"/></testsuite>`)
			}
			if ValidateReports(c, r) == nil {
				t.Fatal("incomplete census accepted")
			}
		})
	}
}
