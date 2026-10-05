package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"core/server/sessionlaunch"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/runtimeids"
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
	service, err := s.sessionSettingsService(ctx, sessionID.String())
	if err != nil {
		return nil, err
	}
	result, err := service.MutateChatSettings(ctx, sessionID, operation)
	if err != nil {
		return nil, err
	}
	responseCtx := context.WithoutCancel(ctx)
	settings, err := service.SessionChatSettings(responseCtx, sessionID)
	if err != nil {
		return nil, err
	}
	contextFacts, err := s.core.safeBundles().Sessions.sessionContextOwner.ReadSessionChatContext(responseCtx, sessionID)
	if err != nil {
		return nil, err
	}
	return &chatsettingspb.MutationSuccess{Result: result, Settings: settings.GetSession().Settings, Session: settings.GetSession().Session, Context: contextFacts}, nil
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
	projectID, err := store.ResolveSessionProjectID(ctx, sessionID)
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
		projectID:      projectID,
		projectRoot:    effectiveRoot,
		projectSession: filepath.Join(projectCfg.PersistenceRoot, "projects", projectID, "sessions"),
	}), nil
}
