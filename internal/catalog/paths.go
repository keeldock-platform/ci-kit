package catalog

import (
	"path/filepath"
	"strings"
)

// OutsideRoot checks already canonical absolute paths without mistaking a
// separate Windows volume for a path-resolution failure. Callers resolve links
// before using this to register trusted tools outside source checkouts.
func OutsideRoot(root, target string) bool {
	if !filepath.IsAbs(root) || !filepath.IsAbs(target) {
		return false
	}
	rootVolume, targetVolume := filepath.VolumeName(root), filepath.VolumeName(target)
	if rootVolume != "" && targetVolume != "" && !strings.EqualFold(rootVolume, targetVolume) {
		return true
	}
	relative, e := filepath.Rel(root, target)
	return e == nil && (relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
