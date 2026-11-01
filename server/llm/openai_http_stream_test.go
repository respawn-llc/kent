package llm

import (
	"context"
	"core/internal/testharness/httpclient"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"core/shared/config"
	"core/shared/textutil"
	"github.com/coder/websocket"
)

type staticAuthHeader struct{}

func (staticAuthHeader) ResolveDispatchAuth(context.Context) (*DispatchAuth, error) {
	return &DispatchAuth{Header: "Bearer test"}, nil
}

func newOpenAIStreamTestServer(t *testing.T, events ...string) *httptest.Server {
	t.Helper()
	var stream strings.Builder
	for _, event := range events {
		_, _ = fmt.Fprintf(&stream, "data: %s\n\n", event)
	}
	return newOpenAIRawStreamTestServer(t, stream.String())
}

func newOpenAIRawStreamTestServer(t *testing.T, stream string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	t.Cleanup(server.Close)
	return server
}

func newOpenAIStreamTestTransport(t *testing.T, events ...string) *HTTPTransport {
	t.Helper()
	server := newOpenAIStreamTestServer(t, events...)
	return newOpenAIStreamTestTransportForServer(t, server)
}

func newOpenAIStreamTestTransportForServer(t *testing.T, server *httptest.Server) *HTTPTransport {
	transport := newTestHTTPTransport(t, staticAuthHeader{}, testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(server.URL + "/v1")}))

	transport.Client = newRewritingHTTPClient(t, server)
	return transport
}

func newOpenAIRawStreamTestTransport(t *testing.T, stream string) *HTTPTransport {
	t.Helper()
	server := newOpenAIRawStreamTestServer(t, stream)
	return newOpenAIStreamTestTransportForServer(t, server)
}

type readErrorAtEOF struct {
	io.Reader
	err error
}

func (r readErrorAtEOF) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if errors.Is(err, io.EOF) {
		return n, r.err
	}
	return n, err
}

func newResponsesClientWithBodies(t *testing.T, bodies ...io.ReadCloser) (*ResponsesClient, *atomic.Int32) {
	t.Helper()
	transport := newTestHTTPTransport(t, staticAuthHeader{}, testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://compatible.example/v1")}))
	connectionID := config.ConnectionID("responses-test")
	transport.ConnectionID = &connectionID
	var requestCount atomic.Int32
	transport.Client = &http.Client{Transport: httpclient.RoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestNumber := requestCount.Add(1)
		if int(requestNumber) > len(bodies) {
			return nil, fmt.Errorf("unexpected Responses test request %d", requestNumber)
		}
		headers := make(http.Header)
		headers.Set("Content-Type", "text/event-stream")
		headers.Set("x-request-id", "responses-test-request")
		headers.Set("x-openai-authorization-error", "responses-test-authorization-diagnostic")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     headers,
			Body:       bodies[requestNumber-1],
			Request:    request,
		}, nil
	})}
	return NewResponsesClient(transport, nil), &requestCount
}

const incompleteResponsesTestStream = `data: {"type":"response.created","response":{"id":"resp_partial"}}` + "\n\n"

func incompleteResponsesBody() io.ReadCloser {
	return io.NopCloser(strings.NewReader(incompleteResponsesTestStream))
}

func newPrematureSocketClient(t *testing.T, recover bool) (*ResponsesClient, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		if _, _, err := socket.Read(t.Context()); err != nil {
			return
		}
		if requests.Add(1) > 1 && recover {
			_ = socket.Write(t.Context(), websocket.MessageText, []byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"recovered"}]}}`))
			_ = socket.Write(t.Context(), websocket.MessageText, []byte(`{"type":"response.completed","response":{"id":"recovered","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"recovered"}]}]}}`))
			_, _, _ = socket.Read(t.Context())
			return
		}
		_ = socket.Write(t.Context(), websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"partial","output":[]}}`))
		_ = socket.Close(websocket.StatusNormalClosure, "")
	}))
	t.Cleanup(server.Close)
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	transport := newTestHTTPTransport(t, staticAuthHeader{}, registration)
	transport.ConnectionID = textutil.Value(config.ConnectionID("responses-test"))
	return NewResponsesClient(transport, nil), &requests
}

func responsesBodyEndingWithError(err error) io.ReadCloser {
	return io.NopCloser(readErrorAtEOF{Reader: strings.NewReader(incompleteResponsesTestStream), err: err})
}

func completedResponsesBody() io.ReadCloser {
	return io.NopCloser(strings.NewReader(strings.Join([]string{
		`event: response.completed`,
		"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"recovered\"}]}}\n\ndata: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"recovered\"}]}]}}",
		``,
		``,
	}, "\n")))
}

func generateWithResponsesClientRetry(ctx context.Context, client *ResponsesClient, schedule GenerationRetrySchedule) (Response, error) {
	return GenerateWithRetry(ctx, func(ctx context.Context, callbacks StreamCallbacks, _ func()) (Response, error) {
		return client.Generate(ctx, Request{
			Model:           "gpt-6-sol",
			SessionID:       textutil.Value("test-session"),
			ToolChoiceMode:  ToolChoiceModeAutomatic,
			ReasoningEffort: "high",
		}, callbacks)
	}, GenerationRetryOptions{Schedule: schedule})
}

func requireResponsesRetryExhaustion(t *testing.T, client *ResponsesClient, requestCount *atomic.Int32, schedule GenerationRetrySchedule) error {
	t.Helper()
	_, err := generateWithResponsesClientRetry(context.Background(), client, schedule)
	if err == nil {
		t.Fatal("generation unexpectedly succeeded after repeated premature stream endings")
	}
	if got, want := requestCount.Load(), int32(len(schedule.Failures)+1); got != want {
		t.Fatalf("provider requests = %d, want %d", got, want)
	}
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) {
		t.Fatalf("generation error = %T, want ProviderAPIError", err)
	}
	if providerErr.Err == nil ||
		providerErr.ProviderID != "openai-compatible" || !HasHTTPStatus(providerErr, http.StatusOK) ||
		providerErr.Code != UnifiedErrorCodeUnknown {
		t.Fatalf("final provider error = %+v, want retained OpenAI HTTP 200 unknown error cause", providerErr)
	}
	if providerErr.ConnectionID == nil || *providerErr.ConnectionID != config.ConnectionID("responses-test") {
		t.Fatalf("final connection ID = %v, want responses-test", providerErr.ConnectionID)
	}
	if providerErr.ProviderRequestID == nil || *providerErr.ProviderRequestID != "responses-test-request" {
		t.Fatalf("final request ID = %v, want enriched response header", providerErr.ProviderRequestID)
	}
	if providerErr.AuthorizationDiagnostic == nil || *providerErr.AuthorizationDiagnostic != "responses-test-authorization-diagnostic" {
		t.Fatalf("final authorization diagnostic = %v, want enriched response header", providerErr.AuthorizationDiagnostic)
	}
	return err
}

type retryWaitObservedContext struct {
	context.Context
	waitStarted chan struct{}
}

func (ctx retryWaitObservedContext) Done() <-chan struct{} {
	select {
	case ctx.waitStarted <- struct{}{}:
	default:
	}
	return ctx.Context.Done()
}

func joinedAssistantDeltas(deltas []AssistantDelta) string {
	var text strings.Builder
	for _, delta := range deltas {
		text.WriteString(delta.Text)
	}
	return text.String()
}

func TestGenerate_RejectsMalformedReasoningCoordinates(t *testing.T) {
	tests := []struct {
		name  string
		event string
	}{
		{
			name:  "negative output index",
			event: `{"type":"response.reasoning_summary_text.delta","output_index":-1,"summary_index":0,"item_id":"reason_1","delta":"plan"}`,
		},
		{
			name:  "missing output index",
			event: `{"type":"response.reasoning_summary_text.delta","summary_index":0,"item_id":"reason_1","delta":"plan"}`,
		},
		{
			name:  "missing summary index",
			event: `{"type":"response.reasoning_summary_text.delta","output_index":0,"item_id":"reason_1","delta":"plan"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newOpenAIStreamTestTransport(t,
				test.event,
				`{"type":"response.completed","response":{"output":[]}}`,
				`[DONE]`,
			)
			_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
			if err == nil {
				t.Fatal("malformed reasoning coordinate was accepted")
			}
			var providerErr *ProviderAPIError
			if !errors.As(err, &providerErr) {
				t.Fatalf("error = %T %v, want ProviderAPIError", err, err)
			}
			if providerErr.Code != UnifiedErrorCodeProviderContract {
				t.Fatalf("provider error code = %q, want provider contract", providerErr.Code)
			}
		})
	}
}

func TestGenerate_RejectsConflictingReasoningIdentities(t *testing.T) {
	tests := []struct {
		name   string
		events []string
	}{
		{
			name: "one identity aliases two coordinates",
			events: []string{
				`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"item_id":"reason_1","delta":"first"}`,
				`{"type":"response.reasoning_summary_text.delta","output_index":1,"summary_index":0,"item_id":"reason_1","delta":"second"}`,
			},
		},
		{
			name: "one coordinate receives two identities",
			events: []string{
				`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"item_id":"reason_1","delta":"first"}`,
				`{"type":"response.reasoning_summary_text.done","output_index":0,"summary_index":0,"item_id":"reason_2","text":"second"}`,
			},
		},
		{
			name: "completed identity conflicts with streamed identity",
			events: []string{
				`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"item_id":"reason_1","delta":"streamed"}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"reason_2","summary":[{"type":"summary_text","text":"completed"}]}}`,
				`{"type":"response.completed","response":{"output":[{"type":"reasoning","id":"reason_2","summary":[{"type":"summary_text","text":"completed"}]}]}}`,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := append([]string(nil), test.events...)
			if test.name != "completed identity conflicts with streamed identity" {
				events = append(events, `{"type":"response.completed","response":{"output":[]}}`)
			}
			events = append(events, `[DONE]`)
			transport := newOpenAIStreamTestTransport(t, events...)
			_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
			if err == nil {
				t.Fatal("conflicting reasoning identity was accepted")
			}
			var providerErr *ProviderAPIError
			if !errors.As(err, &providerErr) {
				t.Fatalf("error = %T %v, want ProviderAPIError", err, err)
			}
			if providerErr.Code != UnifiedErrorCodeProviderContract {
				t.Fatalf("provider error code = %q, want provider contract", providerErr.Code)
			}
		})
	}
}

func TestGeneratePreservesDistinctReasoningTracesWithSameText(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"item_id":"reason_1","delta":"same"}`,
		`{"type":"response.reasoning_summary_text.delta","output_index":1,"summary_index":0,"item_id":"reason_2","delta":"same"}`,
		`{"type":"response.completed","response":{"output":[]}}`,
		`[DONE]`,
	)
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(resp.Reasoning) != 2 {
		t.Fatalf("reasoning traces = %+v, want two distinct traces", resp.Reasoning)
	}
	for index, entry := range resp.Reasoning {
		if entry.Text != "same" || entry.SourceCoordinate == nil ||
			entry.SourceCoordinate.OutputIndex == nil ||
			*entry.SourceCoordinate.OutputIndex != int64(index) {
			t.Fatalf("reasoning trace %d = %+v, want coordinate output=%d", index, entry, index)
		}
	}
}

func TestGenerateCompletedReasoningTextOverridesStreamedPartial(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"item_id":"reason_1","delta":"streamed partial"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"reason_1","summary":[{"type":"summary_text","text":"completed final"}]}}`,
		`{"type":"response.completed","response":{"output":[{"type":"reasoning","id":"reason_1","summary":[{"type":"summary_text","text":"completed final"}]}]}}`,
		`[DONE]`,
	)
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(resp.Reasoning) != 1 {
		t.Fatalf("reasoning traces = %+v, want one trace", resp.Reasoning)
	}
	if resp.Reasoning[0].Text != "completed final" {
		t.Fatalf("reasoning text = %q, want completed final", resp.Reasoning[0].Text)
	}
}

func TestGenerate_AcceptsCompletedResponseEOFWithoutDoneSentinel(t *testing.T) {
	transport := newOpenAIRawStreamTestTransport(t, strings.Join([]string{
		`event: response.completed`,
		"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello\"}]}}\n\ndata: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"usage\":{\"input_tokens\":11,\"output_tokens\":7,\"total_tokens\":18},\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello\"}]}]}}",
		``,
		``,
	}, "\n"))

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if optionalStringValue(resp.AssistantText) != "Hello" {
		t.Fatalf("assistant text = %q, want Hello", optionalStringValue(resp.AssistantText))
	}
	if resp.Usage.InputTokens == nil || resp.Usage.OutputTokens == nil || *resp.Usage.InputTokens != 11 || *resp.Usage.OutputTokens != 7 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestGenerate_SalvagesCompletedResponseBeforeTrailingMalformedEvent(t *testing.T) {
	tests := []struct {
		name         string
		trailingData string
	}{
		{name: "empty event data"},
		{name: "numeric event type", trailingData: `{"type":1}`},
		{name: "missing event type", trailingData: `{}`},
		{name: "null event type", trailingData: `{"type":null}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newOpenAIRawStreamTestTransport(t, strings.Join([]string{
				`event: response.completed`,
				"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"Done\"}]}}\n\ndata: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"usage\":{\"input_tokens\":3,\"output_tokens\":5,\"total_tokens\":8},\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"Done\"}]}]}}",
				``,
				`data: ` + test.trailingData,
				``,
				``,
			}, "\n"))

			resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			if optionalStringValue(resp.AssistantText) != "Done" {
				t.Fatalf("assistant text = %q, want Done", optionalStringValue(resp.AssistantText))
			}
			if resp.Usage.InputTokens == nil || resp.Usage.OutputTokens == nil || *resp.Usage.InputTokens != 3 || *resp.Usage.OutputTokens != 5 {
				t.Fatalf("unexpected usage: %+v", resp.Usage)
			}
		})
	}
}

func TestGenerate_MapsResponseIncompleteEventToProviderAPIError(t *testing.T) {
	transport := newOpenAIRawStreamTestTransport(t, strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","output_index":0,"delta":"partial"}`,
		``,
		`event: response.incomplete`,
		`data: {"type":"response.incomplete","sequence_number":2,"response":{"id":"resp_1","created_at":1,"incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"partial"}]}]}}`,
		``,
		``,
	}, "\n"))

	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err == nil {
		t.Fatal("expected response incomplete event")
	}
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderAPIError, got %T", err)
	}
	if providerErr.ProviderType != "response.incomplete" {
		t.Fatalf("provider type = %q, want response.incomplete", providerErr.ProviderType)
	}
	if providerErr.ProviderCode != "max_output_tokens" {
		t.Fatalf("provider code = %q, want max_output_tokens", providerErr.ProviderCode)
	}
	if providerErr.ProviderParam != "response.incomplete_details.reason" {
		t.Fatalf("provider param = %q, want response.incomplete_details.reason", providerErr.ProviderParam)
	}
	if !HasHTTPStatus(providerErr, http.StatusOK) {
		t.Fatalf("provider status = %d, want %d", providerErr.StatusCode, http.StatusOK)
	}
	if !IsNonRetriableModelError(err) {
		t.Fatalf("expected response.incomplete terminal error to be non-retriable: %v", err)
	}
}

func TestGenerate_MapsResponseIncompleteWithoutReasonToProviderContractError(t *testing.T) {
	transport := newOpenAIRawStreamTestTransport(t, strings.Join([]string{
		`event: response.incomplete`,
		`data: {"type":"response.incomplete","sequence_number":1,"response":{"id":"resp_1","created_at":1,"output":[]}}`,
		``,
		``,
	}, "\n"))

	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err == nil {
		t.Fatal("expected response incomplete event")
	}
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderAPIError, got %T", err)
	}
	if providerErr.Code != UnifiedErrorCodeProviderContract {
		t.Fatalf("provider code = %q, want %q", providerErr.Code, UnifiedErrorCodeProviderContract)
	}
	if !HasHTTPStatus(providerErr, http.StatusOK) {
		t.Fatalf("provider status = %d, want %d", providerErr.StatusCode, http.StatusOK)
	}
}

func TestGenerate_RejectsCompletedEventWithoutResponsePayload(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{name: "missing_response", data: `{"type":"response.completed","sequence_number":1}`},
		{name: "empty_response", data: `{"type":"response.completed","sequence_number":1,"response":{}}`},
		{name: "null_output", data: `{"type":"response.completed","sequence_number":1,"response":{"output":null}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := newOpenAIRawStreamTestTransport(t, strings.Join([]string{
				`event: response.completed`,
				`data: ` + tc.data,
				``,
				``,
			}, "\n"))

			_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
			if err == nil {
				t.Fatal("expected provider contract error")
			}
			var providerErr *ProviderAPIError
			if !errors.As(err, &providerErr) {
				t.Fatalf("expected ProviderAPIError, got %T", err)
			}
			if providerErr.Code != UnifiedErrorCodeProviderContract {
				t.Fatalf("provider code = %q, want %q", providerErr.Code, UnifiedErrorCodeProviderContract)
			}
			if !HasHTTPStatus(providerErr, http.StatusOK) {
				t.Fatalf("provider status = %d, want %d", providerErr.StatusCode, http.StatusOK)
			}
		})
	}
}

func TestGenerate_RejectsPreTerminalMalformedResponsesStream(t *testing.T) {
	cases := map[string]string{
		"malformed_json_before_terminal": strings.Join([]string{
			`event: response.output_text.delta`,
			`data: {"type":`,
			``,
			``,
		}, "\n"),
		"empty_data_before_terminal": strings.Join([]string{
			`data: `,
			``,
			``,
		}, "\n"),
		"invalid_schema_before_terminal": strings.Join([]string{
			`data: {"type":1}`,
			``,
			``,
		}, "\n"),
	}

	for name, stream := range cases {
		t.Run(name, func(t *testing.T) {
			transport := newOpenAIRawStreamTestTransport(t, stream)

			_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
			if err == nil {
				t.Fatal("expected provider contract error")
			}
			var providerErr *ProviderAPIError
			if !errors.As(err, &providerErr) {
				t.Fatalf("expected ProviderAPIError, got %T", err)
			}
			if providerErr.Code != UnifiedErrorCodeProviderContract {
				t.Fatalf("provider code = %q, want %q", providerErr.Code, UnifiedErrorCodeProviderContract)
			}
			if !IsNonRetriableModelError(err) {
				t.Fatalf("expected provider-contract stream error to be non-retriable: %v", err)
			}
		})
	}
}

func TestGenerate_LeavesPreResponseEOFRetryable(t *testing.T) {
	transport := newTestHTTPTransport(t, staticAuthHeader{}, testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)}))

	transport.Client = &http.Client{Transport: httpclient.RoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, io.EOF
	})}

	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err == nil {
		t.Fatal("expected pre-response EOF")
	}
	var providerErr *ProviderAPIError
	if errors.As(err, &providerErr) {
		t.Fatalf("expected pre-response EOF to stay outside provider-contract classification, got %+v", providerErr)
	}
	if IsNonRetriableModelError(err) {
		t.Fatalf("expected pre-response EOF to remain retryable: %v", err)
	}
}

func TestGenerateWithRetryRetriesUnexpectedEOFBeforeResponseCompletion(t *testing.T) {
	client, requestCount := newResponsesClientWithBodies(t,
		responsesBodyEndingWithError(io.ErrUnexpectedEOF),
		completedResponsesBody(),
	)
	response, err := generateWithResponsesClientRetry(context.Background(), client, GenerationRetrySchedule{Failures: []time.Duration{0}})
	if err != nil {
		t.Fatalf("generation after unexpected EOF: %v", err)
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("provider requests = %d, want 2", got)
	}
	if response.Assistant.Content == nil || *response.Assistant.Content != "recovered" {
		t.Fatalf("assistant response = %+v, want recovered response", response.Assistant)
	}
}

func TestGenerateWithRetryRetriesWrappedEOFBeforeResponseCompletion(t *testing.T) {
	client, requestCount := newResponsesClientWithBodies(t,
		responsesBodyEndingWithError(fmt.Errorf("read response: %w", io.EOF)),
		completedResponsesBody(),
	)
	response, err := generateWithResponsesClientRetry(context.Background(), client, GenerationRetrySchedule{Failures: []time.Duration{0}})
	if err != nil {
		t.Fatalf("generation after EOF: %v", err)
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("provider requests = %d, want 2", got)
	}
	if response.Assistant.Content == nil || *response.Assistant.Content != "recovered" {
		t.Fatalf("assistant response = %+v, want recovered response", response.Assistant)
	}
}

func TestGenerateWithRetryRetriesNormallyClosedStreamBeforeResponseCompletion(t *testing.T) {
	client, requestCount := newPrematureSocketClient(t, true)
	response, err := generateWithResponsesClientRetry(context.Background(), client, GenerationRetrySchedule{Failures: []time.Duration{0}})
	if err != nil {
		t.Fatalf("generation after normal stream close: %v", err)
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("provider requests = %d, want 2", got)
	}
	if response.Assistant.Content == nil || *response.Assistant.Content != "recovered" {
		t.Fatalf("assistant response = %+v, want recovered response", response.Assistant)
	}
}

func TestGenerateWithRetryExhaustsUnexpectedEOFsAfterRetryBudget(t *testing.T) {
	schedule := GenerationRetrySchedule{Failures: []time.Duration{0, 0}}
	client, requestCount := newResponsesClientWithBodies(t,
		responsesBodyEndingWithError(io.ErrUnexpectedEOF),
		responsesBodyEndingWithError(io.ErrUnexpectedEOF),
		responsesBodyEndingWithError(io.ErrUnexpectedEOF),
	)
	err := requireResponsesRetryExhaustion(t, client, requestCount, schedule)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("generation error = %v, want the final unexpected EOF cause", err)
	}
}

func TestGenerateWithRetryExhaustsNormallyClosedStreamsAfterRetryBudget(t *testing.T) {
	schedule := GenerationRetrySchedule{Failures: []time.Duration{0, 0}}
	client, requestCount := newPrematureSocketClient(t, false)
	_, err := generateWithResponsesClientRetry(t.Context(), client, schedule)
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != nil || providerErr.Code != UnifiedErrorCodeUnknown || providerErr.Err == nil {
		t.Fatalf("premature socket termination error=%v", err)
	}
	if requestCount.Load() != 3 {
		t.Fatalf("fresh socket attempts=%d", requestCount.Load())
	}
}

func TestGenerateWithRetryCancellationStopsPrematureStreamRetry(t *testing.T) {
	client, requestCount := newResponsesClientWithBodies(t, responsesBodyEndingWithError(io.ErrUnexpectedEOF))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitStarted := make(chan struct{}, 1)
	retryCtx := retryWaitObservedContext{Context: ctx, waitStarted: waitStarted}
	done := make(chan error, 1)
	go func() {
		_, err := GenerateWithRetry(retryCtx, func(_ context.Context, callbacks StreamCallbacks, _ func()) (Response, error) {
			return client.Generate(ctx, Request{
				Model:           "gpt-6-sol",
				SessionID:       textutil.Value("test-session"),
				ToolChoiceMode:  ToolChoiceModeAutomatic,
				ReasoningEffort: "high",
			}, callbacks)
		}, GenerationRetryOptions{Schedule: GenerationRetrySchedule{Failures: []time.Duration{time.Hour}}})
		done <- err
	}()

	select {
	case <-waitStarted:
	case err := <-done:
		t.Fatalf("generation returned before entering retry wait: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("retry wait did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("generation error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled retry wait did not return")
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("provider requests = %d, want no dispatch after cancellation", got)
	}
}

func TestGenerate_EmitsAssistantDeltasAndToolCalls(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","name":"shell","call_id":"call_1","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"command\":\"pwd\"}"}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"message","role":"assistant","phase":"commentary","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"Hel"}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"lo"}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":1,"summary_index":0,"delta":"Plan"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Hello"}]}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Plan"}],"content":[{"type":"reasoning_text","text":"internal trace"}],"encrypted_content":"enc_1"}}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","id":"fc_1","name":"shell","call_id":"call_1","arguments":"{\"command\":\"pwd\"}"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":11,"input_tokens_details":{"cached_tokens":4},"output_tokens":7,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":18},"output":[{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Hello"}]},{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Plan"}],"content":[{"type":"reasoning_text","text":"internal trace"}],"encrypted_content":"enc_1"},{"type":"function_call","id":"fc_1","name":"shell","call_id":"call_1","arguments":"{\"command\":\"pwd\"}"}]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	var reasoning []ReasoningSummaryDelta
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
		OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) {
			reasoning = append(reasoning, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if joinedAssistantDeltas(deltas) != "Hello" {
		t.Fatalf("unexpected deltas: %+v", deltas)
	}
	if len(deltas) != 2 || deltas[0].Phase != MessagePhaseCommentary || deltas[1].Phase != MessagePhaseCommentary {
		t.Fatalf("unexpected delta phases: %+v", deltas)
	}
	if optionalStringValue(resp.AssistantText) != "Hello" {
		t.Fatalf("unexpected assistant text: %q", optionalStringValue(resp.AssistantText))
	}
	if !resp.ProviderPhase.Is(MessagePhaseCommentary) {
		t.Fatalf("unexpected provider phase: %#v", resp.ProviderPhase)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "shell" {
		t.Fatalf("unexpected tool call: %+v", resp.ToolCalls[0])
	}
	if string(resp.ToolCalls[0].Input) != "{\"command\":\"pwd\"}" {
		t.Fatalf("unexpected tool args: %s", string(resp.ToolCalls[0].Input))
	}
	if resp.Usage.InputTokens == nil || resp.Usage.OutputTokens == nil || *resp.Usage.InputTokens != 11 || *resp.Usage.OutputTokens != 7 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if resp.Usage.CachedInputTokens == nil || *resp.Usage.CachedInputTokens != 4 {
		t.Fatalf("unexpected cached usage details: %+v", resp.Usage)
	}
	if len(resp.Reasoning) != 1 || resp.Reasoning[0].Role == nil || *resp.Reasoning[0].Role != "reasoning" || resp.Reasoning[0].Text != "Plan" {
		t.Fatalf("unexpected reasoning summary entries: %+v", resp.Reasoning)
	}
	if len(resp.ReasoningItems) != 1 || resp.ReasoningItems[0].ID != "rs_1" || resp.ReasoningItems[0].EncryptedContent != "enc_1" {
		t.Fatalf("unexpected reasoning items: %+v", resp.ReasoningItems)
	}
	if len(reasoning) != 1 || reasoning[0].SourceCoordinate == nil ||
		reasoning[0].SourceCoordinate.OutputIndex == nil ||
		reasoning[0].SourceCoordinate.PartIndex == nil ||
		reasoning[0].Role != "reasoning" || reasoning[0].Text != "Plan" {
		t.Fatalf("unexpected reasoning delta callbacks: %+v", reasoning)
	}
}

func TestGenerate_PreservesWhitespaceOnlyFunctionArgumentDelta(t *testing.T) {
	const inputPrefix = `{"session_id":1000,"chars":"`
	const inputSuffix = `","yield_time_ms":9223372036854775807}`
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","name":"write_stdin","call_id":"call_1","arguments":""}}`,
		fmt.Sprintf(`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":%q}`, inputPrefix),
		fmt.Sprintf(`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":%q}`, " "),
		fmt.Sprintf(`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":%q}`, inputSuffix),
		fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"write_stdin","arguments":%q}}`, inputPrefix+" "+inputSuffix),
		fmt.Sprintf(`{"type":"response.completed","response":{"output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"write_stdin","arguments":%q}]}}`, inputPrefix+" "+inputSuffix),
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %+v", resp.ToolCalls)
	}
	var decodedInput struct {
		Chars       string `json:"chars"`
		YieldTimeMS int    `json:"yield_time_ms"`
	}
	if err := json.Unmarshal(resp.ToolCalls[0].Input, &decodedInput); err != nil {
		t.Fatalf("decode streamed write_stdin input %q: %v", resp.ToolCalls[0].Input, err)
	}
	if decodedInput.Chars != " " || decodedInput.YieldTimeMS != math.MaxInt {
		t.Fatalf("streamed write_stdin input = %+v", decodedInput)
	}
}

func TestGenerate_EmitsUnknownPhaseWhenDeltaPrecedesAssistantItem(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_text.delta","output_index":0,"delta":"Hel"}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","phase":"final_answer","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"lo"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello"}]}}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello"}]}]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if joinedAssistantDeltas(deltas) != "Hello" {
		t.Fatalf("unexpected deltas: %+v", deltas)
	}
	if len(deltas) != 2 {
		t.Fatalf("expected two deltas, got %+v", deltas)
	}
	if deltas[0].Phase != "" {
		t.Fatalf("expected unknown phase before assistant item, got %+v", deltas[0])
	}
	if deltas[1].Phase != MessagePhaseFinal {
		t.Fatalf("expected structured phase after assistant item, got %+v", deltas[1])
	}
	if !resp.ProviderPhase.Is(MessagePhaseFinal) {
		t.Fatalf("unexpected final provider phase: %#v", resp.ProviderPhase)
	}
}

func TestGenerateRejectsUnmatchedAssistantDelta(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_text.delta","delta":"stray"}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"done"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done"}]}}`,
		`{"type":"response.completed","response":{"output":[{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done"}]}]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"),
		Model:          "gpt-6-sol",
		ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high",
	}, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err == nil {
		t.Fatalf("unmatched assistant delta was accepted; emitted deltas = %+v", deltas)
	}
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderAPIError", err, err)
	}
	if providerErr.Code != UnifiedErrorCodeProviderContract {
		t.Fatalf("provider error code = %q, want %q", providerErr.Code, UnifiedErrorCodeProviderContract)
	}
	if got := joinedAssistantDeltas(deltas); got != "straydone" {
		t.Fatalf("emitted assistant deltas = %q, want all provider bytes", got)
	}
}

func TestGenerate_RejectsCompletedMessageThatConflictsWithDisplayedDeltas(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant","phase":"final_answer","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"Hello"}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"!"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello"}]}}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello"}]}]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err == nil {
		t.Fatalf("Generate response = %+v, want provider contract error", resp)
	}

	const streamed = "Hello!"
	if got := joinedAssistantDeltas(deltas); got != streamed {
		t.Fatalf("streamed deltas = %q, want %q", got, streamed)
	}
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderAPIError", err, err)
	}
	if providerErr.Code != UnifiedErrorCodeProviderContract {
		t.Fatalf("provider code = %q, want %q", providerErr.Code, UnifiedErrorCodeProviderContract)
	}
}

func TestGenerate_OmitsNonFinalEmptyAssistantContentBeforeToolCall(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":1,"content_index":0,"delta":"\n\n"}`,
		`{"type":"response.output_text.done","item_id":"msg_1","output_index":1,"content_index":0,"text":""}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":""}]}}`,
		`{"type":"response.output_item.added","output_index":2,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":2,"delta":"{\"command\":\"pwd\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":2,"arguments":"{\"command\":\"pwd\"}"}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":\"pwd\"}"}}`,
		`{"type":"response.completed","response":{"output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":""}]},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":\"pwd\"}"}]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(deltas) != 0 {
		t.Fatalf("assistant deltas = %+v, want no semantic assistant output", deltas)
	}
	if resp.AssistantText != nil {
		t.Fatalf("assistant text = %#v, want omitted non-final content", resp.AssistantText)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "shell" {
		t.Fatalf("tool calls = %+v, want shell call_1", resp.ToolCalls)
	}
}

func TestGenerate_UsesFinalizedOutputTextWhenProviderOmitsDeltas(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":"Compaction summary"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Compaction summary"}]}}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Compaction summary"}]}]}}`,
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if optionalStringValue(resp.AssistantText) != "Compaction summary" {
		t.Fatalf("assistant text = %q, want finalized output text", optionalStringValue(resp.AssistantText))
	}
	if len(resp.OutputItems) != 1 || resp.OutputItems[0].Content == nil || *resp.OutputItems[0].Content != "Compaction summary" {
		t.Fatalf("output items = %+v, want synthesized finalized assistant output", resp.OutputItems)
	}
}

func TestGenerate_DeliversTrailingWhitespaceBeforeToolCallWithoutBuffering(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"commentary","content":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"I will run it.\n\n"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"I will run it.\n\n"}]}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":1,"delta":"{\"command\":\"pwd\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":1,"arguments":"{\"command\":\"pwd\"}"}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":\"pwd\"}"}}`,
		`{"type":"response.completed","response":{"output":[{"id":"msg_1","type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"I will run it."}]},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":\"pwd\"}"}]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if got := joinedAssistantDeltas(deltas); got != "I will run it.\n\n" {
		t.Fatalf("assistant deltas = %q, want exact streamed content", got)
	}
	if optionalStringValue(resp.AssistantText) != "I will run it.\n\n" {
		t.Fatalf("assistant text = %q, want exact streamed content", optionalStringValue(resp.AssistantText))
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "shell" {
		t.Fatalf("tool calls = %+v, want shell call_1", resp.ToolCalls)
	}
}

func TestGenerate_IgnoresStructuredTrailingWhitespaceShimWithoutDeltaConsumer(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hello\n\n"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello"}]}}`,
		`{"type":"response.completed","response":{"output":[{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello"}]}]}}`,
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if optionalStringValue(resp.AssistantText) != "Hello" {
		t.Fatalf("assistant text = %q, want finalized content", optionalStringValue(resp.AssistantText))
	}
}

func TestGenerate_IgnoresLeadingWhitespaceAssistantShimBeforeContent(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"\n\nHello"}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":" world"}`,
		`{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":"\n\nHello world"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"\n\nHello world"}]}}`,
		`{"type":"response.completed","response":{"output":[{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"\n\nHello world"}]}]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if got := joinedAssistantDeltas(deltas); got != "Hello world" {
		t.Fatalf("assistant deltas = %q, want finalized content", got)
	}
	if optionalStringValue(resp.AssistantText) != "Hello world" {
		t.Fatalf("assistant text = %q, want finalized content", optionalStringValue(resp.AssistantText))
	}
}

func TestGenerate_PreservesInterleavedDeltasButRejectsUnmatchedSources(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_text.delta","output_index":0,"delta":"\nfirst"}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"\nsecond"}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":" continuation"}`,
		`{"type":"response.completed","response":{"output":[]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
	})
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) || providerErr.Code != UnifiedErrorCodeProviderContract {
		t.Fatalf("unmatched sources were accepted: %v", err)
	}
	if got := joinedAssistantDeltas(deltas); got != "firstsecond continuation" {
		t.Fatalf("assistant deltas = %q, want resumed output whitespace preserved", got)
	}
}

func TestGenerate_RejectsPendingSourceAfterAnotherSourceFinalizes(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_text.delta","output_index":0,"delta":"first"}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"second"}`,
		`{"type":"response.output_text.done","output_index":0,"text":"first"}`,
		`{"type":"response.completed","response":{"output":[]}}`,
		`[DONE]`,
	)

	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) || providerErr.Code != UnifiedErrorCodeProviderContract {
		t.Fatalf("pending source was accepted: %v", err)
	}
}

func TestGenerateAcceptsCompletedEmptyFinalAfterDoneOnlyStream(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_text.done","output_index":0,"text":""}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":""}]}}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":""}]}]}}`,
		`[DONE]`,
	)

	resp, err := NewResponsesClient(transport, nil).Generate(context.Background(), Request{SessionID: textutil.Value("test-session"),
		Model:          "gpt-6-sol",
		ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high",
	}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("generate stream: %v", err)
	}
	if resp.Assistant.Content == nil || *resp.Assistant.Content != "" {
		t.Fatalf("assistant content = %#v, want present empty content", resp.Assistant.Content)
	}
	if resp.Assistant.Phase == nil || *resp.Assistant.Phase != MessagePhaseFinal {
		t.Fatalf("assistant phase = %#v, want final", resp.Assistant.Phase)
	}
}

func TestGenerate_PreservesWhitespaceBetweenAssistantContent(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hello "}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"world"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello world"}]}}`,
		`{"type":"response.completed","response":{"output":[{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello world"}]}]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if got := joinedAssistantDeltas(deltas); got != "Hello world" {
		t.Fatalf("assistant deltas = %q, want preserved interstitial whitespace", got)
	}
	if optionalStringValue(resp.AssistantText) != "Hello world" {
		t.Fatalf("assistant text = %q, want preserved interstitial whitespace", optionalStringValue(resp.AssistantText))
	}
}

func TestGenerate_DoesNotRepairMultiMessageAssistantOutputWithAggregateText(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"A"}]}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"B"}]}}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"A"}]},{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"B"}]}]}}`,
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if optionalStringValue(resp.AssistantText) != "AB" {
		t.Fatalf("assistant text = %q, want aggregate completed text", optionalStringValue(resp.AssistantText))
	}
	if len(resp.OutputItems) != 2 ||
		resp.OutputItems[0].Content == nil || *resp.OutputItems[0].Content != "A" ||
		resp.OutputItems[1].Content == nil || *resp.OutputItems[1].Content != "B" {
		t.Fatalf("output items = %+v, want original assistant message segments", resp.OutputItems)
	}
}

func TestGenerate_MapsStructuredStreamErrorToProviderAPIError(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t, `{"type":"error","error":{"type":"invalid_request_error","code":"context_length_exceeded","param":"input","message":"too many tokens"}}`)

	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err == nil {
		t.Fatal("expected stream error")
	}
	if !IsContextLengthOverflowError(err) {
		t.Fatalf("expected context overflow classification, got %v", err)
	}
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderAPIError, got %T", err)
	}
	if providerErr.ProviderCode != "context_length_exceeded" || providerErr.ProviderParam != "input" {
		t.Fatalf("unexpected provider error: %+v", providerErr)
	}
}

func TestGenerate_MapsProviderOverloadCodeWithoutMessageMatching(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"error","error":{"type":"server_error","code":"server_is_overloaded","param":"request","message":"not the overload wording"}}`,
		`[DONE]`,
	)
	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	var providerErr *ProviderAPIError
	if err == nil || !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderAPIError, got %T", err)
	}
	if providerErr.Code != UnifiedErrorCodeProviderOverload || providerErr.ProviderCode != "server_is_overloaded" || !HasHTTPStatus(providerErr, http.StatusOK) {
		t.Fatalf("unexpected provider overload: %+v", providerErr)
	}
	if got := classifyOpenAIUnifiedErrorCode(textutil.Value(http.StatusUnauthorized), "server_is_overloaded", ""); got != UnifiedErrorCodeAuthentication || classifyOpenAIUnifiedErrorCode(textutil.Value(http.StatusOK), "SERVER_IS_OVERLOADED", "") != UnifiedErrorCodeUnknown {
		t.Fatalf("terminal HTTP status classification = %q, want %q", got, UnifiedErrorCodeAuthentication)
	}
}
func TestGenerate_MapsResponseErrorEventToProviderAPIError(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"error","code":"context_length_exceeded","param":"input","message":"too many tokens","sequence_number":1}`,
		`[DONE]`,
	)

	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err == nil {
		t.Fatal("expected response error event")
	}
	if !IsContextLengthOverflowError(err) {
		t.Fatalf("expected context overflow classification, got %v", err)
	}
}

func TestGenerate_MapsResponseFailedEventToProviderAPIError(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.failed","sequence_number":1,"response":{"id":"resp_1","created_at":1,"error":{"code":"context_length_exceeded","message":"too many tokens"}}}`,
		`[DONE]`,
	)

	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err == nil {
		t.Fatal("expected response failed event")
	}
	if !IsContextLengthOverflowError(err) {
		t.Fatalf("expected context overflow classification, got %v", err)
	}
}

func TestGenerate_ReturnsUnknownProviderErrorForUnrecognizedStructuredStreamError(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"error","details":{"unexpected":"shape"},"sequence_number":1}`,
		`[DONE]`,
	)

	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err == nil {
		t.Fatal("expected unrecognized stream error")
	}
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderAPIError, got %T", err)
	}
	if providerErr.Code != UnifiedErrorCodeUnknown {
		t.Fatalf("provider code = %q, want %q", providerErr.Code, UnifiedErrorCodeUnknown)
	}
}

func TestGenerate_ParsesCustomPatchToolCall(t *testing.T) {
	patchInput := "*** Begin Patch\n*** Add File: a.txt\n+hi\n*** End Patch\n"
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","item":{"id":"ct_1","type":"custom_tool_call","name":"patch","call_id":"call_1","input":""}}`,
		fmt.Sprintf(`{"type":"response.custom_tool_call_input.delta","item_id":"ct_1","delta":%q}`, patchInput),
		fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ct_1","name":"patch","call_id":"call_1","input":%q}}`, patchInput),
		fmt.Sprintf(`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"output":[{"type":"custom_tool_call","id":"ct_1","name":"patch","call_id":"call_1","input":%q}]}}`, patchInput),
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "patch" {
		t.Fatalf("unexpected custom tool call: %+v", resp.ToolCalls[0])
	}
	if !resp.ToolCalls[0].Custom || resp.ToolCalls[0].CustomInput == nil || *resp.ToolCalls[0].CustomInput != patchInput {
		t.Fatalf("unexpected custom patch tool call: %+v", resp.ToolCalls[0])
	}
	if len(resp.OutputItems) != 1 || resp.OutputItems[0].Type != ResponseItemTypeCustomToolCall || resp.OutputItems[0].CustomInput == nil || *resp.OutputItems[0].CustomInput != patchInput {
		t.Fatalf("unexpected custom output item: %+v", resp.OutputItems)
	}
	if resp.OutputItems[0].ID == nil || *resp.OutputItems[0].ID != "ct_1" || resp.OutputItems[0].CallID == nil || *resp.OutputItems[0].CallID != "call_1" {
		t.Fatalf("provider item and call identities were not preserved: %+v", resp.OutputItems[0])
	}
}

func TestGenerate_PartitionsReasoningStatusFromTrace(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"**Preparing patch**\n\nPlain summary text"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"**Preparing patch**\n\nPlain summary text"}],"encrypted_content":"enc_1"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"**Preparing patch**\n\nPlain summary text"}],"encrypted_content":"enc_1"}]}}`,
		`[DONE]`,
	)

	var reasoning []ReasoningSummaryDelta
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) {
			reasoning = append(reasoning, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(reasoning) != 1 {
		t.Fatalf("expected 1 reasoning delta callback, got %+v", reasoning)
	}
	if reasoning[0].Text != "\n\nPlain summary text" {
		t.Fatalf("summary = %q", reasoning[0].Text)
	}
	if reasoning[0].CurrentStatus == nil || reasoning[0].CurrentStatus.Text != "Preparing patch" {
		t.Fatalf("unexpected current status: %+v", reasoning[0].CurrentStatus)
	}
	if len(resp.Reasoning) != 1 || resp.Reasoning[0].Text != "Plain summary text" {
		t.Fatalf("unexpected final reasoning summary entries: %+v", resp.Reasoning)
	}
	for _, entry := range []ReasoningSummaryDelta{
		reasoning[0],
		{SourceCoordinate: resp.Reasoning[0].SourceCoordinate, ItemIdentity: resp.Reasoning[0].ItemIdentity},
	} {
		if entry.SourceCoordinate == nil || entry.SourceCoordinate.OutputIndex == nil ||
			*entry.SourceCoordinate.OutputIndex != 0 || entry.SourceCoordinate.PartIndex == nil ||
			*entry.SourceCoordinate.PartIndex != 0 || entry.ItemIdentity == nil ||
			entry.ItemIdentity.ItemID != "rs_1" || entry.ItemIdentity.PartIndex == nil ||
			*entry.ItemIdentity.PartIndex != 0 {
			t.Fatalf("partition lost provider identity: %+v", entry)
		}
	}
}

func TestGenerate_StatusOnlyReasoningRemainsProviderHistoryButNotTrace(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"**Preparing patch**"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"**Preparing patch**"}],"encrypted_content":"enc_1"}}`,
		`{"type":"response.completed","response":{"output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"**Preparing patch**"}],"encrypted_content":"enc_1"}]}}`,
		`[DONE]`,
	)
	var deltas []ReasoningSummaryDelta
	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(deltas) != 1 || deltas[0].Text != "" || deltas[0].CurrentStatus == nil {
		t.Fatalf("status-only output leaked a trace: %+v", deltas)
	}
	if len(resp.Reasoning) != 0 {
		t.Fatalf("status-only output became completed trace: %+v", resp.Reasoning)
	}
	if len(resp.ReasoningItems) != 1 || resp.ReasoningItems[0].EncryptedContent != "enc_1" {
		t.Fatalf("provider reasoning item lost: %+v", resp.ReasoningItems)
	}
	if len(resp.OutputItems) != 1 {
		t.Fatalf("provider output lost: %+v", resp.OutputItems)
	}
	var item struct {
		Summary []struct {
			Text string `json:"text"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(resp.OutputItems[0].Raw, &item); err != nil {
		t.Fatalf("decode preserved provider output: %v", err)
	}
	if len(item.Summary) != 1 || item.Summary[0].Text != "**Preparing patch**" {
		t.Fatalf("provider history changed: %+v", item)
	}
}

func TestGenerate_ReasoningCallbacksCarryCurrentAccumulatedStatus(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"**Checking"}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":" tests**"}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"\nDetails"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"**Checking tests**\nDetails"}],"encrypted_content":"enc_1"}}`,
		`{"type":"response.completed","response":{"output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"**Checking tests**\nDetails"}],"encrypted_content":"enc_1"}]}}`,
		`[DONE]`,
	)

	var reasoning []ReasoningSummaryDelta
	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) {
			reasoning = append(reasoning, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(reasoning) != 3 {
		t.Fatalf("expected 3 reasoning callbacks, got %+v", reasoning)
	}
	if reasoning[0].CurrentStatus != nil {
		t.Fatalf("incomplete callback status = %+v, want absence", reasoning[0].CurrentStatus)
	}
	for index := 1; index < len(reasoning); index++ {
		if reasoning[index].CurrentStatus == nil || reasoning[index].CurrentStatus.Text != "Checking tests" {
			t.Fatalf("callback %d status = %+v, want Checking tests", index, reasoning[index].CurrentStatus)
		}
	}
	if reasoning[1].Text != "" {
		t.Fatalf("status-only callback leaked trace text = %q", reasoning[1].Text)
	}
	if reasoning[2].Text != "\nDetails" {
		t.Fatalf("final callback text = %q", reasoning[2].Text)
	}
}

func TestGenerate_ReasoningCallbackNilStatusMeansCurrentAbsence(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"**Checking tests**"}`,
		`{"type":"response.reasoning_summary_text.done","item_id":"rs_1","output_index":0,"summary_index":0,"text":"Plain summary"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Plain summary"}],"encrypted_content":"enc_1"}}`,
		`{"type":"response.completed","response":{"output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Plain summary"}],"encrypted_content":"enc_1"}]}}`,
		`[DONE]`,
	)

	var reasoning []ReasoningSummaryDelta
	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) {
			reasoning = append(reasoning, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(reasoning) != 2 {
		t.Fatalf("expected 2 reasoning callbacks, got %+v", reasoning)
	}
	if reasoning[0].CurrentStatus == nil || reasoning[0].CurrentStatus.Text != "Checking tests" {
		t.Fatalf("first callback status = %+v, want Checking tests", reasoning[0].CurrentStatus)
	}
	if reasoning[1].CurrentStatus != nil {
		t.Fatalf("replacement callback status = %+v, want current absence", reasoning[1].CurrentStatus)
	}
}

func TestGenerate_EmptyReasoningSnapshotClearsCurrentStatus(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"**Checking tests**"}`,
		`{"type":"response.reasoning_summary_text.done","item_id":"rs_1","output_index":0,"summary_index":0,"text":""}`,
		`{"type":"response.completed","response":{"output":[]}}`,
		`[DONE]`,
	)

	var reasoning []ReasoningSummaryDelta
	_, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{
		OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) {
			reasoning = append(reasoning, delta)
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(reasoning) != 2 {
		t.Fatalf("expected 2 reasoning callbacks, got %+v", reasoning)
	}
	if reasoning[1].Text != "" {
		t.Fatalf("empty snapshot callback text = %q", reasoning[1].Text)
	}
	if reasoning[1].CurrentStatus != nil {
		t.Fatalf("empty snapshot callback status = %+v, want current absence", reasoning[1].CurrentStatus)
	}
}

func TestGenerate_RejectsEmptyCompletedMessageAfterAssistantDeltas(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"commentary","content":[]}}`,
		`{"type":"response.output_text.delta","delta":"Hel"}`,
		`{"type":"response.output_text.delta","delta":"lo"}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"fc_1","type":"function_call","name":"shell","call_id":"call_1","arguments":"{\"command\":\"pwd\"}"}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","name":"shell","call_id":"call_1","arguments":"{\"command\":\"pwd\"}"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7},"output":[{"id":"msg_1","type":"message","role":"assistant","content":[]},{"type":"function_call","id":"fc_1","name":"shell","call_id":"call_1","arguments":"{\"command\":\"pwd\"}"}]}}`,
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err == nil {
		t.Fatalf("Generate response = %+v, want provider contract error", resp)
	}
	var providerErr *ProviderAPIError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T %v, want ProviderAPIError", err, err)
	}
	if providerErr.Code != UnifiedErrorCodeProviderContract {
		t.Fatalf("provider code = %q, want %q", providerErr.Code, UnifiedErrorCodeProviderContract)
	}
}

func TestGenerate_PreservesAssistantOutputItemPhaseWhenCompletedPhaseIsMissing(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done"}]}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":2,"total_tokens":4},"output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Done"}]}]}}`,
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if optionalStringValue(resp.AssistantText) != "Done" {
		t.Fatalf("assistant text = %q, want Done", optionalStringValue(resp.AssistantText))
	}
	if !resp.ProviderPhase.Is(MessagePhaseFinal) {
		t.Fatalf("provider phase = %#v, want %q", resp.ProviderPhase, MessagePhaseFinal)
	}
	if len(resp.OutputItems) != 1 {
		t.Fatalf("expected 1 output item, got %+v", resp.OutputItems)
	}
	if resp.OutputItems[0].Phase == nil || *resp.OutputItems[0].Phase != MessagePhaseFinal {
		t.Fatalf("assistant output phase = %v, want %q", resp.OutputItems[0].Phase, MessagePhaseFinal)
	}
}

func TestGenerate_PrefersPhaseResolvedAssistantTextOverRawDeltaConcatenation(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Draft: "}]}}`,
		`{"type":"response.output_text.delta","delta":"Draft: "}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done"}]}}`,
		`{"type":"response.output_text.delta","delta":"Done"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","name":"shell","call_id":"call_1","arguments":"{\"command\":\"pwd\"}"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5},"output":[{"type":"function_call","id":"fc_1","name":"shell","call_id":"call_1","arguments":"{\"command\":\"pwd\"}"}]}}`,
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if optionalStringValue(resp.AssistantText) != "Done" {
		t.Fatalf("assistant text = %q, want Done", optionalStringValue(resp.AssistantText))
	}
	if !resp.ProviderPhase.Is(MessagePhaseFinal) {
		t.Fatalf("provider phase = %#v, want %q", resp.ProviderPhase, MessagePhaseFinal)
	}
}

func TestGenerate_EmitsCommentaryThenFinalDeltasWithTheirOutputItemPhases(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_commentary","type":"message","role":"assistant","phase":"commentary","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"Checking sources."}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_commentary","type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking sources."}]}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_final","type":"message","role":"assistant","phase":"final_answer","content":[]}}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"Final answer."}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"msg_final","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Final answer."}]}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":4,"total_tokens":6},"output":[{"id":"msg_commentary","type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking sources."}]},{"id":"msg_final","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Final answer."}]}]}}`,
		`[DONE]`,
	)

	var deltas []AssistantDelta
	resp, err := transport.Generate(
		context.Background(),
		ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"},
		StreamCallbacks{OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		}},
	)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(deltas) != 2 ||
		deltas[0].Text != "Checking sources." ||
		deltas[0].Phase != MessagePhaseCommentary ||
		deltas[1].Text != "Final answer." ||
		deltas[1].Phase != MessagePhaseFinal {
		t.Fatalf("assistant deltas = %+v, want commentary then final output-item phases", deltas)
	}
	if optionalStringValue(resp.AssistantText) != "Final answer." {
		t.Fatalf("assistant text = %q, want final output item", optionalStringValue(resp.AssistantText))
	}
	if !resp.ProviderPhase.Is(MessagePhaseFinal) {
		t.Fatalf("provider phase = %#v, want %q", resp.ProviderPhase, MessagePhaseFinal)
	}
}

func TestGenerate_KeepsFinalizedAssistantAtNonZeroOutputIndex(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Plan"}],"encrypted_content":"enc_1"}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","name":"shell","call_id":"call_1","arguments":"{\"command\":\"pwd\"}"}}`,
		`{"type":"response.output_item.done","output_index":2,"item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done"}]}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5},"output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Plan"}],"encrypted_content":"enc_1"},{"type":"function_call","id":"fc_1","name":"shell","call_id":"call_1","arguments":"{\"command\":\"pwd\"}"}]}}`,
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(resp.OutputItems) != 3 {
		t.Fatalf("expected repaired output items, got %+v", resp.OutputItems)
	}
	if resp.OutputItems[0].Type != ResponseItemTypeReasoning || resp.OutputItems[0].OutputIndex != 0 {
		t.Fatalf("expected reasoning item to stay first, got %+v", resp.OutputItems[0])
	}
	if resp.OutputItems[1].Type != ResponseItemTypeFunctionCall || resp.OutputItems[1].OutputIndex != 1 {
		t.Fatalf("expected tool call to stay second, got %+v", resp.OutputItems[1])
	}
	if resp.OutputItems[2].Type != ResponseItemTypeMessage || resp.OutputItems[2].OutputIndex != 2 || resp.OutputItems[2].Content == nil || *resp.OutputItems[2].Content != "Done" {
		t.Fatalf("expected synthesized assistant item inserted at output_index=2, got %+v", resp.OutputItems[2])
	}
}

func TestGenerate_PreservesHostedWebSearchOutputItemFromStream(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"web_search_call","id":"ws_1","status":"in_progress","results":null,"action":{"type":"search","query":"kent cli"}}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"kent cli","queries":["kent cli","kent docs"],"sources":[{"type":"url","url":"https://kent.sh"}]},"results":[{"type":"text_result","title":"Kent","url":"https://kent.sh","snippet":"excluded snippet"}],"usage":{"searches":1}}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done"}]}}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done"}]}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5},"output":[{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"kent cli","queries":["kent cli","kent docs"],"sources":[{"type":"url","url":"https://kent.sh"}]},"results":[{"type":"text_result","title":"Kent","url":"https://kent.sh","snippet":"excluded snippet"}],"usage":{"searches":1}},{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Done"}]}]}}`,
		`[DONE]`,
	)

	resp, err := transport.Generate(context.Background(), ResponsesRequest{SessionID: textutil.Value("test-session"), ToolChoiceMode: ToolChoiceModeAutomatic, Model: "gpt-6-sol", ReasoningEffort: "high"}, StreamCallbacks{})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if optionalStringValue(resp.AssistantText) != "Done" {
		t.Fatalf("assistant text = %q, want Done", optionalStringValue(resp.AssistantText))
	}
	if len(resp.OutputItems) != 2 {
		t.Fatalf("expected hosted passthrough output item + assistant message, got %+v", resp.OutputItems)
	}
	foundAssistant := false
	foundHosted := false
	for _, item := range resp.OutputItems {
		if item.Type == ResponseItemTypeMessage && item.Content != nil && *item.Content == "Done" {
			foundAssistant = true
		}
		if item.Type != ResponseItemTypeOther {
			continue
		}
		var hosted struct {
			Status  string `json:"status"`
			Results []struct {
				Title string `json:"title"`
			} `json:"results"`
		}
		if err := json.Unmarshal(item.Raw, &hosted); err != nil {
			t.Fatal(err)
		}
		if hosted.Status != "completed" || len(hosted.Results) != 1 || hosted.Results[0].Title != "Kent" {
			t.Fatalf("completed search facts were lost: %+v", hosted)
		}
		foundHosted = true
	}
	if !foundHosted {
		t.Fatalf("expected passthrough web_search_call in output items, got %+v", resp.OutputItems)
	}
	if !foundAssistant {
		t.Fatalf("expected assistant message in output items, got %+v", resp.OutputItems)
	}
	if len(resp.ProviderEvidence.HostedTools) != 1 {
		t.Fatalf("hosted tool evidence = %+v, want one streamed observation", resp.ProviderEvidence.HostedTools)
	}
	hosted := resp.ProviderEvidence.HostedTools[0]
	if hosted.ID == nil || *hosted.ID != "ws_1" ||
		hosted.Type == nil || *hosted.Type != "web_search_call" ||
		hosted.Status == nil || *hosted.Status != "completed" ||
		hosted.ActionKind == nil || *hosted.ActionKind != "search" {
		t.Fatalf("streamed hosted tool evidence = %+v", hosted)
	}
	assertJSONField(t, hosted.Usage, "searches", "1")
}
