package runtime

import (
	"context"

	"core/server/llm"
)

func (e *Engine) providerCapabilities(ctx context.Context) (llm.ProviderCapabilities, error) {
	return e.connectionSnapshot().requestCapabilities, nil
}
