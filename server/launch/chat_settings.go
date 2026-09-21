package launch

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/shared/config"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type PreparedChatSettings struct {
	Baseline                session.ChatSettings
	SupportedThinkingValues []string
	FastAvailable           bool
	QuestionsAvailable      bool
}

type PreparedChatAgentCatalogEntry struct {
	ConnectionID   *config.ConnectionID
	Choice         *chatsettingspb.AgentChoice
	Settings       *PreparedChatSettings
	SelectionError error
	comparison     preparedChatAgentComparison
}

type PreparedChatAgentCatalog struct {
	entries []PreparedChatAgentCatalogEntry
}

type preparedChatAgentComparison struct {
	Settings             config.Settings
	Tools                []toolspec.ID
	ProviderCapabilities llm.ProviderCapabilities
	ChatSettings         PreparedChatSettings
}

func PrepareChatAgentCatalog(
	app config.App,
	skipProviderReadinessValidation bool,
) (PreparedChatAgentCatalog, error) {
	return prepareChatAgentCatalog(app, RunPromptPreparationContext{
		Mode:                            ModeInteractive,
		SkipProviderReadinessValidation: skipProviderReadinessValidation,
	})

}

func PrepareSessionChatAgentCatalog(app config.App, meta session.Meta) (PreparedChatAgentCatalog, error) {
	state, err := session.ChatSettingsStateFromMeta(meta)
	if err != nil {
		return PreparedChatAgentCatalog{}, err
	}
	selector := state.AgentSelector()
	continuation := meta.Continuation
	if selector != config.DefaultSubagentRole && config.LookupSubagentRole(app.Settings, selector).Status != config.SubagentRoleLookupPresent {
		selector, continuation = config.DefaultSubagentRole, nil
	}
	// Baselines use configured settings, retaining only the Session's role and
	// binding. Persisted controls and locks are applied by the settings owner.
	target, err := (Planner{Config: app}).SelectedSessionPromptFacingTargetFromMeta(session.Meta{
		ConnectionID: meta.ConnectionID, Continuation: continuation,
	})
	if err != nil {
		return PreparedChatAgentCatalog{}, err
	}
	return prepareChatAgentCatalogForSession(app, RunPromptPreparationContext{Mode: defaultAgentMode(meta)}, selector, target, meta.Locked != nil)
}

func defaultAgentMode(meta session.Meta) Mode {
	if role := session.ContinuationAgentRole(meta); role != nil && *role == config.DefaultSubagentRole {
		return ModeHeadless
	}
	return ModeInteractive
}

func prepareChatAgentCatalog(app config.App, preparation RunPromptPreparationContext) (PreparedChatAgentCatalog, error) {
	return buildChatAgentCatalog(app, nil, false, func(selector string) (PreparedChatAgentCatalogEntry, error) {
		return prepareChatAgentEntry(app, selector, preparation, false)
	})
}

func prepareChatAgentCatalogForSession(app config.App, preparation RunPromptPreparationContext, current string, currentTarget PreparedBaseTarget, locked bool) (PreparedChatAgentCatalog, error) {
	return buildChatAgentCatalog(app, &current, locked, func(selector string) (PreparedChatAgentCatalogEntry, error) {
		if selector == current {
			capabilities, err := llm.ResolveRuntimeProviderCapabilities(currentTarget.Settings)
			if err != nil {
				return PreparedChatAgentCatalogEntry{}, err
			}
			return chatAgentCatalogEntry(app, selector, currentTarget, capabilities, llm.SupportsFastModeProvider(capabilities))
		}
		return prepareChatAgentEntry(app, selector, preparation, true)
	})
}

func buildChatAgentCatalog(app config.App, current *string, locked bool, prepare func(string) (PreparedChatAgentCatalogEntry, error)) (PreparedChatAgentCatalog, error) {
	selectors := append(
		[]string{config.DefaultSubagentRole},
		config.AvailableSubagentRoleNames(app.Settings, false)...,
	)
	if current != nil && !slices.Contains(selectors, *current) {
		selectors = append(selectors, *current)
	}
	entries := make([]PreparedChatAgentCatalogEntry, 0, len(selectors))
	for _, selector := range selectors {
		entry, err := prepare(selector)
		if err != nil {
			return PreparedChatAgentCatalog{}, err
		}
		if (!locked || current == nil || selector != *current) && len(entries) > 0 &&
			entry.SelectionError == nil && entries[0].SelectionError == nil &&
			reflect.DeepEqual(entries[0].comparison, entry.comparison) {
			continue
		}
		entries = append(entries, entry)
	}
	defaultPrompt := entries[0].comparison.Settings.SystemPromptFile
	for index := range entries {
		entries[index].Choice.CustomSystemPrompt = !textutil.EqualOptional(
			entries[index].comparison.Settings.SystemPromptFile,
			defaultPrompt,
		)
	}
	return PreparedChatAgentCatalog{entries: entries}, nil
}

func (c PreparedChatAgentCatalog) Choices() []*chatsettingspb.AgentChoice {
	choices := make([]*chatsettingspb.AgentChoice, 0, len(c.entries))
	for _, entry := range c.entries {
		choices = append(choices, entry.Choice)
	}
	return choices
}

func (c PreparedChatAgentCatalog) Entries() []PreparedChatAgentCatalogEntry {
	return append([]PreparedChatAgentCatalogEntry(nil), c.entries...)
}

func (c PreparedChatAgentCatalog) Lookup(agent string) (PreparedChatAgentCatalogEntry, bool) {
	agent, _ = session.NormalizeChatAgent(agent)
	for _, entry := range c.entries {
		if entry.Choice.Role == agent {
			return entry, true
		}
	}
	return PreparedChatAgentCatalogEntry{}, false
}

func prepareChatAgentEntry(app config.App, selector string, preparation RunPromptPreparationContext, allowUnavailable bool) (PreparedChatAgentCatalogEntry, error) {
	fail := func(category serverapi.ChatSettingsAgentPreparationCategory) (PreparedChatAgentCatalogEntry, error) {
		return PreparedChatAgentCatalogEntry{}, &serverapi.ChatSettingsAgentPreparationError{
			Agent: selector, Category: category,
		}
	}
	prepared, err := prepareAgent(app, serverapi.RunPromptOverrides{AgentRole: &selector}, preparation, applyDerivedModelContextBudgetOverrides)
	if err == nil && prepared.Unavailable != nil {
		if allowUnavailable {
			return partialChatAgentEntry(app, selector, *prepared.Unavailable), nil
		}
		err = prepared.Unavailable.Cause
	}
	if err != nil {
		failed, classified := fail(classifyChatAgentPreparationError(err))
		return failed, fmt.Errorf("%w: %w", classified, err)
	}
	target := prepared.PromptFacingTarget()
	if target == nil {
		return fail(serverapi.ChatSettingsAgentInternalPreparation)
	}
	var capabilities llm.ProviderCapabilities
	var fastAvailable bool
	if preparation.SkipProviderReadinessValidation {
		capabilities, err = llm.ResolveRuntimeProviderCapabilities(target.Settings)
		if err != nil {
			return fail(serverapi.ChatSettingsAgentInternalPreparation)
		}
		fastAvailable = true
	} else {
		if prepared.ProviderCapabilities == nil {
			return fail(serverapi.ChatSettingsAgentInternalPreparation)
		}
		capabilities = *prepared.ProviderCapabilities
		fastAvailable = llm.SupportsFastModeProvider(capabilities)
	}
	return chatAgentCatalogEntry(app, selector, *target, capabilities, fastAvailable)
}

func chatAgentCatalogEntry(app config.App, selector string, target PreparedBaseTarget, capabilities llm.ProviderCapabilities, fastAvailable bool) (PreparedChatAgentCatalogEntry, error) {
	settings, err := PrepareChatSettingsForPreparedTarget(target, fastAvailable)
	if err != nil {
		return PreparedChatAgentCatalogEntry{}, fmt.Errorf("%w: %w", &serverapi.ChatSettingsAgentPreparationError{
			Agent: selector, Category: serverapi.ChatSettingsAgentInvalidConfiguration,
		}, err)
	}
	tools := append([]toolspec.ID(nil), target.EnabledTools...)
	role := app.Settings.Subagents[selector]
	entry := PreparedChatAgentCatalogEntry{
		ConnectionID: target.Settings.Connection,
		Choice: &chatsettingspb.AgentChoice{
			Role:               selector,
			Model:              textutil.OptionalTrimmedString(target.Settings.Model),
			Thinking:           textutil.Value(settings.Baseline.Thinking),
			Tools:              toolspec.IDStrings(tools),
			CustomCapabilities: config.SubagentRoleHasCapabilityOverrides(role),
			AgentCallable:      config.SubagentRoleCallable(role),
		},
		Settings: &settings,
	}
	entry.comparison = preparedChatAgentComparison{
		Settings:             normalizeComparableSettings(target.Settings),
		Tools:                append([]toolspec.ID(nil), tools...),
		ProviderCapabilities: capabilities,
		ChatSettings:         settings,
	}
	return entry, nil
}

func partialChatAgentEntry(app config.App, selector string, partial unavailableAgent) PreparedChatAgentCatalogEntry {
	role := app.Settings.Subagents[selector]
	return PreparedChatAgentCatalogEntry{
		ConnectionID: partial.Role.Settings.Connection,
		Choice: &chatsettingspb.AgentChoice{
			Role: selector, Model: partial.Role.Model, Thinking: partial.Role.Thinking,
			CustomCapabilities: config.SubagentRoleHasCapabilityOverrides(role),
			AgentCallable:      config.SubagentRoleCallable(role),
		},
		SelectionError: partial.Cause,
		comparison:     preparedChatAgentComparison{Settings: normalizeComparableSettings(partial.Role.Settings)},
	}
}

func classifyChatAgentPreparationError(err error) serverapi.ChatSettingsAgentPreparationCategory {
	var providerSelection *llm.ProviderSelectionError
	var connectionReference *config.ConnectionReferenceError
	if errors.Is(err, llm.ErrUnsupportedProvider) || errors.As(err, &providerSelection) || errors.As(err, &connectionReference) {
		return serverapi.ChatSettingsAgentProviderUnavailable
	}
	if errors.Is(err, errInvalidAgentRole) ||
		errors.Is(err, ErrPatchEditToolsConflict) {
		return serverapi.ChatSettingsAgentInvalidConfiguration
	}
	return serverapi.ChatSettingsAgentInternalPreparation
}

func PrepareChatSettingsForAgent(app config.App, agent string) (PreparedChatSettings, error) {
	target, prepared, err := prepareChatSettingsTargetForAgent(app, agent, RunPromptPreparationContext{Mode: ModeInteractive})
	if err != nil {
		return PreparedChatSettings{}, err
	}
	if prepared.ProviderCapabilities == nil {
		return PreparedChatSettings{}, errors.New("Chat settings provider capabilities were not prepared")
	}
	return PrepareChatSettingsForPreparedTarget(
		target,
		llm.SupportsFastModeProvider(*prepared.ProviderCapabilities),
	)
}

func prepareChatSettingsTargetForAgent(
	app config.App,
	agent string,
	preparation RunPromptPreparationContext,
) (PreparedBaseTarget, PreparedRunPromptOverrides, error) {
	var valid bool
	agent, valid = session.NormalizeChatAgent(agent)
	if !valid {
		return PreparedBaseTarget{}, PreparedRunPromptOverrides{}, fmt.Errorf("%w: Chat Agent is required", errInvalidAgentRole)
	}
	prepared, err := PrepareRunPromptOverridesWithContext(
		app,
		serverapi.RunPromptOverrides{AgentRole: &agent},

		preparation)

	if err != nil {
		return PreparedBaseTarget{}, PreparedRunPromptOverrides{}, err
	}
	target := prepared.PromptFacingTarget()
	if target == nil {
		return PreparedBaseTarget{}, PreparedRunPromptOverrides{}, fmt.Errorf("prepare Chat Agent %q returned no target", agent)
	}
	return *target, prepared, nil
}

func PrepareChatSettingsForPreparedTarget(target PreparedBaseTarget, fastAvailable bool) (PreparedChatSettings, error) {
	supervisor, valid := runtime.NormalizeReviewerFrequency(target.Settings.Reviewer.Frequency)
	thinking := strings.TrimSpace(target.Settings.ThinkingLevel)
	if !valid || thinking == "" {
		return PreparedChatSettings{}, errors.New("prepared Chat settings are invalid")
	}
	questionsAvailable := slices.Contains(target.EnabledTools, toolspec.ToolAskQuestion)
	return PreparedChatSettings{
		Baseline: session.ChatSettings{
			Supervisor:     supervisor,
			Thinking:       thinking,
			Fast:           target.Settings.PriorityRequestMode && fastAvailable,
			Questions:      runtime.DefaultQuestionsEnabled && questionsAvailable,
			AutoCompaction: runtime.DefaultAutoCompactionEnabled,
		},
		SupportedThinkingValues: supportedChatThinkingValues(target.Settings.Model, thinking),
		FastAvailable:           fastAvailable,
		QuestionsAvailable:      questionsAvailable,
	}, nil
}

func supportedChatThinkingValues(model string, configured string) []string {
	values := llm.SupportedThinkingLevelsModel(model)
	if _, known := llm.LookupModelCapabilityContract(model); known {
		return values
	}
	configured = strings.TrimSpace(configured)
	if configured != "" && !slices.Contains(values, configured) {
		values = append(values, configured)
	}
	return values
}

func SupportedChatThinkingValues(model string, configured string) []string {
	return supportedChatThinkingValues(model, configured)
}

func ResolveSessionChatSettings(meta session.Meta, current config.Settings) (session.ChatSettings, error) {
	defaultSettings := config.DefaultOnboardingSettings()
	currentOverrides := &session.ChatSettingsOverrides{
		Fast: &current.PriorityRequestMode,
	}
	if supervisor := strings.TrimSpace(current.Reviewer.Frequency); supervisor != "" {
		currentOverrides.Supervisor = &supervisor
	}
	if thinking := strings.TrimSpace(current.ThinkingLevel); thinking != "" {
		currentOverrides.Thinking = &thinking
	}
	return session.ResolveEffectiveChatSettings(
		meta.ChatSettings,
		currentOverrides,
		session.ChatSettings{
			Supervisor:     defaultSettings.Reviewer.Frequency,
			Thinking:       defaultSettings.ThinkingLevel,
			Fast:           defaultSettings.PriorityRequestMode,
			Questions:      runtime.DefaultQuestionsEnabled,
			AutoCompaction: runtime.DefaultAutoCompactionEnabled,
		},
	)
}

func applySessionChatSettings(meta session.Meta, active config.Settings) (config.Settings, session.ChatSettings, error) {
	settings, err := ResolveSessionChatSettings(meta, active)
	if err != nil {
		return config.Settings{}, session.ChatSettings{}, err
	}
	return applyResolvedSessionChatSettings(active, settings, nil, false, nil)
}

func applyResolvedSessionChatSettings(
	active config.Settings,
	settings session.ChatSettings,
	thinkingOverride *string,
	validateThinking bool,
	fastAvailable *bool,
) (config.Settings, session.ChatSettings, error) {
	thinking := settings.Thinking
	if thinkingOverride != nil {
		thinking = *thinkingOverride
	}
	if validateThinking && !slices.Contains(supportedChatThinkingValues(active.Model, thinking), thinking) {
		return config.Settings{}, session.ChatSettings{}, fmt.Errorf(
			"Session Chat Thinking %q is unsupported by model %q",
			thinking,
			active.Model,
		)
	}
	if settings.Fast && fastAvailable != nil && !*fastAvailable {
		return config.Settings{}, session.ChatSettings{}, errors.New(
			"Session Chat Fast mode is unsupported by the active provider",
		)
	}
	active.Reviewer.Frequency = settings.Supervisor
	active.ThinkingLevel = thinking
	active.PriorityRequestMode = settings.Fast
	return active, settings, nil
}
