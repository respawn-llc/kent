package app

func WithUITurnQueueHook(hook turnQueueHook) UIOption {
	return func(m *uiModel) { m.turnQueueHook = hook }
}

func WithUIOngoingTranscriptReopen(request func()) UIOption {
	return func(m *uiModel) { m.requestOngoingOpen = request }
}
