package registry

import (
	"context"
	"fmt"

	"core/server/runtime"
	"core/server/runtimeview"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func (r *RuntimeRegistry) composeTranscriptHydration(
	ctx context.Context,
	sessionID string,
	entry *authorityRuntimeEntry,
	snapshot runtime.TranscriptHydrationSnapshot,
	tailPage runtime.TranscriptSegmentPage,
) (*transcriptpb.Hydration, error) {
	tailSegment, err := runtimeview.TranscriptTailSegmentFromSegment(tailPage)
	if err != nil {
		return nil, transcriptHydrationProjectionError{
			cause: fmt.Errorf("project transcript hydration tail segment: %w", err),
		}
	}
	hydration, err := runtimeview.TranscriptHydrationFromSnapshotChecked(snapshot, tailSegment)
	if err != nil {
		return nil, transcriptHydrationProjectionError{
			cause: fmt.Errorf("project transcript hydration: %w", err),
		}
	}
	readModel, err := r.RuntimeReadModelFeedSnapshot(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("build transcript runtime read model: %w", err)
	}
	hydration.RuntimeReadModelUpdate = readModel
	hydration.ActiveStep = transcriptActiveStepFromRuntimeReadModel(readModel)
	clearMismatchedActiveFacts(hydration)
	hydration.SessionStatus, err = runtimeview.TranscriptSessionStatusFromRuntime(entry.engine)
	if err != nil {
		return nil, fmt.Errorf("build transcript session status: %w", err)
	}
	hydration.SessionIdentity, err = runtimeview.TranscriptSessionIdentityFromRuntime(entry.engine)
	if err != nil {
		return nil, fmt.Errorf("build transcript session identity: %w", err)
	}
	target, err := r.resolveSessionExecutionTarget(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	hydration.SessionIdentity.ExecutionTarget = target

	hydration.PendingPrompts, err = r.transcriptPendingPrompts(sessionID, readModel.Activity.ActiveStep)
	if err != nil {
		return nil, err
	}
	hydration.BackgroundActivities, err = r.backgroundActivitiesForSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("build transcript background activities: %w", err)
	}
	if err := protoapi.Validate(hydration); err != nil {
		return nil, transcriptHydrationProjectionError{
			cause: fmt.Errorf("validate canonical transcript hydration: %w", err),
		}
	}
	return hydration, nil
}

func transcriptActiveStepFromRuntimeReadModel(
	update *runtimepb.ReadModelUpdate,
) *transcriptpb.StepState {
	active := update.Activity.ActiveStep
	if active == nil {
		return nil
	}
	return &transcriptpb.StepState{
		RunId:      active.RunId,
		StepId:     active.StepId,
		Lifecycle:  transcriptpb.StepLifecycle_STEP_LIFECYCLE_STARTED,
		ActiveKind: active.ActiveKind,
		Status:     transcriptpb.RunStatus_RUN_STATUS_RUNNING,
	}
}

func clearMismatchedActiveFacts(hydration *transcriptpb.Hydration) {
	if hydration == nil {
		return
	}
	active := hydration.RuntimeReadModelUpdate.Activity.ActiveStep
	if active == nil {
		hydration.ActiveAssistant = nil
		hydration.ActiveThinkingStatus = nil
		hydration.ActiveReasoningTraces = nil
		hydration.ActiveStep = nil
		hydration.ActiveCompaction = nil
		hydration.InFlightTools = nil
		return
	}
	if hydration.ActiveAssistant != nil && hydration.ActiveAssistant.StepId != active.StepId {
		hydration.ActiveAssistant = nil
	}
	if hydration.ActiveThinkingStatus != nil && hydration.ActiveThinkingStatus.StepId != active.StepId {
		hydration.ActiveThinkingStatus = nil
	}
	if len(hydration.ActiveReasoningTraces) > 0 {
		traces := hydration.ActiveReasoningTraces[:0]
		for _, trace := range hydration.ActiveReasoningTraces {
			if trace.StepId == active.StepId {
				traces = append(traces, trace)
			}
		}
		hydration.ActiveReasoningTraces = traces
	}
	if hydration.ActiveStep != nil &&
		(hydration.ActiveStep.RunId != active.RunId ||
			hydration.ActiveStep.StepId != active.StepId ||
			hydration.ActiveStep.ActiveKind != active.ActiveKind) {
		hydration.ActiveStep = nil
	}
	if hydration.ActiveCompaction != nil && hydration.ActiveCompaction.StepId != active.StepId {
		hydration.ActiveCompaction = nil
	}
	if len(hydration.InFlightTools) > 0 {
		tools := hydration.InFlightTools[:0]
		for _, tool := range hydration.InFlightTools {
			if tool.StepId == active.StepId {
				tools = append(tools, tool)
			}
		}
		hydration.InFlightTools = tools
	}
}

func (r *RuntimeRegistry) transcriptPendingPrompts(
	sessionID string,
	activeStep *runtimepb.ActiveStep,
) ([]*transcriptpb.Prompt, error) {
	snapshots := r.pendingPrompts.List(sessionID)
	if len(snapshots) == 0 {
		return nil, nil
	}
	prompts := make([]*transcriptpb.Prompt, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if activeStep == nil || snapshot.Request.StepID != activeStep.StepId {
			continue
		}
		prompt, err := transcriptPendingPromptFromSnapshot(sessionID, snapshot, pendingPromptEventPending)
		if err != nil {
			return nil, err
		}
		prompts = append(prompts, prompt)
	}
	return prompts, nil
}
