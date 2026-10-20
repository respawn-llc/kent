package core

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"core/server/sessionlaunch"
	"core/shared/apicontract"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type chatSettingsService struct {
	core *Core
}

func (s chatSettingsService) SubscribeChatSettings(ctx context.Context, req *chatsettingspb.SubscribeRequest) (apicontract.ChatSettingsSubscription, error) {
	id := runtimeids.SessionIDFromProto(req.SessionId)
	service, err := s.sessionSettingsService(ctx, req.SessionId)
	if err != nil {
		return nil, err
	}
	return service.SubscribeChatSettings(ctx, id)
}

func (s chatSettingsService) PublishSessionSettings(ctx context.Context, sessionID string) {
	id := runtimeids.SessionIDFromProto(sessionID)
	service, err := s.sessionSettingsService(ctx, sessionID)
	defer func() {
		if err != nil {
			slog.ErrorContext(ctx, "Session settings publication target resolution failed", "session_id", sessionID, "error", err)
		}
	}()
	if err != nil {
		return
	}
	service.PublishSessionSettings(ctx, id)
}

func (s chatSettingsService) ReadChatSettings(
	ctx context.Context,
	req *chatsettingspb.ReadRequest,
) (response *chatsettingspb.ReadSuccess, err error) {
	switch target := req.Target.(type) {
	case *chatsettingspb.ReadRequest_NewChat:
		projectCtx, err := s.core.resolveProjectContext(ctx, target.NewChat.ProjectId, target.NewChat.WorkspaceId, "")
		if err != nil {
			return nil, err
		}
		return s.core.sessionLaunchServiceForProjectContext(projectCtx).NewChatSettings(ctx)
	case *chatsettingspb.ReadRequest_Session:
		identity, resolveErr := s.sessionSettingsTarget(ctx, target.Session.SessionId)
		if resolveErr != nil {
			return nil, resolveErr
		}
		defer func() { err = serverapi.WithSessionTarget(serverapi.ProjectedSessionTarget{Identity: identity}, err) }()
		sessionID := identity.ID
		service, serviceErr := s.sessionSettingsService(ctx, sessionID.String())
		if serviceErr != nil {
			return nil, serviceErr
		}
		return service.SessionChatSettings(ctx, sessionID)
	default:
		return nil, errors.New("Chat settings target kind is invalid")
	}
}
func (s chatSettingsService) MutateChatSettings(
	ctx context.Context,
	req *chatsettingspb.MutationRequest,
) (response *chatsettingspb.MutationSuccess, err error) {
	identity, err := s.sessionSettingsTarget(ctx, req.Session.SessionId)
	if err != nil {
		return nil, err
	}
	defer func() { err = serverapi.WithSessionTarget(serverapi.ProjectedSessionTarget{Identity: identity}, err) }()
	return s.mutateSessionChatSettings(ctx, identity.ID, req.Operation)
}

func (s chatSettingsService) sessionSettingsTarget(ctx context.Context, selector *sharedpb.SessionSelector) (identity runtimeids.SessionIdentity, err error) {
	var target serverapi.SessionTarget = serverapi.UnresolvedSessionTarget{}
	defer func() { err = serverapi.WithSessionTarget(target, err) }()
	store := s.core.MetadataStore()
	id, err := store.ResolveSessionTarget(ctx, selector)
	if err != nil {
		return identity, err
	}
	target = serverapi.CanonicalSessionTarget{ID: id}
	return store.SessionIdentity(ctx, id)
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
