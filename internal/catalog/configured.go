package catalog

import (
	"context"
	"path"
	"path/filepath"
	"strings"
)

func validateDiscovery(roots []string, sharedPath string) error {
	if len(roots) == 0 || len(roots) > 10000 || !safePattern(sharedPath) || sharedPath == "." || path.Base(sharedPath) == "ci-unit.json" || strings.ContainsAny(sharedPath, "*?[]{}") {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, root := range roots {
		if !safePattern(root) || strings.ContainsAny(root, "*?[]{}") || filepath.ToSlash(filepath.Clean(root)) != root || seen[root] {
			return ErrInvalid
		}
		seen[root] = true
	}
	return nil
}
func descriptorIncluded(name string, roots []string) bool {
	directory := path.Dir(name)
	for _, root := range roots {
		if root == "." || directory == root || strings.HasPrefix(directory, root+"/") {
			return true
		}
	}
	return false
}
func directoryRelevant(directory string, roots []string) bool {
	for _, root := range roots {
		if root == "." || directory == "." || directory == root || strings.HasPrefix(directory, root+"/") || strings.HasPrefix(root, directory+"/") {
			return true
		}
	}
	return false
}

// LoadWithConfig honours configured feature directories and the shared file.
func LoadWithConfig(root string, unitRoots []string, sharedPath string) ([]Unit, error) {
	return loadConfigured(root, unitRoots, sharedPath, true, false)
}

// LoadAtWithConfig applies the same discovery contract to immutable Git objects.
func LoadAtWithConfig(root, sha string, unitRoots []string, sharedPath string, gitPath ...string) ([]Unit, error) {
	return loadAtConfigured(context.Background(), root, sha, unitRoots, sharedPath, false, true, gitPath...)
}

// LoadAtWithConfigContext honours configured discovery and caller cancellation.
func LoadAtWithConfigContext(ctx context.Context, root, sha string, unitRoots []string, sharedPath string, gitPath ...string) ([]Unit, error) {
	return loadAtConfigured(ctx, root, sha, unitRoots, sharedPath, false, true, gitPath...)
}
