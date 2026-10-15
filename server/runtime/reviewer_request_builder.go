package runtime

import (
	"context"
	"fmt"

	"core/server/llm"
	"core/shared/jsoncontract"
	"core/shared/transcript"
)

type reviewerSuggestionsPayload struct {
	Suggestions []string `json:"suggestions"`
}

func prepareReviewerSuggestionsContract(
	preparer jsoncontract.Preparer,
) (jsoncontract.Structured, error) {
	return preparer.Structured("reviewer suggestions", reviewerSuggestionsPayload{})
}

func reviewerSuggestionsStructuredOutput(contract jsoncontract.Structured) *llm.StructuredOutput {
	return &llm.StructuredOutput{
		Name:   "reviewer_suggestions",
		Schema: contract,
	}
}

func (e *Engine) buildReviewerRequest(ctx context.Context, reviewerClient *observedModelClient) (llm.Request, error) {
	if _, err := e.ensureLocked(); err != nil {
		return llm.Request{}, err
	}
	reviewerCfg := e.reviewerRequestConfigSnapshot()
	effort := llm.ProviderThinkingEffort(reviewerCfg.Model, reviewerCfg.ThinkingLevel)
	if err := llm.ValidateModelReasoningEffort(reviewerCfg.Model, effort); err != nil {
		return llm.Request{}, err
	}
	reviewerItems, err := buildReviewerRequestItemsWithBuilder(e.transcriptRuntimeState().SnapshotItems(), newActiveMetaContextBuilder(e.store, e.transcriptWorkingDir(), e.cfg.Model, e.ThinkingLevel(), e.cfg.GlobalConfigDir, e.cfg.SkillPolicy, e.reviewerMetaTimestamp()), e.cfg.HeadlessMode)
	if err != nil {
		return llm.Request{}, err
	}
	systemPrompt, err := e.reviewerSystemPrompt(ctx)
	if err != nil {
		return llm.Request{}, err
	}
	req := llm.Request{
		Model:                   reviewerCfg.Model,
		Temperature:             1,
		MaxTokens:               0,
		FastMode:                e.FastModeEnabled(),
		ReasoningEffort:         effort,
		SupportsReasoningEffort: reviewerCfg.ModelCapabilities.SupportsReasoningEffort,
		SystemPrompt:            systemPrompt,
		Items:                   reviewerItems,
		Tools:                   []llm.Tool{},
		ToolChoiceMode:          llm.ToolChoiceModeAutomatic,
		StructuredOutput:        reviewerSuggestionsStructuredOutput(e.reviewerSuggestionsContract),
	}
	if supportsPromptCacheKeyForClient(ctx, reviewerClient) {
		if cacheKey := e.conversationPromptCacheKey(reviewerSessionID(e.store.Meta().SessionID)); cacheKey != "" {
			req.PromptCacheKey = cacheKey
			req.PromptCacheScope = transcript.CacheWarningScopeReviewer
		}
	}
	if err := req.Validate(); err != nil {
		return llm.Request{}, err
	}
	return req, nil
}

func (e *Engine) buildReviewerDispatchRequest(
	ctx context.Context,
	stepID string,
	reviewerClient *observedModelClient,
) (llm.Request, error) {
	req, err := e.buildReviewerRequest(ctx, reviewerClient)
	if err != nil {
		return llm.Request{}, err
	}
	active := e.ActiveRun()
	if active == nil || active.StepID != stepID {
		return llm.Request{}, fmt.Errorf(
			"%w: enclosing Agent Turn Step identity is required for Reviewer dispatch",
			llm.ErrInvalidRequest,
		)
	}
	lifetime, ok := e.stepLifecycle.TurnStateLifetimeForStep(stepID)
	if !ok {
		return llm.Request{}, fmt.Errorf(
			"%w: enclosing Agent Turn routing lifetime is required for Reviewer dispatch",
			llm.ErrInvalidRequest,
		)
	}
	factory, err := newDispatchRequestFactory(dispatchRequestIdentity{
		SessionID:               reviewerSessionID(e.store.Meta().SessionID),
		StepID:                  active.StepID,
		CompactionGeneration:    e.compactionRuntimeState().Count(),
		RequestKind:             llm.CodexRequestKindTurn.Optional(),
		TurnStateLifetime:       llm.NewCodexTurnStateLifetime(),
		SharedTurnStateLifetime: lifetime,
	})
	if err != nil {
		return llm.Request{}, err
	}
	return factory.generation(req)
}
