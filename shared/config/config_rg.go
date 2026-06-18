package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "embed"
)

const managedRGConfigName = "rg.conf"

//go:embed rg.conf
var managedRGConfigContents string

// ResolveManagedRGConfigPath resolves the managed rg.conf path under the active
// config+data root. It honors KENT_PERSISTENCE_ROOT so shell tools point at the
// rg config of the resolved root (including isolated --persistence-root
// instances), rather than always resolving the default ~/.kent.
func ResolveManagedRGConfigPath() (string, error) {
	root := strings.TrimSpace(os.Getenv(PersistenceRootEnvName))
	if root != "" {
		expanded, err := expandTildePath(root)
		if err != nil {
			return "", err
		}
		root = expanded
	}
	settingsPath, err := resolveSettingsFilePathInRoot(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(settingsPath), managedRGConfigName), nil
}

func EnsureManagedRGConfigFile() (path string, created bool, err error) {
	path, err = ResolveManagedRGConfigPath()
	if err != nil {
		return "", false, err
	}
	created, err = writeSettingsFileIfMissing(path, managedRGConfigContents)
	if err != nil {
		return "", false, fmt.Errorf("write managed rg config: %w", err)
	}
	return path, created, nil
}

func writeManagedRGConfigFileForSettingsPath(settingsPath string) (string, error) {
	trimmed := strings.TrimSpace(settingsPath)
	if trimmed == "" {
		return "", fmt.Errorf("settings path is required")
	}
	path := filepath.Join(filepath.Dir(trimmed), managedRGConfigName)
	_, err := writeSettingsFileIfMissing(path, managedRGConfigContents)
	return path, err
}
