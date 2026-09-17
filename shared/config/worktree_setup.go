package config

// LoadWorktreeSetupSettings validates the complete source-workspace configuration
// without performing startup directory preparation.
func LoadWorktreeSetupSettings(workspaceRoot string, persistenceRoot string) (WorktreeSettings, error) {
	loaded, err := resolveSettings(&workspaceConfigRoots{Shared: workspaceRoot, Main: workspaceRoot}, LoadOptions{ConfigRoot: persistenceRoot})
	if err != nil {
		return WorktreeSettings{}, err
	}
	return loaded.App.Settings.Worktrees, nil
}
