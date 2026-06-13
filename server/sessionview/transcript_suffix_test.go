package sessionview

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	"core/shared/serverapi"
)

func TestGetSessionCommittedTranscriptSuffixReturnsRuntimeViewSuffix(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("reply-%03d", i), Phase: llm.MessagePhaseFinal}); err != nil {
			t.Fatalf("append message %d: %v", i, err)
		}
	}
	eng, err := runtime.New(store, &serviceFakeLLM{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	svc := NewService(NewStaticSessionResolver(store), NewStaticRuntimeResolver(eng), nil)

	resp, err := svc.GetSessionCommittedTranscriptSuffix(context.Background(), serverapi.SessionCommittedTranscriptSuffixRequest{
		SessionID:       store.Meta().SessionID,
		AfterEntryCount: 1,
		Limit:           2,
	})
	if err != nil {
		t.Fatalf("get committed transcript suffix: %v", err)
	}
	if resp.Suffix.StartEntryCount != 1 || resp.Suffix.NextEntryCount != 3 {
		t.Fatalf("unexpected cursor metadata: %+v", resp.Suffix)
	}
	if got := len(resp.Suffix.Entries); got != 2 {
		t.Fatalf("entries = %d, want 2", got)
	}
	if resp.Suffix.Entries[0].Text != "reply-001" || resp.Suffix.Entries[1].Text != "reply-002" {
		t.Fatalf("unexpected entries: %+v", resp.Suffix.Entries)
	}
}

func TestGetSessionCommittedTranscriptSuffixReturnsDormantSuffix(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("reply-%03d", i), Phase: llm.MessagePhaseFinal}); err != nil {
			t.Fatalf("append message %d: %v", i, err)
		}
	}
	svc := NewService(NewStaticSessionResolver(store), nil, nil)

	resp, err := svc.GetSessionCommittedTranscriptSuffix(context.Background(), serverapi.SessionCommittedTranscriptSuffixRequest{
		SessionID:       store.Meta().SessionID,
		AfterEntryCount: 2,
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("get dormant committed transcript suffix: %v", err)
	}
	if resp.Suffix.StartEntryCount != 2 || resp.Suffix.NextEntryCount != 4 {
		t.Fatalf("unexpected cursor metadata: %+v", resp.Suffix)
	}
	if got := len(resp.Suffix.Entries); got != 2 {
		t.Fatalf("entries = %d, want 2", got)
	}
	if resp.Suffix.Entries[0].Text != "reply-002" || resp.Suffix.Entries[1].Text != "reply-003" {
		t.Fatalf("unexpected entries: %+v", resp.Suffix.Entries)
	}
}

func TestGetSessionCommittedTranscriptSuffixRejectsInvalidLimit(t *testing.T) {
	svc := NewService(nil, nil, nil)

	_, err := svc.GetSessionCommittedTranscriptSuffix(context.Background(), serverapi.SessionCommittedTranscriptSuffixRequest{
		SessionID:       "session-1",
		AfterEntryCount: 0,
		Limit:           -1,
	})
	if err == nil || !strings.Contains(err.Error(), "limit must be >= 0") {
		t.Fatalf("expected invalid limit error, got %v", err)
	}
}
