package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"core/server/auth"
	"core/shared/config"
	"core/shared/llmerrors"
	"core/shared/modelcontract"
	"core/shared/textutil"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
)

const (
	defaultOpenAIBaseURL   = "https://api.openai.com/v1"
	codexResponsesEndpoint = "https://chatgpt.com/backend-api/codex/responses"
	reasoningRoleSummary   = "reasoning"
)

const openAIResponsesStreamEndedBeforeTerminalMessage = "OpenAI-compatible Responses SSE stream ended before a terminal Responses event"

type DispatchAuthProvider interface {
	// A nil result explicitly selects auth-less access; errors never do.
	ResolveDispatchAuth(context.Context) (*DispatchAuth, error)
}

type DispatchAuth struct {
	Header string
	Mode   OpenAIAuthMode
}

type OpenAIAuthMode struct {
	IsOAuth   bool
	AccountID string
}

type openAIDispatchPreparation struct {
	authHeader   string
	mode         OpenAIAuthMode
	variant      ProviderVariantContract
	providerCaps ProviderCapabilities
	projection   *codexDispatchProjection
}

type HTTPTransport struct {
	BaseURL                      string
	BaseURLExplicit              bool
	Client                       *http.Client
	Auth                         DispatchAuthProvider
	Provider                     Provider
	Store                        bool
	ModelVerbosity               string
	ContextWindowTokens          int
	ProviderIdentifier           string
	ProviderCapabilitiesOverride *ProviderCapabilities

	mu                  sync.RWMutex
	modelContextWindows map[string]int
}

func NewHTTPTransport(auth DispatchAuthProvider) *HTTPTransport {
	window := 200000
	if raw := strings.TrimSpace(os.Getenv("KENT_CONTEXT_WINDOW")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			window = value
		}
	}
	return &HTTPTransport{
		BaseURL:             defaultOpenAIBaseURL,
		Client:              NewHTTPClient(120 * time.Second),
		Auth:                auth,
		Provider:            ProviderOpenAI,
		ProviderIdentifier:  config.Command,
		ContextWindowTokens: window,
		modelContextWindows: make(map[string]int),
	}
}

func (t *HTTPTransport) providerUserAgent() string {
	return t.ProviderIdentifier + "/" + config.Version
}

func (t *HTTPTransport) Generate(ctx context.Context, request OpenAIRequest, callbacks StreamCallbacks) (OpenAIResponse, error) {
	if t.Client == nil {
		t.Client = NewHTTPClient(120 * time.Second)
	}
	preparation, err := t.prepareDispatch(
		ctx,
		request.SessionID,
		request.Model,
		request.CodexDispatch,
		request.FastMode,
	)
	if err != nil {
		return OpenAIResponse{}, err
	}
	windowTokens := t.resolveContextWindowFallback(ctx, request.Model)

	payload, err := t.buildDispatchPayload(request, preparation.mode, preparation.providerCaps, preparation.projection)
	if err != nil {
		return OpenAIResponse{}, err
	}
	compressionOption := requestCompressionOption(preparation.variant)

	service := responses.NewResponseService(
		option.WithBaseURL(t.serviceBaseURL(preparation.mode)),
		option.WithHTTPClient(t.streamingHTTPClient()),
		option.WithMaxRetries(0),
	)
	reqOpts := t.buildRequestOptions(preparation.authHeader, preparation.mode, request.SessionID, preparation.projection, request.CodexDispatch)
	reqOpts = append(reqOpts, compressionOption)
	var rawResp *http.Response
	reqOpts = append(reqOpts, option.WithResponseInto(&rawResp))

	stream := service.NewStreaming(ctx, payload, reqOpts...)
	defer func() { _ = stream.Close() }()

	turnStateObserver := codexTurnStateObserver(preparation.projection, request.CodexDispatch)
	requestEvidence := t.providerUsageRequestEvidence(request, preparation, payload)
	return consumeResponsesStream(
		ctx,
		stream,
		rawResp,
		turnStateObserver,
		preparation.providerCaps.ProviderID,
		windowTokens,
		callbacks,
		requestEvidence,
	)
}

func consumeResponsesStream(
	ctx context.Context,
	stream *ssestream.Stream[responses.ResponseStreamEventUnion],
	rawResp *http.Response,
	dispatch *CodexDispatchContext,
	providerID string,
	windowTokens int,
	callbacks StreamCallbacks,
	requestEvidence modelcontract.ProviderUsageEvidence,
) (OpenAIResponse, error) {
	accumulator := newResponseStreamAccumulator(callbacks, windowTokens)
	headersObserved := false
	observeCodexTurnStateResponseHeader(dispatch, rawResp, &headersObserved)
	for stream.Next() {
		observeCodexTurnStateResponseHeader(dispatch, rawResp, &headersObserved)
		if callbacks.OnStreamActivity != nil {
			callbacks.OnStreamActivity()
		}
		event := stream.Current()
		accumulator.Consume(event)
		if err := accumulator.Err(providerID, newOpenAIResponseStatus(rawResp)); err != nil {
			return OpenAIResponse{}, newOpenAIRequestErrorMapper(providerID).Map(err, rawResp, "read responses stream events")
		}
	}
	observeCodexTurnStateResponseHeader(dispatch, rawResp, &headersObserved)
	if err := stream.Err(); err != nil {
		if accumulator.hasCompleted() && !callerCanceledStreamRead(ctx) {
			return responseFromStreamAccumulator(accumulator, providerID, rawResp, requestEvidence)
		}
		if rawResp != nil && isOpenAIResponsesStreamFramingError(err) {
			return OpenAIResponse{}, fmt.Errorf(
				"read responses stream events: %w",
				newOpenAIProviderContractError(
					providerID,
					rawResp,
					fmt.Errorf("%s: %w", openAIResponsesStreamEndedBeforeTerminalMessage, err),
				),
			)
		}
		return OpenAIResponse{}, newOpenAIRequestErrorMapper(providerID).Map(err, rawResp, "read responses stream events")
	}
	if !accumulator.hasCompleted() {
		if rawResp == nil {
			return OpenAIResponse{}, fmt.Errorf("read responses stream events: %w", errors.New(openAIResponsesStreamEndedBeforeTerminalMessage))
		}
		return OpenAIResponse{}, fmt.Errorf(
			"read responses stream events: %w",
			newOpenAIProviderContractError(
				providerID,
				rawResp,
				errors.New(openAIResponsesStreamEndedBeforeTerminalMessage),
			),
		)
	}
	return responseFromStreamAccumulator(accumulator, providerID, rawResp, requestEvidence)
}

func responseFromStreamAccumulator(
	accumulator *responseStreamAccumulator,
	providerID string,
	rawResp *http.Response,
	requestEvidence modelcontract.ProviderUsageEvidence,
) (OpenAIResponse, error) {
	response, err := accumulator.Response()
	if err != nil {
		return OpenAIResponse{}, fmt.Errorf(
			"read responses stream events: %w",
			newOpenAIProviderContractError(providerID, rawResp, err),
		)
	}
	response.ServedModel = servedModelMetadata(rawResp, optionalStringValue(response.ServedModel))
	response.ReasoningIncluded = reasoningIncludedMetadata(rawResp)
	response.ProviderEvidence.ProviderID = requestEvidence.ProviderID
	response.ProviderEvidence.RequestedModel = requestEvidence.RequestedModel
	response.ProviderEvidence.RequestedServiceTier = requestEvidence.RequestedServiceTier
	response.ProviderEvidence.RequestedHostedTools = requestEvidence.RequestedHostedTools
	if response.ServedModel != nil {
		response.ProviderEvidence.ServedModel = textutil.Pointer(response.ServedModel)
	}
	return response, nil
}

func (t *HTTPTransport) providerUsageRequestEvidence(
	request OpenAIRequest,
	preparation openAIDispatchPreparation,
	payload responses.ResponseNewParams,
) modelcontract.ProviderUsageEvidence {
	evidence := modelcontract.ProviderUsageEvidence{
		ProviderID:     textutil.Value(preparation.providerCaps.ProviderID),
		RequestedModel: request.Model,
	}
	if tier := strings.TrimSpace(string(payload.ServiceTier)); tier != "" {
		evidence.RequestedServiceTier = textutil.Value(tier)
	}
	for _, tool := range payload.Tools {
		if tool.OfWebSearch == nil {
			continue
		}
		evidence.RequestedHostedTools = append(evidence.RequestedHostedTools, modelcontract.HostedToolConfiguration{
			Type: string(tool.OfWebSearch.Type),
		})
	}
	return evidence
}

func isOpenAIResponsesStreamFramingError(err error) bool {
	if err == nil {
		return false
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

type openAIResponseStatus struct {
	Code int
}

type providerContractErrorWithoutStatus struct {
	ProviderID string
	Err        error
}

func (e *providerContractErrorWithoutStatus) Error() string {
	return fmt.Sprintf("%s provider contract error without HTTP response status: %v", e.ProviderID, e.Err)
}

func (e *providerContractErrorWithoutStatus) Unwrap() error {
	return e.Err
}

func newOpenAIResponseStatus(rawResp *http.Response) *openAIResponseStatus {
	if rawResp == nil {
		return nil
	}
	return &openAIResponseStatus{Code: rawResp.StatusCode}
}

func newOpenAIProviderContractError(providerID string, rawResp *http.Response, cause error) error {
	if rawResp == nil {
		return &providerContractErrorWithoutStatus{ProviderID: providerID, Err: cause}
	}
	providerErr := llmerrors.NewProviderContractError(providerID, rawResp.StatusCode, cause)
	enrichProviderAPIErrorFromResponseHeaders(providerErr, rawResp)
	return providerErr
}

func callerCanceledStreamRead(ctx context.Context) bool {
	if ctx.Err() == nil {
		return false
	}
	return !errors.Is(context.Cause(ctx), ErrModelStreamStalled)
}

func (t *HTTPTransport) streamingHTTPClient() *http.Client {
	transport := t.Client.Transport
	if transport == nil {
		transport = sharedHTTPTransport
	}
	return &http.Client{Transport: transport}
}

func (t *HTTPTransport) Compact(ctx context.Context, request OpenAIRequest) (OpenAICompactionResponse, error) {
	if t.Client == nil {
		t.Client = NewHTTPClient(120 * time.Second)
	}
	preparation, err := t.prepareDispatch(
		ctx,
		request.SessionID,
		request.Model,
		request.CodexDispatch,
		request.FastMode,
	)
	if err != nil {
		return OpenAICompactionResponse{}, err
	}
	windowTokens := t.resolveContextWindowFallback(ctx, request.Model)
	switch preparation.variant.RemoteCompactionProtocol {
	case remoteCompactionResponsesTriggerV2:
		return t.compactResponsesTriggerV2(ctx, request, preparation.authHeader, preparation.mode, preparation.variant, preparation.providerCaps, windowTokens, preparation.projection)
	default:
		return OpenAICompactionResponse{}, fmt.Errorf("provider %s does not support remote compaction", preparation.providerCaps.ProviderID)
	}
}

func codexTurnStateObserver(projection *codexDispatchProjection, dispatch *CodexDispatchContext) *CodexDispatchContext {
	if projection == nil {
		return nil
	}
	return dispatch
}

func (t *HTTPTransport) prepareDispatch(
	ctx context.Context,
	sessionID *string,
	model string,
	dispatch *CodexDispatchContext,
	fastMode bool,
) (openAIDispatchPreparation, error) {
	if sessionID == nil {
		return openAIDispatchPreparation{}, fmt.Errorf("%w: Session identity is required for dispatch", ErrInvalidRequest)
	}
	if err := validateSessionDispatchPairing(sessionID, dispatch); err != nil {
		return openAIDispatchPreparation{}, err
	}
	authHeader, mode, err := t.resolveAuth(ctx)
	if err != nil {
		return openAIDispatchPreparation{}, err
	}
	variant, err := t.providerVariantForMode(mode)
	if err != nil {
		return openAIDispatchPreparation{}, err
	}
	providerCaps := t.providerCapabilitiesForVariant(variant)
	isChatGPTCodex := variant.ProviderID == "chatgpt-codex"
	projection, err := validateOpenAIDispatch(
		*sessionID,
		model,
		dispatch,
		isChatGPTCodex,
		effectiveServiceTier(fastMode, providerCaps),
	)
	if err != nil {
		return openAIDispatchPreparation{}, err
	}
	return openAIDispatchPreparation{
		authHeader:   authHeader,
		mode:         mode,
		variant:      variant,
		providerCaps: providerCaps,
		projection:   projection,
	}, nil
}

func (t *HTTPTransport) compactResponsesTriggerV2(ctx context.Context, request OpenAIRequest, authHeader string, mode OpenAIAuthMode, variant ProviderVariantContract, providerCaps ProviderCapabilities, windowTokens int, projection *codexDispatchProjection) (OpenAICompactionResponse, error) {
	payload, err := newOpenAIRequestPayloadBuilder(t.Store, t.ModelVerbosity, providerCaps).BuildCompactV2(request, mode)
	if err != nil {
		return OpenAICompactionResponse{}, err
	}
	applyCodexClientMetadata(&payload, projection)
	compressionOption := requestCompressionOption(variant)
	service := responses.NewResponseService(
		option.WithBaseURL(t.serviceBaseURL(mode)),
		option.WithHTTPClient(t.streamingHTTPClient()),
		option.WithMaxRetries(0),
	)
	reqOpts := t.buildRequestOptions(authHeader, mode, request.SessionID, projection, request.CodexDispatch)
	reqOpts = append(reqOpts, compressionOption)
	var rawResp *http.Response
	reqOpts = append(reqOpts, option.WithResponseInto(&rawResp))
	watchdog := newStreamIdleWatchdog(ctx, t.Client.Timeout)
	defer watchdog.stop()
	stream := service.NewStreaming(watchdog.ctx, payload, reqOpts...)
	defer func() { _ = stream.Close() }()

	accumulator := newResponseStreamAccumulator(StreamCallbacks{}, windowTokens)
	turnStateObserver := codexTurnStateObserver(projection, request.CodexDispatch)
	headersObserved := false
	requestEvidence := t.providerUsageRequestEvidence(
		request,
		openAIDispatchPreparation{mode: mode, providerCaps: providerCaps},
		payload,
	)
	observeCodexTurnStateResponseHeader(turnStateObserver, rawResp, &headersObserved)
	for stream.Next() {
		observeCodexTurnStateResponseHeader(turnStateObserver, rawResp, &headersObserved)
		watchdog.ping()
		event := stream.Current()
		accumulator.Consume(event)
		if err := accumulator.Err(providerCaps.ProviderID, newOpenAIResponseStatus(rawResp)); err != nil {
			return OpenAICompactionResponse{}, newOpenAIRequestErrorMapper(providerCaps.ProviderID).Map(err, rawResp, "read responses compaction stream events")
		}
	}
	observeCodexTurnStateResponseHeader(turnStateObserver, rawResp, &headersObserved)
	if err := stream.Err(); err != nil {
		if errors.Is(context.Cause(watchdog.ctx), ErrModelStreamStalled) {
			return OpenAICompactionResponse{}, fmt.Errorf("model stream stalled: %w", ErrModelStreamStalled)
		}
		return OpenAICompactionResponse{}, newOpenAIRequestErrorMapper(providerCaps.ProviderID).Map(err, rawResp, "read responses compaction stream events")
	}
	if !accumulator.hasCompleted() {
		return OpenAICompactionResponse{}, newOpenAIProviderContractError(providerCaps.ProviderID, rawResp, errors.New(openAIResponsesStreamEndedBeforeTerminalMessage))
	}
	response, err := responseFromStreamAccumulator(accumulator, providerCaps.ProviderID, rawResp, requestEvidence)
	if err != nil {
		return OpenAICompactionResponse{}, err
	}
	checkpoint, err := requireSingleEncryptedCompactionOutput(response.OutputItems)
	if err != nil {
		return OpenAICompactionResponse{}, newOpenAIProviderContractError(providerCaps.ProviderID, rawResp, err)
	}
	checkpoint = CloneResponseItems([]ResponseItem{checkpoint})[0]
	return OpenAICompactionResponse{
		Checkpoint:       checkpoint,
		Usage:            response.Usage,
		ProviderEvidence: response.ProviderEvidence,
	}, nil
}

func requireSingleEncryptedCompactionOutput(items []ResponseItem) (ResponseItem, error) {
	counts := make(map[ResponseItemType]int)
	compactions := make([]ResponseItem, 0, 1)
	for _, item := range items {
		counts[item.Type]++
		if item.Type == ResponseItemTypeCompaction {
			compactions = append(compactions, item)
		}
	}
	if len(compactions) != 1 {
		reason := CompactionCheckpointReasonZero
		if len(compactions) > 1 {
			reason = CompactionCheckpointReasonMultiple
		}
		return ResponseItem{}, &CompactionCheckpointContractError{
			Reason:           reason,
			CompactionCount:  len(compactions),
			OutputCount:      len(items),
			OutputTypeCounts: counts,
		}
	}
	if _, ok := textutil.OptionalTrimmed(compactions[0].EncryptedContent); !ok {
		return ResponseItem{}, &CompactionCheckpointContractError{
			Reason:           CompactionCheckpointReasonMissingEncryptedContent,
			CompactionCount:  len(compactions),
			OutputCount:      len(items),
			OutputTypeCounts: counts,
		}
	}
	return compactions[0], nil
}

func (t *HTTPTransport) ResolveModelContextWindow(ctx context.Context, model string) (int, error) {
	if t.Client == nil {
		t.Client = NewHTTPClient(120 * time.Second)
	}
	if t.ContextWindowTokens > 0 {
		return t.ContextWindowTokens, nil
	}

	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	if normalizedModel == "" {
		if t.ContextWindowTokens > 0 {
			return t.ContextWindowTokens, nil
		}
		return 0, nil
	}

	t.mu.RLock()
	if cached := t.modelContextWindows[normalizedModel]; cached > 0 {
		t.mu.RUnlock()
		return cached, nil
	}
	t.mu.RUnlock()

	resolved := 0
	authHeader, mode, err := t.resolveAuth(ctx)
	if err == nil {
		service := openai.NewModelService(
			option.WithBaseURL(t.serviceBaseURL(mode)),
			option.WithHTTPClient(t.Client),
			option.WithMaxRetries(0),
		)
		reqOpts := t.buildRequestOptions(authHeader, mode, nil, nil, nil)
		var rawResp *http.Response
		reqOpts = append(reqOpts, option.WithResponseInto(&rawResp))
		modelResponse, modelErr := service.Get(ctx, strings.TrimSpace(model), reqOpts...)
		if modelErr == nil && modelResponse != nil {
			resolved = parseContextWindowTokens(modelResponse.RawJSON())
		}
		if resolved <= 0 {
			resolved = parseContextWindowTokensFromHeaders(rawResp)
		}
	}

	if resolved <= 0 {
		if fallbackMeta, ok := LookupModelMetadata(model); ok && fallbackMeta.ContextWindowTokens > 0 {
			resolved = fallbackMeta.ContextWindowTokens
		}
	}
	if resolved <= 0 {
		resolved = t.ContextWindowTokens
	}

	t.cacheModelContextWindow(model, resolved)
	return resolved, nil
}

func (t *HTTPTransport) ProviderCapabilities(ctx context.Context) (ProviderCapabilities, error) {
	_, mode, err := t.resolveAuth(ctx)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	return t.providerCapabilitiesForMode(mode)
}

func (t *HTTPTransport) resolveAuth(ctx context.Context) (string, OpenAIAuthMode, error) {
	if t.Auth == nil {
		return "", OpenAIAuthMode{}, &AuthError{Err: auth.ErrAuthNotConfigured}
	}
	result, err := t.Auth.ResolveDispatchAuth(ctx)
	if err != nil {
		return "", OpenAIAuthMode{}, &AuthError{Err: err}
	}
	if result == nil {
		return "", OpenAIAuthMode{}, nil
	}
	if strings.TrimSpace(result.Header) == "" {
		return "", OpenAIAuthMode{}, &AuthError{Err: errors.New("dispatch credential is empty")}
	}
	return result.Header, result.Mode, nil
}
