package runtimewire

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"core/internal/testharness/httpclient"
	"core/internal/testharness/pty/blackbox"
	"core/internal/testharness/scriptedllm"
	"core/server/launch"
	"core/server/llm"
	"core/server/session"
	"core/shared/config"
	"core/shared/llmerrors"
	"core/shared/textutil"
	"core/shared/toolspec"

	"github.com/google/uuid"
)

func TestUsageFallbackContinuesAndSavesReplacement(t *testing.T) {
	exhausted, exhaustedCalls := usageFailureEndpoint(t, 429, usageExhaustionFixture(t))
	healthy, err := blackbox.StartResponsesStub([]blackbox.RequiredOperation{{
		ID: uuid.New(), Route: blackbox.RouteResponses, Outcome: blackbox.OutcomeStream,
		ResponsePhase: blackbox.NewResponsePhase(blackbox.ResponsePhaseFinal),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = healthy.Stop() })
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(time.Minute), exhausted, healthy.URL(), config.CompactionModeLocal)
	if _, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello"); err != nil {
		t.Fatal(err)
	}
	reopened, err := session.Open(store.Dir(), runtimeWireTestSessionPersistence.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Meta().ConnectionID; got == nil || *got != "b" {
		t.Fatalf("saved connection = %v", got)
	}
	if exhaustedCalls.Load() != 1 || len(healthy.Snapshot().Observed) != 1 {
		t.Fatalf("requests = exhausted %d, healthy %+v", exhaustedCalls.Load(), healthy.Snapshot().Observed)
	}
	var request struct {
		Text struct {
			Verbosity string `json:"verbosity"`
		} `json:"text"`
	}
	if err := json.Unmarshal(healthy.Snapshot().Observed[0].Body, &request); err != nil || request.Text.Verbosity != "high" {
		t.Fatalf("retained request features = %+v, %v", request, err)
	}
	if got := reopened.Meta().Locked.ProviderContract.ProviderID; got != "established-provider" {
		t.Fatalf("established provider contract changed to %s", got)
	}
}

func newUsageFallbackRuntime(t *testing.T, rotation *launch.ConnectionRotation, a, b string, mode config.CompactionMode, enabled ...toolspec.ID) (*RuntimeWiring, *session.Store) {
	t.Helper()
	root := t.TempDir()
	store := newRuntimeWireSession(t, root, "usage-fallback")
	if err := store.SetConnectionID("a"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkModelDispatchLocked(session.LockedContract{
		Model: "operator-alias", HasEnabledTools: true, EnabledTools: toolspec.IDStrings(enabled), WebSearchMode: "native",
		ProviderContract: session.LockedProviderCapabilities{
			ProviderID: "established-provider", SupportsResponsesAPI: true, SupportsProviderVerbosity: textutil.Value(true),
			SupportsResponsesCompact: mode == config.CompactionModeNative,
		},
	}); err != nil {
		t.Fatal(err)
	}
	scope := config.ConnectionSelection{"a", "b"}
	settings := config.Settings{
		Connection:      config.SingleConnection("a"),
		ConnectionOrder: []config.ConnectionID{"a", "b"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"a": {Protocol: config.ConnectionResponses, Endpoint: textutil.Value(a)},
			"b": {Protocol: config.ConnectionResponses, Endpoint: textutil.Value(b)},
		},
		Model: "operator-alias", ModelContextWindow: 200000,
		ModelVerbosity: config.ModelVerbosityHigh,
		CompactionMode: mode,
		Reviewer:       config.ReviewerSettings{Frequency: "off"},
		Timeouts:       config.Timeouts{ModelRequestSeconds: 5},
		Shell:          config.ShellSettings{PostprocessingMode: config.ShellPostprocessingModeBuiltin},
	}
	options := requiredRuntimeWireTestOptions(RuntimeWiringOptions{
		FilesystemContext:  runtimeWireFilesystemContext(t, root),
		ConnectionRotation: rotation,
		ConnectionScope:    config.ConnectionScope{Selection: &scope},
	})
	if mode == config.CompactionModeNative {
		t.Setenv("KENT_FALLBACK_TEST_KEY", "fixture-key")
		for id := range settings.Connections {
			settings.Connections[id] = config.ProviderConnection{
				Protocol: config.ConnectionResponses, Endpoint: textutil.Value(config.DefaultOpenAIResponsesEndpoint),
				EnvironmentVariable: textutil.Value("KENT_FALLBACK_TEST_KEY"),
			}
		}
		targets := map[config.ConnectionID]string{"a": a, "b": b}
		options.ClientFactory = RuntimeClientFactoryFunc(func(_ context.Context, request RuntimeClientRequest) (llm.Client, error) {
			registration, err := llm.ResolveConnectionVariant(request.Connection.Definition)
			if err != nil {
				return nil, err
			}
			target, err := url.Parse(targets[request.Connection.ID])
			if err != nil {
				return nil, err
			}
			return llm.NewProviderClient(llm.ProviderClientOptions{
				Registration: registration, Auth: request.Connection.Auth, ConnectionID: &request.Connection.ID,
				HTTPClient:          &http.Client{Transport: httpclient.NewURLRewriteTransport(target, http.DefaultTransport, "/v1")},
				ContextWindowTokens: request.ActiveSettings.ModelContextWindow,
				ModelVerbosity:      string(request.ActiveSettings.ModelVerbosity),
				RequestCapabilities: &request.RequestCapabilities,
			})
		})
	}
	wiring, err := newTestRuntimeWiringWithBackground(t, store, materializedRuntimeWireEventLog(t, store), settings, enabled, nil, nil, nil, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wiring.Close() })
	return wiring, store
}

func usageFailureEndpoint(t *testing.T, status int, body []byte) (string, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server.URL, calls
}

func usageExhaustionFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("../llm/testdata/usage-exhaustion/openai-quota.json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestUsageFallbackUnavailableIsBoundedAfterCooldownExpires(t *testing.T) {
	fault := usageExhaustionFixture(t)
	a, aCalls := usageFailureEndpoint(t, 429, fault)
	b, bCalls := usageFailureEndpoint(t, 429, fault)
	wiring, _ := newUsageFallbackRuntime(t, launch.NewConnectionRotation(time.Nanosecond), a, b, config.CompactionModeLocal)
	_, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello")
	var unavailable *llmerrors.ConnectionsUnavailableError
	if !errors.As(err, &unavailable) || len(unavailable.Members) != 2 || len(unavailable.Causes) != 2 {
		t.Fatalf("unavailable error = %v", err)
	}
	if aCalls.Load() != 1 || bCalls.Load() != 1 {
		t.Fatalf("attempts = a %d, b %d", aCalls.Load(), bCalls.Load())
	}
}

func TestUsageFallbackZeroRetainsOriginalFailure(t *testing.T) {
	fault := usageExhaustionFixture(t)
	a, aCalls := usageFailureEndpoint(t, 429, fault)
	b, bCalls := usageFailureEndpoint(t, 401, []byte(`{"error":{"code":"invalid_api_key","message":"fixture"}}`))
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(0), a, b, config.CompactionModeLocal)
	_, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello")
	if !llm.IsUsageExhaustionError(err) || aCalls.Load() != 1 || bCalls.Load() != 0 || *store.Meta().ConnectionID != "a" {
		t.Fatalf("disabled fallback = %v; a %d, b %d", err, aCalls.Load(), bCalls.Load())
	}
}

func TestUsageFallbackSavesReplacementBeforeOrdinaryFailure(t *testing.T) {
	fault := usageExhaustionFixture(t)
	a, aCalls := usageFailureEndpoint(t, 429, fault)
	b, bCalls := usageFailureEndpoint(t, 401, []byte(`{"error":{"code":"invalid_api_key","message":"fixture"}}`))
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(time.Minute), a, b, config.CompactionModeLocal)
	_, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello")
	if !llm.IsAuthenticationError(err) || aCalls.Load() != 1 || bCalls.Load() != 1 {
		t.Fatalf("ordinary failure = %v; a %d, b %d", err, aCalls.Load(), bCalls.Load())
	}
	reopened, err := session.Open(store.Dir(), runtimeWireTestSessionPersistence.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	if id := reopened.Meta().ConnectionID; id == nil || *id != "b" {
		t.Fatalf("saved replacement = %v", id)
	}
}

func TestUsageFallbackKeepsCompletedToolWork(t *testing.T) {
	tool, err := blackbox.StartScriptedResponsesStub(scriptedllm.Script{Steps: []scriptedllm.Step{
		scriptedllm.ToolBatch("", llm.ToolCall{ID: uuid.NewString(), Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"printf x >> fallback-marker"}`)}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tool.Stop() })
	a, attempts := usageExhaustionAfterFirst(t, tool.URL())
	b, err := blackbox.StartResponsesStub([]blackbox.RequiredOperation{{
		ID: uuid.New(), Route: blackbox.RouteResponses, Outcome: blackbox.OutcomeStream,
		ResponsePhase: blackbox.NewResponsePhase(blackbox.ResponsePhaseFinal),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Stop() })
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(time.Minute), a, b.URL(), config.CompactionModeLocal, toolspec.ToolExecCommand)
	if _, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello"); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(store.Meta().WorkspaceRoot, "fallback-marker"))
	if err != nil || len(marker) != 1 {
		t.Fatalf("completed tool executions = %q, %v", marker, err)
	}
	if attempts.Load() != 2 || len(b.Snapshot().Observed) != 1 {
		t.Fatalf("provider requests = a %d, b %d", attempts.Load(), len(b.Snapshot().Observed))
	}
}

func TestUsageFallbackSkipsSharedCooldownInBoundSession(t *testing.T) {
	a, aCalls := usageFailureEndpoint(t, 429, usageExhaustionFixture(t))
	operations := make([]blackbox.RequiredOperation, 2)
	for i := range operations {
		operations[i] = blackbox.RequiredOperation{
			ID: uuid.New(), Route: blackbox.RouteResponses, Outcome: blackbox.OutcomeStream,
			ResponsePhase: blackbox.NewResponsePhase(blackbox.ResponsePhaseFinal),
		}
	}
	b, err := blackbox.StartResponsesStub(operations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Stop() })
	rotation := launch.NewConnectionRotation(time.Minute)
	first, _ := newUsageFallbackRuntime(t, rotation, a, b.URL(), config.CompactionModeLocal)
	second, secondStore := newUsageFallbackRuntime(t, rotation, a, b.URL(), config.CompactionModeLocal)
	for _, wiring := range []*RuntimeWiring{first, second} {
		if _, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello"); err != nil {
			t.Fatal(err)
		}
	}
	if aCalls.Load() != 1 || len(b.Snapshot().Observed) != 2 || *secondStore.Meta().ConnectionID != "b" {
		t.Fatalf("shared cooldown: a %d, b %d, binding %v", aCalls.Load(), len(b.Snapshot().Observed), secondStore.Meta().ConnectionID)
	}
}

func usageExhaustionAfterFirst(t *testing.T, upstream string) (string, *atomic.Int32) {
	t.Helper()
	target, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	fault := usageExhaustionFixture(t)
	attempts := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			proxy.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		_, _ = w.Write(fault)
	}))
	t.Cleanup(server.Close)
	return server.URL, attempts
}

func TestUsageFallbackContinuesLocalCompaction(t *testing.T) {
	testUsageFallbackCompaction(t, config.CompactionModeLocal)
}

func TestUsageFallbackContinuesNativeCompaction(t *testing.T) {
	testUsageFallbackCompaction(t, config.CompactionModeNative)
}

func testUsageFallbackCompaction(t *testing.T, mode config.CompactionMode) {
	t.Helper()
	seed, err := blackbox.StartResponsesStub([]blackbox.RequiredOperation{{
		ID: uuid.New(), Route: blackbox.RouteResponses, Outcome: blackbox.OutcomeStream,
		ResponsePhase: blackbox.NewResponsePhase(blackbox.ResponsePhaseFinal),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = seed.Stop() })
	a, attempts := usageExhaustionAfterFirst(t, seed.URL())
	operation := blackbox.RequiredOperation{ID: uuid.New(), Route: blackbox.RouteCompact, Outcome: blackbox.OutcomeJSON}
	if mode == config.CompactionModeLocal {
		operation.Route, operation.Outcome = blackbox.RouteResponses, blackbox.OutcomeStream
		operation.Output = textutil.Value("fixture summary")
		operation.ResponsePhase = blackbox.NewResponsePhase(blackbox.ResponsePhaseFinal)
	}
	b, err := blackbox.StartResponsesStub([]blackbox.RequiredOperation{operation})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Stop() })
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(time.Minute), a, b.URL(), mode)
	if _, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello"); err != nil {
		t.Fatal(err)
	}
	if err := wiring.Engine.CompactContextForWorkflowContinuation(t.Context()); err != nil {
		t.Fatal(err)
	}
	if *store.Meta().ConnectionID != "b" || attempts.Load() != 2 || !b.Snapshot().RequiredConsumed() {
		t.Fatalf("compaction binding/requests = %v, %d, %+v", store.Meta().ConnectionID, attempts.Load(), b.Snapshot())
	}
}
