package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"builder/server/llm"
	"builder/server/tools"
	"builder/server/workflowruntime"
	"builder/shared/toolspec"
)

const (
	workflowFinalAnswerNudge = "Workflow mode: normal final answers do not complete this node. Produce valid workflow completion output instead."
	workflowInvalidNudge     = "Workflow completion was rejected. Retry with valid workflow completion output only."
)

func (e *Engine) workflowCompletionRejectedResult(ctx context.Context, result tools.Result, completionErr error) tools.Result {
	record, err := e.recordWorkflowProtocolViolation(ctx, workflowruntime.ViolationKindInvalidCompletion, completionErr.Error())
	result.IsError = true
	result.Output = workflowruntime.ToolErrorPayload(completionErr)
	result.Summary = "workflow completion rejected"
	if err != nil {
		result.Output = mustJSON(map[string]any{"error": err.Error()})
		result.Summary = err.Error()
	}
	if record.Interrupted {
		result.Terminal = true
	}
	return result
}

func (e *Engine) recordWorkflowProtocolViolation(ctx context.Context, kind workflowruntime.ViolationKind, detail string) (workflowruntime.ViolationResult, error) {
	if !e.workflowRunActive() || e.cfg.WorkflowRun.Controller == nil {
		return workflowruntime.ViolationResult{}, nil
	}
	maxCount := e.cfg.WorkflowRun.MaxInvalidCompletionAttempts
	if kind == workflowruntime.ViolationKindFinalAnswer {
		maxCount = e.cfg.WorkflowRun.MaxFinalAnswerViolations
	}
	if maxCount <= 0 {
		if kind == workflowruntime.ViolationKindFinalAnswer {
			maxCount = 3
		} else {
			maxCount = 5
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"kind":   string(kind),
		"detail": strings.TrimSpace(detail),
	})
	return e.cfg.WorkflowRun.Controller.RecordWorkflowProtocolViolation(ctx, workflowruntime.ViolationRequest{
		RunID:              e.cfg.WorkflowRun.Contract.RunID,
		Kind:               kind,
		MaxCount:           maxCount,
		Detail:             string(payload),
		ExpectedGeneration: e.cfg.WorkflowRun.Contract.ExpectedGeneration,
		RequireGeneration:  e.cfg.WorkflowRun.Contract.RequireGeneration,
	})
}

func workflowCompletionCallCount(calls []llm.ToolCall) int {
	count := 0
	for _, call := range calls {
		id, ok := toolspec.ParseID(call.Name)
		if ok && id == toolspec.ToolCompleteNode {
			count++
		}
	}
	return count
}

func hasWorkflowTerminalResult(results []tools.Result) bool {
	for _, result := range results {
		if result.Name == toolspec.ToolCompleteNode && result.Terminal {
			return true
		}
	}
	return false
}

func workflowPreflightError(workflowActive bool, localToolCalls []llm.ToolCall, hostedToolExecutions []hostedToolExecution) error {
	if !workflowActive {
		return nil
	}
	count := workflowCompletionCallCount(localToolCalls)
	if count == 0 {
		return nil
	}
	if count != 1 {
		return fmt.Errorf("complete_node must be called exactly once")
	}
	return nil
}
