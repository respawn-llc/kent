package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"core/cli/tui/ongoing"
	"core/internal/testharness/pty"
	"core/internal/testharness/pty/analyzer"
	"core/internal/testharness/scriptedllm"
	"core/internal/testharness/testsetup"
	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestSourceCompleteHydrationAcceptsNativeCommitWithoutReemission(t *testing.T) {
	text := "Completed source awaiting native acceptance."
	body, err := json.Marshal(map[string]any{
		"type": "message", "id": "source", "role": "assistant", "phase": "final_answer",
		"content": []map[string]string{{"type": "output_text", "text": text}},
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := llm.MessageFromNativeResponse(session.NativeResponseBody{
		Format: session.NativeResponseOpenAI, Items: [][]byte{body},
	})
	if err != nil {
		t.Fatal(err)
	}
	ready, release := make(chan struct{}), make(chan struct{})
	fixture := newStreamSubscriptionFixture(t, scriptedllm.NewClient(scriptedllm.Script{Steps: []scriptedllm.Step{{
		Response:          llm.Response{Assistant: message, OutputItems: llm.ItemsFromMessages([]llm.Message{message})},
		StreamDeltas:      []llm.AssistantDelta{{Text: text, Phase: llm.MessagePhaseFinal, OutputIndex: textutil.Value(int64(0))}},
		SourceCompletions: []llm.AssistantSource{{Text: text, Phase: llm.MessagePhaseFinal, OutputIndex: 0}},
		AfterResponse: func(ctx context.Context) error {
			close(ready)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}}}), tools.NewRegistry(), nil)
	done := fixture.submitUserMessage("run")
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("source did not complete before native acceptance")
	}
	late := subscribeTranscriptForTest(t, fixture.registry, fixture.engine.SessionID())
	defer late.Close()
	hydration := nextTranscriptMessage(t, late)
	sources := hydration.Event.GetHydration().ActiveAssistants
	if len(sources) != 1 || sources[0].SourceState != transcriptpb.AssistantSourceState_ASSISTANT_SOURCE_STATE_COMPLETE {
		t.Fatalf("hydration lost source-complete lifecycle: %+v", sources)
	}
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	frame := ongoing.FrameInput{Size: ongoing.Size{Width: 80, Height: 30}, Theme: "dark"}
	if _, err := surface.ApplyTerminalMessage(hydration, frame); err != nil {
		t.Fatal(err)
	}
	close(release)
	for {
		event := nextTranscriptMessage(t, late)
		if _, err := surface.ApplyTerminalMessage(event, frame); err != nil {
			t.Fatal(err)
		}
		if row := event.Event.GetCommittedRow(); row != nil && row.GetAssistant() != nil {
			if row.GetAssistant().GetSourceState() != transcriptpb.AssistantSourceState_ASSISTANT_SOURCE_STATE_COMPLETE {
				t.Fatal("native commit lost completed-source correlation")
			}
			break
		}
	}
	fixture.awaitSubmission(done)
	terminal, err := analyzer.NewStream(pty.MustDimensions(frame.Size.Height, frame.Size.Width))
	if err != nil {
		t.Fatal(err)
	}
	if err := terminal.Feed(output.Bytes()); err != nil {
		t.Fatal(err)
	}
	screen, err := terminal.ScreenSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(screen.RenderText(), text) != 1 {
		t.Fatal("late hydration and native acceptance did not emit source exactly once")
	}
}

func TestNativeToolResultUpdatesItsAcceptedSourceRow(t *testing.T) {
	message, err := llm.MessageFromNativeResponse(session.NativeResponseBody{
		Format: session.NativeResponseOpenAI,
		Items: [][]byte{
			[]byte(`{"type":"function_call","id":"item_native","call_id":"call-native","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}`),
			[]byte(`{"type":"message","id":"message_native","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"answer","annotations":[]}]}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := newControlledToolHandler("call-native")
	fixture := newStreamSubscriptionFixture(t,
		scriptedllm.NewClient(scriptedllm.Script{Steps: []scriptedllm.Step{{
			Response:     llm.Response{Assistant: message, ToolCalls: message.ToolCalls, OutputItems: llm.ItemsFromMessages([]llm.Message{message})},
			StreamDeltas: []llm.AssistantDelta{{Text: "answer", Phase: llm.MessagePhaseFinal, OutputIndex: textutil.Value(int64(1))}},
		}}}),
		newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: handler}),
		[]toolspec.ID{toolspec.ToolExecCommand},
	)
	done := fixture.submitUserMessage("run")
	var accepted *transcriptpb.CommittedRowLocator
	for accepted == nil {
		row := nextTranscriptMessage(t, fixture.sub).Event.GetCommittedRow()
		if row != nil && row.GetToolCall() != nil {
			accepted = row.Locator
		}
	}
	handler.waitEntered(t, "call-native")
	page, err := fixture.engine.TranscriptNewestSegmentPage()
	if err != nil {
		t.Fatal(err)
	}
	pendingFromPage := false
	for _, row := range runtime.TranscriptCommittedRowFactsFromSnapshot(page.Snapshot) {
		if row.ToolCall != nil && row.ToolCall.ToolCallID == "call-native" {
			pendingFromPage = row.Locator.EventSequence == accepted.EventSequence && row.Locator.RowOrdinal == int64(accepted.RowOrdinal)
		}
	}
	if !pendingFromPage {
		t.Fatal("pending native call lost its source locator in bounded history")
	}
	handler.release("call-native")
	for {
		row := nextTranscriptMessage(t, fixture.sub).Event.GetCommittedRow()
		if row == nil || row.GetTool() == nil {
			continue
		}
		if row.Locator.EventSequence != accepted.EventSequence || row.Locator.RowOrdinal != accepted.RowOrdinal {
			t.Fatal("completed tool moved away from its accepted source locator")
		}
		break
	}
	fixture.awaitSubmission(done)
	_ = fixture.freshCleanHydration()
}

func TestSessionTranscriptSubscriptionContinuesAfterConcurrentLocalToolsWithoutStreamCollision(t *testing.T) {
	handler := newControlledToolHandler("call-a", "call-b")
	firstStep := scriptedllm.ToolBatch("",
		llm.ToolCall{ID: "call-a", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"a"}`)},
		llm.ToolCall{ID: "call-b", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"b"}`)},
	)
	firstStep.StreamDeltas = []llm.AssistantDelta{{Text: "draft", Phase: llm.MessagePhaseCommentary}}
	secondStep := scriptedllm.FinalAnswer("answer")
	secondStep.StreamDeltas = []llm.AssistantDelta{{Text: "answer", Phase: llm.MessagePhaseFinal}}
	secondStep.ExpectedToolResults = []scriptedllm.ExpectedToolResult{
		{CallID: "call-a", Name: string(toolspec.ToolExecCommand)},
		{CallID: "call-b", Name: string(toolspec.ToolExecCommand)},
	}
	fixture := newStreamSubscriptionFixture(
		t,
		scriptedllm.NewClient(scriptedllm.Script{Steps: []scriptedllm.Step{firstStep, secondStep}}),
		newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: handler}),
		[]toolspec.ID{toolspec.ToolExecCommand},
	)
	submitDone := fixture.submitUserMessage("run both")

	for len(fixture.lifecycle.toolStarts) < 2 {
		fixture.next()
	}
	if fixture.lifecycle.toolStarts["call-a"] == nil || fixture.lifecycle.toolStarts["call-b"] == nil {
		t.Fatalf("tool start set = %+v, want call-a and call-b", fixture.lifecycle.toolStarts)
	}
	handler.waitEntered(t, "call-a")
	handler.waitEntered(t, "call-b")

	handler.release("call-b")
	handler.release("call-a")
	for fixture.lifecycle.toolCompletions["call-a"] == nil ||
		fixture.lifecycle.toolCompletions["call-b"] == nil {
		fixture.next()
	}
	for fixture.lifecycle.finalAssistant == nil {
		fixture.next()
	}

	fixture.lifecycle.assertCompleted(t)
	for callID, start := range fixture.lifecycle.toolStarts {
		if start.position <= fixture.lifecycle.abort.position {
			t.Fatalf("%s start position = %d, want after abort position %d", callID, start.position, fixture.lifecycle.abort.position)
		}
	}
	if fixture.lifecycle.toolCompletions["call-a"].position <= fixture.lifecycle.toolStarts["call-a"].position ||
		fixture.lifecycle.toolCompletions["call-b"].position <= fixture.lifecycle.toolCompletions["call-a"].position ||
		fixture.lifecycle.resumedDelta.position <= fixture.lifecycle.toolCompletions["call-b"].position {
		t.Fatalf("tool continuation lifecycle = starts:%+v completions:%+v resumed:%+v", fixture.lifecycle.toolStarts, fixture.lifecycle.toolCompletions, fixture.lifecycle.resumedDelta)
	}
	fixture.awaitSubmission(submitDone)
	fixture.assertSubscriptionOpen()
	_ = fixture.freshCleanHydration()
}

type streamSubscriptionFixture struct {
	t         *testing.T
	registry  *RuntimeRegistry
	engine    *runtime.Engine
	sub       serverapi.TranscriptSubscription
	lifecycle streamLifecycleRecorder
}

func newStreamSubscriptionFixture(t *testing.T, client llm.Client, toolRegistry *tools.Registry, enabledTools []toolspec.ID) *streamSubscriptionFixture {
	t.Helper()
	registry := NewRuntimeRegistry(nil)
	engine := newRegistryRuntime(t, client, toolRegistry, runtime.Config{
		Model:         "gpt-6-sol",
		ThinkingLevel: "medium",
		EnabledTools:  append([]toolspec.ID(nil), enabledTools...),
	}, func(engine *runtime.Engine, evt runtime.Event) {
		registry.PublishAuthorityRuntimeEvent(registryTestResourceRef(engine.SessionID()), evt)
	})
	registerReady(t, registry, engine.SessionID(), engine)
	t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })
	sub := subscribeTranscriptForTest(t, registry, engine.SessionID())
	t.Cleanup(func() { _ = sub.Close() })
	hydration := nextTranscriptMessage(t, sub)
	if hydration.Sequence != 1 || hydration.Event.GetHydration() == nil {
		t.Fatalf("initial message = %+v, want seq=1 hydration", hydration)
	}
	return &streamSubscriptionFixture{
		t:        t,
		registry: registry,
		engine:   engine,
		sub:      sub,
		lifecycle: streamLifecycleRecorder{
			lastSequence:    hydration.Sequence,
			toolStarts:      make(map[string]*eventPosition),
			toolCompletions: make(map[string]*eventPosition),
		},
	}
}

func (f *streamSubscriptionFixture) submitUserMessage(text string) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := f.engine.SubmitUserMessage(context.Background(), text)
		done <- err
	}()
	return done
}

func (f *streamSubscriptionFixture) next() {
	f.t.Helper()
	message := nextTranscriptMessage(f.t, f.sub)
	f.lifecycle.record(f.t, message)
}

func (f *streamSubscriptionFixture) awaitSubmission(done <-chan error) {
	f.t.Helper()
	select {
	case err := <-done:
		if err != nil {
			f.t.Fatalf("SubmitUserMessage: %v", err)
		}
	case <-time.After(time.Second):
		f.t.Fatal("timed out waiting for resumed turn completion")
	}
}

func (f *streamSubscriptionFixture) assertSubscriptionOpen() {
	f.t.Helper()
	for {
		message, err := nextTranscriptMessageTimeout(f.sub, 20*time.Millisecond)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				f.t.Fatalf("original subscription closed after turn: %v", err)
			}
			return
		}
		f.lifecycle.record(f.t, message)
	}
}

func (f *streamSubscriptionFixture) freshCleanHydration() *transcriptpb.Hydration {
	f.t.Helper()
	fresh := subscribeTranscriptForTest(f.t, f.registry, f.engine.SessionID())
	defer func() { _ = fresh.Close() }()
	message := nextTranscriptMessage(f.t, fresh)
	hydration := message.Event.GetHydration()
	if len(hydration.ActiveAssistants) != 0 {
		f.t.Fatalf("fresh hydration active stream = %+v, want none", hydration)
	}
	return hydration
}

type streamObservation struct {
	position int
	streamID string
}

type eventPosition struct {
	position int
}

type streamLifecycleRecorder struct {
	lastSequence    uint64
	nextPosition    int
	initialDelta    *streamObservation
	abort           *streamObservation
	resumedDelta    *streamObservation
	finalAssistant  *streamObservation
	promptResolved  *eventPosition
	toolStarts      map[string]*eventPosition
	toolCompletions map[string]*eventPosition
}

func (r *streamLifecycleRecorder) record(t *testing.T, message *transcriptpb.Message) {
	t.Helper()
	if message.Sequence <= r.lastSequence {
		t.Fatalf("non-monotonic transcript sequence: previous=%d current=%d message=%+v", r.lastSequence, message.Sequence, message)
	}
	r.lastSequence = message.Sequence
	position := r.nextPosition
	r.nextPosition++
	switch message.Event.Payload.(type) {
	case *transcriptpb.Event_AssistantDelta:
		payload := message.Event.GetAssistantDelta()
		observation := &streamObservation{position: position, streamID: payload.StreamId}
		switch payload.Phase {
		case transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY:
			if r.initialDelta == nil {
				r.initialDelta = observation
			}
		case transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL:
			if r.resumedDelta == nil {
				r.resumedDelta = observation
			}
		}
	case *transcriptpb.Event_AssistantStreamAbort:
		payload := message.Event.GetAssistantStreamAbort()
		if payload.Reason == transcriptpb.AssistantAbortReason_ASSISTANT_ABORT_REASON_SUPERSEDED {
			r.abort = &streamObservation{position: position, streamID: payload.StreamId}
		}
	case *transcriptpb.Event_Prompt:
		payload := message.Event.GetPrompt()
		if payload.Status == transcriptpb.PromptStatus_PROMPT_STATUS_RESOLVED {
			r.promptResolved = &eventPosition{position: position}
		}
	case *transcriptpb.Event_ToolStart:
		payload := message.Event.GetToolStart()
		r.toolStarts[payload.ToolCallId] = &eventPosition{position: position}
	case *transcriptpb.Event_CommittedRow:
		payload := message.Event.GetCommittedRow()
		if tool := payload.GetTool(); tool != nil {
			r.toolCompletions[tool.GetToolCallId()] = &eventPosition{position: position}
		}
		if assistant := payload.GetAssistant(); assistant != nil && assistant.StreamId != nil {
			r.finalAssistant = &streamObservation{position: position, streamID: *assistant.StreamId}
		}
	}
}

func (r streamLifecycleRecorder) assertCompleted(t *testing.T) {
	t.Helper()
	if r.initialDelta == nil || r.abort == nil || r.resumedDelta == nil || r.finalAssistant == nil {
		t.Fatalf("incomplete assistant lifecycle: %+v", r)
	}
	for _, id := range []string{r.initialDelta.streamID, r.resumedDelta.streamID, r.finalAssistant.streamID} {
		if _, err := runtimeids.ParseAssistantStreamID(id); err != nil {
			t.Fatalf("assistant stream identity %q is not UUID v4: %v", id, err)
		}
	}
	if r.initialDelta.streamID != r.abort.streamID {
		t.Fatalf("abort stream id = %s, want initial delta stream id %s", r.abort.streamID, r.initialDelta.streamID)
	}
	if r.initialDelta.streamID == r.resumedDelta.streamID || r.resumedDelta.streamID != r.finalAssistant.streamID {
		t.Fatalf("stream identities = initial:%s resumed:%s final:%s", r.initialDelta.streamID, r.resumedDelta.streamID, r.finalAssistant.streamID)
	}
	if r.initialDelta.position >= r.abort.position ||
		r.abort.position >= r.resumedDelta.position ||
		r.resumedDelta.position >= r.finalAssistant.position {
		t.Fatalf("assistant lifecycle order = initial:%d abort:%d resumed:%d final:%d", r.initialDelta.position, r.abort.position, r.resumedDelta.position, r.finalAssistant.position)
	}
}

func hydrationContainsAssistantStream(hydration *transcriptpb.Hydration, streamID string) bool {
	for _, row := range hydration.TailSegment.Entries {
		if assistant := row.GetAssistant(); assistant != nil && assistant.StreamId != nil && *assistant.StreamId == streamID {
			return true
		}
	}
	return false
}

type blockingToolCall struct {
	entered chan struct{}
	release chan struct{}
}

type controlledToolHandler struct {
	fallback *blockingToolCall
	calls    map[string]*blockingToolCall
}

func newBlockingToolHandler() *controlledToolHandler {
	return &controlledToolHandler{fallback: newBlockingToolCall()}
}

func newControlledToolHandler(callIDs ...string) *controlledToolHandler {
	calls := make(map[string]*blockingToolCall, len(callIDs))
	for _, callID := range callIDs {
		calls[callID] = newBlockingToolCall()
	}
	return &controlledToolHandler{calls: calls}
}

func newBlockingToolCall() *blockingToolCall {
	return &blockingToolCall{entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *controlledToolHandler) Call(ctx context.Context, call tools.Call) (tools.Result, error) {
	control := h.calls[call.ID]
	if control == nil {
		control = h.fallback
	}
	if control == nil {
		return tools.Result{}, errors.New("controlled tool call was not configured")
	}
	close(control.entered)
	select {
	case <-ctx.Done():
		return tools.Result{}, ctx.Err()
	case <-control.release:
		return tools.Result{
			CallID: call.ID,
			Name:   call.Name,
			Output: json.RawMessage(`{"output":"done"}`),
		}, nil
	}
}

func (h *controlledToolHandler) waitEntered(t *testing.T, callID string) {
	t.Helper()
	control := h.calls[callID]
	if control == nil {
		t.Fatalf("controlled tool call %q was not configured", callID)
	}
	control.waitEntered(t)
}

func (h *controlledToolHandler) release(callID string) {
	h.calls[callID].releaseCall()
}

func (h *controlledToolHandler) waitFallbackEntered(t *testing.T) {
	t.Helper()
	h.fallback.waitEntered(t)
}

func (h *controlledToolHandler) releaseFallback() {
	h.fallback.releaseCall()
}

func (c *blockingToolCall) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool call to enter")
	}
}

func (c *blockingToolCall) releaseCall() {
	close(c.release)
}

func waitForPendingPrompt(t *testing.T, registry *RuntimeRegistry, sessionID string, toolCallID string) {
	t.Helper()
	var prompts []PendingPromptSnapshot
	if testsetup.Until(time.Now().Add(time.Second), 10*time.Millisecond, func() bool {
		prompts = registry.ListPendingPrompts(sessionID)
		return len(prompts) == 1 && prompts[0].Request.ToolCallID == toolCallID
	}) {
		return
	}
	t.Fatalf("pending prompt %q was not registered: %+v", toolCallID, prompts)
}
