package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"core/shared/textutil"
)

func TestGenerateHostedSearchIdentityDoesNotDependOnJSONFieldOrder(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"ws_identity","type":"web_search_call","status":"in_progress"}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"ws_identity","type":"web_search_call","status":"completed","action":{"type":"search","query":"kent"}}}`,
		`{"type":"response.completed","response":{"output":[{"type":"web_search_call","id":"ws_identity","status":"completed","action":{"type":"search","query":"kent"}}]}}`,
		`[DONE]`,
	)

	response, err := transport.Generate(context.Background(), ResponsesRequest{
		SessionID:      textutil.Value("test-session"),
		ToolChoiceMode: ToolChoiceModeAutomatic,
		Model:          "gpt-6-sol", ReasoningEffort: "high",
	}, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range response.OutputItems {
		var output struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(item.Raw, &output); err != nil {
			t.Fatal(err)
		}
		if output.ID == "ws_identity" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("one hosted search must appear once in the response, got %d occurrences", count)
	}
}

func TestGenerateHostedSearchKeepsCompletedPayloadAndDistinctCalls(t *testing.T) {
	completed := `{"id":"ws_final","type":"web_search_call","status":"completed","action":{"type":"search","query":"kent"},"usage":{"searches":1}}`
	other := `{"id":"ws_other","type":"web_search_call","status":"completed","action":{"type":"search","query":"kent"},"usage":{"searches":2}}`
	for _, test := range []struct {
		name        string
		streamed    string
		outputIndex int
	}{
		{
			name:        "completed payload replaces in-progress item",
			streamed:    `{"id":"ws_final","type":"web_search_call","status":"in_progress"}`,
			outputIndex: 0,
		},
		{
			name:        "same call at different completed position",
			streamed:    completed,
			outputIndex: 1,
		},
		{
			name:        "distinct call omitted from completed output is not accepted",
			streamed:    other,
			outputIndex: 0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := newOpenAIStreamTestTransport(t,
				fmt.Sprintf(`{"type":"response.output_item.added","output_index":%d,"item":%s}`, test.outputIndex, test.streamed),
				fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":%s}`, completed),
				fmt.Sprintf(`{"type":"response.completed","response":{"output":[%s]}}`, completed),
				`[DONE]`,
			)
			response, err := transport.Generate(context.Background(), ResponsesRequest{
				SessionID:      textutil.Value("test-session"),
				ToolChoiceMode: ToolChoiceModeAutomatic,
				Model:          "gpt-6-sol", ReasoningEffort: "high",
			}, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			expected := []string{completed}
			if len(response.OutputItems) != len(expected) {
				t.Fatalf("got %d output items, want %d", len(response.OutputItems), len(expected))
			}
			if len(response.ProviderEvidence.HostedTools) != 1 {
				t.Fatalf("hosted tool evidence = %+v, want terminal call only", response.ProviderEvidence.HostedTools)
			}
			for _, hosted := range response.ProviderEvidence.HostedTools {
				if hosted.ID == nil || hosted.Type == nil || hosted.Status == nil || hosted.ActionKind == nil {
					t.Fatalf("hosted tool evidence = %+v, want complete identity", response.ProviderEvidence.HostedTools)
				}
				if *hosted.ID != "ws_final" {
					t.Fatalf("accepted omitted streamed call: %+v", hosted)
				}
				if *hosted.Type != "web_search_call" || *hosted.Status != "completed" || *hosted.ActionKind != "search" {
					t.Fatalf("hosted tool evidence = %+v, want web search identity", hosted)
				}
				assertJSONField(t, hosted.Usage, "searches", "1")
			}
			for i, item := range response.OutputItems {
				var got, want any
				if err := json.Unmarshal(item.Raw, &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(expected[i]), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("output item %d = %s, want %s", i, item.Raw, expected[i])
				}
			}
		})
	}
}
