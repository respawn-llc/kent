// Package pathdisplay abbreviates structured, read-only CLI path labels.
package pathdisplay

import (
	"log"
	"os"

	"core/shared/pathutil"
)

func Home(path string) string {
	return format(path, nil)
}

func Compact(path, cwd string) string {
	return format(path, &cwd)
}

func format(path string, cwd *string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("resolve home directory for path display: %v", err)
		return path
	}
	if cwd != nil {
		return pathutil.Compact(path, *cwd, home)
	}
	return pathutil.CollapseHome(path, home)
}
