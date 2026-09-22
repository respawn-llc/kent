package sessionlaunch

import (
	"errors"
	"slices"
	"strings"

	"core/server/launch"
	"core/server/session"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/textutil"
)

type ChatSettingsMutationContext struct {
	Raw            session.ChatSettingsState
	Effective      session.ChatSettings
	Locked         *session.LockedContract
	WorkflowLocked bool
	CompactionMode config.CompactionMode
}

type ResolvedChatSettingsSelection struct {
	State    session.ChatSettingsState
	Settings launch.PreparedChatSettings
}

type ResolvedChatSettingsOperation struct {
	ChatSettingsMutationContext
	Selection ResolvedChatSettingsSelection
	// Agent-only changes finalize the resolved selection without a setting edit.
	Edit *chatsettingspb.MutationOperation
}

type PreparedChatSettingsOperationResult struct {
	State     session.ChatSettingsState
	Effective session.ChatSettings
	Rejection *chatsettingspb.MutationRejected
}

func ProjectResolvedChatSettingsOperation(input ResolvedChatSettingsOperation) (PreparedChatSettingsOperationResult, error) {
	if (input.Locked != nil || input.WorkflowLocked) &&
		!textutil.EqualOptional(input.Raw.AgentRole, input.Selection.State.AgentRole) {
		return rejectedChatSettingsOperation(input.ChatSettingsMutationContext, chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AGENT_LOCKED), nil
	}
	target := input.Selection.State
	prepared := input.Selection.Settings
	if input.Locked != nil {
		var err error
		prepared, err = lockedPreparedChatSettings(*input.Locked, prepared, input.Effective)
		if err != nil {
			return PreparedChatSettingsOperationResult{}, err
		}
	}
	if input.Edit != nil {
		switch operation := input.Edit.Operation.(type) {
		case *chatsettingspb.MutationOperation_Supervisor:
			supervisor, err := protoapi.ChatSettingsSupervisorFromProto(operation.Supervisor)
			if err != nil {
				return PreparedChatSettingsOperationResult{}, err
			}
			target.Settings.Supervisor = &supervisor
		case *chatsettingspb.MutationOperation_Thinking:
			thinking := strings.TrimSpace(operation.Thinking)
			effective, err := session.ResolveEffectiveChatSettings(target.Settings, nil, prepared.Baseline)
			if err != nil {
				return PreparedChatSettingsOperationResult{}, err
			}
			projection := projectChatThinking(effective.Thinking, prepared)
			if projection == nil || (projection.Kind == chatsettingspb.ThinkingKind_THINKING_KIND_ENUMERATED &&
				!slices.Contains(projection.Values, thinking)) {
				return rejectedChatSettingsOperation(input.ChatSettingsMutationContext, chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_THINKING_UNAVAILABLE), nil
			}
			target.Settings.Thinking = &thinking
		case *chatsettingspb.MutationOperation_FastEnabled:
			if !prepared.FastAvailable {
				return rejectedChatSettingsOperation(input.ChatSettingsMutationContext, chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_FAST_UNAVAILABLE), nil
			}
			target.Settings.Fast = &operation.FastEnabled
		case *chatsettingspb.MutationOperation_QuestionsEnabled:
			target.Settings.Questions = &operation.QuestionsEnabled
		case *chatsettingspb.MutationOperation_AutoCompactionEnabled:
			if input.WorkflowLocked || input.CompactionMode == config.CompactionModeNone {
				return rejectedChatSettingsOperation(input.ChatSettingsMutationContext, chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AUTO_COMPACTION_POLICY_LOCKED), nil
			}
			target.Settings.AutoCompaction = &operation.AutoCompactionEnabled
		default:
			return PreparedChatSettingsOperationResult{}, errors.New("resolved Chat settings edit must select a setting")
		}
	}
	normalized, err := session.NormalizeChatSettingsOverrides(target.Settings)
	if err != nil {
		return PreparedChatSettingsOperationResult{}, err
	}
	target.Settings = normalized
	effective, err := session.ResolveEffectiveChatSettings(target.Settings, nil, prepared.Baseline)
	if err != nil {
		return PreparedChatSettingsOperationResult{}, err
	}
	effective = normalizeProjectedChatSettings(effective, prepared)
	return PreparedChatSettingsOperationResult{State: target, Effective: effective}, nil
}

func rejectedChatSettingsOperation(input ChatSettingsMutationContext, reason chatsettingspb.MutationRejectionReason) PreparedChatSettingsOperationResult {
	return PreparedChatSettingsOperationResult{State: input.Raw, Effective: input.Effective, Rejection: &chatsettingspb.MutationRejected{Reason: reason}}
}
