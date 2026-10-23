package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"core/shared/textutil"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

const (
	responsesSocketMessageBytes   = 256 << 20
	responsesSocketBufferedBytes  = 64 << 20
	responsesSocketBufferedEvents = 1600
)

type responsesGenerationSocket struct {
	*responses.ResponseConnection
	mode       OpenAIAuthMode
	checkpoint *responsesContinuation
}

type responsesContinuation struct {
	responseID string
	items      []json.RawMessage
	settings   []byte
}

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
	var preparation responsesDispatchPreparation
	if t.socket == nil {
		preparation, err = t.prepareDispatch(ctx, request.SessionID, request.Model, request.CodexDispatch, request.FastMode)
	} else {
		if err = validateResponsesDispatchIdentity(request.SessionID, request.CodexDispatch); err == nil {
			preparation, err = t.projectDispatch(*request.SessionID, request.Model, request.CodexDispatch, request.FastMode, t.socket.mode)
		}
	}
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
	var controls map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &controls); err != nil {
		return result, err
	}
	delete(controls, "input")
	delete(controls, "client_metadata")
	settings, err := json.Marshal(controls)
	if err != nil {
		return result, err
	}
	start := 0
	if t.socket != nil && t.socket.checkpoint != nil {
		prior := t.socket.checkpoint
		if len(request.Items) >= len(prior.items) && bytes.Equal(settings, prior.settings) {
			matches := true
			for index, item := range prior.items {
				if !bytes.Equal(request.Items[index].Raw, item) {
					matches = false
					break
				}
			}
			if matches {
				start = len(prior.items)
				create.PreviousResponseID = param.NewOpt(prior.responseID)
			}
		}
	}
	input := responses.ResponseInputParam(payload.Input.OfInputItemList[start:])
	create.Input = responses.ResponsesClientEventResponseCreateInputUnionParam{}
	if len(input) > 0 {
		create.Input.OfResponse = &input
	}
	observer := codexTurnStateObserver(preparation.projection, request.CodexDispatch)
	var turnState *string
	if value, present := observer.turnStateForRequest(); present {
		turnState = textutil.Value(value)
	}
	applyCodexClientMetadata(&create, preparation.projection, turnState)
	var handshake *http.Response
	evidence := t.providerUsageRequestEvidence(request, preparation, payload)
	attempt := t.startProviderAttempt(ctx, request, preparation, evidence.RequestedServiceTier, providerAttemptGenerate)
	if attempt != nil {
		attempt.incremental = textutil.Value(create.PreviousResponseID.Valid())
		attempt.transmittedItems = textutil.Value(len(input))
	}
	defer func() { attempt.finish(ctx, handshake, err) }()
	if t.socket == nil {
		service := responses.NewResponseService(option.WithBaseURL(t.serviceBaseURL()), option.WithHTTPClient(t.streamingHTTPClient()), option.WithMaxRetries(0))
		options := append(t.buildRequestOptions(request, preparation), option.WithHeaderDel(codexTurnStateHeader), option.WithResponseInto(&handshake))
		connection, err := service.Connect(ctx, responses.ResponseConnectionOptions{
			MaxMessageBytes: responsesSocketMessageBytes, MaxBufferedBytes: responsesSocketBufferedBytes, MaxBufferedEvents: responsesSocketBufferedEvents,
		}, options...)
		if err != nil {
			return result, newResponsesRequestErrorMapper(preparation.variant.ProviderID).Map(err, handshake, "open responses websocket")
		}
		t.socket = &responsesGenerationSocket{ResponseConnection: connection, mode: preparation.mode}
	}
	abort := context.AfterFunc(ctx, t.socket.Abort)
	defer abort()
	if err := t.socket.Create(ctx, create); err != nil {
		return result, err
	}
	accumulator := newResponseStreamAccumulator(callbacks, t.resolveContextWindowFallback(ctx, request.Model), preparation.variant.ResponsesPolicy)
	accumulator.attempt = attempt
	for {
		event, err := t.socket.Recv(ctx)
		if err != nil {
			return result, &ProviderAPIError{ProviderID: preparation.variant.ProviderID,
				Code: UnifiedErrorCodeUnknown, Message: err.Error(), Err: err}
		}
		if callbacks.OnStreamActivity != nil {
			callbacks.OnStreamActivity()
		}
		if event.Type == "response.metadata" && observer != nil {
			observer.observeTurnStateMetadata([]byte(event.RawJSON()))
		}
		if event.JSON.OfResponsesServerEventResponseWsError.Valid() {
			failure := event.OfResponsesServerEventResponseWsError
			var status *int
			if failure.JSON.Status.Valid() {
				status = textutil.Value(int(failure.Status))
			}
			reducer, reducerErr := providerErrorReducerForID(preparation.variant.ProviderID)
			if reducerErr != nil {
				return result, reducerErr
			}
			responsesReducer, ok := reducer.(responsesErrorReducer)
			if !ok {
				return result, fmt.Errorf("Responses provider has no structured error reducer")
			}
			reduced, ok := responsesReducer.reducePayload([]byte(failure.RawJSON()), nil, status)
			if !ok {
				return result, fmt.Errorf("Responses provider emitted an invalid structured error")
			}
			return result, reduced
		}
		var streamEvent responses.ResponseStreamEventUnion
		if err := json.Unmarshal([]byte(event.RawJSON()), &streamEvent); err != nil {
			return result, err
		}
		accumulator.Consume(streamEvent)
		if err := accumulator.Err(preparation.variant.ProviderID, nil); err != nil {
			return result, newResponsesRequestErrorMapper(preparation.variant.ProviderID).Map(err, nil, "read responses websocket events")
		}
		if accumulator.hasCompleted() {
			if !abort() {
				return result, ctx.Err()
			}
			result, err = responseFromStreamAccumulator(accumulator, preparation.variant.ProviderID, handshake, evidence)
			if err != nil {
				return result, err
			}
			if accumulator.completed.ID == "" {
				return result, newOpenAIProviderContractError(preparation.variant.ProviderID, nil, fmt.Errorf("completed Responses response has no response ID"))
			}
			checkpoint := &responsesContinuation{responseID: accumulator.completed.ID, settings: settings,
				items: make([]json.RawMessage, 0, len(request.Items)+len(accumulator.completed.Output))}
			for _, item := range request.Items {
				checkpoint.items = append(checkpoint.items, item.Raw)
			}
			if result.NativeResponse != nil {
				for _, item := range result.NativeResponse.body.Items {
					checkpoint.items = append(checkpoint.items, item)
				}
			}
			t.socket.checkpoint = checkpoint
			return result, nil
		}
	}
}
