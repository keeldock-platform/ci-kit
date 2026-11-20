// Package packs contains the fixed, reviewed public venue contracts.
package packs

import (
	"embed"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
)

//go:embed */venue.json
var venues embed.FS

// Venue binds the public platform slot to its native interpreter and time cap.
type Venue struct {
	SchemaVersion  int    `json:"schema_version"`
	Platform       string `json:"platform"`
	Runner         string `json:"runner"`
	TimeoutMinutes int    `json:"timeout_minutes"`
	NativeOS       string `json:"native_os"`
	NativeArch     string `json:"native_arch"`
}

// Lookup reads only embedded reviewed constants, never candidate configuration.
func Lookup(platform string) (Venue, error) {
	var v Venue
	if platform == "" || strings.ContainsAny(platform, "/\\.") {
		return v, errors.New("unknown venue")
	}
	data, e := venues.ReadFile(platform + "/venue.json")
	if e != nil {
		return v, errors.New("unknown venue")
	}
	if json.Unmarshal(data, &v) != nil || v.SchemaVersion != 1 || v.Platform != platform || v.Runner == "" || v.TimeoutMinutes < 1 || v.TimeoutMinutes > 90 {
		return Venue{}, errors.New("invalid venue")
	}
	return v, nil
}

// Native confirms execution architecture; it does not establish a GUI, container
// user space, simulator, emulator or product assertion capability.
func Native(platform string) bool {
	v, e := Lookup(platform)
	return e == nil && v.NativeOS == runtime.GOOS && v.NativeArch == runtime.GOARCH
}
