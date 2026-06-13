package sessionlifecycle

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"

	"core/server/auth"
	serverlifecycle "core/server/lifecycle"
	"core/server/metadata"
	"core/server/requestmemo"
	"core/server/session"
	"core/server/sessionpath"
	"core/shared/client"
	"core/shared/rollbacktarget"
	"core/shared/serverapi"
)

type Service struct {
	persistenceRoot string
	containerDir    string
	stores          sessionStoreResolver
	authManager     *auth.Manager
	controller      ControllerLeaseVerifier
	storeOptions    []session.StoreOption
	drafts          *requestmemo.Memo[sessionDraftMemoRequest, serverapi.SessionPersistInputDraftResponse]
	transitions     *requestmemo.Memo[sessionTransitionMemoRequest, serverapi.SessionResolveTransitionResponse]
}

type sessionDraftMemoRequest struct {
	SessionID string
	Input     string
}

type sessionTransitionMemoRequest struct {
	SessionID  string
	Transition serverapi.SessionTransition
}

type sessionStoreResolver interface {
	ResolveStore(ctx context.Context, sessionID string) (*session.Store, error)
}

type ControllerLeaseVerifier interface {
	RequireControllerLease(ctx context.Context, sessionID string, leaseID string) error
}

func NewService(containerDir string, stores sessionStoreResolver, authManager *auth.Manager, storeOptions ...session.StoreOption) *Service {
	return &Service{containerDir: strings.TrimSpace(containerDir), stores: stores, authManager: authManager, storeOptions: append([]session.StoreOption(nil), storeOptions...), drafts: requestmemo.New[sessionDraftMemoRequest, serverapi.SessionPersistInputDraftResponse](), transitions: requestmemo.New[sessionTransitionMemoRequest, serverapi.SessionResolveTransitionResponse]()}
}

func NewGlobalService(persistenceRoot string, stores sessionStoreResolver, authManager *auth.Manager, storeOptions ...session.StoreOption) *Service {
	return &Service{persistenceRoot: strings.TrimSpace(persistenceRoot), stores: stores, authManager: authManager, storeOptions: append([]session.StoreOption(nil), storeOptions...), drafts: requestmemo.New[sessionDraftMemoRequest, serverapi.SessionPersistInputDraftResponse](), transitions: requestmemo.New[sessionTransitionMemoRequest, serverapi.SessionResolveTransitionResponse]()}
}

type MetadataBackedLoopbackClient struct {
	mu     sync.RWMutex
	client client.SessionLifecycleClient
	store  *metadata.Store
}

func NewMetadataBackedLoopbackClient(persistenceRoot string, authManager *auth.Manager) (*MetadataBackedLoopbackClient, error) {
	store, err := metadata.Open(persistenceRoot)
	if err != nil {
		return nil, err
	}
	service := NewGlobalService(persistenceRoot, nil, authManager, store.AuthoritativeSessionStoreOptions()...)
	return &MetadataBackedLoopbackClient{client: client.NewLoopbackSessionLifecycleClient(service), store: store}, nil
}

func (c *MetadataBackedLoopbackClient) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	store := c.store
	c.client = nil
	c.store = nil
	if store == nil {
		return nil
	}
	return store.Close()
}

func (c *MetadataBackedLoopbackClient) GetInitialInput(ctx context.Context, req serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
	return callMetadataBackedLoopbackClient(c, ctx, req, client.SessionLifecycleClient.GetInitialInput)
}

func (c *MetadataBackedLoopbackClient) PersistInputDraft(ctx context.Context, req serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
	return callMetadataBackedLoopbackClient(c, ctx, req, client.SessionLifecycleClient.PersistInputDraft)
}

func (c *MetadataBackedLoopbackClient) RetargetSessionWorkspace(ctx context.Context, req serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
	return callMetadataBackedLoopbackClient(c, ctx, req, client.SessionLifecycleClient.RetargetSessionWorkspace)
}

func (c *MetadataBackedLoopbackClient) ResolveTransition(ctx context.Context, req serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
	return callMetadataBackedLoopbackClient(c, ctx, req, client.SessionLifecycleClient.ResolveTransition)
}

func callMetadataBackedLoopbackClient[Req any, Resp any](c *MetadataBackedLoopbackClient, ctx context.Context, req Req, call func(client.SessionLifecycleClient, context.Context, Req) (Resp, error)) (Resp, error) {
	if c == nil {
		var zero Resp
		return zero, errors.New("session lifecycle client is closed")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	client := c.client
	if client == nil {
		var zero Resp
		return zero, errors.New("session lifecycle client is closed")
	}
	return call(client, ctx, req)
}

func (s *Service) WithControllerLeaseVerifier(verifier ControllerLeaseVerifier) *Service {
	if s == nil {
		return nil
	}
	s.controller = verifier
	return s
}

func (s *Service) WithPersistenceRoot(root string) *Service {
	if s == nil {
		return nil
	}
	s.persistenceRoot = strings.TrimSpace(root)
	return s
}

func (s *Service) requireControllerLease(ctx context.Context, sessionID string, leaseID string) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return nil
	}
	if s == nil || s.controller == nil {
		return serverapi.ErrInvalidControllerLease
	}
	return s.controller.RequireControllerLease(ctx, trimmedSessionID, strings.TrimSpace(leaseID))
}

func (s *Service) GetInitialInput(_ context.Context, req serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.SessionInitialInputResponse{}, err
	}
	store, err := s.openStore(req.SessionID)
	if err != nil {
		return serverapi.SessionInitialInputResponse{}, err
	}
	return serverapi.SessionInitialInputResponse{Input: serverlifecycle.InitialInput(store, req.TransitionInput)}, nil
}

func (s *Service) PersistInputDraft(ctx context.Context, req serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.SessionPersistInputDraftResponse{}, err
	}
	memoReq := sessionDraftMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Input: req.Input}
	return s.drafts.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionDraftMemoRequest, func(context.Context) (serverapi.SessionPersistInputDraftResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.SessionPersistInputDraftResponse{}, err
		}
		store, err := s.openStore(req.SessionID)
		if err != nil {
			return serverapi.SessionPersistInputDraftResponse{}, err
		}
		if err := serverlifecycle.PersistInputDraft(store, req.Input); err != nil {
			return serverapi.SessionPersistInputDraftResponse{}, err
		}
		return serverapi.SessionPersistInputDraftResponse{}, nil
	})
}

func (s *Service) RetargetSessionWorkspace(ctx context.Context, req serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.SessionRetargetWorkspaceResponse{}, err
	}
	if strings.TrimSpace(s.persistenceRoot) == "" {
		return serverapi.SessionRetargetWorkspaceResponse{}, errors.New("persistence root is required")
	}
	metadataStore, err := metadata.Open(s.persistenceRoot)
	if err != nil {
		return serverapi.SessionRetargetWorkspaceResponse{}, err
	}
	defer func() { _ = metadataStore.Close() }()
	binding, err := metadataStore.RetargetSessionWorkspace(ctx, req.SessionID, req.WorkspaceRoot)
	if err != nil {
		return serverapi.SessionRetargetWorkspaceResponse{}, err
	}
	return serverapi.SessionRetargetWorkspaceResponse{Binding: serverapi.ProjectBinding{
		ProjectID:       binding.ProjectID,
		ProjectName:     binding.ProjectName,
		WorkspaceID:     binding.WorkspaceID,
		CanonicalRoot:   binding.CanonicalRoot,
		WorkspaceName:   binding.WorkspaceName,
		WorkspaceStatus: binding.WorkspaceStatus,
	}}, nil
}

func (s *Service) ResolveTransition(ctx context.Context, req serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.SessionResolveTransitionResponse{}, err
	}
	memoReq := sessionTransitionMemoRequest{
		SessionID:  strings.TrimSpace(req.SessionID),
		Transition: req.Transition,
	}
	return s.transitions.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionTransitionMemoRequest, func(context.Context) (serverapi.SessionResolveTransitionResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.SessionResolveTransitionResponse{}, err
		}
		return s.resolveTransitionOnce(ctx, req)
	})
}

func sameSessionTransitionMemoRequest(a sessionTransitionMemoRequest, b sessionTransitionMemoRequest) bool {
	return a.SessionID == b.SessionID &&
		a.Transition.Action == b.Transition.Action &&
		a.Transition.InitialPrompt == b.Transition.InitialPrompt &&
		a.Transition.InitialInput == b.Transition.InitialInput &&
		a.Transition.TargetSessionID == b.Transition.TargetSessionID &&
		a.Transition.ForkRollbackTargetID == b.Transition.ForkRollbackTargetID &&
		a.Transition.ParentSessionID == b.Transition.ParentSessionID
}

func sameSessionDraftMemoRequest(a sessionDraftMemoRequest, b sessionDraftMemoRequest) bool {
	return a.SessionID == b.SessionID && a.Input == b.Input
}

func (s *Service) resolveTransitionOnce(ctx context.Context, req serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
	if req.Transition.Action == serverapi.SessionTransitionActionLogout {
		if s.authManager == nil {
			return serverapi.SessionResolveTransitionResponse{}, errors.New("auth manager is required for logout")
		}
		return serverapi.SessionResolveTransitionResponse{
			NextSessionID:  strings.TrimSpace(req.SessionID),
			ShouldContinue: true,
			RequiresReauth: true,
		}, nil
	}
	var (
		store *session.Store
		err   error
	)
	if req.Transition.Action == serverapi.SessionTransitionActionForkRollback {
		store, err = s.openStore(req.SessionID)
		if err != nil {
			return serverapi.SessionResolveTransitionResponse{}, err
		}
		forkUserMessageIndex, resolveErr := s.resolveForkUserMessageIndex(req.Transition)
		if resolveErr != nil {
			return serverapi.SessionResolveTransitionResponse{}, resolveErr
		}
		resolved, err := serverlifecycle.Resolve(ctx, serverlifecycle.ResolveRequest{
			Store: store,
			Transition: serverlifecycle.Transition{
				Action:               req.Transition.Action,
				InitialPrompt:        req.Transition.InitialPrompt,
				InitialInput:         req.Transition.InitialInput,
				TargetSessionID:      req.Transition.TargetSessionID,
				ForkUserMessageIndex: forkUserMessageIndex,
				ParentSessionID:      req.Transition.ParentSessionID,
			},
		})
		if err != nil {
			return serverapi.SessionResolveTransitionResponse{}, err
		}
		if err := s.preserveForkExecutionTarget(ctx, req.SessionID, resolved.NextSessionID); err != nil {
			return serverapi.SessionResolveTransitionResponse{}, err
		}
		return serverapi.SessionResolveTransitionResponse{
			NextSessionID:   resolved.NextSessionID,
			InitialPrompt:   resolved.InitialPrompt,
			InitialInput:    resolved.InitialInput,
			ParentSessionID: resolved.ParentSessionID,
			ForceNewSession: resolved.ForceNewSession,
			ShouldContinue:  resolved.ShouldContinue,
		}, nil
	}
	resolved, err := serverlifecycle.Resolve(ctx, serverlifecycle.ResolveRequest{
		Store: store,
		Transition: serverlifecycle.Transition{
			Action:          req.Transition.Action,
			InitialPrompt:   req.Transition.InitialPrompt,
			InitialInput:    req.Transition.InitialInput,
			TargetSessionID: req.Transition.TargetSessionID,
			ParentSessionID: req.Transition.ParentSessionID,
		},
	})
	if err != nil {
		return serverapi.SessionResolveTransitionResponse{}, err
	}
	return serverapi.SessionResolveTransitionResponse{
		NextSessionID:   resolved.NextSessionID,
		InitialPrompt:   resolved.InitialPrompt,
		InitialInput:    resolved.InitialInput,
		ParentSessionID: resolved.ParentSessionID,
		ForceNewSession: resolved.ForceNewSession,
		ShouldContinue:  resolved.ShouldContinue,
	}, nil
}

func (s *Service) preserveForkExecutionTarget(ctx context.Context, parentSessionID string, childSessionID string) error {
	if s == nil {
		return nil
	}
	trimmedParentID := strings.TrimSpace(parentSessionID)
	trimmedChildID := strings.TrimSpace(childSessionID)
	if trimmedParentID == "" || trimmedChildID == "" || trimmedParentID == trimmedChildID {
		return nil
	}
	if strings.TrimSpace(s.persistenceRoot) == "" {
		return nil
	}
	metadataStore, err := metadata.Open(s.persistenceRoot)
	if err != nil {
		return err
	}
	defer func() { _ = metadataStore.Close() }()
	target, err := metadataStore.ResolveSessionExecutionTarget(ctx, trimmedParentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, session.ErrSessionNotFound) {
			return nil
		}
		return err
	}
	return metadataStore.UpdateSessionExecutionTargetByID(ctx, trimmedChildID, target.WorkspaceID, target.WorktreeID, target.CwdRelpath)
}

func (s *Service) resolveForkUserMessageIndex(transition serverapi.SessionTransition) (int, error) {
	return rollbacktarget.DecodeUserMessageIndex(transition.ForkRollbackTargetID)
}

func (s *Service) openStore(sessionID string) (*session.Store, error) {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return nil, nil
	}
	if s != nil && s.stores != nil {
		if store, err := s.stores.ResolveStore(context.Background(), trimmed); err != nil {
			return nil, err
		} else if store != nil {
			return store, nil
		}
	}
	if strings.TrimSpace(s.containerDir) == "" {
		if strings.TrimSpace(s.persistenceRoot) == "" {
			return nil, nil
		}
		return session.OpenByID(s.persistenceRoot, trimmed, s.storeOptions...)
	}
	sessionDir, err := sessionpath.ResolveScopedSessionDir(s.containerDir, trimmed)
	if err != nil {
		return nil, err
	}
	return session.Open(sessionDir, s.storeOptions...)
}
