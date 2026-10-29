package llm

import (
	"context"
	"core/shared/config"
	"core/shared/textutil"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponsesClientSelectsServedModelMetadata(t *testing.T) {
	tests := []struct {
		name           string
		createdModel   string
		completedModel string
		headers        http.Header
		want           *string
	}{
		{
			name:           "standard model wins",
			completedModel: " standard-model ",
			headers:        http.Header{"Openai-Model": {"header-model"}, "X-Openai-Model": {"fallback-model"}},
			want:           stringPointer("standard-model"),
		},
		{
			name:    "provider header priority",
			headers: http.Header{"Openai-Model": {" ", " routed-model "}, "X-Openai-Model": {"fallback-model"}},
			want:    stringPointer("routed-model"),
		},
		{
			name:           "stream first standard model wins",
			createdModel:   " created-model ",
			completedModel: "completed-model",
			headers:        http.Header{"Openai-Model": {"header-model"}},
			want:           stringPointer("created-model"),
		},
		{
			name:           "stream completed standard model wins",
			completedModel: " completed-model ",
			headers:        http.Header{"Openai-Model": {"header-model"}},
			want:           stringPointer("completed-model"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var capturedModel string
			transport := newResponseMetadataTransport(t, test.headers, func(raw json.RawMessage) []string {
				capturedModel = decodeRequestedModel(t, raw)
				return successfulMetadataEvents(test.createdModel, test.completedModel)
			})
			request := Request{SessionID: stringPointer("metadata-session"), Model: "requested-model", ToolChoiceMode: ToolChoiceModeAutomatic}
			client := NewResponsesClient(transport, nil)
			response, err := client.Generate(context.Background(), request, StreamCallbacks{})
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			if (response.ServedModel == nil) != (test.want == nil) ||
				response.ServedModel != nil && *response.ServedModel != *test.want {
				t.Fatalf("served model = %#v, want %#v", response.ServedModel, test.want)
			}
			if request.Model != "requested-model" || capturedModel != "requested-model" {
				t.Fatalf("requested model changed: request=%q wire=%q", request.Model, capturedModel)
			}
		})
	}
}

func TestResponsesClientParsesStrictReasoningIncludedHeader(t *testing.T) {
	headers := []struct {
		name  string
		value *string
		want  bool
	}{
		{name: "explicit true", value: stringPointer(" true "), want: true},
		{name: "absent"},
	}
	for _, header := range headers {
		t.Run(header.name, func(t *testing.T) {
			responseHeaders := make(http.Header)
			if header.value != nil {
				responseHeaders.Set("x-reasoning-included", *header.value)
			}
			transport := newResponseMetadataTransport(t, responseHeaders, func(json.RawMessage) []string {
				return successfulMetadataEvents("", "")
			})

			request := Request{SessionID: stringPointer("metadata-session"), Model: "requested-model", ToolChoiceMode: ToolChoiceModeAutomatic}
			client := NewResponsesClient(transport, nil)
			response, err := client.Generate(context.Background(), request, StreamCallbacks{})
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			if response.ReasoningIncluded != header.want {
				t.Fatalf("reasoning included = %v, want %v", response.ReasoningIncluded, header.want)
			}
		})
	}
}

func TestResponsesResponseErrorsPreserveHeaderDiagnostics(t *testing.T) {
	diagnosticHeaders := http.Header{"X-Request-Id": {" ", " request-primary "}, "X-Oai-Request-Id": {"request-fallback"},
		"X-Openai-Authorization-Error": {" ", " token rejected "}}
	for _, test := range []struct {
		name     string
		err      error
		wantCode UnifiedErrorCode
	}{
		{name: "provider contract", err: newOpenAIProviderContractError("openai", &http.Response{
			StatusCode: http.StatusOK, Header: diagnosticHeaders.Clone(),
		}, errors.New("invalid response")), wantCode: UnifiedErrorCodeProviderContract},
	} {
		t.Run(test.name, func(t *testing.T) {
			var providerErr *ProviderAPIError
			if !errors.As(test.err, &providerErr) {
				t.Fatalf("error = %T %v, want ProviderAPIError", test.err, test.err)
			}
			if providerErr.Code != test.wantCode {
				t.Fatalf("unified code = %q, want %q", providerErr.Code, test.wantCode)
			}
			if providerErr.ProviderRequestID == nil || *providerErr.ProviderRequestID != "request-primary" {
				t.Fatalf("provider request ID = %#v, want request-primary", providerErr.ProviderRequestID)
			}
			if providerErr.AuthorizationDiagnostic == nil || *providerErr.AuthorizationDiagnostic != "token rejected" {
				t.Fatalf("authorization diagnostic = %#v, want token rejected", providerErr.AuthorizationDiagnostic)
			}
		})
	}
}

func newResponseMetadataTransport(t *testing.T, headers http.Header, respond func(json.RawMessage) []string) *HTTPTransport {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		for name, values := range headers {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		serveResponsesTestWebsocket(t, w, request, respond)
	}))
	t.Cleanup(server.Close)
	transport := newTestHTTPTransport(t, staticAuth{}, testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)}))

	transport.Client = newRewritingHTTPClient(t, server)
	return transport
}

func decodeRequestedModel(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return payload.Model
}

func successfulMetadataEvents(createdModel string, completedModel string) []string {
	response := fmt.Sprintf(
		`{"id":"resp_1","object":"response","model":%q,"output":[{"type":"message","id":"msg_1","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		completedModel,
	)
	var events []string
	if createdModel != "" {
		events = append(events, fmt.Sprintf(`{"type":"response.created","response":{"model":%q}}`, createdModel))
	}
	return append(events, fmt.Sprintf(`{"type":"response.completed","response":%s}`, response))
}
func stringPointer(value string) *string {
	return &value
}
