package sessionservice

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"core/server/auth"
	"core/server/launch"
	"core/server/metadata"
	"core/server/projectview"
	"core/server/session"
	"core/server/sessionruntime"
	"core/shared/config"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/rollbacktarget"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/worktreecontract"

	"google.golang.org/protobuf/types/known/emptypb"
)

var errSessionWorkspaceRetargeterRequired = errors.New("session workspace retargeter is required")

type SessionLifecycleService struct {
	persistenceRoot string
	authority       *sessionruntime.Authority
	retargeter      sessionWorkspaceRetargeter
	navigation      sessionNavigationTargetResolver
	authManager     *auth.Manager
	metadata        SessionLifecycleMetadata
	rotation        *launch.ConnectionRotation
	debug           bool
}

type SessionLifecycleMetadata interface {
	session.PersistedSessionResolver
	SessionIdentity(context.Context, runtimeids.SessionID) (runtimeids.SessionIdentity, error)
	ResolveSessionTarget(context.Context, *sharedpb.SessionSelector) (runtimeids.SessionID, error)
	DeleteSession(context.Context, string) error
	launch.MetadataExecutionTargetStore
}

type sessionWorkspaceRetargeter interface {
	RetargetWorkspace(ctx context.Context, req metadata.SessionWorkspaceRetargetRequest) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error)
	ScheduleWorkspaceRetarget(ctx context.Context, req metadata.SessionWorkspaceRetargetRequest, origin *sessionlaunchpb.RuntimeStepOrigin, operationID worktreecontract.OperationID) (*worktreepb.ScheduledAcknowledgement, error)
}

type sessionNavigationTargetResolver interface {
	ResolveSessionNavigationBinding(ctx context.Context, sessionID string) (*sessionlaunchpb.SessionNavigationBinding, error)
}

func NewGlobalSessionLifecycleService(persistenceRoot string, authority *sessionruntime.Authority, authManager *auth.Manager, owner SessionLifecycleMetadata) *SessionLifecycleService {
	return &SessionLifecycleService{
		persistenceRoot: strings.TrimSpace(persistenceRoot),
		authority:       authority,
		authManager:     authManager,
		metadata:        owner,
	}
}

func (s *SessionLifecycleService) WithWorkspaceRetargeter(retargeter sessionWorkspaceRetargeter) *SessionLifecycleService {
	if s == nil {
		return nil
	}
	s.retargeter = retargeter
	return s
}

func (s *SessionLifecycleService) WithNavigationTargetResolver(resolver sessionNavigationTargetResolver) *SessionLifecycleService {
	if s == nil {
		return nil
	}
	s.navigation = resolver
	return s
}

func (s *SessionLifecycleService) WithConnectionRotation(rotation *launch.ConnectionRotation) *SessionLifecycleService {
	s.rotation = rotation
	return s
}

func (s *SessionLifecycleService) GetInitialInput(ctx context.Context, req *sessionlaunchpb.SessionInitialInputRequest) (*sessionlaunchpb.SessionInitialInputSuccess, error) {
	if req.SessionId == nil {
		return &sessionlaunchpb.SessionInitialInputSuccess{Input: req.TransitionInput}, nil
	}
	if req.OverrideStoredDraft {
		return &sessionlaunchpb.SessionInitialInputSuccess{Input: req.TransitionInput}, nil
	}
	meta, err := s.resolvePersistedSessionMeta(ctx, req.GetSessionId())
	if err != nil {
		return nil, err
	}
	return &sessionlaunchpb.SessionInitialInputSuccess{
		Input:          initialSessionInput(meta, req.TransitionInput),
		ProtectedInput: meta.ProtectedInputDraft,
	}, nil
}

func (s *SessionLifecycleService) resolvePersistedSessionMeta(ctx context.Context, sessionID string) (session.Meta, error) {
	if s == nil || s.metadata == nil {
		return session.Meta{}, errors.New("persisted Session resolver is required")
	}
	record, err := session.ResolvePersistedSessionRecord(ctx, s.metadata, sessionID)
	if err != nil {
		return session.Meta{}, err
	}
	return *record.Meta, nil
}

func (s *SessionLifecycleService) Promote(ctx context.Context, req *sessionlaunchpb.SessionPromoteRequest) (*emptypb.Empty, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	id, err := s.metadata.ResolveSessionTarget(ctx, req.SessionId)
	if err != nil {
		return nil, err
	}
	err = s.withStore(ctx, id.String(), func(_ context.Context, store *session.Store) error {
		_, err := store.PromoteSubagentToMain()
		return err
	})
	return &emptypb.Empty{}, err
}

func (s *SessionLifecycleService) PersistInputDraft(ctx context.Context, req *sessionlaunchpb.SessionPersistInputDraftRequest) (*emptypb.Empty, error) {
	err := s.withStore(ctx, req.SessionId, func(_ context.Context, store *session.Store) error {
		var protected *session.ProtectedInputDraftUpdate
		if req.ProtectedInput != nil {
			protected = &session.ProtectedInputDraftUpdate{Text: req.ProtectedInput.Text}
		}
		return store.SetInputDraft(req.Input, protected)
	})
	return &emptypb.Empty{}, err
}

func (s *SessionLifecycleService) RetargetSessionWorkspace(ctx context.Context, req *sessionlaunchpb.SessionRetargetWorkspaceRequest) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	if s == nil || s.retargeter == nil {
		return nil, errSessionWorkspaceRetargeterRequired
	}
	if s.metadata == nil {
		return nil, errors.New("Session metadata is required")
	}
	sessionID, err := s.metadata.ResolveSessionTarget(ctx, req.SessionId)
	if err != nil {
		return nil, err
	}
	retargetRequest := metadata.SessionWorkspaceRetargetRequest{
		SessionID:     sessionID.String(),
		WorkspaceRoot: req.WorkspaceRoot,
		ProjectID:     req.ProjectId,
	}
	if req.Origin != nil && req.Origin.SessionId == sessionID.String() {
		acknowledgement, err := s.retargeter.ScheduleWorkspaceRetarget(
			ctx,
			retargetRequest,
			req.Origin,
			worktreecontract.NewOperationID(),
		)
		if err != nil {
			return nil, err
		}
		return &sessionlaunchpb.SessionRetargetWorkspaceSuccess{Scheduled: acknowledgement}, nil
	}
	return s.retargeter.RetargetWorkspace(ctx, retargetRequest)
}

func completedWorkspaceRetargetResponse(result metadata.SessionWorkspaceRetargetResult) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	binding, err := projectview.BindingToProto(result.Binding)
	if err != nil {
		return nil, err
	}
	return &sessionlaunchpb.SessionRetargetWorkspaceSuccess{
		Binding:                 binding,
		WorkspaceBindingCreated: result.WorkspaceBindingCreated,
	}, nil
}

func (s *SessionLifecycleService) ResolveTransition(ctx context.Context, req *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error) {
	return s.resolveTransitionOnce(ctx, req)
}

func (s *SessionLifecycleService) resolveTransitionOnce(ctx context.Context, req *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error) {
	if req.Transition.Action == sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_LOGOUT {
		if s.authManager == nil {
			return &sessionlaunchpb.SessionDirective{}, errors.New("auth manager is required for logout")
		}
		currentID := req.GetSessionId()
		if currentID == "" {
			return &sessionlaunchpb.SessionDirective{Directive: &sessionlaunchpb.SessionDirective_SelectSession{
				SelectSession: &sessionlaunchpb.SessionSelectDirective{Auth: sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE},
			}}, nil
		}
		sessionID := runtimeids.SessionIDFromProto(currentID)
		return protoapi.SessionLaunchDirectiveToProto(
			serverapi.OpenExistingSessionLaunchIntent(sessionID),
			sessionLaunchPreparation(nil, nil, sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE),
		)
	}
	if req.Transition.Action == sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK {
		var resolved *sessionlaunchpb.SessionDirective
		err := s.withStore(ctx, req.GetSessionId(), func(runCtx context.Context, store *session.Store) error {
			var err error
			resolved, err = s.resolveForkRollbackTransition(runCtx, req, store)
			return err
		})
		return resolved, err
	}
	if req.Transition.Action == sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_OPEN_SESSION {
		meta, err := s.resolvePersistedSessionMeta(ctx, req.GetSessionId())
		if err != nil {
			return &sessionlaunchpb.SessionDirective{}, err
		}
		resolved, err := resolveSessionTransition(ctx, sessionTransitionResolveRequest{Transition: sessionTransition{
			Action: req.Transition.Action, InitialInput: textutil.Pointer(req.Transition.InitialInput), TargetSessionID: req.Transition.GetTargetSessionId(),
		}})
		if err != nil {
			return &sessionlaunchpb.SessionDirective{}, err
		}
		return s.authorizeNavigationTransition(ctx, meta, resolved)
	}
	transition := sessionTransitionFromProto(req.Transition)
	return resolveSessionTransition(ctx, sessionTransitionResolveRequest{Transition: transition})
}

func (s *SessionLifecycleService) resolveForkRollbackTransition(ctx context.Context, req *sessionlaunchpb.SessionResolveTransitionRequest, store *session.Store) (*sessionlaunchpb.SessionDirective, error) {
	transition := sessionTransitionFromProto(req.Transition)
	forkUserMessageSeq, err := rollbacktarget.DecodeUserMessageSeq(req.Transition.GetForkRollbackTargetId())
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	transition.ForkUserMessageSeq = forkUserMessageSeq
	if s.metadata == nil {
		return nil, errors.New("session execution-target metadata owner is required")
	}
	target, err := s.metadata.ResolveSessionExecutionTarget(ctx, store.Meta().SessionID)
	if err != nil {
		return nil, err
	}
	app, err := config.Load(store.Meta().WorkspaceRoot, target.WorkspaceRoot, config.LoadOptions{ConfigRoot: s.persistenceRoot})
	if err != nil {
		return nil, err
	}
	targetSettings, err := launch.PrepareInteractiveFork(ctx, app, store.Meta(), s.rotation)
	if err != nil {
		return nil, err
	}
	resolved, err := resolveSessionTransition(ctx, sessionTransitionResolveRequest{
		Store:      store,
		Transition: transition,
		ForkTarget: targetSettings,
	})
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	forkID := runtimeids.SessionIDFromProto(resolved.GetLaunch().GetIntent().GetOpenExistingSessionId())
	if err := s.preserveForkExecutionTarget(ctx, req.GetSessionId(), forkID.String()); err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	return resolved, nil
}

func (s *SessionLifecycleService) authorizeNavigationTransition(ctx context.Context, current session.Meta, resolved *sessionlaunchpb.SessionDirective) (*sessionlaunchpb.SessionDirective, error) {
	requestedTarget := runtimeids.SessionIDFromProto(resolved.GetLaunch().GetIntent().GetOpenExistingSessionId())
	authorizedTarget := session.NavigationTargetSessionID(current)
	if authorizedTarget == nil || *authorizedTarget != requestedTarget {
		return &sessionlaunchpb.SessionDirective{}, errors.New("session navigation target does not match current session provenance")
	}
	if s == nil || s.navigation == nil {
		return &sessionlaunchpb.SessionDirective{}, errors.New("session navigation target resolver is required")
	}
	binding, err := s.navigation.ResolveSessionNavigationBinding(ctx, requestedTarget.String())
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	preparation := resolved.GetLaunch().GetPreparation()
	preparation.NavigationBinding = binding
	return resolved, nil
}

func (s *SessionLifecycleService) preserveForkExecutionTarget(ctx context.Context, parentSessionID string, childSessionID string) error {
	if s == nil {
		return nil
	}
	trimmedParentID := strings.TrimSpace(parentSessionID)
	trimmedChildID := strings.TrimSpace(childSessionID)
	if trimmedParentID == trimmedChildID {
		return nil
	}
	if strings.TrimSpace(s.persistenceRoot) == "" {
		return nil
	}
	if s.metadata == nil {
		return errors.New("session execution-target metadata owner is required")
	}
	target, err := s.metadata.ResolveSessionExecutionTarget(ctx, trimmedParentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, session.ErrSessionNotFound) {
			return nil
		}
		return err
	}
	return s.metadata.UpdateSessionExecutionTarget(ctx, metadata.SessionExecutionTargetUpdateFromReadModel(trimmedChildID, target))
}

func (s *SessionLifecycleService) withStore(
	ctx context.Context,
	sessionID string,
	callback func(context.Context, *session.Store) error,
) error {
	if s == nil || s.authority == nil {
		return errors.New("session runtime authority is required")
	}
	id := runtimeids.SessionIDFromProto(sessionID)
	descriptor, err := session.NewOpenSessionDescriptor(id)
	if err != nil {
		return err
	}
	return s.authority.WithSessionStore(ctx, descriptor, callback)
}
