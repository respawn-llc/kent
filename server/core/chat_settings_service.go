package core

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"core/server/runtime"
	"core/server/session"
	"core/server/sessionlaunch"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

type chatSettingsService struct {
	core *Core
}

func (s chatSettingsService) ReadChatSettings(
	ctx context.Context,
	req *chatsettingspb.ReadRequest,
) (*chatsettingspb.ReadSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	switch target := req.Target.(type) {
	case *chatsettingspb.ReadRequest_NewChat:
		projectCtx, err := s.core.resolveProjectContext(ctx, target.NewChat.ProjectId, target.NewChat.WorkspaceId, "")
		if err != nil {
			return nil, err
		}
		return s.core.sessionLaunchServiceForProjectContext(projectCtx).NewChatSettings(ctx)
	case *chatsettingspb.ReadRequest_Session:
		sessionID, err := runtimeids.ParseSessionID(target.Session.SessionId)
		if err != nil {
			return nil, err
		}
		service, err := s.sessionSettingsService(ctx, sessionID.String())
		if err != nil {
			return nil, err
		}
		return service.SessionChatSettings(ctx, sessionID)
	default:
		return nil, errors.New("Chat settings target kind is invalid")
	}
}
func (s chatSettingsService) MutateChatSettings(
	ctx context.Context,
	req *chatsettingspb.MutationRequest,
) (*chatsettingspb.MutationSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	sessionID, err := runtimeids.ParseSessionID(req.Session.SessionId)
	if err != nil {
		return nil, err
	}
	return s.mutateSessionChatSettings(ctx, sessionID, req.Operation)
}
func (s chatSettingsService) mutateSessionChatSettings(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	operation *chatsettingspb.MutationOperation,
) (*chatsettingspb.MutationSuccess, error) {
	result := &chatsettingspb.MutationResult{Outcome: &chatsettingspb.MutationResult_Applied{Applied: &chatsettingspb.MutationApplied{}}}
	authority := s.core.safeBundles().Runtime.runtimeAuthority
	var changed bool
	err := authority.WithSessionChatSettings(ctx, sessionID.String(), func(
		runCtx context.Context,
		sessionStore *session.Store,
		engine *runtime.Engine,
	) (bool, error) {
		service, err := s.sessionSettingsService(runCtx, sessionID.String())
		if err != nil {
			return false, err
		}
		input, err := service.PrepareSessionChatSettingsOperation(runCtx, sessionStore)
		if err != nil {
			return false, err
		}
		projected, err := sessionlaunch.ProjectPreparedChatSettingsOperation(input, operation)
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
			slog.ErrorContext(
				runCtx,
				"Chat settings persistence notification failed after durable commit",
				"session_id", sessionID.String(),
				"error", commitErr,
			)
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
	}
	responseCtx := context.WithoutCancel(ctx)
	service, err := s.sessionSettingsService(responseCtx, sessionID.String())
	if err != nil {
		return nil, err
	}
	settings, err := service.SessionChatSettings(responseCtx, sessionID)
	if err != nil {
		return nil, err
	}
	contextFacts, err := s.core.safeBundles().Sessions.sessionContextOwner.ReadSessionChatContext(responseCtx, sessionID)
	if err != nil {
		return nil, err
	}
	if result.GetApplied() != nil {
		registry := s.core.safeBundles().Runtime.runtimeRegistry
		feedback, publishFeedback, feedbackErr := transcriptSessionSettingFeedback(operation, changed, settings.GetSession().Settings)
		if feedbackErr != nil {
			slog.ErrorContext(
				responseCtx,
				"Chat settings feedback projection failed after durable commit",
				"session_id", sessionID.String(),
				"error", feedbackErr,
			)
			publishFeedback = false
		}
		var publishErr error
		if publishFeedback {
			publishErr = registry.PublishSessionSettingFeedback(sessionID.String(), feedback)
		} else {
			publishErr = registry.PublishSessionStatus(sessionID.String())
		}
		if publishErr != nil {
			slog.ErrorContext(
				responseCtx,
				"Chat settings publication failed after durable commit",
				"session_id", sessionID.String(),
				"error", publishErr,
			)
		}
	}
	return &chatsettingspb.MutationSuccess{Result: result, Settings: settings.GetSession().Settings, Session: settings.GetSession().Session, Context: contextFacts}, nil
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

func (s chatSettingsService) sessionSettingsService(
	ctx context.Context,
	sessionID string,
) (*sessionlaunch.Service, error) {
	store := s.core.safeBundles().Persistence.metadataStore
	record, err := store.ResolvePersistedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	boundary, err := store.ResolveSessionProjectWorkspaceBoundary(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	effectiveRoot := strings.TrimSpace(record.Meta.WorkspaceRoot)
	if effectiveRoot == "" {
		return nil, errors.New("session effective workspace root is required")
	}
	projectCfg, err := s.core.configForWorkspace(effectiveRoot)
	if err != nil {
		return nil, err
	}
	return s.core.newSessionLaunchService(projectContext{
		config:         projectCfg,
		projectID:      boundary.ProjectID,
		projectRoot:    effectiveRoot,
		projectSession: filepath.Join(projectCfg.PersistenceRoot, "projects", boundary.ProjectID, "sessions"),
	}), nil
}
