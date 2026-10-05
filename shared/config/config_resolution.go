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
	state := configRegistry.defaultState()
	sources := configRegistry.defaultSourceMap()
	locations, err := readConfigurationSources(roots, opts, func(raw settingsFile, file SourceFile) (bool, error) {
		if err := rejectRemovedPersistenceRootKey(raw, file.Path); err != nil {
			return false, err
		}
		if err := configRegistry.applyFile(raw, file.Path, file.Layer, &state, sources); err != nil {
			return false, err
		}
		applied := fileHasDeclarations(file, sources)
		for _, role := range state.Settings.Subagents {
			applied = applied || fileHasDeclarations(file, role.Sources)
		}
		return applied, nil
	})
	if err != nil {
		return loadedConfig{}, err
	}
	state.PersistenceRoot = locations.persistenceRoot
	sources["persistence_root"] = locations.rootOrigin
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
			AppName: DefaultAppName, WorkspaceRoot: locations.workspaceRoot,
			PersistenceRoot: locations.persistenceRoot, Settings: state.Settings,
			Source: SourceReport{Files: locations.files, Sources: sources},
		},
		Client: state.Client,
	}, nil
}

type configurationLocations struct {
	persistenceRoot string
	workspaceRoot   string
	rootOrigin      Origin
	files           []ConfigFileReport
}

func readConfigurationSources(roots *workspaceConfigRoots, opts LoadOptions, apply func(settingsFile, SourceFile) (bool, error)) (configurationLocations, error) {
	configRoot, rootOrigin := resolveConfigRoot(opts)
	if configRoot == "" {
		configRoot = DefaultPersistence
	}
	persistenceRoot, err := NormalizePersistenceRoot(configRoot)
	if err != nil {
		return configurationLocations{}, err
	}
	globalPath, err := resolveSettingsFilePathInRoot(persistenceRoot)
	if err != nil {
		return configurationLocations{}, err
	}
	files := []ConfigFileReport{{SourceFile: SourceFile{Layer: FileGlobal, Path: globalPath}, Enabled: true}}
	workspaceRoot := ""
	if roots != nil {
		if strings.TrimSpace(roots.Shared) == "" {
			return configurationLocations{}, errors.New("shared configuration root is required")
		}
		workspaceRoot, err = filepath.Abs(strings.TrimSpace(roots.Shared))
		if err != nil {
			return configurationLocations{}, fmt.Errorf("resolve shared configuration root: %w", err)
		}
		files = append(files,
			ConfigFileReport{SourceFile: SourceFile{Layer: FileWorkspace, Path: filepath.Join(workspaceRoot, ConfigDirName, "config.toml")}, Enabled: true},
		)
		if roots.Main != nil {
			mainRoot, err := filepath.Abs(strings.TrimSpace(*roots.Main))
			if err != nil {
				return configurationLocations{}, fmt.Errorf("resolve Main Workspace root: %w", err)
			}
			files = append(files, ConfigFileReport{SourceFile: SourceFile{Layer: FilePrivate, Path: filepath.Join(mainRoot, ConfigDirName, "config.local.toml")}, Enabled: true})
		}
	}
	var globalInfo os.FileInfo
	for index := range files {
		file := &files[index]
		info, err := os.Stat(file.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return configurationLocations{}, &ConfigurationFileError{Source: file.SourceFile, Err: err}
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
			file.Applied, err = apply(raw, file.SourceFile)
		}
		if err != nil {
			return configurationLocations{}, &ConfigurationFileError{Source: file.SourceFile, Err: err}
		}
	}
	return configurationLocations{
		persistenceRoot: persistenceRoot, workspaceRoot: workspaceRoot,
		rootOrigin: rootOrigin, files: files,
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
