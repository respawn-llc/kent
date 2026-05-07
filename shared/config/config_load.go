package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Load(workspaceRoot string, opts LoadOptions) (App, error) {
	trimmed := strings.TrimSpace(workspaceRoot)
	if trimmed == "" {
		return App{}, errors.New("workspace root is required")
	}
	return load(trimmed, true, opts)
}

func LoadGlobal(opts LoadOptions) (App, error) {
	return load("", false, opts)
}

func load(workspaceRoot string, includeWorkspaceLayer bool, opts LoadOptions) (App, error) {
	absWorkspace := ""
	trimmedWorkspaceRoot := strings.TrimSpace(workspaceRoot)
	if trimmedWorkspaceRoot != "" {
		resolved, err := filepath.Abs(trimmedWorkspaceRoot)
		if err != nil {
			return App{}, fmt.Errorf("resolve workspace root: %w", err)
		}
		absWorkspace = resolved
	} else if includeWorkspaceLayer {
		return App{}, errors.New("workspace root is required")
	}

	homeSettingsPath, err := resolveSettingsFilePathInRoot(opts.ConfigRoot)
	if err != nil {
		return App{}, err
	}
	homeSettingsExists, err := settingsFileExists(homeSettingsPath)
	if err != nil {
		return App{}, err
	}

	homeFileConfig := settingsFile{}
	if homeSettingsExists {
		homeFileConfig, err = readSettingsFile(homeSettingsPath)
		if err != nil {
			return App{}, err
		}
	}
	workspaceSettingsPath := ""
	workspaceSettingsExists := false
	workspaceFileConfig := settingsFile{}
	if includeWorkspaceLayer {
		workspaceSettingsPath, err = resolveWorkspaceSettingsFilePath(absWorkspace)
		if err != nil {
			return App{}, err
		}
		workspaceSettingsExists, err = settingsFileExists(workspaceSettingsPath)
		if err != nil {
			return App{}, err
		}
		if workspaceSettingsExists {
			workspaceFileConfig, err = readSettingsFile(workspaceSettingsPath)
			if err != nil {
				return App{}, err
			}
		}
	}

	state := configRegistry.defaultState()
	sources := configRegistry.defaultSourceMap()

	if err := configRegistry.applyFile(homeFileConfig, homeSettingsPath, &state, sources); err != nil {
		return App{}, err
	}
	if err := appendSystemPromptFileFromConfig(homeFileConfig, homeSettingsPath, SystemPromptFileScopeHomeConfig, &state); err != nil {
		return App{}, err
	}
	if includeWorkspaceLayer {
		if err := configRegistry.applyFile(workspaceFileConfig, workspaceSettingsPath, &state, sources); err != nil {
			return App{}, err
		}
		if err := appendSystemPromptFileFromConfig(workspaceFileConfig, workspaceSettingsPath, SystemPromptFileScopeWorkspaceConfig, &state); err != nil {
			return App{}, err
		}
	}
	if err := configRegistry.applyEnv(os.LookupEnv, &state, sources); err != nil {
		return App{}, err
	}
	if err := configRegistry.applyCLI(opts, &state, sources); err != nil {
		return App{}, err
	}
	if err := applyExplicitConfigRootPersistence(opts, &state, sources); err != nil {
		return App{}, err
	}
	inheritReviewerDefaultsWithSources(&state.Settings, sources)

	if err := validateSettings(state.Settings, sources); err != nil {
		return App{}, err
	}

	absPersistenceRoot, err := preparePersistenceRoot(state.PersistenceRoot)
	if err != nil {
		return App{}, err
	}
	if _, err := writeManagedRGConfigFileForSettingsPath(homeSettingsPath); err != nil {
		return App{}, fmt.Errorf("write managed rg config: %w", err)
	}
	absWorktreeBaseDir, err := prepareWorktreeBaseDir(absPersistenceRoot, state.Settings.Worktrees.BaseDir)
	if err != nil {
		return App{}, err
	}
	state.Settings.Worktrees.BaseDir = absWorktreeBaseDir

	settingsPath := homeSettingsPath
	if workspaceSettingsExists {
		settingsPath = workspaceSettingsPath
	}
	settingsExists := homeSettingsExists || workspaceSettingsExists
	return App{
		AppName:         DefaultAppName,
		WorkspaceRoot:   absWorkspace,
		PersistenceRoot: absPersistenceRoot,
		Settings:        state.Settings,
		Source: SourceReport{
			SettingsPath:                  settingsPath,
			SettingsFileExists:            settingsExists,
			CreatedDefaultConfig:          false,
			HomeSettingsPath:              homeSettingsPath,
			HomeSettingsFileExists:        homeSettingsExists,
			WorkspaceSettingsPath:         workspaceSettingsPath,
			WorkspaceSettingsFileExists:   workspaceSettingsExists,
			WorkspaceSettingsLayerEnabled: includeWorkspaceLayer,
			Sources:                       sources,
		},
	}, nil
}

func applyExplicitConfigRootPersistence(opts LoadOptions, state *settingsState, sources map[string]string) error {
	configRoot := strings.TrimSpace(opts.ConfigRoot)
	if configRoot == "" {
		return nil
	}
	absConfigRoot, err := filepath.Abs(configRoot)
	if err != nil {
		return fmt.Errorf("resolve config root: %w", err)
	}
	state.PersistenceRoot = absConfigRoot
	sources["persistence_root"] = "config_root"
	return nil
}

func appendSystemPromptFileFromConfig(raw settingsFile, settingsPath string, scope SystemPromptFileScope, state *settingsState) error {
	path, ok, err := lookupFileString(raw, []string{"system_prompt_file"})
	if err != nil || !ok {
		return err
	}
	resolved, err := resolveConfigRelativePath(path, settingsPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resolved) == "" {
		return nil
	}
	state.Settings.SystemPromptFiles = append(state.Settings.SystemPromptFiles, SystemPromptFile{Path: resolved, Scope: scope})
	return nil
}

func resolveConfigRelativePath(path string, settingsPath string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", nil
	}
	expanded, err := expandTildePath(trimmed)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(expanded) {
		return filepath.Abs(expanded)
	}
	baseDir := strings.TrimSpace(filepath.Dir(settingsPath))
	if baseDir == "" || baseDir == "." {
		return filepath.Abs(expanded)
	}
	return filepath.Abs(filepath.Join(baseDir, expanded))
}
