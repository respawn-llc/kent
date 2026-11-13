package shell

import (
	"context"
	"core/internal/testharness/postprocessfixture"
	"core/internal/testharness/testsetup"
	"core/server/tools"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	"core/shared/jsoncontract"
	"core/shared/sessionenv"
	"core/shared/toolspec"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func decodeStringToolOutput(t *testing.T, result tools.Result) string {
	t.Helper()
	var out string
	if err := json.Unmarshal(result.Output, &out); err != nil {
		t.Fatalf("decode string output: %v", err)
	}
	return out
}

func shellLogPathFromOutput(t *testing.T, output string) string {
	t.Helper()
	for _, field := range strings.Fields(output) {
		path := strings.TrimSuffix(field, ":")
		if strings.HasSuffix(path, ".log") {
			return path
		}
	}
	t.Fatalf("shell result did not include an output log path: %q", output)
	return ""
}

func assertShellError(t *testing.T, result tools.Result, call tools.Call, message string) {
	t.Helper()
	if !result.IsError || result.Terminal || result.CallID != call.ID || result.Name != call.Name {
		t.Fatalf("unexpected error result: %+v", result)
	}
	if output := decodeStringToolOutput(t, result); output != message {
		t.Fatalf("error output = %q, want %q", output, message)
	}
	if result.Summary == nil || *result.Summary != message {
		t.Fatalf("error summary = %v, want %q", result.Summary, message)
	}
}

type shellToolCaller interface {
	Call(context.Context, tools.Call) (tools.Result, error)
}

func callShellTestTool(t *testing.T, tool shellToolCaller, id string, name toolspec.ID, input map[string]any) tools.Result {
	t.Helper()
	rawInput, _ := json.Marshal(input)
	result, err := tool.Call(shellTestExecutionContext(t, context.Background(), id), tools.Call{ID: id, Name: name, Input: rawInput})
	if err != nil {
		t.Fatalf("%s call error: %v", name, err)
	}
	return result
}

func callExecCommand(t *testing.T, tool *ExecCommandTool, id string, input map[string]any) tools.Result {
	t.Helper()
	return callShellTestTool(t, tool, id, toolspec.ToolExecCommand, input)
}

func callWriteStdin(t *testing.T, tool *WriteStdinTool, id string, input map[string]any) tools.Result {
	t.Helper()
	return callShellTestTool(t, tool, id, toolspec.ToolWriteStdin, input)
}

func waitForManagerCount(t *testing.T, manager *Manager, want int, timeout time.Duration) {
	t.Helper()
	if testsetup.Until(time.Now().Add(timeout), 25*time.Millisecond, func() bool {
		return manager.Count() == want
	}) {
		return
	}
	t.Fatalf("manager count = %d, want %d", manager.Count(), want)
}

func waitForEntryInteraction(t *testing.T, manager *Manager, id string, timeout time.Duration) {
	t.Helper()
	entry, err := manager.entry(id)
	if err != nil {
		t.Fatalf("background entry %s: %v", id, err)
	}
	testsetup.RequireUntil(t, time.Now().Add(timeout), time.Millisecond, func() bool {
		if !entry.interactMu.TryLock() {
			return true
		}
		entry.interactMu.Unlock()
		return false
	}, "timed out waiting for write_stdin to start interacting with session %s", id)
}

func writeExecutableScript(t *testing.T, contents string) string {
	t.Helper()
	return testsetup.WriteExecutable(t, "hook.sh", contents)
}

func newBackgroundTestManager(t *testing.T) *Manager {
	t.Helper()
	return newShellTestManager(t, 250*time.Millisecond)
}

func newShellTestManager(t *testing.T, minimumExecToBackground time.Duration, options ...ManagerOption) *Manager {
	t.Helper()
	manager, err := NewManager(t.TempDir(), append([]ManagerOption{
		WithMinimumExecToBgTime(minimumExecToBackground),
		WithCloseTimeouts(20*time.Millisecond, 200*time.Millisecond),
	}, options...)...)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func TestExecCommandSilentSuccessIsTerminalAndUnambiguous(t *testing.T) {
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 20, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))

	result := callExecCommand(t, execTool, "silent-success", map[string]any{
		"cmd":           "printf '  \\n'",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 1_000,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	var rendered string
	if err := json.Unmarshal(result.Output, &rendered); err != nil {
		t.Fatalf("exec_command result must be a JSON string: %v", err)
	}
	if rendered == "" {
		t.Fatal("silent terminal completion must render non-empty content")
	}
	if !strings.Contains(rendered, "Shell 1000 exited with code 0 and no output") {
		t.Fatalf("silent completion does not identify the shell and numeric exit status: %q", rendered)
	}
	if result.PresentationDelta != nil && result.PresentationDelta.MovedToBackground {
		t.Fatalf("silent foreground completion must not be marked backgrounded: %+v", result.PresentationDelta)
	}
	waitForManagerCount(t, manager, 0, time.Second)
}

func TestExecCommandForegroundFailureIncludesShellAndExitStatus(t *testing.T) {
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))

	result := callExecCommand(t, execTool, "foreground-failure", map[string]any{
		"cmd":           "printf 'failed payload'; exit 7",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 1_000,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	rendered := decodeStringToolOutput(t, result)
	if !strings.Contains(rendered, "Shell 1000 exited with code 7") {
		t.Fatalf("failure output does not identify the shell and numeric exit status: %q", rendered)
	}
	if !strings.Contains(rendered, "failed payload") {
		t.Fatalf("failure output is missing command output: %q", rendered)
	}
}

func TestExecCommandSuccessfulForegroundOutputRemainsPlaintext(t *testing.T) {
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))

	result := callExecCommand(t, execTool, "foreground-success", map[string]any{
		"cmd":           "printf 'successful payload'",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 1_000,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	if rendered := decodeStringToolOutput(t, result); rendered != "successful payload" {
		t.Fatalf("successful foreground output = %q", rendered)
	}
}

func TestExecCommandSuccessfulForegroundTruncationIncludesPath(t *testing.T) {
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	content := strings.Repeat("x", 200)

	result := callExecCommand(t, execTool, "foreground-truncated-success", map[string]any{
		"cmd":               "printf '%s' '" + content + "'",
		"shell":             "/bin/sh",
		"login":             false,
		"yield_time_ms":     1_000,
		"max_output_tokens": 20,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	rendered := decodeStringToolOutput(t, result)
	if !strings.Contains(rendered, "output written to ") || !strings.Contains(rendered, ".log:\n") {
		t.Fatalf("truncated foreground output does not identify its log: %q", rendered)
	}
	if strings.Count(rendered, ".log") != 1 {
		t.Fatalf("truncated foreground output repeats its log path: %q", rendered)
	}
	if strings.Contains(rendered, "exited with code") {
		t.Fatalf("successful foreground output added an exit-code line: %q", rendered)
	}
	if result.PresentationDelta == nil || !result.PresentationDelta.OutputTruncated {
		t.Fatalf("truncated foreground presentation = %+v", result.PresentationDelta)
	}
	path := shellLogPathFromOutput(t, rendered)
	read := callExecCommand(t, execTool, "foreground-truncated-read", map[string]any{
		"cmd":           "cat '" + path + "'",
		"shell":         "/bin/sh",
		"login":         false,
		"raw":           true,
		"yield_time_ms": 1_000,
	})
	if read.IsError {
		t.Fatalf("read truncated foreground log: %s", string(read.Output))
	}
	if got := decodeStringToolOutput(t, read); got != content {
		t.Fatalf("foreground log content = %q, want the original command output", got)
	}
}

func TestExecCommandBackgroundTransitionIncludesDurationAndPathWithoutOutput(t *testing.T) {
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))

	result := callExecCommand(t, execTool, "background-transition", map[string]any{
		"cmd":           "printf 'early payload'; sleep 1",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 1,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	rendered := decodeStringToolOutput(t, result)
	const prefix = "Shell moved to background with ID 1000 after "
	if !strings.HasPrefix(rendered, prefix) {
		t.Fatalf("background transition does not report its shell and elapsed duration: %q", rendered)
	}
	duration := strings.TrimPrefix(rendered, prefix)
	duration = duration[:strings.IndexByte(duration, 's')]
	decimal := strings.IndexByte(duration, '.')
	if decimal < 0 || len(duration)-decimal-1 != 1 {
		t.Fatalf("background transition duration is not reported to one decimal place: %q", rendered)
	}
	if !strings.Contains(rendered, "Output file: ") || !strings.Contains(rendered, ".log") {
		t.Fatalf("background transition is missing the output-file path: %q", rendered)
	}
	if strings.Count(rendered, ".log") != 1 {
		t.Fatalf("background transition repeats its output-file path: %q", rendered)
	}
	if strings.Contains(rendered, "early payload") {
		t.Fatalf("background transition includes command output: %q", rendered)
	}
	if result.PresentationDelta == nil || !result.PresentationDelta.MovedToBackground {
		t.Fatalf("background transition presentation = %+v", result.PresentationDelta)
	}
}

func TestWriteStdinRunningPollReportsWallTimePathAndOutput(t *testing.T) {
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	stdinTool := NewWriteStdinTool(manager)

	start := callExecCommand(t, execTool, "poll-running-start", map[string]any{
		"cmd":           "read line; printf 'poll payload'; sleep 1",
		"shell":         "/bin/sh",
		"login":         false,
		"tty":           true,
		"yield_time_ms": 1,
	})
	if start.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(start.Output))
	}
	snapshot, err := manager.Snapshot("1000")
	if err != nil || !snapshot.Running {
		t.Fatalf("expected background shell to be running: %+v, %v", snapshot, err)
	}
	poll := callWriteStdin(t, stdinTool, "poll-running-output", map[string]any{
		"session_id":    1000,
		"chars":         "\n",
		"yield_time_ms": 250,
	})
	if poll.IsError {
		t.Fatalf("unexpected write_stdin error: %s", string(poll.Output))
	}
	rendered := decodeStringToolOutput(t, poll)
	if !strings.Contains(rendered, "Wall time:") || !strings.Contains(rendered, "Output file: "+snapshot.LogPath) {
		t.Fatalf("running poll is missing timing or log location: %q", rendered)
	}
	if strings.Count(rendered, snapshot.LogPath) != 1 {
		t.Fatalf("running poll repeats its log path: %q", rendered)
	}
	if !strings.Contains(rendered, "poll payload") {
		t.Fatalf("running poll is missing command output: %q", rendered)
	}
}

func TestWriteStdinPollReturnsRepeatableTailAfterNewOutput(t *testing.T) {
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	stdinTool := NewWriteStdinTool(manager)

	start := callExecCommand(t, execTool, "repeatable-tail-start", map[string]any{
		"cmd":           "printf 'first marker'; read line; printf 'second marker'; sleep 1",
		"shell":         "/bin/sh",
		"login":         false,
		"tty":           true,
		"yield_time_ms": 1,
	})
	if start.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(start.Output))
	}

	poll := func(id string, input map[string]any) string {
		t.Helper()
		result := callWriteStdin(t, stdinTool, id, input)
		if result.IsError {
			t.Fatalf("unexpected write_stdin error: %s", string(result.Output))
		}
		return decodeStringToolOutput(t, result)
	}
	first := poll("repeatable-tail-first", map[string]any{"session_id": 1000})
	second := poll("repeatable-tail-repeat", map[string]any{"session_id": 1000})
	if !strings.Contains(first, "first marker") || !strings.Contains(second, "first marker") {
		t.Fatalf("unchanged polls must return the available output tail: first=%q second=%q", first, second)
	}

	latest := poll("repeatable-tail-latest", map[string]any{
		"session_id":    1000,
		"chars":         "continue\n",
		"yield_time_ms": 250,
	})
	if !strings.Contains(latest, "first marker") || !strings.Contains(latest, "second marker") {
		t.Fatalf("poll after new output must return the latest accumulated tail: %q", latest)
	}
}

func TestWriteStdinPollUsesBoundedNewestOutputTail(t *testing.T) {
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	stdinTool := NewWriteStdinTool(manager)

	start := callExecCommand(t, execTool, "bounded-tail-start", map[string]any{
		"cmd":           "printf 'older marker\\n'; head -c 6000 /dev/zero | tr '\\000' x; printf '\\nnewer marker\\n'; sleep 1",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 1,
	})
	if start.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(start.Output))
	}
	poll := callWriteStdin(t, stdinTool, "bounded-tail-poll", map[string]any{"session_id": 1000})
	if poll.IsError {
		t.Fatalf("unexpected write_stdin error: %s", string(poll.Output))
	}
	rendered := decodeStringToolOutput(t, poll)
	if !strings.Contains(rendered, "newer marker") || strings.Contains(rendered, "older marker") {
		t.Fatalf("poll did not return only the newest bounded output tail: %q", rendered)
	}
	if poll.PresentationDelta == nil || !poll.PresentationDelta.OutputTruncated {
		t.Fatalf("bounded tail did not retain truncation metadata: %+v", poll.PresentationDelta)
	}
}

func TestWriteStdinCompletionPollUsesBoundedNewestOutputTail(t *testing.T) {
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	stdinTool := NewWriteStdinTool(manager)

	start := callExecCommand(t, execTool, "bounded-completion-start", map[string]any{
		"cmd":           "read line; printf 'older marker\\n'; head -c 6000 /dev/zero | tr '\\000' x; printf '\\nnewer marker\\n'",
		"shell":         "/bin/sh",
		"login":         false,
		"tty":           true,
		"yield_time_ms": 1,
	})
	if start.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(start.Output))
	}
	poll := callWriteStdin(t, stdinTool, "bounded-completion-poll", map[string]any{
		"session_id":    1000,
		"chars":         "continue\n",
		"yield_time_ms": 250,
	})
	if poll.IsError {
		t.Fatalf("unexpected write_stdin error: %s", string(poll.Output))
	}
	rendered := decodeStringToolOutput(t, poll)
	if !strings.Contains(rendered, "newer marker") || strings.Contains(rendered, "older marker") {
		t.Fatalf("completion poll did not return only the newest bounded output tail: %q", rendered)
	}
	if poll.CompletedBackgroundSessionID == nil || *poll.CompletedBackgroundSessionID != 1000 {
		t.Fatalf("completed poll lost its shell identity: %+v", poll)
	}
	if poll.PresentationDelta == nil || !poll.PresentationDelta.OutputTruncated {
		t.Fatalf("completed bounded tail did not retain truncation metadata: %+v", poll.PresentationDelta)
	}
}

func TestExecCommandReadsCompleteShellLogWhileRunningAndAfterCompletion(t *testing.T) {
	for _, test := range []struct {
		name    string
		raw     bool
		concise bool
	}{
		{name: "processed output"},
		{name: "raw concise output", raw: true, concise: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := newBackgroundTestManager(t)
			completedEvents := make(chan Event, 2)
			manager.SetEventHandler(func(event Event) bool {
				if event.Type == EventCompleted || event.Type == EventKilled {
					completedEvents <- event
				}
				return true
			})
			runner := postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin})
			execTool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 200_000, manager, "", runner)
			stdinTool := NewWriteStdinTool(manager)
			content := "\x1b[31mstart\x1b[0m\n" + strings.Repeat("0123456789", 600) + "\nend\n"

			start := callExecCommand(t, execTool, "log-retrieval-start", map[string]any{
				"cmd":           "printf '%s' '" + content + "'; sleep 3",
				"shell":         "/bin/sh",
				"login":         false,
				"raw":           test.raw,
				"yield_time_ms": 1,
			})
			if start.IsError {
				t.Fatalf("unexpected exec_command error: %s", string(start.Output))
			}
			snapshot, err := manager.Snapshot("1000")
			if err != nil || !snapshot.Running {
				t.Fatalf("expected background process to remain running: %+v, %v", snapshot, err)
			}
			startText := decodeStringToolOutput(t, start)
			returnedPath := shellLogPathFromOutput(t, startText)
			if returnedPath != snapshot.LogPath {
				t.Fatalf("background result path = %q, manager path = %q", returnedPath, snapshot.LogPath)
			}

			readRanges := func(path string) string {
				t.Helper()
				var read strings.Builder
				for offset := 0; offset < len(content); {
					count := min(2_048, len(content)-offset)
					command := fmt.Sprintf("dd if='%s' bs=1 skip=%d count=%d 2>/dev/null | base64 | tr -d '\\n'", path, offset, count)
					rangeResult := callExecCommand(t, execTool, fmt.Sprintf("log-range-%d", offset), map[string]any{
						"cmd":           command,
						"shell":         "/bin/sh",
						"login":         false,
						"raw":           true,
						"yield_time_ms": 1_000,
					})
					if rangeResult.IsError {
						t.Fatalf("read log range at %d: %s", offset, string(rangeResult.Output))
					}
					decoded, err := base64.StdEncoding.DecodeString(decodeStringToolOutput(t, rangeResult))
					if err != nil {
						t.Fatalf("decode shell-log range at %d: %v", offset, err)
					}
					read.Write(decoded)
					offset += count
				}
				return read.String()
			}
			if got := readRanges(returnedPath); got != content {
				offset := 0
				for offset < min(len(got), len(content)) && got[offset] == content[offset] {
					offset++
				}
				t.Fatalf("running shell log content differs from original output at byte %d: got %q, want %q (lengths %d and %d)", offset, got[max(0, offset-8):min(len(got), offset+8)], content[max(0, offset-8):min(len(content), offset+8)], len(got), len(content))
			}

			poll := callWriteStdin(t, stdinTool, "log-retrieval-completion", map[string]any{
				"session_id":    1000,
				"yield_time_ms": 15_000,
			})
			if poll.IsError {
				t.Fatalf("unexpected write_stdin error: %s", string(poll.Output))
			}
			pollText := decodeStringToolOutput(t, poll)
			pollPath := shellLogPathFromOutput(t, pollText)
			if pollPath != returnedPath {
				t.Fatalf("completed poll path = %q, background path = %q", pollPath, returnedPath)
			}
			if strings.Contains(pollText, "start") {
				t.Fatalf("bounded poll unexpectedly includes output older than its tail: %q", pollText)
			}

			var event Event
			select {
			case event = <-completedEvents:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for background completion")
			}
			mode := BackgroundOutputDefault
			if test.concise {
				mode = BackgroundOutputConcise
			}
			summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{SuccessOutputMode: mode})
			if err != nil {
				t.Fatalf("SummarizeBackgroundEvent: %v", err)
			}
			if test.concise && !strings.Contains(summary.DetailText, summary.LogPath) {
				t.Fatalf("concise completion did not expose its log path: %q", summary.DetailText)
			}
			logPath := pollPath
			if test.concise {
				logPath = shellLogPathFromOutput(t, summary.DetailText)
			}
			if got := readRanges(logPath); got != content {
				offset := 0
				for offset < min(len(got), len(content)) && got[offset] == content[offset] {
					offset++
				}
				t.Fatalf("completed shell log content differs from original output at byte %d: got %q, want %q (lengths %d and %d)", offset, got[max(0, offset-8):min(len(got), offset+8)], content[max(0, offset-8):min(len(content), offset+8)], len(got), len(content))
			}
		})
	}
}

func TestWriteStdinPublishedContractDoesNotExposeOutputSize(t *testing.T) {
	manager := newBackgroundTestManager(t)
	contracts, err := tools.NewStaticToolContracts(
		jsoncontract.NewPreparer(false),
		WriteStdinStaticContractSource(),
	)
	if err != nil {
		t.Fatalf("prepare static write_stdin contract: %v", err)
	}
	registry, err := tools.NewStaticToolRegistry(
		contracts,
		tools.HandlerRegistration{
			ID:      toolspec.ToolWriteStdin,
			Handler: NewWriteStdinTool(manager),
		},
	)
	if err != nil {
		t.Fatalf("prepare write_stdin registry: %v", err)
	}
	definitions := registry.Definitions()
	if len(definitions) != 1 {
		t.Fatalf("write_stdin definition count = %d, want one", len(definitions))
	}
	for _, field := range definitions[0].Schema.Fields() {
		if field == "max_output_tokens" {
			t.Fatal("write_stdin schema still publishes max_output_tokens")
		}
	}
	for _, name := range []string{"max_output_tokens", "max_tokens", "output_token_limit"} {
		if _, ok := toolspec.ResolveModelParameterName(toolspec.ToolWriteStdin, name); ok {
			t.Fatalf("write_stdin still normalizes the removed output-size parameter %q", name)
		}
	}
	if canonical, ok := toolspec.ResolveModelParameterName(toolspec.ToolExecCommand, "max_tokens"); !ok || canonical != "max_output_tokens" {
		t.Fatalf("exec_command max_tokens alias = %q, %t; want max_output_tokens, true", canonical, ok)
	}
	for _, name := range []string{"max_output_tokens", "max_tokens", "output_token_limit"} {
		input, err := registry.PrepareInput(toolspec.ToolWriteStdin, json.RawMessage(`{"session_id":1000,"`+name+`":32}`))
		if err != nil {
			t.Fatalf("prepare write_stdin input with %q: %v", name, err)
		}
		var canonical map[string]json.RawMessage
		if err := json.Unmarshal(input, &canonical); err != nil {
			t.Fatalf("decode prepared write_stdin input: %v", err)
		}
		if _, exists := canonical["max_output_tokens"]; exists {
			t.Fatalf("removed write_stdin output-size parameter %q was retained: %s", name, input)
		}
	}
}

func TestExecCommandEmptyDefaultWorkdirReturnsManagerErrorWithoutExecuting(t *testing.T) {
	serverCWD := t.TempDir()
	t.Chdir(serverCWD)
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor("", 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	call := tools.Call{
		ID:   "empty-default-workdir",
		Name: toolspec.ToolExecCommand,
	}
	input, err := json.Marshal(map[string]any{
		"cmd":           "touch exec-command-empty-default-side-effect",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 1_000,
	})
	if err != nil {
		t.Fatalf("marshal exec_command input: %v", err)
	}
	call.Input = input

	_, managerErr := manager.Start(context.Background(), ExecRequest{
		OwnerSessionShortID: testsetup.ShellLogSessionShortID(t),
		Postprocessor:       postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:             []string{"/bin/sh", "-c", "touch exec-command-empty-default-side-effect"},
		Workdir:             "",
	})
	if managerErr == nil {
		t.Fatal("expected empty workdir manager error")
	}
	got, err := execTool.Call(shellTestExecutionContext(t, context.Background(), call.ID), call)
	if err != nil {
		t.Fatalf("exec_command call returned transport error: %v", err)
	}
	assertShellError(t, got, call, managerErr.Error())
	if _, err := os.Stat(filepath.Join(serverCWD, "exec-command-empty-default-side-effect")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty default workdir command side effect = %v, want os.ErrNotExist", err)
	}
	waitForManagerCount(t, manager, 0, time.Second)
}

func TestExecCommandWorkdirValidationErrors(t *testing.T) {
	tests := []struct {
		name, id, workdir, reason string
		prepare                   func(*testing.T, string)
	}{
		{
			name: "deleted default", id: "deleted-default-workdir", reason: missingWorkingDirectoryReason,
			prepare: func(t *testing.T, workspace string) {
				if err := os.RemoveAll(workspace); err != nil {
					t.Fatalf("remove default workdir: %v", err)
				}
			},
		},
		{
			name: "missing relative", id: "missing-relative-workdir",
			workdir: filepath.Join("nested", "..", "missing", ".", "child"), reason: missingWorkingDirectoryReason,
		},
		{
			name: "regular file", id: "file-workdir", workdir: "workdir-file",
			reason: nonDirectoryWorkingDirectoryReason,
			prepare: func(t *testing.T, workspace string) {
				if err := os.WriteFile(filepath.Join(workspace, "workdir-file"), []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("write workdir file: %v", err)
				}
			},
		},
		{
			name: "file ancestor", id: "file-ancestor-workdir",
			workdir: filepath.Join("workdir-file", "child"), reason: missingWorkingDirectoryReason,
			prepare: func(t *testing.T, workspace string) {
				if err := os.WriteFile(filepath.Join(workspace, "workdir-file"), []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("write workdir file: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			resolved, err := filepath.Abs(ResolveWorkdir(workspace, tt.workdir))
			if err != nil {
				t.Fatalf("normalize workdir: %v", err)
			}
			if tt.prepare != nil {
				tt.prepare(t, workspace)
			}
			sideEffect := filepath.Join(t.TempDir(), "side-effect")
			input := map[string]any{
				"cmd": "touch " + sideEffect, "shell": "/bin/sh", "login": false, "yield_time_ms": 1_000,
			}
			if tt.workdir != "" {
				input["workdir"] = tt.workdir
			}
			rawInput, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("marshal exec_command input: %v", err)
			}
			call := tools.Call{ID: tt.id, Name: toolspec.ToolExecCommand, Input: rawInput}
			manager := newBackgroundTestManager(t)
			got, err := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin})).Call(shellTestExecutionContext(t, context.Background(), call.ID), call)
			if err != nil {
				t.Fatalf("exec_command call returned transport error: %v", err)
			}
			wantMessage := strings.Join([]string{resolved, tt.reason, existingWorkingDirectoryHint}, " ")
			assertShellError(t, got, call, wantMessage)
			if _, err := os.Stat(sideEffect); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("command side effect = %v, want os.ErrNotExist", err)
			}
			waitForManagerCount(t, manager, 0, time.Second)
		})
	}
}

func envSliceToMap(t *testing.T, in []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(in))
	for _, entry := range in {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			t.Fatalf("invalid env entry: %q", entry)
		}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate env key: %s", key)
		}
		out[key] = value
	}
	return out
}

func TestManagerStartEmbedsOwnerSessionIDInProcessEnv(t *testing.T) {
	manager := newBackgroundTestManager(t)
	result, err := manager.Start(context.Background(), ExecRequest{
		OwnerSessionShortID: testsetup.ShellLogSessionShortID(t),
		Postprocessor:       postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:             []string{"/bin/sh", "-c", "printf %s \"$" + sessionenv.SessionIDEnv + "\""},
		DisplayCommand:      "print kent session id",
		OwnerSessionID:      "session-env-123",
		Workdir:             t.TempDir(),
		YieldTime:           time.Second,
		MaxOutputChars:      1000,
	})
	if err != nil {
		t.Fatalf("start command: %v", err)
	}
	if result.Output != "session-env-123" {
		t.Fatalf("output = %q, want session-env-123", result.Output)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("exit code = %v, want 0", result.ExitCode)
	}
}

func TestEnrichEnvAddsManagedRGConfigPathWhenAvailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, _, err := config.EnsureManagedRGConfigFile(); err != nil {
		t.Fatalf("ensure managed rg config file: %v", err)
	}

	env := envSliceToMap(t, tools.EnrichShellEnvForSession([]string{"KEEP=1"}, ""))
	want := filepath.Join(home, config.ConfigDirName, "rg.conf")
	if env["RIPGREP_CONFIG_PATH"] != want {
		t.Fatalf("RIPGREP_CONFIG_PATH = %q, want %q", env["RIPGREP_CONFIG_PATH"], want)
	}
}

func TestEnrichEnvKeepsUserRIPGREPConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, _, err := config.EnsureManagedRGConfigFile(); err != nil {
		t.Fatalf("ensure managed rg config file: %v", err)
	}

	env := envSliceToMap(t, tools.EnrichShellEnvForSession([]string{"RIPGREP_CONFIG_PATH=/tmp/user-rg.conf"}, ""))
	if env["RIPGREP_CONFIG_PATH"] != "/tmp/user-rg.conf" {
		t.Fatalf("RIPGREP_CONFIG_PATH = %q, want /tmp/user-rg.conf", env["RIPGREP_CONFIG_PATH"])
	}
}

func TestManagerInlineOutputUsesRecentOutputBeforeLogFlush(t *testing.T) {
	manager := newBackgroundTestManager(t)
	workspace := t.TempDir()

	result, err := manager.Start(context.Background(), ExecRequest{
		OwnerSessionShortID: testsetup.ShellLogSessionShortID(t),
		Postprocessor:       postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:             []string{"sh", "-c", "printf 'inline-ready\\n'; sleep 1"},
		DisplayCommand:      "inline-recent",
		Workdir:             workspace,
		YieldTime:           250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !result.Backgrounded {
		t.Fatalf("expected backgrounded process, got %+v", result)
	}
	defer func() { _ = manager.Kill(result.SessionID) }()

	preview, _, err := manager.InlineOutput(result.SessionID, 1024)
	if err != nil {
		t.Fatalf("InlineOutput: %v", err)
	}
	if !strings.Contains(preview, "inline-ready") {
		t.Fatalf("expected recent output fallback, got %q", preview)
	}
}

func TestManagerInlineOutputTruncatesRecentOutputFallback(t *testing.T) {
	manager := newBackgroundTestManager(t)
	workspace := t.TempDir()

	result, err := manager.Start(context.Background(), ExecRequest{
		OwnerSessionShortID: testsetup.ShellLogSessionShortID(t),
		Postprocessor:       postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:             []string{"sh", "-c", "printf '%0500d\\n' 1; sleep 1"},
		DisplayCommand:      "inline-recent-truncated",
		Workdir:             workspace,
		YieldTime:           250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !result.Backgrounded {
		t.Fatalf("expected backgrounded process, got %+v", result)
	}
	defer func() { _ = manager.Kill(result.SessionID) }()

	preview, _, err := manager.InlineOutput(result.SessionID, 80)
	if err != nil {
		t.Fatalf("InlineOutput: %v", err)
	}
	if len(preview) > 200 {
		t.Fatalf("expected truncated recent output fallback, got len=%d preview=%q", len(preview), preview)
	}
}

func TestTruncateDoesNotDuplicateWholeOutputWhenShorterThanHeadTailWindow(t *testing.T) {
	in := strings.Repeat("x", 543)
	out, truncated, removed := truncateWithTemplate(in, 80, truncationBannerTemplate)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if removed <= 0 {
		t.Fatalf("expected positive removed bytes, got %d", removed)
	}
	if strings.Contains(out, "omitted -") {
		t.Fatalf("did not expect negative omitted bytes, got %q", out)
	}
	if strings.Count(out, in) > 0 {
		t.Fatalf("did not expect full input duplicated in output, got %q", out)
	}
	headLen, tailLen := truncationSegmentLengths(len(in), 80)
	wantMax := headLen + tailLen + len(fmt.Sprintf(truncationBannerTemplate, removed))
	if got := len(out); got > wantMax {
		t.Fatalf("expected bounded truncated output <= %d bytes, got %d", wantMax, got)
	}
	if len(out) >= len(in) {
		t.Fatalf("expected truncated output smaller than input, got out=%d in=%d", len(out), len(in))
	}
}

func TestWriteStdinPollingPreservesTerminalLifecycleForAllCompletionShapes(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		wantExitCode int
		yieldTimeMS  int
	}{
		{name: "zero silent", command: "sleep 0.15", wantExitCode: 0, yieldTimeMS: 15_000},
		{name: "zero with output", command: "sleep 0.15; printf visible", wantExitCode: 0, yieldTimeMS: 15_000},
		{name: "non-zero silent", command: "sleep 0.15; exit 7", wantExitCode: 7, yieldTimeMS: 15_000},
		{name: "non-zero with output", command: "sleep 0.15; printf visible; exit 7", wantExitCode: 7, yieldTimeMS: 15_000},
		{name: "maximum output poll wait", command: "sleep 0.15", wantExitCode: 0, yieldTimeMS: 86_400_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			manager := newShellTestManager(t, 50*time.Millisecond)
			execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
			pollTool := NewWriteStdinTool(manager)

			start := callExecCommand(t, execTool, "poll-start", map[string]any{
				"cmd":           tt.command,
				"shell":         "/bin/sh",
				"login":         false,
				"yield_time_ms": 50,
			})
			if start.IsError {
				t.Fatalf("unexpected exec_command error: %s", string(start.Output))
			}
			if start.PresentationDelta == nil || !start.PresentationDelta.MovedToBackground {
				t.Fatalf("expected background transition, got %+v", start.PresentationDelta)
			}
			snapshots := manager.List()
			if len(snapshots) != 1 {
				t.Fatalf("background snapshot count = %d, want 1", len(snapshots))
			}
			sessionID, err := strconv.Atoi(snapshots[0].ID)
			if err != nil {
				t.Fatalf("background session ID must be numeric: %v", err)
			}

			poll := callWriteStdin(t, pollTool, "poll-complete", map[string]any{
				"session_id":    sessionID,
				"yield_time_ms": tt.yieldTimeMS,
			})
			if poll.IsError {
				t.Fatalf("unexpected write_stdin error: %s", string(poll.Output))
			}
			if poll.CompletedBackgroundSessionID == nil || *poll.CompletedBackgroundSessionID != sessionID {
				t.Fatalf("completion provenance = %v, want session %d", poll.CompletedBackgroundSessionID, sessionID)
			}
			if poll.PresentationDelta == nil || poll.PresentationDelta.ShellExitCode == nil ||
				*poll.PresentationDelta.ShellExitCode != tt.wantExitCode {
				t.Fatalf("completion presentation = %+v, want exit code %d", poll.PresentationDelta, tt.wantExitCode)
			}
			if decodeStringToolOutput(t, poll) == "" {
				t.Fatal("terminal polling response must contain non-empty presentation")
			}
			waitForManagerCount(t, manager, 0, time.Second)
		})
	}
}

func TestWriteStdinNonpositiveOutputPollDoesNotWait(t *testing.T) {
	milliseconds := func(value int) *int { return &value }
	for _, test := range []struct {
		name          string
		yieldTimeMS   *int
		includeWaitMS bool
	}{
		{name: "negative", yieldTimeMS: milliseconds(-1), includeWaitMS: true},
		{name: "zero", yieldTimeMS: milliseconds(0), includeWaitMS: true},
		{name: "omitted"},
		{name: "null", includeWaitMS: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			manager := newShellTestManager(t, 50*time.Millisecond)
			execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
			pollTool := NewWriteStdinTool(manager)

			start := callExecCommand(t, execTool, "short-window-start", map[string]any{
				"cmd":           "sleep 3",
				"shell":         "/bin/sh",
				"login":         false,
				"yield_time_ms": 50,
			})
			if start.IsError {
				t.Fatalf("unexpected exec_command error: %s", string(start.Output))
			}
			snapshots := manager.List()
			if len(snapshots) != 1 {
				t.Fatalf("background snapshot count = %d, want 1", len(snapshots))
			}
			sessionID, err := strconv.Atoi(snapshots[0].ID)
			if err != nil {
				t.Fatalf("background session ID must be numeric: %v", err)
			}

			input := map[string]any{"session_id": sessionID}
			if test.includeWaitMS {
				input["yield_time_ms"] = test.yieldTimeMS
			}
			started := time.Now()
			poll := callWriteStdin(t, pollTool, "short-window-poll", input)
			elapsed := time.Since(started)
			if poll.IsError {
				t.Fatalf("unexpected write_stdin error: %s", string(poll.Output))
			}
			if elapsed >= 200*time.Millisecond {
				t.Fatalf("output-only poll waited for output: %s", elapsed)
			}
			if snapshot, err := manager.Snapshot(snapshots[0].ID); err != nil || !snapshot.Running {
				t.Fatalf("expected process to remain running: snapshot=%+v err=%v", snapshot, err)
			}
		})
	}
}

func TestWriteStdinRejectsShortTimedOutputPolls(t *testing.T) {
	workspace := t.TempDir()
	manager := newShellTestManager(t, 50*time.Millisecond)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	pollTool := NewWriteStdinTool(manager)

	start := callExecCommand(t, execTool, "short-poll-start", map[string]any{
		"cmd":           "sleep 0.15",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 50,
	})
	if start.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(start.Output))
	}

	for _, test := range []struct {
		name        string
		yieldTimeMS int
	}{
		{name: "one millisecond", yieldTimeMS: 1},
		{name: "250 milliseconds", yieldTimeMS: 250},
		{name: "one second", yieldTimeMS: 1_000},
		{name: "1,100 milliseconds", yieldTimeMS: 1_100},
		{name: "1,101 milliseconds", yieldTimeMS: 1_101},
		{name: "three seconds", yieldTimeMS: 3_000},
		{name: "below minimum wait", yieldTimeMS: 14_999},
		{name: "above maximum", yieldTimeMS: 86_400_001},
	} {
		input := map[string]any{
			"session_id":    1000,
			"yield_time_ms": test.yieldTimeMS,
		}
		rejected := callWriteStdin(t, pollTool, "short-poll-rejected", input)
		if !rejected.IsError {
			t.Fatalf("expected %s request to fail, got %+v", test.name, rejected)
		}
		message := decodeStringToolOutput(t, rejected)
		if message == "" {
			t.Fatal("expected rejected write_stdin error value")
		}
		if rejected.Summary == nil || *rejected.Summary == "" {
			t.Fatal("expected rejected write_stdin summary")
		}
		if *rejected.Summary != message {
			t.Fatalf("rejected summary = %q, want error value %q", *rejected.Summary, message)
		}
	}

	accepted := callWriteStdin(t, pollTool, "short-poll-accepted", map[string]any{
		"session_id": 1000,
	})
	if accepted.IsError {
		t.Fatalf("expected process to remain usable after rejection: %s", string(accepted.Output))
	}
	waitForManagerCount(t, manager, 0, time.Second)
}

func TestWriteStdinCancellationReportsActiveProcess(t *testing.T) {
	workspace := t.TempDir()
	manager := newBackgroundTestManager(t)
	pollTool := NewWriteStdinTool(manager)

	result, err := manager.Start(context.Background(), ExecRequest{
		OwnerSessionShortID: testsetup.ShellLogSessionShortID(t),
		Postprocessor:       postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:             []string{"sh", "-c", "sleep 2"},
		DisplayCommand:      "sleep 2",
		Workdir:             workspace,
		YieldTime:           250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start background process: %v", err)
	}
	if !result.Backgrounded {
		t.Fatalf("expected backgrounded process, got %+v", result)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan tools.Result, 1)
	sessionID, err := strconv.Atoi(result.SessionID)
	if err != nil {
		t.Fatalf("parse session id: %v", err)
	}
	pollInput, _ := json.Marshal(map[string]any{
		"session_id":    sessionID,
		"yield_time_ms": 15_000,
	})
	pollCall := tools.Call{ID: "cancel-poll", Name: toolspec.ToolWriteStdin, Input: pollInput}
	go func() {
		pollResult, err := pollTool.Call(ctx, pollCall)
		if err != nil {
			t.Errorf("write_stdin call returned transport error: %v", err)
		}
		done <- pollResult
	}()

	waitForEntryInteraction(t, manager, result.SessionID, time.Second)
	cancel()

	select {
	case pollResult := <-done:
		if !pollResult.IsError {
			t.Fatalf("expected write_stdin error result, got %+v", pollResult)
		}
		pollErr := &PollingCanceledError{SessionID: result.SessionID, Active: true}
		assertShellError(t, pollResult, pollCall, formatToolCallErrorDecoration("write_stdin", pollErr.Error()))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for canceled write_stdin")
	}
	if snapshot, err := manager.Snapshot(result.SessionID); err != nil || !snapshot.Running {
		t.Fatalf("expected process to remain active after polling cancellation, snapshot=%+v err=%v", snapshot, err)
	}
}

func TestExecCommandCancellationReturnsUndecoratedBaseError(t *testing.T) {
	workspace := t.TempDir()
	readyMarker := filepath.Join(workspace, "exec-command-cancellation-ready")
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	call := tools.Call{
		ID:   "exec-command-cancellation",
		Name: toolspec.ToolExecCommand,
	}
	var err error
	call.Input, err = json.Marshal(map[string]any{
		"cmd":           "touch " + readyMarker + "; while :; do :; done",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 15_000,
	})
	if err != nil {
		t.Fatalf("marshal exec_command input: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type callResult struct {
		result tools.Result
		err    error
	}
	done := make(chan callResult, 1)
	ctx = shellTestExecutionContext(t, ctx, call.ID)
	go func() {
		result, err := execTool.Call(ctx, call)
		done <- callResult{result: result, err: err}
	}()

	testsetup.RequireUntil(t, time.Now().Add(time.Second), time.Millisecond, func() bool {
		_, err := os.Stat(readyMarker)
		return err == nil
	}, "timed out waiting for exec_command readiness marker")
	cancel()

	select {
	case completed := <-done:
		if completed.err != nil {
			t.Fatalf("exec_command call returned transport error: %v", completed.err)
		}
		assertShellError(t, completed.result, call, canceledByUserMessage)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for canceled exec_command")
	}
	waitForManagerCount(t, manager, 0, time.Second)
}

func TestExecCommandReturnsClosedManagerErrorUnchanged(t *testing.T) {
	workspace := t.TempDir()
	manager := newBackgroundTestManager(t)
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	call := tools.Call{
		ID:   "closed-manager",
		Name: toolspec.ToolExecCommand,
	}
	input, err := json.Marshal(map[string]any{
		"cmd":           "true",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 1_000,
	})
	if err != nil {
		t.Fatalf("marshal exec_command input: %v", err)
	}
	call.Input = input
	_, managerErr := manager.Start(context.Background(), ExecRequest{
		OwnerSessionShortID: testsetup.ShellLogSessionShortID(t),
		Postprocessor:       postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:             []string{"/bin/sh", "-c", "true"},
		Workdir:             workspace,
	})
	if managerErr == nil {
		t.Fatal("expected closed manager error")
	}
	got, err := execTool.Call(shellTestExecutionContext(t, context.Background(), call.ID), call)
	if err != nil {
		t.Fatalf("exec_command call returned transport error: %v", err)
	}
	assertShellError(t, got, call, managerErr.Error())
}

func TestManagerWriteStdinCancellationPreservesContextCanceled(t *testing.T) {
	workspace := t.TempDir()
	manager := newBackgroundTestManager(t)

	result, err := manager.Start(context.Background(), ExecRequest{
		OwnerSessionShortID: testsetup.ShellLogSessionShortID(t),
		Postprocessor:       postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:             []string{"sh", "-c", "sleep 2"},
		DisplayCommand:      "sleep 2",
		Workdir:             workspace,
		YieldTime:           250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start background process: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = manager.WriteStdin(ctx, WriteRequest{SessionID: result.SessionID, YieldTime: 5 * time.Second})
	if err == nil {
		t.Fatal("expected canceled polling error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected errors.Is(..., context.Canceled), got %v", err)
	}
	var pollErr *PollingCanceledError
	if !errors.As(err, &pollErr) {
		t.Fatalf("expected PollingCanceledError, got %T %v", err, err)
	}
	if !pollErr.Active {
		t.Fatalf("expected active process metadata, got %+v", pollErr)
	}
}

func TestExecCommandReportsNonZeroExitCode(t *testing.T) {
	workspace := t.TempDir()
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))

	result := callExecCommand(t, execTool, "nonzero-1", map[string]any{
		"cmd":           "printf 'bad\\n'; exit 7",
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 1_000,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	var output string
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("exec_command result must be a JSON string: %v", err)
	}
	if output == "" {
		t.Fatal("non-zero completion with output must render non-empty content")
	}
	if result.PresentationDelta == nil ||
		result.PresentationDelta.ShellExitCode == nil ||
		*result.PresentationDelta.ShellExitCode != 7 {
		t.Fatalf("non-zero shell presentation delta = %+v, want typed exit code 7", result.PresentationDelta)
	}
}

func TestExecCommandClampsShortYieldTime(t *testing.T) {
	const commandDelay = 100 * time.Millisecond
	const clampedForegroundWindow = 2 * time.Second

	workspace := t.TempDir()
	manager, err := NewManager(t.TempDir(), WithMinimumExecToBgTime(clampedForegroundWindow))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))

	result := callExecCommand(t, execTool, "clamp-1", map[string]any{
		"cmd":           fmt.Sprintf("sleep %.1f; echo done", commandDelay.Seconds()),
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 20,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	if text := decodeStringToolOutput(t, result); !strings.Contains(text, "done") {
		t.Fatalf("expected command output, got %q", text)
	}
	if manager.Count() != 0 {
		t.Fatalf("manager count = %d, want 0", manager.Count())
	}
}

func TestNormalizeWriteYieldTimeDoesNotCapLongPolls(t *testing.T) {
	yieldTime := normalizeWriteYieldTime(5*time.Minute, defaultWriteYieldTime)
	if yieldTime != 5*time.Minute {
		t.Fatalf("yield time = %s, want %s", yieldTime, 5*time.Minute)
	}

	yieldTime = normalizeWriteYieldTime(100*time.Millisecond, defaultWriteYieldTime)
	if yieldTime != minWriteYieldTime {
		t.Fatalf("yield time = %s, want %s for short input", yieldTime, minWriteYieldTime)
	}

	yieldTime = normalizeWriteYieldTime(0, defaultWriteYieldTime)
	if yieldTime != defaultWriteYieldTime {
		t.Fatalf("yield time = %s, want %s for zero input", yieldTime, defaultWriteYieldTime)
	}
}

func TestWriteStdinPollHonorsRequestedDuration(t *testing.T) {
	workspace := t.TempDir()
	manager := newShellTestManager(t, 50*time.Millisecond)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	pollTool := NewWriteStdinTool(manager)

	result := callExecCommand(t, execTool, "poll-duration-exec", map[string]any{
		"cmd":           "read line; sleep 0.6",
		"shell":         "/bin/sh",
		"login":         false,
		"tty":           true,
		"yield_time_ms": 50,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}

	pollInput := map[string]any{
		"session_id":    1000,
		"chars":         "\n",
		"yield_time_ms": 300,
	}
	start := time.Now()
	pollResult := callWriteStdin(t, pollTool, "poll-duration-poll", pollInput)
	elapsed := time.Since(start)
	if pollResult.IsError {
		t.Fatalf("unexpected write_stdin error: %s", string(pollResult.Output))
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("poll returned too early: %s", elapsed)
	}
	if elapsed > time.Second {
		t.Fatalf("poll took too long: %s", elapsed)
	}

	if pollResult.CompletedBackgroundSessionID != nil {
		t.Fatalf("running session reported completed: %v", pollResult.CompletedBackgroundSessionID)
	}
	rendered := decodeStringToolOutput(t, pollResult)
	if rendered == "" {
		t.Fatal("running polling response must contain presentation")
	}
	if !strings.Contains(rendered, "Wall time:") || !strings.Contains(rendered, "Output file: ") {
		t.Fatalf("running polling response is missing timing or log location: %q", rendered)
	}
	if snapshot, err := manager.Snapshot("1000"); err != nil || !snapshot.Running {
		t.Fatalf("expected session to still be running: %+v, %v", snapshot, err)
	}
	waitForManagerCount(t, manager, 0, 2*time.Second)
}

func TestWriteStdinInputBypassesShortPollError(t *testing.T) {
	workspace := t.TempDir()
	manager := newShellTestManager(t, 50*time.Millisecond)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	stdinTool := NewWriteStdinTool(manager)

	start := callExecCommand(t, execTool, "input-short-poll-start", map[string]any{
		"cmd":           "IFS= read -r line && [ \"$line\" = continue ]",
		"shell":         "/bin/sh",
		"login":         false,
		"tty":           true,
		"yield_time_ms": 50,
	})
	if start.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(start.Output))
	}
	snapshots := manager.List()
	if len(snapshots) != 1 {
		t.Fatalf("background snapshot count = %d, want 1", len(snapshots))
	}
	sessionID, err := strconv.Atoi(snapshots[0].ID)
	if err != nil {
		t.Fatalf("background session ID must be numeric: %v", err)
	}
	snapshot, err := manager.Snapshot(snapshots[0].ID)
	if err != nil {
		t.Fatalf("background shell snapshot: %v", err)
	}

	result := callWriteStdin(t, stdinTool, "input-short-poll", map[string]any{
		"session_id":    sessionID,
		"chars":         "continue\n",
		"yield_time_ms": 1_101,
	})
	if result.IsError {
		t.Fatalf("unexpected write_stdin error: %s", string(result.Output))
	}
	if result.CompletedBackgroundSessionID == nil || *result.CompletedBackgroundSessionID != sessionID ||
		result.PresentationDelta == nil || result.PresentationDelta.ShellExitCode == nil ||
		*result.PresentationDelta.ShellExitCode != 0 {
		t.Fatalf("completed input result = %+v", result)
	}
	rendered := decodeStringToolOutput(t, result)
	if rendered == "" {
		t.Fatal("completed input response must contain presentation")
	}
	if !strings.Contains(rendered, fmt.Sprintf("Shell %s exited with code 0 and no output", snapshot.ID)) {
		t.Fatalf("completed polling response is missing the shell's exit status: %q", rendered)
	}
	if !strings.Contains(rendered, "Wall time:") || !strings.Contains(rendered, "Output file: "+snapshot.LogPath) {
		t.Fatalf("completed polling response is missing timing or log location: %q", rendered)
	}
	if strings.Count(rendered, snapshot.LogPath) != 1 {
		t.Fatalf("completed polling response repeats its log path: %q", rendered)
	}
	waitForManagerCount(t, manager, 0, time.Second)
}

func TestWriteStdinWhitespaceInputRemainsInputAtLongWaits(t *testing.T) {
	tests := []struct {
		name        string
		yieldTimeMS int
	}{
		{name: "above output poll maximum", yieldTimeMS: 86_400_001},
		{name: "maximum integer", yieldTimeMS: math.MaxInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			manager := newShellTestManager(t, 50*time.Millisecond)
			execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
			stdinTool := NewWriteStdinTool(manager)

			start := callExecCommand(t, execTool, "whitespace-input-start", map[string]any{
				"cmd":           "read line; sleep 0.6",
				"shell":         "/bin/sh",
				"login":         false,
				"tty":           true,
				"yield_time_ms": 50,
			})
			if start.IsError {
				t.Fatalf("unexpected exec_command error: %s", string(start.Output))
			}

			started := time.Now()
			result := callWriteStdin(t, stdinTool, "whitespace-input", map[string]any{
				"session_id":    1000,
				"chars":         "\n",
				"yield_time_ms": tt.yieldTimeMS,
			})
			elapsed := time.Since(started)
			if result.IsError {
				t.Fatalf("unexpected write_stdin error: %s", string(result.Output))
			}
			if elapsed < 500*time.Millisecond {
				t.Fatalf("write_stdin returned before post-input delay: %s", elapsed)
			}
			if elapsed > 2*time.Second {
				t.Fatalf("write_stdin took too long: %s", elapsed)
			}
			if result.CompletedBackgroundSessionID == nil || *result.CompletedBackgroundSessionID != 1000 ||
				result.PresentationDelta == nil || result.PresentationDelta.ShellExitCode == nil ||
				*result.PresentationDelta.ShellExitCode != 0 {
				t.Fatalf("completed whitespace input result = %+v", result)
			}
			decodeStringToolOutput(t, result)
			waitForManagerCount(t, manager, 0, time.Second)
		})
	}
}

func TestExecCommandForegroundTruncationSetsPresentationMetadata(t *testing.T) {
	workspace := t.TempDir()
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))

	result := callExecCommand(t, execTool, "fg-trunc-1", map[string]any{
		"cmd":               "i=0; while [ $i -lt 400 ]; do printf x; i=$((i+1)); done",
		"shell":             "/bin/sh",
		"login":             false,
		"yield_time_ms":     2_000,
		"max_output_tokens": 10,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	if result.PresentationDelta == nil || !result.PresentationDelta.OutputTruncated {
		t.Fatalf("expected foreground truncation presentation delta, got %+v", result.PresentationDelta)
	}
	if manager.Count() != 0 {
		t.Fatalf("manager count = %d, want 0", manager.Count())
	}
}

func TestExecCommandRawOutputAddsPresentationMetadata(t *testing.T) {
	workspace := t.TempDir()
	manager := newBackgroundTestManager(t)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))

	result := callExecCommand(t, execTool, "raw-presentation-1", map[string]any{
		"cmd":           "printf raw",
		"shell":         "/bin/sh",
		"login":         false,
		"raw":           true,
		"yield_time_ms": 2_000,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	if result.PresentationDelta == nil || !result.PresentationDelta.RawOutputRequested || result.PresentationDelta.OutputTruncated {
		t.Fatalf("expected raw output presentation delta without truncation, got %+v", result.PresentationDelta)
	}
}

func TestWriteStdinRawSessionAddsPresentationMetadata(t *testing.T) {
	workspace := t.TempDir()
	manager := newShellTestManager(t, 50*time.Millisecond)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	stdinTool := NewWriteStdinTool(manager)

	result := callExecCommand(t, execTool, "raw-tty-1", map[string]any{
		"cmd":           "read line; printf '\\033[31m%s\\033[0m' \"$line\"",
		"shell":         "/bin/sh",
		"login":         false,
		"raw":           true,
		"tty":           true,
		"yield_time_ms": 50,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}

	stdinResult := callWriteStdin(t, stdinTool, "raw-tty-2", map[string]any{
		"session_id":    1000,
		"chars":         "raw app\n",
		"yield_time_ms": 2_000,
	})
	if stdinResult.IsError {
		t.Fatalf("unexpected write_stdin error: %s", string(stdinResult.Output))
	}
	if stdinResult.PresentationDelta == nil || !stdinResult.PresentationDelta.RawOutputRequested || stdinResult.PresentationDelta.OutputTruncated {
		t.Fatalf("expected raw write_stdin presentation delta without truncation, got %+v", stdinResult.PresentationDelta)
	}
	waitForManagerCount(t, manager, 0, time.Second)
}

func TestWriteStdinSendsInputToInteractiveProcess(t *testing.T) {
	workspace := t.TempDir()
	manager := newShellTestManager(t, 50*time.Millisecond)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	stdinTool := NewWriteStdinTool(manager)

	result := callExecCommand(t, execTool, "tty-1", map[string]any{
		"cmd":           "read line; echo $line",
		"shell":         "/bin/sh",
		"login":         false,
		"tty":           true,
		"yield_time_ms": 50,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}
	if manager.Count() != 1 {
		t.Fatalf("manager count = %d, want 1", manager.Count())
	}

	stdinResult := callWriteStdin(t, stdinTool, "tty-2", map[string]any{
		"session_id":    1000,
		"chars":         "hello app\n",
		"yield_time_ms": 86_400_001,
	})
	if stdinResult.IsError {
		t.Fatalf("unexpected write_stdin error: %s", string(stdinResult.Output))
	}
	if stdinResult.CompletedBackgroundSessionID == nil || *stdinResult.CompletedBackgroundSessionID != 1000 ||
		stdinResult.PresentationDelta == nil || stdinResult.PresentationDelta.ShellExitCode == nil ||
		*stdinResult.PresentationDelta.ShellExitCode != 0 {
		t.Fatalf("completed interactive write_stdin result = %+v", stdinResult)
	}
	if !strings.Contains(decodeStringToolOutput(t, stdinResult), "hello app") {
		t.Fatalf("interactive output missing: %s", stdinResult.Output)
	}
	waitForManagerCount(t, manager, 0, time.Second)
}

func TestWriteStdinCompletionTruncationSetsPresentationMetadata(t *testing.T) {
	workspace := t.TempDir()
	manager := newShellTestManager(t, 50*time.Millisecond)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	stdinTool := NewWriteStdinTool(manager)

	result := callExecCommand(t, execTool, "tty-trunc-1", map[string]any{
		"cmd":           "read line; printf '%s' \"$line\"",
		"shell":         "/bin/sh",
		"login":         false,
		"tty":           true,
		"yield_time_ms": 50,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}

	snapshot, err := manager.Snapshot("1000")
	if err != nil {
		t.Fatalf("background shell snapshot: %v", err)
	}
	stdinResult := callWriteStdin(t, stdinTool, "tty-trunc-2", map[string]any{
		"session_id":    1000,
		"chars":         strings.Repeat("x", 6_000) + "\n",
		"yield_time_ms": 2_000,
	})
	if stdinResult.IsError {
		t.Fatalf("unexpected write_stdin error: %s", string(stdinResult.Output))
	}
	if stdinResult.PresentationDelta == nil || !stdinResult.PresentationDelta.OutputTruncated {
		t.Fatalf("expected write_stdin truncation presentation delta, got %+v", stdinResult.PresentationDelta)
	}
	rendered := decodeStringToolOutput(t, stdinResult)
	if !strings.Contains(rendered, "Output file: "+snapshot.LogPath) {
		t.Fatalf("truncated polling response is missing its log path: %q", rendered)
	}
	if strings.Count(rendered, snapshot.LogPath) != 1 {
		t.Fatalf("truncated polling response repeats its log path: %q", rendered)
	}
	waitForManagerCount(t, manager, 0, 3*time.Second)
}

func TestWriteStdinPreservesBackgroundSummaryTruncationMetadata(t *testing.T) {
	workspace := t.TempDir()
	manager := newShellTestManager(t, 50*time.Millisecond)
	execTool := NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	stdinTool := NewWriteStdinTool(manager)

	result := callExecCommand(t, execTool, "tty-summary-trunc-1", map[string]any{
		"cmd":           "read line; head -c 2200000 /dev/zero | tr '\\0' x",
		"shell":         "/bin/sh",
		"login":         false,
		"tty":           true,
		"yield_time_ms": 50,
	})
	if result.IsError {
		t.Fatalf("unexpected exec_command error: %s", string(result.Output))
	}

	stdinResult := callWriteStdin(t, stdinTool, "tty-summary-trunc-2", map[string]any{
		"session_id":    1000,
		"chars":         "go\n",
		"yield_time_ms": 5_000,
	})
	if stdinResult.IsError {
		t.Fatalf("unexpected write_stdin error: %s", string(stdinResult.Output))
	}
	if stdinResult.PresentationDelta == nil || !stdinResult.PresentationDelta.OutputTruncated {
		t.Fatalf("expected source truncation presentation delta, got %+v", stdinResult.PresentationDelta)
	}
	waitForManagerCount(t, manager, 0, 3*time.Second)
}
