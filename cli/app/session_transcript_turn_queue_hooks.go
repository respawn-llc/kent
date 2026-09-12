package app

import (
	"sync"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

type turnQueueHook interface {
	OnTranscriptMessage(*transcriptpb.Message)
	OnTurnQueueDrained()
	OnTurnQueueAborted()
	OnUserCompactionCompleted(bool)
}

type taskCompletionSink interface {
	enqueueTaskCompletion(*transcriptpb.LiveRunFinished)
}

type turnQueueHooks struct {
	mu                     sync.Mutex
	notifications          *bellHooks
	taskCompletions        taskCompletionSink
	pendingTaskCompletions []*transcriptpb.LiveRunFinished
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
	if h.notifications != nil {
		h.notifications.OnTranscriptMessage(message)
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
	h.mu.Lock()
	h.pendingTaskCompletions = append(h.pendingTaskCompletions, result)
	h.mu.Unlock()
}

func (h *turnQueueHooks) OnTurnQueueDrained() {
	if h == nil {
		return
	}
	h.mu.Lock()
	pending := h.pendingTaskCompletions
	h.pendingTaskCompletions = nil
	h.mu.Unlock()
	for _, result := range pending {
		h.taskCompletions.enqueueTaskCompletion(result)
	}
	if h.notifications != nil {
		h.notifications.OnTurnQueueDrained()
	}
}

func (h *turnQueueHooks) OnTurnQueueAborted() {
	if h == nil || h.notifications == nil {
		return
	}
	h.notifications.OnTurnQueueAborted()
}

func (h *turnQueueHooks) OnUserCompactionCompleted(queueDrained bool) {
	if h == nil || h.notifications == nil {
		return
	}
	h.notifications.OnUserCompactionCompleted(queueDrained)
}
