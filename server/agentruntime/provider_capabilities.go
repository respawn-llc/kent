package runtime

import (
	"context"
	"fmt"

	"core/server/llm"
)

func (e *Engine) providerCapabilities(ctx context.Context) (llm.ProviderCapabilities, error) {
	if e.cfg.ProviderCapabilitiesOverride != nil {
		return *e.cfg.ProviderCapabilitiesOverride, nil
	}
	if caps, ok := llm.ProviderCapabilitiesFromLocked(e.store.Meta().Locked); ok {
		return caps, nil
	}
	return e.currentProviderCapabilities(ctx)
}

func (e *Engine) currentProviderCapabilities(ctx context.Context) (llm.ProviderCapabilities, error) {
	if e.cfg.ProviderCapabilitiesOverride != nil {
		return *e.cfg.ProviderCapabilitiesOverride, nil
	}
	providerCaps, err := e.llm.capabilities(ctx)
	if err != nil {
		return llm.ProviderCapabilities{}, fmt.Errorf("resolve provider capabilities: %w", err)
	}
	return providerCaps, nil
}
