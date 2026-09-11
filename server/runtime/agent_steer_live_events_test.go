package runtime

import (
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/shared/runtimeids"
)

func TestAgentSteerLiveEventsProjectTheCommittedMessage(t *testing.T) {
	t.Run("direct submission", func(t *testing.T) {
		var events []Event
		eng := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{responses: []llm.Response{finalTextResponse("done")}}, tools.NewRegistry(), Config{
			Model: "gpt-5",
			OnEvent: func(event Event) {
				events = append(events, event)
			},
		})
		steer, err := NewAgentSteer(runtimeids.NewSessionID(), "direct")
		if err != nil {
			t.Fatalf("NewAgentSteer: %v", err)
		}
		if _, err := eng.SubmitAgentSteerWithHooks(t.Context(), steer, nil, nil); err != nil {
			t.Fatalf("SubmitAgentSteerWithHooks: %v", err)
		}
		assertAgentSteerConversationEvent(t, events)
	})

	t.Run("queued flush", func(t *testing.T) {
		var events []Event
		eng := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{responses: []llm.Response{finalOutputItemResponse("done")}}, tools.NewRegistry(), Config{
			Model: "gpt-5",
			OnEvent: func(event Event) {
				events = append(events, event)
			},
		})
		steer, err := NewAgentSteer(runtimeids.NewSessionID(), "queued")
		if err != nil {
			t.Fatalf("NewAgentSteer: %v", err)
		}
		if _, err := eng.QueueAgentSteer(t.Context(), steer, nil); err != nil {
			t.Fatal(err)
		}
		waitEngineLifecycleTasks(t, eng)
		assertAgentSteerConversationEvent(t, events)
	})
}

func assertAgentSteerConversationEvent(t *testing.T, events []Event) {
	t.Helper()
	for _, event := range events {
		if event.Kind != EventConversationUpdated ||
			event.Message.Role != llm.RoleDeveloper ||
			event.Message.MessageType == nil ||
			*event.Message.MessageType != llm.MessageTypeAgentSteer ||
			event.Message.Content == nil {
			continue
		}
		if entries := TranscriptEntriesFromEvent(event); len(entries) != 1 {
			t.Fatalf("agent steer event entries = %+v, want one row", entries)
		}
		if facts := TranscriptCommittedRowFactsFromEvent(event); len(facts) != 1 ||
			facts[0].Notice == nil ||
			facts[0].Notice.MessageType != llm.MessageTypeAgentSteer {
			t.Fatalf("agent steer event facts = %+v, want typed notice", facts)
		}
		return
	}
	t.Fatalf("no committed agent steer conversation event in %+v", events)
}
