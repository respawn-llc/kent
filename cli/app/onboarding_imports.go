package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"builder/cli/app/internal/onboardingimport"
	"builder/cli/app/internal/onboardingimportchoice"
	"builder/cli/app/internal/onboardingimportfs"
	"builder/cli/app/internal/onboardingimportgenerated"
	"builder/cli/app/internal/onboardingimportproviders"
	"builder/cli/app/internal/onboardingimportskills"
)

type onboardingImportProviderID = onboardingimportchoice.ProviderID

const (
	onboardingImportProviderClaudeCode onboardingImportProviderID = onboardingimportproviders.ClaudeCode
	onboardingImportProviderCodex      onboardingImportProviderID = onboardingimportproviders.Codex
	onboardingImportProviderAgents     onboardingImportProviderID = onboardingimportproviders.Agents
)

type onboardingImportProvider = onboardingimportfs.Provider

type onboardingImportDiscovery struct {
	pending             bool
	err                 error
	skipSkills          bool
	skipCommands        bool
	skillSymlinkRoots   map[onboardingImportProviderID]string
	skillSymlinkItems   map[onboardingImportProviderID][]onboardingSkillImportItem
	generatedSkillItems []onboardingSkillImportItem
	existingSkillNames  map[string]bool
	commandSymlinkRoots map[onboardingImportProviderID]string
	commandSymlinkItems map[onboardingImportProviderID][]onboardingCommandImportItem
}

type onboardingSkillImportItem = onboardingimportskills.Item

type onboardingCommandImportItem = onboardingimportfs.CommandItem

type onboardingImportDiscoveryDoneMsg struct {
	discovery onboardingImportDiscovery
}

func supportedOnboardingImportProviders() []onboardingImportProvider {
	return onboardingimportproviders.Supported()
}

func supportedOnboardingSkillImportProviders() []onboardingImportProvider {
	return onboardingimportproviders.SkillSupported()
}

func supportedOnboardingCommandImportProviders() []onboardingImportProvider {
	return onboardingimportproviders.CommandSupported()
}

func onboardingImportProviderByID(providerID onboardingImportProviderID) (onboardingImportProvider, bool) {
	return onboardingimportproviders.ByID(providerID)
}

func onboardingImportProviderOrder(providerID onboardingImportProviderID) int {
	return onboardingimportproviders.Order(providerID)
}

func onboardingImportProviderLabels(providers []onboardingImportProvider) string {
	return onboardingimportproviders.Labels(providers)
}

func discoverOnboardingImports(globalRoot string) onboardingImportDiscovery {
	return discoverOnboardingImportsForWorkspace(globalRoot, "")
}

func discoverOnboardingImportsForWorkspace(globalRoot string, workspaceRoot string) onboardingImportDiscovery {
	discovery := onboardingImportDiscovery{
		skillSymlinkRoots:   map[onboardingImportProviderID]string{},
		skillSymlinkItems:   map[onboardingImportProviderID][]onboardingSkillImportItem{},
		existingSkillNames:  map[string]bool{},
		commandSymlinkRoots: map[onboardingImportProviderID]string{},
		commandSymlinkItems: map[onboardingImportProviderID][]onboardingCommandImportItem{},
	}
	var err error
	discovery.generatedSkillItems, err = discoverGeneratedSkillItems()
	if err != nil {
		discovery.err = err
		return discovery
	}
	discovery.existingSkillNames, err = discoverExistingOnboardingSkillNames(globalRoot, workspaceRoot)
	if err != nil {
		discovery.err = err
		return discovery
	}
	discovery.skipSkills, err = shouldSkipOnboardingImport(filepath.Join(globalRoot, "skills"))
	if err != nil {
		discovery.err = err
		return discovery
	}
	discovery.skipCommands, err = shouldSkipCommandImport(globalRoot)
	if err != nil {
		discovery.err = err
		return discovery
	}
	home, err := os.UserHomeDir()
	if err != nil {
		discovery.err = fmt.Errorf("resolve home dir: %w", err)
		return discovery
	}
	for _, provider := range supportedOnboardingSkillImportProviders() {
		base := filepath.Join(home, provider.HomeEntry)
		if !discovery.skipSkills {
			skillRoot, symlinkSkills, symlinkSkillsErr := discoverProviderSkillSymlinkItems(provider, base)
			if symlinkSkillsErr != nil {
				discovery.err = symlinkSkillsErr
				return discovery
			}
			if strings.TrimSpace(skillRoot) != "" && len(symlinkSkills) > 0 {
				discovery.skillSymlinkRoots[provider.ID] = skillRoot
				discovery.skillSymlinkItems[provider.ID] = symlinkSkills
			}
		}
	}
	for _, provider := range supportedOnboardingCommandImportProviders() {
		base := filepath.Join(home, provider.HomeEntry)
		if !discovery.skipCommands {
			commandRoot, symlinkItems, symlinkErr := discoverProviderCommandSymlinkItems(provider, base)
			if symlinkErr != nil {
				discovery.err = symlinkErr
				return discovery
			}
			if strings.TrimSpace(commandRoot) != "" && len(symlinkItems) > 0 {
				discovery.commandSymlinkRoots[provider.ID] = commandRoot
				discovery.commandSymlinkItems[provider.ID] = symlinkItems
			}
		}
	}
	return discovery
}

func discoverGeneratedSkillItems() ([]onboardingSkillImportItem, error) {
	return onboardingimportgenerated.Discover()
}

func discoverExistingOnboardingSkillNames(globalRoot string, workspaceRoot string) (map[string]bool, error) {
	names := map[string]bool{}
	for _, root := range onboardingExistingSkillRoots(globalRoot, workspaceRoot) {
		rootNames, err := discoverExistingOnboardingSkillNamesInRoot(root)
		if err != nil {
			return nil, err
		}
		for name := range rootNames {
			names[name] = true
		}
	}
	return names, nil
}

func onboardingExistingSkillRoots(globalRoot string, workspaceRoot string) []string {
	roots := []string{filepath.Join(globalRoot, "skills")}
	if strings.TrimSpace(workspaceRoot) != "" {
		roots = append(roots, filepath.Join(workspaceRoot, ".builder", "skills"))
	}
	return roots
}

func discoverExistingOnboardingSkillNamesInRoot(root string) (map[string]bool, error) {
	names := map[string]bool{}
	info, statErr := os.Stat(root)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return names, nil
		}
		return nil, fmt.Errorf("inspect existing skills: %w", statErr)
	}
	if !info.IsDir() {
		return names, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read existing skills: %w", err)
	}
	for _, entry := range entries {
		skillPath := filepath.Join(root, entry.Name(), "SKILL.md")
		meta, ok := onboardingimport.ParseSkillMetadata(skillPath)
		if !ok {
			continue
		}
		if normalized := normalizeOnboardingSkillName(meta.Name); normalized != "" {
			names[normalized] = true
		}
	}
	return names, nil
}

func discoverProviderSkillSymlinkItems(provider onboardingImportProvider, base string) (string, []onboardingSkillImportItem, error) {
	return onboardingimportfs.DiscoverProviderSkills(provider, base)
}

func discoverProviderCommandSymlinkItems(provider onboardingImportProvider, base string) (string, []onboardingCommandImportItem, error) {
	return onboardingimportfs.DiscoverProviderCommands(provider, base)
}

func (d onboardingImportDiscovery) hasSkillCandidates() bool {
	if d.skipSkills {
		return false
	}
	return hasImportProviderItems(d.skillSymlinkItems)
}

func (d onboardingImportDiscovery) hasCommandCandidates() bool {
	if d.skipCommands {
		return false
	}
	return hasImportProviderItems(d.commandSymlinkItems)
}

func hasImportProviderItems[T any](byProvider map[onboardingImportProviderID][]T) bool {
	for _, items := range byProvider {
		if len(items) > 0 {
			return true
		}
	}
	return false
}

func providerLabel(provider onboardingImportProviderID) string {
	return onboardingimportproviders.Label(provider)
}

func applyImportChoice(selection *onboardingImportSelection, choiceID string) error {
	next, err := onboardingimportchoice.ApplyChoice(*selection, choiceID)
	if err != nil {
		return err
	}
	*selection = next
	return nil
}

func buildSkillImportScreen(state *onboardingFlowState) onboardingScreen {
	if state.imports.pending {
		return onboardingScreen{ID: "skills_import", Kind: onboardingScreenLoading, Title: "Import skills?", LoadingText: "Scanning skills..."}
	}
	if state.imports.err != nil {
		return onboardingScreen{ID: "skills_import", Kind: onboardingScreenChoice, Title: "Import skills?", Body: "Builder could not inspect importable skills on this machine.", ErrorText: state.imports.err.Error(), Options: []onboardingOption{{ID: "none", Title: "Do not import"}}, DefaultOptionID: "none"}
	}
	defaultID := recommendedSymlinkImportChoiceID(state.imports.skillSymlinkItems)
	if state.skillImport.Mode == onboardingImportModeNone {
		defaultID = "none"
	}
	if state.skillImport.Mode == onboardingImportModeSymlinkSource {
		defaultID = "symlink:" + string(state.skillImport.Provider)
	}
	options := []onboardingOption{{ID: "none", Title: "Do not import"}}
	for _, provider := range sortedImportProviders(state.imports.skillSymlinkItems) {
		count := len(state.imports.skillSymlinkItems[provider])
		options = append(options, onboardingOption{ID: "symlink:" + string(provider), Title: fmt.Sprintf("Symlink to %s (%d found)", providerLabel(provider), count)})
	}
	if !containsOnboardingOption(options, defaultID) && len(options) > 1 {
		defaultID = options[1].ID
	}
	return onboardingScreen{ID: "skills_import", Kind: onboardingScreenChoice, Title: "Import skills?", Body: importSkillsBody(state.imports), Options: options, DefaultOptionID: defaultID}
}

func importSkillsBody(discovery onboardingImportDiscovery) string {
	providers := make([]string, 0)
	for _, provider := range sortedImportProviders(discovery.skillSymlinkItems) {
		providers = append(providers, providerLabel(provider))
	}
	return "Builder found importable skills from " + strings.Join(providers, ", ") + ". Would you like to symlink to the other provider's directories?"
}

func buildCommandImportScreen(state *onboardingFlowState) onboardingScreen {
	if state.imports.pending {
		return onboardingScreen{ID: "commands_import", Kind: onboardingScreenLoading, Title: "Import slash commands?", LoadingText: "Scanning " + onboardingImportProviderLabels(supportedOnboardingCommandImportProviders()) + " slash commands..."}
	}
	if state.imports.err != nil {
		return onboardingScreen{ID: "commands_import", Kind: onboardingScreenChoice, Title: "Import slash commands?", Body: "Builder could not inspect importable slash commands on this machine.", ErrorText: state.imports.err.Error(), Options: []onboardingOption{{ID: "none", Title: "Do not import"}}, DefaultOptionID: "none"}
	}
	defaultID := recommendedSymlinkImportChoiceID(state.imports.commandSymlinkItems)
	if state.commandImport.Mode == onboardingImportModeNone {
		defaultID = "none"
	}
	if state.commandImport.Mode == onboardingImportModeSymlinkSource {
		defaultID = "symlink:" + string(state.commandImport.Provider)
	}
	options := []onboardingOption{{ID: "none", Title: "Do not import"}}
	for _, provider := range sortedImportProviders(state.imports.commandSymlinkItems) {
		count := len(state.imports.commandSymlinkItems[provider])
		options = append(options, onboardingOption{ID: "symlink:" + string(provider), Title: fmt.Sprintf("Symlink to %s (%d found)", providerLabel(provider), count)})
	}
	if !containsOnboardingOption(options, defaultID) && len(options) > 1 {
		defaultID = options[1].ID
	}
	return onboardingScreen{ID: "commands_import", Kind: onboardingScreenChoice, Title: "Import slash commands?", Body: importCommandsBody(state.imports), Options: options, DefaultOptionID: defaultID}
}

func importCommandsBody(discovery onboardingImportDiscovery) string {
	providers := make([]string, 0)
	for _, provider := range sortedImportProviders(discovery.commandSymlinkItems) {
		providers = append(providers, providerLabel(provider))
	}
	return "Builder found importable slash commands from " + strings.Join(providers, ", ") + ". Would you like to symlink to provider directories?"
}

func recommendedSymlinkImportChoiceID[T any](byProvider map[onboardingImportProviderID][]T) string {
	return onboardingimportchoice.RecommendedSymlinkChoiceID(byProvider, onboardingImportProviderOrderList())
}

func onboardingImportProviderOrderList() []onboardingImportProviderID {
	return onboardingimportproviders.OrderList()
}

func buildSkillSelectionScreen(state *onboardingFlowState) onboardingScreen {
	items := skillSelectionCandidates(state)
	selection := effectiveSkillSelection(state)
	body := "Pick skills to keep enabled for now. Builder will write config toggles for the unchecked skills."
	options := make([]onboardingOption, 0, len(items))
	if len(items) > 2 {
		options = append(options, onboardingOption{ID: onboardingToggleAllOptionID, Title: toggleAllOptionTitleForSelection(items, selection)})
	}
	for _, item := range items {
		warning := ""
		if item.DuplicateSourceNote != "" {
			warning = "Duplicated in " + item.DuplicateSourceNote
		}
		options = append(options, onboardingOption{ID: item.ID, Title: item.ProviderLabel + " / " + item.TargetDirName, Group: item.ProviderLabel, Warning: warning})
	}
	return onboardingScreen{ID: "skills_enabled", Kind: onboardingScreenMulti, Title: "Choose enabled skills", Body: body, Options: options, Selection: selection}
}

func toggleAllOptionTitleForSelection(items []onboardingSkillImportItem, selection map[string]bool) string {
	return onboardingimportskills.ToggleAllTitle(items, selection)
}

func allSkillSelectionItemsSelected(items []onboardingSkillImportItem, selection map[string]bool) bool {
	return onboardingimportskills.AllSelected(items, selection)
}

func skillSelectionCandidates(state *onboardingFlowState) []onboardingSkillImportItem {
	imported := make([]onboardingSkillImportItem, 0)
	if state.skillImport.Mode == onboardingImportModeSymlinkSource && !state.imports.skipSkills {
		imported = append(imported, state.imports.skillSymlinkItems[state.skillImport.Provider]...)
	}
	return onboardingimportskills.Candidates(imported, state.imports.generatedSkillItems, state.imports.existingSkillNames)
}

func normalizeOnboardingSkillName(raw string) string {
	return onboardingimportskills.NormalizeName(raw)
}

func sanitizeOnboardingSkillName(raw string) string {
	return onboardingimportskills.SanitizeName(raw)
}

func skillImportSummary(state *onboardingFlowState) string {
	if state.imports.skipSkills {
		return "skipped - existing found"
	}
	if state.skillImport.Mode != onboardingImportModeSymlinkSource {
		return ""
	}
	return fmt.Sprintf("Symlink %d skills from %s", len(skillSelectionCandidates(state)), providerLabel(state.skillImport.Provider))
}

func commandImportSummary(state *onboardingFlowState) string {
	if state.imports.skipCommands {
		return "skipped - existing found"
	}
	if state.commandImport.Mode != onboardingImportModeSymlinkSource {
		return ""
	}
	return fmt.Sprintf("Symlink %d from %s", len(state.imports.commandSymlinkItems[state.commandImport.Provider]), providerLabel(state.commandImport.Provider))
}

func executeOnboardingImports(globalRoot string, state onboardingFlowState) (func() error, error) {
	createdPaths := []string{}
	skillPaths, err := executeSkillImport(globalRoot, state.imports, state.skillImport)
	if err != nil {
		return func() error { return nil }, err
	}
	createdPaths = append(createdPaths, skillPaths...)
	commandPaths, err := executeCommandImport(globalRoot, state.imports, state.commandImport)
	if err != nil {
		rollbackErr := onboardingimportfs.RollbackCreatedPaths(createdPaths)
		if rollbackErr != nil {
			err = errors.Join(err, rollbackErr)
		}
		return func() error { return nil }, err
	}
	createdPaths = append(createdPaths, commandPaths...)
	return func() error {
		return onboardingimportfs.RollbackCreatedPaths(createdPaths)
	}, nil
}

func normalizeOnboardingImportSelection(selection onboardingImportSelection) onboardingImportSelection {
	return onboardingimportchoice.NormalizeSelection(selection)
}

func executeSkillImport(globalRoot string, discovery onboardingImportDiscovery, selection onboardingImportSelection) ([]string, error) {
	selection = normalizeOnboardingImportSelection(selection)
	if discovery.skipSkills {
		if selection.Mode != onboardingImportModeNone {
			return nil, fmt.Errorf("skills import should have been skipped because existing content was found")
		}
		return nil, nil
	}
	if selection.Mode == onboardingImportModeNone {
		return nil, nil
	}
	if selection.Mode != onboardingImportModeSymlinkSource {
		return nil, fmt.Errorf("unsupported skills import mode %q", selection.Mode)
	}
	targetRoot := filepath.Join(globalRoot, "skills")
	sourcePath := strings.TrimSpace(discovery.skillSymlinkRoots[selection.Provider])
	if sourcePath == "" {
		fallbackPath, fallbackErr := providerSkillSymlinkSource(selection.Provider)
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		sourcePath = fallbackPath
	}
	return onboardingimportfs.ExecuteSymlink(targetRoot, sourcePath, "skills", fmt.Sprintf("skills source %s", providerLabel(selection.Provider)))
}

func executeCommandImport(globalRoot string, discovery onboardingImportDiscovery, selection onboardingImportSelection) ([]string, error) {
	selection = normalizeOnboardingImportSelection(selection)
	if discovery.skipCommands {
		if selection.Mode != onboardingImportModeNone {
			return nil, fmt.Errorf("slash command import should have been skipped because existing content was found")
		}
		return nil, nil
	}
	if selection.Mode == onboardingImportModeNone {
		return nil, nil
	}
	if selection.Mode != onboardingImportModeSymlinkSource {
		return nil, fmt.Errorf("unsupported slash command import mode %q", selection.Mode)
	}
	targetRoot := filepath.Join(globalRoot, "prompts")
	sourcePath := strings.TrimSpace(discovery.commandSymlinkRoots[selection.Provider])
	if sourcePath == "" {
		fallbackPath, fallbackErr := providerCommandSymlinkSource(selection.Provider)
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		sourcePath = fallbackPath
	}
	return onboardingimportfs.ExecuteSymlink(targetRoot, sourcePath, "slash command", fmt.Sprintf("slash command source %s", providerLabel(selection.Provider)))
}

func providerSkillSymlinkSource(providerID onboardingImportProviderID) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	provider, ok := onboardingImportProviderByID(providerID)
	if !ok {
		return "", fmt.Errorf("unknown skills import provider %q", providerID)
	}
	return onboardingimportfs.ProviderSkillSourceAtBase(provider, filepath.Join(home, provider.HomeEntry))
}

func providerCommandSymlinkSource(providerID onboardingImportProviderID) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	provider, ok := onboardingImportProviderByID(providerID)
	if !ok || !provider.SupportsCommandImport {
		return "", fmt.Errorf("unknown slash command import provider %q", providerID)
	}
	base := filepath.Join(home, provider.HomeEntry)
	return onboardingimportfs.ProviderCommandSourceAtBase(provider, base)
}

func shouldSkipOnboardingImport(path string) (bool, error) {
	return onboardingimportfs.ShouldSkipTarget(path)
}

func shouldSkipCommandImport(globalRoot string) (bool, error) {
	return onboardingimportfs.ShouldSkipCommandImport(globalRoot)
}
