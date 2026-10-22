package llm

import (
	"context"
	"encoding/json"
	"net/http"

	"core/shared/textutil"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

const (
	responsesSocketMessageBytes   = 256 << 20
	responsesSocketBufferedBytes  = 64 << 20
	responsesSocketBufferedEvents = 1600
)

func (t *HTTPTransport) Close() error {
	t.generationMu.Lock()
	defer t.generationMu.Unlock()
	if t.socket == nil {
		return nil
	}
	socket := t.socket
	t.socket = nil
	return socket.Close()
}

func (t *HTTPTransport) generateWebsocket(ctx context.Context, request ResponsesRequest, callbacks StreamCallbacks) (result ResponsesResponse, err error) {
	t.generationMu.Lock()
	defer t.generationMu.Unlock()
	defer func() {
		if err != nil && t.socket != nil {
			t.socket.Abort()
			t.socket = nil
		}
	}()
	preparation, err := t.prepareDispatch(ctx, request.SessionID, request.Model, request.CodexDispatch, request.FastMode)
	if err != nil {
		return result, err
	}
	payload, err := t.buildDispatchPayload(request, preparation.mode, preparation.providerCaps, preparation.projection)
	if err != nil {
		return result, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	var create responses.ResponsesClientEventResponseCreateParam
	if err := json.Unmarshal(encoded, &create); err != nil {
		return result, err
	}
	applyCodexClientMetadata(&create, preparation.projection)
	var handshake *http.Response
	evidence := t.providerUsageRequestEvidence(request, preparation, payload)
	attempt := t.startProviderAttempt(ctx, request, preparation, evidence.RequestedServiceTier, providerAttemptGenerate)
	if attempt != nil {
		attempt.incremental = textutil.Value(false)
		attempt.transmittedItems = textutil.Value(len(payload.Input.OfInputItemList))
	}
	defer func() { attempt.finish(ctx, handshake, err) }()
	if t.socket == nil {
		service := responses.NewResponseService(option.WithBaseURL(t.serviceBaseURL()), option.WithHTTPClient(t.streamingHTTPClient()), option.WithMaxRetries(0))
		options := append(t.buildRequestOptions(request, preparation), option.WithResponseInto(&handshake))
		t.socket, err = service.Connect(ctx, responses.ResponseConnectionOptions{
			MaxMessageBytes: responsesSocketMessageBytes, MaxBufferedBytes: responsesSocketBufferedBytes, MaxBufferedEvents: responsesSocketBufferedEvents,
		}, options...)
		if err != nil {
			return result, newResponsesRequestErrorMapper(preparation.variant.ProviderID).Map(err, handshake, "open responses websocket")
		}
	}
	headersObserved := false
	observeCodexTurnStateResponseHeader(codexTurnStateObserver(preparation.projection, request.CodexDispatch), handshake, &headersObserved)
	if err := t.socket.Create(ctx, create); err != nil {
		return result, err
	}
	accumulator := newResponseStreamAccumulator(callbacks, t.resolveContextWindowFallback(ctx, request.Model), preparation.variant.ResponsesPolicy)
	accumulator.attempt = attempt
	for {
		event, err := t.socket.Recv(ctx)
		if err != nil {
			return result, err
		}
		if callbacks.OnStreamActivity != nil {
			callbacks.OnStreamActivity()
		}
		var streamEvent responses.ResponseStreamEventUnion
		if err := json.Unmarshal([]byte(event.RawJSON()), &streamEvent); err != nil {
			return result, err
		}
		accumulator.Consume(streamEvent)
		if err := accumulator.Err(preparation.variant.ProviderID, nil); err != nil {
			return result, err
		}
		if accumulator.hasCompleted() {
			return responseFromStreamAccumulator(accumulator, preparation.variant.ProviderID, handshake, evidence)
		}
	}
}
