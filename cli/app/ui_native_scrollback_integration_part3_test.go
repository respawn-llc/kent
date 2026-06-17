package app

import (
	"bytes"
	"context"
	"core/cli/tui"
	"core/server/llm"
	"core/server/runtime"
	"core/server/tools"
	shelltool "core/server/tools/shell"
	"core/shared/clientui"
	"core/shared/toolspec"
	"core/shared/transcript"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestNativeNoopFinalNeverAppearsOnScreen(t *testing.T) {
	runtimeEvents := make(chan runtime.Event, 256)
	_, eng := newAppRuntimeEngine(
		t,
		noopFinalStreamClient{},
		runtime.Config{
			OnEvent: func(evt runtime.Event) {
				runtimeEvents <- evt
			},
		},
	)

	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(newUIRuntimeClient(eng), projectRuntimeEventChannel(runtimeEvents, nil, nil), closedAskEvents())

	program := startNativeProgram(t, model, out)

	time.Sleep(40 * time.Millisecond)
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
		for _, entry := range eng.ChatSnapshot().Entries {
			if strings.Contains(entry.Text, "NO_OP") {
				return false
			}
		}
		return true
	})
	waitForSubmitResult(t, 2*time.Second, submitDone)
	program.QuitAndWait(2 * time.Second)

	plain := xansi.Strip(out.String())
	if strings.Contains(plain, "NO_OP") {
		t.Fatalf("expected NO_OP to stay invisible in native ongoing output, got %q", plain)
	}
	if strings.TrimSpace(model.view.OngoingStreamingText()) != "" {
		t.Fatalf("expected live streaming buffer cleared after noop final, got %q", model.view.OngoingStreamingText())
	}
	if model.sawAssistantDelta {
		t.Fatal("expected sawAssistantDelta cleared after noop final")
	}
	for _, entry := range eng.ChatSnapshot().Entries {
		if strings.Contains(entry.Text, "NO_OP") {
			t.Fatalf("expected NO_OP to stay out of transcript entries, got %+v", eng.ChatSnapshot().Entries)
		}
	}
}

func TestNativeProgramKeepsPendingToolTailLiveOnlyUntilCompletion(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedTestUIModel(
		nil,
		closedProjectedRuntimeEvents(),
		closedAskEvents(),
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "user", Text: "prompt once"}}),
	)
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	time.Sleep(40 * time.Millisecond)
	baselineRaw := out.String()
	baselineNormalized := normalizedOutput(baselineRaw)
	if strings.Count(baselineNormalized, "prompt once") != 1 {
		t.Fatalf("expected prompt once in baseline startup output, got %q", baselineNormalized)
	}

	call := tui.TranscriptEntry{
		Role:       "tool_call",
		Text:       "pwd",
		ToolCallID: "call_1",
		ToolCall:   &transcript.ToolCallMeta{ToolName: "shell", IsShell: true, Command: "pwd"},
	}
	model.transcriptEntries = append(model.transcriptEntries, call)
	model.forwardToView(tui.SetConversationMsg{Entries: model.transcriptEntries})
	model.layout().syncViewport()
	if cmd := model.syncNativeHistoryFromTranscript(); cmd != nil {
		t.Fatalf("expected pending tool call not to flush committed history, got %T", cmd())
	}
	program.Send(spinnerTickMsg{})
	time.Sleep(40 * time.Millisecond)
	pendingDelta := out.String()[len(baselineRaw):]
	pendingNormalized := normalizedOutput(pendingDelta)
	if strings.Contains(pendingNormalized, "prompt once") {
		t.Fatalf("expected no prompt replay while tool call is pending, got %q", pendingNormalized)
	}

	result := tui.TranscriptEntry{Role: "tool_result_ok", Text: "/tmp", ToolCallID: "call_1"}
	model.transcriptEntries = append(model.transcriptEntries, result)
	model.forwardToView(tui.SetConversationMsg{Entries: model.transcriptEntries})
	model.layout().syncViewport()
	cmd := model.syncNativeHistoryFromTranscript()
	if cmd == nil {
		t.Fatal("expected finalized tool block flush")
	}
	program.Send(cmd())
	time.Sleep(40 * time.Millisecond)
	finalDelta := out.String()[len(baselineRaw)+len(pendingDelta):]
	finalNormalized := normalizedOutput(finalDelta)
	if strings.Contains(finalNormalized, "prompt once") {
		t.Fatalf("expected finalized flush without prompt replay, got %q", finalNormalized)
	}
	if strings.Count(finalNormalized, "pwd") != 1 {
		t.Fatalf("expected finalized tool call exactly once in append output, got %q", finalNormalized)
	}
	if strings.Contains(finalNormalized, "/tmp") {
		t.Fatalf("did not expect native ongoing scrollback to append shell output inline, got %q", finalNormalized)
	}

	program.QuitAndWait(2 * time.Second)
}

func TestNativeProgramRendersMixedRuntimeEventsFromChannelInRealtime(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 16)
	model := newProjectedTestUIModel(
		nil,
		runtimeEvents,
		closedAskEvents(),
		WithUIInitialTranscript([]UITranscriptEntry{{Role: "assistant", Text: "seed"}}),
	)
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "startup replay", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "seed")
	})

	callMeta := transcript.ToolCallMeta{ToolName: "shell", Command: "pwd", CompactText: "pwd", IsShell: true}
	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventRunStateChanged, RunState: &runtime.RunState{Lifecycle: runtime.RunningRunLifecycle(runtime.RunModeTurn)}})
	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventUserMessageFlushed, StepID: "step-1", UserMessage: "say hi"})
	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventLocalEntryAdded, StepID: "step-1", CommittedTranscriptChanged: true, CommittedEntryStart: 2, CommittedEntryStartSet: true, CommittedEntryCount: 3, LocalEntry: &runtime.ChatEntry{Role: "reviewer_status", Text: "Supervisor ran: 2 suggestions, applied."}})
	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventReviewerCompleted, StepID: "step-1", Reviewer: &runtime.ReviewerStatus{Outcome: "applied", SuggestionsCount: 2}})
	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventBackgroundUpdated, StepID: "step-1", Background: &runtime.BackgroundShellEvent{Type: "completed", ID: "1000", State: "completed", NoticeText: "Background shell 1000 completed.\nOutput:\nhello", CompactText: "Background shell 1000 completed"}})
	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventToolCallStarted, StepID: "step-1", ToolCall: &llm.ToolCall{ID: "call_1", Name: string(toolspec.ToolExecCommand), Presentation: transcript.EncodeToolCallMeta(callMeta)}})

	lastTranscript := ""
	lastNormalized := ""
	firstBatchDeadline := time.Now().Add(2 * time.Second)
	firstBatchReady := false
	for time.Now().Before(firstBatchDeadline) {
		transcriptText := strings.Builder{}
		for _, entry := range model.transcriptEntries {
			transcriptText.WriteString(entry.Text)
			transcriptText.WriteString("\n")
			if strings.TrimSpace(entry.OngoingText) != "" {
				transcriptText.WriteString(entry.OngoingText)
				transcriptText.WriteString("\n")
			}
		}
		lastTranscript = transcriptText.String()
		if !containsInOrder(lastTranscript, "say hi", "Supervisor ran", "Background shell 1000 completed") {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		lastNormalized = normalizedOutput(out.String())
		if strings.Contains(lastNormalized, "pwd") && strings.Contains(strings.ToLower(lastNormalized), "background shell 1000 completed") {
			firstBatchReady = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !firstBatchReady {
		lastNormalized = normalizedOutput(out.String())
		t.Fatalf(
			"expected mixed realtime terminal order after first batch, transcript=%q output=%q committed=%q nativeProjection=%q nativeRendered=%q",
			lastTranscript,
			lastNormalized,
			model.view.CommittedOngoingProjection().Render(tui.TranscriptDivider),
			model.nativeProjection.Render(tui.TranscriptDivider),
			model.nativeRenderedProjection.Render(tui.TranscriptDivider),
		)
	}

	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventToolCallCompleted, StepID: "step-1", ToolResult: &tools.Result{CallID: "call_1", Name: toolspec.ToolExecCommand, Output: []byte("/tmp")}})
	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventAssistantMessage, StepID: "step-1", Message: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal}})
	runtimeEvents <- projectRuntimeEvent(runtime.Event{Kind: runtime.EventRunStateChanged, RunState: &runtime.RunState{Lifecycle: runtime.IdleRunLifecycle()}})

	waitForTestCondition(t, 2*time.Second, "assistant completion after mixed realtime events", func() bool {
		normalized := normalizedOutput(out.String())
		return containsInOrder(normalized, "say hi", "Supervisor ran", "Background shell 1000 completed", "pwd", "done")
	})

	program.QuitAndWait(2 * time.Second)
	transcriptText := strings.Builder{}
	for _, entry := range model.transcriptEntries {
		transcriptText.WriteString(entry.Text)
		transcriptText.WriteString("\n")
		if strings.TrimSpace(entry.OngoingText) != "" {
			transcriptText.WriteString(entry.OngoingText)
			transcriptText.WriteString("\n")
		}
	}
	if !containsInOrder(transcriptText.String(), "say hi", "Supervisor ran", "Background shell 1000 completed", "pwd", "done") {
		t.Fatalf("expected mixed runtime event transcript sequence in projected transcript state, got %q", transcriptText.String())
	}
	if normalized := normalizedOutput(out.String()); !containsInOrder(normalized, "seed", "say hi", "Supervisor ran", "Background shell 1000 completed", "pwd", "done") {
		t.Fatalf("expected mixed runtime event terminal sequence, got %q", normalized)
	}
}

func TestNativeProgramDoesNotDuplicateSupervisorFollowUpAfterHydration(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 8)
	client := &staleTranscriptRuntimeClient{}
	client.sessionView = clientui.RuntimeSessionView{SessionID: "session-1"}
	client.transcript = clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     1,
		TotalEntries: 1,
		Entries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "seed",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}
	client.page = clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     3,
		TotalEntries: 3,
		Entries: []clientui.ChatEntry{
			{Role: "assistant", Text: "seed", Phase: string(llm.MessagePhaseFinal)},
			{Role: "assistant", Text: "follow-up final unique", Phase: string(llm.MessagePhaseFinal)},
			{Role: "reviewer_status", Text: "Supervisor ran: 2 suggestions, applied."},
		},
	}
	model := newProjectedTestUIModel(client, runtimeEvents, closedAskEvents())
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "startup replay", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "seed")
	})
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventAssistantMessage,
		CommittedTranscriptChanged: true,
		StepID:                     "step-1",
		TranscriptRevision:         2,
		CommittedEntryCount:        2,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:  "assistant",
			Text:  "follow-up final unique",
			Phase: string(llm.MessagePhaseFinal),
		}},
	}
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventLocalEntryAdded,
		CommittedTranscriptChanged: true,
		StepID:                     "step-1",
		TranscriptRevision:         3,
		CommittedEntryCount:        3,
		CommittedEntryStart:        2,
		CommittedEntryStartSet:     true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role: "reviewer_status",
			Text: "Supervisor ran: 2 suggestions, applied.",
		}},
	}
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		StepID:                     "step-1",
		CommittedTranscriptChanged: true,
		TranscriptRevision:         3,
		CommittedEntryCount:        3,
	}

	waitForTestCondition(t, 2*time.Second, "supervisor follow-up remains single and ordered", func() bool {
		normalized := normalizedOutput(out.String())
		return containsInOrder(normalized, "seed", "follow-up final unique", "Supervisor ran: 2 suggestions, applied.") &&
			strings.Count(normalized, "follow-up final unique") == 1 &&
			strings.Count(normalized, "Supervisor ran: 2 suggestions, applied.") == 1
	})

	program.QuitAndWait(2 * time.Second)

	if got := len(model.transcriptEntries); got != 3 {
		t.Fatalf("expected authoritative transcript tail without duplication, got %+v", model.transcriptEntries)
	}
	if got := model.transcriptEntries[1].Text; got != "follow-up final unique" {
		t.Fatalf("expected follow-up assistant before reviewer status, got %+v", model.transcriptEntries)
	}
	if got := model.transcriptEntries[2].Text; got != "Supervisor ran: 2 suggestions, applied." {
		t.Fatalf("expected reviewer status at transcript tail, got %+v", model.transcriptEntries)
	}
	if normalized := normalizedOutput(out.String()); strings.Count(normalized, "follow-up final unique") != 1 || strings.Count(normalized, "Supervisor ran: 2 suggestions, applied.") != 1 {
		t.Fatalf("expected follow-up assistant and reviewer status exactly once in terminal output, got %q", normalized)
	}
}

func TestNativeProgramDoesNotReemitOverlappedTailRowsWhenAuthoritativeTailSlides(t *testing.T) {
	out := &bytes.Buffer{}
	model := newProjectedStaticUIModel()
	model.startupCmds = nil
	model.transcriptEntries = []tui.TranscriptEntry{
		{Role: "assistant", Text: "seed"},
		{Role: "cache_warning", Visibility: transcript.EntryVisibilityAll, Text: "Cache miss: postfix-compatible supervisor cache reuse disappeared, -79k tokens"},
		{Role: "reviewer_suggestions", Text: "Supervisor suggested:\n1. Add verification notes.", OngoingText: "Supervisor suggested:\n1. Add verification notes."},
		{Role: "assistant", Text: "previous answer"},
	}
	model.forwardToView(tui.SetConversationMsg{Entries: model.transcriptEntries})
	model.runtimeTranscriptBusy = true
	model.runtimeTranscriptToken = 1
	model.transcriptRevision = 1
	model.transcriptBaseOffset = 0
	model.transcriptTotalEntries = 4

	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	waitForTestCondition(t, 2*time.Second, "startup replay", func() bool {
		return strings.Contains(normalizedOutput(out.String()), "previous answer")
	})

	program.Send(runtimeTranscriptRefreshedMsg{token: 1, transcript: clientui.TranscriptPage{
		SessionID:    "session-1",
		Revision:     2,
		Offset:       1,
		TotalEntries: 5,
		Entries: []clientui.ChatEntry{
			{Role: "cache_warning", Visibility: clientui.EntryVisibilityAll, Text: "Cache miss: postfix-compatible supervisor cache reuse disappeared, -79k tokens"},
			{Role: "reviewer_suggestions", Text: "Supervisor suggested:\n1. Add verification notes.", OngoingText: "Supervisor suggested:\n1. Add verification notes."},
			{Role: "assistant", Text: "previous answer"},
			{Role: "user", Text: "did you fix the actual transcript bugs, or only reporting/observability?"},
		},
	}})
	waitForTestCondition(t, 2*time.Second, "sliding authoritative tail appends only newest suffix", func() bool {
		normalized := normalizedOutput(out.String())
		return containsInOrder(normalized, "previous answer", "did you fix the actual transcript bugs, or only reporting/observability?")
	})

	program.QuitAndWait(2 * time.Second)

	normalized := normalizedOutput(out.String())
	if strings.Count(normalized, "Cache miss: postfix-compatible supervisor cache reuse disappeared, -79k tokens") != 1 {
		t.Fatalf("expected overlapped cache warning exactly once after sliding tail hydrate, got %q", normalized)
	}
	if strings.Count(normalized, "Supervisor suggested:") != 1 {
		t.Fatalf("expected overlapped reviewer suggestions exactly once after sliding tail hydrate, got %q", normalized)
	}
	if strings.Count(normalized, "did you fix the actual transcript bugs, or only reporting/observability?") != 1 {
		t.Fatalf("expected newest suffix exactly once after sliding tail hydrate, got %q", normalized)
	}
}

func TestNativeProgramRendersSingleBackgroundCompletionFromChannelWhileIdle(t *testing.T) {
	out := &bytes.Buffer{}
	runtimeEvents := make(chan clientui.Event, 4)
	model := newProjectedTestUIModel(
		nil,
		runtimeEvents,
		closedAskEvents(),
	)
	program := startNativeProgram(t, model, out)

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	runtimeEvents <- projectRuntimeEvent(runtime.Event{
		Kind: runtime.EventBackgroundUpdated,
		Background: &runtime.BackgroundShellEvent{
			Type:        "completed",
			ID:          "1000",
			State:       "completed",
			NoticeText:  "Background shell 1000 completed.\nOutput:\nhello",
			CompactText: "Background shell 1000 completed",
		},
	})

	waitForTestCondition(t, 2*time.Second, "single background completion projected into transcript state", func() bool {
		return len(model.transcriptEntries) == 1 && strings.Contains(model.transcriptEntries[0].Text, "Background shell 1000 completed")
	})
	waitForTestCondition(t, 2*time.Second, "single background completion rendered into native output", func() bool {
		return strings.Contains(strings.ToLower(normalizedOutput(out.String())), "background shell 1000 completed")
	})

	program.QuitAndWait(2 * time.Second)

	if normalized := normalizedOutput(out.String()); !containsInOrder(strings.ToLower(normalized), "background shell 1000 completed") {
		t.Fatalf("expected single background completion visible in terminal output, got %q", normalized)
	}
}

func TestNativeProgramRendersBackgroundCompletionFromEmbeddedRuntimeWhileIdle(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)

	server, err := startEmbeddedServer(context.Background(), Options{WorkspaceRoot: workspace}, readyMemoryAuthHandler(), false)
	if err != nil {
		t.Fatalf("start embedded server: %v", err)
	}
	defer func() { _ = server.Close() }()

	plan, runtimePlan := prepareAppRuntimePlan(t, server, sessionLaunchRequest{Mode: launchModeInteractive}, &bytes.Buffer{}, "test background completion while idle")
	defer runtimePlan.Close()

	out := &bytes.Buffer{}
	programCtx, cancelProgram := context.WithCancel(context.Background())
	defer cancelProgram()
	model := newProjectedTestUIModel(
		runtimePlan.Wiring.runtimeClient,
		runtimePlan.Wiring.runtimeEvents,
		runtimePlan.Wiring.askEvents,
	)
	program := startNativeProgram(t, model, out, tea.WithContext(programCtx))

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	server.inner.BackgroundRouter().Handle(shelltool.Event{
		Type: shelltool.EventCompleted,
		Snapshot: shelltool.Snapshot{
			ID:             "bg-1000",
			OwnerSessionID: plan.SessionID,
			State:          "completed",
			Command:        "sleep 1; printf done",
			Workdir:        workspace,
			LogPath:        "/tmp/bg-1000.log",
		},
		Preview: "done",
	})

	waitForTestCondition(t, 5*time.Second, "embedded background completion projected into transcript state", func() bool {
		for _, entry := range model.transcriptEntries {
			if strings.Contains(entry.Text, "Background shell bg-1000 completed") {
				return true
			}
		}
		return false
	})
	waitForTestCondition(t, 5*time.Second, "embedded background completion rendered into native output", func() bool {
		return strings.Contains(strings.ToLower(normalizedOutput(out.String())), "background shell bg-1000 completed")
	})

	cancelProgram()
	program.WaitAllowContextCanceled(2 * time.Second)

	if normalized := normalizedOutput(out.String()); !containsInOrder(strings.ToLower(normalized), "background shell bg-1000 completed") {
		t.Fatalf("expected embedded background completion visible in terminal output, got %q", normalized)
	}
}

func TestNativeProgramRendersBackgroundCompletionFromShellManagerWhileIdle(t *testing.T) {
	_, workspace := newRegisteredAppWorkspace(t)

	server, err := startEmbeddedServer(context.Background(), Options{WorkspaceRoot: workspace}, readyMemoryAuthHandler(), false)
	if err != nil {
		t.Fatalf("start embedded server: %v", err)
	}
	defer func() { _ = server.Close() }()

	plan, runtimePlan := prepareAppRuntimePlan(t, server, sessionLaunchRequest{Mode: launchModeInteractive}, &bytes.Buffer{}, "test shell-manager background completion while idle")
	defer runtimePlan.Close()

	manager := server.inner.Background()
	if manager == nil {
		t.Fatal("expected server background manager")
	}
	manager.SetMinimumExecToBgTime(25 * time.Millisecond)

	out := &bytes.Buffer{}
	programCtx, cancelProgram := context.WithCancel(context.Background())
	defer cancelProgram()
	model := newProjectedTestUIModel(
		runtimePlan.Wiring.runtimeClient,
		runtimePlan.Wiring.runtimeEvents,
		runtimePlan.Wiring.askEvents,
	)
	program := startNativeProgram(t, model, out, tea.WithContext(programCtx))

	time.Sleep(30 * time.Millisecond)
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	result, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Command:        []string{"sh", "-c", "sleep 0.05; printf done"},
		DisplayCommand: "bg-notify",
		OwnerSessionID: plan.SessionID,
		OwnerRunID:     "run-1",
		OwnerStepID:    "step-1",
		Workdir:        workspace,
		YieldTime:      25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start background process: %v", err)
	}
	if !result.Backgrounded {
		t.Fatalf("expected backgrounded process, got %+v", result)
	}
	want := "background shell " + result.SessionID + " completed"

	waitForTestCondition(t, 5*time.Second, "shell-manager background completion projected into transcript state", func() bool {
		for _, entry := range model.transcriptEntries {
			if strings.Contains(strings.ToLower(entry.Text), want) {
				return true
			}
		}
		return false
	})
	waitForTestCondition(t, 5*time.Second, "shell-manager background completion rendered into native output", func() bool {
		return strings.Contains(normalizedOutput(strings.ToLower(out.String())), want)
	})

	cancelProgram()
	program.WaitAllowContextCanceled(2 * time.Second)

	if normalized := normalizedOutput(strings.ToLower(out.String())); !containsInOrder(normalized, want) {
		t.Fatalf("expected shell-manager background completion visible in terminal output, got %q", normalized)
	}
}
