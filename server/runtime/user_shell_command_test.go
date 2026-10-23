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
				_, err := engine.QueueShellInput(caller, runtimeids.NewQueueItemID(), runtimeinput.ShellCommand{Command: "pwd"}, runtimeinput.PendingWorkLaneSteer, accept)
				done <- err
			}()
			if acceptCommand {
				pendingWorkTestWait(t, handler.started, "accepted shell execution")
				cancel()
			}
			select {
			case err := <-done:
				if !acceptCommand && !errors.Is(err, context.Canceled) || acceptCommand && err != nil {
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
				err := executeTestShell(t.Context(), engine, command)
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
	if err := executeTestShell(t.Context(), engine, command); err != nil {
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

func TestBusyShellSteerRunsAtBoundaryAndQueueWaitsForTurn(t *testing.T) {
	started, release := make(chan int, 3), make(chan struct{})
	defer close(release)
	client := &hookClient{response: finalTextResponse("done")}
	requests := 0
	client.beforeReturn = func() error {
		requests++
		started <- requests
		<-release
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
	if request := <-started; request != 1 {
		t.Fatalf("initial request = %d", request)
	}
	_, err := engine.Steer(t.Context(), "before shell", nil)
	pendingWorkTestNoError(t, err)
	for _, lane := range []runtimeinput.PendingWorkLane{runtimeinput.PendingWorkLaneSteer, runtimeinput.PendingWorkLaneQueue} {
		_, err := engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), runtimeinput.ShellCommand{Command: string(lane)}, lane, nil)
		pendingWorkTestNoError(t, err)
	}
	release <- struct{}{}
	if request := <-started; request != 2 {
		t.Fatalf("continuation request = %d", request)
	}
	if len(commands) != 1 || <-commands != string(runtimeinput.PendingWorkLaneSteer) {
		t.Fatal("Steer did not execute alone at the Step Boundary")
	}
	work := pendingWorkTestSnapshot(t, engine)
	if len(work.Items) != 1 || work.Items[0].Lane != runtimeinput.PendingWorkLaneQueue {
		t.Fatalf("Queue Shell did not remain pending: %+v", work)
	}
	var ordered []string
	for _, message := range requestMessages(client.calls[1]) {
		if messageContent(message) == "before shell" {
			ordered = append(ordered, "text")
		} else if message.MessageType != nil && *message.MessageType == llm.MessageTypeUserShellCommand {
			ordered = append(ordered, "shell")
		}
	}
	if len(ordered) != 2 || ordered[0] != "text" || ordered[1] != "shell" {
		t.Fatalf("same-lane input order = %v", ordered)
	}
	release <- struct{}{}
	pendingWorkTestNoError(t, <-done)
	pendingWorkTestNoError(t, engine.WaitForScheduledQueuedUserWork(t.Context()))
	if len(commands) != 1 || <-commands != string(runtimeinput.PendingWorkLaneQueue) || len(client.calls) != 2 {
		t.Fatal("Queue Shell did not execute after the turn without another model request")
	}
}

func TestShellOutcomeSurvivesPreparationRebuild(t *testing.T) {
	preparation := &heldPromptPreparation{started: make(chan struct{}), release: make(chan struct{})}
	commands := make(chan string, 4)
	client := &fakeClient{responses: []llm.Response{finalTextResponse("done")}}
	engine := shellPreparationTestEngine(t, client, preparation, commands, Config{})
	release := pendingWorkTestHoldMaintenance(t, engine)
	text, err := engine.Steer(t.Context(), "before shell", nil)
	pendingWorkTestNoError(t, err)
	_, err = engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), runtimeinput.ShellCommand{Command: "echo once"}, runtimeinput.PendingWorkLaneSteer, nil)
	pendingWorkTestNoError(t, err)
	release()
	pendingWorkTestWait(t, preparation.started, "preparation after Shell execution")
	if len(commands) != 1 {
		t.Fatalf("Shell execution count before preparation = %d", len(commands))
	}
	pending := pendingWorkTestSnapshot(t, engine)
	if len(pending.Items) != 1 || pending.Items[0].ID.String() != text.ID {
		t.Fatalf("uncommitted text did not remain pending: %+v", pending)
	}
	_, err = engine.Steer(t.Context(), "rebuild input", nil)
	pendingWorkTestNoError(t, err)
	close(preparation.release)
	pendingWorkTestNoError(t, engine.WaitForScheduledQueuedUserWork(t.Context()))
	if len(commands) != 1 || len(client.calls) != 1 {
		t.Fatalf("Shell executions=%d model requests=%d", len(commands), len(client.calls))
	}
	var ordered []string
	for _, message := range requestMessages(client.calls[0]) {
		if message.MessageType != nil && *message.MessageType == llm.MessageTypeUserShellCommand {
			ordered = append(ordered, *message.CompactContent)
		} else if messageContent(message) == "before shell" || messageContent(message) == "rebuild input" {
			ordered = append(ordered, messageContent(message))
		}
	}
	if len(ordered) != 3 || ordered[0] != "before shell" || ordered[1] != "echo once" || ordered[2] != "rebuild input" {
		t.Fatalf("rebuilt input order = %v", ordered)
	}
}

func TestShellAdmittedDuringBoundaryExecutionIsProcessedBeforePreparation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	commands := make(chan string, 4)
	client := &fakeClient{responses: []llm.Response{finalTextResponse("done")}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t, tools.HandlerRegistration{
		ID: toolspec.ToolExecCommand,
		Handler: userShellTestTool{commands: commands, run: func(ctx context.Context, command string) error {
			if command == "A" {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}},
	}), Config{Model: "gpt-6-sol"})
	resume := pendingWorkTestHoldMaintenance(t, engine)
	_, err := engine.Steer(t.Context(), "start", nil)
	pendingWorkTestNoError(t, err)
	_, err = engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), runtimeinput.ShellCommand{Command: "A"}, runtimeinput.PendingWorkLaneSteer, nil)
	pendingWorkTestNoError(t, err)
	resume()
	pendingWorkTestWait(t, started, "Shell A")
	_, err = engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), runtimeinput.ShellCommand{Command: "B"}, runtimeinput.PendingWorkLaneSteer, nil)
	pendingWorkTestNoError(t, err)
	releaseOnce()
	pendingWorkTestNoError(t, engine.WaitForScheduledQueuedUserWork(t.Context()))
	if len(commands) != 2 || <-commands != "A" || <-commands != "B" || len(client.calls) != 1 {
		t.Fatal("late Shell did not execute once before provider progress")
	}
	var outputs []string
	for _, message := range requestMessages(client.calls[0]) {
		if message.MessageType != nil && *message.MessageType == llm.MessageTypeUserShellCommand {
			outputs = append(outputs, *message.CompactContent)
		}
	}
	if len(outputs) != 2 || outputs[0] != "A" || outputs[1] != "B" {
		t.Fatalf("Shell context order = %v", outputs)
	}
}

func shellPreparationTestEngine(t *testing.T, client *fakeClient, preparation *heldPromptPreparation, commands chan string, config Config) *Engine {
	t.Helper()
	store := mustCreateTestSession(t)
	seed := mustNewTestEngine(t, store, &fakeClient{responses: []llm.Response{finalTextResponse("seed")}}, tools.NewRegistry(), Config{})
	_, err := seed.SubmitUserMessage(t.Context(), "seed")
	pendingWorkTestNoError(t, err)
	pendingWorkTestNoError(t, seed.Close())
	_, err = store.RefreshLockedMainPromptSnapshot(session.LockedMainPromptSnapshot{})
	pendingWorkTestNoError(t, err)
	config.PromptFacingSnapshotReloader = preparation
	return mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, newTestToolRegistry(t, tools.HandlerRegistration{
		ID: toolspec.ToolExecCommand, Handler: userShellTestTool{commands: commands},
	}), config)
}

func TestCompletedShellResultSettlesWithoutPreparedRequest(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(strconv.FormatBool(stop), func(t *testing.T) {
			preparation := &heldPromptPreparation{started: make(chan struct{}), release: make(chan struct{})}
			if !stop {
				preparation.err = errors.New("request preparation failed")
			}
			commands := make(chan string, 4)
			client := &fakeClient{responses: []llm.Response{finalTextResponse("done")}}
			restored := make(chan InterruptedHumanInput, 4)
			engine := shellPreparationTestEngine(t, client, preparation, commands, Config{
				OnEvent: func(event Event) {
					if event.HumanInputInterrupted != nil {
						for _, item := range event.HumanInputInterrupted.Items {
							restored <- item
						}
					}
					if status := event.QueuedUserMessageStatus; status != nil && status.Status == QueuedUserMessageFailed && status.Text != "" {
						restored <- InterruptedHumanInput{QueueItemID: status.QueueItemID, Text: status.Text}
					}
				},
			})
			release := pendingWorkTestHoldMaintenance(t, engine)
			text, err := engine.Steer(t.Context(), "uncommitted text", nil)
			pendingWorkTestNoError(t, err)
			command := runtimeinput.ShellCommand{Command: "echo completed"}
			_, err = engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), command, runtimeinput.PendingWorkLaneSteer, nil)
			pendingWorkTestNoError(t, err)
			release()
			pendingWorkTestWait(t, preparation.started, "preparation after completed command")
			if len(commands) != 1 {
				t.Fatal("Shell did not finish before preparation")
			}
			if stop {
				interrupted, err := engine.TryInterruptActiveRun()
				if err != nil || !interrupted {
					t.Fatalf("Stop = %v, %v", interrupted, err)
				}
			}
			close(preparation.release)
			waitEngineLifecycleTasks(t, engine)
			if len(client.calls) != 0 || len(commands) != 1 {
				t.Fatal("terminal preparation replayed Shell or called the model")
			}
			results := 0
			for _, entry := range engine.ChatSnapshot().Entries {
				if entry.MessageType == llm.MessageTypeUserShellCommand {
					results++
				}
			}
			if results != 1 {
				t.Fatalf("completed Shell results in Session = %d", results)
			}
			work := pendingWorkTestSnapshot(t, engine)
			if stop {
				if len(work.Items) != 0 || len(restored) != 1 || (<-restored).QueueItemID != text.ID {
					t.Fatal("Stop did not restore only the uncommitted text")
				}
			} else if len(work.Items) != 0 || len(restored) != 1 || (<-restored).QueueItemID != text.ID {
				t.Fatalf("ordinary text failure restoration: pending=%+v restored=%d", work, len(restored))
			}
		})
	}
}

func TestShellResultPersistenceFailureDoesNotRestoreExecutedCommand(t *testing.T) {
	preparation := &heldPromptPreparation{started: make(chan struct{}), release: make(chan struct{})}
	commands := make(chan string, 4)
	restored := make(chan string, 4)
	client := &fakeClient{}
	engine := shellPreparationTestEngine(t, client, preparation, commands, Config{
		OnEvent: func(event Event) {
			if event.PendingWorkRestoration != nil {
				restored <- event.PendingWorkRestoration.CanonicalInput
			}
		},
	})
	release := pendingWorkTestHoldMaintenance(t, engine)
	_, err := engine.Steer(t.Context(), "uncommitted", nil)
	pendingWorkTestNoError(t, err)
	_, err = engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), runtimeinput.ShellCommand{Command: "echo once"}, runtimeinput.PendingWorkLaneSteer, nil)
	pendingWorkTestNoError(t, err)
	release()
	pendingWorkTestWait(t, preparation.started, "preparation after execution")
	mustBlockTestEventLogAppends(t, engine.store)
	close(preparation.release)
	waitEngineLifecycleTasks(t, engine)
	if len(commands) != 1 || len(client.calls) != 0 {
		t.Fatal("failed result persistence replayed execution")
	}
	if len(restored) != 1 || <-restored != "uncommitted" {
		t.Fatal("failed persistence restored an executed Shell command")
	}
	_, diagnostic, _ := engine.transcriptRuntimeState().StreamingSnapshot()
	if diagnostic == "" {
		t.Fatal("persistence failure omitted diagnostics")
	}
}

func TestRejectedShellAdmissionDoesNotExecute(t *testing.T) {
	engine := pendingWorkTestEngine(t, Config{})
	_, err := engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(),
		runtimeinput.ShellCommand{Command: "echo rejected"}, runtimeinput.PendingWorkLaneQueue,
		func(func() (bool, error)) (bool, error) { return false, nil })
	if !errors.Is(err, context.Canceled) || len(pendingWorkTestSnapshot(t, engine).Items) != 0 || engine.HasScheduledQueuedUserWork() {
		t.Fatalf("rejected Shell admission = %v", err)
	}
}

func TestStopRestoresUnstartedShell(t *testing.T) {
	client := newBlockingThenQueuedClient()
	restored := make(chan InterruptedHumanInput, 4)
	commands := make(chan string, 4)
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t, tools.HandlerRegistration{
		ID: toolspec.ToolExecCommand, Handler: userShellTestTool{commands: commands},
	}), Config{OnEvent: func(event Event) {
		if event.HumanInputInterrupted != nil {
			for _, input := range event.HumanInputInterrupted.Items {
				restored <- input
			}
		}
	}})
	done := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "start")
		done <- err
	}()
	pendingWorkTestWait(t, client.started, "active turn")
	command := runtimeinput.ShellCommand{Command: "echo unstarted"}
	item, err := engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), command, runtimeinput.PendingWorkLaneQueue, nil)
	pendingWorkTestNoError(t, err)
	stopped, err := engine.TryInterruptActiveRun()
	pendingWorkTestNoError(t, err)
	close(client.releaseC)
	if !stopped || !errors.Is(<-done, context.Canceled) {
		t.Fatal("Stop did not interrupt the active turn")
	}
	waitEngineLifecycleTasks(t, engine)
	if len(commands) != 0 || len(restored) != 1 {
		t.Fatalf("Shell commands=%d restored=%d", len(commands), len(restored))
	}
	input := <-restored
	if input.QueueItemID != item.ID || input.Text != command.CanonicalInput() {
		t.Fatalf("restored Shell = %+v", input)
	}
	if len(pendingWorkTestSnapshot(t, engine).Items) != 0 {
		t.Fatal("Stop left the unstarted command eligible")
	}
}

func TestNonzeroShellResultIsRecordedWithoutReplay(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	workdir := t.TempDir()
	manager, err := shelltool.NewManager(t.TempDir(), shelltool.WithMinimumExecToBgTime(time.Hour))
	pendingWorkTestNoError(t, err)
	t.Cleanup(func() { pendingWorkTestNoError(t, manager.Close()) })
	store := mustCreateTestSession(t)
	handler := shelltool.NewExecCommandToolWithPostprocessor(workdir, 16_000, 200_000, manager, store.Meta().SessionID,
		postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	client := &fakeClient{responses: []llm.Response{finalTextResponse("done")}}
	restored := make(chan string, 4)
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t, tools.HandlerRegistration{
		ID: toolspec.ToolExecCommand, Handler: handler,
	}), Config{OnEvent: func(event Event) {
		if event.HumanInputInterrupted != nil || event.PendingWorkRestoration != nil {
			restored <- "restored"
		}
		if status := event.QueuedUserMessageStatus; status != nil && status.Status == QueuedUserMessageFailed && status.Text != "" {
			restored <- status.Text
		}
	}})
	command := runtimeinput.ShellCommand{Command: "echo executed >> count; exit 7"}
	_, err = engine.QueueShellInput(t.Context(), runtimeids.NewQueueItemID(), command, runtimeinput.PendingWorkLaneQueue, nil)
	pendingWorkTestNoError(t, err)
	pendingWorkTestNoError(t, engine.WaitForScheduledQueuedUserWork(t.Context()))
	if len(client.calls) != 0 || len(restored) != 0 {
		t.Fatal("nonzero exit started a model turn or restored executed input")
	}
	_, err = engine.SubmitUserMessage(t.Context(), "continue")
	pendingWorkTestNoError(t, err)
	content, err := os.ReadFile(filepath.Join(workdir, "count"))
	pendingWorkTestNoError(t, err)
	if string(content) != "executed\n" {
		t.Fatalf("command execution count changed: %q", content)
	}
	results := 0
	for _, message := range requestMessages(client.calls[0]) {
		if message.MessageType != nil && *message.MessageType == llm.MessageTypeUserShellCommand {
			results++
			if message.CompactContent == nil || *message.CompactContent != command.Command {
				t.Fatalf("nonzero Shell result attribution = %+v", message)
			}
		}
	}
	if results != 1 {
		t.Fatalf("nonzero Shell results = %d", results)
	}
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
	if err := executeTestShell(t.Context(), engine, command); err != nil {
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
	if err := executeTestShell(t.Context(), engine, command); err != nil {
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

	if err := executeTestShell(t.Context(), engine, "pwd"); !errors.Is(err, errUnknownTool) {
		t.Fatalf("shell error = %v, want unknown tool", err)
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

func executeTestShell(ctx context.Context, engine *Engine, command string) error {
	if _, err := engine.QueueShellInput(ctx, runtimeids.NewQueueItemID(), runtimeinput.ShellCommand{Command: command}, runtimeinput.PendingWorkLaneSteer, nil); err != nil {
		return err
	}
	return engine.WaitForScheduledQueuedUserWork(ctx)
}
