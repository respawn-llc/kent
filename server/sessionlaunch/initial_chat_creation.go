package sessionlaunch

import (
	"errors"
	"slices"

	"core/server/launch"
	"core/server/llm"
	"core/server/session"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/textutil"
)

type InitialChatCreation struct {
	Settings   serverapi.InitialChatSettings
	InputDraft *string
	Name       *string
}

func initialChatCreationFromGenerated(
	settings *chatsettingspb.InitialChatSettings,
	inputDraft *string,
	name *string,
) *InitialChatCreation {
	if settings == nil {
		return nil
	}
	creation := &InitialChatCreation{
		Settings:   protoapi.InitialChatSettingsFromProto(settings),
		InputDraft: textutil.Pointer(inputDraft),
		Name:       textutil.Pointer(name),
	}
	return creation
}

func (c InitialChatCreation) Validate(mode launch.Mode, intent serverapi.SessionLaunchIntent) error {
	return launch.ValidateInitialChatCreationTarget(mode, intent)
}

func prepareInitialChatCreation(
	creation InitialChatCreation,
	prepared launch.PreparedRunPromptOverrides,
) (*session.ChatDraftState, error) {
	target := prepared.PromptFacingTarget()
	if target == nil || prepared.ProviderCapabilities == nil {
		return nil, errors.New("prepared initial Chat target and capabilities are required")
	}
	capabilities, err := launch.PrepareChatSettingsForPreparedTarget(*target, llm.SupportsFastModeProvider(*prepared.ProviderCapabilities))
	if err != nil {
		return nil, err
	}
	settings := capabilities.Baseline
	agent := config.DefaultSubagentRole
	if prepared.NamedTarget != nil {
		agent = prepared.NamedTarget.Selector
	}
	if creation.Settings.AgentRole == agent {
		settings.Supervisor = creation.Settings.Supervisor
		if creation.Settings.Thinking != nil {
			thinking := *creation.Settings.Thinking
			_, enumerated := llm.LookupModelCapabilityContract(target.Settings.Model)
			if len(capabilities.SupportedThinkingValues) > 0 &&
				(!enumerated || slices.Contains(capabilities.SupportedThinkingValues, thinking)) {
				settings.Thinking = thinking
			}
		}
		if creation.Settings.Fast != nil && capabilities.FastAvailable {
			settings.Fast = *creation.Settings.Fast
		}
		settings.Questions = creation.Settings.QuestionsEnabled
		settings.AutoCompaction = creation.Settings.AutoCompactionEnabled
	}
	state, err := session.ChatSettingsStateFromCompleteSettings(agent, settings)
	if err != nil {
		return nil, err
	}
	draft := ""
	if creation.InputDraft != nil {
		draft = *creation.InputDraft
	}
	return &session.ChatDraftState{
		Name:    creation.Name,
		Message: draft,
		Agent:   state.AgentSelector(),
		Settings: &session.ChatSettingsOverrides{
			Supervisor:     textutil.Pointer(state.Settings.Supervisor),
			Thinking:       textutil.Pointer(state.Settings.Thinking),
			Fast:           textutil.Pointer(state.Settings.Fast),
			Questions:      textutil.Pointer(state.Settings.Questions),
			AutoCompaction: textutil.Pointer(state.Settings.AutoCompaction),
		},
	}, nil
}
