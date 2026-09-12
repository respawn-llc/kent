package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type heldBudgetGeneration struct {
	*fakeCompactionClient
	started chan struct{}
	release chan struct{}
}

func (c *heldBudgetGeneration) Generate(ctx context.Context, request llm.Request, callbacks llm.StreamCallbacks) (llm.Response, error) {
	response, err := c.fakeCompactionClient.Generate(ctx, request, callbacks)
	close(c.started)
	<-c.release
	return response, err
}

func TestSelectedPendingInputTriggersCompactionBeforeCommit(t *testing.T) {
	t.Run("synchronous", func(t *testing.T) { testSelectedPendingInputBudget(t, false) })
	t.Run("tool continuation", func(t *testing.T) { testSelectedPendingInputBudget(t, true) })
}

func TestPendingBudgetRebuildSelectsNewSteerAndExcludesQueue(t *testing.T) {
	store := mustCreateTestSession(t)
	configuration := Config{Model: "gpt-5", ContextWindowTokens: 200000, AutoCompactTokenLimit: 100000, CompactionMode: "native"}
	seed := mustNewTestEngine(t, store, &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("seeded"), Phase: textutil.Value(llm.MessagePhaseFinal)},
		Usage:     llm.Usage{InputTokens: 95000, WindowTokens: 200000},
	}}}, tools.NewRegistry(), configuration)
	if _, err := seed.SubmitUserMessage(t.Context(), "seed"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkLockedPromptFacingSnapshotsStale(); err != nil {
		t.Fatal(err)
	}
	preparation := &heldPromptPreparation{started: make(chan struct{}), release: make(chan struct{})}
	configuration.PromptFacingSnapshotReloader = preparation
	client := &heldBudgetGeneration{
		fakeCompactionClient: &fakeCompactionClient{
			responses: []llm.Response{finalOutputItemResponse("done")},
			compactionResponses: []llm.CompactionResponse{{
				Checkpoint: llm.ResponseItem{Type: llm.ResponseItemTypeCompaction, EncryptedContent: textutil.Value("checkpoint")},
				Usage:      llm.Usage{InputTokens: 1000, OutputTokens: 10, WindowTokens: 200000},
			}},
		},
		started: make(chan struct{}), release: make(chan struct{}),
	}
	engine := mustNewTestEngine(t, mustOpenTestSession(t, store.Dir()), client, tools.NewRegistry(), configuration)
	done := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "selected")
		done <- err
	}()
	pendingWorkTestWait(t, preparation.started, "preparation")
	unselected := strings.Repeat("unselected ", 100000)
	queued, err := engine.QueueUserInput(t.Context(), plainQueuedUserInput(unselected))
	if err != nil {
		t.Fatal(err)
	}
	pending := strings.Repeat("pending ", 6000)
	if _, err := engine.Steer(t.Context(), pending, nil); err != nil {
		t.Fatal(err)
	}
	close(preparation.release)
	pendingWorkTestWait(t, client.started, "generation after rebuilt budget")
	if len(client.compactionCalls) != 1 {
		t.Fatalf("rebuilt selected budget caused %d compactions", len(client.compactionCalls))
	}
	for _, request := range []llm.Request{client.compactionCalls[0], client.calls[0]} {
		for _, item := range request.Items {
			if item.Content != nil && *item.Content == unselected {
				t.Error("unselected Queue input entered preparation")
			}
		}
	}
	if _, err := engine.RemovePendingWork(t.Context(), mustQueueItemID(queued.ID)); err != nil {
		t.Fatal(err)
	}
	close(client.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func testSelectedPendingInputBudget(t *testing.T, toolContinuation bool) {
	client := &fakeCompactionClient{
		responses: []llm.Response{
			{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("seeded"), Phase: textutil.Value(llm.MessagePhaseFinal)}, Usage: llm.Usage{InputTokens: 95000, WindowTokens: 200000}},
			finalOutputItemResponse("done"),
		},
		compactionResponses: []llm.CompactionResponse{{
			Checkpoint: llm.ResponseItem{Type: llm.ResponseItemTypeCompaction, EncryptedContent: textutil.Value("checkpoint")},
			Usage:      llm.Usage{InputTokens: 1000, OutputTokens: 10, WindowTokens: 200000},
		}},
	}
	registry := tools.NewRegistry()
	var enabled []toolspec.ID
	tool := blockingTool{name: toolspec.ToolExecCommand, started: make(chan struct{}), release: make(chan struct{})}
	if toolContinuation {
		enabled = []toolspec.ID{toolspec.ToolExecCommand}
		registry = newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: tool})
		client.responses[0].Assistant.Phase = textutil.Value(llm.MessagePhaseCommentary)
		client.responses[0].ToolCalls = []llm.ToolCall{{ID: "budget-tool", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"pwd"}`)}}
	}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, registry, Config{
		Model: "gpt-5", EnabledTools: enabled, ContextWindowTokens: 200000, AutoCompactTokenLimit: 100000, CompactionMode: "native",
	})
	pending := strings.Repeat("pending ", 6000)
	if toolContinuation {
		done := make(chan error, 1)
		go func() {
			_, err := engine.SubmitUserMessage(t.Context(), strings.Repeat("seed ", 2000))
			done <- err
		}()
		pendingWorkTestWait(t, tool.started, "tool call")
		if _, err := engine.Steer(t.Context(), pending, nil); err != nil {
			t.Fatal(err)
		}
		close(tool.release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := engine.SubmitUserMessage(t.Context(), strings.Repeat("seed ", 2000)); err != nil {
			t.Fatal(err)
		}
		if _, err := engine.SubmitUserMessage(t.Context(), pending); err != nil {
			t.Fatal(err)
		}
	}
	if len(client.compactionCalls) != 1 || len(client.calls) != 2 {
		t.Fatalf("compactions=%d generations=%d", len(client.compactionCalls), len(client.calls))
	}
	for _, item := range client.compactionCalls[0].Items {
		if item.Content != nil && *item.Content == pending {
			t.Fatal("pending input was compacted")
		}
	}
	count := 0
	for _, item := range client.calls[1].Items {
		if item.Content != nil && *item.Content == pending {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("selected input occurrences = %d", count)
	}
}
