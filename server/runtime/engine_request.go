package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"builder/server/llm"
	"builder/server/session"
	"builder/server/tools"
	"builder/server/workflowruntime"
	"builder/shared/cachewarn"
	compactionutil "builder/shared/compaction"
	"builder/shared/toolspec"
	xansi "github.com/charmbracelet/x/ansi"
)

type requestBuildPlan struct {
	Request llm.Request
}

func (e *Engine) buildRequest(ctx context.Context, stepID string, allowTools bool) (llm.Request, error) {
	plan, err := e.buildRequestPlan(ctx, stepID, allowTools)
	if err != nil {
		return llm.Request{}, err
	}
	return plan.Request, nil
}

func (e *Engine) buildRequestWithExtraItems(ctx context.Context, stepID string, extra []llm.ResponseItem, allowTools bool) (llm.Request, error) {
	plan, err := e.buildRequestPlanWithExtraItems(ctx, stepID, extra, allowTools)
	if err != nil {
		return llm.Request{}, err
	}
	return plan.Request, nil
}

func (e *Engine) buildRequestPlan(ctx context.Context, stepID string, allowTools bool) (requestBuildPlan, error) {
	return e.buildRequestPlanWithExtraItems(ctx, stepID, nil, allowTools)
}

func (e *Engine) buildRequestPlanWithExtraItems(ctx context.Context, stepID string, extra []llm.ResponseItem, allowTools bool) (requestBuildPlan, error) {
	locked, err := e.ensureLocked()
	if err != nil {
		return requestBuildPlan{}, err
	}

	var workflowMode workflowruntime.CompletionMode
	if e.workflowRunActive() {
		resolved, modeErr := e.workflowCompletionMode(ctx)
		if modeErr != nil {
			return requestBuildPlan{}, modeErr
		}
		workflowMode = resolved
	}
	var requestTools []llm.Tool
	if allowTools {
		requestTools = e.requestTools(ctx, workflowMode)
	} else {
		requestTools = []llm.Tool{}
	}

	items := filterHistoricalWorktreeReminderItems(e.snapshotItems())
	if len(extra) > 0 {
		items = append(items, llm.CloneResponseItems(extra)...)
	}
	items = sanitizeItemsForLLM(items)

	systemPrompt, err := e.systemPrompt(locked)
	if err != nil {
		return requestBuildPlan{}, err
	}
	req, err := llm.RequestFromLockedContract(locked, systemPrompt, items, requestTools)
	if err != nil {
		return requestBuildPlan{}, err
	}
	req.ReasoningEffort = e.ThinkingLevel()
	req.FastMode = e.FastModeEnabled()
	req.SessionID = e.conversationSessionID()
	if e.supportsPromptCacheKey(ctx) {
		if cacheKey := e.conversationPromptCacheKey(); cacheKey != "" {
			req.PromptCacheKey = cacheKey
			req.PromptCacheScope = cachewarn.ScopeConversation
		}
	}
	if allowTools {
		nativeWebSearch, nativeErr := e.enableNativeWebSearch(ctx)
		if nativeErr != nil {
			return requestBuildPlan{}, nativeErr
		}
		req.EnableNativeWebSearch = nativeWebSearch
	}
	if workflowMode != "" {
		if workflowMode == workflowruntime.CompletionModeStructuredOutput {
			output, outputErr := workflowruntime.StructuredOutput(e.cfg.WorkflowRun.Contract)
			if outputErr != nil {
				return requestBuildPlan{}, outputErr
			}
			req.StructuredOutput = output
		}
		if err := req.Validate(); err != nil {
			return requestBuildPlan{}, err
		}
	}
	return requestBuildPlan{Request: req}, nil
}

func (e *Engine) supportsPromptCacheKey(ctx context.Context) bool {
	caps, err := e.providerCapabilities(ctx)
	if err != nil {
		return false
	}
	return llm.SupportsPromptCacheKeyProvider(caps)
}

func supportsPromptCacheKeyForClient(ctx context.Context, client llm.Client) bool {
	if client == nil {
		return false
	}
	provider, ok := client.(llm.ProviderCapabilitiesClient)
	if !ok {
		return false
	}
	caps, err := provider.ProviderCapabilities(ctx)
	if err != nil {
		return false
	}
	return llm.SupportsPromptCacheKeyProvider(caps)
}

func (e *Engine) enableNativeWebSearch(ctx context.Context) (bool, error) {
	if !tools.NeedsNativeWebSearch(e.cfg.EnabledTools, e.cfg.WebSearchMode) {
		return false, nil
	}
	caps, err := e.providerCapabilities(ctx)
	if err != nil {
		return false, fmt.Errorf("resolve provider capabilities for native web search: %w", err)
	}
	return caps.SupportsNativeWebSearch, nil
}

func (e *Engine) workflowRunActive() bool {
	return e != nil && e.cfg.WorkflowRun != nil && strings.TrimSpace(string(e.cfg.WorkflowRun.Contract.RunID)) != ""
}

func (e *Engine) workflowCompletionMode(ctx context.Context) (workflowruntime.CompletionMode, error) {
	if !e.workflowRunActive() {
		return "", nil
	}
	caps, err := e.providerCapabilities(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve provider capabilities for workflow completion: %w", err)
	}
	return workflowruntime.SelectCompletionMode(e.cfg.WorkflowRun.CompletionMode, caps)
}

func (e *Engine) systemPrompt(locked session.LockedContract) (string, error) {
	if locked.HasSystemPrompt {
		return strings.TrimSpace(locked.SystemPrompt), nil
	}
	if prompt := strings.TrimSpace(locked.SystemPrompt); prompt != "" {
		return prompt, nil
	}
	prompt, err := e.buildSystemPromptSnapshot(locked)
	if err != nil {
		return "", err
	}
	if err := e.store.BackfillLockedSystemPrompt(prompt); err != nil {
		return "", err
	}
	if meta := e.store.Meta(); meta.Locked != nil && meta.Locked.HasSystemPrompt {
		persisted := strings.TrimSpace(meta.Locked.SystemPrompt)
		prompt = persisted
	}
	e.lockedContractState().FillSystemPrompt(prompt)
	return prompt, nil
}

func (e *Engine) estimatedToolCallsForLockedContext(locked session.LockedContract) int {
	budget := e.promptContextBudget(locked)
	return compactionutil.EstimatedToolCallsForContextWindow(budget.window, budget.percent)
}

type promptContextBudget struct {
	window  int
	percent int
}

func (e *Engine) promptContextBudget(locked session.LockedContract) promptContextBudget {
	budget := e.promptContextBudgetFromConfig()
	if locked.ContextWindow > 0 {
		budget.window = locked.ContextWindow
	}
	if locked.ContextPercent > 0 {
		budget.percent = locked.ContextPercent
	}
	return budget
}

func (e *Engine) promptContextBudgetFromConfig() promptContextBudget {
	return promptContextBudget{window: e.cfg.ContextWindowTokens, percent: e.cfg.EffectiveContextWindowPercent}
}

func summarizeOutputItemTypes(items []llm.ResponseItem) []string {
	if len(items) == 0 {
		return nil
	}
	counts := make(map[string]int, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		t := strings.TrimSpace(string(item.Type))
		if t == "" {
			t = "unknown"
		}
		if _, ok := counts[t]; !ok {
			order = append(order, t)
		}
		counts[t]++
	}
	out := make([]string, 0, len(order))
	for _, t := range order {
		out = append(out, fmt.Sprintf("%s:%d", t, counts[t]))
	}
	return out
}

type hostedToolExecution struct {
	Call   llm.ToolCall
	Result tools.Result
}

func hostedToolExecutionsFromOutputItems(items []llm.ResponseItem, defs []tools.Definition) []hostedToolExecution {
	hostedOutputs := make([]tools.HostedToolOutput, 0, len(items))
	for _, item := range items {
		hostedOutputs = append(hostedOutputs, tools.HostedToolOutput{
			ID:     strings.TrimSpace(item.ID),
			CallID: strings.TrimSpace(item.CallID),
			Raw:    append(json.RawMessage(nil), item.Raw...),
		})
	}
	decoded := tools.HostedExecutionsFromOutputs(hostedOutputs, defs)
	out := make([]hostedToolExecution, 0, len(decoded))
	for _, execution := range decoded {
		out = append(out, hostedToolExecution{
			Call: llm.ToolCall{
				ID:    execution.Call.ID,
				Name:  string(execution.Call.Name),
				Input: execution.Call.Input,
			},
			Result: execution.Result,
		})
	}
	return out
}

func (e *Engine) requestTools(ctx context.Context, workflowMode workflowruntime.CompletionMode) []llm.Tool {
	workflowToolMode := workflowMode == workflowruntime.CompletionModeTool
	exposure := tools.RequestExposureContext{
		SupportsVision:     llm.LockedContractSupportsVisionInputs(e.store.Meta().Locked, e.cfg.Model),
		WorkflowCompletion: workflowToolMode,
	}
	defs := tools.RequestExposedDefinitionsForSession(e.cfg.EnabledTools, e.registry.Definitions(), exposure)
	if workflowToolMode {
		if def, ok := tools.DefinitionFor(toolspec.ToolCompleteNode); ok && !definitionListContains(defs, toolspec.ToolCompleteNode) {
			defs = append(defs, def)
		}
	}
	if len(defs) == 0 {
		return nil
	}
	out := make([]llm.Tool, 0, len(defs))
	customPatchSupported := e.supportsCustomPatchTool(ctx)
	for _, d := range defs {
		tool := llm.Tool{Name: string(d.ID), Description: d.Description, Schema: d.Schema}
		if d.ID == toolspec.ToolCompleteNode {
			schema, err := workflowruntime.CompletionJSONSchema(e.cfg.WorkflowRun.Contract)
			if err != nil {
				continue
			}
			tool.Schema = schema
		}
		if d.ID == toolspec.ToolPatch && customPatchSupported {
			tool.Schema = nil
			tool.Custom = &llm.CustomToolFormat{Type: "grammar", Syntax: "lark", Definition: llm.PatchToolLarkGrammar}
		}
		out = append(out, tool)
	}
	return out
}

func definitionListContains(defs []tools.Definition, id toolspec.ID) bool {
	for _, def := range defs {
		if def.ID == id {
			return true
		}
	}
	return false
}

func (e *Engine) supportsCustomPatchTool(ctx context.Context) bool {
	caps, err := e.providerCapabilities(ctx)
	if err != nil {
		return false
	}
	return caps.SupportsResponsesAPI && caps.IsOpenAIFirstParty
}

func sanitizeItemsForLLM(items []llm.ResponseItem) []llm.ResponseItem {
	if len(items) == 0 {
		return items
	}
	cleaned := llm.CloneResponseItems(items)
	for i := range cleaned {
		if cleaned[i].Type == llm.ResponseItemTypeMessage {
			cleaned[i].Content = xansi.Strip(cleaned[i].Content)
		}
		if (cleaned[i].Type == llm.ResponseItemTypeFunctionCallOutput || cleaned[i].Type == llm.ResponseItemTypeCustomToolOutput) && len(cleaned[i].Output) > 0 {
			normalized := normalizeToolMessageForLLM(string(cleaned[i].Output))
			if json.Valid([]byte(normalized)) {
				cleaned[i].Output = json.RawMessage(normalized)
			} else {
				quoted, _ := json.Marshal(normalized)
				cleaned[i].Output = quoted
			}
		}
	}
	return cleaned
}

func normalizeToolMessageForLLM(content string) string {
	var payload any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return content
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return content
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
