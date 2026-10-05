package promptcontrol

import (
	"context"
	"testing"
	"time"

	"core/server/registry"
	askquestion "core/server/tools"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
)

const promptViewStepID = "11111111-1111-4111-8111-111111111111"

type stubAskPendingPromptSource struct {
	items []registry.PendingPromptSnapshot
}

func (s *stubAskPendingPromptSource) ListPendingPrompts(string) []registry.PendingPromptSnapshot {
	return append([]registry.PendingPromptSnapshot(nil), s.items...)
}

func TestServiceListsPendingAsksBySession(t *testing.T) {
	now := time.Now().UTC()
	svc := NewAskViewService(&stubAskPendingPromptSource{items: []registry.PendingPromptSnapshot{
		{Request: askquestion.AskQuestionRequest{ToolCallID: "ask-1", StepID: promptViewStepID, Question: "one?", Suggestions: []string{"a", "b"}, RecommendedOptionIndex: 2}, CreatedAt: now},
		{Request: askquestion.AskQuestionRequest{ToolCallID: "approval-1", StepID: promptViewStepID, Question: "allow?", Approval: true}, CreatedAt: now.Add(time.Second)},
	}})

	resp, err := svc.ListPendingAsksBySession(context.Background(), &promptpb.ListPendingRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("ListPendingAsksBySession: %v", err)
	}
	if len(resp.Questions) != 1 {
		t.Fatalf("expected one pending ask, got %+v", resp)
	}
	if resp.Questions[0].ToolCallId != "ask-1" ||
		resp.Questions[0].SessionId != "session-1" ||
		resp.Questions[0].StepId != promptViewStepID ||
		resp.Questions[0].RecommendedOptionIndex == nil ||
		*resp.Questions[0].RecommendedOptionIndex != 2 {
		t.Fatalf("unexpected pending ask: %+v", resp.Questions[0])
	}
}

func TestAskViewServiceRejectsMalformedPendingToolCallIdentity(t *testing.T) {
	for name, request := range map[string]askquestion.AskQuestionRequest{
		"prompt": {ToolCallID: " ask-1", StepID: promptViewStepID, Question: "one?"},
		"step":   {ToolCallID: "ask-1", StepID: "step-1", Question: "one?"},
	} {
		t.Run(name, func(t *testing.T) {
			svc := NewAskViewService(&stubAskPendingPromptSource{items: []registry.PendingPromptSnapshot{{Request: request}}})
			if _, err := svc.ListPendingAsksBySession(context.Background(), &promptpb.ListPendingRequest{SessionId: "session-1"}); err == nil {
				t.Fatal("accepted malformed pending prompt identity")
			}
		})
	}
}

func TestServiceEncodesAbsentPendingAskRecommendationAsNil(t *testing.T) {
	svc := NewAskViewService(&stubAskPendingPromptSource{items: []registry.PendingPromptSnapshot{{
		CreatedAt: time.Now().UTC(),
		Request: askquestion.AskQuestionRequest{
			ToolCallID:  "ask-1",
			StepID:      promptViewStepID,
			Question:    "one?",
			Suggestions: []string{"a"},
		},
	}}})

	resp, err := svc.ListPendingAsksBySession(
		context.Background(),
		&promptpb.ListPendingRequest{SessionId: "session-1"},
	)
	if err != nil {
		t.Fatalf("ListPendingAsksBySession: %v", err)
	}
	if len(resp.Questions) != 1 || resp.Questions[0].RecommendedOptionIndex != nil {
		t.Fatalf("pending asks = %+v, want absent recommendation", resp.Questions)
	}
}

func TestServiceRejectsInvalidPendingAskRecommendation(t *testing.T) {
	svc := NewAskViewService(&stubAskPendingPromptSource{items: []registry.PendingPromptSnapshot{{
		Request: askquestion.AskQuestionRequest{
			ToolCallID:             "ask-1",
			StepID:                 promptViewStepID,
			Question:               "one?",
			Suggestions:            []string{"a"},
			RecommendedOptionIndex: 2,
		},
	}}})

	if _, err := svc.ListPendingAsksBySession(
		context.Background(),
		&promptpb.ListPendingRequest{SessionId: "session-1"},
	); err == nil {
		t.Fatal("accepted pending ask recommendation outside suggestions")
	}
}

func TestAskViewServiceRequiresSessionID(t *testing.T) {
	if _, err := NewAskViewService(&stubAskPendingPromptSource{}).ListPendingAsksBySession(context.Background(), &promptpb.ListPendingRequest{}); err == nil {
		t.Fatal("expected validation error")
	}
}
