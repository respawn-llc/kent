package app

import (
	"time"

	"core/shared/lifecyclecontract"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func (p *clientLifecycleProxy) AcceptTranscript(message *transcriptpb.Message) {
	if p == nil {
		return
	}
	switch message.Event.Payload.(type) {
	case *transcriptpb.Event_Hydration:
		hydration := message.Event.GetHydration()
		p.acceptSessionIdentity(hydration.SessionIdentity)
		p.acceptSessionStatus(hydration.SessionStatus)
		for _, prompt := range hydration.PendingPrompts {
			p.acceptPendingPrompt(prompt)
		}
	case *transcriptpb.Event_SessionIdentity:
		p.acceptSessionIdentity(message.Event.GetSessionIdentity())
	case *transcriptpb.Event_SessionStatus:
		p.acceptSessionStatus(message.Event.GetSessionStatus())
	case *transcriptpb.Event_LiveRunFinished:
		p.acceptLiveRunFailure(message.Event.GetLiveRunFinished())
	case *transcriptpb.Event_CompactionStatus:
		status := message.Event.GetCompactionStatus()
		if status.State == transcriptpb.CompactionState_COMPACTION_STATE_STARTED {
			p.enqueue(lifecyclecontract.NewCompactionStarted(
				time.Now().UTC(),
				p.isFocused(),
				p.context(),
				transcriptCompactionModeName(status.Mode),
			))
		}
	case *transcriptpb.Event_Prompt:
		prompt := message.Event.GetPrompt()
		if prompt.Status == transcriptpb.PromptStatus_PROMPT_STATUS_PENDING {
			p.acceptPendingPrompt(prompt)
		}
	}
}

func (p *clientLifecycleProxy) acceptPendingPrompt(prompt *transcriptpb.Prompt) {
	var kind lifecyclecontract.InputKind
	switch prompt.Prompt.(type) {
	case *transcriptpb.Prompt_Question:
		kind = lifecyclecontract.InputKindQuestion
	case *transcriptpb.Prompt_Approval:
		kind = lifecyclecontract.InputKindApproval
	default:
		return
	}
	p.enqueue(lifecyclecontract.NewInputRequired(
		transcriptPromptCreatedAt(prompt),
		p.isFocused(),
		p.context(),
		kind,
		transcriptPromptQuestion(prompt),
	))
}

func transcriptCompactionModeName(mode transcriptpb.CompactionMode) string {
	switch mode {
	case transcriptpb.CompactionMode_COMPACTION_MODE_AUTO:
		return "auto"
	case transcriptpb.CompactionMode_COMPACTION_MODE_HANDOFF:
		return "handoff"
	case transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL:
		return "manual"
	case transcriptpb.CompactionMode_COMPACTION_MODE_WORKFLOW_POST_COMPLETION:
		return "workflow_post_completion"
	default:
		panic("invalid transcript compaction mode")
	}
}
