package runtime

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"builder/server/llm"
	"builder/server/session"
	"builder/server/tools"
	"builder/shared/toolspec"
)

func TestBuildReviewerTranscriptMessagesSummarizesViewImagePayloads(t *testing.T) {
	messages := []llm.Message{
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:    "call-view-image-1",
				Name:  string(toolspec.ToolViewImage),
				Input: []byte(`{"path":"docs/page.pdf"}`),
			}},
		},
		{
			Role:       llm.RoleTool,
			ToolCallID: "call-view-image-1",
			Name:       string(toolspec.ToolViewImage),
			Content:    `[{"type":"input_file","filename":"page.pdf","file_data":"data:application/pdf;base64,JVBERi0xLjQKJUVPRg=="}]`,
		},
	}

	got := buildReviewerTranscriptMessages(messages)
	if len(got) != 2 {
		t.Fatalf("reviewer transcript messages = %d, want 2 (%+v)", len(got), got)
	}
	if !strings.Contains(got[0].Content, "Tool call:") || !strings.Contains(got[0].Content, "docs/page.pdf") {
		t.Fatalf("expected tool call entry with source path, got %q", got[0].Content)
	}
	if !strings.Contains(got[1].Content, "Tool result:") || !strings.Contains(got[1].Content, "attached PDF: page.pdf") {
		t.Fatalf("expected summarized view_image tool result, got %q", got[1].Content)
	}
	if strings.Contains(got[1].Content, "base64") || strings.Contains(got[1].Content, "data:application/pdf") {
		t.Fatalf("expected reviewer transcript to omit binary payloads, got %q", got[1].Content)
	}
}

func TestReviewerSuggestions_ReusesStableMetaForPromptCachePrefix(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	engineClient := &fakeClient{caps: llm.ProviderCapabilities{ProviderID: "openai-compatible", SupportsResponsesAPI: true}}
	reviewerClient := &fakeClient{
		caps: llm.ProviderCapabilities{ProviderID: "openai-compatible", SupportsResponsesAPI: true, SupportsPromptCacheKey: true},
		responses: []llm.Response{
			{Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"suggestions":[]}`}, Usage: llm.Usage{InputTokens: 10}},
			{Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"suggestions":[]}`}, Usage: llm.Usage{InputTokens: 10}},
		},
	}
	eng, err := New(store, engineClient, tools.NewRegistry(), Config{Model: "gpt-5", Reviewer: ReviewerConfig{Model: "gpt-5"}})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if _, err := eng.runReviewerSuggestions(context.Background(), "step-1", reviewerClient); err != nil {
		t.Fatalf("first reviewer suggestions: %v", err)
	}
	if _, err := eng.runReviewerSuggestions(context.Background(), "step-2", reviewerClient); err != nil {
		t.Fatalf("second reviewer suggestions: %v", err)
	}

	if len(reviewerClient.calls) != 2 {
		t.Fatalf("reviewer client calls = %d, want 2", len(reviewerClient.calls))
	}
	firstMessages := requestMessages(reviewerClient.calls[0])
	secondMessages := requestMessages(reviewerClient.calls[1])
	if !hasReviewerMessagePrefix(secondMessages, firstMessages) {
		t.Fatalf("expected second reviewer request to reuse first as prefix\nfirst=%+v\nsecond=%+v", firstMessages, secondMessages)
	}
	if warnings := persistedCacheWarnings(t, store); len(warnings) != 0 {
		t.Fatalf("expected stable reviewer prompt cache lineage, got warnings %+v", warnings)
	}
}

func TestBuildReviewerRequestUsesReviewerModelCapabilities(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	eng, err := New(store, &fakeClient{}, tools.NewRegistry(), Config{
		Model: "gpt-5",
		Reviewer: ReviewerConfig{
			Model: "local-reviewer",
			ModelCapabilities: session.LockedModelCapabilities{
				SupportsReasoningEffort: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	req, err := eng.buildReviewerRequest(context.Background(), &fakeClient{})
	if err != nil {
		t.Fatalf("build reviewer request: %v", err)
	}
	if !req.SupportsReasoningEffort {
		t.Fatal("expected reviewer request to use reviewer model capability override")
	}
}

func TestReviewerSuggestions_ReopenKeepsPromptCachePrefixStable(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	engineClient := &fakeClient{caps: llm.ProviderCapabilities{ProviderID: "openai-compatible", SupportsResponsesAPI: true}}
	reviewerClient := &fakeClient{
		caps: llm.ProviderCapabilities{ProviderID: "openai-compatible", SupportsResponsesAPI: true, SupportsPromptCacheKey: true},
		responses: []llm.Response{
			{Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"suggestions":[]}`}, Usage: llm.Usage{InputTokens: 10}},
			{Assistant: llm.Message{Role: llm.RoleAssistant, Content: `{"suggestions":[]}`}, Usage: llm.Usage{InputTokens: 10}},
		},
	}
	eng, err := New(store, engineClient, tools.NewRegistry(), Config{Model: "gpt-5", Reviewer: ReviewerConfig{Model: "gpt-5"}})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	if err := eng.appendUserMessage("prep-1", "first request"); err != nil {
		t.Fatalf("append first message: %v", err)
	}
	if _, err := eng.runReviewerSuggestions(context.Background(), "step-1", reviewerClient); err != nil {
		t.Fatalf("first reviewer suggestions: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("close original engine: %v", err)
	}

	reopened, err := session.Open(store.Dir())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	reopenedEng, err := New(reopened, engineClient, tools.NewRegistry(), Config{Model: "gpt-5", Reviewer: ReviewerConfig{Model: "gpt-5"}})
	if err != nil {
		t.Fatalf("new reopened engine: %v", err)
	}
	t.Cleanup(func() { _ = reopenedEng.Close() })
	if err := reopenedEng.appendUserMessage("prep-2", "second request"); err != nil {
		t.Fatalf("append second message: %v", err)
	}
	if _, err := reopenedEng.runReviewerSuggestions(context.Background(), "step-2", reviewerClient); err != nil {
		t.Fatalf("second reviewer suggestions: %v", err)
	}

	if len(reviewerClient.calls) != 2 {
		t.Fatalf("reviewer client calls = %d, want 2", len(reviewerClient.calls))
	}
	firstMessages := requestMessages(reviewerClient.calls[0])
	secondMessages := requestMessages(reviewerClient.calls[1])
	if !hasReviewerMessagePrefix(secondMessages, firstMessages) {
		t.Fatalf("expected reopened reviewer request to extend the original request\nfirst=%+v\nsecond=%+v", firstMessages, secondMessages)
	}
	if warnings := persistedCacheWarnings(t, reopened); len(warnings) != 0 {
		t.Fatalf("expected no reviewer cache warnings after reopen, got %+v", warnings)
	}
}

func hasReviewerMessagePrefix(messages []llm.Message, prefix []llm.Message) bool {
	if len(prefix) > len(messages) {
		return false
	}
	for i := range prefix {
		if !reflect.DeepEqual(messages[i], prefix[i]) {
			return false
		}
	}
	return true
}
