//go:build unix

package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const localRPCSocketFilename = "rpc.sock"

func ServerLocalRPCSocketPath(persistenceRoot string) (string, bool, error) {
	trimmedRoot := strings.TrimSpace(persistenceRoot)
	if trimmedRoot == "" {
		return "", false, nil
	}
	runtimeBase := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if runtimeBase == "" {
		runtimeBase = filepath.Join(os.TempDir(), DefaultAppName+"-"+strconv.Itoa(os.Getuid()))
	}
	return filepath.Join(runtimeBase, DefaultAppName, "rpc", PersistenceRootHash(trimmedRoot), localRPCSocketFilename), true, nil
}
