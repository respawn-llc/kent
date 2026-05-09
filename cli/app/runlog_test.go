package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"builder/server/llm"
	"builder/server/runprompt"
	"builder/server/runtime"
	"builder/server/tools"
	"builder/shared/clientui"
	"builder/shared/toolspec"
)

func TestRunLoggerWritesStepsFile(t *testing.T) {
	dir := t.TempDir()
	logger, err := newRunLogger(dir, nil)
	if err != nil {
		t.Fatalf("newRunLogger failed: %v", err)
	}
	logger.Logf("step.start user_chars=%d", 10)
	logger.Logf("step.error err=%q", "boom")
	if err := logger.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, runLogFileName))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "step.start user_chars=10") {
		t.Fatalf("missing step start log: %q", text)
	}
	if !strings.Contains(text, `step.error err="boom"`) {
		t.Fatalf("missing step error log: %q", text)
	}
}

func TestNewRunLoggerNoopsWhenSessionDirDoesNotExist(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing-session")
	logger, err := newRunLogger(missingDir, nil)
	if err != nil {
		t.Fatalf("new run logger: %v", err)
	}
	logger.Logf("hello %s", "world")
	if err := logger.Close(); err != nil {
		t.Fatalf("close run logger: %v", err)
	}
	if _, err := os.Stat(filepath.Join(missingDir, runLogFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no run log file for non-persisted session, stat err=%v", err)
	}
}

type failingWriteCloser struct{}

func (failingWriteCloser) WriteString(string) (int, error) {
	return 0, errors.New("disk full")
}

func (failingWriteCloser) Close() error {
	return nil
}

func TestRunLoggerReportsWriteFailureDiagnosticOnce(t *testing.T) {
	var diagnostics []runLoggerDiagnostic
	logger := &runLogger{
		fp: failingWriteCloser{},
		onDiagnostic: func(diag runLoggerDiagnostic) {
			diagnostics = append(diagnostics, diag)
		},
	}
	logger.Logf("first")
	logger.Logf("second")

	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %d", len(diagnostics))
	}
	if diagnostics[0].Kind != "write_failed" {
		t.Fatalf("expected write_failed diagnostic kind, got %+v", diagnostics[0])
	}
	if diagnostics[0].Err == nil || !strings.Contains(diagnostics[0].Err.Error(), "disk full") {
		t.Fatalf("expected underlying write error, got %+v", diagnostics[0])
	}
}

func TestReportRunLoggerDiagnosticWritesMessage(t *testing.T) {
	var buf bytes.Buffer
	reportRunLoggerDiagnostic(&buf, runLoggerDiagnostic{Kind: "write_failed", Message: "run log write failed; observability degraded: disk full", Err: errors.New("disk full")})
	if got := strings.TrimSpace(buf.String()); got != `run_logger.diagnostic kind=write_failed message="run log write failed; observability degraded: disk full" err="disk full"` {
		t.Fatalf("unexpected diagnostic output: %q", got)
	}
}

func TestFormatRuntimeEventIncludesToolMetadata(t *testing.T) {
	call := llm.ToolCall{ID: "call-1", Name: "shell"}
	line := formatRuntimeEvent(runtime.Event{
		Kind:     runtime.EventToolCallStarted,
		StepID:   "step-1",
		ToolCall: &call,
	})
	if !strings.Contains(line, "call_id=call-1") || !strings.Contains(line, "name=shell") {
		t.Fatalf("unexpected event line: %q", line)
	}

	res := tools.Result{CallID: "call-1", Name: toolspec.ToolExecCommand, IsError: true}
	line = formatRuntimeEvent(runtime.Event{
		Kind:       runtime.EventToolCallCompleted,
		StepID:     "step-1",
		ToolResult: &res,
	})
	if !strings.Contains(line, "is_error=true") {
		t.Fatalf("unexpected completion line: %q", line)
	}

	line = formatRuntimeEvent(runtime.Event{
		Kind:   runtime.EventInFlightClearFailed,
		StepID: "step-2",
		Error:  "mark in-flight false: write failed",
	})
	if !strings.Contains(line, "kind=in_flight_clear_failed") || !strings.Contains(line, `err="mark in-flight false: write failed"`) {
		t.Fatalf("unexpected in-flight clear failure line: %q", line)
	}
}

func formatRuntimeEvent(evt runtime.Event) string {
	switch evt.Kind {
	case runtime.EventAssistantDelta:
		return fmt.Sprintf("runtime.event kind=%s step_id=%s delta_chars=%d", evt.Kind, evt.StepID, len(evt.AssistantDelta))
	case runtime.EventAssistantDeltaReset:
		return fmt.Sprintf("runtime.event kind=%s step_id=%s", evt.Kind, evt.StepID)
	case runtime.EventAssistantMessage:
		return fmt.Sprintf("runtime.event kind=%s step_id=%s message_chars=%d", evt.Kind, evt.StepID, len(evt.Message.Content))
	case runtime.EventModelResponse:
		if evt.ModelResponse != nil {
			return fmt.Sprintf(
				"runtime.event kind=%s step_id=%s phase=%s assistant_chars=%d tool_calls=%d output_items=%d output_types=%q",
				evt.Kind,
				evt.StepID,
				evt.ModelResponse.AssistantPhase,
				evt.ModelResponse.AssistantChars,
				evt.ModelResponse.ToolCallsCount,
				evt.ModelResponse.OutputItemsCount,
				strings.Join(evt.ModelResponse.OutputItemTypes, ","),
			)
		}
	case runtime.EventUserMessageFlushed:
		return fmt.Sprintf("runtime.event kind=%s step_id=%s user_chars=%d", evt.Kind, evt.StepID, len(evt.UserMessage))
	case runtime.EventToolCallStarted:
		if evt.ToolCall != nil {
			return fmt.Sprintf("runtime.event kind=%s step_id=%s call_id=%s name=%s", evt.Kind, evt.StepID, evt.ToolCall.ID, evt.ToolCall.Name)
		}
	case runtime.EventToolCallCompleted:
		if evt.ToolResult != nil {
			return fmt.Sprintf("runtime.event kind=%s step_id=%s call_id=%s name=%s is_error=%t", evt.Kind, evt.StepID, evt.ToolResult.CallID, evt.ToolResult.Name, evt.ToolResult.IsError)
		}
	case runtime.EventReviewerCompleted:
		if evt.Reviewer != nil {
			line := fmt.Sprintf(
				"runtime.event kind=%s step_id=%s outcome=%s suggestions=%d",
				evt.Kind,
				evt.StepID,
				evt.Reviewer.Outcome,
				evt.Reviewer.SuggestionsCount,
			)
			if strings.TrimSpace(evt.Reviewer.Error) != "" {
				line += fmt.Sprintf(" err=%q", evt.Reviewer.Error)
			}
			return line
		}
	case runtime.EventInFlightClearFailed:
		if strings.TrimSpace(evt.Error) != "" {
			return fmt.Sprintf("runtime.event kind=%s step_id=%s err=%q", evt.Kind, evt.StepID, evt.Error)
		}
	case runtime.EventCompactionStarted, runtime.EventCompactionCompleted, runtime.EventCompactionFailed:
		if evt.Compaction != nil {
			line := fmt.Sprintf(
				"runtime.event kind=%s step_id=%s mode=%s engine=%s provider=%s trimmed=%d count=%d",
				evt.Kind,
				evt.StepID,
				evt.Compaction.Mode,
				evt.Compaction.Engine,
				evt.Compaction.Provider,
				evt.Compaction.TrimmedItemsCount,
				evt.Compaction.Count,
			)
			if strings.TrimSpace(evt.Compaction.Error) != "" {
				line += fmt.Sprintf(" err=%q", evt.Compaction.Error)
			}
			return line
		}
	case runtime.EventRunStateChanged:
		if evt.RunState != nil {
			return fmt.Sprintf("runtime.event kind=%s step_id=%s busy=%t", evt.Kind, evt.StepID, evt.RunState.Busy)
		}
	case runtime.EventBackgroundUpdated:
		if evt.Background != nil {
			line := fmt.Sprintf("runtime.event kind=%s id=%s type=%s state=%s", evt.Kind, evt.Background.ID, evt.Background.Type, evt.Background.State)
			if evt.Background.ExitCode != nil {
				line += fmt.Sprintf(" exit_code=%d", *evt.Background.ExitCode)
			}
			return line
		}
	}
	return fmt.Sprintf("runtime.event kind=%s step_id=%s", evt.Kind, evt.StepID)
}

func TestTranscriptDiagnosticsIncludeRevisionAndCommittedEntryCount(t *testing.T) {
	projected := clientui.Event{
		Kind:                clientui.EventToolCallStarted,
		StepID:              "step-1",
		TranscriptRevision:  17,
		CommittedEntryCount: 42,
	}
	projectionLine := runprompt.FormatTranscriptProjectionDiagnostic("session-1", projected)
	if !strings.Contains(projectionLine, "revision=17") || !strings.Contains(projectionLine, "committed_entry_count=42") {
		t.Fatalf("unexpected projection diagnostic: %q", projectionLine)
	}
	publishLine := runprompt.FormatTranscriptPublishDiagnostic("session-1", projected)
	if !strings.Contains(publishLine, "revision=17") || !strings.Contains(publishLine, "committed_entry_count=42") {
		t.Fatalf("unexpected publish diagnostic: %q", publishLine)
	}
}
