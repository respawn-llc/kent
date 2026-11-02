package app

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

type turnQueueHook interface {
	OnSessionTurnEnd(string)
	OnTranscriptMessage(*transcriptpb.Message)
	OnUserCompactionCompleted()
}

func (h *turnQueueHooks) OnSessionTurnEnd(content string) {
	if h != nil && h.notifications != nil {
		h.notifications.OnSessionTurnEnd(content)
	}
}

type taskCompletionSink interface {
	enqueueTaskCompletion(*transcriptpb.LiveRunFinished)
}

type turnQueueHooks struct {
	notifications   *bellHooks
	taskCompletions taskCompletionSink
}

func newTurnQueueHooks(
	notifications *bellHooks,
	taskCompletions taskCompletionSink,
) *turnQueueHooks {
	return &turnQueueHooks{
		notifications:   notifications,
		taskCompletions: taskCompletions,
	}
}

func (h *turnQueueHooks) OnTranscriptMessage(message *transcriptpb.Message) {
	if h == nil {
		return
	}
	if h.taskCompletions == nil || message.Event.GetLiveRunFinished() == nil {
		return
	}
	result := message.Event.GetLiveRunFinished()
	if result.Status != transcriptpb.LiveRunStatus_LIVE_RUN_STATUS_COMPLETED ||
		result.ResultKind != transcriptpb.LiveRunResultKind_LIVE_RUN_RESULT_KIND_ASSISTANT_FINAL_ANSWER ||
		result.FinalAnswer == nil {
		return
	}
	h.taskCompletions.enqueueTaskCompletion(result)
}

func (h *turnQueueHooks) OnUserCompactionCompleted() {
	if h == nil || h.notifications == nil {
		return
	}
	h.notifications.OnUserCompactionCompleted()
}
