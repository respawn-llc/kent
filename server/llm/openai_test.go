package llm

import (
	"context"
	"testing"

	"core/shared/textutil"
)

type streamingOnlyTransport struct{}

func (streamingOnlyTransport) PrepareCompaction(request CompactionRequest) CompactionRequest {
	return request
}

func (streamingOnlyTransport) Compact(context.Context, ResponsesRequest) (ResponsesCompactionResponse, error) {
	return ResponsesCompactionResponse{}, nil
}

func (streamingOnlyTransport) Generate(_ context.Context, _ ResponsesRequest, callbacks StreamCallbacks) (ResponsesResponse, error) {
	if callbacks.OnAssistantDelta != nil {
		callbacks.OnAssistantDelta(AssistantDelta{Text: "Hel"})
		callbacks.OnAssistantDelta(AssistantDelta{Text: "lo"})
	}
	return ResponsesResponse{AssistantText: textutil.Value("Hello"), ProviderPhase: AbsentProviderPhase()}, nil
}

func TestRequestAsResponsesClonesPreparedSchemaCarriers(t *testing.T) {
	request := Request{
		Model:          "gpt-6-sol",
		ToolChoiceMode: ToolChoiceModeAutomatic,
		Tools: []Tool{{
			Name:   "shell",
			Schema: mustTestFunctionSchema(t, struct{}{}),
		}},
		StructuredOutput: &StructuredOutput{
			Name:   "reviewer_suggestions",
			Schema: mustTestStructuredSchema(t, testReviewerStructuredOutput{}),
		}, ReasoningEffort: "high",
	}
	projected := RequestAsResponses(request)
	request.Tools[0].Name = "mutated"
	request.StructuredOutput.Name = "mutated"
	if len(projected.Tools) != 1 ||
		projected.Tools[0].Name != "shell" ||
		!projected.Tools[0].Schema.Prepared() {
		t.Fatalf("projected tools changed with source mutation: %+v", projected.Tools)
	}
	if projected.StructuredOutput == nil || projected.StructuredOutput.Name != "reviewer_suggestions" {
		t.Fatalf("projected structured output changed with source mutation: %+v", projected.StructuredOutput)
	}
	if !projected.StructuredOutput.Schema.Prepared() {
		t.Fatal("projected structured output lost its prepared schema")
	}
}

func TestResponsesClientGenerateDoesNotReplayFinalTextAsDelta(t *testing.T) {
	client := NewResponsesClient(streamingOnlyTransport{})
	req := Request{Model: "gpt-6-sol", ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high"}

	var deltas []string
	resp, err := client.Generate(context.Background(), req, StreamCallbacks{
		OnAssistantDelta: func(delta AssistantDelta) {
			deltas = append(deltas, delta.Text)
		},
	})
	if err != nil {
		t.Fatalf("generate stream failed: %v", err)
	}
	if messageContent(resp.Assistant) != "Hello" {
		t.Fatalf("expected final assistant content, got %q", messageContent(resp.Assistant))
	}
	if len(deltas) != 2 || deltas[0] != "Hel" || deltas[1] != "lo" {
		t.Fatalf("expected only incremental stream deltas, got %+v", deltas)
	}
}

func TestResponsesClientGeneratePreservesFinalTextThatExtendsStreamWithWhitespace(t *testing.T) {
	transport := trailingWhitespaceStreamingTransport{}
	client := NewResponsesClient(transport)

	var deltas []string
	resp, err := client.Generate(
		context.Background(),
		Request{Model: "gpt-6-sol", ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high"},
		StreamCallbacks{
			OnAssistantDelta: func(delta AssistantDelta) {
				deltas = append(deltas, delta.Text)
			},
		},
	)
	if err != nil {
		t.Fatalf("generate stream: %v", err)
	}
	if len(deltas) != 1 || deltas[0] != "done\n\n" {
		t.Fatalf("stream deltas = %#v", deltas)
	}
	if resp.Assistant.Content == nil || *resp.Assistant.Content != "done\n\n" {
		t.Fatalf("final assistant content = %#v, want exact streamed text", resp.Assistant.Content)
	}
}

type trailingWhitespaceStreamingTransport struct {
	streamingOnlyTransport
}

func (trailingWhitespaceStreamingTransport) Generate(
	_ context.Context,
	_ ResponsesRequest,
	callbacks StreamCallbacks,
) (ResponsesResponse, error) {
	if callbacks.OnAssistantDelta != nil {
		callbacks.OnAssistantDelta(AssistantDelta{Text: "done\n\n"})
	}
	return ResponsesResponse{
		AssistantText: textutil.Value("done\n\n"),
		ProviderPhase: AbsentProviderPhase(),
	}, nil
}

func TestResponsesClientGenerateEmitsUnknownDeltaPhase(t *testing.T) {
	client := NewResponsesClient(streamingOnlyTransport{})
	req := Request{Model: "gpt-6-sol", ToolChoiceMode: ToolChoiceModeAutomatic, ReasoningEffort: "high"}

	var deltas []AssistantDelta
	_, err := client.Generate(context.Background(), req, StreamCallbacks{
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
