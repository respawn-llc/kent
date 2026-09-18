// Package pathutil formats filesystem paths without changing their identity.
package pathutil

import "path/filepath"

// CollapseHome abbreviates an absolute path inside home using ~.
// Paths outside home retain their absolute representation.
func CollapseHome(path, home string) string {
	if relative, err := filepath.Rel(home, path); err == nil && filepath.IsLocal(relative) {
		if relative == "." {
			return "~"
		}
		return "~/" + filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

// Compact renders an absolute path relative to cwd when it is inside cwd,
// relative to home otherwise, and absolute when outside both directories.
// It never constructs parent-relative paths or resolves symlinks.
func Compact(path, cwd, home string) string {
	if relative, err := filepath.Rel(cwd, path); err == nil && filepath.IsLocal(relative) {
		return filepath.ToSlash(relative)
	}
	return CollapseHome(path, home)
}
