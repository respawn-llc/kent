package config

import (
	"fmt"
	"path/filepath"
	"strings"

	_ "embed"
)

const managedRGConfigName = "rg.conf"

//go:embed rg.conf
var managedRGConfigContents string

func ResolveManagedRGConfigPath() (string, error) {
	settingsPath, err := resolveSettingsFilePath()
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
