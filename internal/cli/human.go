package cli

import (
	"encoding/json"
	"fmt"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"sort"
	"strconv"
	"strings"
)

func (a *app) writeHuman() {
	r := a.result
	fmt.Fprintf(a.out, "%s: %s\n", r.Command, r.State)
	if r.Identity.URL != nil {
		fmt.Fprintln(a.out, *r.Identity.URL)
	}
	if r.Scope == "hosted" {
		rows := humanStages(r)
		for _, row := range rows {
			if r.Command == "failures" && (row.State == "passed" || row.State == "skipped") {
				continue
			}
			name := row.Slot
			if row.Platform != nil {
				name += " " + *row.Platform
			}
			fmt.Fprintf(a.out, "%s: %s\n", name, row.State)
		}
		fmt.Fprintf(a.out, "admission eligible: %t\n", r.AdmissionEligible)
		if r.Operation != nil {
			if r.Command == "dispatch" {
				fmt.Fprintf(a.out, "dispatch accepted: %t\n", r.Operation.DispatchAccepted)
			}
			if r.Command == "cancel" {
				fmt.Fprintf(a.out, "cancellation requested: %t, confirmed: %t\n", r.Operation.CancelRequested, r.Operation.CancelConfirmed)
			}
		}
		if r.Command == "dispatch" && r.Identity.RequestID != nil {
			fmt.Fprintln(a.out, "request:", *r.Identity.RequestID)
		}
		a.humanNext(r)
	} else if r.Local != nil {
		if r.OK {
			switch r.Command {
			case "version":
				fmt.Fprintln(a.out, Version)
			case "list":
				var data struct {
					Units []struct {
						ID, Owner string
						Platforms []string
					}
				}
				b, _ := json.Marshal(r.Local.Data)
				json.Unmarshal(b, &data)
				for _, unit := range data.Units {
					fmt.Fprintf(a.out, "%s owner=%s platforms=%s\n", unit.ID, strconv.QuoteToGraphic(unit.Owner), strings.Join(unit.Platforms, ","))
				}
			case "explain":
				for _, unit := range r.Local.Units {
					fmt.Fprintln(a.out, "selected:", unit)
				}
				for _, reason := range r.Local.Explanation {
					fmt.Fprintln(a.out, reason)
				}
			case "results purge":
				var data struct {
					Deleted  []string `json:"deleted_result_ids"`
					Retained int      `json:"retained_unknown_count"`
				}
				b, _ := json.Marshal(r.Local.Data)
				json.Unmarshal(b, &data)
				for _, id := range data.Deleted {
					fmt.Fprintln(a.out, "deleted:", id)
				}
				fmt.Fprintf(a.out, "retained entries: %d\n", data.Retained)
			}
		}
		if r.Command == "run" {
			fmt.Fprintln(a.out, "admission eligible: false")
			if r.Local.ChildExit != nil {
				fmt.Fprintf(a.out, "child exit: %d\n", *r.Local.ChildExit)
			}
			if r.Local.ChildSignal != nil {
				fmt.Fprintln(a.out, "child signal:", strconv.QuoteToGraphic(*r.Local.ChildSignal))
			}
			if r.Local.LogDir != nil {
				fmt.Fprintln(a.out, "logs:", strconv.QuoteToGraphic(*r.Local.LogDir))
			}
			for _, stage := range r.Stages {
				fmt.Fprintf(a.out, "%s: %s\n", stage.Slot, stage.State)
			}
		}
	}
	if r.Error != nil {
		fmt.Fprintln(a.err, "error:", r.Error.Message)
	}
}
func humanStages(r protocol.Result) []protocol.Stage {
	rows := append([]protocol.Stage{}, r.Stages...)
	if r.Command == "status" || r.Command == "wait" || r.Command == "failures" {
		for _, platform := range r.Coverage.Expected {
			found := false
			for _, row := range rows {
				found = found || row.Slot == "checks" && row.Platform != nil && *row.Platform == platform
			}
			if !found {
				rows = append(rows, protocol.Stage{Slot: "checks", Platform: protocol.Ptr(platform), State: "missing"})
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Slot, rows[j].Slot
		if rows[i].Platform != nil {
			a += "/" + *rows[i].Platform
		}
		if rows[j].Platform != nil {
			b += "/" + *rows[j].Platform
		}
		return a < b
	})
	return rows
}
func (a *app) humanNext(r protocol.Result) {
	if r.Command == "cancel" && r.Identity.RunID != nil && r.Identity.Concern != nil {
		fmt.Fprintf(a.out, "next: kd-ci status %s --concern %s\n", *r.Identity.RunID, *r.Identity.Concern)
		return
	}
	if (r.State == "queued" || r.State == "running") && r.Identity.RunID != nil && r.Identity.Concern != nil {
		fmt.Fprintf(a.out, "next: kd-ci wait %s --concern %s", *r.Identity.RunID, *r.Identity.Concern)
		if r.Identity.Attempt != nil {
			fmt.Fprintf(a.out, " --attempt %d", *r.Identity.Attempt)
		}
		fmt.Fprintln(a.out)
		return
	}
	if r.Command == "failures" || r.ExitCode == 1 || r.ExitCode == 6 {
		fmt.Fprintln(a.out, "next: kd-ci list")
	}
}
