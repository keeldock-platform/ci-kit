package collector

import (
	"strings"
	"testing"
)

func good() (Expected, Observed) {
	e := Expected{RunID: 8, Attempt: 2, Slots: []Slot{{"linux", "checks"}, {"windows", "checks"}}, Finalizer: "finalize"}
	o := Observed{Jobs: []Job{{ID: 1, RunID: 8, Attempt: 2, Platform: "linux", Slot: "checks", Status: "completed", Conclusion: "success"}, {ID: 2, RunID: 8, Attempt: 2, Platform: "windows", Slot: "checks", Status: "completed", Conclusion: "success"}, {ID: 3, RunID: 8, Attempt: 2, Slot: "finalize", Status: "completed", Conclusion: "success"}}, Needs: map[string]string{"linux/checks": "success", "windows/checks": "success"}}
	return e, o
}
func TestOnlyCompleteExpectedAttemptPasses(t *testing.T) {
	e, o := good()
	v := Evaluate(e, o)
	if !v.Passed || !v.Complete {
		t.Fatal("valid exact set rejected")
	}
}
func TestNonPassingProviderConclusionsRefused(t *testing.T) {
	for _, state := range []string{"failure", "cancelled", "skipped", "neutral", "timed_out", "action_required", "stale", "unknown", ""} {
		t.Run(state, func(t *testing.T) {
			e, o := good()
			o.Jobs[0].Conclusion = state
			if Evaluate(e, o).Passed {
				t.Fatal("nonpassing result admitted")
			}
		})
	}
}
func TestMissingDuplicateUnknownAttemptAndFinalizerRefused(t *testing.T) {
	tests := map[string]func(*Expected, *Observed){"missing": func(e *Expected, o *Observed) { o.Jobs = o.Jobs[1:] }, "duplicate": func(e *Expected, o *Observed) { o.Jobs = append(o.Jobs, o.Jobs[0]) }, "unknown": func(e *Expected, o *Observed) { o.Jobs[0].Slot = "unknown" }, "attempt": func(e *Expected, o *Observed) { o.Jobs[0].Attempt = 1 }, "run": func(e *Expected, o *Observed) { o.Jobs[0].RunID = 9 }, "finalizer": func(e *Expected, o *Observed) { o.Jobs = o.Jobs[:2] }, "running": func(e *Expected, o *Observed) { o.Jobs[0].Status = "in_progress" }, "zero": func(e *Expected, o *Observed) { e.Slots = nil }, "native-needs": func(e *Expected, o *Observed) { o.Needs = nil }, "native-cancel": func(e *Expected, o *Observed) { o.Needs["linux/checks"] = "cancelled" }, "unknown-needs": func(e *Expected, o *Observed) { o.Needs["other/checks"] = "success" }, "duplicate-id": func(e *Expected, o *Observed) { o.Jobs[1].ID = o.Jobs[0].ID }}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			e, o := good()
			mutate(&e, &o)
			if Evaluate(e, o).Passed {
				t.Fatal("invalid set admitted")
			}
		})
	}
}
func TestActualFailureSurvivesCancellationAndMissingEvidence(t *testing.T) {
	e, o := good()
	o.Jobs[0].Conclusion = "failure"
	o.Jobs[1].Conclusion = "cancelled"
	o.Jobs = o.Jobs[:2]
	v := Evaluate(e, o)
	if v.Passed || v.FirstFailure != "linux/checks" {
		t.Fatal("first executed failure lost")
	}
}
func TestPublicLineCannotPublishUnknownNames(t *testing.T) {
	for _, name := range []string{"private-feature", "checks\n::error::oops"} {
		if line, ok := PublicLine(name, true, map[string]bool{"checks": true}); ok || line != "" {
			t.Fatal("unknown slot published")
		}
	}
	line, ok := PublicLine("checks", false, map[string]bool{"checks": true})
	if !ok || strings.TrimSpace(line) != "checks FAIL" {
		t.Fatal("fixed output changed")
	}
}
func TestGlobalPreflightAndPlanSlotsPassWithNativeNeeds(t *testing.T) {
	e, o := good()
	for i, name := range []string{"preflight", "plan"} {
		e.Slots = append(e.Slots, Slot{Slot: name})
		o.Jobs = append(o.Jobs, Job{ID: int64(10 + i), RunID: 8, Attempt: 2, Slot: name, Status: "completed", Conclusion: "success"})
		o.Needs["/"+name] = "success"
	}
	if !Evaluate(e, o).Passed {
		t.Fatal("fixed global slots rejected")
	}
}
