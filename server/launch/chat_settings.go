package launch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"core/server/agentruntime"
	"core/server/llm"
	"core/server/sessionstore"
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
	Defaults             PreparedBaseTarget
	ConnectionID         *config.ConnectionID
	ConnectionSelection  *config.ConnectionSelection
	Choice               *chatsettingspb.AgentChoice
	Settings             *PreparedChatSettings
	SelectionError       error
	providerCapabilities llm.ProviderCapabilities
	systemPromptFile     *config.SystemPromptFile
}

func (e PreparedChatAgentCatalogEntry) ProviderCapabilities() llm.ProviderCapabilities {
	return e.providerCapabilities
}

type PreparedChatAgentCatalog struct {
	entries []PreparedChatAgentCatalogEntry
}

func PrepareChatAgentCatalog(
	app config.App,
	skipProviderReadinessValidation bool,
	invocation serverapi.RunPromptOverrides,
) (PreparedChatAgentCatalog, error) {
	return prepareChatAgentCatalog(context.Background(), app, RunPromptPreparationContext{
		Mode:                            ModeInteractive,
		SkipProviderReadinessValidation: skipProviderReadinessValidation,
	}, invocation)

}

func PrepareChatAgentSelection(ctx context.Context, app config.App, selector string, meta session.Meta, rotation *ConnectionRotation) (PreparedChatAgentCatalogEntry, error) {
	return prepareChatAgentEntry(ctx, app, selector, RunPromptPreparationContext{
		Mode: defaultAgentMode(meta), Rotation: rotation, AllocateConnection: true,
	}, false, serverapi.RunPromptOverrides{})
}

func RevalidateChatAgentSelection(app config.App, selector string, meta session.Meta, connection config.ConnectionID) (PreparedChatAgentCatalogEntry, error) {
	return prepareChatAgentEntry(context.Background(), app, selector, RunPromptPreparationContext{
		Mode: defaultAgentMode(meta), RetainedConnectionSelection: &connection,
	}, false, serverapi.RunPromptOverrides{})
}

func PrepareSessionChatAgentCatalog(ctx context.Context, app config.App, meta session.Meta, rotation *ConnectionRotation) (PreparedChatAgentCatalog, error) {
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
	target, err := (Planner{Config: app, Rotation: rotation}).SelectedSessionPromptFacingTargetFromMeta(ctx, session.Meta{
		ConnectionID: meta.ConnectionID, Continuation: continuation,
	}, false)
	if err != nil {
		return PreparedChatAgentCatalog{}, err
	}
	return prepareChatAgentCatalogForSession(ctx, app, RunPromptPreparationContext{Mode: defaultAgentMode(meta)}, selector, target)
}

func defaultAgentMode(meta session.Meta) Mode {
	if role := session.ContinuationAgentRole(meta); role != nil && *role == config.DefaultSubagentRole {
		return ModeHeadless
	}
	return ModeInteractive
}

func prepareChatAgentCatalog(ctx context.Context, app config.App, preparation RunPromptPreparationContext, invocation serverapi.RunPromptOverrides) (PreparedChatAgentCatalog, error) {
	selection, err := invocation.AgentRoleOverride()
	if err != nil {
		return PreparedChatAgentCatalog{}, err
	}
	var selected *string
	if selection.Present {
		role := selection.Role
		if selection.Default {
			role = config.DefaultSubagentRole
		}
		selected = &role
	}
	return buildChatAgentCatalog(app, selected, func(selector string) (PreparedChatAgentCatalogEntry, error) {
		return prepareChatAgentEntry(ctx, app, selector, preparation, false, invocation)
	})
}

func prepareChatAgentCatalogForSession(ctx context.Context, app config.App, preparation RunPromptPreparationContext, current string, currentTarget PreparedBaseTarget) (PreparedChatAgentCatalog, error) {
	return buildChatAgentCatalog(app, &current, func(selector string) (PreparedChatAgentCatalogEntry, error) {
		if selector == current {
			capabilities, err := llm.ResolveRuntimeProviderCapabilities(currentTarget.Settings)
			if err != nil {
				return PreparedChatAgentCatalogEntry{}, err
			}
			return chatAgentCatalogEntry(app, selector, currentTarget, capabilities, llm.SupportsFastModeProvider(capabilities))
		}
		return prepareChatAgentEntry(ctx, app, selector, preparation, true, serverapi.RunPromptOverrides{})
	})
}

func buildChatAgentCatalog(app config.App, current *string, prepare func(string) (PreparedChatAgentCatalogEntry, error)) (PreparedChatAgentCatalog, error) {
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
		configured := app.Settings
		if selector != config.DefaultSubagentRole {
			var err error
			configured, err = config.OverlaySubagentRoleProviderSettings(app, app.Settings.Subagents[selector])
			if err != nil {
				return PreparedChatAgentCatalog{}, err
			}
		}
		members, _, membershipErr := ConnectionMembership(configured)
		if membershipErr == nil {
			selection := config.ConnectionSelection(members)
			entry.ConnectionSelection = &selection
		} else {
			var reference *config.ConnectionReferenceError
			if !errors.As(membershipErr, &reference) {
				return PreparedChatAgentCatalog{}, membershipErr
			}
		}
		entries = append(entries, entry)
	}
	defaultPrompt := entries[0].systemPromptFile
	for index := range entries {
		entries[index].Choice.CustomSystemPrompt = !textutil.EqualOptional(
			entries[index].systemPromptFile,
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

func prepareChatAgentEntry(ctx context.Context, app config.App, selector string, preparation RunPromptPreparationContext, allowUnavailable bool, invocation serverapi.RunPromptOverrides) (PreparedChatAgentCatalogEntry, error) {
	fail := func(category serverapi.ChatSettingsAgentPreparationCategory) (PreparedChatAgentCatalogEntry, error) {
		return PreparedChatAgentCatalogEntry{}, &serverapi.ChatSettingsAgentPreparationError{
			Agent: selector, Category: category,
		}
	}
	invocation.AgentRole = &selector
	prepared, err := prepareAgent(ctx, app, invocation, preparation, applyDerivedModelContextBudgetOverrides)
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
		fastAvailable = llm.SupportsFastModeProvider(capabilities)
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
	connection, err := target.Settings.Connection.ConcreteID()
	if err != nil {
		return PreparedChatAgentCatalogEntry{}, err
	}
	entry := PreparedChatAgentCatalogEntry{
		Defaults:     target,
		ConnectionID: connection,
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
	entry.providerCapabilities = capabilities
	entry.systemPromptFile = target.Settings.SystemPromptFile
	return entry, nil
}

func partialChatAgentEntry(app config.App, selector string, partial unavailableAgent) PreparedChatAgentCatalogEntry {
	role := app.Settings.Subagents[selector]
	return PreparedChatAgentCatalogEntry{
		Choice: &chatsettingspb.AgentChoice{
			Role: selector, Model: partial.Role.Model, Thinking: partial.Role.Thinking,
			CustomCapabilities: config.SubagentRoleHasCapabilityOverrides(role),
			AgentCallable:      config.SubagentRoleCallable(role),
		},
		SelectionError:   partial.Cause,
		systemPromptFile: partial.Role.Settings.SystemPromptFile,
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
	target, prepared, err := prepareChatSettingsTargetForAgent(context.Background(), app, agent, RunPromptPreparationContext{Mode: ModeInteractive})
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

func prepareChatSettingsTargetForAgent(ctx context.Context,
	app config.App,
	agent string,
	preparation RunPromptPreparationContext,
) (PreparedBaseTarget, PreparedRunPromptOverrides, error) {
	var valid bool
	agent, valid = session.NormalizeChatAgent(agent)
	if !valid {
		return PreparedBaseTarget{}, PreparedRunPromptOverrides{}, fmt.Errorf("%w: Chat Agent is required", errInvalidAgentRole)
	}
	prepared, err := PrepareRunPromptOverridesWithContext(ctx,
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
	thinking := llm.ProviderThinkingEffort(target.Settings.Model, strings.TrimSpace(target.Settings.ThinkingLevel))
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
	thinking := llm.ProviderThinkingEffort(current.Model, strings.TrimSpace(current.ThinkingLevel))
	if meta.ChatSettings == nil || meta.ChatSettings.Thinking == nil {
		if err := llm.ValidateModelReasoningEffort(current.Model, thinking); err != nil {
			return session.ChatSettings{}, err
		}
	}
	if thinking != "" {
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
