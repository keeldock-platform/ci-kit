package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sample struct {
	Value  int      `json:"value"`
	Nested []sample `json:"nested,omitempty"`
}

func TestDecodeRejectsAmbiguity(t *testing.T) {
	cases := []string{`{"value":1,"value":2}`, `{"value":1,"Value":2}`, `{"VALUE":1}`, `{"value":1,"unknown":2}`, `{"value":1} {}`, `{"value":"\ud800"}`, `{"value":"\udc00"}`, `{"value":1,}`, string([]byte{'{', '"', 0xff, '"', ':', '1', '}'}), strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34)}
	for i, s := range cases {
		var v sample
		if Decode([]byte(s), &v, 1024) == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
func TestDecodeBoundsAndValidUnicode(t *testing.T) {
	var v struct {
		Value string `json:"value"`
	}
	b := []byte(`{"value":"\ud83d\ude80"}`)
	if Decode(b, &v, 1024) != nil || v.Value != "🚀" {
		t.Fatal("valid surrogate pair refused")
	}
	if Decode(b, &v, 2) == nil {
		t.Fatal("size accepted")
	}
	var generic any
	if Decode([]byte(`{"x":[1,true,null]}`), &generic, 1024) != nil {
		t.Fatal("generic document refused")
	}
}
func TestSafePathRejectsEscapeAndLinks(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"../escape", "a/../b", "/absolute", "a\\b", "a:b", "a\nb", "CON", "a//b", "trailing.", "trailing "} {
		if _, e := SafePath(root, p); e == nil {
			t.Fatalf("accepted case %q", p)
		}
	}
	if _, e := SafePath(root, "new/child.json"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "file"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := SafePath(root, "file/child"); e == nil {
		t.Fatal("file parent accepted")
	}
	if os.Symlink(t.TempDir(), filepath.Join(root, "link")) == nil {
		if _, e := SafePath(root, "link/child"); e == nil {
			t.Fatal("symlink accepted")
		}
	}
}
func TestRepositoryExampleAndClosedFields(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, ".kd-ci.json")
	valid := `{"schema_version":1,"concern":"example","kit_api":1,"unit_roots":["."],"shared_units":"ci/units.json","composition_lock":"ci/composition.json","local_policy":"developer-machine","results_dir":".ci-local/results","retention_days":7}`
	if os.WriteFile(p, []byte(valid), 0600) != nil {
		t.Fatal("write")
	}
	if _, e := Load(root, ""); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1), strings.Replace(valid, `"concern":"example"`, `"Concern":"example"`, 1), strings.Replace(valid, `"unit_roots":["."]`, `"unit_roots":[".","."]`, 1), strings.Replace(valid, `"results_dir":".ci-local/results"`, `"results_dir":"../escape"`, 1), strings.Replace(valid, `"retention_days":7`, `"retention_days":31`, 1)} {
		os.WriteFile(p, []byte(s), 0600)
		if _, e := Load(root, ""); e == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
func FuzzDecode(f *testing.F) {
	for _, s := range []string{`{}`, `{"value":1}`, `{"value":1,"Value":2}`, `{"value":"\ud800"}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) { var v sample; _ = Decode(b, &v, 1<<20) })
}
func FuzzSafePath(f *testing.F) {
	for _, s := range []string{".", "a/b", "../x", "CON", "a\\b"} {
		f.Add(s)
	}
	root := f.TempDir()
	f.Fuzz(func(t *testing.T, s string) { _, _ = SafePath(root, s) })
}
func TestCompositionRejectsDuplicateAliasAndUnknownVersion(t *testing.T) {
	sha := strings.Repeat("a", 40)
	valid := `{"schema_version":1,"sources":[{"alias":"example","sha":"` + sha + `","base_sha":"` + sha + `"}]}`
	if _, e := DecodeComposition([]byte(valid)); e != nil {
		t.Fatal(e)
	}
	bad := strings.Replace(valid, `"sources":[`, `"sources":[{"alias":"example","sha":"`+sha+`","base_sha":"`+sha+`"},`, 1)
	if _, e := DecodeComposition([]byte(bad)); e == nil {
		t.Fatal("duplicate source alias accepted")
	}
	if _, e := DecodeComposition([]byte(strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1))); e == nil {
		t.Fatal("unknown major accepted")
	}
}

func TestRawMessagesAndTimeRetainStrictStructure(t *testing.T) {
	var input map[string]json.RawMessage
	if Decode([]byte(`{"request":{"value":1},"array":[1],"text":"ok"}`), &input, 1024) != nil || string(input["text"]) != `"ok"` {
		t.Fatal("valid raw protocol refused")
	}
	if Decode([]byte(`{"request":{"value":1,"value":2}}`), &input, 1024) == nil {
		t.Fatal("raw duplicate bypass")
	}
	want := struct {
		Created time.Time `json:"created"`
	}{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	b, _ := json.Marshal(want)
	var got struct {
		Created time.Time `json:"created"`
	}
	if Decode(b, &got, 1024) != nil || !got.Created.Equal(want.Created) {
		t.Fatal("time roundtrip refused")
	}
	if Decode([]byte(`{"created":{"private":1}}`), &got, 1024) == nil {
		t.Fatal("invalid time accepted")
	}
}
func TestSelectedCompositionDigestAndSingleSourceIdentity(t *testing.T) {
	single := SingleSourceComposition()
	one, e := CompositionDigest(single)
	if e != nil || len(one) != 64 {
		t.Fatal("single source identity")
	}
	again, e := CompositionDigest(Composition{SchemaVersion: 1, Sources: []LockedSource{}})
	if e != nil || one != again {
		t.Fatal("single source drift")
	}
	a := LockedSource{Alias: "alpha", SHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40)}
	b := LockedSource{Alias: "beta", SHA: strings.Repeat("c", 40), BaseSHA: strings.Repeat("d", 40)}
	first, e := CompositionDigest(Composition{1, []LockedSource{a, b}})
	if e != nil {
		t.Fatal(e)
	}
	reordered, e := CompositionDigest(Composition{1, []LockedSource{b, a}})
	if e != nil || first != reordered {
		t.Fatal("canonical order drift")
	}
	a.SHA = strings.Repeat("e", 40)
	changed, e := CompositionDigest(Composition{1, []LockedSource{a, b}})
	if e != nil || changed == first {
		t.Fatal("changed dependency reused stale composition")
	}
}
