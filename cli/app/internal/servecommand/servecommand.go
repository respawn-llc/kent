package servecommand

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"builder/cli/app/internal/serverbridge"
	"builder/shared/config"
)

func ExecutablePath() (string, bool) {
	execPath, err := os.Executable()
	if err != nil {
		return "", false
	}
	if strings.HasSuffix(filepath.Base(execPath), ".test") {
		return "", false
	}
	return execPath, true
}

func Args() []string {
	return []string{"serve"}
}

func Env(cfg config.App) []string {
	env := os.Environ()
	if strings.TrimSpace(cfg.PersistenceRoot) != "" {
		env = append(env, "BUILDER_PERSISTENCE_ROOT="+cfg.PersistenceRoot)
	}
	if strings.TrimSpace(cfg.Settings.ServerHost) != "" {
		env = append(env, "BUILDER_SERVER_HOST="+cfg.Settings.ServerHost)
	}
	if cfg.Settings.ServerPort > 0 {
		env = append(env, "BUILDER_SERVER_PORT="+strconv.Itoa(cfg.Settings.ServerPort))
	}
	return env
}

func ReleaseReservation(cfg config.App) {
	serverbridge.ReleaseServeReservation(cfg)
}
