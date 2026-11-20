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
