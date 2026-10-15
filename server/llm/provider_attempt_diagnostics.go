package llm

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"core/shared/config"
	"core/shared/textutil"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
)

type providerAttemptOperation string

const (
	providerAttemptGenerate providerAttemptOperation = "generation"
	providerAttemptCompact  providerAttemptOperation = "compaction"
)

// Attempts are diagnostic only: they neither change provider requests nor add
// anything to model-visible history. Missing usage is not a zero-token charge.
type providerAttemptDiagnostic struct {
	logger          *slog.Logger
	started         time.Time
	responseID      *string
	servedModel     *string
	servedTier      *string
	usage           *Usage
	totalTokens     *int64
	reasoningTokens *int64
}

func startProviderAttempt(
	ctx context.Context,
	connectionID *config.ConnectionID,
	request ResponsesRequest,
	preparation responsesDispatchPreparation,
	requestedTier *string,
	operation providerAttemptOperation,
) *providerAttemptDiagnostic {
	attempt := &providerAttemptDiagnostic{
		started: time.Now(),
		logger: slog.Default().With(
			"attempt_id", uuid.NewString(),
			"operation", operation,
			"session_id", diagnosticOptional(request.SessionID),
			"connection_id", diagnosticOptional(connectionID),
			"account_id", diagnosticOptional(textutil.OptionalTrimmedString(preparation.mode.AccountID)),
			"provider_id", preparation.variant.ProviderID,
			"model", request.Model,
			"cache_key", diagnosticOptional(textutil.OptionalTrimmedString(request.PromptCacheKey)),
			"requested_service_tier", diagnosticOptional(requestedTier),
		),
	}
	attempt.logger.InfoContext(ctx, "provider attempt", "kind", "started")
	return attempt
}

func (a *providerAttemptDiagnostic) observeResponse(response responses.Response, windowTokens int) {
	if a == nil {
		return
	}
	if id := textutil.OptionalTrimmedString(response.ID); id != nil {
		a.responseID = id
	}
	if model := textutil.OptionalTrimmedString(string(response.Model)); model != nil {
		a.servedModel = model
	}
	if tier := textutil.OptionalTrimmedString(string(response.ServiceTier)); tier != nil {
		a.servedTier = tier
	}
	if response.JSON.Usage.Valid() {
		usage := usageFromSDK(response.Usage, windowTokens)
		a.usage = &usage
		if response.Usage.JSON.TotalTokens.Valid() {
			a.totalTokens = textutil.Value(response.Usage.TotalTokens)
		}
		if response.Usage.JSON.OutputTokensDetails.Valid() &&
			response.Usage.OutputTokensDetails.JSON.ReasoningTokens.Valid() {
			a.reasoningTokens = textutil.Value(response.Usage.OutputTokensDetails.ReasoningTokens)
		}
	}
}

func (a *providerAttemptDiagnostic) finish(ctx context.Context, response *http.Response, err error) {
	outcome := "completed"
	if err != nil {
		outcome = "failed"
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			outcome = "canceled"
		}
	}
	var status *int
	var requestID *string
	if response != nil {
		status = textutil.Value(response.StatusCode)
		requestID = textutil.OptionalTrimmedString(response.Header.Get("x-request-id"))
	}
	var code *UnifiedErrorCode
	var providerError *ProviderAPIError
	if errors.As(err, &providerError) {
		code = &providerError.Code
		if requestID == nil {
			requestID = providerError.ProviderRequestID
		}
	}
	usage := Usage{}
	if a.usage != nil {
		usage = *a.usage
	}
	a.logger.InfoContext(ctx, "provider attempt",
		"kind", "finished",
		"outcome", outcome,
		"duration_ms", time.Since(a.started).Milliseconds(),
		"http_status", diagnosticOptional(status),
		"provider_request_id", diagnosticOptional(requestID),
		"response_id", diagnosticOptional(a.responseID),
		"served_model", diagnosticOptional(a.servedModel),
		"served_service_tier", diagnosticOptional(a.servedTier),
		"error_code", diagnosticOptional(code),
		"stream_stalled", errors.Is(err, ErrModelStreamStalled),
		"deadline_exceeded", errors.Is(err, context.DeadlineExceeded),
		"usage_reported", a.usage != nil,
		"input_tokens", diagnosticOptional(usage.InputTokens),
		"cached_input_tokens", diagnosticOptional(usage.CachedInputTokens),
		"output_tokens", diagnosticOptional(usage.OutputTokens),
		"total_tokens", diagnosticOptional(a.totalTokens),
		"reasoning_tokens", diagnosticOptional(a.reasoningTokens),
	)
}

func diagnosticOptional[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}
