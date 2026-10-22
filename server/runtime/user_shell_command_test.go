package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/internal/testharness/testsetup"
	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/toolspec"
	"core/shared/transcript"
)

func TestUnavailableShellToolsReturnPlaintextToTheModel(t *testing.T) {
	for _, toolID := range []toolspec.ID{toolspec.ToolExecCommand, toolspec.ToolWriteStdin} {
		t.Run(string(toolID), func(t *testing.T) {
			call := llm.ToolCall{ID: "unavailable-shell-call", Name: string(toolID), Input: mustJSON(map[string]any{})}
			client := &fakeClient{responses: []llm.Response{
				{Assistant: llm.Message{Role: llm.RoleAssistant}, ToolCalls: []llm.ToolCall{call}},
				finalTextResponse("done"),
			}}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
			if _, err := engine.SubmitUserMessage(t.Context(), t.Name()); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, request := range client.calls {
				for _, item := range request.Items {
					if item.Type != llm.ResponseItemTypeFunctionCallOutput || item.CallID == nil || *item.CallID != call.ID {
						continue
					}
					found = true
					var output string
					if err := json.Unmarshal(item.Output, &output); err != nil || output == "" {
						t.Fatalf("unavailable shell tool did not produce plaintext: output=%q err=%v", item.Output, err)
					}
				}
			}
			if !found {
				t.Fatal("model did not receive the unavailable shell result")
			}
		})
	}
}

func TestUserShellAcceptanceOwnsExecutionAcrossCallerCancellation(t *testing.T) {
	for _, acceptCommand := range []bool{false, true} {
		t.Run(strconv.FormatBool(acceptCommand), func(t *testing.T) {
			handler := &heldRuntimeShell{started: make(chan struct{}), release: make(chan struct{})}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, newTestToolRegistry(t,
				tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: handler}),
				Config{Model: "gpt-6-sol"})
			release := sync.OnceFunc(func() { close(handler.release) })
			t.Cleanup(release)
			caller, cancel := context.WithCancel(t.Context())
			defer cancel()
			accept := func(commit func() (bool, error)) (bool, error) {
				if !acceptCommand {
					cancel()
					return false, context.Cause(caller)
				}
				return commit()
			}
			done := make(chan error, 1)
			go func() {
				_, err := engine.SubmitUserShellCommandWithAcceptance(caller, "pwd", accept)
				done <- err
			}()
			if acceptCommand {
				pendingWorkTestWait(t, handler.started, "accepted shell execution")
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("caller cancellation = %v", err)
				}
			case <-time.After(runtimeTestSynchronizationTimeout):
				t.Fatal("caller cancellation did not release its wait")
			}
			release()
			waitEngineLifecycleTasks(t, engine)
			if engine.ActiveRun() != nil {
				t.Fatal("finished shell left an active runtime execution")
			}
			messages := 0
			for _, entry := range engine.ChatSnapshot().Entries {
				if entry.MessageType == llm.MessageTypeUserShellCommand {
					messages++
				}
			}
			if acceptCommand && messages != 1 || !acceptCommand && messages != 0 {
				t.Fatalf("accepted=%t shell messages=%d", acceptCommand, messages)
			}
			if !acceptCommand {
				select {
				case <-handler.started:
					t.Fatal("rejected command executed")
				default:
				}
			}
		})
	}
}

func TestUserShellRealProcessLifecycle(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	for _, mode := range []string{"foreground", "background", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			workdir := t.TempDir()
			minimumWait := time.Hour
			if mode == "background" {
				minimumWait = time.Millisecond
			}
			manager, err := shelltool.NewManager(t.TempDir(), shelltool.WithMinimumExecToBgTime(minimumWait))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := manager.Close(); err != nil {
					t.Error(err)
				}
			})
			store := mustCreateTestSession(t)
			handler := shelltool.NewExecCommandToolWithPostprocessor(workdir, 16_000, 200_000, manager, store.Meta().SessionID, postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
			engine := mustNewTestEngine(t, store, &fakeClient{}, newTestToolRegistry(t,
				tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: handler}), Config{Model: "gpt-6-sol"})
			command := "echo complete"
			if mode != "foreground" {
				command = "touch started; while [ ! -f release ]; do sleep 0.02; done; echo complete"
			}
			done := make(chan error, 1)
			go func() {
				_, err := engine.SubmitUserShellCommand(t.Context(), command)
				done <- err
			}()
			if mode != "foreground" {
				testsetup.RequireUntil(t, time.Now().Add(runtimeTestSynchronizationTimeout), time.Millisecond, func() bool {
					_, err := os.Stat(filepath.Join(workdir, "started"))
					return err == nil
				}, "shell process did not start")
			}
			if mode == "interrupted" {
				stopped, err := engine.TryInterruptActiveRun()
				if err != nil || !stopped {
					t.Fatalf("interrupt=%t err=%v", stopped, err)
				}
			}
			select {
			case err := <-done:
				if err != nil && !(mode == "interrupted" && errors.Is(err, context.Canceled)) {
					t.Fatal(err)
				}
			case <-time.After(runtimeTestSynchronizationTimeout):
				t.Fatal("shell operation did not finish")
			}
			if engine.ActiveRun() != nil {
				t.Fatal("shell operation left a stale active runtime execution")
			}
			if mode == "background" {
				processes := manager.List()
				if len(processes) != 1 || !processes[0].Running {
					t.Fatalf("background command lost its live process: %+v", processes)
				}
				if err := os.WriteFile(filepath.Join(workdir, "release"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				result, err := manager.WriteStdin(t.Context(), shelltool.WriteRequest{
					SessionID: processes[0].ID, YieldTime: 15 * time.Second, MaxOutputChars: 16_000,
				})
				if err != nil || result.Running || result.ExitCode == nil || *result.ExitCode != 0 {
					t.Fatalf("background result=%+v err=%v", result, err)
				}
			}
			testsetup.RequireUntil(t, time.Now().Add(runtimeTestSynchronizationTimeout), time.Millisecond,
				func() bool { return manager.Count() == 0 }, "shell command left a running process")
		})
	}
}

func TestUserShellCommandIsUserContextInNextModelRequest(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{finalTextResponse("done")}}
	command := "printf '<fixture>\\n'"
	output := "<fixture>\n"
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t, tools.HandlerRegistration{
		ID:      toolspec.ToolExecCommand,
		Handler: fakeTool{name: toolspec.ToolExecCommand, out: mustJSON(output)},
	}), Config{Model: "gpt-6-sol"})
	if _, err := engine.SubmitUserShellCommand(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 0 {
		t.Fatal("a user shell command must not invoke the model")
	}
	if _, err := engine.SubmitUserMessage(t.Context(), "describe the command result"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range requestMessages(client.calls[0]) {
		if len(message.ToolCalls) != 0 || message.Role == llm.RoleTool {
			t.Fatalf("user shell command fabricated model tool activity: %+v", message)
		}
		if message.Role == llm.RoleUser && strings.Contains(messageContent(message), command) {
			found = true
			if !strings.HasSuffix(messageContent(message), output) {
				t.Fatalf("user shell output was not preserved: %q", messageContent(message))
			}
		}
	}
	if !found {
		t.Fatal("next model request has no user-attributed shell command")
	}
}

func TestIdleShellAdmissionExecutesWithoutModelTurn(t *testing.T) {
	for _, lane := range []runtimeinput.PendingWorkLane{runtimeinput.PendingWorkLaneSteer, runtimeinput.PendingWorkLaneQueue} {
		t.Run(string(lane), func(t *testing.T) {
			client := &fakeClient{responses: []llm.Response{finalTextResponse("done")}}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t, tools.HandlerRegistration{
				ID:      toolspec.ToolExecCommand,
				Handler: fakeTool{name: toolspec.ToolExecCommand, out: mustJSON("shell output")},
			}), Config{Model: "gpt-6-sol"})
			command := runtimeinput.ShellCommand{Command: "echo ready"}
			item, err := engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), command, lane, nil)
			pendingWorkTestNoError(t, err)
			if item.Disposition != runtimeinput.InputAdmissionStartedShell {
				t.Fatalf("idle Shell disposition = %s", item.Disposition)
			}
			pendingWorkTestNoError(t, engine.WaitForScheduledQueuedUserWork(t.Context()))
			if len(client.calls) != 0 || len(pendingWorkTestSnapshot(t, engine).Items) != 0 {
				t.Fatal("Shell left pending work or started a model turn")
			}
			_, err = engine.SubmitUserMessage(t.Context(), "continue")
			pendingWorkTestNoError(t, err)
			found := 0
			for _, message := range requestMessages(client.calls[0]) {
				if message.MessageType != nil && *message.MessageType == llm.MessageTypeUserShellCommand {
					found++
					if message.CompactContent == nil || *message.CompactContent != command.Command {
						t.Fatalf("Shell attribution = %+v", message)
					}
				}
			}
			if found != 1 {
				t.Fatalf("Shell results in next request = %d", found)
			}
		})
	}
}

func TestQueueShellPrefixPrecedesNextModelMessage(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	client := &hookClient{response: finalTextResponse("done")}
	requests := 0
	client.beforeReturn = func() error {
		requests++
		if requests == 1 {
			close(started)
			<-release
		}
		return nil
	}
	commands := make(chan string, 4)
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t, tools.HandlerRegistration{
		ID: toolspec.ToolExecCommand, Handler: userShellTestTool{commands: commands},
	}), Config{Model: "gpt-6-sol"})
	done := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "start")
		done <- err
	}()
	pendingWorkTestWait(t, started, "initial model request")
	for _, command := range []string{"echo A", "echo B"} {
		_, err := engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), runtimeinput.ShellCommand{Command: command}, runtimeinput.PendingWorkLaneQueue, nil)
		pendingWorkTestNoError(t, err)
	}
	_, err := engine.QueueUserInput(t.Context(), plainQueuedUserInput("message C"))
	pendingWorkTestNoError(t, err)
	if len(commands) != 0 || len(pendingWorkTestSnapshot(t, engine).Items) != 3 {
		t.Fatal("Queue Shell executed before the Agent Turn ended")
	}
	close(release)
	pendingWorkTestNoError(t, <-done)
	pendingWorkTestNoError(t, engine.WaitForScheduledQueuedUserWork(t.Context()))
	if len(client.calls) != 2 || len(commands) != 2 {
		t.Fatalf("model calls=%d, Shell calls=%d", len(client.calls), len(commands))
	}
	for _, want := range []string{"echo A", "echo B"} {
		if got := <-commands; got != want {
			t.Fatalf("Shell command=%q, want %q", got, want)
		}
	}
	var ordered []string
	for _, message := range requestMessages(client.calls[1]) {
		if message.MessageType != nil && *message.MessageType == llm.MessageTypeUserShellCommand {
			ordered = append(ordered, *message.CompactContent)
		} else if message.Role == llm.RoleUser && messageContent(message) == "message C" {
			ordered = append(ordered, messageContent(message))
		}
	}
	if len(ordered) != 3 || ordered[0] != "echo A" || ordered[1] != "echo B" || ordered[2] != "message C" {
		t.Fatalf("next model input order = %v", ordered)
	}
}

type userShellTestTool struct {
	commands chan string
	run      func(context.Context, string) error
}

func (h userShellTestTool) Call(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		Command string `json:"cmd"`
	}
	if err := json.Unmarshal(call.Input, &input); err != nil {
		return tools.Result{}, err
	}
	h.commands <- input.Command
	if h.run != nil {
		if err := h.run(ctx, input.Command); err != nil {
			return tools.Result{}, err
		}
	}
	return tools.Result{CallID: call.ID, Name: call.Name, Output: mustJSON(input.Command)}, nil
}

func TestUserShellCommandSurvivesReopenWithoutBecomingARollbackTarget(t *testing.T) {
	store := mustCreateTestSession(t)
	engine := mustNewExecTestEngine(t, store, &fakeClient{}, Config{Model: "gpt-6-sol"})
	command := "echo fixture"
	if _, err := engine.SubmitUserShellCommand(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if store.Meta().FirstPromptPreview != "" {
		t.Fatal("user shell context must not become the Session's first prompt preview")
	}
	var savedText string
	for _, entry := range engine.ChatSnapshot().Entries {
		if entry.MessageType == llm.MessageTypeUserShellCommand {
			savedText = entry.Text
		}
	}
	if savedText == "" {
		t.Fatal("command did not appear in chat")
	}
	engine.Close()

	client := &fakeClient{responses: []llm.Response{finalTextResponse("done")}}
	reopened := mustNewExecTestEngine(t, store, client, Config{Model: "gpt-6-sol"})
	for _, entry := range reopened.ChatSnapshot().Entries {
		if entry.MessageType != llm.MessageTypeUserShellCommand {
			continue
		}
		if entry.Text != savedText || entry.CompactLabel != command || entry.RollbackTargetID != nil {
			t.Fatalf("reopened user shell command lost its content or attribution: %+v", entry)
		}
	}
	if _, err := reopened.SubmitUserMessage(t.Context(), "describe the result"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range requestMessages(client.calls[0]) {
		if message.Role == llm.RoleUser && messageContent(message) == savedText {
			found = true
		}
		if len(message.ToolCalls) > 0 || message.Role == llm.RoleTool {
			t.Fatalf("reopen fabricated shell tool activity: %+v", message)
		}
	}
	if !found {
		t.Fatal("reopened model request lost the user shell message")
	}
}

func TestUserShellCommandLiveDeliveryKeepsOutputCollapsed(t *testing.T) {
	store := mustCreateTestSession(t)
	var delivered []TranscriptCommittedRowFact
	engine := mustNewExecTestEngine(t, store, &fakeClient{}, Config{
		Model: "gpt-6-sol",
		OnEvent: func(event Event) {
			if event.Kind == EventConversationUpdated {
				delivered = append(delivered, TranscriptCommittedRowFactsFromEvent(event)...)
			}
		},
	})
	command := "echo fixture"
	if _, err := engine.SubmitUserShellCommand(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, fact := range delivered {
		if fact.User != nil {
			t.Fatal("live user shell output must not be delivered as an expanded chat bubble")
		}
		if fact.Notice == nil || fact.Notice.MessageType != llm.MessageTypeUserShellCommand {
			continue
		}
		found = true
		if fact.Visibility != transcript.EntryVisibilityOngoingCollapsed ||
			fact.Notice.CompactLabel != command || fact.Notice.DiagnosticDetail == "" ||
			fact.Notice.DiagnosticDetail == command {
			t.Fatalf("live shell notice lost separate command and full output: %+v", fact)
		}
	}
	if !found {
		t.Fatal("live delivery omitted the typed shell notice")
	}
}

func TestSubmitUserShellCommandPersistsErrorWithoutRegisteredHandler(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	client := &fakeClient{}
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), Config{Model: "gpt-6-sol"})

	result, err := engine.SubmitUserShellCommand(context.Background(), "pwd")
	if !errors.Is(err, errUnknownTool) {
		t.Fatalf("submit shell command error = %v, want unknown tool", err)
	}
	if result.CallID == "" ||
		result.Name != toolspec.ToolExecCommand ||
		!result.IsError {
		t.Fatalf("unknown shell handler result = %+v", result)
	}
	if len(client.calls) != 0 {
		t.Fatalf("unknown shell handler dispatched model calls = %d", len(client.calls))
	}

	window, err := mustMaterializeTestEventLog(t, store).ReadRecentRecords(8)
	if err != nil {
		t.Fatalf("read bounded shell command records: %v", err)
	}
	messages := 0
	for _, record := range window.Records {
		message, ok := mustSessionEventPayload(record).(session.MessageRecord)
		if !ok || message.MessageType == nil || *message.MessageType != session.MessageTypeUserShellCommand {
			continue
		}
		messages++
		if message.Role != session.MessageRoleUser || message.Content == nil ||
			!strings.HasSuffix(*message.Content, errUnknownTool.Error()) {
			t.Fatalf("persisted shell error is not user context: %+v", message)
		}
	}
	if messages != 1 {
		t.Fatalf("persisted shell error messages = %d, want one", messages)
	}
}
