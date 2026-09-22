package sessionlaunch

import (
	"core/server/launch"
	"core/server/session"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type PreparedChatSettingsOperationInput struct {
	Raw                session.ChatSettingsState
	Effective          session.ChatSettings
	PersistedQuestions bool
	PersistedThinking  *string
	Catalog            launch.PreparedChatAgentCatalog
	Locked             *session.LockedContract
	WorkflowLocked     bool
	CompactionMode     config.CompactionMode
}
type PreparedChatSettingsOperationResult struct {
	State     session.ChatSettingsState
	Effective session.ChatSettings
	Rejection *chatsettingspb.MutationRejected
}

func ProjectPreparedChatSettingsOperation(input PreparedChatSettingsOperationInput, operation *chatsettingspb.MutationOperation) (PreparedChatSettingsOperationResult, error) {
	rawAgent := input.Raw.AgentSelector()
	defaultEntry, ok := input.Catalog.Lookup(config.DefaultSubagentRole)
	if !ok {
		return PreparedChatSettingsOperationResult{}, errors.New("default Chat Agent baseline is missing")
	}
	selectedEntry, selectedAvailable := input.Catalog.Lookup(rawAgent)
	if !selectedAvailable {
		selectedEntry = defaultEntry
	}
	if input.Locked != nil {
		selectedSettings, err := lockedPreparedChatSettings(*input.Locked, *selectedEntry.Settings, input.Effective)
		if err != nil {
			return PreparedChatSettingsOperationResult{}, err
		}
		selectedEntry.Settings = &selectedSettings
	}
	baseSettings := input.Effective
	if !selectedAvailable && input.Locked == nil {
		baseSettings = defaultEntry.Settings.Baseline
		baseSettings.Questions = input.PersistedQuestions
	} else if selectedAvailable {
		baseSettings.Questions = input.PersistedQuestions
		if input.PersistedThinking != nil {
			persistedThinking := strings.TrimSpace(*input.PersistedThinking)
			if persistedThinking == "" {
				return PreparedChatSettingsOperationResult{}, errors.New("persisted Chat settings Thinking is required when present")
			}
			if projectChatThinking(persistedThinking, *selectedEntry.Settings) != nil {
				baseSettings.Thinking = persistedThinking
			}
		}
	}
	var baseRole *string
	if selectedAvailable || input.Locked != nil {
		baseRole = input.Raw.AgentRole
	}
	base, err := session.ChatSettingsStateFromRole(baseRole, baseSettings)
	if err != nil {
		return PreparedChatSettingsOperationResult{}, err
	}
	base.ConnectionID = input.Raw.ConnectionID
	target := base
	switch operation := operation.Operation.(type) {
	case *chatsettingspb.MutationOperation_AgentRole:
		agent, ok := session.NormalizeChatAgent(operation.AgentRole)
		if !ok {
			return PreparedChatSettingsOperationResult{}, fmt.Errorf("Chat Agent %q is invalid", operation.AgentRole)
		}
		entry, available := input.Catalog.Lookup(agent)
		if !available {
			return rejectedChatSettingsOperation(input, chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AGENT_UNAVAILABLE), nil
		}
		if (input.Locked != nil || input.WorkflowLocked) && agent != rawAgent {
			return rejectedChatSettingsOperation(input, chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AGENT_LOCKED), nil
		}
		if agent != rawAgent || !selectedAvailable {
			if entry.SelectionError != nil {
				return PreparedChatSettingsOperationResult{}, fmt.Errorf("select Agent %q: %w", agent, entry.SelectionError)
			}
			target, err = session.ChatSettingsStateFromCompleteSettings(entry.Choice.Role, entry.Settings.Baseline)
			if err != nil {
				return PreparedChatSettingsOperationResult{}, err
			}
			target.ConnectionID = entry.ConnectionID
		}
		selectedEntry = entry
	case *chatsettingspb.MutationOperation_Supervisor:
		supervisor, err := protoapi.ChatSettingsSupervisorFromProto(operation.Supervisor)
		if err != nil {
			return PreparedChatSettingsOperationResult{}, err
		}
		target.Settings.Supervisor = &supervisor
	case *chatsettingspb.MutationOperation_Thinking:
		thinking := strings.TrimSpace(operation.Thinking)
		thinkingProjection := projectChatThinking(baseSettings.Thinking, *selectedEntry.Settings)
		if thinkingProjection == nil ||
			(thinkingProjection.Kind == chatsettingspb.ThinkingKind_THINKING_KIND_ENUMERATED &&
				!slices.Contains(thinkingProjection.Values, thinking)) {
			return rejectedChatSettingsOperation(input, chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_THINKING_UNAVAILABLE), nil
		}
		target.Settings.Thinking = &thinking
	case *chatsettingspb.MutationOperation_FastEnabled:
		if !selectedEntry.Settings.FastAvailable {
			return rejectedChatSettingsOperation(input, chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_FAST_UNAVAILABLE), nil
		}
		target.Settings.Fast = &operation.FastEnabled
	case *chatsettingspb.MutationOperation_QuestionsEnabled:
		target.Settings.Questions = &operation.QuestionsEnabled
	case *chatsettingspb.MutationOperation_AutoCompactionEnabled:
		if input.WorkflowLocked || input.CompactionMode == config.CompactionModeNone {
			return rejectedChatSettingsOperation(input, chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AUTO_COMPACTION_POLICY_LOCKED), nil
		}
		target.Settings.AutoCompaction = &operation.AutoCompactionEnabled
	}
	normalized, err := session.NormalizeChatSettingsOverrides(target.Settings)
	if err != nil {
		return PreparedChatSettingsOperationResult{}, err
	}
	target.Settings = normalized
	effective, err := session.ResolveEffectiveChatSettings(target.Settings, nil, selectedEntry.Settings.Baseline)
	if err != nil {
		return PreparedChatSettingsOperationResult{}, err
	}
	effective = normalizeProjectedChatSettings(effective, *selectedEntry.Settings)
	return PreparedChatSettingsOperationResult{State: target, Effective: effective}, nil
}
func rejectedChatSettingsOperation(
	input PreparedChatSettingsOperationInput,
	reason chatsettingspb.MutationRejectionReason,
) PreparedChatSettingsOperationResult {
	return PreparedChatSettingsOperationResult{State: input.Raw, Effective: input.Effective, Rejection: &chatsettingspb.MutationRejected{Reason: reason}}
}
