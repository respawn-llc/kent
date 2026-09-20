package runtime

import "core/shared/config"

func (e *Engine) PrepareConnectionReplacement(replacement config.ConnectionReplacement) error {
	return e.steerRuntime(steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items:    []steeringItem{{connectionReplacement: &replacement}},
	})
}

func (e *Engine) PublishConnectionReplacement() error {
	replacement := e.transcriptRuntimeState().ConnectionReplacement()
	if replacement == nil {
		return nil
	}
	return e.steerRuntime(steerEventIntent(Event{
		Kind: EventConnectionReplaced, ConnectionReplacement: replacement,
	}))
}
