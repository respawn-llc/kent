package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"core/prompts"
	"core/server/llm"
	"core/server/tools"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/shared/config"
	"core/shared/toolspec"
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
			Transitions: []workflowruntime.CompletionTransition{{
				ID:         "done",
				Parameters: []workflow.Parameter{{Key: "summary", Description: "Summary of work."}},
			}},
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

func completeNodeCall(id string, input json.RawMessage) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: string(toolspec.ToolCompleteNode), Input: input}
}

func structuredFinalResponse(content string) llm.Response {
	return llm.Response{Assistant: llm.Message{Role: llm.RoleAssistant, Content: content, Phase: llm.MessagePhaseFinal}, Usage: llm.Usage{WindowTokens: 200000}}
}

func TestWorkflowToolModeExposesCompleteNodeDespiteEnabledTools(t *testing.T) {
	store := mustCreateTestSession(t)
	workflowCfg := testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeTool)
	workflowCfg.Contract.Transitions[0].Parameters = append(workflowCfg.Contract.Transitions[0].Parameters, workflow.Parameter{Key: "details", Description: "Detailed evidence."})
	eng := mustNewWorkflowTestEngine(t, store, &fakeClient{}, workflowCfg, Config{
		EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion},
	})
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
	})
	if _, ok := toolsByName[string(toolspec.ToolExecCommand)]; ok {
		t.Fatalf("exec_command should not be re-added from role tools, tools=%+v", req.Tools)
	}
}

func TestCompleteNodeNotAdvertisedOutsideWorkflow(t *testing.T) {
	store := mustCreateTestSession(t)
	eng := mustNewTestEngine(t, store, &fakeClient{}, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand}}), Config{
		EnabledTools: []toolspec.ID{toolspec.ToolCompleteNode},
	})
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
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{commentaryResponse("complete",
		llm.ToolCall{
			ID:    "call_complete",
			Name:  string(toolspec.ToolCompleteNode),
			Input: json.RawMessage(`{"commentary":"complete","summary":"done"}`),
		},
	)}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeTool), Config{
		HeadlessMode: true,
	})
	if _, err := eng.SubmitWorkflowTurn(context.Background()); err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertModelCallCount(t, client, 1)
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
	store := mustCreateTestSession(t)
	if _, _, err := store.AppendEvent("seed", "message", llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeWorkflowMode, SourcePath: "run-old", Content: "old workflow instructions"}); err != nil {
		t.Fatalf("seed workflow message: %v", err)
	}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{commentaryResponse("complete",
		completeNodeCall("call_complete", json.RawMessage(`{"commentary":"complete","summary":"done"}`)),
	)}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeTool), Config{})
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
	store := mustCreateTestSession(t)
	workflowCfg := testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeStructuredOutput)
	workflowCfg.Contract.Transitions[0].Parameters = append(workflowCfg.Contract.Transitions[0].Parameters, workflow.Parameter{Key: "details", Description: "Detailed evidence."})
	client := &fakeClient{responses: []llm.Response{structuredFinalResponse(`{"commentary":"complete","summary":"done","details":"evidence"}`)}}
	eng := mustNewWorkflowTestEngine(t, store, client, workflowCfg, Config{})
	if _, err := eng.SubmitUserMessage(context.Background(), "node prompt"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertModelCallCount(t, client, 1)
	req := client.calls[0]
	if req.StructuredOutput == nil {
		t.Fatal("expected structured output")
	}
	assertCompletionSchema(t, req.StructuredOutput.Schema, map[string]string{
		"summary": "Summary of work.",
		"details": "Detailed evidence.",
	})
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
	store := mustCreateTestSession(t)
	client := &fakeClient{caps: llm.ProviderCapabilities{ProviderID: "legacy"}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeAuto), Config{
		Model: "legacy",
	})
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
	store := mustCreateTestSession(t)
	client := &fakeClient{caps: llm.ProviderCapabilities{ProviderID: "legacy"}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(&fakeWorkflowController{}, config.WorkflowCompletionModeStructuredOutput), Config{
		Model: "legacy",
	})
	_, err := eng.buildRequest(context.Background(), "step", true)
	if !errors.Is(err, workflowruntime.ErrStructuredOutputUnsupported) {
		t.Fatalf("buildRequest error = %v, want structured output support error", err)
	}
}

func TestCompleteNodeOutsideWorkflowReturnsToolError(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{
		commentaryResponse("complete", completeNodeCall("call_complete", json.RawMessage(`{"transition":"done"}`))),
		structuredFinalResponse("done"),
	}}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{})
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
	store := mustCreateTestSession(t)
	sideEffect := &countingTool{name: toolspec.ToolExecCommand}
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		commentaryResponse("mixed",
			completeNodeCall("call_complete", json.RawMessage(`{"commentary":"complete","summary":"done"}`)),
			llm.ToolCall{ID: "call_shell", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"echo side-effect"}`)},
		),
		commentaryResponse("complete", completeNodeCall("call_complete_2", json.RawMessage(`{"commentary":"complete","summary":"done"}`))),
	}}
	eng := mustNewTestEngine(t, store, client, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: sideEffect}), Config{
		WorkflowRun: testWorkflowConfig(controller, config.WorkflowCompletionModeTool),
	})
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
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		commentaryResponse("duplicated",
			completeNodeCall("call_complete_1", json.RawMessage(`{"commentary":"complete","summary":"done"}`)),
			completeNodeCall("call_complete_2", json.RawMessage(`{"commentary":"complete","summary":"done"}`)),
		),
		commentaryResponse("complete", completeNodeCall("call_complete_3", json.RawMessage(`{"commentary":"complete","summary":"done"}`))),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeTool), Config{})
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
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		structuredFinalResponse(`{"commentary":"complete","summary":"done"}`),
		structuredFinalResponse("unexpected"),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeStructuredOutput), Config{})
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertModelCallCount(t, client, 1)
	if got := controller.completed.Load(); got != 1 {
		t.Fatalf("completions = %d, want 1", got)
	}
}

func TestWorkflowInvalidCompletionAttemptsInterruptAtCap(t *testing.T) {
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		commentaryResponse("bad", completeNodeCall("call_bad_1", json.RawMessage(`{"summary":1}`))),
		commentaryResponse("bad", completeNodeCall("call_bad_2", json.RawMessage(`{"summary":1}`))),
		structuredFinalResponse("unexpected"),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeTool), Config{})
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertModelCallCount(t, client, 2)
	if got := controller.maxHits.Load(); got != 1 {
		t.Fatalf("max hits = %d, want 1", got)
	}
}

func TestWorkflowFinalAnswerViolationsInterruptAtCap(t *testing.T) {
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		structuredFinalResponse("done 1"),
		structuredFinalResponse("done 2"),
		structuredFinalResponse("done 3"),
		structuredFinalResponse("unexpected"),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeTool), Config{})
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertModelCallCount(t, client, 3)
	if got := controller.maxHits.Load(); got != 1 {
		t.Fatalf("max hits = %d, want 1", got)
	}
	assertDeveloperErrorFeedbackAfterAssistantFinal(t, eng, "done 1", strings.TrimSpace(prompts.WorkflowFinalAnswerNudgePrompt))
}

func TestWorkflowEmptyFinalAnswerViolationsInterruptAtCap(t *testing.T) {
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		structuredFinalResponse(""),
		structuredFinalResponse(""),
		structuredFinalResponse(""),
		structuredFinalResponse("unexpected"),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeTool), Config{})
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertModelCallCount(t, client, 3)
	if got := controller.maxHits.Load(); got != 1 {
		t.Fatalf("max hits = %d, want 1", got)
	}
}

func TestWorkflowStructuredEmptyFinalInterruptsAtInvalidCompletionCap(t *testing.T) {
	store := mustCreateTestSession(t)
	controller := &fakeWorkflowController{}
	client := &fakeClient{responses: []llm.Response{
		structuredFinalResponse(""),
		structuredFinalResponse(""),
		structuredFinalResponse("unexpected"),
	}}
	eng := mustNewWorkflowTestEngine(t, store, client, testWorkflowConfig(controller, config.WorkflowCompletionModeStructuredOutput), Config{})
	if _, err := eng.SubmitUserMessage(context.Background(), "run"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertModelCallCount(t, client, 2)
	if got := controller.maxHits.Load(); got != 1 {
		t.Fatalf("max hits = %d, want 1", got)
	}
}

func assertCompletionSchema(t *testing.T, schema json.RawMessage, parameterDescriptions map[string]string) {
	t.Helper()
	root := schemaRoot(t, schema)
	if got := root["additionalProperties"]; got != false {
		t.Fatalf("schema additionalProperties = %v, want false in %s", got, string(schema))
	}
	if _, ok := schemaProperties(t, schema)["transition"]; ok {
		t.Fatalf("single-transition schema should infer transition instead of advertising it: %s", string(schema))
	}
	assertSchemaProperty(t, schema, "commentary", "string", "Brief explanation of what was completed and why this transition was selected.")
	required := []string{"commentary"}
	for name, description := range parameterDescriptions {
		assertSchemaProperty(t, schema, name, "string", description)
		required = append(required, name)
	}
	assertSchemaRequiredFields(t, schema, required)
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

func schemaProperty(t *testing.T, schema json.RawMessage, name string) map[string]any {
	t.Helper()
	properties := schemaProperties(t, schema)
	property, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("schema property %s missing: %s", name, string(schema))
	}
	return property
}

func schemaProperties(t *testing.T, schema json.RawMessage) map[string]any {
	t.Helper()
	root := schemaRoot(t, schema)
	properties, ok := root["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %s", string(schema))
	}
	return properties
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

func assertSchemaRequiredFields(t *testing.T, schema json.RawMessage, expected []string) {
	t.Helper()
	required := schemaRequired(t, schema)
	seen := map[string]bool{}
	for _, name := range required {
		if seen[name] {
			t.Fatalf("schema required contains duplicate %s, required=%+v schema=%s", name, required, string(schema))
		}
		seen[name] = true
	}
	if len(seen) != len(expected) {
		t.Fatalf("schema required = %+v, want exactly %+v in %s", required, expected, string(schema))
	}
	for _, name := range expected {
		if !seen[name] {
			t.Fatalf("schema required missing %s, required=%+v schema=%s", name, required, string(schema))
		}
	}
}

func assertDeveloperErrorFeedbackAfterAssistantFinal(t *testing.T, eng *Engine, assistantContent string, feedbackContent string) {
	t.Helper()
	messages := eng.snapshotMessages()
	for index, message := range messages {
		if message.Role != llm.RoleAssistant || message.Phase != llm.MessagePhaseFinal || message.Content != assistantContent {
			continue
		}
		nextIndex := index + 1
		if nextIndex >= len(messages) {
			t.Fatalf("assistant final %q had no following message: %+v", assistantContent, messages)
		}
		next := messages[nextIndex]
		if next.Role != llm.RoleDeveloper || next.MessageType != llm.MessageTypeErrorFeedback || next.Content != feedbackContent {
			t.Fatalf("message after assistant final %q = %+v, want developer error feedback %q; messages=%+v", assistantContent, next, feedbackContent, messages)
		}
		return
	}
	t.Fatalf("assistant final %q not found in messages: %+v", assistantContent, messages)
}

func schemaRoot(t *testing.T, schema json.RawMessage) map[string]any {
	t.Helper()
	root := map[string]any{}
	if err := json.Unmarshal(schema, &root); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	return root
}
