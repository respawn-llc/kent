package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"builder/server/llm"
	"builder/server/session"
	"builder/shared/cachewarn"
)

func (e *Engine) compactRemote(ctx context.Context, stepID string, input []llm.ResponseItem, providerID string, instructions string) (compactionResult, error) {
	compactor, ok := e.llm.(llm.CompactionClient)
	if !ok {
		return compactionResult{}, errors.New("llm client does not support remote compaction")
	}
	locked, err := e.ensureLocked()
	if err != nil {
		return compactionResult{}, err
	}
	requestItems := llm.CloneResponseItems(input)
	baseRequest := llm.CompactionRequest{
		Model:        locked.Model,
		Instructions: instructions,
		SessionID:    e.store.Meta().SessionID,
		InputItems:   requestItems,
	}

	resp, _, repairStats, err := e.compactWithContextRepairRetry(ctx, stepID, compactor, baseRequest)
	if err != nil {
		return compactionResult{}, err
	}

	sanitized, err := sanitizeRemoteCompactionOutput(resp.OutputItems)
	if err != nil {
		return compactionResult{}, err
	}
	replacement := compactionSeedItems(sanitized)
	return compactionResult{
		engine:            "remote",
		items:             replacement,
		usage:             resp.Usage,
		trimmedItemsCount: resp.TrimmedItemsCount,
		overflowRepair:    repairStats,
		provider:          providerID,
	}, nil
}

func compactionConversationWithPromptItems(items []llm.ResponseItem, instructions string) []llm.ResponseItem {
	conversation := llm.CloneResponseItems(items)
	prompt := strings.TrimSpace(instructions)
	if prompt == "" {
		return conversation
	}
	return append(conversation, llm.ResponseItem{Type: llm.ResponseItemTypeMessage, Role: llm.RoleDeveloper, Content: prompt})
}

func (e *Engine) compactWithContextRepairRetry(
	ctx context.Context,
	stepID string,
	client llm.CompactionClient,
	request llm.CompactionRequest,
) (llm.CompactionResponse, []llm.ResponseItem, compactionOverflowRepairStats, error) {
	currentInput := llm.CloneResponseItems(request.InputItems)
	repairStats := compactionOverflowRepairStats{}
	contextWindowTokens := e.contextWindowTokens()

	for attempt := 0; attempt <= len(compactionOverflowRepairTargetPercents); attempt++ {
		req := request
		req.InputItems = llm.CloneResponseItems(currentInput)

		resp, err := e.compactWithRetry(ctx, stepID, client, req)
		if err == nil {
			return resp, currentInput, repairStats, nil
		}
		if !llm.IsContextLengthOverflowError(err) || attempt == len(compactionOverflowRepairTargetPercents) {
			return llm.CompactionResponse{}, nil, repairStats, err
		}

		targetSavedTokens := compactionOverflowRepairTargetTokens(contextWindowTokens, attempt+1)
		nextInput, repaired := collapseCompactionOverflowToolPayloadsAfterSavings(currentInput, targetSavedTokens, repairStats.EstimatedSavedTokens)
		if !repaired.Collapsed() {
			// Only known tool payloads are safe to collapse here. Ordinary
			// conversation history must not be trimmed or request-shaped at
			// compaction time, so fail instead of retrying the same payload.
			return llm.CompactionResponse{}, nil, repairStats, err
		}
		currentInput = nextInput
		repairStats = repairStats.Add(repaired)
	}

	return llm.CompactionResponse{}, nil, repairStats, errors.New("compaction context repair retry exhausted")
}

func (e *Engine) compactWithRetry(ctx context.Context, stepID string, client llm.CompactionClient, request llm.CompactionRequest) (llm.CompactionResponse, error) {
	prepared, err := e.prepareCompactionCacheObservation(ctx, request)
	if err != nil {
		return llm.CompactionResponse{}, err
	}
	if err := e.observePromptCacheRequest(stepID, prepared); err != nil {
		return llm.CompactionResponse{}, err
	}

	delays := compactionRetryDelays
	var lastErr error
	for i := 0; i <= len(delays); i++ {
		resp, err := client.Compact(ctx, request)
		if err != nil && ctx.Err() != nil {
			return llm.CompactionResponse{}, ctx.Err()
		}
		if err == nil {
			if err := e.observePromptCacheResponse(stepID, prepared, resp.Usage); err != nil {
				return llm.CompactionResponse{}, err
			}
			return resp, nil
		}
		if llm.IsNonRetriableModelError(err) || llm.IsContextLengthOverflowError(err) {
			return llm.CompactionResponse{}, err
		}
		lastErr = err
		if i == len(delays) {
			break
		}
		if err := waitForRetryDelay(ctx, delays[i]); err != nil {
			return llm.CompactionResponse{}, err
		}
	}
	return llm.CompactionResponse{}, fmt.Errorf("compaction request failed after retries: %w", lastErr)
}

func (e *Engine) prepareCompactionCacheObservation(ctx context.Context, request llm.CompactionRequest) (preparedCacheRequestObservation, error) {
	if e == nil || e.modelRequests().RequestCache() == nil || !e.supportsPromptCacheKey(ctx) {
		return preparedCacheRequestObservation{}, nil
	}
	lineageRequest, ok, err := e.compactionCacheObservationRequest(ctx, request)
	if err != nil || !ok {
		return preparedCacheRequestObservation{}, err
	}
	return e.modelRequests().RequestCache().Prepare(lineageRequest)
}

func (e *Engine) compactionCacheObservationRequest(ctx context.Context, request llm.CompactionRequest) (llm.Request, bool, error) {
	if e == nil {
		return llm.Request{}, false, nil
	}
	cacheKey := e.conversationPromptCacheKey()
	if cacheKey == "" {
		return llm.Request{}, false, nil
	}
	locked, err := e.ensureLocked()
	if err != nil {
		return llm.Request{}, false, err
	}
	items := compactionConversationWithPromptItems(request.InputItems, request.Instructions)
	systemPrompt, err := e.systemPrompt(locked)
	if err != nil {
		return llm.Request{}, false, err
	}
	workflowMode, err := e.workflowCompletionMode(ctx)
	if err != nil {
		return llm.Request{}, false, err
	}
	req, err := llm.RequestFromLockedContract(locked, systemPrompt, items, e.requestTools(ctx, workflowMode))
	if err != nil {
		return llm.Request{}, false, err
	}
	req.ReasoningEffort = e.ThinkingLevel()
	req.FastMode = e.FastModeEnabled()
	req.SessionID = e.conversationSessionID()
	req.PromptCacheKey = cacheKey
	req.PromptCacheScope = cachewarn.ScopeConversation
	return req, true, nil
}

func (e *Engine) compactLocal(ctx context.Context, input []llm.ResponseItem, providerID string, instructions string, mode compactionMode) (compactionResult, error) {
	summary, repairStats, err := e.localCompactionSummaryWithRepair(ctx, input, instructions, mode)
	if err != nil {
		return compactionResult{}, err
	}
	replacement := compactionSeedItems([]llm.ResponseItem{{
		Type:        llm.ResponseItemTypeMessage,
		Role:        llm.RoleDeveloper,
		MessageType: llm.MessageTypeCompactionSummary,
		Content:     strings.TrimSpace(summary),
	}})
	usageInputTokens := estimateItemsTokens(replacement)
	if preciseInput, ok := e.inputTokensForItems(ctx, e.currentModel(), "", replacement); ok {
		usageInputTokens = preciseInput
	}
	return compactionResult{
		engine:            "local",
		items:             replacement,
		usage:             llm.Usage{InputTokens: usageInputTokens, WindowTokens: e.contextWindowTokens()},
		trimmedItemsCount: 0,
		overflowRepair:    repairStats,
		provider:          providerID,
		summary:           strings.TrimSpace(summary),
	}, nil
}

func (e *Engine) localCompactionSummary(ctx context.Context, input []llm.ResponseItem, instructions string, mode compactionMode) (string, error) {
	summary, _, err := e.localCompactionSummaryWithRepair(ctx, input, instructions, mode)
	return summary, err
}

func (e *Engine) localCompactionSummaryWithRepair(ctx context.Context, input []llm.ResponseItem, instructions string, mode compactionMode) (string, compactionOverflowRepairStats, error) {
	locked, err := e.ensureLocked()
	if err != nil {
		return "", compactionOverflowRepairStats{}, err
	}
	systemPrompt, err := e.systemPrompt(locked)
	if err != nil {
		return "", compactionOverflowRepairStats{}, err
	}
	workflowMode, err := e.workflowCompletionMode(ctx)
	if err != nil {
		return "", compactionOverflowRepairStats{}, err
	}
	requestTools := e.requestTools(ctx, workflowMode)
	window := localCompactionWindow(input)
	repairStats := compactionOverflowRepairStats{}
	contextWindowTokens := e.contextWindowTokens()
	for repairAttempt := 0; repairAttempt <= len(compactionOverflowRepairTargetPercents); repairAttempt++ {
		summary, err := e.localCompactionSummaryFromWindow(ctx, locked, systemPrompt, window, instructions, requestTools, mode)
		if err == nil {
			return summary, repairStats, nil
		}
		if !llm.IsContextLengthOverflowError(err) || repairAttempt == len(compactionOverflowRepairTargetPercents) {
			return "", repairStats, err
		}
		targetSavedTokens := compactionOverflowRepairTargetTokens(contextWindowTokens, repairAttempt+1)
		nextWindow, repaired := collapseCompactionOverflowToolPayloadsAfterSavings(window, targetSavedTokens, repairStats.EstimatedSavedTokens)
		if !repaired.Collapsed() {
			// Only known tool payloads are safe to collapse here. Ordinary
			// conversation history must not be trimmed or request-shaped at
			// compaction time, so fail instead of retrying the same payload.
			return "", repairStats, err
		}
		window = nextWindow
		repairStats = repairStats.Add(repaired)
	}
	return "", repairStats, errors.New("local compaction context repair retry exhausted")
}

func (e *Engine) localCompactionSummaryFromWindow(ctx context.Context, locked session.LockedContract, systemPrompt string, window []llm.ResponseItem, instructions string, requestTools []llm.Tool, mode compactionMode) (string, error) {
	items := append(llm.CloneResponseItems(window), llm.ResponseItem{
		Type:    llm.ResponseItemTypeMessage,
		Role:    llm.RoleDeveloper,
		Content: instructions,
	})
	for attempt := 0; ; attempt++ {
		req, err := llm.RequestFromLockedContract(locked, systemPrompt, items, requestTools)
		if err != nil {
			return "", err
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

		resp, err := e.generateWithRetry(ctx, "", req, nil, nil, nil)
		if err != nil {
			return "", err
		}
		if len(resp.ToolCalls) > 0 {
			if mode != compactionModeHandoff || attempt >= handoffCompactionToolCallRetries {
				return "", errors.New("local compaction summary attempted tool calls")
			}
			retryItems, err := handoffCompactionToolCallRetryItems(resp)
			if err != nil {
				return "", err
			}
			items = append(items, retryItems...)
			continue
		}
		summary := strings.TrimSpace(resp.Assistant.Content)
		if summary == "" {
			return "", errors.New("local compaction summary was empty")
		}
		return summary, nil
	}
}

func handoffCompactionToolCallRetryItems(resp llm.Response) ([]llm.ResponseItem, error) {
	if len(resp.ToolCalls) == 0 {
		return nil, nil
	}
	calls := make([]llm.ToolCall, 0, len(resp.ToolCalls))
	for _, call := range resp.ToolCalls {
		if strings.TrimSpace(call.ID) == "" {
			return nil, errors.New("local compaction summary attempted tool call with empty id")
		}
		calls = append(calls, call)
	}
	items := llm.ItemsFromMessages([]llm.Message{{
		Role:      llm.RoleAssistant,
		Content:   resp.Assistant.Content,
		ToolCalls: calls,
	}})
	for _, call := range calls {
		items = append(items, llm.ResponseItem{
			Type:   llm.ToolOutputItemType(call.Custom),
			CallID: strings.TrimSpace(call.ID),
			Name:   call.Name,
			Output: mustJSON(map[string]any{"error": handoffCompactionToolsDisabledMessage}),
		})
	}
	return items, nil
}

func localCompactionWindow(input []llm.ResponseItem) []llm.ResponseItem {
	if len(input) == 0 {
		return nil
	}
	start := 0
	for i := len(input) - 1; i >= 0; i-- {
		if isCompactionBoundaryItem(input[i]) {
			start = i
			break
		}
	}
	window := llm.CloneResponseItems(input[start:])
	return window
}

func isCompactionBoundaryItem(item llm.ResponseItem) bool {
	if item.Type == llm.ResponseItemTypeCompaction {
		return true
	}
	if item.Type == llm.ResponseItemTypeMessage {
		return item.MessageType == llm.MessageTypeCompactionSummary
	}
	return false
}
