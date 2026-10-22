package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"core/shared/config"
	"core/shared/textutil"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	sdkwebsocket "github.com/openai/openai-go/v3/packages/websocket"
)

func TestFirstPartyResponsesStreamsThroughManagedWebsocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("selected connection credential was not sent")
		}
		_, raw, err := socket.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		var create struct {
			Type  string `json:"type"`
			Model string `json:"model"`
			Store *bool  `json:"store"`
		}
		if err := json.Unmarshal(raw, &create); err != nil {
			t.Error(err)
		}
		if create.Type != "response.create" || create.Model != "gpt-6-sol" || create.Store == nil || *create.Store {
			t.Errorf("prepared generation settings lost: %+v", create)
		}
		for _, event := range []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"reasoning-native","summary":[]}}`,
			`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"item_id":"reasoning-native","delta":"plan"}`,
			`{"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"native","role":"assistant","content":[]}}`,
			`{"type":"response.output_text.delta","output_index":1,"delta":"answer"}`,
			`{"type":"response.completed","response":{"id":"resp-native","status":"completed","model":"gpt-6-sol","output":[{"type":"reasoning","id":"reasoning-native","summary":[{"type":"summary_text","text":"plan"}],"encrypted_content":"opaque"},{"type":"message","id":"native","role":"assistant","content":[{"type":"output_text","text":"answer","annotations":[]}]},{"type":"function_call","id":"call-item","call_id":"call-native","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}],"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}}`,
		} {
			if err := socket.Write(ctx, websocket.MessageText, []byte(event)); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: staticAuthHeader{}, HTTPClient: server.Client(), Model: "gpt-6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	var text, reasoning string
	response, err := client.Generate(ctx, Request{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value(uuid.NewString()), ToolChoiceMode: ToolChoiceModeAutomatic}, StreamCallbacks{
		OnAssistantDelta:        func(delta AssistantDelta) { text += delta.Text },
		OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) { reasoning += delta.Text },
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "answer" || response.Assistant.Content == nil || *response.Assistant.Content != text || response.Assistant.NativeResponse == nil {
		t.Fatal("streamed native response was not projected through the provider client")
	}
	if reasoning != "plan" || len(response.ToolCalls) != 1 || response.ToolCalls[0].ID != "call-native" || response.ToolCalls[0].OutputIndex == nil || *response.ToolCalls[0].OutputIndex != 2 {
		t.Fatal("reasoning or indexed native tool output was lost across the SDK event bridge")
	}
}

func TestResponsesSocketFailsWhenItsReceiveQueueExceeds1600Events(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	blocked, release, sent := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		defer close(sent)
		if _, _, err := socket.Read(ctx); err != nil {
			return
		}
		frame := []byte(`{"type":"response.created","response":{"id":"pending","output":[]}}`)
		if err := socket.Write(ctx, websocket.MessageText, frame); err != nil {
			return
		}
		select {
		case <-blocked:
		case <-ctx.Done():
			return
		}
		for range 1601 {
			if err := socket.Write(ctx, websocket.MessageText, frame); err != nil {
				return
			}
		}
		_, _, _ = socket.Read(ctx)
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	transport := newTestHTTPTransport(t, staticAuthHeader{}, registration)
	done := make(chan error, 1)
	var first sync.Once
	go func() {
		_, err := transport.Generate(ctx, ResponsesRequest{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic}, StreamCallbacks{
			OnStreamActivity: func() { first.Do(func() { close(blocked); <-release }) },
		})
		done <- err
	}()
	select {
	case <-sent:
	case <-ctx.Done():
		close(release)
		t.Fatal(ctx.Err())
	}
	close(release)
	if err := <-done; !errors.Is(err, sdkwebsocket.ErrBufferLimit) {
		t.Fatalf("receive bound failure = %v", err)
	}
}

func TestResponsesSocketRetainsOpeningCredentialsAcrossCompletedCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveResponsesTestWebsocket(t, w, r, func(json.RawMessage) []string {
			return []string{completedResponseSSEJSON}
		})
	}))
	t.Cleanup(server.Close)
	auth := &countingAuth{}
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	transport := newTestHTTPTransport(t, auth, registration)
	request := ResponsesRequest{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic}
	for range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		if _, err := transport.Generate(ctx, request, StreamCallbacks{}); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
	}
	if auth.calls.Load() != 1 {
		t.Fatalf("credential resolutions on one reusable socket = %d", auth.calls.Load())
	}
}

func TestResponsesStopAbandonsOnlyActiveSocketAndNextCallStartsFresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started, abandoned := make(chan struct{}), make(chan struct{})
	var openings atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer socket.CloseNow()
		if _, _, err := socket.Read(ctx); err != nil {
			t.Error(err)
			return
		}
		if openings.Add(1) == 1 {
			close(started)
			_, _, _ = socket.Read(ctx)
			close(abandoned)
			return
		}
		if err := socket.Write(ctx, websocket.MessageText, []byte(completedResponseSSEJSON)); err != nil {
			t.Error(err)
		}
		_, _, _ = socket.Read(ctx)
	}))
	defer server.Close()
	registration := testConnectionRegistration(t, config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value(defaultOpenAIBaseURL)})
	registration.Variant.BaseURL = textutil.Value(server.URL + "/v1")
	auth := &countingAuth{}
	client, err := NewProviderClient(ProviderClientOptions{Registration: registration, Auth: auth, HTTPClient: server.Client(), Model: "gpt-6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.(io.Closer).Close(); err != nil {
			t.Error(err)
		}
	}()
	request := Request{Model: "gpt-6-sol", ReasoningEffort: "high", SessionID: textutil.Value("session"), ToolChoiceMode: ToolChoiceModeAutomatic}
	active, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := client.Generate(active, request, StreamCallbacks{})
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop returned %v", err)
	}
	select {
	case <-abandoned:
	case <-ctx.Done():
		t.Fatal("Stop did not release the provider socket")
	}
	if _, err := client.Generate(ctx, request, StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	if openings.Load() != 2 || auth.calls.Load() != 2 {
		t.Fatalf("fresh invocation: sockets=%d credentials=%d", openings.Load(), auth.calls.Load())
	}
}
