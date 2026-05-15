package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"builder/server/llm"
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
	contextLimit := e.effectiveContextTokenLimit()
	requestItems := compactionConversationReplicaItems(input)
	baseRequest := llm.CompactionRequest{
		Model:        locked.Model,
		Instructions: instructions,
		SessionID:    e.store.Meta().SessionID,
		InputItems:   requestItems,
	}

	resp, _, extraTrimmed, err := e.compactWithContextTrimRetry(ctx, stepID, compactor, baseRequest, contextLimit)
	if err != nil {
		return compactionResult{}, err
	}

	sanitized, err := sanitizeRemoteCompactionOutput(resp.OutputItems)
	if err != nil {
		return compactionResult{}, err
	}
	replacement, err := e.buildCanonicalCompactionReplacement(sanitized)
	if err != nil {
		return compactionResult{}, err
	}
	return compactionResult{
		engine:            "remote",
		items:             replacement,
		usage:             resp.Usage,
		trimmedItemsCount: extraTrimmed + resp.TrimmedItemsCount,
		provider:          providerID,
	}, nil
}

func compactionConversationReplicaItems(items []llm.ResponseItem) []llm.ResponseItem {
	return llm.CloneResponseItems(items)
}

func compactionConversationWithPromptItems(items []llm.ResponseItem, instructions string) []llm.ResponseItem {
	conversation := compactionConversationReplicaItems(items)
	prompt := strings.TrimSpace(instructions)
	if prompt == "" {
		return conversation
	}
	return append(conversation, llm.ResponseItem{Type: llm.ResponseItemTypeMessage, Role: llm.RoleDeveloper, Content: prompt})
}

func (e *Engine) compactWithContextTrimRetry(
	ctx context.Context,
	stepID string,
	client llm.CompactionClient,
	request llm.CompactionRequest,
	limit int,
) (llm.CompactionResponse, []llm.ResponseItem, int, error) {
	currentInput := llm.CloneResponseItems(request.InputItems)
	additionalTrimmed := 0

	for attempt := 0; attempt <= compactOverflowRetries; attempt++ {
		req := request
		req.InputItems = llm.CloneResponseItems(currentInput)

		resp, err := e.compactWithRetry(ctx, stepID, client, req)
		if err == nil {
			return resp, currentInput, additionalTrimmed, nil
		}
		if !isCompactionContextOverflow(err) || attempt == compactOverflowRetries {
			return llm.CompactionResponse{}, nil, additionalTrimmed, err
		}

		nextInput, trimmed := e.trimCompactionInputToLimit(ctx, request.Model, request.Instructions, currentInput, limit)
		if trimmed == 0 {
			nextInput, trimmed = trimOldestEligibleItems(currentInput, 1+attempt)
		}
		if trimmed == 0 {
			return llm.CompactionResponse{}, nil, additionalTrimmed, err
		}
		currentInput = nextInput
		additionalTrimmed += trimmed
	}

	return llm.CompactionResponse{}, nil, additionalTrimmed, errors.New("compaction context trim retry exhausted")
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
	req, err := llm.RequestFromLockedContract(locked, systemPrompt, sanitizeItemsForLLM(items), e.requestTools(ctx, ""))
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

func isCompactionContextOverflow(err error) bool {
	return llm.IsContextLengthOverflowError(err)
}

func (e *Engine) compactLocal(ctx context.Context, input []llm.ResponseItem, providerID string, instructions string, mode compactionMode) (compactionResult, error) {
	summary, err := e.localCompactionSummary(ctx, input, instructions, mode)
	if err != nil {
		return compactionResult{}, err
	}
	replacement, err := e.buildCanonicalCompactionReplacement([]llm.ResponseItem{{
		Type:        llm.ResponseItemTypeMessage,
		Role:        llm.RoleDeveloper,
		MessageType: llm.MessageTypeCompactionSummary,
		Content:     strings.TrimSpace(summary),
	}})
	if err != nil {
		return compactionResult{}, err
	}
	usageInputTokens := estimateItemsTokens(replacement)
	if preciseInput, ok := e.inputTokensForItems(ctx, e.currentModel(), "", replacement); ok {
		usageInputTokens = preciseInput
	}
	return compactionResult{
		engine:            "local",
		items:             replacement,
		usage:             llm.Usage{InputTokens: usageInputTokens, WindowTokens: e.contextWindowTokens()},
		trimmedItemsCount: 0,
		provider:          providerID,
		summary:           strings.TrimSpace(summary),
	}, nil
}

func (e *Engine) localCompactionSummary(ctx context.Context, input []llm.ResponseItem, instructions string, mode compactionMode) (string, error) {
	locked, err := e.ensureLocked()
	if err != nil {
		return "", err
	}
	window := localCompactionWindow(input)
	items := append(window, llm.ResponseItem{
		Type:    llm.ResponseItemTypeMessage,
		Role:    llm.RoleDeveloper,
		Content: instructions,
	})
	items = sanitizeItemsForLLM(items)

	systemPrompt, err := e.systemPrompt(locked)
	if err != nil {
		return "", err
	}
	requestTools := e.requestTools(ctx, "")
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
	return sanitizeItemsForLLM(items), nil
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
