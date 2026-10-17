package runtimewire

import (
	"context"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/launch"
	"core/server/llm"
	"core/shared/config"
	"core/shared/modelcontract"
	"core/shared/textutil"
)

type consumptionTransport struct {
	cancelRequest context.CancelFunc
}

func (transport consumptionTransport) Generate(context.Context, llm.ResponsesRequest, llm.StreamCallbacks) (llm.ResponsesResponse, error) {
	transport.cancelRequest()
	return llm.ResponsesResponse{Usage: llm.Usage{InputTokens: textutil.Value(100), CachedInputTokens: textutil.Value(80), OutputTokens: textutil.Value(3)},
		ServedModel: textutil.Value("gpt-5.6-luna"), ProviderPhase: llm.AbsentProviderPhase()}, nil
}

func (consumptionTransport) Compact(context.Context, llm.ResponsesRequest) (llm.ResponsesCompactionResponse, error) {
	return llm.ResponsesCompactionResponse{
		Usage:            llm.Usage{InputTokens: textutil.Value(20), OutputTokens: textutil.Value(1)},
		ProviderEvidence: modelcontract.ProviderUsageEvidence{ServedModel: textutil.Value("gpt-5.6-luna")},
	}, nil
}

func TestConsumptionFollowsServingConnection(t *testing.T) {
	root := t.TempDir()
	store := testsetup.OpenStore(t, root)
	rotation := launch.NewConnectionRotation(store, 0)
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
	requestContext, cancelRequest := context.WithCancel(t.Context())
	defer cancelRequest()
	clients := make(map[config.ConnectionID]llm.Client)
	factory := RuntimeClientFactoryFunc(func(_ context.Context, request RuntimeClientRequest) (llm.Client, error) {
		client := llm.NewResponsesClient(consumptionTransport{cancelRequest: cancelRequest}, request.UsageObserver)
		clients[request.Connection.ID] = client
		return client, nil
	})
	wiring, err := newTestRuntimeWiringWithBackground(t, sessionStore, materializedRuntimeWireEventLog(t, sessionStore),
		settings, nil, nil, nil, nil,
		requiredRuntimeWireTestOptions(RuntimeWiringOptions{
			Context:           t.Context(),
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
	if _, err := main.Generate(requestContext, request, llm.StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	assertConsumption := func(wantMain, wantReviewer int64) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			totals, err := store.ConnectionConsumption(t.Context(), []config.ConnectionID{"main", "reviewer"}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if totals["main"] == wantMain && totals["reviewer"] == wantReviewer {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("totals = %v, want main %d reviewer %d", totals, wantMain, wantReviewer)
			}
			time.Sleep(time.Millisecond)
		}
	}
	assertConsumption(9_200, 0)
	if _, err := reviewer.Generate(requestContext, request, llm.StreamCallbacks{}); err != nil {
		t.Fatal(err)
	}
	assertConsumption(9_200, 9_200)
	compactor, ok := reviewer.(llm.CompactionClient)
	if !ok {
		t.Fatal("reviewer must support compaction")
	}
	if _, err := compactor.Compact(requestContext, request); err != nil {
		t.Fatal(err)
	}
	assertConsumption(9_200, 14_400)
}
