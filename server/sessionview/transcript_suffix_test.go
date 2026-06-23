package sessionview

import (
	"context"
	"fmt"
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
		SessionID: store.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("get committed transcript suffix: %v", err)
	}
	if got := len(resp.Suffix.Entries); got != 4 {
		t.Fatalf("entries = %d, want 4 (newest segment)", got)
	}
	if resp.Suffix.Entries[0].Text != "reply-000" || resp.Suffix.Entries[3].Text != "reply-003" {
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
		SessionID: store.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("get dormant committed transcript suffix: %v", err)
	}
	if got := len(resp.Suffix.Entries); got != 4 {
		t.Fatalf("entries = %d, want 4 (newest segment)", got)
	}
	if resp.Suffix.Entries[0].Text != "reply-000" || resp.Suffix.Entries[3].Text != "reply-003" {
		t.Fatalf("unexpected entries: %+v", resp.Suffix.Entries)
	}
}
