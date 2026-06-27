package llm

import (
	"context"
	"testing"
)

type streamingOnlyTransport struct{}

func (streamingOnlyTransport) Generate(context.Context, OpenAIRequest) (OpenAIResponse, error) {
	return OpenAIResponse{}, nil
}

func (streamingOnlyTransport) Compact(context.Context, OpenAICompactionRequest) (OpenAICompactionResponse, error) {
	return OpenAICompactionResponse{}, nil
}

func (streamingOnlyTransport) GenerateStream(_ context.Context, _ OpenAIRequest, onDelta func(text string)) (OpenAIResponse, error) {
	if onDelta != nil {
		onDelta("Hel")
		onDelta("lo")
	}
	return OpenAIResponse{AssistantText: "Hello"}, nil
}

func TestOpenAIClientGenerateStreamDoesNotReplayFinalTextAsDelta(t *testing.T) {
	client := NewOpenAIClient(streamingOnlyTransport{})
	req := Request{Model: "gpt-5"}

	var deltas []string
	resp, err := client.GenerateStream(context.Background(), req, func(text string) {
		deltas = append(deltas, text)
	})
	if err != nil {
		t.Fatalf("generate stream failed: %v", err)
	}
	if resp.Assistant.Content != "Hello" {
		t.Fatalf("expected final assistant content, got %q", resp.Assistant.Content)
	}
	if len(deltas) != 2 || deltas[0] != "Hel" || deltas[1] != "lo" {
		t.Fatalf("expected only incremental stream deltas, got %+v", deltas)
	}
}

func TestOpenAIClientLegacyStreamTransportEmitsUnknownDeltaPhase(t *testing.T) {
	client := NewOpenAIClient(streamingOnlyTransport{})
	req := Request{Model: "gpt-5"}

	var deltas []AssistantDelta
	_, err := client.GenerateStreamWithEvents(context.Background(), req, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("generate stream failed: %v", err)
	}
	if len(deltas) != 2 {
		t.Fatalf("expected two deltas, got %+v", deltas)
	}
	for _, delta := range deltas {
		if delta.Phase != "" {
			t.Fatalf("expected unknown phase for legacy text-only stream delta, got %+v", deltas)
		}
	}
}
