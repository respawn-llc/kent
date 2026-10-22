// Package pathutil formats filesystem paths without changing their identity.
package pathutil

import (
	"log"
	"os"
	"path/filepath"
)

// Compact renders an absolute path relative to cwd when it is inside cwd,
// relative to home otherwise, and absolute when outside both directories.
// A nil cwd uses the process working directory; a nil home skips home formatting.
// Relative inputs remain unchanged. It never constructs parent-relative paths
// or resolves symlinks.
func Compact(path string, home, cwd *string) string {
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	if cwd == nil {
		workdir, err := os.Getwd()
		if err != nil {
			log.Printf("resolve working directory for path display: %v", err)
		} else {
			cwd = &workdir
		}
	}
	if cwd != nil {
		if relative, err := filepath.Rel(*cwd, path); err == nil && filepath.IsLocal(relative) {
			if relative == "." {
				return "./"
			}
			return "./" + filepath.ToSlash(relative)
		}
	}
	return compactHome(path, home)
}

// CompactHome renders paths relative to home and leaves paths outside home absolute.
// Relative inputs remain unchanged. It never reads the process working directory.
func CompactHome(path string, home *string) string {
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	return compactHome(path, home)
}

func compactHome(path string, home *string) string {
	if home != nil {
		if relative, err := filepath.Rel(*home, path); err == nil && filepath.IsLocal(relative) {
			if relative == "." {
				return "~"
			}
			return "~/" + filepath.ToSlash(relative)
		}
	}
	return filepath.ToSlash(path)
}
