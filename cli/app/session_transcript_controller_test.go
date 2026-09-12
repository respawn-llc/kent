package app

import (
	"core/cli/tui/ongoing"
	"core/cli/tui/transcriptrender"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"reflect"
	"testing"
	"time"
	"unicode"
)

func TestOngoingTranscriptControllerRequiresHydrationFirst(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)

	result, err := controller.Accept(ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_SessionStatus]()))
	if err != nil {
		t.Fatalf("accept non-hydration first message: %v", err)
	}

	if result.Action != ongoing.ResultRequestScratchRehydration {
		t.Fatalf("result action = %q, want scratch rehydration", result.Action)
	}
	if len(surface.calls) != 0 {
		t.Fatalf("surface calls = %v, want none", surface.calls)
	}
}

func TestOngoingTranscriptControllerSequenceGapRequestsScratchRehydration(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)

	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	surface.calls = nil

	result, err := controller.Accept(ongoingTranscriptMessage(3, reflect.TypeFor[*transcriptpb.Event_SessionStatus]()))
	if err != nil {
		t.Fatalf("accept sequence gap: %v", err)
	}

	if result.Action != ongoing.ResultRequestScratchRehydration {
		t.Fatalf("result action = %q, want scratch rehydration", result.Action)
	}
	if len(surface.calls) != 0 {
		t.Fatalf("surface calls = %v, want none", surface.calls)
	}
}

func TestOngoingTranscriptControllerQueuesOriginalMessagesWhileUnowned(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)
	if result, err := controller.SetNormalBufferOwned(false); err != nil || result.Action != ongoing.ResultNoop {
		t.Fatalf("mark unowned result=%+v err=%v", result, err)
	}

	hydration := ongoingHydrationMessage(1)
	live := ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_CommittedRow]())
	if _, err := controller.Accept(hydration); err != nil {
		t.Fatalf("accept queued hydration: %v", err)
	}
	if _, err := controller.Accept(live); err != nil {
		t.Fatalf("accept queued live row: %v", err)
	}
	if len(surface.calls) != 0 {
		t.Fatalf("surface calls while unowned = %v, want none", surface.calls)
	}

	result, err := controller.SetNormalBufferOwned(true)
	if err != nil {
		t.Fatalf("restore ownership: %v", err)
	}
	if result.Action != ongoing.ResultNoop {
		t.Fatalf("restore action = %q, want noop", result.Action)
	}
	wantKinds := []reflect.Type{reflect.TypeFor[*transcriptpb.Event_Hydration](), reflect.TypeFor[*transcriptpb.Event_CommittedRow]()}
	if got := surface.appliedKinds(); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("drained message kinds = %v, want %v", got, wantKinds)
	}
}

func TestOngoingTranscriptControllerHydrationCarriesNoPendingWork(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)
	hydration := ongoingHydrationMessage(1)

	if _, err := controller.Accept(hydration); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}

	if got, want := surface.callKinds(), []string{"apply"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("surface calls = %v, want %v", got, want)
	}
	lines := surface.lastFrameSectionLines(ongoing.FrameSectionQueuedOrSteered)
	if len(lines) != 0 {
		t.Fatalf("hydration produced Pending Work lines = %v", lines)
	}
}

func TestOngoingTranscriptControllerLeavesUserMessageFlushPresentationToStateObserver(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	observed := make([]reflect.Type, 0, 1)
	controller := newOngoingTranscriptController(
		surface,
		ongoingTestFrameProvider,
		noopOngoingTranscriptRuntimeAdmission,
		func(message *transcriptpb.Message, _ runtimeTupleMergeResult) tea.Cmd {
			observed = append(observed, reflect.TypeOf(message.Event.Payload))
			return nil
		})
	if _, _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	surface.calls = nil
	observed = nil

	if _, command, err := controller.Accept(ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_UserMessageFlushed]())); err != nil {
		t.Fatalf("accept user-message flush: %v", err)
	} else if command != nil {
		t.Fatal("user-message flush returned an unexpected state command")
	}

	if got, want := observed, []reflect.Type{reflect.TypeFor[*transcriptpb.Event_UserMessageFlushed]()}; !reflect.DeepEqual(got, want) {
		t.Fatalf("observed message kinds = %v, want %v", got, want)
	}
	if got, want := surface.callKinds(), []string{"render"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("surface calls = %v, want %v", got, want)
	}
}

func TestOngoingTranscriptControllerDrainsQueuedAssistantFinalizationAfterDetail(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)
	streamID := runtimeids.NewAssistantStreamID()
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	if _, err := controller.Accept(transcriptTestMessage(2, &transcriptpb.AssistantDelta{StepId: ongoingTestStepID().String(), StreamId: streamID.String(),
		Delta: "roundtrip commentary\n\n",
		Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY})); err != nil {
		t.Fatalf("accept assistant delta: %v", err)
	}
	surface.calls = nil
	if _, err := controller.SetNormalBufferOwned(false); err != nil {
		t.Fatalf("mark unowned: %v", err)
	}

	if _, err := controller.Accept(transcriptTestMessage(3, &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 1, RowOrdinal: 1}, Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{StepId: ongoingTestStepID().String(), StreamId: textutil.Value(streamID.String()),
			Text:  "roundtrip commentary\n\nroundtrip complete",
			Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL}}})); err != nil {
		t.Fatalf("accept queued assistant finalization: %v", err)
	}
	if len(surface.calls) != 0 {
		t.Fatalf("surface calls while unowned = %v, want none", surface.calls)
	}
	if _, err := controller.SetNormalBufferOwned(true); err != nil {
		t.Fatalf("restore ownership: %v", err)
	}

	if got, want := surface.callKinds(), []string{"apply"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("surface calls after restore = %v, want %v", got, want)
	}
	row := surface.calls[0].message.Event.GetCommittedRow()
	if got, want := row.GetAssistant().Text, "roundtrip commentary\n\nroundtrip complete"; got != want {
		t.Fatalf("drained finalization text = %q, want %q", got, want)
	}
}

func TestOngoingTranscriptControllerQueueOverflowRequestsScratchRehydrationOnRestore(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)
	if _, err := controller.SetNormalBufferOwned(false); err != nil {
		t.Fatalf("mark unowned: %v", err)
	}

	for seq := uint64(1); seq <= ongoingTranscriptQueueLimit+1; seq++ {
		message := ongoingTranscriptMessage(seq, reflect.TypeFor[*transcriptpb.Event_CommittedRow]())
		if seq == 1 {
			message = ongoingHydrationMessage(seq)
		}
		if _, err := controller.Accept(message); err != nil {
			t.Fatalf("accept queued message %d: %v", seq, err)
		}
	}

	result, err := controller.SetNormalBufferOwned(true)
	if err != nil {
		t.Fatalf("restore ownership after overflow: %v", err)
	}
	if result.Action != ongoing.ResultRequestScratchRehydration {
		t.Fatalf("restore action = %q, want scratch rehydration", result.Action)
	}
	if len(surface.calls) != 0 {
		t.Fatalf("surface calls after overflow restore = %v, want no partial drain", surface.calls)
	}
}

func TestOngoingTranscriptControllerQueuesOnlyTerminalWorkWhileUnowned(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	surface.calls = nil
	if _, err := controller.SetNormalBufferOwned(false); err != nil {
		t.Fatalf("mark unowned: %v", err)
	}

	for sequence := uint64(2); sequence <= ongoingTranscriptQueueLimit+2; sequence++ {
		message := ongoingTranscriptMessage(sequence, reflect.TypeFor[*transcriptpb.Event_QueuedMessageState]())
		payload := message.Event.GetQueuedMessageState()
		*payload.Text = "latest queued prompt"
		message = transcriptTestMessage(sequence, payload)
		if _, err := controller.Accept(message); err != nil {
			t.Fatalf("accept app-owned message %d: %v", sequence, err)
		}
	}
	if len(surface.calls) != 0 {
		t.Fatalf("surface calls while unowned = %v, want none", surface.calls)
	}

	result, err := controller.SetNormalBufferOwned(true)
	if err != nil {
		t.Fatalf("restore ownership: %v", err)
	}
	if result.Action != ongoing.ResultNoop {
		t.Fatalf("restore action = %q, want no scratch rehydration", result.Action)
	}
	if got, want := surface.callKinds(), []string{"render"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("surface calls = %v, want %v", got, want)
	}
}

func TestOngoingTranscriptControllerDrainsQueuedNonRowsInArrivalOrderWithOneRender(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	surface.calls = nil

	if _, err := controller.SetNormalBufferOwned(false); err != nil {
		t.Fatalf("mark unowned: %v", err)
	}
	queued := []*transcriptpb.Message{
		ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_RuntimeReadModelUpdate]()),
		ongoingTranscriptMessage(3, reflect.TypeFor[*transcriptpb.Event_PendingWorkChanged]()),
		ongoingTranscriptMessage(4, reflect.TypeFor[*transcriptpb.Event_Prompt]()),
		ongoingTranscriptMessage(5, reflect.TypeFor[*transcriptpb.Event_ContextUsage]()),
		ongoingTranscriptMessage(6, reflect.TypeFor[*transcriptpb.Event_GoalStatus]())}
	for _, message := range queued {
		if _, err := controller.Accept(message); err != nil {
			t.Fatalf("accept queued %s: %v", reflect.TypeOf(message.Event.Payload), err)
		}
	}
	if len(surface.calls) != 0 {
		t.Fatalf("surface calls while unowned = %v, want none", surface.calls)
	}

	if _, err := controller.SetNormalBufferOwned(true); err != nil {
		t.Fatalf("restore ownership: %v", err)
	}

	if got, want := surface.callKinds(), []string{"render"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("surface calls = %v, want %v", got, want)
	}
	wantSections := []ongoing.FrameSectionKind{
		ongoing.FrameSectionPendingPrompt,
	}
	if got := surface.lastFrameSectionKinds(); !reflect.DeepEqual(got, wantSections) {
		t.Fatalf("frame sections = %v, want %v", got, wantSections)
	}
}

func TestOngoingTranscriptControllerReturnsSurfaceErrorsSynchronously(t *testing.T) {
	wantErr := errors.New("surface failed")
	surface := &ongoingSurfaceSpy{err: wantErr}
	controller := newTestOngoingTranscriptController(surface, ongoingTestFrameProvider)

	_, err := controller.Accept(ongoingHydrationMessage(1))
	if !errors.Is(err, wantErr) {
		t.Fatalf("accept error = %v, want %v", err, wantErr)
	}
}

type ongoingSurfaceSpy struct {
	calls []ongoingSurfaceCall
	err   error
}

func ongoingTestFrameProvider() ongoing.FrameInput {
	return ongoing.FrameInput{
		Size:   ongoing.Size{Width: 80, Height: 24},
		Cursor: ongoing.Cursor{Visible: true, Row: 24, Column: 1},
	}
}

type testOngoingTranscriptController struct {
	*ongoingTranscriptController
}

func newTestOngoingTranscriptController(surface ongoingTranscriptSurface, frameProvider ongoingFrameProvider) *testOngoingTranscriptController {
	return &testOngoingTranscriptController{
		ongoingTranscriptController: newNoopOngoingTranscriptController(surface, frameProvider),
	}
}

func newNoopOngoingTranscriptController(surface ongoingTranscriptSurface, frameProvider ongoingFrameProvider) *ongoingTranscriptController {
	return newOngoingTranscriptController(
		surface,
		frameProvider,
		noopOngoingTranscriptRuntimeAdmission,
		func(*transcriptpb.Message, runtimeTupleMergeResult) tea.Cmd {
			return nil
		})
}

func noopOngoingTranscriptRuntimeAdmission(*transcriptpb.Message) (runtimeTupleMergeResult, error) {
	return runtimeTupleMergeResult{}, nil
}

func (c *testOngoingTranscriptController) Accept(message *transcriptpb.Message) (ongoing.Result, error) {
	result, command, err := c.ongoingTranscriptController.Accept(message)
	if command != nil {
		panic("test ongoing transcript controller received an unexpected state command")
	}
	return result, err
}

type ongoingSurfaceCall struct {
	name    string
	message *transcriptpb.Message
	frame   ongoing.FrameInput
}

func (s *ongoingSurfaceSpy) ApplyTerminalMessage(message *transcriptpb.Message, frame ongoing.FrameInput) (ongoing.Result, error) {
	s.calls = append(s.calls, ongoingSurfaceCall{name: "apply", message: message, frame: frame})
	return ongoing.Result{}, s.err
}

func (s *ongoingSurfaceSpy) Render(frame ongoing.FrameInput) (ongoing.Result, error) {
	s.calls = append(s.calls, ongoingSurfaceCall{name: "render", frame: frame})
	return ongoing.Result{}, s.err
}

func (s *ongoingSurfaceSpy) Resize(_ ongoing.Size, frame ongoing.FrameInput) (ongoing.Result, error) {
	s.calls = append(s.calls, ongoingSurfaceCall{name: "resize", frame: frame})
	return ongoing.Result{}, s.err
}

func (s *ongoingSurfaceSpy) appliedKinds() []reflect.Type {
	kinds := make([]reflect.Type, 0, len(s.calls))
	for _, call := range s.calls {
		if call.name == "apply" {
			kinds = append(kinds, reflect.TypeOf(call.message.Event.Payload))
		}
	}
	return kinds
}

func (s *ongoingSurfaceSpy) appliedEvents() []*transcriptpb.Event {
	var events []*transcriptpb.Event
	for _, call := range s.calls {
		if call.name == "apply" {
			events = append(events, call.message.Event)
		}
	}
	return events
}

func (s *ongoingSurfaceSpy) callKinds() []string {
	kinds := make([]string, 0, len(s.calls))
	for _, call := range s.calls {
		kinds = append(kinds, call.name)
	}
	return kinds
}

func (s *ongoingSurfaceSpy) lastFrameSectionKinds() []ongoing.FrameSectionKind {
	if len(s.calls) == 0 {
		return nil
	}
	sections := s.calls[len(s.calls)-1].frame.Sections
	kinds := make([]ongoing.FrameSectionKind, 0, len(sections))
	for _, section := range sections {
		kinds = append(kinds, section.Kind)
	}
	return kinds
}

func (s *ongoingSurfaceSpy) lastFrameSectionLines(kind ongoing.FrameSectionKind) []string {
	if len(s.calls) == 0 {
		return nil
	}
	for _, section := range s.calls[len(s.calls)-1].frame.Sections {
		if section.Kind == kind {
			lines := make([]string, 0, len(section.StyledLines)+len(section.Lines))
			for _, line := range section.StyledLines {
				lines = append(lines, line.Plain())
			}
			lines = append(lines, section.Lines...)
			return lines
		}
	}
	return nil
}

func (s *ongoingSurfaceSpy) lastFrameStyledSection(kind ongoing.FrameSectionKind) []transcriptrender.Line {
	if len(s.calls) == 0 {
		return nil
	}
	for _, section := range s.calls[len(s.calls)-1].frame.Sections {
		if section.Kind == kind {
			return append([]transcriptrender.Line(nil), section.StyledLines...)
		}
	}
	return nil
}

func assertTerminalSafeFrameLines(t *testing.T, lines []string) {
	t.Helper()
	for _, line := range lines {
		for _, r := range line {
			if unicode.IsControl(r) {
				t.Fatalf("frame line %q contains control rune %U", line, r)
			}
		}
	}
}

func ongoingHydrationMessage(sequence uint64) *transcriptpb.Message {
	return transcriptTestMessage(sequence, &transcriptpb.Hydration{
		SessionIdentity: &transcriptpb.SessionIdentity{SessionId: ongoingTestSessionID().String(),
			ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH},
		SessionStatus: &transcriptpb.SessionStatus{
			ReviewerFrequency: "off",
			ThinkingLevel:     "medium",
			CompactionMode:    "native"},
		RuntimeReadModelUpdate: &runtimepb.ReadModelUpdate{
			Version: &runtimepb.ReadModelVersion{Epoch: "ongoing-test", Generation: 1, Sequence: 1},
			Activity: &runtimepb.Activity{
				State:          runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
				Reviewer:       runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
				QueueAccepting: true}},
		TailSegment: &transcriptpb.TailSegment{Entries: []*transcriptpb.CommittedRow{}}})

}

func ongoingTranscriptMessage(sequence uint64, kind reflect.Type) *transcriptpb.Message {
	var payload proto.Message
	switch kind {
	case reflect.TypeFor[*transcriptpb.Event_CommittedRow]():
		payload = &transcriptpb.CommittedRow{
			Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
			Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
			Locator:    &transcriptpb.CommittedRowLocator{EventSequence: int64(sequence), RowOrdinal: 1}, Row: &transcriptpb.CommittedRow_User{User: &transcriptpb.UserRow{StepId: textutil.Value(ongoingTestStepIDPointer().String()), Text: "hello"}}}
	case reflect.TypeFor[*transcriptpb.Event_RuntimeReadModelUpdate]():
		payload = &runtimepb.ReadModelUpdate{
			Version: &runtimepb.ReadModelVersion{Epoch: "ongoing-test", Generation: 1, Sequence: sequence},
			Activity: &runtimepb.Activity{
				State:          runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
				Reviewer:       runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
				QueueAccepting: true}}
	case reflect.TypeFor[*transcriptpb.Event_QueuedMessageState]():
		text := "queued prompt"
		payload = &transcriptpb.QueuedMessageState{QueueItemId: ongoingTestQueueItemID().String(),
			Status: transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_ACCEPTED,
			Text:   &text}
	case reflect.TypeFor[*transcriptpb.Event_PendingWorkChanged]():
		payload = &transcriptpb.PendingWorkChanged{}
	case reflect.TypeFor[*transcriptpb.Event_UserMessageFlushed]():
		payload = &transcriptpb.UserMessageFlushed{StepId: textutil.Value(ongoingTestStepIDPointer().String())}
	case reflect.TypeFor[*transcriptpb.Event_SessionStatus]():
		payload = &transcriptpb.SessionStatus{
			ReviewerFrequency: "off",
			ThinkingLevel:     "high",
			CompactionMode:    "native"}
	case reflect.TypeFor[*transcriptpb.Event_SessionIdentity]():
		sessionName := "KENT-196"
		payload = &transcriptpb.SessionIdentity{SessionId: ongoingTestSessionID().String(),
			SessionName:           &sessionName,
			ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED}
	case reflect.TypeFor[*transcriptpb.Event_CompactionStatus]():
		payload = &transcriptpb.CompactionStatus{StepId: ongoingTestStepID().String(),
			Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_AUTO,
			Count: 2,
			State: transcriptpb.CompactionState_COMPACTION_STATE_COMPLETED}
	case reflect.TypeFor[*transcriptpb.Event_Prompt]():
		payload = testQuestionPrompt("ask-1", "Approve command?")
	case reflect.TypeFor[*transcriptpb.Event_ContextUsage]():
		payload = &runtimepb.ContextUsage{UsedTokens: 1200, WindowTokens: 2000}
	case reflect.TypeFor[*transcriptpb.Event_GoalStatus]():
		payload = &runtimepb.GoalView{Goal: &runtimepb.Goal{Id: "99999999-9999-4999-8999-999999999999", Objective: "finish review fixes",
			Status:    runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE,
			CreatedAt: timestamppb.New(time.Unix(1, 0).UTC()), UpdatedAt: timestamppb.New(time.Unix(1, 0).UTC())}}
	case reflect.TypeFor[*transcriptpb.Event_BackgroundActivity]():
		preview := "running tests"
		payload = &transcriptpb.BackgroundActivity{ActivityId: ongoingTestBackgroundActivityID().String(), ProcessId: "process-1", OwnerRunId: ongoingTestRunID().String(), OwnerStepId: ongoingTestStepID().String(),
			Lifecycle: transcriptpb.BackgroundLifecycle_BACKGROUND_LIFECYCLE_BACKGROUNDED,
			Command:   "go test ./...",
			Workdir:   "/tmp",
			Preview:   &preview}
	case reflect.TypeFor[*transcriptpb.Event_ToolStart]():
		payload = &transcriptpb.ToolStart{StepId: ongoingTestStepID().String(), ToolCallId: "tool-1",
			ToolName: "shell"}
	case reflect.TypeFor[*transcriptpb.Event_ToolAbort]():
		payload = &transcriptpb.ToolAbort{StepId: ongoingTestStepID().String(), ToolCallId: "tool-1",
			Reason: transcriptpb.ToolAbortReason_TOOL_ABORT_REASON_CANCELED}
	default:
		panic("unsupported test message kind " + kind.String())
	}
	return transcriptTestMessage(sequence, payload)
}

func ongoingTestSessionID() runtimeids.SessionID {
	id, err := runtimeids.ParseSessionID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	if err != nil {
		panic(err)
	}
	return id
}

func ongoingTestRunID() runtimeids.RunID {
	id, err := runtimeids.ParseRunID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if err != nil {
		panic(err)
	}
	return id
}

func ongoingTestStepID() runtimeids.StepID {
	id, err := runtimeids.ParseStepID("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	if err != nil {
		panic(err)
	}
	return id
}

func ongoingTestStepIDPointer() *runtimeids.StepID {
	stepID := ongoingTestStepID()
	return &stepID
}

func ongoingTestQueueItemID() runtimeids.QueueItemID {
	id, err := runtimeids.ParseQueueItemID("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	if err != nil {
		panic(err)
	}
	return id
}

func ongoingTestBackgroundActivityID() runtimeids.BackgroundActivityID {
	id, err := runtimeids.ParseBackgroundActivityID("ffffffff-ffff-4fff-8fff-ffffffffffff")
	if err != nil {
		panic(err)
	}
	return id
}
