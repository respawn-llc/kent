package config

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/BurntSushi/toml"
	"os"
	"path/filepath"
	"strings"
)

func resolveSettingsFilePathInRoot(root string) (string, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed != "" {
		absRoot, err := filepath.Abs(trimmed)
		if err != nil {
			return "", fmt.Errorf("resolve settings root: %w", err)
		}
		return filepath.Join(absRoot, "config.toml"), nil
	}
	home, err := currentHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ConfigDirName, "config.toml"), nil
}

func resolveWorkspaceSettingsFilePath(workspaceRoot string) (string, error) {
	trimmed := strings.TrimSpace(workspaceRoot)
	if trimmed == "" {
		return "", nil
	}
	absRoot, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	return filepath.Join(absRoot, ConfigDirName, "config.toml"), nil
}

// FindNearestWorkspaceSettingsRoot returns the nearest ancestor containing
// workspace-local settings. The global persistence settings file is excluded.
func FindNearestWorkspaceSettingsRoot(path string) (*string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, errors.New("workspace search path is required")
	}
	root, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace search path: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat workspace search path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace search path is not a directory: %q", root)
	}
	persistenceRoot, err := ResolvePersistenceRoot("")
	if err != nil {
		return nil, err
	}
	globalSettingsPath, err := resolveSettingsFilePathInRoot(persistenceRoot)
	if err != nil {
		return nil, err
	}
	for {
		settingsPath, err := resolveWorkspaceSettingsFilePath(root)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(settingsPath) == filepath.Clean(globalSettingsPath) {
			return nil, nil
		}
		exists, err := settingsFileExists(settingsPath)
		if err != nil {
			return nil, err
		}
		if exists {
			return &root, nil
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil, nil
		}
		root = parent
	}
}

func settingsFileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, fmt.Errorf("stat settings file: %w", err)
	}
}

func ensureSettingsDir(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	return nil
}

func writeSettingsFileIfMissing(path string, contents string) (bool, error) {
	return installSettingsFile(path, contents, false)
}

func replaceSettingsFile(path string, contents string) error {
	_, err := installSettingsFile(path, contents, true)
	return err
}

func installSettingsFile(path string, contents string, replace bool) (bool, error) {
	if err := ensureSettingsDir(path); err != nil {
		return false, err
	}
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".config.toml.tmp-*")
	if err != nil {
		return false, fmt.Errorf("create settings temp file: %w", err)
	}
	tempPath := temp.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.WriteString(contents); err != nil {
		_ = temp.Close()
		return false, fmt.Errorf("write settings temp file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return false, fmt.Errorf("sync settings temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return false, fmt.Errorf("close settings temp file: %w", err)
	}
	if replace {
		if err := os.Rename(tempPath, path); err != nil {
			return false, fmt.Errorf("replace settings file: %w", err)
		}
		cleanupTemp = false
		return true, nil
	}
	if err := os.Link(tempPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("install settings file: %w", err)
	}
	cleanupTemp = true
	return true, nil
}

func WriteDefaultSettingsFile() (path string, created bool, err error) {
	path, err = resolveSettingsFilePathInRoot("")
	if err != nil {
		return "", false, err
	}
	return WriteDefaultSettingsFileAt(path)
}

// WriteDefaultSettingsFileAt writes the default settings file at an explicit
// settings path. Callers that resolved a non-default config+data root (via
// --persistence-root / KENT_PERSISTENCE_ROOT) pass that root's config.toml path
// so first-run defaults land in the selected root rather than the default ~/.kent.
func WriteDefaultSettingsFileAt(path string) (string, bool, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", false, fmt.Errorf("settings path is required")
	}
	exists, err := settingsFileExists(trimmed)
	if err != nil {
		return "", false, err
	}
	if exists {
		return trimmed, false, nil
	}
	created, err := writeSettingsFileIfMissing(trimmed, settingsTOMLWithRenderingOptions(configRegistry.defaultState().Settings, true, nil, nil))
	if err != nil {
		return "", false, fmt.Errorf("write default settings file: %w", err)
	}
	return trimmed, created, nil
}

func WriteSettingsFileForOnboarding(settings Settings) (string, error) {
	return WriteSettingsFileForOnboardingWithOptions(settings, OnboardingWriteOptions{})
}

type OnboardingWriteOptions struct {
	PreservedDefaults map[string]bool
}

func DefaultOnboardingSettings() Settings {
	return configRegistry.defaultState().Settings
}

func RenderSettingsTOMLForOnboarding(settings Settings, options OnboardingWriteOptions) (string, error) {
	normalized, err := NormalizeSettingsForPersistenceWithSources(settings, onboardingPreservedSources(options.PreservedDefaults))
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	encoder := toml.NewEncoder(&output)
	if normalized.Connection != nil {
		if err := encoder.Encode(map[string]any{"connection": string(*normalized.Connection)}); err != nil {
			return "", err
		}
	}
	output.WriteString(settingsTOMLForOnboarding(normalized, options.PreservedDefaults))
	if len(normalized.Connections) > 0 {
		connections := map[string]any{}
		for id, definition := range normalized.Connections {
			connections[string(id)] = connectionSettingsTable(definition)
		}
		if err := encoder.Encode(map[string]any{"connections": connections}); err != nil {
			return "", err
		}
	}
	return output.String(), nil
}

func onboardingPreservedSources(preserved map[string]bool) map[string]Origin {
	sources := configRegistry.defaultSourceMap()
	for key, preserve := range preserved {
		if preserve {
			sources[key] = Origin{Kind: SourceInput, Property: PropertyAddress{Key: key}}
		}
	}
	return sources
}

func ResolveSettingsFilePathInRoot(root string) (string, error) {
	return resolveSettingsFilePathInRoot(root)
}

func WriteSettingsFileForOnboardingWithOptions(settings Settings, options OnboardingWriteOptions) (string, error) {
	path, err := resolveSettingsFilePathInRoot("")
	if err != nil {
		return "", err
	}
	return WriteSettingsFileForOnboardingWithOptionsAt(path, settings, options)
}

// WriteSettingsFileForOnboardingWithOptionsAt persists onboarding settings at an
// explicit settings path so interactive onboarding writes into the resolved
// config+data root (--persistence-root / KENT_PERSISTENCE_ROOT) rather than the
// default ~/.kent.
func WriteSettingsFileForOnboardingWithOptionsAt(path string, settings Settings, options OnboardingWriteOptions) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("settings path is required")
	}
	rendered, err := RenderSettingsTOMLForOnboarding(settings, options)
	if err != nil {
		return "", err
	}
	created, err := writeSettingsFileIfMissing(path, rendered)
	if err != nil {
		return "", err
	}
	if !created {
		return path, fmt.Errorf("%w: %s", errSettingsFileAlreadyExists, path)
	}
	return path, nil
}

func readSettingsFile(path string) (settingsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read settings file %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return settingsFile{}, nil
	}
	var raw settingsFile
	if _, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse settings file %s: %w", path, err)
	}
	return raw, nil
}
