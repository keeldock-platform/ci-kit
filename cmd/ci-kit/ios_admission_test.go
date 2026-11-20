package main

import (
	"context"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/packs"
	"testing"
)

func TestIOSMissingAdmissionVariantCreatesNoDevices(t *testing.T) {
	created := 0
	factory := func(context.Context, packs.IOSSupport, packs.Execute) (packs.Adapter, error) {
		created++
		return packs.Host(), nil
	}
	support := packs.IOSSupport{Devices: []packs.IOSDevice{{Variant: "phone"}, {Variant: "tablet"}}}
	selected := []planner.Invocation{{Variant: "phone"}}
	if _, e := acquireIOSAdapter(context.Background(), support, nil, selected, true, factory); e == nil {
		t.Fatal("incomplete admission admitted")
	}
	if created != 0 {
		t.Fatal("devices created before missing-variant refusal")
	}
	selected = append(selected, planner.Invocation{Variant: "tablet"})
	if _, e := acquireIOSAdapter(context.Background(), support, nil, selected, true, factory); e != nil || created != 1 {
		t.Fatal("complete admission refused")
	}
	if _, e := acquireIOSAdapter(context.Background(), support, nil, selected[:1], false, factory); e != nil || created != 2 {
		t.Fatal("single-variant feedback refused")
	}
}
