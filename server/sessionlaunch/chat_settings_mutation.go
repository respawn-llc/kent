package sessionlaunch

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"core/server/registry"
	"core/server/runtime"
	"core/server/session"
	"core/server/sessionruntime"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

type ChatSettingsOwner struct {
	Authority *sessionruntime.Authority
	Registry  *registry.RuntimeRegistry
}

func (s *Service) MutateChatSettings(ctx context.Context, sessionID runtimeids.SessionID, operation *chatsettingspb.MutationOperation) (*chatsettingspb.MutationResult, error) {
	return s.applyChatSettings(ctx, sessionID, operation, func(ctx context.Context, store *session.Store) (PreparedChatSettingsOperationInput, PreparedChatSettingsOperationResult, error) {
		input, err := s.PrepareSessionChatSettingsOperation(ctx, store)
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		resolved, rejected, err := resolveChatSettingsSelection(input, operation)
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		if rejected != nil {
			return input, rejectedChatSettingsOperation(input.ChatSettingsMutationContext, rejected.Reason), nil
		}
		projected, err := ProjectResolvedChatSettingsOperation(resolved)
		return input, projected, err
	})
}

func (s *Service) applyChatSettings(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	operation *chatsettingspb.MutationOperation,
	prepare func(context.Context, *session.Store) (PreparedChatSettingsOperationInput, PreparedChatSettingsOperationResult, error),
) (*chatsettingspb.MutationResult, error) {
	if s.settingsOwner.Authority == nil || s.settingsOwner.Registry == nil {
		return nil, errors.New("Session Chat settings owner is required")
	}
	result := &chatsettingspb.MutationResult{Outcome: &chatsettingspb.MutationResult_Applied{Applied: &chatsettingspb.MutationApplied{}}}
	var changed bool
	err := s.settingsOwner.Authority.WithSessionChatSettings(ctx, sessionID.String(), func(
		runCtx context.Context,
		sessionStore *session.Store,
		engine *runtime.Engine,
	) (bool, error) {
		input, projected, err := prepare(runCtx, sessionStore)
		if err != nil {
			return false, err
		}
		if projected.Rejection != nil {
			result.Outcome = &chatsettingspb.MutationResult_Rejected{Rejected: projected.Rejection}
			return false, nil
		}
		sameAgent := textutil.EqualOptional(projected.State.AgentRole, input.Raw.AgentRole)
		if engine != nil && sameAgent {
			if _, err := engine.PrepareReviewerFrequency(projected.Effective.Supervisor); err != nil {
				return false, err
			}
			if projected.Effective.Fast && !engine.FastModeAvailable() {
				return false, errors.New("fast mode is only available for OpenAI-based Responses providers")
			}
		}
		committed, commitErr := sessionStore.CommitChatSettingsState(projected.State)
		if commitErr != nil && !committed.Committed {
			return false, commitErr
		}
		if commitErr != nil {
			slog.ErrorContext(runCtx, "Chat settings persistence notification failed after durable commit", "session_id", sessionID.String(), "error", commitErr)
		}
		changed = committed.Changed
		if engine != nil && sameAgent {
			if err := engine.AcceptPreparedChatSettings(projected.Effective); err != nil {
				return false, err
			}
		}
		return !sameAgent && changed, nil
	})
	if err != nil {
		return nil, err
	}
	if applied := result.GetApplied(); applied != nil {
		applied.Changed = changed
		responseCtx := context.WithoutCancel(ctx)
		settings, err := s.SessionChatSettings(responseCtx, sessionID)
		if err != nil {
			return nil, err
		}
		feedback, publishFeedback, feedbackErr := transcriptSessionSettingFeedback(operation, changed, settings.GetSession().Settings)
		if feedbackErr != nil {
			slog.ErrorContext(responseCtx, "Chat settings feedback projection failed after durable commit", "session_id", sessionID.String(), "error", feedbackErr)
			publishFeedback = false
		}
		var publishErr error
		if publishFeedback {
			publishErr = s.settingsOwner.Registry.PublishSessionSettingFeedback(sessionID.String(), feedback)
		} else {
			publishErr = s.settingsOwner.Registry.PublishSessionStatus(sessionID.String())
		}
		if publishErr != nil {
			slog.ErrorContext(responseCtx, "Chat settings publication failed after durable commit", "session_id", sessionID.String(), "error", publishErr)
		}
	}
	return result, nil
}

func transcriptSessionSettingFeedback(
	operation *chatsettingspb.MutationOperation,
	changed bool,
	settings *chatsettingspb.Settings,
) (*transcriptpb.SessionSettingFeedback, bool, error) {
	feedback := &transcriptpb.SessionSettingFeedback{Changed: changed}
	switch operation.Operation.(type) {
	case *chatsettingspb.MutationOperation_AgentRole:
		return nil, false, nil
	case *chatsettingspb.MutationOperation_Supervisor:
		value, err := protoapi.ChatSettingsSupervisorFromProto(settings.Supervisor.Value)
		if err != nil {
			return nil, false, err
		}
		feedback.Kind = transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_SUPERVISOR
		feedback.Value = &transcriptpb.SessionSettingFeedback_Supervisor{Supervisor: value}
	case *chatsettingspb.MutationOperation_Thinking:
		value := strings.TrimSpace(settings.SelectedAgent.Thinking)
		feedback.Kind = transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_THINKING
		feedback.Value = &transcriptpb.SessionSettingFeedback_Thinking{Thinking: value}
	case *chatsettingspb.MutationOperation_FastEnabled:
		if settings.Fast == nil {
			return nil, false, errors.New("applied Fast setting has no authoritative value")
		}
		feedback.Kind = transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_FAST_MODE
		feedback.Value = &transcriptpb.SessionSettingFeedback_FastMode{FastMode: settings.Fast.Value}
	case *chatsettingspb.MutationOperation_QuestionsEnabled:
		feedback.Kind = transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_QUESTIONS
		feedback.Value = &transcriptpb.SessionSettingFeedback_Questions{Questions: settings.Questions.Enabled}
	case *chatsettingspb.MutationOperation_AutoCompactionEnabled:
		feedback.Kind = transcriptpb.SessionSettingKind_SESSION_SETTING_KIND_AUTO_COMPACTION
		feedback.Value = &transcriptpb.SessionSettingFeedback_AutoCompaction{AutoCompaction: settings.AutoCompaction.Stored}
	default:
		return nil, false, errors.New("applied Chat settings operation kind is invalid")
	}
	if err := protoapi.Validate(feedback); err != nil {
		return nil, false, err
	}
	return feedback, true, nil
}
