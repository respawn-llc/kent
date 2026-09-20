package runtimeview

import (
	"errors"
	"fmt"
	"strings"

	"core/server/goalview"
	"core/server/llm"
	"core/server/runtime"
	"core/server/runtimeactivity"
	"core/server/session"
	"core/server/tools"
	shelltool "core/server/tools/shell"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"core/shared/transcript"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TranscriptHydrationFromSnapshotChecked(
	runtimeSnapshot runtime.TranscriptHydrationSnapshot,
	tailSegment *transcriptpb.TailSegment,
) (*transcriptpb.Hydration, error) {
	if tailSegment == nil {
		return nil, fmt.Errorf("transcript hydration tail segment is required")
	}
	if err := protoapi.Validate(tailSegment); err != nil {
		return nil, fmt.Errorf("validate transcript hydration tail segment: %w", err)
	}
	assistant, err := transcriptAssistantStream(runtimeSnapshot)
	if err != nil {
		return nil, err
	}
	hydration := &transcriptpb.Hydration{TailSegment: tailSegment, ActiveAssistant: assistant}
	hydration.ActiveThinkingStatus = transcriptThinkingStatusFromRuntime(runtimeSnapshot.ActiveThinkingStatus)
	hydration.ActiveReasoningTraces, err = transcriptReasoningTracesFromRuntime(runtimeSnapshot.ActiveReasoningTraces)
	if err != nil {
		return nil, err
	}
	hydration.InFlightTools, err = transcriptToolStartsFromRuntime(runtimeSnapshot.InFlightTools)
	if err != nil {
		return nil, err
	}
	hydration.ActiveCompaction, err = transcriptCompactionStateFromRuntime(runtimeSnapshot.ActiveCompaction)
	if err != nil {
		return nil, err
	}
	hydration.ContextUsage, err = transcriptContextUsageFromRuntime(runtimeSnapshot.ContextUsage)
	if err != nil {
		return nil, err
	}
	hydration.GoalStatus, err = transcriptGoalStatusFromRuntime(runtimeSnapshot.Goal, runtimeSnapshot.GoalSuspended)
	if err != nil {
		return nil, err
	}
	// The registry supplies Session identity/status and the canonical read model
	// before validating the complete hydration and its ownership constraints.
	if hydration.ActiveThinkingStatus != nil {
		if err := protoapi.Validate(hydration.ActiveThinkingStatus); err != nil {
			return nil, err
		}
	}
	return hydration, nil
}

func transcriptThinkingStatusFromRuntime(state *runtime.TranscriptThinkingStatusState) *transcriptpb.ThinkingStatusUpdate {
	if state == nil {
		return nil
	}
	return &transcriptpb.ThinkingStatusUpdate{
		StepId: strings.TrimSpace(state.StepID),
		Text:   state.Text,
	}
}

func transcriptReasoningTracesFromRuntime(states []runtime.TranscriptReasoningTraceState) ([]*transcriptpb.ReasoningTraceUpdate, error) {
	if len(states) == 0 {
		return nil, nil
	}
	out := make([]*transcriptpb.ReasoningTraceUpdate, 0, len(states))
	for index, state := range states {
		presentation := runtime.ProjectReasoningTrace(state.Text)
		identity, err := transcriptReasoningTraceIdentityProjection(&state.Identity, fmt.Sprintf("hydrated reasoning trace %d", index))
		if err != nil {
			return nil, fmt.Errorf("hydrated reasoning trace %d: %w", index, err)
		}
		trace := &transcriptpb.ReasoningTraceUpdate{
			StepId:      strings.TrimSpace(state.StepID),
			Identity:    identity,
			CompactText: presentation.CompactText,
			Text:        presentation.Text,
		}
		if err := protoapi.Validate(trace); err != nil {
			return nil, err
		}
		out = append(out, trace)
	}
	return out, nil
}

func transcriptCompactionStateFromRuntime(state *runtime.TranscriptCompactionState) (*transcriptpb.CompactionStatus, error) {
	if state == nil {
		return nil, nil
	}
	return transcriptCompactionProjection(&state.StepID, state.RequestID, state.Mode, state.Count, transcriptpb.CompactionState_COMPACTION_STATE_STARTED, nil)
}

func transcriptContextUsageFromRuntime(usage *runtime.ContextUsage) (*runtimepb.ContextUsage, error) {
	if usage == nil {
		return nil, nil
	}
	used, err := protoapi.Int32(usage.UsedTokens, "used tokens")
	if err != nil {
		return nil, err
	}
	window, err := protoapi.Int32(usage.WindowTokens, "context window tokens")
	if err != nil {
		return nil, err
	}
	projected := &runtimepb.ContextUsage{
		UsedTokens:   used,
		WindowTokens: window,
	}
	if usage.HasCacheHitPercentage {
		cacheHitPercent, err := protoapi.Int32(usage.CacheHitPercent, "cache hit percent")
		if err != nil {
			return nil, err
		}
		projected.CacheHitPercent = &cacheHitPercent
	}
	return projected, protoapi.Validate(projected)
}

func transcriptGoalStatusFromRuntime(goal *session.GoalState, suspended bool) (*runtimepb.GoalView, error) {
	if goal == nil {
		return nil, nil
	}
	core, err := goalview.CoreFromSessionState(goal)
	if err != nil {
		return nil, err
	}
	projected := &runtimepb.GoalView{Goal: core, Suspended: suspended}
	return projected, protoapi.Validate(projected)
}

func transcriptToolStartsFromRuntime(starts []runtime.TranscriptLiveToolStart) ([]*transcriptpb.ToolStart, error) {
	if len(starts) == 0 {
		return nil, nil
	}
	out := make([]*transcriptpb.ToolStart, 0, len(starts))
	for index, start := range starts {
		presentation, err := transcriptToolPresentation(start.ToolName, start.Presentation)
		if err != nil {
			return nil, fmt.Errorf("in-flight tool %d: %w", index, err)
		}
		projected := &transcriptpb.ToolStart{
			StepId:       strings.TrimSpace(start.StepID),
			ToolCallId:   strings.TrimSpace(start.ToolCallID),
			ToolName:     strings.TrimSpace(start.ToolName),
			Presentation: presentation,
		}
		if err := protoapi.Validate(projected); err != nil {
			return nil, fmt.Errorf("in-flight tool %d: %w", index, err)
		}
		out = append(out, projected)
	}
	return out, nil
}

func TranscriptMessagesFromRuntimeEventChecked(evt runtime.Event) ([]*transcriptpb.Event, error) {
	for index, fact := range runtime.TranscriptCommittedRowFactsFromEvent(evt) {
		if err := fact.Locator.Validate(); err != nil {
			return nil, fmt.Errorf("runtime committed row fact %d from event %q lacks valid provenance: %w", index, evt.Kind, err)
		}
	}
	messages, err := transcriptMessagesFromRuntimeEvent(evt)
	if err != nil {
		return nil, fmt.Errorf("runtime transcript event %q: %w", evt.Kind, err)
	}
	for index := range messages {
		if err := protoapi.Validate(messages[index]); err != nil {
			return nil, fmt.Errorf(
				"runtime transcript event %q projection %d violates contract: %w",
				evt.Kind,
				index,
				err,
			)
		}
	}
	return messages, nil
}

func transcriptMessagesFromRuntimeEvent(evt runtime.Event) ([]*transcriptpb.Event, error) {
	switch evt.Kind {
	case runtime.EventAssistantDelta:
		if evt.AssistantDelta == "" {
			return nil, nil
		}
		stepID, streamID, phase, err := transcriptAssistantIdentity(evt.StepID, evt.AssistantTranscriptStreamID, evt.AssistantDeltaPhase)
		if err != nil {
			return nil, err
		}
		delta := &transcriptpb.AssistantDelta{
			StepId:   stepID,
			StreamId: streamID,
			Delta:    evt.AssistantDelta,
			Phase:    phase,
		}
		return []*transcriptpb.Event{{Payload: &transcriptpb.Event_AssistantDelta{AssistantDelta: delta}}}, nil
	case runtime.EventAssistantDeltaReset:
		reason := strings.TrimSpace(evt.AssistantStreamAbortReason)
		if reason == "" || evt.AssistantTranscriptStreamID == nil {
			return nil, nil
		}
		stepID, err := transcriptRequiredStepID(evt.StepID)
		if err != nil {
			return nil, err
		}
		abortReason, err := transcriptAssistantAbortReason(reason)
		if err != nil {
			return nil, err
		}
		abort := &transcriptpb.AssistantStreamAbort{
			StepId: stepID, StreamId: evt.AssistantTranscriptStreamID.String(), Reason: abortReason,
		}
		return []*transcriptpb.Event{{Payload: &transcriptpb.Event_AssistantStreamAbort{AssistantStreamAbort: abort}}}, nil
	case runtime.EventReasoningDelta:
		if evt.ReasoningDelta == nil {
			return nil, nil
		}
		if evt.ReasoningTraceIdentity == nil && strings.TrimSpace(evt.ReasoningDelta.Text) == "" && evt.ReasoningDelta.CurrentStatus == nil {
			return nil, nil
		}
		presentation := runtime.ProjectReasoningTrace(evt.ReasoningDelta.Text)
		stepID, err := transcriptRequiredStepID(evt.StepID)
		if err != nil {
			return nil, err
		}
		var messages []*transcriptpb.Event
		if evt.ReasoningDelta.CurrentStatus != nil {
			status := &transcriptpb.ThinkingStatusUpdate{StepId: stepID, Text: evt.ReasoningDelta.CurrentStatus.Text}
			messages = append(messages, &transcriptpb.Event{Payload: &transcriptpb.Event_ThinkingStatusUpdate{ThinkingStatusUpdate: status}})
		}
		if evt.ReasoningTraceIdentity == nil && strings.TrimSpace(evt.ReasoningDelta.Text) == "" {
			return messages, nil
		}
		traceIdentity, err := transcriptReasoningTraceIdentityProjection(evt.ReasoningTraceIdentity, "reasoning delta")
		if err != nil {
			return nil, err
		}
		update := &transcriptpb.ReasoningTraceUpdate{
			StepId:      stepID,
			Identity:    traceIdentity,
			CompactText: presentation.CompactText,
			Text:        presentation.Text,
		}
		return append(messages, &transcriptpb.Event{Payload: &transcriptpb.Event_ReasoningTraceUpdate{ReasoningTraceUpdate: update}}), nil
	case runtime.EventReasoningDeltaReset:
		stepID, err := transcriptRequiredStepID(evt.StepID)
		if err != nil {
			return nil, err
		}
		reset := &transcriptpb.ReasoningTraceReset{StepId: stepID}
		return []*transcriptpb.Event{{Payload: &transcriptpb.Event_ReasoningTraceReset{ReasoningTraceReset: reset}}}, nil
	case runtime.EventToolCallStarted:
		starts, err := runtime.TranscriptToolStartFactsFromEventChecked(evt)
		if err != nil {
			return nil, err
		}
		return transcriptToolStartMessages(starts)
	case runtime.EventToolCallAborted:
		return transcriptToolAbortMessages(evt)
	case runtime.EventQueuedUserMessageStatus:
		return transcriptQueuedMessageStateMessages(evt)
	case runtime.EventPendingWorkChanged:
		return []*transcriptpb.Event{{Payload: &transcriptpb.Event_PendingWorkChanged{PendingWorkChanged: &transcriptpb.PendingWorkChanged{}}}}, nil
	case runtime.EventPendingWorkRestored:
		if evt.PendingWorkRestoration == nil {
			return nil, nil
		}
		restoration, err := transcriptPendingWorkRestoration(evt.PendingWorkRestoration)
		if err != nil {
			return nil, err
		}
		return []*transcriptpb.Event{{Payload: &transcriptpb.Event_PendingWorkRestored{PendingWorkRestored: &transcriptpb.PendingWorkRestored{Restoration: restoration}}}}, nil
	case runtime.EventHumanInputInterrupted:
		return transcriptHumanInputInterruptedMessages(evt)
	case runtime.EventRunStateChanged:
		return transcriptStepStateMessages(evt)
	case runtime.EventLiveRunFinished:
		return transcriptLiveRunFinishedMessages(evt)
	case runtime.EventConnectionReplaced:
		if evt.ConnectionReplacement == nil {
			return nil, errors.New("connection replacement event has no binding change")
		}
		return []*transcriptpb.Event{{Payload: &transcriptpb.Event_ConnectionReplaced{ConnectionReplaced: &transcriptpb.ConnectionReplacement{
			PreviousId: string(evt.ConnectionReplacement.Previous), CurrentId: string(evt.ConnectionReplacement.Current),
		}}}}, nil
	case runtime.EventSleepGuardFailed,
		runtime.EventPromptHistoryPersistFailed,
		runtime.EventContextFactsPersistFailed,
		runtime.EventInFlightClearFailed:
		return transcriptOperationalDiagnosticMessages(evt)
	case runtime.EventUserMessageFlushed:
		messages, err := transcriptFeedStateMessages(evt)
		if err != nil {
			return nil, err
		}
		flushed, err := transcriptUserMessageFlushedMessages(evt)
		if err != nil {
			return nil, err
		}
		rows, err := transcriptCommittedRowMessages(evt)
		return append(append(messages, flushed...), rows...), err
	default:
		messages, err := transcriptFeedStateMessages(evt)
		if err != nil {
			return nil, err
		}
		rows, err := transcriptCommittedRowMessages(evt)
		return append(messages, rows...), err
	}
}

func transcriptLiveRunFinishedMessages(evt runtime.Event) ([]*transcriptpb.Event, error) {
	if evt.LiveRunResult == nil {
		return nil, nil
	}
	result := evt.LiveRunResult
	projected := &transcriptpb.LiveRunFinished{
		NoFinalReason: string(result.NoFinalReason),
		WorkPerformed: result.WorkPerformed,
		StartedAt:     timestamppb.New(result.StartedAt),
		FinishedAt:    timestamppb.New(result.FinishedAt),
	}
	switch result.Status {
	case runtime.RunStatusCompleted:
		projected.Status = transcriptpb.LiveRunStatus_LIVE_RUN_STATUS_COMPLETED
	case runtime.RunStatusInterrupted:
		projected.Status = transcriptpb.LiveRunStatus_LIVE_RUN_STATUS_INTERRUPTED
	case runtime.RunStatusFailed:
		projected.Status = transcriptpb.LiveRunStatus_LIVE_RUN_STATUS_FAILED
	default:
		return nil, fmt.Errorf("unknown live run status %q", result.Status)
	}
	switch result.ResultKind {
	case runtime.LiveRunResultAssistantFinalAnswer:
		projected.ResultKind = transcriptpb.LiveRunResultKind_LIVE_RUN_RESULT_KIND_ASSISTANT_FINAL_ANSWER
	case runtime.LiveRunResultNoFinalAnswer:
		projected.ResultKind = transcriptpb.LiveRunResultKind_LIVE_RUN_RESULT_KIND_NO_FINAL_ANSWER
	default:
		return nil, fmt.Errorf("unknown live run result kind %q", result.ResultKind)
	}
	if result.ResultKind == runtime.LiveRunResultAssistantFinalAnswer {
		if result.AssistantMessage.Content != nil {
			projected.FinalAnswer = textutil.Pointer(result.AssistantMessage.Content)
		} else {
			projected.ResultKind = transcriptpb.LiveRunResultKind_LIVE_RUN_RESULT_KIND_NO_FINAL_ANSWER
		}
	}
	if result.Status == runtime.RunStatusFailed && result.Error != nil {
		failure := result.Error.Error()
		projected.Failure = &failure
	}
	return []*transcriptpb.Event{{Payload: &transcriptpb.Event_LiveRunFinished{LiveRunFinished: projected}}}, nil
}

func transcriptFeedStateMessages(evt runtime.Event) ([]*transcriptpb.Event, error) {
	out := make([]*transcriptpb.Event, 0, 4)
	if evt.Compaction != nil {
		status, err := transcriptCompactionStatus(evt)
		if err != nil {
			return nil, err
		}
		out = append(out, &transcriptpb.Event{Payload: &transcriptpb.Event_CompactionStatus{CompactionStatus: status}})
	}
	if evt.ContextUsage != nil {
		usage, err := transcriptContextUsageFromRuntime(evt.ContextUsage)
		if err != nil {
			return nil, err
		}
		out = append(out, &transcriptpb.Event{Payload: &transcriptpb.Event_ContextUsage{ContextUsage: usage}})
	}
	if evt.GoalStatus != nil {
		goal, err := transcriptGoalStatus(*evt.GoalStatus)
		if err != nil {
			return nil, err
		}
		out = append(out, &transcriptpb.Event{Payload: &transcriptpb.Event_GoalStatus{GoalStatus: goal}})
	}
	if evt.Background != nil {
		background, err := transcriptBackgroundActivity(*evt.Background)
		if err != nil {
			return nil, err
		}
		out = append(out, &transcriptpb.Event{Payload: &transcriptpb.Event_BackgroundActivity{BackgroundActivity: background}})
	}
	return out, nil
}

func transcriptCompactionStatus(evt runtime.Event) (*transcriptpb.CompactionStatus, error) {
	var state transcriptpb.CompactionState
	var diagnostic *transcriptpb.Diagnostic
	switch evt.Kind {
	case runtime.EventCompactionStarted:
		state = transcriptpb.CompactionState_COMPACTION_STATE_STARTED
	case runtime.EventCompactionCompleted:
		state = transcriptpb.CompactionState_COMPACTION_STATE_COMPLETED
	case runtime.EventCompactionFailed:
		state = transcriptpb.CompactionState_COMPACTION_STATE_FAILED
		diagnostic = &transcriptpb.Diagnostic{
			Code:   "compaction_failed",
			Detail: strings.TrimSpace(evt.Compaction.Error),
		}
	default:
		return nil, fmt.Errorf("runtime event %q carries compaction facts outside the compaction lifecycle", evt.Kind)
	}
	return transcriptCompactionProjection(evt.StepID, evt.Compaction.RequestID, evt.Compaction.Mode, evt.Compaction.Count, state, diagnostic)
}

func transcriptGoalStatus(update runtime.GoalStatusUpdate) (*runtimepb.GoalView, error) {
	if update.Cleared {
		return &runtimepb.GoalView{}, nil
	}
	return transcriptGoalStatusFromRuntime(&update.State, false)
}

type transcriptBackgroundActivityFacts struct {
	activityID        runtimeids.BackgroundActivityID
	processID         string
	ownerRunID        runtimeids.RunID
	ownerStepID       runtimeids.StepID
	lifecycle         transcriptpb.BackgroundLifecycle
	command           string
	workdir           string
	logPath           *string
	preview           *string
	exitCode          *int
	userRequestedKill bool
	noticeSuppressed  bool
}

func transcriptBackgroundActivityFromFacts(facts transcriptBackgroundActivityFacts) (*transcriptpb.BackgroundActivity, error) {
	var exitCode *int32
	if facts.exitCode != nil {
		value, err := protoapi.Int32(*facts.exitCode, "background exit code")
		if err != nil {
			return nil, err
		}
		exitCode = &value
	}
	projected := &transcriptpb.BackgroundActivity{
		ActivityId:        facts.activityID.String(),
		ProcessId:         facts.processID,
		OwnerRunId:        facts.ownerRunID.String(),
		OwnerStepId:       facts.ownerStepID.String(),
		Lifecycle:         facts.lifecycle,
		Command:           facts.command,
		Workdir:           facts.workdir,
		LogPath:           facts.logPath,
		Preview:           facts.preview,
		ExitCode:          exitCode,
		UserRequestedKill: facts.userRequestedKill,
		NoticeSuppressed:  facts.noticeSuppressed,
	}
	return projected, protoapi.Validate(projected)
}

func transcriptBackgroundActivity(evt runtime.BackgroundShellEvent) (*transcriptpb.BackgroundActivity, error) {
	var lifecycle transcriptpb.BackgroundLifecycle
	switch evt.Type {
	case runtime.BackgroundShellEventBackgrounded:
		lifecycle = transcriptpb.BackgroundLifecycle_BACKGROUND_LIFECYCLE_BACKGROUNDED
	case runtime.BackgroundShellEventCompleted:
		lifecycle = transcriptpb.BackgroundLifecycle_BACKGROUND_LIFECYCLE_COMPLETED
	case runtime.BackgroundShellEventKilled:
		lifecycle = transcriptpb.BackgroundLifecycle_BACKGROUND_LIFECYCLE_KILLED
	default:
		return nil, fmt.Errorf("runtime background activity has unknown lifecycle %q: process_id=%q activity_id=%q", evt.Type, evt.ID, evt.ActivityID)
	}
	activityID, err := runtimeids.ParseBackgroundActivityID(evt.ActivityID.String())
	if err != nil {
		return nil, err
	}
	runID, err := runtimeids.ParseRunID(strings.TrimSpace(evt.OwnerRunID))
	if err != nil {
		return nil, err
	}
	stepID, err := runtimeids.ParseStepID(strings.TrimSpace(evt.OwnerStepID))
	if err != nil {
		return nil, err
	}
	return transcriptBackgroundActivityFromFacts(transcriptBackgroundActivityFacts{
		activityID:        activityID,
		processID:         strings.TrimSpace(evt.ID),
		ownerRunID:        runID,
		ownerStepID:       stepID,
		lifecycle:         lifecycle,
		command:           evt.Command,
		workdir:           evt.Workdir,
		logPath:           textutil.OptionalTrimmedString(evt.LogPath),
		preview:           textutil.OptionalTrimmedString(evt.Preview),
		exitCode:          textutil.Pointer(evt.ExitCode),
		userRequestedKill: evt.UserRequestedKill,
		noticeSuppressed:  evt.NoticeSuppressed,
	})
}

func TranscriptBackgroundActivitiesFromProcessSnapshots(
	sessionID string,
	snapshots []shelltool.Snapshot,
) ([]*transcriptpb.BackgroundActivity, error) {
	sessionID = strings.TrimSpace(sessionID)
	out := make([]*transcriptpb.BackgroundActivity, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !snapshot.Running || !snapshot.Backgrounded || strings.TrimSpace(snapshot.OwnerSessionID) != sessionID {
			continue
		}
		activityID, err := runtimeids.ParseBackgroundActivityID(snapshot.ActivityID.String())
		if err != nil {
			return nil, fmt.Errorf("background process %q activity id: %w", snapshot.ID, err)
		}
		runID, err := runtimeids.ParseRunID(snapshot.OwnerRunID)
		if err != nil {
			return nil, fmt.Errorf("background process %q owner run id: %w", snapshot.ID, err)
		}
		stepID, err := runtimeids.ParseStepID(snapshot.OwnerStepID)
		if err != nil {
			return nil, fmt.Errorf("background process %q owner step id: %w", snapshot.ID, err)
		}
		projected, err := transcriptBackgroundActivityFromFacts(transcriptBackgroundActivityFacts{
			activityID:  activityID,
			processID:   strings.TrimSpace(snapshot.ID),
			ownerRunID:  runID,
			ownerStepID: stepID,
			lifecycle:   transcriptpb.BackgroundLifecycle_BACKGROUND_LIFECYCLE_BACKGROUNDED,
			command:     snapshot.Command,
			workdir:     snapshot.Workdir,
			logPath:     textutil.OptionalTrimmedString(snapshot.LogPath),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, projected)
	}
	return out, nil
}

func TranscriptSessionIdentityFromRuntime(
	engine *runtime.Engine,
) (*transcriptpb.SessionIdentity, error) {
	if engine == nil {
		return &transcriptpb.SessionIdentity{}, nil
	}
	freshness, err := engine.ConversationFreshness()
	if err != nil {
		return nil, err
	}
	sessionID, err := runtimeids.ParseSessionID(strings.TrimSpace(engine.SessionID()))
	if err != nil {
		return nil, err
	}
	return &transcriptpb.SessionIdentity{
		SessionId:             sessionID.String(),
		SessionName:           textutil.OptionalTrimmedString(engine.SessionName()),
		ConversationFreshness: ConversationFreshnessFromSession(freshness),
	}, nil
}

func transcriptCommittedRowMessages(evt runtime.Event) ([]*transcriptpb.Event, error) {
	rowFacts := runtime.TranscriptCommittedRowFactsFromEvent(evt)
	if len(rowFacts) == 0 {
		return nil, nil
	}
	out := make([]*transcriptpb.Event, 0, len(rowFacts))
	for _, fact := range rowFacts {
		if err := fact.Locator.Validate(); err != nil {
			return nil, fmt.Errorf("runtime committed row lacks valid provenance: event_kind=%q fact=%+v error=%v", evt.Kind, fact, err)
		}
		row, err := transcriptRowFromFact(fact)
		if err != nil {
			return nil, err
		}
		out = append(out, &transcriptpb.Event{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: row}})
	}
	return out, nil
}

func transcriptRowsFromFactsChecked(facts []runtime.TranscriptCommittedRowFact) ([]*transcriptpb.CommittedRow, error) {
	if len(facts) == 0 {
		return []*transcriptpb.CommittedRow{}, nil
	}
	rows := make([]*transcriptpb.CommittedRow, 0, len(facts))
	for index, fact := range facts {
		if err := fact.Locator.Validate(); err != nil {
			return nil, fmt.Errorf(
				"runtime hydrated committed row lacks valid provenance: fact_index=%d kind=%q provenance=%+v notice=%+v error=%v",
				index,
				fact.Kind,
				fact.Provenance,
				transcriptNoticeFactDiagnostic(fact.Notice),
				err,
			)
		}
		row, err := transcriptRowFromFact(fact)
		if err != nil {
			return nil, err
		}
		if row.GetTool() != nil {
			if err := protoapi.Validate(row.GetTool()); err != nil {
				return nil, fmt.Errorf(
					"runtime hydrated committed tool row %d violates transcript contract: %w",
					index,
					err,
				)
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func transcriptNoticeFactDiagnostic(notice *runtime.TranscriptNoticeRowFact) any {
	if notice == nil {
		return nil
	}
	return *notice
}

func transcriptAssistantStream(snapshot runtime.TranscriptHydrationSnapshot) (*transcriptpb.AssistantStream, error) {
	text := snapshot.ActiveAssistantText
	if text == "" && snapshot.ActiveAssistantMetadata == nil && snapshot.ActiveAssistantStreamID == nil {
		return nil, nil
	}
	if text == "" || snapshot.ActiveAssistantMetadata == nil || snapshot.ActiveAssistantStreamID == nil {
		return nil, fmt.Errorf(
			"runtime transcript hydration has partial assistant stream identity: text_present=%t metadata_present=%t stream_id_present=%t",
			text != "",
			snapshot.ActiveAssistantMetadata != nil,
			snapshot.ActiveAssistantStreamID != nil,
		)
	}
	stepID, streamID, phase, err := transcriptAssistantIdentity(&snapshot.ActiveAssistantMetadata.StepID, snapshot.ActiveAssistantStreamID, snapshot.ActiveAssistantPhase)
	if err != nil {
		return nil, err
	}
	projected := &transcriptpb.AssistantStream{
		StepId:   stepID,
		StreamId: streamID,
		Text:     text,
		Phase:    phase,
	}
	return projected, protoapi.Validate(projected)
}

func transcriptToolStartMessages(starts []runtime.TranscriptLiveToolStart) ([]*transcriptpb.Event, error) {
	projected, err := transcriptToolStartsFromRuntime(starts)
	if err != nil {
		return nil, err
	}
	if len(projected) == 0 {
		return nil, nil
	}
	out := make([]*transcriptpb.Event, 0, len(projected))
	for index := range projected {
		start := projected[index]
		out = append(out, &transcriptpb.Event{Payload: &transcriptpb.Event_ToolStart{ToolStart: start}})
	}
	return out, nil
}

func transcriptToolAbortMessages(evt runtime.Event) ([]*transcriptpb.Event, error) {
	if evt.ToolCall == nil {
		return nil, fmt.Errorf("runtime tool abort is missing its tool call identity")
	}
	var reason transcriptpb.ToolAbortReason
	switch strings.TrimSpace(evt.ToolAbortReason) {
	case "", "interrupted", "canceled":
		reason = transcriptpb.ToolAbortReason_TOOL_ABORT_REASON_CANCELED
	case "failed":
		reason = transcriptpb.ToolAbortReason_TOOL_ABORT_REASON_FAILED
	default:
		return nil, fmt.Errorf("unknown tool abort reason %q", evt.ToolAbortReason)
	}
	stepID, err := transcriptRequiredStepID(evt.StepID)
	if err != nil {
		return nil, err
	}
	abort := &transcriptpb.ToolAbort{
		StepId:     stepID,
		ToolCallId: strings.TrimSpace(evt.ToolCall.ID),
		Reason:     reason,
	}
	if reason == transcriptpb.ToolAbortReason_TOOL_ABORT_REASON_FAILED {
		abort.Diagnostic = &transcriptpb.Diagnostic{
			Code:   "tool_failed",
			Detail: strings.TrimSpace(evt.Error),
		}
	}
	return []*transcriptpb.Event{{Payload: &transcriptpb.Event_ToolAbort{ToolAbort: abort}}}, nil
}

func transcriptQueuedMessageStateMessages(evt runtime.Event) ([]*transcriptpb.Event, error) {
	if evt.QueuedUserMessageStatus == nil {
		return nil, nil
	}
	status := evt.QueuedUserMessageStatus
	state := &transcriptpb.QueuedMessageState{
		QueueItemId: strings.TrimSpace(status.QueueItemID),
	}
	switch status.Status {
	case runtime.QueuedUserMessageAccepted:
		state.Status = transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_ACCEPTED
		state.Text = textutil.OptionalTrimmedString(status.Text)
	case runtime.QueuedUserMessageFailed:
		state.Status = transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_FAILED
		reason, err := transcriptQueuedFailureReason(status.FailureReason)
		if err != nil {
			return nil, err
		}
		state.FailureReason = &reason
		state.Text = textutil.OptionalTrimmedString(status.Text)
	case runtime.QueuedUserMessageSubmitted:
		state.Status = transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_SUBMITTED
	case runtime.QueuedUserMessageDiscarded:
		state.Status = transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_DISCARDED
	default:
		return nil, fmt.Errorf("runtime queued-message event has unknown status %q", status.Status)
	}
	return []*transcriptpb.Event{{Payload: &transcriptpb.Event_QueuedMessageState{QueuedMessageState: state}}}, nil
}

func transcriptHumanInputInterruptedMessages(evt runtime.Event) ([]*transcriptpb.Event, error) {
	if evt.HumanInputInterrupted == nil || len(evt.HumanInputInterrupted.Items) == 0 {
		return nil, nil
	}
	items := make([]*transcriptpb.InterruptedHumanInputItem, 0, len(evt.HumanInputInterrupted.Items))
	for _, item := range evt.HumanInputInterrupted.Items {
		items = append(items, &transcriptpb.InterruptedHumanInputItem{
			QueueItemId: strings.TrimSpace(item.QueueItemID),
			Text:        item.Text,
		})
	}
	return []*transcriptpb.Event{{Payload: &transcriptpb.Event_HumanInputInterrupted{HumanInputInterrupted: &transcriptpb.HumanInputInterrupted{Items: items}}}}, nil
}

func transcriptUserMessageFlushedMessages(evt runtime.Event) ([]*transcriptpb.Event, error) {
	if len(evt.UserMessageBatchQueuedItems) == 0 {
		return nil, nil
	}
	for index, item := range evt.UserMessageBatchQueuedItems {
		if _, err := runtimeids.ParseQueueItemID(strings.TrimSpace(item.QueueItemID)); err != nil {
			return nil, fmt.Errorf("flushed queued message %d: %w", index, err)
		}
	}
	flushed := &transcriptpb.UserMessageFlushed{
		StepId: textutil.Pointer(evt.StepID),
	}
	return []*transcriptpb.Event{{Payload: &transcriptpb.Event_UserMessageFlushed{UserMessageFlushed: flushed}}}, nil
}

func transcriptStepStateMessages(evt runtime.Event) ([]*transcriptpb.Event, error) {
	if evt.RunState == nil || evt.RunState.Lifecycle.Phase == runtime.RunLifecycleIdle {
		return nil, nil
	}
	stepID, err := transcriptRequiredStepID(evt.StepID)
	if err != nil {
		return nil, err
	}
	activeKind, err := runtimeactivity.ClientActiveKindFromRuntime(evt.RunState.ActiveKind)
	if err != nil {
		return nil, err
	}
	state := &transcriptpb.StepState{
		RunId:      strings.TrimSpace(evt.RunState.RunID),
		StepId:     stepID,
		ActiveKind: activeKind,
	}
	switch evt.RunState.Status {
	case runtime.RunStatusRunning:
		state.Status = transcriptpb.RunStatus_RUN_STATUS_RUNNING
	case runtime.RunStatusCompleted:
		state.Status = transcriptpb.RunStatus_RUN_STATUS_COMPLETED
	case runtime.RunStatusInterrupted:
		state.Status = transcriptpb.RunStatus_RUN_STATUS_INTERRUPTED
	case runtime.RunStatusFailed:
		state.Status = transcriptpb.RunStatus_RUN_STATUS_FAILED
	default:
		return nil, fmt.Errorf("unknown run status %q", evt.RunState.Status)
	}
	switch evt.RunState.Lifecycle.Phase {
	case runtime.RunLifecycleRunning:
		state.Lifecycle = transcriptpb.StepLifecycle_STEP_LIFECYCLE_STARTED
	case runtime.RunLifecycleFinished:
		state.Lifecycle = transcriptpb.StepLifecycle_STEP_LIFECYCLE_FINISHED
	default:
		return nil, fmt.Errorf("runtime run state has unknown lifecycle phase %q", evt.RunState.Lifecycle.Phase)
	}
	return []*transcriptpb.Event{{Payload: &transcriptpb.Event_StepState{StepState: state}}}, nil
}

func transcriptOperationalDiagnosticMessages(evt runtime.Event) ([]*transcriptpb.Event, error) {
	diagnostic := &transcriptpb.OperationalDiagnostic{Detail: strings.TrimSpace(evt.Error), StepId: textutil.Pointer(evt.StepID)}
	switch evt.Kind {
	case runtime.EventSleepGuardFailed:
		diagnostic.Code = transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_SLEEP_GUARD_FAILED
	case runtime.EventPromptHistoryPersistFailed:
		diagnostic.Code = transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_PROMPT_HISTORY_PERSIST_FAILED
	case runtime.EventContextFactsPersistFailed:
		diagnostic.Code = transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_CONTEXT_FACTS_PERSIST_FAILED
	case runtime.EventInFlightClearFailed:
		diagnostic.Code = transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_IN_FLIGHT_CLEAR_FAILED
	default:
		return nil, fmt.Errorf("runtime event %q is not an operational diagnostic", evt.Kind)
	}
	return []*transcriptpb.Event{{Payload: &transcriptpb.Event_OperationalDiagnostic{OperationalDiagnostic: diagnostic}}}, nil
}

func transcriptRowFromFact(fact runtime.TranscriptCommittedRowFact) (*transcriptpb.CommittedRow, error) {
	visibility, err := transcriptVisibility(fact.Visibility)
	if err != nil {
		return nil, err
	}
	integrity, err := transcriptIntegrity(fact.Integrity)
	if err != nil {
		return nil, err
	}
	ordinal, err := protoapi.Int32(int(fact.Locator.RowOrdinal), "committed row ordinal")
	if err != nil {
		return nil, err
	}
	row := &transcriptpb.CommittedRow{
		Visibility: visibility,
		Integrity:  integrity,
		Locator:    &transcriptpb.CommittedRowLocator{EventSequence: fact.Locator.EventSequence, RowOrdinal: ordinal},
	}
	switch fact.Kind {
	case runtime.TranscriptCommittedRowFactUser:
		if fact.User == nil {
			return nil, fmt.Errorf("runtime transcript user row fact is missing its user payload")
		}
		committedAt, err := protoapi.CommittedTimeToProto(fact.User.CommittedAtUnixMs)
		if err != nil {
			return nil, err
		}
		row.Row = &transcriptpb.CommittedRow_User{User: &transcriptpb.UserRow{
			StepId:           textutil.Pointer(fact.StepID),
			Text:             fact.User.Text,
			CondensedText:    optionalNonBlankString(fact.User.CondensedText),
			RollbackTargetId: textutil.Pointer(fact.User.RollbackTargetID),
			CommittedAt:      committedAt,
		}}
	case runtime.TranscriptCommittedRowFactAssistant:
		if fact.Assistant == nil || fact.StepID == nil {
			return nil, fmt.Errorf("runtime transcript assistant row fact is missing its payload or Step")
		}
		committedAt, err := protoapi.CommittedTimeToProto(fact.Assistant.CommittedAtUnixMs)
		if err != nil {
			return nil, err
		}
		phase, err := transcriptAssistantPhase(transcript.AssistantPhase(fact.Assistant.Phase))
		if err != nil {
			return nil, err
		}
		assistant := &transcriptpb.AssistantRow{
			StepId:        *fact.StepID,
			Text:          fact.Assistant.Text,
			CondensedText: optionalNonBlankString(fact.Assistant.CondensedText),
			Phase:         phase,
			CommittedAt:   committedAt,
		}
		if fact.Assistant.StreamID != nil {
			streamID := fact.Assistant.StreamID.String()
			assistant.StreamId = &streamID
		}
		row.Row = &transcriptpb.CommittedRow_Assistant{Assistant: assistant}
	case runtime.TranscriptCommittedRowFactTool:
		if fact.Tool == nil {
			return nil, fmt.Errorf("runtime transcript tool row fact is missing its tool payload")
		}
		presentation, err := transcriptToolPresentation(fact.Tool.ToolName, fact.Tool.Presentation)
		if err != nil {
			return nil, err
		}
		answer, err := transcriptQuestionAnswerFromRuntime(fact.Tool.QuestionAnswer)
		if err != nil {
			return nil, err
		}
		webSearch, err := webSearchDetailToProto(fact.Tool.WebSearch)
		if err != nil {
			return nil, err
		}
		row.Row = &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			StepId:         textutil.Pointer(fact.StepID),
			ToolCallId:     textutil.OptionalTrimmedString(fact.Tool.ToolCallID),
			ToolName:       textutil.OptionalTrimmedString(fact.Tool.ToolName),
			Text:           fact.Tool.Text,
			IsError:        fact.Tool.IsError,
			ResultSummary:  optionalNonBlankString(fact.Tool.ResultSummary),
			CondensedText:  optionalNonBlankString(fact.Tool.CondensedText),
			Presentation:   presentation,
			QuestionAnswer: answer,
			WebSearch:      webSearch,
		}}
	case runtime.TranscriptCommittedRowFactReasoningTrace:
		if fact.ReasoningTrace == nil || fact.StepID == nil {
			return nil, fmt.Errorf("runtime transcript reasoning row fact is missing its payload or Step")
		}
		identity, err := transcriptReasoningTraceIdentityFromRuntime(fact.ReasoningTrace.ProvisionalIdentity)
		if err != nil {
			return nil, err
		}
		trace := &transcriptpb.ReasoningTraceRow{
			StepId:              *fact.StepID,
			CompactText:         fact.ReasoningTrace.CompactText,
			Text:                fact.ReasoningTrace.Text,
			ProvisionalIdentity: identity,
		}
		if fact.ReasoningTrace.DurationMs != nil {
			trace.Duration, err = protoapi.MillisecondsToProto(*fact.ReasoningTrace.DurationMs)
			if err != nil {
				return nil, err
			}
		}
		row.Row = &transcriptpb.CommittedRow_ReasoningTrace{ReasoningTrace: trace}
	case runtime.TranscriptCommittedRowFactNotice:
		notice, err := transcriptNoticeFromFact(fact.StepID, fact.Notice)
		if err != nil {
			return nil, err
		}
		row.Row = &transcriptpb.CommittedRow_Notice{Notice: notice}
	case runtime.TranscriptCommittedRowFactReviewerFeedback:
		if fact.ReviewerFeedback == nil || fact.StepID == nil {
			return nil, fmt.Errorf("runtime transcript Reviewer feedback row fact is missing its payload or Step")
		}
		count, err := protoapi.Int32(fact.ReviewerFeedback.SuggestionCount, "reviewer suggestion count")
		if err != nil {
			return nil, err
		}
		row.Row = &transcriptpb.CommittedRow_ReviewerFeedback{ReviewerFeedback: &transcriptpb.ReviewerFeedbackRow{
			Id:              fact.ReviewerFeedback.ID.String(),
			StepId:          *fact.StepID,
			Suggestions:     append([]string(nil), fact.ReviewerFeedback.Suggestions...),
			SuggestionCount: count,
		}}
	case runtime.TranscriptCommittedRowFactReviewerError:
		if fact.ReviewerError == nil || fact.StepID == nil {
			return nil, fmt.Errorf("runtime transcript Reviewer error row fact is missing its payload or Step")
		}
		row.Row = &transcriptpb.CommittedRow_ReviewerError{ReviewerError: &transcriptpb.ReviewerErrorRow{
			Id:     fact.ReviewerError.ID.String(),
			StepId: *fact.StepID,
			Detail: fact.ReviewerError.Detail,
		}}
	default:
		return nil, fmt.Errorf("runtime transcript row fact has unknown kind %q", fact.Kind)
	}
	return row, nil
}

func transcriptQuestionAnswerFromRuntime(answer *tools.AskQuestionAnswer) (*promptpb.QuestionAnswer, error) {
	if answer == nil {
		return nil, nil
	}
	result := &promptpb.QuestionAnswer{
		Freeform: textutil.Pointer(answer.Freeform),
	}
	if answer.SelectedOptionNumber != nil {
		number, err := protoapi.Int32(*answer.SelectedOptionNumber, "selected option number")
		if err != nil {
			return nil, err
		}
		result.SelectedOptionNumber = &number
	}
	return result, nil
}

func transcriptReasoningTraceIdentityFromRuntime(identity *runtime.TranscriptReasoningTraceIdentity) (*transcriptpb.ReasoningTraceIdentity, error) {
	if identity == nil {
		return nil, nil
	}
	return transcriptReasoningTraceIdentityProjection(identity, "runtime reasoning trace identity")
}

func transcriptReasoningTraceIdentityProjection(identity *runtime.TranscriptReasoningTraceIdentity, context string) (*transcriptpb.ReasoningTraceIdentity, error) {
	if identity == nil || (identity.Provider == nil) == (identity.Kent == nil) {
		return nil, fmt.Errorf("%s requires exactly one public identity", context)
	}
	projected := &transcriptpb.ReasoningTraceIdentity{}
	switch {
	case identity.Provider != nil:
		projected.Identity = &transcriptpb.ReasoningTraceIdentity_Provider{Provider: &transcriptpb.ProviderReasoningTraceIdentity{
			ItemId:       identity.Provider.ItemID,
			SummaryIndex: textutil.Pointer(identity.Provider.PartIndex),
		}}
	case identity.Kent != nil:
		projected.Identity = &transcriptpb.ReasoningTraceIdentity_KentTraceId{KentTraceId: identity.Kent.String()}
	}
	return projected, protoapi.Validate(projected)
}

func transcriptNoticeFromFact(stepID *string, fact *runtime.TranscriptNoticeRowFact) (*transcriptpb.NoticeRow, error) {
	if fact == nil {
		return nil, fmt.Errorf("runtime transcript notice row fact is missing its notice payload")
	}
	reasons := map[string]transcriptpb.NoticeReason{
		transcript.NoticeReasonCacheWarning:          transcriptpb.NoticeReason_NOTICE_REASON_CACHE_WARNING,
		transcript.NoticeReasonCompaction:            transcriptpb.NoticeReason_NOTICE_REASON_COMPACTION,
		transcript.NoticeReasonLegacyUntypedNotice:   transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE,
		transcript.NoticeReasonRuntimeDiagnostic:     transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
		transcript.NoticeReasonToolOutputRepair:      transcriptpb.NoticeReason_NOTICE_REASON_TOOL_OUTPUT_REPAIR,
		transcript.NoticeReasonProviderModelMismatch: transcriptpb.NoticeReason_NOTICE_REASON_PROVIDER_MODEL_MISMATCH,
		transcript.NoticeReasonThinkingUpdate:        transcriptpb.NoticeReason_NOTICE_REASON_THINKING_UPDATE,
	}
	reason, ok := reasons[strings.TrimSpace(fact.Reason)]
	if !ok {
		return nil, fmt.Errorf("invalid notice reason %q", fact.Reason)
	}
	severities := map[string]transcriptpb.NoticeSeverity{
		transcript.NoticeSeverityInfo:    transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
		transcript.NoticeSeverityWarning: transcriptpb.NoticeSeverity_NOTICE_SEVERITY_WARNING,
		transcript.NoticeSeverityError:   transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
	}
	severity, ok := severities[strings.TrimSpace(fact.Severity)]
	if !ok {
		return nil, fmt.Errorf("invalid notice severity %q", fact.Severity)
	}
	worktree, err := transcriptWorktreeContext(fact.MessageType, fact.WorktreeContext)
	if err != nil {
		return nil, err
	}
	notice := &transcriptpb.NoticeRow{
		Reason:         reason,
		Severity:       severity,
		StepId:         textutil.Pointer(stepID),
		LegacyText:     optionalStringPointer(fact.LegacyText),
		SourcePath:     textutil.OptionalTrimmedString(fact.SourcePath),
		Worktree:       worktree,
		CondensedText:  optionalNonBlankString(fact.CondensedText),
		CompactLabel:   optionalNonBlankString(fact.CompactLabel),
		ThinkingEffort: textutil.Pointer(fact.ThinkingEffort),
	}
	if messageType := strings.TrimSpace(string(fact.MessageType)); messageType != "" {
		typed, err := transcriptNoticeMessageType(llm.MessageType(messageType))
		if err != nil {
			return nil, err
		}
		notice.MessageType = &typed
	}
	if fact.NoticeID != nil {
		value := strings.TrimSpace(*fact.NoticeID)
		notice.NoticeId = &value
	}
	if fact.CacheWarning != nil {
		visibility, err := transcriptVisibility(fact.CacheWarning.Visibility)
		if err != nil {
			return nil, err
		}
		notice.CacheWarning = &transcriptpb.CacheWarning{
			Scope:      strings.TrimSpace(fact.CacheWarning.Scope),
			Reason:     strings.TrimSpace(fact.CacheWarning.Reason),
			Visibility: visibility,
		}
		if fact.CacheWarning.LostInputTokens != nil {
			count, err := protoapi.Int32(*fact.CacheWarning.LostInputTokens, "lost input tokens")
			if err != nil {
				return nil, err
			}
			notice.CacheWarning.LostInputTokens = &count
		}
	}
	if fact.Compaction != nil {
		notice.Compaction = &transcriptpb.CompactionNotice{
			Detail: optionalStringPointer(fact.Compaction.Detail),
		}
		if fact.Compaction.Count != nil {
			count, err := protoapi.Int32(*fact.Compaction.Count, "compaction count")
			if err != nil {
				return nil, err
			}
			notice.Compaction.Count = &count
		}
	}
	if fact.ToolOutputRepair != nil {
		count, err := protoapi.Int32(fact.ToolOutputRepair.Count, "repaired output count")
		if err != nil {
			return nil, err
		}
		notice.ToolOutputRepair = &transcriptpb.ToolOutputRepair{Kind: string(fact.ToolOutputRepair.Kind), Count: count}
	}
	if fact.ProviderModelMismatch != nil {
		notice.ProviderModelMismatch = &transcriptpb.ProviderModelMismatch{
			RequestedModel: fact.ProviderModelMismatch.RequestedModel, ServedModel: fact.ProviderModelMismatch.ServedModel,
		}
	}
	diagnosticCode := strings.TrimSpace(fact.DiagnosticCode)
	diagnosticDetail := fact.DiagnosticDetail
	if diagnosticCode != "" || strings.TrimSpace(diagnosticDetail) != "" {
		if diagnosticCode == "" || strings.TrimSpace(diagnosticDetail) == "" {
			return nil, fmt.Errorf(
				"runtime transcript notice has partial diagnostic facts: code=%q detail_present=%t reason=%q",
				diagnosticCode,
				diagnosticDetail != "",
				fact.Reason,
			)
		}
		notice.Diagnostic = &transcriptpb.Diagnostic{
			Code:   diagnosticCode,
			Detail: diagnosticDetail,
		}
	}
	activityID := strings.TrimSpace(fact.BackgroundActivityID)
	processID := strings.TrimSpace(fact.BackgroundProcessID)
	if activityID != "" || processID != "" {
		if activityID == "" || processID == "" {
			return nil, fmt.Errorf(
				"runtime transcript background notice has partial identity: activity_id=%q process_id=%q",
				activityID,
				processID,
			)
		}
		notice.Background = &transcriptpb.BackgroundNoticeIdentity{
			ActivityId: activityID,
			ProcessId:  processID,
		}
		if fact.BackgroundExitCode != nil {
			exitCode, err := protoapi.Int32(*fact.BackgroundExitCode, "background exit code")
			if err != nil {
				return nil, err
			}
			notice.Background.ExitCode = &exitCode
		}
	}
	return notice, nil
}

func transcriptWorktreeContext(messageType llm.MessageType, context *session.WorktreeContext) (*transcriptpb.WorktreeContext, error) {
	if context == nil {
		return nil, nil
	}
	var mode session.WorktreeReminderMode
	switch messageType {
	case llm.MessageTypeWorktreeMode:
		mode = session.WorktreeReminderModeEnter
	case llm.MessageTypeWorktreeModeExit:
		mode = session.WorktreeReminderModeExit
	default:
		return nil, fmt.Errorf("worktree transcript context has non-worktree message type %q", messageType)
	}
	state, err := session.NormalizeWorktreeReminderState(session.WorktreeReminderState{
		Mode:            mode,
		WorktreeContext: *session.CloneWorktreeContext(context),
	})
	if err != nil {
		return nil, fmt.Errorf("project worktree transcript context: message_type=%q context=%+v: %w", messageType, context, err)
	}
	return &transcriptpb.WorktreeContext{
		Branch:        textutil.Pointer(state.Branch),
		WorktreePath:  strings.TrimSpace(state.WorktreePath),
		WorkspaceRoot: strings.TrimSpace(state.WorkspaceRoot),
		EffectiveCwd:  strings.TrimSpace(state.EffectiveCwd),
	}, nil
}

func transcriptAssistantAbortReason(reason string) (transcriptpb.AssistantAbortReason, error) {
	switch strings.TrimSpace(reason) {
	case string(runtime.AssistantStreamAbortSuperseded):
		return transcriptpb.AssistantAbortReason_ASSISTANT_ABORT_REASON_SUPERSEDED, nil
	case "interrupted", "canceled":
		return transcriptpb.AssistantAbortReason_ASSISTANT_ABORT_REASON_INTERRUPTED, nil
	case "failed":
		return transcriptpb.AssistantAbortReason_ASSISTANT_ABORT_REASON_FAILED, nil
	default:
		return transcriptpb.AssistantAbortReason_ASSISTANT_ABORT_REASON_UNSPECIFIED, fmt.Errorf("runtime assistant stream abort has unknown reason %q", reason)
	}
}

func optionalNonBlankString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func optionalStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return optionalNonBlankString(*value)
}
