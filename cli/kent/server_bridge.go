package main

// Package serverbridge is the documented CLI binary composition bridge for
// local server startup/lifecycle wiring. Command handlers should depend on this
// package or shared client contracts instead of server packages directly.

import (
	"context"
	"errors"

	"core/server/sessionservice"
	"core/shared/client"
	"core/shared/config"
	"core/shared/serverapi"
)

type ServeServer interface {
	Close() error
	Serve(ctx context.Context) error
}

func NewLocalSessionLifecycleClient(cfg config.App) client.SessionLifecycleClient {
	client, err := sessionservice.NewMetadataBackedSessionLifecycleClient(cfg.PersistenceRoot, nil)
	if err != nil {
		return failingSessionLifecycleClient{err: err}
	}
	return client
}

type failingSessionLifecycleClient struct {
	err error
}

func (c failingSessionLifecycleClient) failure() error {
	if c.err == nil {
		return errors.New("session lifecycle client unavailable")
	}
	return c.err
}

func (c failingSessionLifecycleClient) Close() error {
	return nil
}

func (c failingSessionLifecycleClient) GetInitialInput(context.Context, serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
	return serverapi.SessionInitialInputResponse{}, c.failure()
}

func (c failingSessionLifecycleClient) PersistInputDraft(context.Context, serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
	return serverapi.SessionPersistInputDraftResponse{}, c.failure()
}

func (c failingSessionLifecycleClient) RetargetSessionWorkspace(context.Context, serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
	return serverapi.SessionRetargetWorkspaceResponse{}, c.failure()
}

func (c failingSessionLifecycleClient) ResolveTransition(context.Context, serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
	return serverapi.SessionResolveTransitionResponse{}, c.failure()
}
