package runtime

import (
	"context"

	"builder/server/llm"
	"builder/shared/cachewarn"
)

func reviewerSuggestionsStructuredOutput() *llm.StructuredOutput {
	return &llm.StructuredOutput{
		Name: "reviewer_suggestions",
		Schema: mustJSON(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"suggestions": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
				},
			},
			"required": []string{"suggestions"},
		}),
		Strict: true,
	}
}

func (e *Engine) buildReviewerRequest(ctx context.Context, reviewerClient llm.Client) (llm.Request, error) {
	reviewerCfg := e.reviewerRequestConfigSnapshot()
	messages := llm.MessagesFromItems(sanitizeItemsForLLM(e.snapshotItems()))
	reviewerMessages, err := buildReviewerRequestMessagesWithBuilder(messages, newActiveMetaContextBuilder(e.store.Meta(), e.cfg.Model, e.ThinkingLevel(), e.cfg.DisabledSkills, e.reviewerMetaTimestamp()), e.cfg.HeadlessMode)
	if err != nil {
		return llm.Request{}, err
	}
	systemPrompt, err := e.reviewerSystemPrompt()
	if err != nil {
		return llm.Request{}, err
	}
	reviewerItems := sanitizeItemsForLLM(llm.ItemsFromMessages(reviewerMessages))
	req := llm.Request{
		Model:                   reviewerCfg.Model,
		Temperature:             1,
		MaxTokens:               0,
		FastMode:                e.FastModeEnabled(),
		ReasoningEffort:         reviewerCfg.ThinkingLevel,
		SupportsReasoningEffort: reviewerCfg.ModelCapabilities.SupportsReasoningEffort,
		SystemPrompt:            systemPrompt,
		SessionID:               reviewerSessionID(e.store.Meta().SessionID),
		Items:                   reviewerItems,
		Tools:                   []llm.Tool{},
		StructuredOutput:        reviewerSuggestionsStructuredOutput(),
	}
	if supportsPromptCacheKeyForClient(ctx, reviewerClient) {
		if cacheKey := reviewerPromptCacheKey(e.store.Meta().SessionID, e.compactionCountSnapshot()); cacheKey != "" {
			req.PromptCacheKey = cacheKey
			req.PromptCacheScope = cachewarn.ScopeReviewer
		}
	}
	if err := req.Validate(); err != nil {
		return llm.Request{}, err
	}
	return req, nil
}
