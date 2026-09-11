package runtime

import (
	"bytes"
	"encoding/json"
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/server/workflow"
)

func TestThinkingChangesPreserveDispatchedPrefix(t *testing.T) {
	testThinkingChangesPreserveDispatchedPrefix(t, func(engine *Engine, effort string) error {
		return engine.SetThinkingLevel(t.Context(), effort)
	})
}

func TestWorkflowNativeThinkingChangesPreserveDispatchedPrefix(t *testing.T) {
	testThinkingChangesPreserveDispatchedPrefix(t, func(engine *Engine, effort string) error {
		value, err := workflow.NewThinkingValue(effort)
		if err != nil {
			return err
		}
		return engine.SetWorkflowThinkingValue(value)
	})
}

func testThinkingChangesPreserveDispatchedPrefix(t *testing.T, selectThinking func(*Engine, string) error) {
	t.Helper()
	for _, queued := range []bool{false, true} {
		name := "synchronous"
		if queued {
			name = "queued"
		}
		t.Run(name, func(t *testing.T) {
			client := &fakeClient{
				caps: llm.ProviderCapabilities{
					ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true, SupportsNativeThinkingUpdates: true,
				},
				responses: []llm.Response{finalOutputItemResponse("one"), finalOutputItemResponse("two"), finalOutputItemResponse("three")},
			}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
				Model: "gpt-6-astra", ThinkingLevel: "xhigh",
				SupportedThinkingValues: []string{"low", "medium", "high", "xhigh"},
			})
			submit := func(text string) {
				t.Helper()
				if queued {
					if _, err := engine.Steer(t.Context(), text, nil); err != nil {
						t.Fatal(err)
					}
					waitEngineLifecycleTasks(t, engine)
				} else if _, err := engine.SubmitUserMessage(t.Context(), text); err != nil {
					t.Fatal(err)
				}
			}
			submit("first")
			for _, effort := range []string{"medium", "high", "low"} {
				if err := selectThinking(engine, effort); err != nil {
					t.Fatal(err)
				}
			}
			submit("second")
			if len(client.calls) != 2 {
				t.Fatalf("requests = %d, want two", len(client.calls))
			}
			first, second := client.calls[0], client.calls[1]
			if first.ReasoningEffort != "xhigh" || second.ReasoningEffort != first.ReasoningEffort {
				t.Fatalf("request efforts = %q, %q; want original xhigh on both", first.ReasoningEffort, second.ReasoningEffort)
			}
			for i, item := range first.Items {
				if !bytes.Equal(item.Raw, second.Items[i].Raw) {
					t.Fatalf("dispatched input %d changed", i)
				}
			}
			updates := 0
			for i, item := range second.Items {
				var wire struct {
					Type      string `json:"type"`
					Reasoning struct {
						Effort string `json:"effort"`
					} `json:"reasoning"`
				}
				if err := json.Unmarshal(item.Raw, &wire); err != nil {
					t.Fatal(err)
				}
				if wire.Type != "configuration_update" {
					continue
				}
				updates++
				if wire.Reasoning.Effort != "low" || i+1 >= len(second.Items) {
					t.Fatalf("invalid final update: %+v", wire)
				}
				next := llm.MessagesFromItems(second.Items[i+1 : i+2])
				if len(next) != 1 || next[0].Role != llm.RoleUser || next[0].Content == nil || *next[0].Content != "second" {
					t.Fatalf("update does not precede selected user input: %+v", next)
				}
			}
			if updates != 1 {
				t.Fatalf("configuration updates = %d, want one", updates)
			}
			for _, effort := range []string{"high", "low"} {
				if err := selectThinking(engine, effort); err != nil {
					t.Fatal(err)
				}
			}
			submit("third")
			if len(client.calls) != 3 {
				t.Fatalf("requests after no-op selections = %d, want three", len(client.calls))
			}
			third := client.calls[2]
			if third.ReasoningEffort != first.ReasoningEffort {
				t.Fatal("no-op selections changed the original request baseline")
			}
			for i, item := range second.Items {
				if i >= len(third.Items) || !bytes.Equal(item.Raw, third.Items[i].Raw) {
					t.Fatal("no-op selections changed earlier dispatched input")
				}
			}
			updates = 0
			for _, item := range third.Items {
				if item.Type == llm.ResponseItemTypeConfigurationUpdate {
					updates++
				}
			}
			if updates != 1 || engine.ThinkingLevel() != "low" {
				t.Fatal("final-value no-op appended another update or changed desired Thinking")
			}
		})
	}
}
