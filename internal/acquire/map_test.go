package acquire

import (
	"encoding/json"
	"strings"
	"testing"
)

func mapping() SourceMap {
	return SourceMap{SchemaVersion: 1, Concern: "example", Root: "root", Sources: []Source{{Alias: "root", Owner: "sample-owner", Repository: "sample-source", RepositoryID: 42, Branch: "main", SecretSlot: "SOURCE_READ_01"}}}
}
func mapBytes(t *testing.T, m SourceMap) []byte {
	t.Helper()
	data, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return data
}
func TestBoundedCanonicalMap(t *testing.T) {
	m, e := ParseMap(mapBytes(t, mapping()), "example")
	if e != nil || m.Root != "root" {
		t.Fatal("valid mapping refused")
	}
}
func TestMapRejectsDuplicateUnknownCaseAndOversize(t *testing.T) {
	base := string(mapBytes(t, mapping()))
	cases := map[string]string{"duplicate": strings.Replace(base, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1), "unknown": strings.Replace(base, `"schema_version":1`, `"schema_version":1,"unexpected":0`, 1), "case": strings.Replace(base, `"schema_version"`, `"SCHEMA_VERSION"`, 1), "null": `null`, "trailing": base + `{}`, "oversize": strings.Repeat(" ", 16<<10) + base, "invalid-utf8": string([]byte{255})}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, e := ParseMap([]byte(data), "example"); e == nil {
				t.Fatal("unsafe mapping accepted")
			}
		})
	}
}
func TestMapRejectsRoutingAndCredentialExpansion(t *testing.T) {
	cases := map[string]func(*SourceMap){"concern": func(m *SourceMap) { m.Concern = "other" }, "root-slot": func(m *SourceMap) { m.Sources[0].SecretSlot = "SOURCE_READ_02" }, "slot": func(m *SourceMap) { m.Sources[0].SecretSlot = "SOURCE_READ_33" }, "owner": func(m *SourceMap) { m.Sources[0].Owner = "https://elsewhere" }, "branch": func(m *SourceMap) { m.Sources[0].Branch = "main\nother" }, "id": func(m *SourceMap) { m.Sources[0].RepositoryID = 0 }, "duplicate": func(m *SourceMap) { m.Sources = append(m.Sources, m.Sources[0]) }, "duplicate-slot": func(m *SourceMap) {
		s := m.Sources[0]
		s.Alias = "dependency"
		s.RepositoryID = 43
		m.Sources = append(m.Sources, s)
	}}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := mapping()
			mutate(&m)
			if _, e := ParseMap(mapBytes(t, m), "example"); e == nil {
				t.Fatal("routing expansion accepted")
			}
		})
	}
}
func FuzzSourceMap(f *testing.F) {
	b, _ := json.Marshal(mapping())
	f.Add(b)
	f.Add([]byte(`{"schema_version":1,"schema_version":1}`))
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = ParseMap(data, "example") })
}
