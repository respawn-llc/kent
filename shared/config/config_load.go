package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type workspaceConfigRoots struct {
	Shared string
	Main   *string
}

func Load(sharedRoot, mainWorkspaceRoot string, opts LoadOptions) (App, error) {
	if strings.TrimSpace(mainWorkspaceRoot) == "" {
		return App{}, errors.New("Main Workspace root is required")
	}
	loaded, err := loadAll(&workspaceConfigRoots{Shared: sharedRoot, Main: &mainWorkspaceRoot}, opts)
	return loaded.App, err
}

// LoadConnectionDiscovery reads the existing global/shared/environment inputs
// used before RPC. Main Workspace private ownership is resolved by the server.
func LoadConnectionDiscovery(sharedRoot string) (App, error) {
	app, _, err := LoadInteractiveConnectionDiscovery(sharedRoot, LoadOptions{})
	return app, err
}

// LoadInteractiveConnectionDiscovery includes client preferences and launch
// overrides, without resolving or reading the server-owned private layer.
func LoadInteractiveConnectionDiscovery(sharedRoot string, opts LoadOptions) (App, ClientSettings, error) {
	loaded, err := loadAll(&workspaceConfigRoots{Shared: sharedRoot}, opts)
	return loaded.App, loaded.Client, err
}

func LoadGlobal(opts LoadOptions) (App, error) {
	loaded, err := loadAll(nil, opts)
	return loaded.App, err
}

func LoadInteractive(sharedRoot, mainWorkspaceRoot string, opts LoadOptions) (App, ClientSettings, error) {
	if strings.TrimSpace(mainWorkspaceRoot) == "" {
		return App{}, ClientSettings{}, errors.New("Main Workspace root is required")
	}
	loaded, err := loadAll(&workspaceConfigRoots{Shared: sharedRoot, Main: &mainWorkspaceRoot}, opts)
	if err != nil {
		return App{}, ClientSettings{}, err
	}
	return loaded.App, loaded.Client, nil
}

type loadedConfig struct {
	App    App
	Client ClientSettings
}

// ResolvePersistenceRoot resolves the config+data root using the production
// precedence: an explicit flag, KENT_PERSISTENCE_ROOT, then ~/.kent.
func ResolvePersistenceRoot(explicitRoot string) (string, error) {
	root, _ := resolveConfigRoot(LoadOptions{ConfigRoot: explicitRoot})
	if root == "" {
		root = DefaultPersistence
	}
	return NormalizePersistenceRoot(root)
}

func loadAll(roots *workspaceConfigRoots, opts LoadOptions) (loadedConfig, error) {
	loaded, err := resolveSettings(roots, opts)
	if err != nil {
		return loadedConfig{}, err
	}
	absPersistenceRoot, err := preparePersistenceRoot(loaded.App.PersistenceRoot)
	if err != nil {
		return loadedConfig{}, err
	}
	global := loaded.App.Source.File(FileGlobal)
	if _, err := writeManagedRGConfigFileForSettingsPath(global.Path); err != nil {
		return loadedConfig{}, fmt.Errorf("write managed rg config: %w", err)
	}
	absWorktreeBaseDir, err := prepareWorktreeBaseDir(absPersistenceRoot, loaded.App.Settings.Worktrees.BaseDir)
	if err != nil {
		return loadedConfig{}, err
	}
	loaded.App.PersistenceRoot = absPersistenceRoot
	loaded.App.Settings.Worktrees.BaseDir = absWorktreeBaseDir
	return loaded, nil
}

// resolveConfigRoot picks the explicit config+data root from the
// --persistence-root flag (opts.ConfigRoot) or the KENT_PERSISTENCE_ROOT env
// var, returning the trimmed root and a source label for the source report.
func resolveConfigRoot(opts LoadOptions) (root string, source Origin) {
	if trimmed := strings.TrimSpace(opts.ConfigRoot); trimmed != "" {
		return trimmed, optionOrigin("persistence_root", SourceCLI, "--persistence-root")
	}
	if trimmed := strings.TrimSpace(os.Getenv(PersistenceRootEnvName)); trimmed != "" {
		return trimmed, optionOrigin("persistence_root", SourceEnv, PersistenceRootEnvName)
	}
	return "", defaultOrigin("persistence_root")
}

// rejectRemovedPersistenceRootKey fails loads when a config.toml still declares
// persistence_root, which is no longer a settings key. A config file cannot
// relocate the directory it is read from, so the root is set via the
// --persistence-root flag or the KENT_PERSISTENCE_ROOT env var instead.
func rejectRemovedPersistenceRootKey(raw settingsFile, settingsPath string) error {
	if _, ok, err := lookupFileString(raw, []string{"persistence_root"}); ok || err != nil {
		return fmt.Errorf("%w (in %s)", errPersistenceRootInConfigFile, settingsPath)
	}
	return nil
}
