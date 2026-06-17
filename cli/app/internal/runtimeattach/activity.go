package runtimeattach

import (
	"context"
	"errors"
	"strings"

	servicecontract "core/shared/apicontract"
	"core/shared/serverapi"
)

type ActivityRequest struct {
	SessionID       string
	Runtime         servicecontract.SessionRuntimeService
	LeaseID         string
	Mode            serverapi.SessionRuntimeAttachMode
	ReadOnly        bool
	SessionActivity servicecontract.SessionActivityService
	PromptActivity  servicecontract.PromptActivityService
}

type Activities struct {
	Session serverapi.SessionActivitySubscription
	Prompt  serverapi.PromptActivitySubscription
}

func SubscribeActivities(ctx context.Context, req ActivityRequest) (Activities, error) {
	if req.SessionActivity == nil {
		releaseIfOwned(req)
		return Activities{}, errors.New("session activity service is required")
	}
	if req.PromptActivity == nil && shouldSubscribePromptActivity(req) {
		releaseIfOwned(req)
		return Activities{}, errors.New("prompt activity service is required")
	}
	sessionSub, err := req.SessionActivity.SubscribeSessionActivity(ctx, serverapi.SessionActivitySubscribeRequest{SessionID: req.SessionID})
	if err != nil {
		releaseIfOwned(req)
		return Activities{}, err
	}
	if !shouldSubscribePromptActivity(req) {
		return Activities{Session: sessionSub}, nil
	}
	promptSub, err := req.PromptActivity.SubscribePromptActivity(ctx, serverapi.PromptActivitySubscribeRequest{SessionID: req.SessionID})
	if err != nil {
		_ = sessionSub.Close()
		releaseIfOwned(req)
		return Activities{}, err
	}
	return Activities{Session: sessionSub, Prompt: promptSub}, nil
}

func releaseIfOwned(req ActivityRequest) {
	if req.ReadOnly || req.Mode == serverapi.SessionRuntimeAttachModeCollaborative || strings.TrimSpace(req.LeaseID) == "" {
		return
	}
	Release(req.Runtime, req.SessionID, req.LeaseID)
}

func shouldSubscribePromptActivity(req ActivityRequest) bool {
	return !req.ReadOnly && req.Mode != serverapi.SessionRuntimeAttachModeNoControl
}
