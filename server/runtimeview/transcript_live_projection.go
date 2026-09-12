package runtimeview

import (
	"fmt"
	"strings"

	"core/server/llm"
	"core/server/runtime"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/transcript"

	"github.com/google/uuid"
)

func transcriptRequiredStepID(raw *string) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("runtime transcript fact is missing its Step")
	}
	stepID, err := runtimeids.ParseStepID(strings.TrimSpace(*raw))
	if err != nil {
		return "", err
	}
	return stepID.String(), nil
}

func transcriptAssistantIdentity(step *string, stream *uuid.UUID, phase llm.MessagePhase) (string, string, transcriptpb.AssistantPhase, error) {
	stepID, err := transcriptRequiredStepID(step)
	if err != nil {
		return "", "", transcriptpb.AssistantPhase_ASSISTANT_PHASE_UNSPECIFIED, err
	}
	if stream == nil {
		return "", "", transcriptpb.AssistantPhase_ASSISTANT_PHASE_UNSPECIFIED, fmt.Errorf("runtime assistant stream is missing its identity")
	}
	streamID, err := runtimeids.ParseAssistantStreamID(stream.String())
	if err != nil {
		return "", "", transcriptpb.AssistantPhase_ASSISTANT_PHASE_UNSPECIFIED, err
	}
	projectedPhase, err := transcriptAssistantPhase(transcript.AssistantPhase(phase))
	if err != nil {
		return "", "", transcriptpb.AssistantPhase_ASSISTANT_PHASE_UNSPECIFIED, err
	}
	return stepID, streamID.String(), projectedPhase, nil
}

func transcriptCompactionProjection(
	step *string,
	requestID *runtimeids.CompactionRequestID,
	mode string,
	count int,
	state transcriptpb.CompactionState,
	diagnostic *transcriptpb.Diagnostic,
) (*transcriptpb.CompactionStatus, error) {
	stepID, err := transcriptRequiredStepID(step)
	if err != nil {
		return nil, err
	}
	projectedCount, err := protoapi.Int32(count, "compaction count")
	if err != nil {
		return nil, err
	}
	projected := &transcriptpb.CompactionStatus{
		StepId: stepID, Count: projectedCount, State: state, Diagnostic: diagnostic,
	}
	switch strings.TrimSpace(mode) {
	case "auto":
		projected.Mode = transcriptpb.CompactionMode_COMPACTION_MODE_AUTO
	case "handoff":
		projected.Mode = transcriptpb.CompactionMode_COMPACTION_MODE_HANDOFF
	case "manual":
		projected.Mode = transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL
	case "workflow_post_completion":
		projected.Mode = transcriptpb.CompactionMode_COMPACTION_MODE_WORKFLOW_POST_COMPLETION
	default:
		return nil, fmt.Errorf("unknown compaction mode %q", mode)
	}
	if requestID != nil {
		value := requestID.String()
		projected.RequestId = &value
	}
	return projected, protoapi.Validate(projected)
}

func transcriptQueuedFailureReason(reason runtime.QueuedUserMessageFailureReason) (transcriptpb.QueuedMessageFailureReason, error) {
	switch reason {
	case runtime.QueuedUserMessageFailureClosing:
		return transcriptpb.QueuedMessageFailureReason_QUEUED_MESSAGE_FAILURE_REASON_CLOSING, nil
	case runtime.QueuedUserMessageFailureTerminalWorkflowCompletion:
		return transcriptpb.QueuedMessageFailureReason_QUEUED_MESSAGE_FAILURE_REASON_TERMINAL_WORKFLOW_COMPLETION, nil
	case runtime.QueuedUserMessageFailureRuntimeUnavailable:
		return transcriptpb.QueuedMessageFailureReason_QUEUED_MESSAGE_FAILURE_REASON_RUNTIME_UNAVAILABLE, nil
	default:
		return transcriptpb.QueuedMessageFailureReason_QUEUED_MESSAGE_FAILURE_REASON_UNSPECIFIED, fmt.Errorf("unknown queued message failure reason %q", reason)
	}
}

func transcriptPendingWorkRestoration(restoration *runtimeinput.PendingWorkTechnicalRestoration) (*transcriptpb.PendingWorkTechnicalRestoration, error) {
	projected := &transcriptpb.PendingWorkTechnicalRestoration{
		ItemId: restoration.ItemID.String(), CanonicalInput: restoration.CanonicalInput,
	}
	switch restoration.Kind {
	case runtimeinput.PendingWorkItemKindMessage:
		projected.Kind = runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_MESSAGE
	case runtimeinput.PendingWorkItemKindManualCompaction:
		projected.Kind = runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_MANUAL_COMPACTION
	case runtimeinput.PendingWorkItemKindWorktreeTransition:
		projected.Kind = runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_WORKTREE_TRANSITION
	default:
		return nil, fmt.Errorf("unknown pending work kind %q", restoration.Kind)
	}
	return projected, nil
}

func transcriptToolPresentation(toolName string, presentation *transcript.ToolCallMeta) (*transcriptpb.ToolPresentation, error) {
	toolName = strings.TrimSpace(toolName)
	if presentation == nil {
		if transcript.IsPatchFamilyToolName(toolName) {
			return nil, fmt.Errorf("Patch/Edit tool presentation is required")
		}
		return nil, nil
	}
	if strings.TrimSpace(presentation.ToolName) != toolName {
		return nil, fmt.Errorf("tool presentation name does not match tool identity")
	}
	return ToolPresentationToProto(presentation)
}
