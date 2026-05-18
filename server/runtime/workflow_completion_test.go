package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"builder/server/llm"
	"builder/server/session"
	"builder/server/tools"
	"builder/server/workflow"
	"builder/server/workflowruntime"
	"builder/shared/config"
	"builder/shared/toolspec"
)

type fakeWorkflowController struct {
	completed  atomic.Int64
	violations atomic.Int64
	maxHits    atomic.Int64
}

func (c *fakeWorkflowController) CompleteWorkflowRun(context.Context, workflowruntime.CompletionRequest) (workflowruntime.CompletionResult, error) {
	c.completed.Add(1)
	return workflowruntime.CompletionResult{TransitionID: "transition-applied", State: "applied"}, nil
}

func (c *fakeWorkflowController) RecordWorkflowProtocolViolation(_ context.Context, req workflowruntime.ViolationRequest) (workflowruntime.ViolationResult, error) {
	count := c.violations.Add(1)
	interrupted := count >= int64(req.MaxCount)
	if interrupted {
		c.maxHits.Add(1)
	}
	return workflowruntime.ViolationResult{Count: count, Interrupted: interrupted}, nil
}

type countingTool struct {
	name  toolspec.ID
	count atomic.Int64
}

func (t *countingTool) Name() toolspec.ID { return t.name }

func (t *countingTool) Call(_ context.Context, c tools.Call) (tools.Result, error) {
	t.count.Add(1)
	return tools.Result{CallID: c.ID, Name: c.Name, Output: json.RawMessage(`{"ok":true}`)}, nil
}

func testWorkflowConfig(controller workflowruntime.Controller, mode config.WorkflowCompletionMode) *workflowruntime.Config {
	return &workflowruntime.Config{
		Contract: workflowruntime.CompletionContract{
			RunID:              "run-1",
			ExpectedGeneration: 7,
			RequireGeneration:  true,
			TransitionIDs:      []string{"done"},
			OutputFields:       []workflow.OutputField{{Name: "summary", Description: "Summary of work."}},
		},
		CompletionMode:               mode,
		MaxFinalAnswerViolations:     3,
		MaxInvalidCompletionAttempts: 2,
		Controller:                   controller,
	}
}

func TestWorkflowToolModeExposesCompleteNodeDespiteEnabledTools(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := New(store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:        "gpt-5",
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
		WorkflowRun:  testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeTool),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	req, err := eng.buildRequest(context.Background(), "step", true)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	toolsByName := map[string]llm.Tool{}
	for _, tool := range req.Tools {
		toolsByName[tool.Name] = tool
	}
	if _, ok := toolsByName[string(toolspec.ToolCompleteNode)]; !ok {
		t.Fatalf("complete_node not advertised, tools=%+v", req.Tools)
	}
	if _, ok := toolsByName[string(toolspec.ToolExecCommand)]; ok {
		t.Fatalf("exec_command should not be re-added from role tools, tools=%+v", req.Tools)
	}
}

func TestCompleteNodeNotAdvertisedOutsideWorkflow(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := New(store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:        "gpt-5",
		EnabledTools: []toolspec.ID{toolspec.ToolCompleteNode},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	req, err := eng.buildRequest(context.Background(), "step", true)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	for _, tool := range req.Tools {
		if tool.Name == string(toolspec.ToolCompleteNode) {
			t.Fatalf("complete_node advertised outside workflow: %+v", req.Tools)
		}
	}
}

func TestWorkflowModePromptInjectedBeforeUserPrompt(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "complete", Phase: llm.MessagePhaseCommentary},
		ToolCalls: []llm.ToolCall{{
			ID:    "call_complete",
			Name:  string(toolspec.ToolCompleteNode),
			Input: json.RawMessage(`{"transition_id":"done","commentary":"complete","summary":"done"}`),
		}},
		Usage: llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "gpt-5",
		WorkflowRun: testWorkflowConfig(controller, config.WorkflowCompletionModeTool),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "node prompt"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("model calls = %d, want 1", len(client.calls))
	}
	messages := requestMessages(client.calls[0])
	workflowIdx := -1
	userIdx := -1
	for idx, msg := range messages {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorkflowMode {
			workflowIdx = idx
		}
		if msg.Role == llm.RoleUser && msg.Content == "node prompt" {
			userIdx = idx
		}
	}
	if workflowIdx < 0 || userIdx < 0 || workflowIdx > userIdx {
		t.Fatalf("workflow prompt/user ordering invalid: workflow=%d user=%d messages=%+v", workflowIdx, userIdx, messages)
	}
}

func TestWorkflowStructuredModeUsesStructuredOutput(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := New(store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "gpt-5",
		WorkflowRun: testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeStructuredOutput),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	req, err := eng.buildRequest(context.Background(), "step", true)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if req.StructuredOutput == nil {
		t.Fatal("expected structured output")
	}
	for _, tool := range req.Tools {
		if tool.Name == string(toolspec.ToolCompleteNode) {
			t.Fatalf("complete_node advertised in structured mode: %+v", req.Tools)
		}
	}
}

func TestWorkflowAutoFallsBackToToolModeWhenStructuredOutputUnsupported(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	client := &fakeClient{caps: llm.ProviderCapabilities{ProviderID: "legacy"}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "legacy",
		WorkflowRun: testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeAuto),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	req, err := eng.buildRequest(context.Background(), "step", true)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if req.StructuredOutput != nil {
		t.Fatalf("structured output set in fallback tool mode: %+v", req.StructuredOutput)
	}
	for _, tool := range req.Tools {
		if tool.Name == string(toolspec.ToolCompleteNode) {
			return
		}
	}
	t.Fatalf("complete_node not advertised in auto fallback tool mode: %+v", req.Tools)
}

func TestWorkflowForcedStructuredOutputFailsWhenUnsupported(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	client := &fakeClient{caps: llm.ProviderCapabilities{ProviderID: "legacy"}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "legacy",
		WorkflowRun: testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeStructuredOutput),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	_, err = eng.buildRequest(context.Background(), "step", true)
	if err == nil || !strings.Contains(err.Error(), "structured output") {
		t.Fatalf("buildRequest error = %v, want structured output support error", err)
	}
}

func TestCompleteNodeOutsideWorkflowReturnsToolError(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "complete", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{
				ID:    "call_complete",
				Name:  string(toolspec.ToolCompleteNode),
				Input: json.RawMessage(`{"transition_id":"done"}`),
			}},
			Usage: llm.Usage{WindowTokens: 200000},
		},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
	}}
	eng, err := New(store, client, tools.NewRegistry(), Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	found := false
	for _, msg := range eng.snapshotMessages() {
		if msg.Role == llm.RoleTool && msg.Name == string(toolspec.ToolCompleteNode) && strings.Contains(msg.Content, "only available during a workflow run") {
			found = true
		}
	}
	if !found {
		t.Fatalf("complete_node error output missing from messages: %+v", eng.snapshotMessages())
	}
}

func TestWorkflowMixedCompleteNodeRunsSideEffects(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	sideEffect := &countingTool{name: toolspec.ToolExecCommand}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "mixed", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{
				{ID: "call_complete", Name: string(toolspec.ToolCompleteNode), Input: json.RawMessage(`{"transition_id":"done","commentary":"complete","summary":"done"}`)},
				{ID: "call_shell", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"echo side-effect"}`)},
			},
			Usage: llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "complete", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{ID: "call_complete_2", Name: string(toolspec.ToolCompleteNode), Input: json.RawMessage(`{"transition_id":"done","commentary":"complete","summary":"done"}`)}},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}
	eng, err := New(store, client, tools.NewRegistry(sideEffect), Config{
		Model:       "gpt-5",
		WorkflowRun: testWorkflowConfig(controller, config.WorkflowCompletionModeTool),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if got := sideEffect.count.Load(); got != 1 {
		t.Fatalf("side-effect tool executions = %d, want 1", got)
	}
	if got := controller.completed.Load(); got != 1 {
		t.Fatalf("completions = %d, want 1", got)
	}
	if got := controller.violations.Load(); got != 0 {
		t.Fatalf("violations = %d, want 0", got)
	}
}

func TestWorkflowDuplicateCompleteNodePreflightSkipsSideEffects(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "duplicated", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{
				{ID: "call_complete_1", Name: string(toolspec.ToolCompleteNode), Input: json.RawMessage(`{"transition_id":"done","commentary":"complete","summary":"done"}`)},
				{ID: "call_complete_2", Name: string(toolspec.ToolCompleteNode), Input: json.RawMessage(`{"transition_id":"done","commentary":"complete","summary":"done"}`)},
			},
			Usage: llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "complete", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{ID: "call_complete_3", Name: string(toolspec.ToolCompleteNode), Input: json.RawMessage(`{"transition_id":"done","commentary":"complete","summary":"done"}`)}},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "gpt-5",
		WorkflowRun: testWorkflowConfig(controller, config.WorkflowCompletionModeTool),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if got := controller.completed.Load(); got != 1 {
		t.Fatalf("completions = %d, want 1", got)
	}
	if got := controller.violations.Load(); got != 1 {
		t.Fatalf("violations = %d, want 1", got)
	}
}

func TestWorkflowStructuredCompletionStopsWithoutAnotherTurn(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"transition_id":"done","commentary":"complete","summary":"done"}`, Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "unexpected", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
	}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "gpt-5",
		WorkflowRun: testWorkflowConfig(controller, config.WorkflowCompletionModeStructuredOutput),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("model calls = %d, want 1", len(client.calls))
	}
	if got := controller.completed.Load(); got != 1 {
		t.Fatalf("completions = %d, want 1", got)
	}
}

func TestWorkflowInvalidCompletionAttemptsInterruptAtCap(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "bad", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{ID: "call_bad_1", Name: string(toolspec.ToolCompleteNode), Input: json.RawMessage(`{"summary":1}`)}},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "bad", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{ID: "call_bad_2", Name: string(toolspec.ToolCompleteNode), Input: json.RawMessage(`{"summary":1}`)}},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "unexpected", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
	}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "gpt-5",
		WorkflowRun: testWorkflowConfig(controller, config.WorkflowCompletionModeTool),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(client.calls))
	}
	if got := controller.maxHits.Load(); got != 1 {
		t.Fatalf("max hits = %d, want 1", got)
	}
}

func TestWorkflowFinalAnswerViolationsInterruptAtCap(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done 1", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done 2", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done 3", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "unexpected", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
	}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "gpt-5",
		WorkflowRun: testWorkflowConfig(controller, config.WorkflowCompletionModeTool),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(client.calls) != 3 {
		t.Fatalf("model calls = %d, want 3", len(client.calls))
	}
	if got := controller.maxHits.Load(); got != 1 {
		t.Fatalf("max hits = %d, want 1", got)
	}
}

func TestWorkflowEmptyFinalAnswerViolationsInterruptAtCap(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "unexpected", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
	}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "gpt-5",
		WorkflowRun: testWorkflowConfig(controller, config.WorkflowCompletionModeTool),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(client.calls) != 3 {
		t.Fatalf("model calls = %d, want 3", len(client.calls))
	}
	if got := controller.maxHits.Load(); got != 1 {
		t.Fatalf("max hits = %d, want 1", got)
	}
}

func TestWorkflowStructuredEmptyFinalInterruptsAtInvalidCompletionCap(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: "unexpected", Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}},
	}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "gpt-5",
		WorkflowRun: testWorkflowConfig(controller, config.WorkflowCompletionModeStructuredOutput),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(client.calls))
	}
	if got := controller.maxHits.Load(); got != 1 {
		t.Fatalf("max hits = %d, want 1", got)
	}
}
