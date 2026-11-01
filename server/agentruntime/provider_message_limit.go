package runtime

import (
	"errors"

	"core/server/llm"
)

// generationDispatchError distinguishes a failed ordinary dispatch from request
// preparation (which can itself dispatch compaction requests).
type generationDispatchError struct{ error }

func (e *generationDispatchError) Unwrap() error { return e.error }

type modelDispatchError struct{ error }

func (e *modelDispatchError) Unwrap() error { return e.error }

func isProviderMessageLimit(err error) bool {
	var provider *llm.ProviderAPIError
	return errors.As(err, &provider) &&
		provider.ProviderType == "response.incomplete" &&
		provider.ProviderCode == "max_messages"
}
