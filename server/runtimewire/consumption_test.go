package runtimewire

import (
	"context"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/launch"
	"core/server/llm"
	"core/shared/config"
	"core/shared/textutil"
)

type consumptionTransport struct{}

func (consumptionTransport) Generate(context.Context, llm.ResponsesRequest, llm.StreamCallbacks) (llm.ResponsesResponse, error) {
	return llm.ResponsesResponse{Usage: llm.Usage{InputTokens: textutil.Value(100), CachedInputTokens: textutil.Value(80), OutputTokens: textutil.Value(3)},
		ProviderPhase: llm.AbsentProviderPhase()}, nil
}

func (consumptionTransport) Compact(context.Context, llm.ResponsesRequest) (llm.ResponsesCompactionResponse, error) {
	return llm.ResponsesCompactionResponse{Usage: llm.Usage{InputTokens: textutil.Value(20), OutputTokens: textutil.Value(1)}}, nil
}

func TestConsumptionFollowsServingConnection(t *testing.T) {
	root := t.TempDir()
	store := testsetup.OpenStore(t, root)
	rotation := launch.NewConnectionRotation(store)
	sessionStore := newRuntimeWireSession(t, root, "consumption")
	settings := config.Settings{
		Model: "unpriced-local-model", Connection: config.SingleConnection("main"),
		ConnectionOrder: []config.ConnectionID{"main", "reviewer"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"main":     {Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://127.0.0.1:1/v1")},
			"reviewer": {Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://127.0.0.1:2/v1")},
		},
		Reviewer: config.ReviewerSettings{Frequency: "all", Connection: config.SingleConnection("reviewer"), Model: "unpriced-local-model"},
		Timeouts: config.Timeouts{ModelRequestSeconds: 1},
		Shell:    config.ShellSettings{PostprocessingMode: config.ShellPostprocessingModeBuiltin},
	}
	clients := make(map[config.ConnectionID]llm.Client)
	factory := RuntimeClientFactoryFunc(func(_ context.Context, request RuntimeClientRequest) (llm.Client, error) {
		client := llm.NewResponsesClient(consumptionTransport{}, request.UsageObserver)
		clients[request.Connection.ID] = client
		return client, nil
	})
	wiring, err := newTestRuntimeWiringWithBackground(t, sessionStore, materializedRuntimeWireEventLog(t, sessionStore),
		settings, nil, nil, nil, nil,
		requiredRuntimeWireTestOptions(RuntimeWiringOptions{
			FilesystemContext: runtimeWireFilesystemContext(t, root), ClientFactory: factory,
			RecordUsage: rotation.RecordUsage,
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wiring.Close() })
	request := llm.Request{Model: settings.Model, ToolChoiceMode: llm.ToolChoiceModeAutomatic,
		Items: llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("hello")}})}
	main := clients["main"]
	reviewer := clients["reviewer"]
	if main == nil || reviewer == nil {
		t.Fatalf("resolved clients = %v", clients)
	}
	if _, err := main.Generate(t.Context(), request, llm.StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	assertConsumption := func(wantMain, wantReviewer int64) {
		t.Helper()
		totals, err := store.ConnectionConsumption(t.Context(), []config.ConnectionID{"main", "reviewer"}, time.Now())
		if err != nil || totals["main"] != wantMain || totals["reviewer"] != wantReviewer {
			t.Fatalf("totals = %v, want main %d reviewer %d, error %v", totals, wantMain, wantReviewer, err)
		}
	}
	assertConsumption(58_000, 0)
	if _, err := reviewer.Generate(t.Context(), request, llm.StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	assertConsumption(58_000, 58_000)
	compactor, ok := reviewer.(llm.CompactionClient)
	if !ok {
		t.Fatal("reviewer must support compaction")
	}
	if _, err := compactor.Compact(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	assertConsumption(58_000, 88_000)
}
