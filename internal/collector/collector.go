// Package collector reconciles exact expected results. Provider success is only
// useful after the source tuple and this complete expected set have been bound.
package collector

import "sort"

type Slot struct{ Platform, Slot string }

func (s Slot) Key() string { return s.Platform + "/" + s.Slot }

type Expected struct {
	RunID     int64
	Attempt   int
	Slots     []Slot
	Finalizer string
}
type Job struct {
	ID                                 int64
	RunID                              int64
	Attempt                            int
	Platform, Slot, Status, Conclusion string
}
type Observed struct {
	Jobs  []Job
	Needs map[string]string
}
type Verdict struct {
	Passed, Complete bool
	FirstFailure     string
	Missing, Invalid []Slot
}

func Evaluate(e Expected, o Observed) Verdict {
	v := Verdict{Complete: true, Missing: []Slot{}, Invalid: []Slot{}}
	expected := map[string]Slot{}
	seen := map[string]bool{}
	ids := map[int64]bool{}
	invalid := func(s Slot) { v.Invalid = append(v.Invalid, s); v.Complete = false }
	if e.RunID <= 0 || e.Attempt <= 0 || len(e.Slots) == 0 || e.Finalizer == "" {
		v.Complete = false
		return v
	}
	for _, s := range e.Slots {
		if (s.Platform == "" && s.Slot != "preflight" && s.Slot != "plan") || s.Slot == "" || seen[s.Key()] {
			invalid(s)
		}
		seen[s.Key()] = true
		expected[s.Key()] = s
	}
	seen = map[string]bool{}
	final := Slot{Slot: e.Finalizer}
	if _, ok := expected[final.Key()]; ok {
		invalid(final)
	}
	expected[final.Key()] = final
	// Preserve the first actual provider failure in observation order even when
	// fail-fast cancellation or an incomplete snapshot follows it.
	for _, j := range o.Jobs {
		if j.Conclusion == "failure" && v.FirstFailure == "" {
			v.FirstFailure = (Slot{j.Platform, j.Slot}).Key()
		}
	}
	for _, j := range o.Jobs {
		s := Slot{j.Platform, j.Slot}
		key := s.Key()
		_, known := expected[key]
		if !known || seen[key] || j.ID <= 0 || ids[j.ID] || j.RunID != e.RunID || j.Attempt != e.Attempt {
			invalid(s)
		}
		seen[key] = true
		ids[j.ID] = true
		if j.Status != "completed" {
			v.Complete = false
			invalid(s)
		}
		if j.Conclusion != "success" {
			invalid(s)
		}
	}
	for key, s := range expected {
		if !seen[key] {
			v.Missing = append(v.Missing, s)
			v.Complete = false
		}
	}
	if len(o.Needs) != len(e.Slots) {
		v.Complete = false
	}
	for _, s := range e.Slots {
		if o.Needs[s.Key()] != "success" {
			invalid(s)
		}
	}
	for key := range o.Needs {
		if _, ok := expected[key]; !ok || key == final.Key() {
			v.Complete = false
		}
	}
	sort.Slice(v.Missing, func(i, j int) bool { return v.Missing[i].Key() < v.Missing[j].Key() })
	sort.Slice(v.Invalid, func(i, j int) bool { return v.Invalid[i].Key() < v.Invalid[j].Key() })
	v.Passed = v.Complete && len(v.Invalid) == 0 && len(v.Missing) == 0 && v.FirstFailure == ""
	return v
}

// PublicLine accepts a trusted fixed slot enum supplied by the control plane.
// Private test names and arbitrary upstream messages are never output.
func PublicLine(slot string, passed bool, allowed map[string]bool) (string, bool) {
	if !allowed[slot] {
		return "", false
	}
	if passed {
		return slot + " PASS", true
	}
	return slot + " FAIL", true
}
