// Package pathdisplay abbreviates structured, read-only CLI path labels.
package pathdisplay

import (
	"log"
	"os"

	"core/shared/pathutil"
)

func Compact(path string, cwd *string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("resolve home directory for path display: %v", err)
		return pathutil.Compact(path, nil, cwd)
	}
	return pathutil.Compact(path, &home, cwd)
}
