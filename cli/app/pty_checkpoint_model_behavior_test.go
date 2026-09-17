package app

import (
	"bytes"
	"context"
	"core/cli/tui/ongoing"
	"core/internal/testharness/pty/analyzer"
	"core/internal/testharness/pty/appfixture"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"testing"
)

type ptyCheckpointOrderingModel struct {
	output *analyzer.Writer
}

func (model ptyCheckpointOrderingModel) Init() tea.Cmd { return nil }

func (model ptyCheckpointOrderingModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	if _, err := model.output.Write([]byte("updated|")); err != nil {
		panic(err)
	}
	return model, nil
}

func (model ptyCheckpointOrderingModel) View() string { return "" }

func TestPTYCheckpointModelEmitsInputAppliedAfterInnerUpdate(t *testing.T) {
	var out bytes.Buffer
	writer := analyzer.NewWriter(&out)
	model := newPTYCheckpointModel(
		ptyCheckpointOrderingModel{output: writer},
		writer,
		newPTYCheckpointScenarioState(appfixture.ScriptFinalAssistantOrdinal(1)))

	model.Update(tea.KeyMsg{Type: tea.KeyDown})

	analysis := analyzeCheckpointBytes(t, out.Bytes())
	if got := analysis.Screen.TextInRegion(analyzer.Region{Top: 0, Bottom: 1, Left: 0, Right: 8}); got != "updated|" {
		t.Fatalf("screen text = %q, want update output before checkpoint", got)
	}
	if len(analysis.PhaseEvents) != 1 || analysis.PhaseEvents[0].Phase != analyzer.PhaseInputApplied {
		t.Fatalf("checkpoint events = %#v, want one input-applied event", analysis.PhaseEvents)
	}
	if analysis.PhaseEvents[0].ByteRange.Start < int64(len("updated|")) {
		t.Fatalf("input-applied checkpoint range = %+v, want after inner update output", analysis.PhaseEvents[0].ByteRange)
	}
}

func TestPTYCheckpointModelEmitsPromptReadyOnInteractiveTransition(t *testing.T) {
	var out bytes.Buffer
	writer := analyzer.NewWriter(&out)
	model := sizedTestUIModel(newProjectedStaticUIModel(), 64, 20)
	wrapped := newPTYCheckpointModel(
		model,
		writer,
		newPTYCheckpointScenarioState(appfixture.ScriptFinalAssistantOrdinal(1)))

	_, projection := wrapped.Update(askEventMsg{
		event: testQuestionAskEvent("ask-ready", "Choose.", "continue")})
	if projection == nil {
		t.Fatal("pending prompt did not schedule its projection")
	}
	if events := analyzeCheckpointBytes(t, out.Bytes()).PhaseEvents; len(events) != 0 {
		t.Fatalf("prompt emitted readiness before projection: %#v", events)
	}

	wrapped.Update(projection())

	events := analyzeCheckpointBytes(t, out.Bytes()).PhaseEvents
	if len(events) != 1 || events[0].Phase != analyzer.PhasePromptReady {
		t.Fatalf("checkpoint events = %#v, want one prompt-ready event", events)
	}
}

func TestPTYCheckpointModelQueuesInitialDetailApplicationBeforeRendererWrite(t *testing.T) {
	var out bytes.Buffer
	writer := analyzer.NewWriter(&out)
	model, requestID := newPendingPTYDetailCheckpointModel(t)
	wrapped := newPTYCheckpointModel(
		model,
		writer,
		newPTYCheckpointScenarioState(appfixture.ScriptFinalAssistantOrdinal(1)))

	wrapped.Update(detailTranscriptLoadMsg{
		requestID: requestID,
		page: &transcriptpb.Page{
			SessionId:             detailTestSessionID,
			ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
			Entries:               []*transcriptpb.CommittedRow{detailTestAssistantRow("hydrated row")},
		}})
	view := wrapped.View()
	if view == "" {
		t.Fatal("detail application produced an empty view")
	}
	if _, err := writer.Write([]byte(view)); err != nil {
		t.Fatalf("write rendered view: %v", err)
	}

	analysis := analyzeCheckpointBytes(t, out.Bytes())
	if len(analysis.PhaseEvents) != 1 || analysis.PhaseEvents[0].Phase != analyzer.PhaseDetailInitialPageApplied {
		t.Fatalf("checkpoint events = %#v, want one detail-initial-page-applied event", analysis.PhaseEvents)
	}
	if analysis.PhaseEvents[0].ByteRange.Start != 0 {
		t.Fatalf("detail checkpoint starts at byte %d, want immediately before renderer output", analysis.PhaseEvents[0].ByteRange.Start)
	}
	if analysis.PhaseEvents[0].ByteRange.End >= int64(len(out.Bytes())) {
		t.Fatal("detail checkpoint was not followed by renderer output")
	}
}

func TestPTYCheckpointModelDoesNotQueueInitialDetailApplicationForMalformedPage(t *testing.T) {
	var out bytes.Buffer
	writer := analyzer.NewWriter(&out)
	model, requestID := newPendingPTYDetailCheckpointModel(t)
	wrapped := newPTYCheckpointModel(
		model,
		writer,
		newPTYCheckpointScenarioState(appfixture.ScriptFinalAssistantOrdinal(1)))

	wrapped.Update(detailTranscriptLoadMsg{
		requestID: requestID,
		page: &transcriptpb.Page{
			SessionId:             detailTestSessionID,
			ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
			Entries: []*transcriptpb.CommittedRow{{
				Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_UNSPECIFIED,
				Row:        &transcriptpb.CommittedRow_User{User: &transcriptpb.UserRow{Text: "malformed"}},
			}},
		}})
	view := wrapped.View()
	if view == "" {
		t.Fatal("rejected detail page produced an empty error-state view")
	}
	if _, err := writer.Write([]byte(view)); err != nil {
		t.Fatalf("write rejected-page view: %v", err)
	}

	analysis := analyzeCheckpointBytes(t, out.Bytes())
	for _, event := range analysis.PhaseEvents {
		if event.Phase == analyzer.PhaseDetailInitialPageApplied {
			t.Fatalf("malformed detail page emitted page-applied checkpoint: %#v", analysis.PhaseEvents)
		}
	}
	if model.pendingDetailTranscript != nil || model.detailTranscript.loaded {
		t.Fatalf(
			"malformed detail page changed request state: pending=%#v loaded=%t",
			model.pendingDetailTranscript,
			model.detailTranscript.loaded)
	}
}

func TestPTYCheckpointModelEmitsScenarioFinalAppliedAfterTerminalTransaction(t *testing.T) {
	var out bytes.Buffer
	writer := analyzer.NewWriter(&out)
	model := newPTYCheckpointOngoingModel(t, writer)
	out.Reset()

	scenario := newPTYCheckpointScenarioState(appfixture.ScriptFinalAssistantOrdinal(1))
	scenario.markScenarioComplete()
	wrapped := newPTYCheckpointModel(model, writer, scenario)
	wrapped.Update(dispatchPTYCheckpointTranscriptEvent(ongoingTranscriptEvent{
		Kind: ongoingTranscriptEventMessage,
		Message: transcriptTestMessage(2, &transcriptpb.CommittedRow{
			Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
			Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
			Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 1, RowOrdinal: 1}, Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{StepId: ptyCheckpointStepID().String(), StreamId: ptyCheckpointAssistantStreamID(),
				Text:  "final response",
				Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL}}})}))

	analysis := analyzeCheckpointBytes(t, out.Bytes())
	if len(analysis.PhaseEvents) != 1 ||
		analysis.PhaseEvents[0].Phase != analyzer.PhaseScenarioFinalApplied {
		t.Fatalf("checkpoint events = %#v, want one scenario-final-applied event", analysis.PhaseEvents)
	}
	lastTerminalOperationEnd := ptyLastTerminalOperationEnd(analysis)
	if analysis.PhaseEvents[0].ByteRange.Start < lastTerminalOperationEnd {
		t.Fatalf(
			"scenario-final checkpoint starts at byte %d before terminal transaction ended at %d",
			analysis.PhaseEvents[0].ByteRange.Start,
			lastTerminalOperationEnd)
	}
}

func TestPTYCheckpointModelEmitsToolStartedOnceAfterAcceptedToolStart(t *testing.T) {
	var out bytes.Buffer
	writer := analyzer.NewWriter(&out)
	rejectedModel := newPTYCheckpointOngoingModel(t, writer)
	out.Reset()

	wrapped := newPTYCheckpointModel(
		rejectedModel,
		writer,
		newPTYCheckpointScenarioState(appfixture.ScriptFinalAssistantOrdinal(1)))
	wrapped.Update(dispatchPTYCheckpointTranscriptEvent(ongoingTranscriptEvent{
		Kind: ongoingTranscriptEventMessage,
		Message: transcriptTestMessage(3, &transcriptpb.ToolStart{StepId: ptyCheckpointStepID().String(), ToolCallId: "66666666-6666-4666-8666-666666666666",
			ToolName: "exec_command"})}))
	if events := analyzeCheckpointBytes(t, out.Bytes()).PhaseEvents; len(events) != 0 {
		t.Fatalf("rejected tool start emitted checkpoint events: %#v", events)
	}

	model := newPTYCheckpointOngoingModel(t, writer)
	out.Reset()
	wrapped = newPTYCheckpointModel(
		model,
		writer,
		newPTYCheckpointScenarioState(appfixture.ScriptFinalAssistantOrdinal(1)))
	wrapped.Update(dispatchPTYCheckpointTranscriptEvent(ongoingTranscriptEvent{
		Kind: ongoingTranscriptEventMessage,
		Message: transcriptTestMessage(2, &transcriptpb.ToolStart{StepId: ptyCheckpointStepID().String(), ToolCallId: "77777777-7777-4777-8777-777777777777",
			ToolName: "exec_command"})}))
	wrapped.Update(dispatchPTYCheckpointTranscriptEvent(ongoingTranscriptEvent{
		Kind: ongoingTranscriptEventMessage,
		Message: transcriptTestMessage(3, &transcriptpb.ToolStart{StepId: ptyCheckpointStepID().String(), ToolCallId: "88888888-8888-4888-8888-888888888888",
			ToolName: "exec_command"})}))

	analysis := analyzeCheckpointBytes(t, out.Bytes())
	if len(analysis.PhaseEvents) != 1 ||
		analysis.PhaseEvents[0].Phase != analyzer.PhaseToolStarted {
		t.Fatalf("checkpoint events = %#v, want one tool-started event", analysis.PhaseEvents)
	}
	if model.ongoingTranscript.lastSequence != 3 {
		t.Fatalf("accepted transcript sequence = %d, want 3", model.ongoingTranscript.lastSequence)
	}
}

func TestPTYCheckpointModelIgnoresUninitializedToolStartMessage(t *testing.T) {
	message := dispatchPTYCheckpointTranscriptEvent(ongoingTranscriptEvent{
		Kind: ongoingTranscriptEventMessage})

	got := ongoingToolStartCandidate(nil, message)
	if got.valid {
		t.Fatalf("uninitialized transcript message produced a tool-start candidate: %+v", got)
	}
}

func TestPTYCheckpointModelEmitsScenarioFinalAppliedAfterDeferredDetailTransaction(t *testing.T) {
	var out bytes.Buffer
	writer := analyzer.NewWriter(&out)
	model := newPTYCheckpointOngoingModel(t, writer)
	if _, err := model.ongoingTranscript.SetNormalBufferOwned(false); err != nil {
		t.Fatalf("mark normal buffer unowned: %v", err)
	}
	model.activeSurface = uiSurfaceTranscriptDetail
	_ = model.reconcileOngoingOwnership()
	out.Reset()

	scenario := newPTYCheckpointScenarioState(appfixture.ScriptFinalAssistantOrdinal(1))
	scenario.markScenarioComplete()
	wrapped := newPTYCheckpointModel(model, writer, scenario)
	applyPTYCheckpointAssistantFinal(wrapped, 2, "deferred target final")
	if events := analyzeCheckpointBytes(t, out.Bytes()).PhaseEvents; len(events) != 0 {
		t.Fatalf("phase events before normal-buffer restore = %#v, want none", events)
	}
	if out.Len() != 0 {
		t.Fatalf("deferred target final wrote terminal bytes before restore: %q", out.String())
	}

	model.activeSurface = uiSurfaceOngoingTranscript
	wrapped.Update(ongoingNormalBufferOwnedMsg{owned: true})

	analysis := analyzeCheckpointBytes(t, out.Bytes())
	if len(analysis.PhaseEvents) != 1 ||
		analysis.PhaseEvents[0].Phase != analyzer.PhaseScenarioFinalApplied {
		t.Fatalf("phase events after deferred transaction = %#v, want one scenario-final-applied event", analysis.PhaseEvents)
	}
	lastTerminalOperationEnd := ptyLastTerminalOperationEnd(analysis)
	if analysis.PhaseEvents[0].ByteRange.Start < lastTerminalOperationEnd {
		t.Fatalf(
			"deferred scenario-final checkpoint starts at byte %d before terminal transaction ended at %d",
			analysis.PhaseEvents[0].ByteRange.Start,
			lastTerminalOperationEnd)
	}
}

func TestPTYCheckpointModelCorrelatesScenarioFinalAppliedToTargetScriptFinalExactlyOnce(t *testing.T) {
	var out bytes.Buffer
	writer := analyzer.NewWriter(&out)
	model := newPTYCheckpointOngoingModel(t, writer)
	out.Reset()

	scenario := newPTYCheckpointScenarioState(appfixture.ScriptFinalAssistantOrdinal(2))
	scenario.markScenarioComplete()
	wrapped := newPTYCheckpointModel(model, writer, scenario)

	applyPTYCheckpointAssistantFinal(wrapped, 2, "delayed earlier final")
	if events := analyzeCheckpointBytes(t, out.Bytes()).PhaseEvents; len(events) != 0 {
		t.Fatalf("phase events after earlier final = %#v, want none before target final", events)
	}

	applyPTYCheckpointAssistantFinal(wrapped, 3, "target final")
	analysis := analyzeCheckpointBytes(t, out.Bytes())
	if len(analysis.PhaseEvents) != 1 ||
		analysis.PhaseEvents[0].Phase != analyzer.PhaseScenarioFinalApplied {
		t.Fatalf("phase events after target final = %#v, want one scenario-final-applied event", analysis.PhaseEvents)
	}

	applyPTYCheckpointAssistantFinal(wrapped, 4, "unexpected later final")
	analysis = analyzeCheckpointBytes(t, out.Bytes())
	if len(analysis.PhaseEvents) != 1 ||
		analysis.PhaseEvents[0].Phase != analyzer.PhaseScenarioFinalApplied {
		t.Fatalf("phase events after later final = %#v, want exactly one scenario-final-applied event", analysis.PhaseEvents)
	}
}

func newPTYCheckpointOngoingModel(t *testing.T, writer *analyzer.Writer) *uiModel {
	t.Helper()
	surface := ongoing.NewSurface(writer)
	model := sizedTestUIModel(newProjectedStaticUIModel(
		WithUIOngoingSurface(surface)), 40, 10)
	runtimeClient := newUIRuntimeClientWithReads(
		ongoingTestSessionID().String(), &countingSessionViewClient{},
		newUnavailableRuntimeControlService(), nil, nil,
	).(*sessionRuntimeClient)
	model.ongoingTranscript = newOngoingTranscriptController(
		surface,
		model.ongoingFrameInput,
		runtimeClient.admitTranscriptMessageState,
		model.applyAdmittedTranscriptMessageState)
	if _, _, err := model.ongoingTranscript.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept initial hydration: %v", err)
	}
	return model
}

func ptyLastTerminalOperationEnd(analysis analyzer.Analysis) int64 {
	var last int64
	for _, operation := range analysis.Operations {
		if operation.ByteRange.End > last {
			last = operation.ByteRange.End
		}
	}
	return last
}

func applyPTYCheckpointAssistantFinal(model *ptyCheckpointModel, sequence uint64, text string) {
	model.Update(dispatchPTYCheckpointTranscriptEvent(ongoingTranscriptEvent{
		Kind: ongoingTranscriptEventMessage,
		Message: transcriptTestMessage(sequence, &transcriptpb.CommittedRow{
			Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
			Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
			Locator:    &transcriptpb.CommittedRowLocator{EventSequence: int64(sequence), RowOrdinal: 1}, Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{StepId: ptyCheckpointStepID().String(), StreamId: ptyCheckpointAssistantStreamID(),
				Text:  text,
				Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL}}})}))
}

func ptyCheckpointStepID() runtimeids.StepID {
	id, err := runtimeids.ParseStepID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		panic(err)
	}
	return id
}

func ptyCheckpointAssistantStreamID() *string {
	id, err := runtimeids.ParseAssistantStreamID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		panic(err)
	}
	value := id.String()
	return &value
}

func newPendingPTYDetailCheckpointModel(t *testing.T) (*uiModel, uuid.UUID) {
	t.Helper()
	model := newProjectedClosedUIModel(
		&runtimeControlFakeClient{},
		WithUISessionID(detailTestSessionID))
	model.view = mustUpdateTUIModel(t, model.view, tea.KeyMsg{Type: tea.KeyShiftTab})
	model.activeSurface = uiSurfaceTranscriptDetail
	requestID := uuid.New()
	_, cancel := context.WithCancel(context.Background())
	sessionID, err := runtimeids.ParseSessionID(detailTestSessionID)
	if err != nil {
		t.Fatalf("parse detail test session ID: %v", err)
	}
	model.pendingDetailTranscript = &uiPendingDetailTranscriptRequest{
		id:         requestID,
		sessionID:  sessionID,
		request:    &transcriptpb.PageRequest{},
		detailMode: true,
		cancel:     cancel}
	t.Cleanup(model.Close)
	return model, requestID
}

func analyzeCheckpointBytes(t *testing.T, payload []byte) analyzer.Analysis {
	t.Helper()
	capture, err := analyzer.NewCapture(
		analyzer.MustDimensions(24, 80),
		[]analyzer.Chunk{analyzer.NewChunk(0, 0, payload)})
	if err != nil {
		t.Fatalf("new checkpoint capture: %v", err)
	}
	analysis, err := analyzer.Analyze(capture)
	if err != nil {
		t.Fatalf("analyze checkpoint capture: %v", err)
	}
	return analysis
}
