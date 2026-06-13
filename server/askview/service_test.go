package askview

import (
	"context"
	"testing"
	"time"

	"core/server/registry"
	askquestion "core/server/tools/askquestion"
	"core/shared/serverapi"
)

type stubPendingPromptSource struct {
	items []registry.PendingPromptSnapshot
}

func (s *stubPendingPromptSource) ListPendingPrompts(string) []registry.PendingPromptSnapshot {
	return append([]registry.PendingPromptSnapshot(nil), s.items...)
}

func TestServiceListsPendingAsksBySession(t *testing.T) {
	now := time.Now().UTC()
	svc := NewService(&stubPendingPromptSource{items: []registry.PendingPromptSnapshot{
		{Request: askquestion.Request{ID: "ask-1", Question: "one?", Suggestions: []string{"a", "b"}, RecommendedOptionIndex: 2}, CreatedAt: now},
		{Request: askquestion.Request{ID: "approval-1", Question: "allow?", Approval: true}, CreatedAt: now.Add(time.Second)},
	}})

	resp, err := svc.ListPendingAsksBySession(context.Background(), serverapi.AskListPendingBySessionRequest{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("ListPendingAsksBySession: %v", err)
	}
	if len(resp.Asks) != 1 {
		t.Fatalf("expected one pending ask, got %+v", resp)
	}
	if resp.Asks[0].AskID != "ask-1" || resp.Asks[0].RecommendedOptionIndex != 2 {
		t.Fatalf("unexpected pending ask: %+v", resp.Asks[0])
	}
}

func TestServiceRequiresSessionID(t *testing.T) {
	if _, err := NewService(&stubPendingPromptSource{}).ListPendingAsksBySession(context.Background(), serverapi.AskListPendingBySessionRequest{}); err == nil {
		t.Fatal("expected validation error")
	}
}
