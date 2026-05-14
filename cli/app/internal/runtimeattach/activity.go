package runtimeattach

import (
	"context"
	"errors"

	"builder/shared/serverapi"
	"builder/shared/servicecontract"
)

type ActivityRequest struct {
	SessionID       string
	Runtime         servicecontract.SessionRuntimeService
	LeaseID         string
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
	if req.PromptActivity == nil && !req.ReadOnly {
		releaseIfOwned(req)
		return Activities{}, errors.New("prompt activity service is required")
	}
	sessionSub, err := req.SessionActivity.SubscribeSessionActivity(ctx, serverapi.SessionActivitySubscribeRequest{SessionID: req.SessionID})
	if err != nil {
		releaseIfOwned(req)
		return Activities{}, err
	}
	if req.ReadOnly {
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
	if req.ReadOnly {
		return
	}
	Release(req.Runtime, req.SessionID, req.LeaseID)
}
