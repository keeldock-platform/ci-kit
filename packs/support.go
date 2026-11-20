package packs

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/config"
)

var ErrSupport = errors.New("platform support refused")
var numericVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}$`)
var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,127}$`)
var udid = regexp.MustCompile(`^[A-Fa-f0-9]{8}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{12}$`)

// Support is committed privately alongside the existing pack policy. Missing
// fields refuse execution; no SDK, API, device or dated archive is guessed.
type Support struct {
	RemainingWorkingSetBytes int64             `json:"remaining_working_set_bytes"`
	SchemaVersion            int               `json:"schema_version"`
	Container                *ContainerSupport `json:"container,omitempty"`
	IOS                      *IOSSupport       `json:"ios,omitempty"`
	Android                  *AndroidSupport   `json:"android,omitempty"`
}
type ContainerSupport struct {
	Image             string   `json:"image"`
	Snapshot          string   `json:"snapshot"`
	Packages          []string `json:"packages"`
	PlaywrightVersion string   `json:"playwright_version,omitempty"`
}
type IOSDevice struct {
	Variant    string `json:"variant"`
	DeviceType string `json:"device_type"`
	Family     string `json:"family"`
}
type IOSSupport struct {
	DeveloperDirectory string      `json:"developer_directory"`
	XcodeVersion       string      `json:"xcode_version"`
	BuildVersion       string      `json:"build_version"`
	SDKVersion         string      `json:"sdk_version"`
	Runtime            string      `json:"runtime"`
	Devices            []IOSDevice `json:"devices"`
}
type AndroidImage struct {
	Variant string `json:"variant"`
	API     int    `json:"api"`
	Target  string `json:"target"`
	Profile string `json:"profile"`
}
type AndroidSupport struct {
	CommandLineToolsVersion string            `json:"command_line_tools_version"`
	EmulatorBuild           string            `json:"emulator_build"`
	NDK                     string            `json:"ndk"`
	CMake                   string            `json:"cmake"`
	Images                  []AndroidImage    `json:"images"`
	Packages                map[string]string `json:"packages"`
	ToolSHA256              map[string]string `json:"tool_sha256"`
}

// DecodeSupport shares the strict bounded JSON decoder; private names never
// become error text or public execution arguments.
func DecodeSupport(platform string, data json.RawMessage) (Support, error) {
	var s Support
	if config.Decode(data, &s, 16384) != nil || s.SchemaVersion != 1 || s.RemainingWorkingSetBytes < 1<<30 || s.RemainingWorkingSetBytes > 1<<40 {
		return s, ErrSupport
	}
	switch platform {
	case "linux-debian", "linux-arch", "web-linux":
		if s.Container == nil || s.IOS != nil || s.Android != nil {
			return s, ErrSupport
		}
		c := s.Container
		if c.Image != images[platform] || len(c.Packages) > 128 {
			return s, ErrSupport
		}
		for _, p := range c.Packages {
			if !regexp.MustCompile(`^[a-z0-9][a-z0-9+._-]*(=[A-Za-z0-9+.:~_-]+)?$`).MatchString(p) {
				return s, ErrSupport
			}
		}
		if platform == "linux-debian" {
			if _, dateErr := time.Parse("20060102T150405Z", c.Snapshot); dateErr != nil || len(c.Packages) == 0 {
				return s, ErrSupport
			}
			for _, p := range c.Packages {
				if !strings.Contains(p, "=") {
					return s, ErrSupport
				}
			}
		}
		if platform == "linux-arch" {
			if _, dateErr := time.Parse("2006/01/02", c.Snapshot); dateErr != nil || len(c.Packages) == 0 {
				return s, ErrSupport
			}
			for _, p := range c.Packages {
				if strings.Contains(p, "=") {
					return s, ErrSupport
				}
			}
		}
		if platform == "web-linux" && (c.PlaywrightVersion != "1.65.1" || c.Snapshot != "" || len(c.Packages) != 0) {
			return s, ErrSupport
		}
	case "ios-simulator":
		if s.IOS == nil || s.Container != nil || s.Android != nil {
			return s, ErrSupport
		}
		v := s.IOS
		if !strings.HasPrefix(v.DeveloperDirectory, "/Applications/Xcode_") || !strings.HasSuffix(v.DeveloperDirectory, ".app/Contents/Developer") || strings.ContainsAny(v.DeveloperDirectory, "\x00\r\n") || !numericVersion.MatchString(v.XcodeVersion) || !regexp.MustCompile(`^[0-9]{2}[A-Z][0-9]+[a-z]?$`).MatchString(v.BuildVersion) || !numericVersion.MatchString(v.SDKVersion) || !regexp.MustCompile(`^com\.apple\.CoreSimulator\.SimRuntime\.iOS-[0-9]+(-[0-9]+){1,2}$`).MatchString(v.Runtime) || len(v.Devices) < 2 || len(v.Devices) > 64 {
			return s, ErrSupport
		}
		families := map[string]bool{}
		variants := map[string]bool{}
		for _, d := range v.Devices {
			if !config.Alias(d.Variant) || variants[d.Variant] || !regexp.MustCompile(`^com\.apple\.CoreSimulator\.SimDeviceType\.(iPhone|iPad)-[A-Za-z0-9.-]+$`).MatchString(d.DeviceType) || (d.Family != "iphone" && d.Family != "ipad") {
				return s, ErrSupport
			}
			variants[d.Variant] = true
			families[d.Family] = true
		}
		if !families["iphone"] || !families["ipad"] {
			return s, ErrSupport
		}
	case "android-emulator":
		if s.Android == nil || s.Container != nil || s.IOS != nil {
			return s, ErrSupport
		}
		v := s.Android
		if !numericVersion.MatchString(v.CommandLineToolsVersion) || !regexp.MustCompile(`^[0-9]{6,12}$`).MatchString(v.EmulatorBuild) || !numericVersion.MatchString(v.NDK) || !numericVersion.MatchString(v.CMake) || len(v.Images) < 2 || len(v.Images) > 64 {
			return s, ErrSupport
		}
		apis := map[int]bool{}
		variants := map[string]bool{}
		for _, image := range v.Images {
			if !config.Alias(image.Variant) || variants[image.Variant] || image.API < 21 || image.API > 99 || !safeName.MatchString(image.Profile) || (image.Target != "default" && image.Target != "google_apis" && image.Target != "google_atd" && image.Target != "aosp_atd") {
				return s, ErrSupport
			}
			variants[image.Variant] = true
			apis[image.API] = true
		}
		if len(v.Packages) < 6 || len(v.Packages) > 64 || len(v.ToolSHA256) < 2 || len(v.ToolSHA256) > 64 {
			return s, ErrSupport
		}
		for path, version := range v.Packages {
			if !regexp.MustCompile(`^[A-Za-z0-9._-]+(;[A-Za-z0-9._-]+)*$`).MatchString(path) || !regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}$`).MatchString(version) {
				return s, ErrSupport
			}
		}
		for path, digest := range v.ToolSHA256 {
			if !regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`).MatchString(path) || strings.Contains(path, "..") || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest) {
				return s, ErrSupport
			}
		}
		for _, path := range []string{"platform-tools/adb", "emulator/emulator", "cmdline-tools/latest/bin/sdkmanager", "cmdline-tools/latest/bin/avdmanager"} {
			if v.ToolSHA256[path] == "" {
				return s, ErrSupport
			}
		}
		for _, key := range []string{"platform-tools", "emulator", "build-tools;37.0.0", "ndk;" + v.NDK, "cmake;" + v.CMake} {
			if v.Packages[key] == "" {
				return s, ErrSupport
			}
		}
		perAPI := map[int]AndroidImage{}
		for _, image := range v.Images {
			if v.ToolSHA256["system-images/android-"+strconv.Itoa(image.API)+"/"+image.Target+"/x86_64/system.img"] == "" {
				return s, ErrSupport
			}
			for _, key := range []string{"platforms;android-" + strconv.Itoa(image.API), "system-images;android-" + strconv.Itoa(image.API) + ";" + image.Target + ";x86_64"} {
				if v.Packages[key] == "" {
					return s, ErrSupport
				}
			}
			if old, ok := perAPI[image.API]; ok && (old.Target != image.Target || old.Profile != image.Profile) {
				return s, ErrSupport
			}
			perAPI[image.API] = image
		}
		if len(apis) < 2 {
			return s, ErrSupport
		}
	default:
		return s, ErrSupport
	}
	return s, nil
}

var images = map[string]string{
	"linux-debian": "debian:13@sha256:9c6e381421d0f48d1b8133e573f33c185ee16cbf505eda9c4a8a4791b25ea8a6",
	"linux-arch":   "archlinux:base@sha256:d4594bb2300cba11f41c2f27923eb56f63d9988909c402f9c95517d1e362c899",
	"web-linux":    "mcr.microsoft.com/playwright:v1.65.1-noble@sha256:1507cbfc550a63a6b7396791ca4f00df17beb56342d2447b3f106c306f525579",
}
