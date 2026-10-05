package runprompt

import (
	"strings"

	"core/server/llm"
	"core/server/runtime"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/emptypb"
)

func PublishRunPromptProgress(progress serverapi.RunPromptProgressSink, evt runtime.Event) {
	if progress == nil {
		return
	}
	state, ok := RunPromptProgressFromRuntimeEvent(evt)
	if !ok {
		return
	}
	progress.PublishRunPromptProgress(state)
}

func RunPromptProgressFromRuntimeEvent(evt runtime.Event) (*runpromptpb.ProgressEvent, bool) {
	switch evt.Kind {
	case runtime.EventAssistantMessage:
		content := evt.Message.Content
		if evt.Message.Role != llm.RoleAssistant ||
			content == nil ||
			evt.Message.Phase == nil ||
			strings.TrimSpace(*content) == "" {
			return nil, false
		}
		var phase runpromptpb.MessagePhase
		switch *evt.Message.Phase {
		case llm.MessagePhaseCommentary:
			phase = runpromptpb.MessagePhase_MESSAGE_PHASE_COMMENTARY
		case llm.MessagePhaseFinal:
			phase = runpromptpb.MessagePhase_MESSAGE_PHASE_FINAL
		default:
			return nil, false
		}
		return &runpromptpb.ProgressEvent{
			Payload: &runpromptpb.ProgressEvent_AssistantMessage{AssistantMessage: &runpromptpb.AssistantMessage{
				Phase:   phase,
				Content: *content,
			}},
		}, true
	case runtime.EventCompactionStarted:
		return &runpromptpb.ProgressEvent{Payload: &runpromptpb.ProgressEvent_CompactionStarted{CompactionStarted: &emptypb.Empty{}}}, true
	case runtime.EventCompactionFailed:
		var detail string
		if evt.Compaction != nil {
			detail = evt.Compaction.Error
		}
		return &runpromptpb.ProgressEvent{Payload: &runpromptpb.ProgressEvent_CompactionFailed{CompactionFailed: runPromptFailure(detail)}}, true
	case runtime.EventInFlightClearFailed:
		return &runpromptpb.ProgressEvent{
			Payload: &runpromptpb.ProgressEvent_RunCleanupFailed{RunCleanupFailed: runPromptFailure(evt.Error)},
		}, true
	case runtime.EventQueuedUserMessageStatus:
		status := evt.QueuedUserMessageStatus
		if status == nil || status.Status != runtime.QueuedUserMessageAccepted {
			return nil, false
		}
		content := status.Text
		if strings.TrimSpace(content) == "" {
			return nil, false
		}
		return &runpromptpb.ProgressEvent{
			Payload: &runpromptpb.ProgressEvent_SteeredMessage{SteeredMessage: &runpromptpb.SteeredMessage{Content: content}},
		}, true
	default:
		return nil, false
	}
}

func runPromptFailure(raw string) *runpromptpb.ProgressFailure {
	detail := strings.TrimSpace(raw)
	if detail == "" {
		return &runpromptpb.ProgressFailure{}
	}
	return &runpromptpb.ProgressFailure{Error: &detail}
}
