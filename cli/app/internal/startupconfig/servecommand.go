package startupconfig

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"core/shared/config"
)

func ServeExecutablePath() (string, bool) {
	execPath, err := os.Executable()
	if err != nil {
		return "", false
	}
	if strings.HasSuffix(filepath.Base(execPath), ".test") {
		return "", false
	}
	return execPath, true
}

func ServeArgs() []string {
	return []string{"serve"}
}

func ServeEnv(cfg config.App) []string {
	env := os.Environ()
	if strings.TrimSpace(cfg.PersistenceRoot) != "" {
		env = append(env, "KENT_PERSISTENCE_ROOT="+cfg.PersistenceRoot)
	}
	if strings.TrimSpace(cfg.Settings.ServerHost) != "" {
		env = append(env, "KENT_SERVER_HOST="+cfg.Settings.ServerHost)
	}
	if cfg.Settings.ServerPort > 0 {
		env = append(env, "KENT_SERVER_PORT="+strconv.Itoa(cfg.Settings.ServerPort))
	}
	return env
}
