package nano

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"core/internal/testharness/httpclient"
	"core/internal/testharness/testsetup"
	"core/server/auth"
	"core/server/authservice"
	"core/server/launch"
	"core/server/llm"
	"core/server/runtimewire"
	"core/shared/config"
	"core/shared/jsoncontract"
	"core/shared/textutil"

	"github.com/google/uuid"
)

const completedResponse = `{"id":"resp_nano","object":"response","status":"completed","model":"local-model","output":[{"id":"msg_nano","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Fix invoice totals","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`

func nanoTestConfig(t *testing.T, endpoint string) (config.Settings, *authservice.ConnectionResolver) {
	t.Helper()
	root := t.TempDir()
	body := fmt.Sprintf("[connections.local]\nprotocol = \"responses\"\nendpoint = %q\n[nano]\nconnection = \"local\"\nmodel = \"local-model\"\nmodel_context_window = 2500\n", endpoint)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	return app.Settings, authservice.NewConnectionResolver(root, nil, nil)
}

func TestNanoChatGPTDispatchIdentityAndRetry(t *testing.T) {
	parent, endParent := context.WithCancel(t.Context())
	parentID := uuid.NewString()
	type metadata struct {
		SessionID   string `json:"session_id"`
		ThreadID    string `json:"thread_id"`
		TurnID      string `json:"turn_id"`
		WindowID    string `json:"window_id"`
		RequestKind string `json:"request_kind"`
	}
	var received []metadata
	var identities, states []string
	var observations sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observations.Lock()
		defer observations.Unlock()
		var body struct {
			ClientMetadata map[string]string `json:"client_metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		var facts metadata
		if err := json.Unmarshal([]byte(body.ClientMetadata["x-codex-turn-metadata"]), &facts); err != nil {
			t.Error(err)
		}
		received = append(received, facts)
		identities = append(identities, r.Header.Get("session-id"))
		states = append(states, r.Header.Get("x-codex-turn-state"))
		if len(received) == 1 {
			endParent()
			w.Header().Set("x-codex-turn-state", "opaque,state=value")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"message":"retry","type":"server_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", completedResponse)
	}))
	defer server.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[connections.subscription]\nprotocol = \"chatgpt-codex\"\n[nano]\nconnection = \"subscription\"\nmodel = \"gpt-6-sol\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), nil)
	if err := manager.SaveOAuth(t.Context(), "subscription", auth.OAuthMethod{AccessToken: "fixture", AccountID: "fixture-account"}); err != nil {
		t.Fatal(err)
	}
	resolver := authservice.NewConnectionResolver(root, manager, nil)
	targetURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	// Reuse the production factory seam and existing HTTP fixture adapter;
	// the actual provider client, authentication and serializer still dispatch.
	factory := runtimewire.RuntimeClientFactoryFunc(func(ctx context.Context, request runtimewire.RuntimeClientRequest) (llm.Client, error) {
		selected, err := llm.ResolveConnectionVariant(request.Connection.Definition)
		if err != nil {
			return nil, err
		}
		return llm.NewProviderClient(llm.ProviderClientOptions{
			Registration: selected, Model: request.ActiveSettings.Model,
			Auth: request.Connection.Auth, ConnectionID: &request.Connection.ID,
			ContextWindowTokens: request.ActiveSettings.ModelContextWindow,
			HTTPClient:          &http.Client{Transport: httpclient.NewURLRewriteTransport(targetURL, server.Client().Transport, "")},
			RequestCapabilities: &request.RequestCapabilities,
			UsageObserver:       request.UsageObserver,
		})
	})
	service, err := NewService(5, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir())), resolver, factory)
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := service.Admit()
	if err != nil {
		t.Fatal(err)
	}
	_, err = invoke(t.Context(), app.Settings, func(target llm.NanoTarget) (llm.Request, error) {
		request, err := nanoTestRequest(target)
		request.SessionID = &parentID
		request.CodexDispatch, _ = llm.NewCodexDispatchContext(llm.CodexDispatchFacts{
			SessionID: parentID, StepID: uuid.NewString(), CompactionGeneration: 9, RequestKind: llm.CodexRequestKindTurn.Optional(),
		})
		return request, err
	})
	if err != nil {
		t.Fatal(err)
	}
	observations.Lock()
	defer observations.Unlock()
	if parent.Err() == nil || len(received) != 2 || received[0] != received[1] || identities[0] != identities[1] || states[0] != "" || states[1] != "opaque,state=value" {
		t.Fatalf("retry identity/context = %+v, identities = %v, states = %v", received, identities, states)
	}
	facts := received[0]
	id, err := uuid.Parse(facts.SessionID)
	if err != nil || id.Version() != 4 || facts.SessionID == parentID || facts.ThreadID != facts.SessionID ||
		facts.TurnID != facts.SessionID || facts.WindowID != facts.SessionID+":0" || facts.RequestKind != "turn" || identities[0] != facts.SessionID {
		t.Fatalf("nano dispatch mapping = %+v", facts)
	}
}

func TestNanoToolsAndStructuredResultForwarding(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			ToolChoice string            `json:"tool_choice"`
			Tools      []json.RawMessage `json:"tools"`
			Text       struct {
				Format struct {
					Type string `json:"type"`
				} `json:"format"`
			} `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.ToolChoice != "required" || len(body.Tools) != 1 || body.Text.Format.Type != "json_schema" {
			t.Errorf("request features = %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_tool\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"local-model\",\"output\":[{\"id\":\"fc1\",\"type\":\"function_call\",\"call_id\":\"call1\",\"name\":\"choose\",\"arguments\":\"{}\",\"status\":\"completed\"}]}}\n\n")
	}))
	defer server.Close()
	settings, resolver := nanoTestConfig(t, server.URL+"/v1")
	service, err := NewService(5, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir())), resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(target llm.NanoTarget) (llm.Request, error) {
		request, err := nanoTestRequest(target)
		if err != nil {
			return request, err
		}
		schema, err := jsoncontract.NewPreparer(false).Function("choose", struct{}{})
		if err != nil {
			return request, err
		}
		output, err := jsoncontract.NewPreparer(false).Structured("choice", struct{}{})
		if err != nil {
			return request, err
		}
		request.Tools = []llm.Tool{{Name: "choose", Schema: schema}}
		request.ToolChoiceMode = llm.ToolChoiceModeRequired
		request.StructuredOutput = &llm.StructuredOutput{Name: "choice", Schema: output}
		return request, nil
	}
	invoke, err := service.Admit()
	if err != nil {
		t.Fatal(err)
	}
	response, err := invoke(t.Context(), settings, prepare)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "choose" || len(response.OutputItems) != 1 {
		t.Fatalf("calls = %d, response = %+v", calls.Load(), response)
	}
}

func TestNanoOversizedInputRejected(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	settings, resolver := nanoTestConfig(t, server.URL+"/v1")
	service, err := NewService(5, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir())), resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := service.Admit()
	if err != nil {
		t.Fatal(err)
	}
	_, err = invoke(t.Context(), settings, func(target llm.NanoTarget) (llm.Request, error) {
		request, err := nanoTestRequest(target)
		request.SystemPrompt = string(make([]byte, 20000))
		return request, err
	})
	if !errors.Is(err, llm.ErrInvalidRequest) || calls.Load() != 0 {
		t.Fatalf("oversized input: %v, calls = %d", err, calls.Load())
	}
}

func TestNanoOversizedSchemasRejected(t *testing.T) {
	for _, tool := range []bool{true, false} {
		t.Run(fmt.Sprintf("tool=%t", tool), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer server.Close()
			settings, resolver := nanoTestConfig(t, server.URL+"/v1")
			service, err := NewService(5, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir())), resolver, nil)
			if err != nil {
				t.Fatal(err)
			}
			invoke, err := service.Admit()
			if err != nil {
				t.Fatal(err)
			}
			document, err := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string", "description": strings.Repeat("large schema content ", 3000)}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = invoke(t.Context(), settings, func(target llm.NanoTarget) (llm.Request, error) {
				request, err := nanoTestRequest(target)
				if err != nil {
					return request, err
				}
				if tool {
					schema, err := jsoncontract.NewPreparer(false).FunctionDocument("choose", document)
					request.Tools = []llm.Tool{{Name: "choose", Schema: schema}}
					return request, err
				}
				schema, err := jsoncontract.NewPreparer(false).StructuredDocument("choice", document)
				request.StructuredOutput = &llm.StructuredOutput{Name: "choice", Schema: schema}
				return request, err
			})
			if !errors.Is(err, llm.ErrInvalidRequest) || calls.Load() != 0 {
				t.Fatalf("oversized schema: %v, calls = %d", err, calls.Load())
			}
		})
	}
}

func TestNanoUnsupportedVisionRejected(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", completedResponse)
	}))
	defer server.Close()
	settings, resolver := nanoTestConfig(t, server.URL+"/v1")
	settings.Nano.ModelContextWindow = textutil.Value(20000)
	settings.Nano.SupportsVisionInputs = textutil.Value(false)
	service, err := NewService(5, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir())), resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := service.Admit()
	if err != nil {
		t.Fatal(err)
	}
	_, err = invoke(t.Context(), settings, func(target llm.NanoTarget) (llm.Request, error) {
		request, err := nanoTestRequest(target)
		request.Items[0].Raw = json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.invalid/image.png"}]}`)
		return request, err
	})
	if !errors.Is(err, llm.ErrInvalidRequest) || calls.Load() != 0 {
		t.Fatalf("unsupported vision = %v, calls = %d", err, calls.Load())
	}
}

func TestNanoAdmissionCapacityAndWaitingDeadline(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":%s}\n\n", completedResponse)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	settings, resolver := nanoTestConfig(t, server.URL+"/v1")
	service, err := NewService(2, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir())), resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	var admitted []Invocation
	for range 102 {
		invoke, err := service.Admit()
		if err != nil {
			t.Fatal(err)
		}
		admitted = append(admitted, invoke)
	}
	_, err = service.Admit()
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("capacity error = %v", err)
	}
	results := make(chan error, 3)
	for _, invoke := range admitted[:2] {
		go func() { _, err := invoke(t.Context(), settings, nanoTestRequest); results <- err }()
	}
	for range 2 {
		<-entered
	}
	// Queue one request with a one-second inference timeout behind held calls.
	waitSettings := settings
	n := *settings.Nano
	n.TimeoutSeconds = 1
	waitSettings.Nano = &n
	go func() { _, err := admitted[2](t.Context(), waitSettings, nanoTestRequest); results <- err }()
	timer := time.NewTimer(1100 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-results:
		t.Fatalf("inference finished while both slots were held: %v", err)
	case <-timer.C:
	}
	close(release)
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	var cleanup sync.WaitGroup
	for _, invoke := range admitted[3:] {
		cleanup.Go(func() { _, _ = invoke(canceled, settings, nanoTestRequest) })
	}
	cleanup.Wait()
	// Canceled waiting reservations must not permanently exhaust the backlog.
	invoke, err := service.Admit()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(t.Context(), settings, nanoTestRequest); err != nil {
		t.Fatal(err)
	}
}

func nanoTestRequest(target llm.NanoTarget) (llm.Request, error) {
	return llm.Request{Model: target.Settings.Model, MaxTokens: 100, ToolChoiceMode: llm.ToolChoiceModeAutomatic,
		Items: llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("Fix invoice totals")}})}, nil
}

func TestNanoAnonymousOneStep(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		id, err := uuid.Parse(r.Header.Get("session-id"))
		if err != nil || id.Version() != 4 {
			t.Errorf("dispatch identity = %q", r.Header.Get("session-id"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("anonymous connection sent credentials")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":%s}\n\n", completedResponse)
	}))
	defer server.Close()
	settings, resolver := nanoTestConfig(t, server.URL+"/v1")
	service, err := NewService(5, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir())), resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	invoke, err := service.Admit()
	if err != nil {
		t.Fatal(err)
	}
	response, err := invoke(context.Background(), settings, nanoTestRequest)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || response.Assistant.Content == nil || *response.Assistant.Content != "Fix invoice totals" {
		t.Fatalf("calls = %d, response = %+v", calls.Load(), response)
	}
}
