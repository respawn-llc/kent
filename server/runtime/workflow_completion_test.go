package runtime

import (
	"context"
	"encoding/json"
	"sort"
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
		Instructions: workflowruntime.TaskInstructions{
			TaskID:          "task-1",
			TaskShortID:     "BUI-1",
			TaskTitle:       "Workflow task",
			TaskBody:        "Task body.",
			WorkflowID:      "workflow-1",
			WorkflowShortID: "workflow-1",
			NodeID:          "node-1",
			NodeKey:         "agent",
			NodeDisplayName: "Agent",
			ContextMode:     "new_session",
			Transitions:     []workflowruntime.TransitionInstruction{{ID: "done", DisplayName: "Done"}},
			NodePrompt:      "Do node work.",
		},
	}
}

func TestWorkflowToolModeExposesCompleteNodeDespiteEnabledTools(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	workflowCfg := testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeTool)
	workflowCfg.Contract.OutputFields = append(workflowCfg.Contract.OutputFields, workflow.OutputField{Name: "details", Description: "Detailed evidence."})
	eng, err := New(store, &fakeClient{}, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:        "gpt-5",
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
		WorkflowRun:  workflowCfg,
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
	assertCompletionSchema(t, toolsByName[string(toolspec.ToolCompleteNode)].Schema, map[string]string{
		"summary": "Summary of work.",
		"details": "Detailed evidence.",
	}, "done")
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

func TestWorkflowModePromptInjectedWithoutHeadlessOrUserPrompt(t *testing.T) {
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
		Model:        "gpt-5",
		HeadlessMode: true,
		WorkflowRun:  testWorkflowConfig(controller, config.WorkflowCompletionModeTool),
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if _, err := eng.SubmitWorkflowTurn(context.Background()); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("model calls = %d, want 1", len(client.calls))
	}
	messages := requestMessages(client.calls[0])
	workflowIdx := -1
	for idx, msg := range messages {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorkflowMode {
			workflowIdx = idx
		}
	}
	if workflowIdx < 0 {
		t.Fatalf("workflow prompt missing: messages=%+v", messages)
	}
	if workflowContent := messages[workflowIdx].Content; !strings.Contains(workflowContent, "ticket `BUI-1`") || !strings.Contains(workflowContent, "Do node work.") || !strings.Contains(workflowContent, "complete_node") {
		t.Fatalf("workflow instructions missing expected content:\n%s", workflowContent)
	}
	for _, msg := range messages {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeHeadlessMode {
			t.Fatalf("headless prompt should not be injected during workflow runs: %+v", messages)
		}
		if msg.Role == llm.RoleUser {
			t.Fatalf("workflow run should not inject user prompt: %+v", messages)
		}
	}
}

func TestWorkflowModePromptReinjectedForNewRunAfterExistingWorkflowPrompt(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.AppendEvent("seed", "message", llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeWorkflowMode, SourcePath: "run-old", Content: "old workflow instructions"}); err != nil {
		t.Fatalf("seed workflow message: %v", err)
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
	if _, err := eng.SubmitWorkflowTurn(context.Background()); err != nil {
		t.Fatalf("submit: %v", err)
	}
	messages := requestMessages(client.calls[0])
	workflowMessages := []llm.Message{}
	for _, msg := range messages {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorkflowMode {
			workflowMessages = append(workflowMessages, msg)
		}
	}
	if len(workflowMessages) != 2 {
		t.Fatalf("workflow message count = %d, want old and new messages: %+v", len(workflowMessages), workflowMessages)
	}
	if workflowMessages[0].SourcePath != "run-old" || !strings.Contains(workflowMessages[0].Content, "old workflow instructions") {
		t.Fatalf("old workflow message missing/preserved incorrectly: %+v", workflowMessages)
	}
	if workflowMessages[1].SourcePath != "run-1" || !strings.Contains(workflowMessages[1].Content, "ticket `BUI-1`") {
		t.Fatalf("new workflow message missing run-scoped source path: %+v", workflowMessages)
	}
}

func TestWorkflowStructuredModeUsesStructuredOutput(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	workflowCfg := testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeStructuredOutput)
	workflowCfg.Contract.OutputFields = append(workflowCfg.Contract.OutputFields, workflow.OutputField{Name: "details", Description: "Detailed evidence."})
	client := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"transition_id":"done","commentary":"complete","summary":"done","details":"evidence"}`, Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	eng, err := New(store, client, tools.NewRegistry(fakeTool{name: toolspec.ToolExecCommand}), Config{
		Model:       "gpt-5",
		WorkflowRun: workflowCfg,
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
	req := client.calls[0]
	if req.StructuredOutput == nil {
		t.Fatal("expected structured output")
	}
	assertCompletionSchema(t, req.StructuredOutput.Schema, map[string]string{
		"summary": "Summary of work.",
		"details": "Detailed evidence.",
	}, "done")
	messages := requestMessages(req)
	workflowIdx := -1
	for idx, msg := range messages {
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeWorkflowMode {
			workflowIdx = idx
		}
	}
	if workflowIdx < 0 {
		t.Fatalf("workflow prompt missing from structured-output request: %+v", messages)
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

func assertCompletionSchema(t *testing.T, schema json.RawMessage, outputDescriptions map[string]string, transitionID string) {
	t.Helper()
	root := schemaRoot(t, schema)
	if got := root["additionalProperties"]; got != false {
		t.Fatalf("schema additionalProperties = %v, want false in %s", got, string(schema))
	}
	assertSchemaProperty(t, schema, "transition_id", "string", "Transition ID to take. Required when multiple outgoing transitions are available.")
	assertToolSchemaEnum(t, schema, "transition_id", transitionID)
	assertSchemaProperty(t, schema, "commentary", "string", "Brief explanation of what was completed and why this transition was selected.")
	for name, description := range outputDescriptions {
		assertNullableStringSchemaProperty(t, schema, name, description)
	}
	required := schemaRequired(t, schema)
	for _, name := range []string{"transition_id", "commentary"} {
		if !schemaRequiredContains(required, name) {
			t.Fatalf("schema required missing %s, required=%+v schema=%s", name, required, string(schema))
		}
	}
	for _, name := range sortedSchemaNames(outputDescriptions) {
		if schemaRequiredContains(required, name) {
			t.Fatalf("schema required includes optional output %s, required=%+v schema=%s", name, required, string(schema))
		}
	}
}

func assertSchemaProperty(t *testing.T, schema json.RawMessage, name string, propertyType string, description string) {
	t.Helper()
	property := schemaProperty(t, schema, name)
	if got := property["type"]; got != propertyType {
		t.Fatalf("schema property %s type = %v, want %s in %s", name, got, propertyType, string(schema))
	}
	if got := property["description"]; got != description {
		t.Fatalf("schema property %s description = %v, want %q in %s", name, got, description, string(schema))
	}
}

func assertNullableStringSchemaProperty(t *testing.T, schema json.RawMessage, name string, description string) {
	t.Helper()
	property := schemaProperty(t, schema, name)
	rawTypes, ok := property["type"].([]any)
	if !ok || len(rawTypes) != 2 || rawTypes[0] != "string" || rawTypes[1] != "null" {
		t.Fatalf("schema property %s type = %v, want [string null] in %s", name, property["type"], string(schema))
	}
	if got := property["description"]; got != description {
		t.Fatalf("schema property %s description = %v, want %q in %s", name, got, description, string(schema))
	}
}

func assertToolSchemaEnum(t *testing.T, schema json.RawMessage, name string, value string) {
	t.Helper()
	property := schemaProperty(t, schema, name)
	rawEnum, ok := property["enum"].([]any)
	if !ok {
		t.Fatalf("schema property %s enum missing in %s", name, string(schema))
	}
	for _, item := range rawEnum {
		if item == value {
			return
		}
	}
	t.Fatalf("schema property %s enum missing %q: %+v", name, value, rawEnum)
}

func schemaProperty(t *testing.T, schema json.RawMessage, name string) map[string]any {
	t.Helper()
	root := schemaRoot(t, schema)
	properties, ok := root["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %s", string(schema))
	}
	property, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("schema property %s missing: %s", name, string(schema))
	}
	return property
}

func schemaRequired(t *testing.T, schema json.RawMessage) []string {
	t.Helper()
	root := schemaRoot(t, schema)
	raw, ok := root["required"].([]any)
	if !ok {
		t.Fatalf("schema required missing: %s", string(schema))
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("schema required item is %T: %v", item, item)
		}
		out = append(out, text)
	}
	if len(out) == 0 {
		t.Fatalf("schema required empty: %s", string(schema))
	}
	return out
}

func schemaRoot(t *testing.T, schema json.RawMessage) map[string]any {
	t.Helper()
	root := map[string]any{}
	if err := json.Unmarshal(schema, &root); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	return root
}

func schemaRequiredContains(required []string, name string) bool {
	for _, field := range required {
		if field == name {
			return true
		}
	}
	return false
}

func sortedSchemaNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
