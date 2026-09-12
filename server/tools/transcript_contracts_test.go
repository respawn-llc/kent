package tools

import (
	"encoding/json"
	"testing"

	"core/shared/toolspec"
	"core/shared/transcript"
)

func TestWebSearchDetailProjection(t *testing.T) {
	detail, err := DecodeWebSearchDetail(json.RawMessage(`{"action":{"type":"search","queries":["first","second"],"sources":[{"url":"https://example.com"},{"url":"https://example.com"}]},"results":[{"type":"text_result","title":"Example","url":"https://example.com","snippet":"not display content"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if detail == nil || len(detail.Results) != 1 || len(detail.Sources) != 2 || detail.Sources[0] != detail.Sources[1] {
		t.Fatalf("lost ordered search facts: %+v", detail)
	}
	search, ok := detail.Action.(transcript.WebSearchSearch)
	if !ok || len(search.Queries) != 2 || search.Queries[0] != "first" || search.Queries[1] != "second" {
		t.Fatalf("lost query list: %+v", detail.Action)
	}
	if detail.Results[0].Title == nil || *detail.Results[0].Title != "Example" {
		t.Fatalf("lost result title: %+v", detail.Results[0])
	}
}

func TestWebSearchDetailOptionalAndNonTextFacts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		results int
		sources int
	}{
		{"missing", `{"action":{"type":"search","queries":["query"]}}`, 0, 0},
		{"null", `{"action":{"type":"search","sources":null},"results":null}`, 0, 0},
		{"metadata", `{"action":{"type":"search"},"results":[{"type":"image_result","tokens":12}]}`, 0, 0},
		{"unrelated action fields", `{"action":{"type":"search","url":"https://example.com","pattern":"ignored"}}`, 0, 0},
		{"source only", `{"action":{"type":"search","sources":[{"url":"javascript:alert(1)"}]}}`, 0, 1},
		{"title only", `{"action":{"type":"search"},"results":[{"title":"Title"}]}`, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail, err := DecodeWebSearchDetail(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if tc.results+tc.sources == 0 {
				if detail != nil {
					t.Fatalf("non-useful detail: %+v", detail)
				}
				return
			}
			if detail == nil || len(detail.Results) != tc.results || len(detail.Sources) != tc.sources {
				t.Fatalf("lost optional facts: %+v", detail)
			}
		})
	}
	detail, err := DecodeWebSearchDetail(json.RawMessage(`{"action":{"type":"search"},"results":[{"type":"image_result","image_url":"https://example.com/image","source_website_url":"https://example.com/page"},{"type":"image_result","source_website_url":"https://example.com/page"},{"type":"video_result","title":"Video","url":"not a URL"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if detail == nil || len(detail.Results) != 3 {
		t.Fatalf("lost results: %+v", detail)
	}
	for i, want := range []string{"https://example.com/image", "https://example.com/page", "not a URL"} {
		result := detail.Results[i]
		if result.Destination == nil || *result.Destination != want || result.Image != (i < 2) {
			t.Fatalf("incorrect non-text destination: %+v", result)
		}
	}
}

func TestWebSearchPageDetails(t *testing.T) {
	for _, raw := range []string{
		`{"action":{"type":"open_page","url":"https://example.com"}}`,
		`{"action":{"type":"find_in_page","url":"https://example.com","pattern":"needle"}}`,
	} {
		detail, err := DecodeWebSearchDetail(json.RawMessage(raw))
		if err != nil || detail == nil {
			t.Fatalf("page detail missing: %+v, %v", detail, err)
		}
		switch action := detail.Action.(type) {
		case transcript.WebSearchOpenPage:
			if action.URL == nil || *action.URL != "https://example.com" {
				t.Fatal("page URL lost")
			}
		case transcript.WebSearchFindInPage:
			if action.Pattern == nil || *action.Pattern != "needle" || action.URL == nil {
				t.Fatal("find facts lost")
			}
		default:
			t.Fatalf("unexpected page action: %+v", action)
		}
	}
}

func TestWebSearchDetailRejectsMalformedConsumedFacts(t *testing.T) {
	for _, raw := range []string{
		`{"action":{"type":"search"},"results":{}}`,
		`{"action":{"type":"search"},"results":[{"title":42}]}`,
		`{"action":{"type":"search"},"results":[{"url":""}]}`,
		`{"action":{"type":"search","sources":[{"url":false}]}}`,
		`{"action":{"type":"search","sources":[{"url":""}]}}`,
		`{"action":{"type":"search","queries":[""]}}`,
	} {
		if detail, err := DecodeWebSearchDetail(json.RawMessage(raw)); err == nil || detail != nil {
			t.Fatalf("malformed facts accepted: %s", raw)
		}
	}
}

func TestWebSearchMalformedQueryListRetainsIdentifiedCompletion(t *testing.T) {
	raw := json.RawMessage(`{"type":"web_search_call","id":"search-1","action":{"type":"search","query":"example","queries":42}}`)
	executions := HostedExecutionsFromOutputs([]HostedToolOutput{{Raw: raw}}, DefinitionsFor([]toolspec.ID{toolspec.ToolWebSearch}))
	if len(executions) != 1 || string(executions[0].Result.Output) != string(raw) {
		t.Fatalf("identified completion lost or rewritten: %+v", executions)
	}
	if detail, err := DecodeWebSearchDetail(executions[0].Result.Output); err == nil || detail != nil {
		t.Fatal("malformed queries did not reach failed-row projection")
	}
}

func TestFormatAskQuestionToolOutputRejectsPresentZeroSelection(t *testing.T) {
	if formatted, ok := formatAskQuestionToolOutput([]byte(`{"selected_option_number":0,"freeform_answer":"typed"}`)); ok || formatted != "" {
		t.Fatalf("present zero selection formatted as %q, %t; want malformed payload rejection", formatted, ok)
	}
}

func TestFormatAskQuestionToolOutputAcceptsLegacyOmittedSelection(t *testing.T) {
	formatted, ok := formatAskQuestionToolOutput([]byte(`{"freeform_answer":"typed"}`))
	if !ok || formatted == "" {
		t.Fatalf("legacy omitted selection formatted as %q, %t; want freeform answer", formatted, ok)
	}
}

func TestViewImageCallMetadataCarriesTypedPath(t *testing.T) {
	const imagePath = "/tmp/kent-clipboard.png"

	meta := BuildCallTranscriptMeta(
		string(toolspec.ToolViewImage),
		ToolCallContext{},
		[]byte(`{"path":"`+imagePath+`"}`),
	)

	if meta.RenderHint == nil {
		t.Fatal("view_image metadata is missing render hint")
	}
	if meta.RenderHint.Kind != transcript.ToolRenderKindPlain {
		t.Fatalf("view_image render hint kind = %q, want %q", meta.RenderHint.Kind, transcript.ToolRenderKindPlain)
	}
	if meta.RenderHint.Path != imagePath {
		t.Fatalf("view_image render hint path = %q, want %q", meta.RenderHint.Path, imagePath)
	}
	if meta.Command != "" {
		t.Fatalf("view_image metadata carries command text %q; path belongs only in render hint", meta.Command)
	}
	if meta.CompactText != "" {
		t.Fatalf("view_image metadata carries compact text %q; path belongs only in render hint", meta.CompactText)
	}
}

func TestWebSearchCallMetadataKeepsQueryAsInput(t *testing.T) {
	const query = "Go error handling"

	meta := BuildCallTranscriptMeta(
		string(toolspec.ToolWebSearch),
		ToolCallContext{},
		[]byte(`{"query":"`+query+`"}`),
	)

	if meta.Command != query {
		t.Fatalf("web_search command = %q, want query %q", meta.Command, query)
	}
	if meta.CompactText != query {
		t.Fatalf("web_search compact text = %q, want query %q", meta.CompactText, query)
	}
}
