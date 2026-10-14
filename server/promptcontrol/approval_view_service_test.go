package promptcontrol

import (
	"context"
	"testing"

	"core/internal/testharness/requestfixture"
	"core/internal/testharness/testsetup"
	"core/server/registry"
	askquestion "core/server/tools"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
)

type stubApprovalPendingPromptSource struct {
	items []registry.PendingPromptSnapshot
}

func (s *stubApprovalPendingPromptSource) ListPendingPrompts(string) []registry.PendingPromptSnapshot {
	return append([]registry.PendingPromptSnapshot(nil), s.items...)
}

func TestApprovalViewServiceRejectsMalformedPendingToolCallIdentity(t *testing.T) {
	for name, request := range map[string]askquestion.AskQuestionRequest{
		"step": {ToolCallID: "approval-1", StepID: "step-1", Question: "allow?", Approval: true},
	} {
		t.Run(name, func(t *testing.T) {
			svc := NewApprovalViewService(&stubApprovalPendingPromptSource{items: []registry.PendingPromptSnapshot{{Request: request}}}, testsetup.OpenStore(t, t.TempDir()))
			if _, err := svc.ListPendingApprovalsBySession(context.Background(), &promptpb.ListPendingRequest{SessionId: requestfixture.Selector(t, "33333333-3333-4333-8333-333333333333")}); err == nil {
				t.Fatal("accepted malformed pending prompt identity")
			}
		})
	}
}
