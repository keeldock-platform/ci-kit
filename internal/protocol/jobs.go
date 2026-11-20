package protocol

import "strings"

// ParseJobName recognises only the fixed native reusable-workflow chains.
func ParseJobName(name string) (slot, platform string, ok bool) {
	parts := strings.Split(name, " / ")
	if len(parts) > 3 || len(parts) == 0 {
		return "", "", false
	}
	leaf := parts[len(parts)-1]
	if len(parts) > 1 {
		switch {
		case parts[0] == "validate":
			if len(parts) == 3 && (parts[1] != leaf || !strings.HasPrefix(leaf, "pack-")) {
				return "", "", false
			}
		case strings.HasPrefix(parts[0], "pack-"):
			if len(parts) != 2 || parts[0] != leaf {
				return "", "", false
			}
		default:
			return "", "", false
		}
	}
	switch leaf {
	case "preflight", "plan":
		return leaf, "", len(parts) <= 2
	case "collect":
		return "finalizer", "", len(parts) <= 2
	}
	if strings.HasPrefix(leaf, "pack-") {
		p := strings.TrimPrefix(leaf, "pack-")
		for _, v := range Platforms {
			if v == p {
				return "checks", p, true
			}
		}
	}
	return "", "", false
}
