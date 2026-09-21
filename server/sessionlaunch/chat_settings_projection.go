package sessionlaunch

import (
	"errors"
	"slices"
	"strings"

	"core/server/launch"
	"core/server/llm"
	"core/server/session"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/toolspec"
)

type ChatSettingsProjectionInput struct {
	Catalog        launch.PreparedChatAgentCatalog
	Agent          string
	Settings       session.ChatSettings
	WorkflowLocked bool
	CompactionMode config.CompactionMode
	Locked         *session.LockedContract
}

func projectNewChatCatalog(catalog launch.PreparedChatAgentCatalog, mode config.CompactionMode) (*chatsettingspb.NewChatCatalog, error) {
	entries := catalog.Entries()
	choices := catalog.Choices()
	result := &chatsettingspb.NewChatCatalog{Choices: make([]*chatsettingspb.NewChatAgentChoice, 0, len(entries))}
	for _, entry := range entries {
		settings, err := projectSelectedChatSettings(ChatSettingsProjectionInput{
			Agent: entry.Choice.Role, Settings: entry.Settings.Baseline, CompactionMode: mode,
		}, entry, choices)
		if err != nil {
			return nil, err
		}
		baseline := &chatsettingspb.InitialChatSettings{
			AgentRole: entry.Choice.Role, Supervisor: settings.Supervisor.Value,
			QuestionsEnabled: &settings.Questions.Enabled, AutoCompactionEnabled: &settings.AutoCompaction.Stored,
		}
		if settings.Thinking != nil {
			baseline.Thinking = &settings.Thinking.Value
		}
		if settings.Fast != nil {
			baseline.Fast = &settings.Fast.Value
		}
		result.Choices = append(result.Choices, &chatsettingspb.NewChatAgentChoice{
			Agent: entry.Choice, Baseline: baseline, Supervisor: settings.Supervisor,
			Thinking: settings.Thinking, Fast: settings.Fast, Questions: settings.Questions, AutoCompaction: settings.AutoCompaction,
		})
		if entry.Choice.Role == config.DefaultSubagentRole {
			result.InitialSettings = baseline
		}
	}
	if err := protoapi.Validate(result); err != nil {
		return nil, err
	}
	return result, nil
}

func ProjectChatSettings(input ChatSettingsProjectionInput) (*chatsettingspb.Settings, error) {
	cachingLocked := input.Locked != nil
	defaultEntry, _ := input.Catalog.Lookup(config.DefaultSubagentRole)
	selected, ok := input.Catalog.Lookup(input.Agent)
	if !ok {
		if cachingLocked {
			selected = defaultEntry
		} else {
			selected = defaultEntry
			input.Agent = config.DefaultSubagentRole
			input.Settings = defaultEntry.Settings.Baseline
		}
	}
	return projectSelectedChatSettings(input, selected, input.Catalog.Choices())
}

func projectSelectedChatSettings(
	input ChatSettingsProjectionInput,
	selected launch.PreparedChatAgentCatalogEntry,
	choices []*chatsettingspb.AgentChoice,
) (*chatsettingspb.Settings, error) {
	cachingLocked := input.Locked != nil
	effective := input.Settings
	selectedRole := selected.Choice.Role
	selectedModel := selected.Choice.GetModel()
	selectedSettings := *selected.Settings
	if cachingLocked {
		normalizedAgent, ok := session.NormalizeChatAgent(input.Agent)
		if !ok {
			return nil, errors.New("caching-locked Chat Agent is invalid")
		}
		selectedRole = normalizedAgent
		selectedModel = input.Locked.Model
		lockedSettings, err := lockedPreparedChatSettings(
			*input.Locked,
			*selected.Settings,
			effective,
		)
		if err != nil {
			return nil, err
		}
		selectedSettings = lockedSettings
	}
	effective = normalizeProjectedChatSettings(effective, selectedSettings)
	supervisor, err := protoapi.ChatSettingsSupervisorToProto(effective.Supervisor)
	if err != nil {
		return nil, err
	}
	supervisorBaseline, err := protoapi.ChatSettingsSupervisorToProto(selectedSettings.Baseline.Supervisor)
	if err != nil {
		return nil, err
	}

	agentEditability := chatsettingspb.Editability_EDITABILITY_EDITABLE
	if input.WorkflowLocked {
		agentEditability = chatsettingspb.Editability_EDITABILITY_WORKFLOW_LOCK
	} else if cachingLocked {
		agentEditability = chatsettingspb.Editability_EDITABILITY_CACHING_LOCK
	}
	thinking := projectChatThinking(effective.Thinking, selectedSettings)
	var fast *chatsettingspb.Fast
	if selectedSettings.FastAvailable {
		fast = &chatsettingspb.Fast{
			Value:       effective.Fast,
			Editability: chatsettingspb.Editability_EDITABILITY_EDITABLE,
		}
	}
	autoCompaction := projectChatAutoCompaction(
		input.CompactionMode,
		effective.AutoCompaction,
		input.WorkflowLocked,
	)
	return &chatsettingspb.Settings{
		SelectedAgent: &chatsettingspb.AgentSummary{
			Role:     selectedRole,
			Model:    selectedModel,
			Thinking: effective.Thinking,
		},
		AgentChoices:     choices,
		AgentEditability: agentEditability,
		Supervisor: &chatsettingspb.Supervisor{
			Value:       supervisor,
			Baseline:    supervisorBaseline,
			Editability: chatsettingspb.Editability_EDITABILITY_EDITABLE,
		},
		Thinking: thinking,
		Fast:     fast,
		Questions: &chatsettingspb.Questions{
			Capable:     selectedSettings.QuestionsAvailable,
			Enabled:     effective.Questions,
			Editability: chatsettingspb.Editability_EDITABILITY_EDITABLE,
		},
		AutoCompaction: autoCompaction,
		AgentLocked:    input.WorkflowLocked || cachingLocked,
		WorkflowLocked: input.WorkflowLocked,
		CachingLocked:  cachingLocked,
	}, nil
}

func lockedPreparedChatSettings(
	locked session.LockedContract,
	fallback launch.PreparedChatSettings,
	effective session.ChatSettings,
) (launch.PreparedChatSettings, error) {
	capabilities, ok := llm.ProviderCapabilitiesFromLocked(&locked)
	if !ok {
		return launch.PreparedChatSettings{}, errors.New(
			"caching-locked Chat provider contract is required",
		)
	}
	fallback.SupportedThinkingValues = nil
	if llm.LockedContractSupportsReasoningEffort(&locked, locked.Model) {
		fallback.SupportedThinkingValues = launch.SupportedChatThinkingValues(
			locked.Model,
			effective.Thinking,
		)
	}
	fallback.Baseline.Thinking = effective.Thinking
	fallback.Baseline.Fast = effective.Fast
	fallback.Baseline.Questions = effective.Questions
	fallback.FastAvailable = llm.SupportsFastModeProvider(capabilities)
	fallback.QuestionsAvailable = slices.Contains(
		locked.EnabledTools,
		string(toolspec.ToolAskQuestion),
	)
	return fallback, nil
}

func normalizeProjectedChatSettings(
	current session.ChatSettings,
	prepared launch.PreparedChatSettings,
) session.ChatSettings {
	current.Thinking = strings.TrimSpace(current.Thinking)
	if current.Thinking == "" {
		current.Thinking = prepared.Baseline.Thinking
	}
	if current.Fast && !prepared.FastAvailable {
		current.Fast = prepared.Baseline.Fast
	}
	return current
}

func projectChatThinking(
	current string,
	prepared launch.PreparedChatSettings,
) *chatsettingspb.Thinking {
	if len(prepared.SupportedThinkingValues) == 0 {
		return nil
	}
	kind := chatsettingspb.ThinkingKind_THINKING_KIND_ENUMERATED
	values := append([]string(nil), prepared.SupportedThinkingValues...)
	if !slices.Contains(values, current) ||
		!slices.Contains(values, prepared.Baseline.Thinking) {
		kind = chatsettingspb.ThinkingKind_THINKING_KIND_CUSTOM
		values = nil
	}
	return &chatsettingspb.Thinking{
		Kind:          kind,
		Value:         current,
		BaselineValue: prepared.Baseline.Thinking,
		Values:        values,
		Editability:   chatsettingspb.Editability_EDITABILITY_EDITABLE,
	}
}

func projectChatAutoCompaction(
	mode config.CompactionMode,
	stored bool,
	workflowLocked bool,
) *chatsettingspb.AutoCompaction {
	policy := chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_OPTIONAL
	if mode == config.CompactionModeNone {
		policy = chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_DISABLED
	} else if workflowLocked {
		policy = chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_REQUIRED
	}
	projected := &chatsettingspb.AutoCompaction{Policy: policy, Stored: stored}
	switch {
	case mode == config.CompactionModeNone:
		projected.Editability = chatsettingspb.Editability_EDITABILITY_POLICY_DISABLED
	case workflowLocked:
		projected.Effective = true
		projected.Editability = chatsettingspb.Editability_EDITABILITY_WORKFLOW_LOCK
	default:
		projected.Effective = stored
		projected.Editability = chatsettingspb.Editability_EDITABILITY_EDITABLE
	}
	return projected
}
