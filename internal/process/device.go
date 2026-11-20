package process

import (
	"regexp"
	"strconv"
	"strings"
)

var simulatorUUID = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// Device selectors are explicit trusted setup outputs, never inherited state.
func validDevice(key, value string) bool {
	if key == "KD_CI_SIMULATOR_UDID" {
		return simulatorUUID.MatchString(value)
	}
	if key != "ANDROID_SERIAL" || !strings.HasPrefix(value, "emulator-") {
		return false
	}
	port, e := strconv.Atoi(strings.TrimPrefix(value, "emulator-"))
	return e == nil && port >= 5554 && port <= 5682 && port%2 == 0 && value == "emulator-"+strconv.Itoa(port)
}
