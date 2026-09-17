package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveSettings reads and validates all sources without preparing directories.
// Setup uses the same resolver as startup, but has no startup side effects.
func resolveSettings(roots *workspaceConfigRoots, opts LoadOptions) (loadedConfig, error) {
	configRoot, rootOrigin := resolveConfigRoot(opts)
	if configRoot == "" {
		configRoot = DefaultPersistence
	}
	persistenceRoot, err := NormalizePersistenceRoot(configRoot)
	if err != nil {
		return loadedConfig{}, err
	}
	globalPath, err := resolveSettingsFilePathInRoot(persistenceRoot)
	if err != nil {
		return loadedConfig{}, err
	}
	files := []ConfigFileReport{{SourceFile: SourceFile{Layer: FileGlobal, Path: globalPath}, Enabled: true}}
	workspaceRoot := ""
	if roots != nil {
		if strings.TrimSpace(roots.Shared) == "" || strings.TrimSpace(roots.Main) == "" {
			return loadedConfig{}, errors.New("shared configuration root and Main Workspace root are required")
		}
		workspaceRoot, err = filepath.Abs(strings.TrimSpace(roots.Shared))
		if err != nil {
			return loadedConfig{}, fmt.Errorf("resolve shared configuration root: %w", err)
		}
		mainRoot, err := filepath.Abs(strings.TrimSpace(roots.Main))
		if err != nil {
			return loadedConfig{}, fmt.Errorf("resolve Main Workspace root: %w", err)
		}
		files = append(files,
			ConfigFileReport{SourceFile: SourceFile{Layer: FileWorkspace, Path: filepath.Join(workspaceRoot, ConfigDirName, "config.toml")}, Enabled: true},
			ConfigFileReport{SourceFile: SourceFile{Layer: FilePrivate, Path: filepath.Join(mainRoot, ConfigDirName, "config.local.toml")}, Enabled: true},
		)
	}
	state := configRegistry.defaultState()
	state.PersistenceRoot = persistenceRoot
	sources := configRegistry.defaultSourceMap()
	sources["persistence_root"] = rootOrigin
	var globalInfo os.FileInfo
	for index := range files {
		file := &files[index]
		info, err := os.Stat(file.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return loadedConfig{}, &ConfigurationFileError{Source: file.SourceFile, Err: err}
		}
		file.Exists = true
		if file.Layer == FileGlobal {
			globalInfo = info
		}
		if file.Layer == FileWorkspace && globalInfo != nil && os.SameFile(globalInfo, info) {
			file.Enabled = false
			continue
		}
		raw, err := readSettingsFile(file.Path)
		if err == nil {
			err = rejectRemovedPersistenceRootKey(raw, file.Path)
		}
		if err == nil {
			err = configRegistry.applyFile(raw, file.Path, file.Layer, &state, sources)
		}
		if err != nil {
			return loadedConfig{}, &ConfigurationFileError{Source: file.SourceFile, Err: err}
		}
		file.Applied = fileHasDeclarations(file.SourceFile, sources)
		for _, role := range state.Settings.Subagents {
			file.Applied = file.Applied || fileHasDeclarations(file.SourceFile, role.Sources)
		}
	}
	if err := configRegistry.applyEnv(os.LookupEnv, &state, sources); err != nil {
		return loadedConfig{}, err
	}
	if err := configRegistry.applyCLI(opts, &state, sources); err != nil {
		return loadedConfig{}, err
	}
	InheritReviewerSettings(&state.Settings, sources)
	if err := configRegistry.validate(state, sources, resolvedContextConstraints(state.Settings)); err != nil {
		return loadedConfig{}, err
	}
	for name, role := range state.Settings.Subagents {
		effective, roleSources := overlaySubagentRoleSettings(state.Settings, sources, role, func(string) bool { return true }, true)
		InheritReviewerSettings(&effective, roleSources)
		if err := configRegistry.validate(settingsState{Settings: effective}, roleSources, declaredContextConstraints(effective, roleSources)); err != nil {
			return loadedConfig{}, fmt.Errorf("%w subagents.%s: %w", errSubagentRole, name, err)
		}
	}
	return loadedConfig{
		App: App{
			AppName: DefaultAppName, WorkspaceRoot: workspaceRoot,
			PersistenceRoot: persistenceRoot, Settings: state.Settings,
			Source: SourceReport{Files: files, Sources: sources},
		},
		Client: state.Client,
	}, nil
}

func fileHasDeclarations(file SourceFile, sources map[string]Origin) bool {
	for _, origin := range sources {
		if origin.File != nil && *origin.File == file {
			return true
		}
	}
	return false
}
