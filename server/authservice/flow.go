package authservice

import (
	"context"
	"errors"
	"os"
	"strings"

	"core/server/auth"
)

type FlowInteractionRequest struct {
	Manager        *auth.Manager
	State          auth.State
	StoredState    auth.State
	Gate           auth.StartupGate
	AuthRequired   bool
	PromptOptional bool
	StartupErr     error
	FlowErr        error
	OAuthOptions   auth.OpenAIOAuthOptions
	Theme          string
	HasEnvAPIKey   bool
}

type FlowInteractionOutcome struct {
	ProceedWithoutAuth bool
}

type FlowHandler interface {
	NeedsInteraction(req FlowInteractionRequest) bool
	Interact(ctx context.Context, req FlowInteractionRequest) (FlowInteractionOutcome, error)
}

func WrapStoreWithEnvAPIKeyOverride(base auth.Store, lookupEnv func(string) string) auth.Store {
	if lookupEnv == nil {
		lookupEnv = os.Getenv
	}
	return auth.NewEnvAPIKeyOverrideStore(base, func(key string) (string, bool) {
		value := lookupEnv(key)
		return value, strings.TrimSpace(value) != ""
	})
}

func EnsureFlowReady(ctx context.Context, mgr *auth.Manager, oauthOpts auth.OpenAIOAuthOptions, theme string, lookupEnv func(string) string, authRequired bool, promptOptional bool, handler FlowHandler) error {
	if mgr == nil {
		return errors.New("auth manager is required")
	}
	if handler == nil {
		return errors.New("auth flow handler is required")
	}
	if lookupEnv == nil {
		lookupEnv = os.Getenv
	}
	for {
		state, err := mgr.Load(ctx)
		if err != nil {
			return err
		}
		storedState, err := mgr.StoredState(ctx)
		if err != nil {
			return err
		}
		gate := auth.EvaluateStartupGate(state)
		var startupErr error
		if authRequired && !gate.Ready {
			startupErr = auth.EnsureStartupReady(state)
		}
		req := FlowInteractionRequest{
			Manager:        mgr,
			State:          state,
			StoredState:    storedState,
			Gate:           gate,
			AuthRequired:   authRequired,
			PromptOptional: promptOptional,
			StartupErr:     startupErr,
			OAuthOptions:   oauthOpts,
			Theme:          theme,
			HasEnvAPIKey:   strings.TrimSpace(lookupEnv("OPENAI_API_KEY")) != "",
		}
		if !handler.NeedsInteraction(req) {
			if startupErr != nil {
				return startupErr
			}
			return nil
		}
		outcome, err := handler.Interact(ctx, req)
		if err != nil {
			return err
		}
		if outcome.ProceedWithoutAuth {
			return nil
		}
	}
}
