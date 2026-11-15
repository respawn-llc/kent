package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/server/tools"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func assertOversizedOutputFailure(t *testing.T, result tools.Result, logPath string) {
	t.Helper()
	message := fmt.Sprintf(oversizedOutputMessageTemplate, logPath)
	if !result.IsError || decodeStringToolOutput(t, result) != message {
		t.Fatalf("failure = %s, want %q", result.Output, message)
	}
}

func assertGuardedPresentation(t *testing.T, result tools.Result, raw bool, backgrounded bool, exitCode *int) {
	t.Helper()
	delta := result.PresentationDelta
	if !result.IsError || delta == nil || delta.RawOutputRequested != raw ||
		delta.MovedToBackground != backgrounded {
		t.Fatalf("result = %+v, want guarded result with presentation facts", result)
	}
	if exitCode == nil && delta.ShellExitCode != nil ||
		exitCode != nil && (delta.ShellExitCode == nil || *delta.ShellExitCode != *exitCode) {
		t.Fatalf("exit code presentation = %+v, want %v", delta.ShellExitCode, exitCode)
	}
}

func TestExecCommandGuardPreservesOutputPathPresentationAndLog(t *testing.T) {
	manager := newBackgroundTestManager(t)
	tool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 20, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	const output = "123456789012345678901234567890123456789012345678"
	input, err := json.Marshal(map[string]any{
		"cmd": "printf '" + output + "'; exit 7", "shell": "/bin/sh", "login": false,
		"raw": true, "yield_time_ms": 1_000, "max_output_tokens": 11,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := shellTestExecutionContext(t, context.Background(), "guarded")
	identity, err := tools.ExecutionIdentityFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Call(ctx, tools.Call{ID: "guarded", Name: toolspec.ToolExecCommand, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join("/tmp/kent", identity.SessionShortID.String())
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("retained log entries = %v, error=%v", entries, err)
	}
	logPath := filepath.Join(directory, entries[0].Name())
	assertOversizedOutputFailure(t, result, logPath)
	if log, err := os.ReadFile(logPath); err != nil || string(log) != output {
		t.Fatalf("retained output = %q, error=%v", log, err)
	}
	exitCode := 7
	assertGuardedPresentation(t, result, true, false, &exitCode)
}

func TestExecCommandGuardUsesSelectedPlaintextEstimator(t *testing.T) {
	for _, test := range []struct {
		name      string
		estimate  func(string) int
		cap       *int
		wantError bool
	}{
		{"eligible higher estimate", func(text string) int { return len(text) / 2 }, textutil.Value(11), true},
		{"eligible within threshold", func(text string) int { return len(text) / 3 }, textutil.Value(11), false},
		{"omitted cap", func(text string) int { return len(text) / 2 }, nil, false},
		{"cap at threshold", func(text string) int { return len(text) / 2 }, textutil.Value(10), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := newBackgroundTestManager(t)
			tool := NewExecCommandToolWithConfig(t.TempDir(), 16_000, 20, manager, "", ExecCommandToolConfig{
				EstimateText: test.estimate,
				Postprocessor: postprocessfixture.NewRunner(t, postprocess.Settings{
					Mode: config.ShellPostprocessingModeBuiltin,
				}),
			})
			input := map[string]any{
				"cmd": "printf '123456789012345678901234567890'", "shell": "/bin/sh",
				"login": false, "raw": true, "yield_time_ms": 1_000,
			}
			if test.cap != nil {
				input["max_output_tokens"] = *test.cap
			}
			result := callExecCommand(t, tool, "selected-estimator", input)
			if result.IsError != test.wantError {
				t.Fatalf("guarded = %t, want %t", result.IsError, test.wantError)
			}
		})
	}
}

func TestExecCommandGuardBoundariesAndOrdinaryTruncation(t *testing.T) {
	ptr := func(value int) *int { return &value }
	tests := []struct {
		name, output string
		cap          *int
		full         bool
		truncated    bool
	}{
		{"output at half", strings.Repeat("1", 80), ptr(21), true, false},
		{"cap equal to half", strings.Repeat("1", 100), ptr(20), false, true},
		{"cap below half", strings.Repeat("1", 100), ptr(19), false, true},
		{"cap omitted", strings.Repeat("1", 84), nil, true, false},
		{"decoded escaping", strings.Repeat(`"\`, 40), ptr(21), true, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := newBackgroundTestManager(t)
			input := map[string]any{
				"cmd": "printf '%s' '" + test.output + "'", "shell": "/bin/sh",
				"login": false, "yield_time_ms": 1_000,
			}
			if test.cap != nil {
				input["max_output_tokens"] = *test.cap
			}
			result := callExecCommand(t, NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 40, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin})), test.name, input)
			if result.IsError {
				t.Fatalf("unexpected error: %s", result.Output)
			}
			got := decodeStringToolOutput(t, result)
			if test.full != (got == test.output) {
				t.Fatalf("output = %q, want full=%t", got, test.full)
			}
			if test.truncated != (result.PresentationDelta != nil && result.PresentationDelta.OutputTruncated) {
				t.Fatalf("presentation = %+v, want truncated=%t", result.PresentationDelta, test.truncated)
			}
		})
	}
}

func TestRunningExecCommandGuardPreservesLifecycleAndIndependentPoll(t *testing.T) {
	manager := newShellTestManager(t, 50*time.Millisecond)
	tool := NewExecCommandToolWithPostprocessor(t.TempDir(), 16_000, 40, manager, "", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	const output = "12345678901234567890123456789012345678901234567890123456789012345678901234567890"
	result := callExecCommand(t, tool, "running", map[string]any{
		"cmd": "printf '" + output + "'; sleep 0.5", "shell": "/bin/sh", "login": false,
		"raw": true, "yield_time_ms": 50, "max_output_tokens": 21,
	})
	snapshot, err := manager.Snapshot("1000")
	if err != nil || !snapshot.Running {
		t.Fatalf("running snapshot = %+v, error=%v", snapshot, err)
	}
	assertOversizedOutputFailure(t, result, snapshot.LogPath)
	assertGuardedPresentation(t, result, true, true, nil)
	if poll := callWriteStdin(t, NewWriteStdinTool(manager), "later", map[string]any{
		"session_id": 1000, "yield_time_ms": 15_000,
	}); poll.IsError {
		t.Fatalf("independent poll = %s", poll.Output)
	}
	if log, err := os.ReadFile(snapshot.LogPath); err != nil || string(log) != output {
		t.Fatalf("retained output = %q, error=%v", log, err)
	}
}
