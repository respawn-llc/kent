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
	"core/internal/testharness/testsetup"
	"core/server/launch"
	"core/server/llm"
	"core/server/runtime"
	"core/server/runtimeview"
	"core/server/session"
	"core/shared/config"
	"core/shared/llmerrors"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcript"

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
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute), exhausted, healthy.URL(), config.CompactionModeLocal)
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
	page, err := runtime.TranscriptNewestSegmentPageFromEventLog(materializedRuntimeWireEventLog(t, reopened), config.CacheWarningModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeview.TranscriptTailSegmentFromSegment(page, nil); err != nil {
		t.Fatalf("project reopened fallback through generated validation: %v", err)
	}
	var notices []config.ConnectionReplacement
	for _, entry := range page.Snapshot.Entries {
		if entry.ConnectionFallback != nil {
			notices = append(notices, *entry.ConnectionFallback)
		}
	}
	if len(notices) != 1 || notices[0].Previous != "a" || notices[0].Current != "b" {
		t.Fatalf("reopened fallback notices = %+v", notices)
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

func TestUsageFallbackInheritedReviewerContinues(t *testing.T) {
	testUsageFallbackReviewer(t, false, false, false)
}

func TestUsageFallbackExplicitReviewerStaysIsolated(t *testing.T) {
	testUsageFallbackReviewer(t, true, false, false)
}

func TestUsageFallbackInheritedReviewerUnavailable(t *testing.T) {
	testUsageFallbackReviewer(t, false, true, false)
}

func TestUsageFallbackReviewerPreparationFailureKeepsBinding(t *testing.T) {
	testUsageFallbackReviewer(t, false, false, true)
}

func testUsageFallbackReviewer(t *testing.T, explicit, unavailable, preparationFailure bool) {
	healthy, err := blackbox.StartResponsesStub([]blackbox.RequiredOperation{
		{ID: uuid.New(), Route: blackbox.RouteResponses, Outcome: blackbox.OutcomeStream, Output: textutil.Value("done"), ResponsePhase: blackbox.NewResponsePhase(blackbox.ResponsePhaseFinal)},
		{ID: uuid.New(), Route: blackbox.RouteResponses, Outcome: blackbox.OutcomeStream, Output: textutil.Value(`{"suggestions":[]}`), ResponsePhase: blackbox.NewResponsePhase(blackbox.ResponsePhaseFinal)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = healthy.Stop() })
	target, err := url.Parse(healthy.URL())
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	var calls atomic.Int32
	fault := usageExhaustionFixture(t)
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if calls.Add(1) == 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write(fault)
			return
		}
		proxy.ServeHTTP(w, req)
	}))
	t.Cleanup(a.Close)
	reviewer := config.ReviewerSettings{Frequency: "all", Model: "operator-alias"}
	if explicit {
		reviewer.Connection = config.SingleConnection("a")
	}
	rotation := launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute)
	b := healthy.URL()
	if unavailable {
		b, _ = usageFailureEndpoint(t, 429, fault)
	}
	var factory RuntimeClientFactory
	if preparationFailure {
		factory = RuntimeClientFactoryFunc(func(ctx context.Context, req RuntimeClientRequest) (llm.Client, error) {
			if req.Connection.ID == "b" && req.Purpose == RuntimeClientPurposeReviewer {
				return nil, errors.New("fixture Reviewer resource preparation failure")
			}
			return NewRuntimeClient(ctx, nil, req)
		})
	}
	wiring, store := newUsageFallbackRuntimeWithReviewer(t, rotation, a.URL, b, config.CompactionModeLocal, reviewer, factory)
	if _, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello"); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for wiring.Engine.ReviewerActive() {
		select {
		case <-deadline.C:
			t.Fatal("Reviewer did not complete")
		case <-tick.C:
		}
	}
	wantID, wantRequests := config.ConnectionID("b"), 2
	if explicit || preparationFailure {
		wantID, wantRequests = "a", 1
	}
	if unavailable {
		wantRequests = 1
	}
	if got := store.Meta().ConnectionID; got == nil || *got != wantID {
		_ = wiring.Engine.WithTranscriptHydrationSnapshot(func(value runtime.TranscriptHydrationSnapshot) error {
			t.Logf("transcript %+v", value)
			return nil
		})
		t.Fatalf("Reviewer replacement = %v, calls %d", got, calls.Load())
	}
	if !rotation.CoolingDown("a") || calls.Load() != 2 || len(healthy.Snapshot().Observed) != wantRequests {
		t.Fatalf("requests = a %d, healthy %+v", calls.Load(), healthy.Snapshot().Observed)
	}
	if explicit || unavailable || preparationFailure {
		reopened, err := session.Open(store.Dir(), runtimeWireTestSessionPersistence.Options()...)
		if err != nil {
			t.Fatal(err)
		}
		page, err := runtime.TranscriptNewestSegmentPageFromEventLog(materializedRuntimeWireEventLog(t, reopened), config.CacheWarningModeDefault)
		if err != nil {
			t.Fatal(err)
		}
		var errorsFound int
		for _, entry := range page.Snapshot.Entries {
			if entry.ReviewerError != nil {
				errorsFound++
				if entry.ReviewerError.ID.String() == "" || entry.ReviewerError.Detail == "" {
					t.Fatalf("Reviewer error lacks identity or diagnostics: %+v", entry.ReviewerError)
				}
			}
			if entry.ConnectionsUnavailable != nil {
				t.Fatal("Reviewer failure projected an extra generic unavailable row")
			}
		}
		if errorsFound != 1 {
			t.Fatalf("durable Reviewer errors = %d, want 1", errorsFound)
		}
	}
}

func newUsageFallbackRuntime(t *testing.T, rotation *launch.ConnectionRotation, a, b string, mode config.CompactionMode, enabled ...toolspec.ID) (*RuntimeWiring, *session.Store) {
	return newUsageFallbackRuntimeWithReviewer(t, rotation, a, b, mode, config.ReviewerSettings{Frequency: "off"}, nil, enabled...)
}

func newUsageFallbackRuntimeWithReviewer(t *testing.T, rotation *launch.ConnectionRotation, a, b string, mode config.CompactionMode, reviewer config.ReviewerSettings, factory RuntimeClientFactory, enabled ...toolspec.ID) (*RuntimeWiring, *session.Store) {
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
		Reviewer:       reviewer,
		Timeouts:       config.Timeouts{ModelRequestSeconds: 5},
		Shell:          config.ShellSettings{PostprocessingMode: config.ShellPostprocessingModeBuiltin},
	}
	options := requiredRuntimeWireTestOptions(RuntimeWiringOptions{
		ClientFactory:      factory,
		FilesystemContext:  runtimeWireFilesystemContext(t, root),
		ConnectionRotation: rotation,
		ConnectionScope:    config.ConnectionScope{Selection: &scope},
	})
	if reviewer.Connection != nil {
		options.Sources = map[string]config.Origin{
			"reviewer.connection": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "reviewer.connection"}},
		}
	}
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
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Nanosecond), a, b, config.CompactionModeLocal)
	_, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello")
	var unavailable *llmerrors.ConnectionsUnavailableError
	if !errors.As(err, &unavailable) || len(unavailable.Members) != 2 || len(unavailable.Causes) != 2 {
		t.Fatalf("unavailable error = %v", err)
	}
	if aCalls.Load() != 1 || bCalls.Load() != 1 {
		t.Fatalf("attempts = a %d, b %d", aCalls.Load(), bCalls.Load())
	}
	var facts *transcript.ConnectionsUnavailableNotice
	livePage, err := wiring.Engine.TranscriptNewestSegmentPage()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range runtime.TranscriptCommittedRowFactsFromSnapshot(livePage.Snapshot) {
		if row.Notice != nil && row.Notice.ConnectionsUnavailable != nil {
			facts = row.Notice.ConnectionsUnavailable
		}
	}
	if facts == nil || len(facts.Members) != 2 || len(facts.Diagnostics) != 2 {
		t.Fatalf("unavailable notice facts = %+v", facts)
	}
	reopened, err := session.Open(store.Dir(), runtimeWireTestSessionPersistence.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	page, err := runtime.TranscriptNewestSegmentPageFromEventLog(materializedRuntimeWireEventLog(t, reopened), config.CacheWarningModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeview.TranscriptTailSegmentFromSegment(page, nil); err != nil {
		t.Fatalf("project reopened unavailable through generated validation: %v", err)
	}
}

func TestUsageFallbackCooldownExpiryKeepsCurrentConnection(t *testing.T) {
	store := newRuntimeWireSession(t, t.TempDir(), "cooldown-expiry")
	settings := config.Settings{
		Connection: config.SingleConnection("a"),
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"a": {Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://127.0.0.1:1")},
		},
	}
	rotation := launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Microsecond)
	resolve := connectionMaterializer(store, settings, RuntimeWiringOptions{
		ConnectionRotation: rotation,
		ConnectionScope:    config.ConnectionScope{Selection: settings.Connection},
	}, func(config.Settings, RuntimeClientPurpose, RuntimeClientFactory, llm.ProviderCapabilities) (llm.Client, error) {
		return nil, errors.New("unchanged connection must not reconstruct its client")
	})
	// Exercise eligibility changing during selection without asserting scheduler
	// timing: either still unavailable or usable with its existing resources.
	for range 10000 {
		rotation.RecordExhaustion("a")
		replacement, err := resolve(t.Context(), runtime.ConnectionSelectionRequest{
			Current: "a", Reason: runtime.ConnectionBeforeDispatch,
		})
		var unavailable *llmerrors.ConnectionsUnavailableError
		if errors.As(err, &unavailable) {
			continue
		}
		if err != nil || replacement != nil {
			t.Fatalf("unchanged selection = %+v, %v", replacement, err)
		}
	}
}

func TestUsageFallbackZeroRetainsOriginalFailure(t *testing.T) {
	fault := usageExhaustionFixture(t)
	a, aCalls := usageFailureEndpoint(t, 429, fault)
	b, bCalls := usageFailureEndpoint(t, 401, []byte(`{"error":{"code":"invalid_api_key","message":"fixture"}}`))
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0), a, b, config.CompactionModeLocal)
	_, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello")
	if !llm.IsUsageExhaustionError(err) || aCalls.Load() != 1 || bCalls.Load() != 0 || *store.Meta().ConnectionID != "a" {
		t.Fatalf("disabled fallback = %v; a %d, b %d", err, aCalls.Load(), bCalls.Load())
	}
}

func TestUsageFallbackSavesReplacementBeforeOrdinaryFailure(t *testing.T) {
	fault := usageExhaustionFixture(t)
	a, aCalls := usageFailureEndpoint(t, 429, fault)
	b, bCalls := usageFailureEndpoint(t, 401, []byte(`{"error":{"code":"invalid_api_key","message":"fixture"}}`))
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute), a, b, config.CompactionModeLocal)
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
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute), a, b.URL(), config.CompactionModeLocal, toolspec.ToolExecCommand)
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
	rotation := launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute)
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
	wiring, store := newUsageFallbackRuntime(t, launch.NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute), a, b.URL(), mode)
	if _, err := wiring.Engine.SubmitUserMessage(t.Context(), "hello"); err != nil {
		t.Fatal(err)
	}
	if err := wiring.Engine.CompactContextForWorkflowContinuation(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	var snapshot blackbox.StubSnapshot
	for {
		snapshot = b.Snapshot()
		if snapshot.Failure != nil {
			t.Fatalf("compaction request failed: %v", snapshot.Failure)
		}
		if snapshot.RequiredConsumed() {
			break
		}
		select {
		case <-b.Events():
		case <-deadline.C:
			t.Fatalf("timed out waiting for compaction request completion: required %d/%d, active %d", snapshot.RequiredIndex, snapshot.RequiredTotal, snapshot.ActiveRequests)
		}
	}
	connectionID := store.Meta().ConnectionID
	if connectionID == nil || *connectionID != "b" || attempts.Load() != 2 || !snapshot.RequiredConsumed() {
		t.Fatalf("compaction binding/requests = %v, %d, required %d/%d, active %d", connectionID, attempts.Load(), snapshot.RequiredIndex, snapshot.RequiredTotal, snapshot.ActiveRequests)
	}
}
