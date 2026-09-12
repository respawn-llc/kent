package app

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
)

func (h *bellHooks) OnTranscriptMessage(message *transcriptpb.Message) {
	switch message.Event.Payload.(type) {
	case *transcriptpb.Event_AssistantDelta:
		_ = message.Event.GetAssistantDelta()
	case *transcriptpb.Event_ToolStart:
		tool := message.Event.GetToolStart()
		h.recordToolCall(transcriptBellStepID(tool.StepId))
	case *transcriptpb.Event_StepState:
		step := message.Event.GetStepState()
		if step.Lifecycle == transcriptpb.StepLifecycle_STEP_LIFECYCLE_FINISHED {
			h.recordStepFinished(transcriptBellStepID(step.StepId))
		}
	case *transcriptpb.Event_LiveRunFinished:
		result := message.Event.GetLiveRunFinished()
		if result.ResultKind == transcriptpb.LiveRunResultKind_LIVE_RUN_RESULT_KIND_NO_FINAL_ANSWER {
			h.clearPendingTurnCompletionForNoFinal()
		}
	case *transcriptpb.Event_CommittedRow:
		assistant := message.Event.GetCommittedRow().GetAssistant()
		if assistant == nil {
			return
		}
		switch assistant.Phase {
		case transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL:
			h.recordTurnCompletion(transcriptBellStepID(assistant.StepId), assistant.Text)
		}
	}
}

func transcriptBellStepID(raw string) runtimeids.StepID {
	id, err := runtimeids.ParseStepID(raw)
	if err != nil {
		panic(err)
	}
	return id
}
