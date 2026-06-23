package app

import (
	"bytes"
	"context"
	"core/cli/tui"
	"core/server/llm"
	"core/server/runtime"
	"core/server/tools"
	sharedclient "core/shared/client"
	"core/shared/clientui"
	"core/shared/toolspec"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNativePSOverlayEscBalancesAltScreenAndAlternateScroll(t *testing.T) {
	var seqMu sync.Mutex
	var terminalSequences []string
	originalWriteTerminalSequence := writeTerminalSequence
	writeTerminalSequence = func(sequence string) {
		seqMu.Lock()
		terminalSequences = append(terminalSequences, sequence)
		seqMu.Unlock()
	}
	defer func() {
		writeTerminalSequence = originalWriteTerminalSequence
	}()
	sequenceLogSnapshot := func() string {
		seqMu.Lock()
		defer seqMu.Unlock()
		return strings.Join(terminalSequences, "")
	}

	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
	)
	model.input = "/ps"

	program := startNativeProgram(t, model, out)

	time.Sleep(40 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	time.Sleep(20 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	// Gate on the mutex-guarded alternate-scroll enable sequence (not live model state) so the
	// overlay is open before it is closed, without racing the program goroutine's model writes.
	waitForTestCondition(t, 2*time.Second, "/ps overlay to enable alternate-scroll", func() bool {
		return strings.Contains(sequenceLogSnapshot(), "\x1b[?1007h")
	})
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForTestCondition(t, 2*time.Second, "/ps overlay to close and disable alternate-scroll", func() bool {
		return !model.processList.open && model.surface() != uiSurfaceProcessList && model.view.Mode() == tui.ModeOngoing &&
			strings.Contains(sequenceLogSnapshot(), "\x1b[?1007l")
	})
	program.QuitAndWait(2 * time.Second)

	raw := out.String()
	enterAlt := strings.Count(raw, "\x1b[?1049h")
	exitAlt := strings.Count(raw, "\x1b[?1049l")
	if enterAlt != exitAlt {
		t.Fatalf("expected balanced /ps alt-screen enter/exit sequences, enter=%d exit=%d", enterAlt, exitAlt)
	}
	if enterAlt == 0 {
		t.Fatal("expected /ps overlay in native mode to enter alt-screen under auto policy")
	}
	sequenceLog := sequenceLogSnapshot()
	enableAltScroll := strings.Count(sequenceLog, "\x1b[?1007h")
	disableAltScroll := strings.Count(sequenceLog, "\x1b[?1007l")
	if enableAltScroll != 1 || disableAltScroll != 1 {
		t.Fatalf("expected /ps overlay to pair alternate-scroll enable/disable, enable=%d disable=%d log=%q", enableAltScroll, disableAltScroll, sequenceLog)
	}
}

func TestNativePSOverlayUsesFixedAltScreen(t *testing.T) {
	var seqMu sync.Mutex
	var terminalSequences []string
	originalWriteTerminalSequence := writeTerminalSequence
	writeTerminalSequence = func(sequence string) {
		seqMu.Lock()
		terminalSequences = append(terminalSequences, sequence)
		seqMu.Unlock()
	}
	defer func() {
		writeTerminalSequence = originalWriteTerminalSequence
	}()
	sequenceLogSnapshot := func() string {
		seqMu.Lock()
		defer seqMu.Unlock()
		return strings.Join(terminalSequences, "")
	}

	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
	)
	model.input = "/ps"

	program := startNativeProgram(t, model, out)

	time.Sleep(40 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	time.Sleep(20 * time.Millisecond)
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	// Gate on the mutex-guarded alternate-scroll enable sequence (not live model state) so the
	// overlay is open before it is closed, without racing the program goroutine's model writes.
	waitForTestCondition(t, 2*time.Second, "/ps overlay to enable alternate-scroll", func() bool {
		return strings.Contains(sequenceLogSnapshot(), "\x1b[?1007h")
	})
	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForTestCondition(t, 2*time.Second, "/ps overlay to disable alternate-scroll", func() bool {
		return strings.Contains(sequenceLogSnapshot(), "\x1b[?1007l")
	})
	program.QuitAndWait(2 * time.Second)

	raw := out.String()
	enterAlt := strings.Count(raw, "\x1b[?1049h")
	exitAlt := strings.Count(raw, "\x1b[?1049l")
	if enterAlt == 0 || enterAlt != exitAlt {
		t.Fatalf("expected balanced /ps alt-screen enter/exit sequences, enter=%d exit=%d raw=%q", enterAlt, exitAlt, raw)
	}
	sequenceLog := sequenceLogSnapshot()
	enableAltScroll := strings.Count(sequenceLog, "\x1b[?1007h")
	disableAltScroll := strings.Count(sequenceLog, "\x1b[?1007l")
	if enableAltScroll != 1 || disableAltScroll != 1 {
		t.Fatalf("expected /ps overlay to pair alternate-scroll enable/disable, enable=%d disable=%d log=%q", enableAltScroll, disableAltScroll, sequenceLog)
	}
}

func TestNativeFinalizeDoesNotBlinkDuplicateTailTokens(t *testing.T) {
	runtimeEvents := make(chan runtime.Event, 256)
	_, eng := newAppRuntimeEngine(
		t,
		singleChunkStreamClient{delta: "TAIL-ONCE"},
		runtime.Config{
			OnEvent: func(evt runtime.Event) {
				runtimeEvents <- evt
			},
		},
	)

	out := newLockedBuffer()
	model := newProjectedTestUIModel(newUIRuntimeClient(eng), projectRuntimeEventChannel(runtimeEvents, nil, nil), closedAskEvents())
	observed := newObservedUIModel(model)

	program := startNativeProgram(t, observed, out)

	waitForSignal(t, 2*time.Second, "program startup output", out.Started())
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	submitDone := make(chan error, 1)
	go func() {
		_, err := eng.SubmitUserMessage(context.Background(), "trigger")
		submitDone <- err
	}()
	waitForTestCondition(t, 2*time.Second, "noop final to clear ongoing state", func() bool {
		if strings.TrimSpace(model.view.OngoingStreamingText()) != "" {
			return false
		}
		if model.sawAssistantDelta {
			return false
		}
		if strings.Count(model.nativeRenderedSnapshot, "TAIL-ONCE") != 1 {
			return false
		}
		for _, entry := range eng.RecentTailTranscriptWindow(1 << 20).Snapshot.Entries {
			if strings.Contains(entry.Text, "NO_OP") {
				return false
			}
		}
		return true
	})
	waitForSubmitResult(t, 2*time.Second, submitDone)
	program.QuitAndWait(2 * time.Second)

	if count := strings.Count(model.nativeRenderedSnapshot, "TAIL-ONCE"); count != 1 {
		t.Fatalf("expected native rendered snapshot to contain tail token once, count=%d snapshot=%q", count, model.nativeRenderedSnapshot)
	}
}

func TestNativeFinalizeSuppressesLateAsyncDeltaArtifacts(t *testing.T) {
	runtimeEvents := make(chan runtime.Event, 256)
	_, eng := newAppRuntimeEngine(
		t,
		asyncLateDeltaStreamClient{initial: "FINAL-CONTENT", late: "LATE-BLINK", delay: 25 * time.Millisecond},
		runtime.Config{
			OnEvent: func(evt runtime.Event) {
				runtimeEvents <- evt
			},
		},
	)

	out := newLockedBuffer()
	model := newProjectedTestUIModel(newUIRuntimeClient(eng), projectRuntimeEventChannel(runtimeEvents, nil, nil), closedAskEvents())
	observed := newObservedUIModel(model)

	program := startNativeProgram(t, observed, out)

	waitForSignal(t, 2*time.Second, "program startup output", out.Started())
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	submitDone := make(chan error, 1)
	go func() {
		_, err := eng.SubmitUserMessage(context.Background(), "trigger")
		submitDone <- err
	}()
	time.Sleep(260 * time.Millisecond)
	waitForSubmitResult(t, 2*time.Second, submitDone)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if strings.TrimSpace(model.view.OngoingStreamingText()) == "" && !model.sawAssistantDelta {
			break
		}
		if time.Now().After(deadline) {
			snapshot := eng.RecentTailTranscriptWindow(1 << 20).Snapshot
			t.Fatalf("timed out waiting for final commit to clear ongoing state output=%q flush_seq=%d flushed_seq=%d pending_flushes=%d runtime_transcript=%+v ui_transcript=%+v native_projection=%+v native_rendered_projection=%+v native_snapshot=%q ongoing=%q", normalizedOutput(out.String()), model.nativeFlushSequence, model.nativeFlushedSequence, len(model.nativePendingFlushes), snapshot.Entries, model.transcriptEntries, model.nativeProjection, model.nativeRenderedProjection, model.nativeRenderedSnapshot, stripANSIAndTrimRight(model.view.OngoingSnapshot()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	program.QuitAndWait(2 * time.Second)

	normalized := normalizedOutput(out.String())
	if !strings.Contains(normalized, "FINAL-CONTENT") {
		snapshot := eng.RecentTailTranscriptWindow(1 << 20).Snapshot
		t.Fatalf("expected final content in output, got output=%q flush_seq=%d flushed_seq=%d pending_flushes=%d runtime_transcript=%+v ui_transcript=%+v native_projection=%+v native_rendered_projection=%+v native_snapshot=%q ongoing=%q", normalized, model.nativeFlushSequence, model.nativeFlushedSequence, len(model.nativePendingFlushes), snapshot.Entries, model.transcriptEntries, model.nativeProjection, model.nativeRenderedProjection, model.nativeRenderedSnapshot, stripANSIAndTrimRight(model.view.OngoingSnapshot()))
	}
	if strings.Contains(normalized, "LATE-BLINK") {
		t.Fatalf("expected late async delta to be suppressed after finalize, got %q", normalized)
	}
	if strings.TrimSpace(model.view.OngoingStreamingText()) != "" {
		t.Fatalf("expected live streaming buffer cleared after commit, got %q", model.view.OngoingStreamingText())
	}
	if model.sawAssistantDelta {
		t.Fatal("expected sawAssistantDelta cleared after finalize commit")
	}
}

func TestNativeSubmitErrorShowsStatusOnlyWhenRuntimeAppendFails(t *testing.T) {
	out := &bytes.Buffer{}
	client := &runtimeControlFakeClient{submitErr: errors.New("daemon stalled"), appendErr: errors.New("append failed")}
	model := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	model.input = "run task"

	program := startNativeProgram(t, model, out)

	time.Sleep(40 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	waitForTestCondition(t, 2*time.Second, "window size to apply before submit", func() bool {
		return model.windowSizeKnown
	})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(model.transcriptEntries) == 0 && model.transientStatus == "append failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for submit append error status output=%q status=%q transcript=%+v native_projection=%+v native_rendered_projection=%+v native_snapshot=%q ongoing=%q window=%t replayed=%t flushed=%d", normalizedOutput(out.String()), model.transientStatus, model.transcriptEntries, model.nativeProjection, model.nativeRenderedProjection, model.nativeRenderedSnapshot, stripANSIAndTrimRight(model.view.OngoingSnapshot()), model.windowSizeKnown, model.nativeHistoryReplayed, model.nativeFlushedEntryCount)
		}
		time.Sleep(10 * time.Millisecond)
	}

	program.QuitAndWait(2 * time.Second)

	if len(model.transcriptEntries) != 0 {
		t.Fatalf("runtime append failure must not create native transcript entries: %+v", model.transcriptEntries)
	}
	if normalized := normalizedOutput(out.String()); strings.Contains(normalized, "daemon stalled") {
		t.Fatalf("submit error was written into native scrollback/status output: %q", normalized)
	}
}

func TestNativeDisconnectedSubmissionShowsStatusOnlyWhenRuntimeAppendFails(t *testing.T) {
	out := &bytes.Buffer{}
	client := &runtimeControlFakeClient{appendErr: errors.New("append failed")}
	model := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	model.input = "run task"

	program := startNativeProgram(t, model, out)

	time.Sleep(40 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	waitForTestCondition(t, 2*time.Second, "window size to apply before disconnected submit", func() bool {
		return model.windowSizeKnown
	})
	model.setRuntimeDisconnected(true)
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(model.transcriptEntries) == 0 && model.transientStatus == "append failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for disconnected append error status output=%q status=%q transcript=%+v native_projection=%+v native_rendered_projection=%+v native_snapshot=%q ongoing=%q window=%t replayed=%t flushed=%d", normalizedOutput(out.String()), model.transientStatus, model.transcriptEntries, model.nativeProjection, model.nativeRenderedProjection, model.nativeRenderedSnapshot, stripANSIAndTrimRight(model.view.OngoingSnapshot()), model.windowSizeKnown, model.nativeHistoryReplayed, model.nativeFlushedEntryCount)
		}
		time.Sleep(10 * time.Millisecond)
	}

	program.QuitAndWait(2 * time.Second)

	if len(model.transcriptEntries) != 0 {
		t.Fatalf("runtime append failure must not create native transcript entries: %+v", model.transcriptEntries)
	}
	// A reachability-confirming append failure reconnects, so the disconnect notice is
	// cleared and the live status shows the transient append error instead. Assert on
	// the resolved connection state rather than scanning the accumulated terminal
	// buffer: that buffer also retains transient pre-reconnect frames, which made the
	// raw-string check timing-dependent (it flaked under slow CI render scheduling).
	if model.runtimeDisconnectStatusVisible() {
		t.Fatalf("a reachability-confirming append failure must clear the disconnect status; transient=%q", model.transientStatus)
	}
}

func TestNativeDisconnectedSubmissionAfterRealRemoteDisconnectAppendsToScrollback(t *testing.T) {
	server := newRuntimeDisconnectTestRemote(t)
	defer server.Close()
	remote, err := sharedclient.DialRemoteURLForProject(context.Background(), "ws"+server.URL()[len("http"):], "project-1")
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	defer func() { _ = remote.Close() }()

	runtimeClient := newUIRuntimeClientWithReads("session-1", remote, remote)
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(runtimeClient, closedProjectedRuntimeEvents(), closedAskEvents())
	model.input = "run task"

	program := startNativeProgram(t, model, out)

	time.Sleep(40 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	waitForTestCondition(t, 2*time.Second, "window size to apply before real disconnect", func() bool {
		return model.windowSizeKnown
	})

	server.Close()
	var refreshErr error
	waitForTestCondition(t, 2*time.Second, "refresh main view error after remote shutdown", func() bool {
		_, refreshErr = runtimeClient.RefreshMainView()
		return refreshErr != nil
	})
	waitForTestCondition(t, 2*time.Second, "disconnect state after real remote shutdown", func() bool {
		return model.runtimeDisconnectStatusVisible()
	})

	program.Send(tea.KeyMsg{Type: tea.KeyEnter})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(model.transcriptEntries) == 0 && strings.Contains(normalizedOutput(out.String()), runtimeDisconnectedStatusMessage) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for real disconnect status output=%q transcript=%+v native_projection=%+v native_rendered_projection=%+v native_snapshot=%q ongoing=%q window=%t replayed=%t flushed=%d", normalizedOutput(out.String()), model.transcriptEntries, model.nativeProjection, model.nativeRenderedProjection, model.nativeRenderedSnapshot, stripANSIAndTrimRight(model.view.OngoingSnapshot()), model.windowSizeKnown, model.nativeHistoryReplayed, model.nativeFlushedEntryCount)
		}
		time.Sleep(10 * time.Millisecond)
	}

	program.QuitAndWait(2 * time.Second)

	if normalized := normalizedOutput(out.String()); !strings.Contains(normalized, runtimeDisconnectedStatusMessage) {
		t.Fatalf("expected disconnect error in native status output, got %q", normalized)
	}
	if len(model.transcriptEntries) != 0 {
		t.Fatalf("real disconnect must not create local transcript entries: %+v", model.transcriptEntries)
	}
}

func TestNativeBackCommandSystemFeedbackAppendsToScrollback(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedStaticUIModel()
	model.input = "/back"

	program := startNativeProgram(t, model, out)

	time.Sleep(40 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	waitForTestCondition(t, 2*time.Second, "window size to apply before back command", func() bool {
		return model.windowSizeKnown
	})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(model.transcriptEntries) == 0 && strings.Contains(normalizedOutput(out.String()), "No parent session available") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for back command status output=%q transcript=%+v native_projection=%+v native_rendered_projection=%+v native_snapshot=%q ongoing=%q window=%t replayed=%t flushed=%d", normalizedOutput(out.String()), model.transcriptEntries, model.nativeProjection, model.nativeRenderedProjection, model.nativeRenderedSnapshot, stripANSIAndTrimRight(model.view.OngoingSnapshot()), model.windowSizeKnown, model.nativeHistoryReplayed, model.nativeFlushedEntryCount)
		}
		time.Sleep(10 * time.Millisecond)
	}

	program.QuitAndWait(2 * time.Second)

	if normalized := normalizedOutput(out.String()); !strings.Contains(normalized, "No parent session available") {
		t.Fatalf("expected back command feedback in native status output, got %q", normalized)
	}
	if len(model.transcriptEntries) != 0 {
		t.Fatalf("back command feedback must not create transcript entries: %+v", model.transcriptEntries)
	}
}

func TestNativeDeferredFinalWithQueuedInjectionKeepsAssistantBeforeQueuedUserInScrollback(t *testing.T) {
	releaseFirst := make(chan struct{})
	firstDelta := make(chan struct{})
	releasedFirst := false
	defer func() {
		if !releasedFirst {
			close(releaseFirst)
		}
	}()
	var program *tea.Program
	_, eng := newAppRuntimeEngine(
		t,
		&deferredFinalQueuedInjectionStreamClient{releaseFirst: releaseFirst, firstDelta: firstDelta},
		runtime.Config{
			Reviewer: runtime.ReviewerConfig{
				Frequency:     "all",
				Model:         "gpt-5",
				ThinkingLevel: "low",
				Client:        reviewerNoSuggestionsClient{},
			},
			OnEvent: func(evt runtime.Event) {
				if evt.Kind == runtime.EventAssistantDelta {
					return
				}
				if program != nil {
					program.Send(projectedRuntimeEventMsg(evt))
				}
			},
		},
	)
	eng.QueueUserMessage("steer now")

	out := newLockedBuffer()
	model := newProjectedTestUIModel(newUIRuntimeClient(eng), closedProjectedRuntimeEvents(), closedAskEvents())
	observed := newObservedUIModel(model)

	programHarness := startNativeProgram(t, observed, out)
	program = programHarness.program

	waitForSignal(t, 2*time.Second, "program startup output", out.Started())
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	submitDone := make(chan error, 1)
	go func() {
		_, err := eng.SubmitUserMessage(context.Background(), "run task")
		submitDone <- err
	}()

	waitForSignal(t, 2*time.Second, "first deferred final delta", firstDelta)
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "foreground done"}))
	observed.waitFor(t, 2*time.Second, "live deferred final delta visible", func(snapshot observedUISnapshot) bool {
		return strings.Contains(snapshot.OngoingStreamingText, "foreground done")
	})
	close(releaseFirst)
	releasedFirst = true

	waitForSubmitResult(t, 2*time.Second, submitDone)
	observed.waitFor(t, 2*time.Second, "deferred final stream cleared after commit", func(snapshot observedUISnapshot) bool {
		return strings.TrimSpace(snapshot.OngoingStreamingText) == "" && !snapshot.SawAssistantDelta &&
			containsInOrder(normalizedOutput(out.String()), "run task", "foreground done", "steer now")
	})
	waitForTestCondition(t, 2*time.Second, "deferred final committed before queued user flush in output", func() bool {
		return containsInOrder(normalizedOutput(out.String()), "run task", "foreground done", "steer now")
	})

	programHarness.QuitAndWait(2 * time.Second)

	normalized := normalizedOutput(out.String())
	if !containsInOrder(normalized, "run task", "foreground done", "steer now") {
		t.Fatalf("expected deferred final before queued injected user in ongoing scrollback, got %q", normalized)
	}
}

func TestNativeDeferredFinalWithQueuedInjectionSurvivesDetailModeRoundTrip(t *testing.T) {
	const roundTripTimeout = 5 * time.Second
	releaseFirst := make(chan struct{})
	firstDelta := make(chan struct{})
	releasedFirst := false
	defer func() {
		if !releasedFirst {
			close(releaseFirst)
		}
	}()
	var program *tea.Program
	_, eng := newAppRuntimeEngine(
		t,
		&deferredFinalQueuedInjectionStreamClient{releaseFirst: releaseFirst, firstDelta: firstDelta},
		runtime.Config{
			Reviewer: runtime.ReviewerConfig{
				Frequency:     "all",
				Model:         "gpt-5",
				ThinkingLevel: "low",
				Client:        reviewerNoSuggestionsClient{},
			},
			OnEvent: func(evt runtime.Event) {
				if evt.Kind == runtime.EventAssistantDelta {
					return
				}
				if program != nil {
					program.Send(projectedRuntimeEventMsg(evt))
				}
			},
		},
	)
	eng.QueueUserMessage("steer now")

	out := newLockedBuffer()
	model := newProjectedTestUIModel(newUIRuntimeClient(eng), closedProjectedRuntimeEvents(), closedAskEvents())
	observed := newObservedUIModel(model)

	programHarness := startNativeProgram(t, observed, out)
	program = programHarness.program

	waitForSignal(t, roundTripTimeout, "program startup output", out.Started())
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	submitDone := make(chan error, 1)
	go func() {
		_, err := eng.SubmitUserMessage(context.Background(), "run task")
		submitDone <- err
	}()

	waitForSignal(t, roundTripTimeout, "first deferred final delta", firstDelta)
	program.Send(projectedRuntimeEventMsg(runtime.Event{Kind: runtime.EventAssistantDelta, AssistantDelta: "foreground done"}))
	observed.waitFor(t, roundTripTimeout, "live deferred final delta visible", func(snapshot observedUISnapshot) bool {
		return strings.Contains(snapshot.OngoingStreamingText, "foreground done")
	})
	program.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	observed.waitFor(t, roundTripTimeout, "detail mode active", func(snapshot observedUISnapshot) bool {
		return snapshot.Mode == tui.ModeDetail
	})
	close(releaseFirst)
	releasedFirst = true

	waitForSubmitResult(t, roundTripTimeout, submitDone)

	program.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	observed.waitFor(t, roundTripTimeout, "ongoing view keeps deferred final visible after detail exit", func(snapshot observedUISnapshot) bool {
		return snapshot.Mode == tui.ModeOngoing && strings.Contains(snapshot.OngoingSnapshot, "foreground done")
	})

	programHarness.QuitAndWait(2 * time.Second)
}

func TestNativeDeferredFinalWithQueuedInjectionSurvivesDetailRoundTripBeforeCommit(t *testing.T) {
	client := &runtimeControlFakeClient{}
	model := newProjectedTestUIModel(client, closedProjectedRuntimeEvents(), closedAskEvents())
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 32})
	model.pendingInjected = queuedUserMessagesForTest("steer now")
	model.input = "steer now"
	model.lockedInjectText = "steer now"
	model.lockedInjectID = "queue-test-0"
	model.setInputSubmitLocked(true)

	_ = model.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:     clientui.EventRunStateChanged,
		StepID:   "step-1",
		RunState: &clientui.RunState{Lifecycle: clientui.MustRunLifecycle(clientui.RunLifecycleRunning, clientui.RunModeTurn)},
	}, true).cmd
	_ = model.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:           clientui.EventAssistantDelta,
		StepID:         "step-1",
		AssistantDelta: "foreground done",
	}, true).cmd
	if got := model.view.OngoingStreamingText(); !strings.Contains(got, "foreground done") {
		t.Fatalf("expected live deferred final delta visible, got %q", got)
	}

	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyShiftTab})
	if model.view.Mode() != tui.ModeDetail {
		t.Fatalf("expected detail mode active, got %q", model.view.Mode())
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyShiftTab})
	if model.view.Mode() != tui.ModeOngoing {
		t.Fatalf("expected ongoing mode active before final commit, got %q", model.view.Mode())
	}

	_ = model.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                         clientui.EventUserMessageFlushed,
		StepID:                       "step-1",
		CommittedTranscriptChanged:   true,
		TranscriptRevision:           1,
		CommittedEntryCount:          1,
		UserMessage:                  "steer now",
		UserMessageBatch:             []string{"steer now"},
		UserMessageBatchQueueItemIDs: []string{"queue-test-0"},
		TranscriptEntries:            []clientui.ChatEntry{{Role: "user", Text: "steer now"}},
	}, true).cmd
	assistantCommitCmd := model.runtimeAdapter().applyProjectedRuntimeEvent(clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        1,
		CommittedEntryStartSet:     true,
		TranscriptRevision:         2,
		CommittedEntryCount:        2,
		TranscriptEntries:          []clientui.ChatEntry{{Role: "assistant", Text: "foreground done", Phase: string(llm.MessagePhaseFinal)}},
	}, true).cmd
	for _, msg := range collectCmdMessages(t, assistantCommitCmd) {
		if _, ok := msg.(runtimeTranscriptRefreshedMsg); ok {
			t.Fatalf("did not expect hydration after assistant caught up with deferred user flush, got %+v", msg)
		}
	}
	if got := len(model.deferredCommittedTail); got != 0 {
		t.Fatalf("expected deferred queued user flush merged by assistant commit, got %d", got)
	}
	if got := model.view.OngoingStreamingText(); got != "" {
		t.Fatalf("expected assistant commit to clear live stream, got %q", got)
	}
	if model.sawAssistantDelta {
		t.Fatal("expected assistant commit to clear assistant delta flag")
	}
	if got := stripANSIAndTrimRight(model.view.OngoingSnapshot()); !strings.Contains(got, "foreground done") {
		t.Fatalf("expected ongoing view to keep deferred final visible after early detail exit, got %q", got)
	}
}

func TestNativeQueuedSteerDuringBlockingToolAppearsInScrollback(t *testing.T) {
	runtimeEvents := make(chan runtime.Event, 256)
	blockingTool := &blockingShellTool{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	_, eng := newAppRuntimeEngine(
		t,
		&queuedSteerDuringBlockingToolClient{},
		runtime.Config{
			OnEvent: func(evt runtime.Event) {
				runtimeEvents <- evt
			},
		},
		tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: blockingTool},
	)

	out := newLockedBuffer()
	model := newProjectedTestUIModel(newUIRuntimeClient(eng), projectRuntimeEventChannel(runtimeEvents, nil, nil), closedAskEvents())
	observed := newObservedUIModel(model)

	program := startNativeProgram(t, observed, out)

	waitForSignal(t, 2*time.Second, "program startup output", out.Started())
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 32})
	submitDone := make(chan error, 1)
	go func() {
		_, err := eng.SubmitUserMessage(context.Background(), "run task")
		submitDone <- err
	}()

	select {
	case <-blockingTool.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocking tool to start")
	}
	eng.QueueUserMessage("steer now")
	close(blockingTool.release)

	waitForSubmitResult(t, 2*time.Second, submitDone)
	observed.waitFor(t, 2*time.Second, "queued steer and follow-up visible in ongoing scrollback", func(snapshot observedUISnapshot) bool {
		return containsInOrder(snapshot.OngoingSnapshot, "steer now", "after steer")
	})

	snapshot := eng.RecentTailTranscriptWindow(1 << 20).Snapshot
	hasQueuedUser := false
	for _, entry := range snapshot.Entries {
		if entry.Role == string(llm.RoleUser) && entry.Text == "steer now" {
			hasQueuedUser = true
			break
		}
	}
	if !hasQueuedUser {
		t.Fatalf("expected runtime transcript to contain queued steer, got %+v", snapshot.Entries)
	}
	program.QuitAndWait(2 * time.Second)
}
