package app

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"core/cli/tui/ongoing"
	"core/shared/protoapi"
	processpb "core/shared/protoapi/gen/kent/api/process"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"

	"google.golang.org/protobuf/proto"
)

func TestStaleContentCompleteHydrationFailsBeforeAnySideEffect(t *testing.T) {
	runtimeClient := newTestSessionRuntimeClient(
		&countingSessionViewClient{},
		newUnavailableRuntimeControlService(),
	)
	current := runtimeTupleTestView(11, runtimeTupleTestIdleActivity())
	current.Status.ThinkingLevel = "current"
	current.Session.SessionName = proto.String("current")
	runtimeClient.storeMainView(current)
	m := newProjectedTestUIModel(runtimeClient)
	m.pendingWorkRefresh = pendingWorkRefreshOwner{
		sessionID:       ongoingTestSessionID(),
		generation:      7,
		collection:      runtimeinput.PendingWork{Items: []runtimeinput.PendingWorkItem{pendingWorkMessageForTest(runtimeinput.PendingWorkLaneSteer, "existing Pending Work")}},
		successfulFetch: true,
	}
	m.queued = []queuedInputItem{{ID: "existing", Text: "existing"}}
	m.reasoningStatusHeader = "existing reasoning"
	surface := &ongoingSurfaceSpy{}
	controller := newOngoingTranscriptController(
		surface,
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	beforeView := runtimeClient.MainView()
	beforeActivity := m.runtimeActivityProjection
	beforeQueue := append([]queuedInputItem(nil), m.queued...)
	beforeReasoning := m.reasoningStatusHeader
	beforeLifecycle := m.runtimeLifecycle
	beforeAsk := m.ask
	beforePendingWorkRefresh := m.pendingWorkRefresh

	hydration := runtimeTupleTestRichHydration(10)
	_, cmd, err := controller.Accept(hydration)
	assertRuntimeTupleHydrationError(t, err)
	if cmd != nil {
		t.Fatal("stale hydration returned a state command")
	}
	assertUnchanged(t, "cached main view", runtimeClient.MainView(), beforeView)
	assertUnchanged(t, "UI activity", m.runtimeActivityProjection, beforeActivity)
	assertUnchanged(t, "UI queue", m.queued, beforeQueue)
	assertUnchanged(t, "UI reasoning", m.reasoningStatusHeader, beforeReasoning)
	assertUnchanged(t, "UI lifecycle", m.runtimeLifecycle, beforeLifecycle)
	assertUnchanged(t, "ask controller", m.ask, beforeAsk)
	assertUnchanged(t, "Pending Work refresh", m.pendingWorkRefresh, beforePendingWorkRefresh)
	if controller.hydrated || controller.lastSequence != 0 || len(controller.liveReadModel.sections) != 0 {
		t.Fatalf(
			"stale hydration changed controller state: hydrated=%t sequence=%d sections=%+v",
			controller.hydrated,
			controller.lastSequence,
			controller.liveReadModel.sections,
		)
	}
	if len(surface.calls) != 0 {
		t.Fatalf("stale hydration reached terminal surface: %+v", surface.calls)
	}
}

func TestStaleHydrationDeveloperErrorPanicsInEveryModeBeforeSideEffects(t *testing.T) {
	for _, debugMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "release", true: "debug"}[debugMode], func(t *testing.T) {
			runtimeClient := newTestSessionRuntimeClient(
				&countingSessionViewClient{},
				newUnavailableRuntimeControlService(),
			)
			current := runtimeTupleTestView(11, runtimeTupleTestIdleActivity())
			runtimeClient.storeMainView(current)
			m := sizedTestUIModel(newProjectedTestUIModel(runtimeClient, WithUIDebug(debugMode)), 93, 31)
			m.queued = []queuedInputItem{{ID: "queued-1", Text: "do not flush"}}
			surface := &ongoingSurfaceSpy{}
			m.ongoingTranscript = newOngoingTranscriptController(
				surface,
				m.ongoingFrameInput,
				runtimeClient.admitTranscriptMessageState,
				m.applyAdmittedTranscriptMessageState,
			)
			beforeView := runtimeClient.MainView()
			beforeQueue := append([]queuedInputItem(nil), m.queued...)
			hydration := runtimeTupleTestRichHydration(10)

			recovered := capturePanic(func() {
				_ = m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{
					Kind:    ongoingTranscriptEventMessage,
					Message: hydration,
				})
			})
			developerErr, ok := recovered.(ongoing.DeveloperError)
			if !ok {
				t.Fatalf("panic = %T, want ongoing.DeveloperError", recovered)
			}
			if developerErr.Operation != "admit_transcript_hydration_runtime_tuple" {
				t.Fatalf("developer-error operation = %q", developerErr.Operation)
			}
			if got := developerErr.Facts["terminal_size"]; got != (ongoing.Size{Width: 93, Height: 31}) {
				t.Fatalf("terminal size diagnostic = %+v, want 93x31", got)
			}
			wantPayload := strconv.Quote(fmt.Sprintf("%+v", hydration.Event.GetHydration()))
			if got, ok := developerErr.Facts["quoted_payload"].(string); !ok || got != wantPayload {
				t.Fatalf("quoted payload diagnostic = %#v, want %#v", developerErr.Facts["quoted_payload"], wantPayload)
			}
			if developerErr.Stack == "" {
				t.Fatal("developer error omitted stack trace")
			}
			assertUnchanged(t, "cached main view", runtimeClient.MainView(), beforeView)
			assertUnchanged(t, "queued input", m.queued, beforeQueue)
			if len(surface.calls) != 0 {
				t.Fatalf("stale hydration reached terminal surface: %+v", surface.calls)
			}
		})
	}
}

func capturePanic(action func()) (recovered any) {
	defer func() {
		recovered = recover()
	}()
	action()
	return nil
}

func TestNonStaleContentCompleteHydrationAppliesWholeEvent(t *testing.T) {
	runtimeClient := newTestSessionRuntimeClient(
		&countingSessionViewClient{},
		newUnavailableRuntimeControlService(),
	)
	current := runtimeTupleTestView(10, runtimeTupleTestIdleActivity())
	runtimeClient.storeMainView(current)
	m := newProjectedTestUIModel(runtimeClient)
	surface := &ongoingSurfaceSpy{}
	controller := newOngoingTranscriptController(
		surface,
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	hydration := runtimeTupleTestRichHydration(11)

	if _, _, err := controller.Accept(hydration); err != nil {
		t.Fatalf("accept current hydration: %v", err)
	}

	wantUpdate := hydration.Event.GetHydration().RuntimeReadModelUpdate
	assertRuntimeTupleView(t, runtimeClient.MainView(), runtimeTupleTestView(
		11,
		wantUpdate.Activity,
	))
	if !controller.hydrated || controller.lastSequence != 1 {
		t.Fatalf("delivery state = hydrated=%t sequence=%d, want true/1", controller.hydrated, controller.lastSequence)
	}
	if got := surface.appliedEvents(); len(got) != 1 || got[0].GetHydration() == nil {
		t.Fatalf("surface messages = %v, want one hydration", got)
	}
	if m.reasoningStatusHeader != "reasoning" {
		t.Fatalf("reasoning status = %q, want reasoning", m.reasoningStatusHeader)
	}
	if m.currentRunID == "" || m.currentStepID == "" || !m.runtimeActivityBusy() {
		t.Fatalf("hydrated running state missing: activity=%+v run=%q step=%q", m.runtimeActivityProjection, m.currentRunID, m.currentStepID)
	}
	if len(m.injectedQueue) != 0 {
		t.Fatalf("foreign hydrated queue created local restoration ownership: %+v", m.injectedQueue)
	}
	if len(controller.liveReadModel.sections) == 0 {
		t.Fatal("hydrated tools/prompts/queue did not reach controller live state")
	}
}

func TestHydrationReplacesStaleBackgroundProcessEntries(t *testing.T) {
	runtimeClient := newTestSessionRuntimeClient(
		&countingSessionViewClient{},
		newUnavailableRuntimeControlService(),
	)
	current := runtimeTupleTestView(10, runtimeTupleTestRunningActivity())
	runtimeClient.storeMainView(current)
	m := newProjectedTestUIModel(runtimeClient)
	m.processList.entries = []*processpb.BackgroundProcess{
		{Id: "completed-while-disconnected", OwnerSessionId: ongoingTestSessionID().String(), Running: true, Backgrounded: true},
		{Id: "other-session", OwnerSessionId: "other-session", Running: true, Backgrounded: true},
	}
	m.ongoingTranscript = newOngoingTranscriptController(
		&ongoingSurfaceSpy{},
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	hydration := runtimeTupleTestRichHydration(11)
	payload := hydration.Event.GetHydration()
	payload.BackgroundActivities = []*transcriptpb.BackgroundActivity{{
		ActivityId:  runtimeids.NewBackgroundActivityID().String(),
		ProcessId:   "still-running",
		OwnerRunId:  ongoingTestRunID().String(),
		OwnerStepId: ongoingTestStepID().String(),
		Lifecycle:   transcriptpb.BackgroundLifecycle_BACKGROUND_LIFECYCLE_BACKGROUNDED,
		Command:     "sleep 10",
		Workdir:     "/workspace",
	}}
	hydration = &transcriptpb.Message{Sequence: 1, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_Hydration{Hydration: payload}}}

	if _, _, err := m.ongoingTranscript.Accept(hydration); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	if len(m.processList.entries) != 2 || m.processList.entries[0].Id != "still-running" || m.processList.entries[1].Id != "other-session" {
		t.Fatalf("hydrated process entries = %+v, want fresh current-session process plus other-session cache", m.processList.entries)
	}
}

func TestHydrationReplacesStaleCompletedCompactionCountWithoutActiveCompaction(t *testing.T) {
	runtimeClient := newTestSessionRuntimeClient(
		&countingSessionViewClient{},
		newUnavailableRuntimeControlService(),
	)
	current := runtimeTupleTestView(10, runtimeTupleTestIdleActivity())
	current.Status.CompactionCount = 9
	runtimeClient.storeMainView(current)
	m := newProjectedTestUIModel(runtimeClient)
	surface := &ongoingSurfaceSpy{}
	controller := newOngoingTranscriptController(
		surface,
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	hydration := runtimeTupleTestRichHydration(11)
	payload := hydration.Event.GetHydration()
	payload.SessionStatus.CompactionCount = 4
	payload.ActiveCompaction = nil
	hydration = &transcriptpb.Message{Sequence: 1, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_Hydration{Hydration: payload}}}

	if _, _, err := controller.Accept(hydration); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	if got := runtimeClient.MainView().Status.CompactionCount; got != 4 {
		t.Fatalf("hydrated compaction count = %d, want 4", got)
	}
}

func TestAcceptedHydrationDoesNotAdvanceCacheWithUnaryRead(t *testing.T) {
	v12 := runtimeTupleTestView(12, runtimeTupleTestIdleActivity())
	reads := &countingSessionViewClient{view: v12}
	runtimeClient := newTestSessionRuntimeClient(reads, newUnavailableRuntimeControlService())
	runtimeClient.storeMainView(runtimeTupleTestView(
		10,
		runtimeTupleTestIdleActivity(),
	))
	m := newProjectedTestUIModel(runtimeClient)
	surface := &ongoingSurfaceSpy{}
	m.ongoingTranscript = newOngoingTranscriptController(
		surface,
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	hydration := runtimeTupleTestRichHydration(11)

	cmd := m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{
		Kind:    ongoingTranscriptEventMessage,
		Message: hydration,
	})

	if got := reads.mainViewCount.Load(); got != 0 {
		t.Fatalf("main-view reads before hydration consumption returned = %d, want 0", got)
	}
	if got := surface.appliedEvents(); len(got) != 1 || got[0].GetHydration() == nil {
		t.Fatalf("surface messages before refresh execution = %v", got)
	}
	_ = collectCmdMessages(t, cmd)
	if got := reads.mainViewCount.Load(); got != 0 {
		t.Fatalf("main-view reads after accepted hydration = %d, want 0", got)
	}
	if m.runtimeMainViewBusy || m.runtimeMainViewPendingSet {
		t.Fatalf(
			"accepted hydration scheduled unary refresh: busy=%t pending=%t",
			m.runtimeMainViewBusy,
			m.runtimeMainViewPendingSet,
		)
	}
	assertRuntimeTupleView(t, runtimeClient.MainView(), runtimeTupleTestView(
		11,
		hydration.Event.GetHydration().RuntimeReadModelUpdate.Activity,
	))
}

func TestRejectedDuplicateHydrationDoesNotStartUnaryRefresh(t *testing.T) {
	v12 := runtimeTupleTestView(12, runtimeTupleTestIdleActivity())
	reads := &countingSessionViewClient{view: v12}
	runtimeClient := newTestSessionRuntimeClient(reads, newUnavailableRuntimeControlService())
	runtimeClient.storeMainView(runtimeTupleTestView(
		10,
		runtimeTupleTestIdleActivity(),
	))
	m := newProjectedTestUIModel(runtimeClient)
	m.ongoingTranscript = newOngoingTranscriptController(
		&ongoingSurfaceSpy{},
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	hydration := runtimeTupleTestRichHydration(11)
	if _, _, err := m.ongoingTranscript.Accept(hydration); err != nil {
		t.Fatalf("accept initial hydration: %v", err)
	}

	cmd := m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{
		Kind:    ongoingTranscriptEventMessage,
		Message: hydration,
	})
	_ = collectCmdMessages(t, cmd)

	if got := reads.mainViewCount.Load(); got != 0 {
		t.Fatalf("rejected duplicate hydration main-view reads = %d, want 0", got)
	}
	if m.runtimeMainViewBusy || m.runtimeMainViewPendingSet {
		t.Fatalf(
			"rejected duplicate hydration scheduled refresh: busy=%t pending=%t",
			m.runtimeMainViewBusy,
			m.runtimeMainViewPendingSet,
		)
	}
}

func TestHydrationAdmissionSerializesUnaryAndInterruptTupleCommitsUntilWholeEventCompletes(t *testing.T) {
	v12 := runtimeTupleTestView(12, runtimeTupleTestIdleActivity())
	v12.Session.SessionId = ongoingTestSessionID().String()
	v12.Status.ThinkingLevel = "captured unary"
	reads := &countingSessionViewClient{view: v12}
	controls := &reconnectRetryRuntimeControlClient{interruptResp: &runtimepb.ReadModelUpdate{
		Version:  &runtimepb.ReadModelVersion{Epoch: "runtime-tuple-test", Generation: 1, Sequence: 13},
		Activity: runtimeTupleTestIdleActivity(),
	}}
	runtimeClient := newTestSessionRuntimeClient(reads, controls)
	v10 := runtimeTupleTestView(10, runtimeTupleTestIdleActivity())
	v10.Session.SessionId = ongoingTestSessionID().String()
	runtimeClient.storeMainView(v10)
	m := newProjectedTestUIModel(runtimeClient)
	surface := newBlockingOngoingSurface()
	controller := newOngoingTranscriptController(
		surface,
		m.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		m.applyAdmittedTranscriptMessageState,
	)
	hydration := runtimeTupleTestRichHydration(11)
	acceptDone := make(chan error, 1)
	go func() {
		_, _, err := controller.Accept(hydration)
		acceptDone <- err
	}()
	<-surface.started

	wantV11 := runtimeTupleTestView(
		11,
		hydration.Event.GetHydration().RuntimeReadModelUpdate.Activity,
	)
	assertRuntimeTupleView(t, runtimeClient.MainView(), wantV11)
	if !m.runtimeActivityBusy() || controller.lastSequence != 1 || !controller.hydrated {
		t.Fatalf(
			"hydration admission was not coherent before terminal completion: busy=%t hydrated=%t sequence=%d",
			m.runtimeActivityBusy(),
			controller.hydrated,
			controller.lastSequence,
		)
	}
	if surface.completedApplyCount() != 0 {
		t.Fatal("terminal hydration completed before release")
	}

	refresh := m.startRuntimeMainViewRefreshRequest(runtimeMainViewRefreshRequestForCause(runtimeMainViewRefreshCauseManual))
	refreshMessage, ok := refresh.cmd().(runtimeMainViewRefreshedMsg)
	if !ok {
		t.Fatalf("refresh command returned an unexpected message")
	}
	if !protoapi.ReadModelVersionsEqual(refreshMessage.view.Version, v12.Version) {
		t.Fatalf("refresh candidate version = %+v, want %+v", refreshMessage.view.Version, v12.Version)
	}
	assertRuntimeTupleView(t, runtimeClient.MainView(), wantV11)

	interruptCommand := m.runtimeControlCommand(runtimeControlInterrupt, "", false, "")
	interruptMessage, ok := interruptCommand().(runtimeControlDoneMsg)
	if !ok {
		t.Fatalf("interrupt command returned an unexpected message")
	}
	if interruptMessage.runtimeTuple == nil || interruptMessage.runtimeTuple.Version.Sequence != 13 {
		t.Fatalf("interrupt candidate = %+v, want V13", interruptMessage.runtimeTuple)
	}
	assertRuntimeTupleView(t, runtimeClient.MainView(), wantV11)
	if surface.completedApplyCount() != 0 {
		t.Fatal("worker completion partially replaced terminal hydration")
	}

	close(surface.release)
	if err := <-acceptDone; err != nil {
		t.Fatalf("finish hydration: %v", err)
	}
	if surface.completedApplyCount() != 1 {
		t.Fatalf("completed terminal hydration count = %d, want 1", surface.completedApplyCount())
	}

	next, _ := m.Update(refreshMessage)
	m = next.(*uiModel)
	assertRuntimeTupleView(t, runtimeClient.MainView(), v12)
	next, _ = m.Update(interruptMessage)
	m = next.(*uiModel)
	wantV13 := runtimeTupleTestView(13, controls.interruptResp.Activity)
	assertRuntimeTupleView(t, runtimeClient.MainView(), wantV13)
	if m.runtimeActivityBusy() || m.runtimeActivityBlocksInput() {
		t.Fatalf("reduced monotonic candidates left runtime busy: %+v", m.runtimeActivityProjection)
	}
}

type blockingOngoingSurface struct {
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	mu        sync.Mutex
	applied   int
}

func newBlockingOngoingSurface() *blockingOngoingSurface {
	return &blockingOngoingSurface{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *blockingOngoingSurface) ApplyTerminalMessage(*transcriptpb.Message, ongoing.FrameInput) (ongoing.Result, error) {
	s.startOnce.Do(func() { close(s.started) })
	<-s.release
	s.mu.Lock()
	s.applied++
	s.mu.Unlock()
	return ongoing.Result{}, nil
}

func (s *blockingOngoingSurface) Render(ongoing.FrameInput) (ongoing.Result, error) {
	return ongoing.Result{}, nil
}

func (s *blockingOngoingSurface) Resize(ongoing.Size, ongoing.FrameInput) (ongoing.Result, error) {
	return ongoing.Result{}, nil
}

func (s *blockingOngoingSurface) completedApplyCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applied
}

func runtimeTupleTestRichHydration(runtimeSequence uint64) *transcriptpb.Message {
	message := runtimeTupleTestHydration(
		runtimeSequence,
		runtimeTupleTestRunningActivity(),
	)
	hydration := message.Event.GetHydration()
	stepID := ongoingTestStepID()
	runID := ongoingTestRunID()
	hydration.RuntimeReadModelUpdate.Activity.ActiveStep = &runtimepb.ActiveStep{
		RunId:      runID.String(),
		StepId:     stepID.String(),
		ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN,
	}
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 2, RowOrdinal: 1},
		Row:        &transcriptpb.CommittedRow_User{User: &transcriptpb.UserRow{StepId: proto.String(stepID.String()), Text: "committed hydration row"}},
	}
	hydration.TailSegment.Entries = []*transcriptpb.CommittedRow{row}
	hydration.ActiveAssistant = &transcriptpb.AssistantStream{
		StepId:   stepID.String(),
		StreamId: runtimeids.NewAssistantStreamID().String(),
		Text:     "assistant hydration",
		Phase:    transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
	}
	hydration.ActiveThinkingStatus = &transcriptpb.ThinkingStatusUpdate{
		StepId: stepID.String(),
		Text:   "reasoning",
	}
	traceID := runtimeids.NewReasoningTraceID()
	hydration.ActiveReasoningTraces = []*transcriptpb.ReasoningTraceUpdate{{
		StepId: stepID.String(),
		Identity: &transcriptpb.ReasoningTraceIdentity{
			Identity: &transcriptpb.ReasoningTraceIdentity_KentTraceId{KentTraceId: traceID.String()},
		},
		CompactText: "reasoning body",
		Text:        "reasoning body",
	}}
	hydration.ActiveStep = &transcriptpb.StepState{
		RunId:      runID.String(),
		StepId:     stepID.String(),
		Lifecycle:  transcriptpb.StepLifecycle_STEP_LIFECYCLE_STARTED,
		ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN,
		Status:     transcriptpb.RunStatus_RUN_STATUS_RUNNING,
	}
	hydration.ActiveCompaction = &transcriptpb.CompactionStatus{
		StepId: stepID.String(),
		State:  transcriptpb.CompactionState_COMPACTION_STATE_STARTED,
		Mode:   transcriptpb.CompactionMode_COMPACTION_MODE_AUTO,
		Count:  1,
	}
	hydration.InFlightTools = []*transcriptpb.ToolStart{{
		StepId:     stepID.String(),
		ToolCallId: "tool-1",
		ToolName:   "shell",
	}}
	hydration.PendingPrompts = []*transcriptpb.Prompt{
		testQuestionPrompt("prompt-1", "Approve hydration?", "yes"),
	}
	return &transcriptpb.Message{Sequence: 1, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_Hydration{Hydration: hydration}}}
}
