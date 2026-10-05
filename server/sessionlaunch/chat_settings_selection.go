package sessionlaunch

import (
	"errors"
	"fmt"
	"strings"

	"core/server/launch"
	"core/server/session"
	"core/shared/config"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
)

type PreparedChatSettingsOperationInput struct {
	ChatSettingsMutationContext
	PersistedQuestions bool
	PersistedThinking  *string
	Catalog            launch.PreparedChatAgentCatalog
}

// resolveChatSettingsSelection owns picker availability and repair policy.
// Run supplies its launch-resolved selection directly to the shared projector.
func resolveChatSettingsSelection(input PreparedChatSettingsOperationInput, operation *chatsettingspb.MutationOperation) (ResolvedChatSettingsOperation, *chatsettingspb.MutationRejected, error) {
	defaultEntry, ok := input.Catalog.Lookup(config.DefaultSubagentRole)
	if !ok {
		return ResolvedChatSettingsOperation{}, nil, errors.New("default Chat Agent baseline is missing")
	}
	selected, available := input.Catalog.Lookup(input.Raw.AgentSelector())
	if !available {
		selected = defaultEntry
	}
	settings := input.Effective
	if !available && input.Locked == nil {
		settings = defaultEntry.Settings.Baseline
		settings.Questions = input.PersistedQuestions
	} else if available {
		settings.Questions = input.PersistedQuestions
		if input.PersistedThinking != nil {
			thinking := strings.TrimSpace(*input.PersistedThinking)
			if thinking == "" {
				return ResolvedChatSettingsOperation{}, nil, errors.New("persisted Chat settings Thinking is required when present")
			}
			settings.Thinking = thinking
		}
	}
	var role *string
	if available || input.Locked != nil {
		role = input.Raw.AgentRole
	}
	state, err := session.ChatSettingsStateFromRole(role, settings)
	if err != nil {
		return ResolvedChatSettingsOperation{}, nil, err
	}
	state.ConnectionID = input.Raw.ConnectionID
	edit := operation
	if agentEdit, ok := operation.Operation.(*chatsettingspb.MutationOperation_AgentRole); ok {
		agent, valid := session.NormalizeChatAgent(agentEdit.AgentRole)
		if !valid {
			return ResolvedChatSettingsOperation{}, nil, fmt.Errorf("Chat Agent %q is invalid", agentEdit.AgentRole)
		}
		entry, found := input.Catalog.Lookup(agent)
		if !found {
			return ResolvedChatSettingsOperation{}, &chatsettingspb.MutationRejected{Reason: chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AGENT_UNAVAILABLE}, nil
		}
		if agent != input.Raw.AgentSelector() || !available {
			if entry.SelectionError != nil {
				return ResolvedChatSettingsOperation{}, nil, fmt.Errorf("select Agent %q: %w", agent, entry.SelectionError)
			}
			state, err = session.ChatSettingsStateFromCompleteSettings(entry.Choice.Role, entry.Settings.Baseline)
			if err != nil {
				return ResolvedChatSettingsOperation{}, nil, err
			}
			state.ConnectionID = entry.ConnectionID
		}
		selected = entry
		edit = nil
	}
	return ResolvedChatSettingsOperation{
		ChatSettingsMutationContext: input.ChatSettingsMutationContext,
		Selection:                   ResolvedChatSettingsSelection{State: state, Settings: *selected.Settings},
		Edit:                        edit,
	}, nil, nil
}
