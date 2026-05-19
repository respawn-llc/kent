package client

import (
	"context"
	"errors"

	"builder/shared/serverapi"
	"builder/shared/servicecontract"
)

type SessionLifecycleClient interface {
	Close() error
	GetInitialInput(ctx context.Context, req serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error)
	PersistInputDraft(ctx context.Context, req serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error)
	RetargetSessionWorkspace(ctx context.Context, req serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error)
	ResolveTransition(ctx context.Context, req serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error)
}

type loopbackSessionLifecycleClient struct {
	service servicecontract.SessionLifecycleService
}

func NewLoopbackSessionLifecycleClient(service servicecontract.SessionLifecycleService) SessionLifecycleClient {
	return &loopbackSessionLifecycleClient{service: service}
}

func (c *loopbackSessionLifecycleClient) Close() error {
	return nil
}

func (c *loopbackSessionLifecycleClient) GetInitialInput(ctx context.Context, req serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.SessionInitialInputResponse{}, errors.New("session lifecycle service is required")
	}
	return c.service.GetInitialInput(ctx, req)
}

func (c *loopbackSessionLifecycleClient) PersistInputDraft(ctx context.Context, req serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.SessionPersistInputDraftResponse{}, errors.New("session lifecycle service is required")
	}
	return c.service.PersistInputDraft(ctx, req)
}

func (c *loopbackSessionLifecycleClient) RetargetSessionWorkspace(ctx context.Context, req serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.SessionRetargetWorkspaceResponse{}, errors.New("session lifecycle service is required")
	}
	return c.service.RetargetSessionWorkspace(ctx, req)
}

func (c *loopbackSessionLifecycleClient) ResolveTransition(ctx context.Context, req serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.SessionResolveTransitionResponse{}, errors.New("session lifecycle service is required")
	}
	return c.service.ResolveTransition(ctx, req)
}
