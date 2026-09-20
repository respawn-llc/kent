package startup

import (
	"context"
	"os"

	"core/server/auth"
	"core/server/authservice"
)

type headlessAuthHandler struct {
	lookupEnv func(string) string
}

func NewHeadlessAuthHandler(lookupEnv func(string) string) AuthHandler {
	return headlessAuthHandler{lookupEnv: lookupEnv}
}

func (h headlessAuthHandler) WrapStore(base auth.Store) auth.Store {
	return authservice.WrapStoreWithEnvAPIKeyOverride(base, h.LookupEnv)
}

func (h headlessAuthHandler) NeedsInteraction(req authservice.FlowInteractionRequest) bool {
	return req.AuthRequired && !req.Gate.Ready
}

func (h headlessAuthHandler) Interact(context.Context, authservice.FlowInteractionRequest) (authservice.FlowInteractionOutcome, error) {
	return authservice.FlowInteractionOutcome{}, auth.ErrAuthNotConfigured
}

func (h headlessAuthHandler) LookupEnv(key string) string {
	if h.lookupEnv == nil {
		return os.Getenv(key)
	}
	return h.lookupEnv(key)
}
