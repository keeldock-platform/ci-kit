package packs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/config"
)

type iosAdapter struct {
	run      Execute
	support  IOSSupport
	current  string
	created  map[string]bool
	started  map[string]bool
	variants map[string]string
}
type simulatorList struct {
	Devices map[string][]struct {
		UDID      string `json:"udid"`
		Name      string `json:"name"`
		Available bool   `json:"isAvailable"`
		Type      string `json:"deviceTypeIdentifier"`
		State     string `json:"state"`
	} `json:"devices"`
}

// IOS selects a committed installed runtime and approved device types, then lets
// simctl create explicit per-run UDIDs. Host image UUIDs never enter source locks.
func IOS(ctx context.Context, s IOSSupport, run Execute) (adapter Adapter, err error) {
	if !Native("ios-simulator") {
		return nil, ErrSupport
	}
	if _, e := config.SafePath("/Applications", strings.TrimPrefix(s.DeveloperDirectory, "/Applications/")); e != nil {
		return nil, ErrSupport
	}
	env := map[string]string{"DEVELOPER_DIR": s.DeveloperDirectory}
	out, e := run(ctx, []string{"/usr/bin/xcodebuild", "-version"}, ".", time.Minute, env)
	if e != nil || strings.TrimSpace(string(out)) != "Xcode "+s.XcodeVersion+"\nBuild version "+s.BuildVersion {
		return nil, ErrSupport
	}
	out, e = run(ctx, []string{"/usr/bin/xcrun", "--sdk", "iphonesimulator", "--show-sdk-version"}, ".", time.Minute, env)
	if e != nil || strings.TrimSpace(string(out)) != s.SDKVersion {
		return nil, ErrSupport
	}
	out, e = run(ctx, []string{"/usr/bin/xcrun", "simctl", "list", "runtimes", "--json"}, ".", time.Minute, env)
	if e != nil {
		return nil, ErrSupport
	}
	var runtimes struct {
		Runtimes []struct {
			ID        string `json:"identifier"`
			Available bool   `json:"isAvailable"`
		} `json:"runtimes"`
	}
	if json.Unmarshal(out, &runtimes) != nil {
		return nil, ErrSupport
	}
	found := false
	for _, runtime := range runtimes.Runtimes {
		if runtime.ID == s.Runtime && runtime.Available {
			found = true
		}
	}
	if !found {
		return nil, ErrSupport
	}
	out, e = run(ctx, []string{"/usr/bin/xcrun", "simctl", "list", "devicetypes", "--json"}, ".", time.Minute, env)
	if e != nil {
		return nil, ErrSupport
	}
	var types struct {
		Types []struct {
			ID string `json:"identifier"`
		} `json:"devicetypes"`
	}
	if json.Unmarshal(out, &types) != nil {
		return nil, ErrSupport
	}
	available := map[string]bool{}
	for _, device := range types.Types {
		available[device.ID] = true
	}
	a := &iosAdapter{run: run, support: s, created: map[string]bool{}, started: map[string]bool{}, variants: map[string]string{}}
	success := false
	defer func() {
		if !success {
			if a.Close(context.Background()) != nil {
				err = ErrSupport
			}
		}
	}()
	byType := map[string]string{}
	for _, want := range s.Devices {
		family := "iPhone"
		if want.Family == "ipad" {
			family = "iPad"
		}
		if !available[want.DeviceType] || !strings.Contains(want.DeviceType, "SimDeviceType."+family+"-") {
			return nil, ErrSupport
		}
		if id := byType[want.DeviceType]; id != "" {
			a.variants[want.Variant] = id
			continue
		}
		var nonce [12]byte
		if _, e := rand.Read(nonce[:]); e != nil {
			return nil, ErrSupport
		}
		name := "ci-device-" + hex.EncodeToString(nonce[:])
		out, createErr := run(ctx, []string{"/usr/bin/xcrun", "simctl", "create", name, want.DeviceType, s.Runtime}, ".", time.Minute, env)
		id := strings.TrimSpace(string(out))
		// Recover a just-created identity on a failed client response before cleanup.
		if !udid.MatchString(id) {
			id = ""
			listOut, listErr := run(ctx, []string{"/usr/bin/xcrun", "simctl", "list", "devices", "--json"}, ".", time.Minute, env)
			var list simulatorList
			if listErr == nil && json.Unmarshal(listOut, &list) == nil {
				for _, d := range list.Devices[s.Runtime] {
					if d.Name == name && d.Type == want.DeviceType && udid.MatchString(d.UDID) {
						a.created[d.UDID] = true
						if id != "" {
							return nil, ErrSupport
						}
						id = d.UDID
					}
				}
			}
		}
		if !udid.MatchString(id) {
			return nil, ErrSupport
		}
		a.created[id] = true
		if createErr != nil {
			return nil, ErrSupport
		}
		a.variants[want.Variant] = id
		byType[want.DeviceType] = id
	}
	success = true
	return a, nil
}
func (a *iosAdapter) Wrap(argv []string, source, cwd string) ([]string, string, map[string]string, error) {
	env := map[string]string{"DEVELOPER_DIR": a.support.DeveloperDirectory}
	if a.current != "" {
		env["KD_CI_SIMULATOR_UDID"] = a.current
	}
	return argv, cwd, env, nil
}
func (a *iosAdapter) Before(ctx context.Context, variant string) error {
	device := a.variants[variant]
	if device == "" {
		return ErrSupport
	}
	if a.current == device {
		return nil
	}
	if a.current != "" {
		if e := a.shutdown(ctx, a.current); e != nil {
			return e
		}
	}
	env := map[string]string{"DEVELOPER_DIR": a.support.DeveloperDirectory}
	a.started[device] = true
	a.current = device
	if _, e := a.run(ctx, []string{"/usr/bin/xcrun", "simctl", "boot", device}, ".", time.Minute, env); e != nil {
		return ErrSupport
	}
	if _, e := a.run(ctx, []string{"/usr/bin/xcrun", "simctl", "bootstatus", device, "-b"}, ".", 5*time.Minute, env); e != nil {
		return ErrSupport
	}
	return nil
}
func (a *iosAdapter) shutdown(ctx context.Context, id string) error {
	if !a.created[id] || !a.started[id] {
		return ErrSupport
	}
	if _, e := a.run(ctx, []string{"/usr/bin/xcrun", "simctl", "shutdown", id}, ".", time.Minute, map[string]string{"DEVELOPER_DIR": a.support.DeveloperDirectory}); e != nil {
		return ErrSupport
	}
	delete(a.started, id)
	a.current = ""
	return nil
}
func (a *iosAdapter) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var failure error
	for id := range a.created {
		if a.started[id] {
			if a.shutdown(ctx, id) != nil {
				failure = ErrSupport
			}
		}
		if _, e := a.run(ctx, []string{"/usr/bin/xcrun", "simctl", "delete", id}, ".", time.Minute, map[string]string{"DEVELOPER_DIR": a.support.DeveloperDirectory}); e != nil {
			failure = ErrSupport
		} else {
			delete(a.created, id)
		}
	}
	return failure
}
