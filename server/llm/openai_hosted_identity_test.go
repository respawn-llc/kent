package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"core/shared/modelcontract"
	"core/shared/textutil"
)

func TestGenerateHostedSearchIdentityDoesNotDependOnJSONFieldOrder(t *testing.T) {
	transport := newOpenAIStreamTestTransport(t,
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"ws_identity","type":"web_search_call","status":"in_progress"}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"ws_identity","type":"web_search_call","status":"completed","action":{"type":"search","query":"kent"}}}`,
		`{"type":"response.completed","response":{"output":[{"type":"web_search_call","id":"ws_identity","status":"completed","action":{"type":"search","query":"kent"}}]}}`,
		`[DONE]`,
	)

	response, err := transport.Generate(context.Background(), OpenAIRequest{
		SessionID:      textutil.Value("test-session"),
		ToolChoiceMode: ToolChoiceModeAutomatic,
		Model:          "gpt-5",
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
		name         string
		streamed     string
		outputIndex  int
		wantStreamed bool
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
			name:         "distinct call omitted from completed output is retained",
			streamed:     other,
			outputIndex:  0,
			wantStreamed: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := newOpenAIStreamTestTransport(t,
				fmt.Sprintf(`{"type":"response.output_item.added","output_index":%d,"item":%s}`, test.outputIndex, test.streamed),
				fmt.Sprintf(`{"type":"response.completed","response":{"output":[%s]}}`, completed),
				`[DONE]`,
			)
			response, err := transport.Generate(context.Background(), OpenAIRequest{
				SessionID:      textutil.Value("test-session"),
				ToolChoiceMode: ToolChoiceModeAutomatic,
				Model:          "gpt-5",
			}, StreamCallbacks{})
			if err != nil {
				t.Fatal(err)
			}
			expected := []string{completed}
			if test.wantStreamed {
				expected = append(expected, test.streamed)
			}
			if len(response.OutputItems) != len(expected) {
				t.Fatalf("got %d output items, want %d", len(response.OutputItems), len(expected))
			}
			if test.wantStreamed && len(response.ProviderEvidence.HostedTools) != 2 {
				t.Fatalf("hosted tool evidence = %+v, want completed and streamed calls", response.ProviderEvidence.HostedTools)
			}
			if test.wantStreamed {
				hostedByID := make(map[string]modelcontract.HostedToolUsageEvidence, len(response.ProviderEvidence.HostedTools))
				for _, hosted := range response.ProviderEvidence.HostedTools {
					if hosted.ID == nil || hosted.Type == nil || hosted.Status == nil || hosted.ActionKind == nil {
						t.Fatalf("hosted tool evidence = %+v, want complete identity", response.ProviderEvidence.HostedTools)
					}
					hostedByID[*hosted.ID] = hosted
					if *hosted.Type != "web_search_call" || *hosted.Status != "completed" || *hosted.ActionKind != "search" {
						t.Fatalf("hosted tool evidence = %+v, want web search identity", hosted)
					}
				}
				if len(hostedByID) != 2 {
					t.Fatalf("hosted tool evidence IDs = %v, want two distinct calls", hostedByID)
				}
				assertJSONField(t, hostedByID["ws_other"].Usage, "searches", "2")
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
