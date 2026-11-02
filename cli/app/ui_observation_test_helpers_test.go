package app

import "core/cli/tui/ongoing"

func WithUITurnQueueHook(hook turnQueueHook) UIOption {
	return func(m *uiModel) { m.turnQueueHook = hook }
}

func WithUIOngoingTranscriptReopen(request func(ongoing.RehydrateReason)) UIOption {
	return func(m *uiModel) { m.requestOngoingOpen = request }
}
