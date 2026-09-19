package runtime

import "core/shared/config"

func (e *Engine) NotifyConnectionReplacement(replacement config.ConnectionReplacement) error {
	return e.steerRuntime(steerEventIntent(Event{
		Kind: EventConnectionReplaced, ConnectionReplacement: &replacement,
	}))
}
