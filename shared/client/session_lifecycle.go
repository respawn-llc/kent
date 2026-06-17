package client

import (
	"context"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type SessionLifecycleClient interface {
	servicecontract.SessionLifecycleService
	Close() error
}
type loopbackSessionLifecycleClient struct {
	loopbackClient[servicecontract.SessionLifecycleService]
}

func NewLoopbackSessionLifecycleClient(service servicecontract.SessionLifecycleService) SessionLifecycleClient {
	return &loopbackSessionLifecycleClient{loopbackClient: newLoopbackClient(service)}
}

func (c *loopbackSessionLifecycleClient) Close() error {
	return nil
}

func (c *loopbackSessionLifecycleClient) GetInitialInput(ctx context.Context, req serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
	return callLoopbackClient(c, "session lifecycle service is required", ctx, req, servicecontract.SessionLifecycleService.GetInitialInput)
}

func (c *loopbackSessionLifecycleClient) PersistInputDraft(ctx context.Context, req serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
	return callLoopbackClient(c, "session lifecycle service is required", ctx, req, servicecontract.SessionLifecycleService.PersistInputDraft)
}

func (c *loopbackSessionLifecycleClient) RetargetSessionWorkspace(ctx context.Context, req serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
	return callLoopbackClient(c, "session lifecycle service is required", ctx, req, servicecontract.SessionLifecycleService.RetargetSessionWorkspace)
}

func (c *loopbackSessionLifecycleClient) ResolveTransition(ctx context.Context, req serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
	return callLoopbackClient(c, "session lifecycle service is required", ctx, req, servicecontract.SessionLifecycleService.ResolveTransition)
}
