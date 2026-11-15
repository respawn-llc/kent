package core

import (
	"slices"
	"testing"
	"time"

	"core/server/registry"
	"core/server/tools"
	"core/shared/clientui"
	"core/shared/protoapi"
	"core/shared/runtimeids"
)

type taskPromptSnapshots []registry.PendingPromptSnapshot

func (s taskPromptSnapshots) ListPendingPrompts(string) []registry.PendingPromptSnapshot {
	return s
}

func TestTaskPromptProjectionPreservesAnswerMetadata(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	stepID := "11111111-1111-4111-8111-111111111111"
	createdAt := time.Unix(42, 123456789).UTC()
	batch := &clientui.QuestionBatch{ToolCallIDs: []string{"question", "access", "next"}, UnmaterializedCount: 1}
	source := workflowViewPendingPromptSource{prompts: taskPromptSnapshots{
		{
			Request: tools.AskQuestionRequest{
				ToolCallID: "question", StepID: stepID, Question: "Choose?",
				Suggestions: []string{"First", "Second"},
			},
			CreatedAt: createdAt,
			Batch:     batch,
		},
		{
			Request: tools.AskQuestionRequest{
				ToolCallID: "access", StepID: stepID, Approval: true,
				ApprovalOptions: []tools.AskQuestionApprovalOption{
					{Decision: tools.AskQuestionApprovalDecisionDeny},
					{Decision: tools.AskQuestionApprovalDecisionAllowOnce},
				},
				AccessTargets: []clientui.FileAccessTarget{
					{RequestedPath: "../outside.txt", ResolvedPath: "/outside.txt"},
					{RequestedPath: "/outside.txt", ResolvedPath: "/outside.txt"},
				},
			},
			CreatedAt: createdAt,
			Batch:     batch,
		},
	}}
	prompts, err := source.ListPendingPrompts(sessionID.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 {
		t.Fatalf("pending prompts = %d", len(prompts))
	}
	for _, prompt := range prompts {
		if err := protoapi.Validate(prompt); err != nil {
			t.Fatalf("invalid Task prompt: %v", err)
		}
	}
	ask := prompts[0].GetAsk()
	if ask == nil || ask.ToolCallId != "question" || ask.StepId != stepID || ask.SessionId != sessionID.String() ||
		ask.RecommendedOptionIndex != nil || !slices.Equal(ask.Suggestions, []string{"First", "Second"}) ||
		!ask.CreatedAt.AsTime().Equal(createdAt) || ask.Batch == nil ||
		ask.Batch.UnmaterializedCount != 1 || !slices.Equal(ask.Batch.ToolCallIds, batch.ToolCallIDs) {
		t.Fatalf("Question lost answer metadata: %+v", ask)
	}
	approval := prompts[1].GetApproval()
	if approval == nil || approval.ToolCallId != "access" || approval.StepId != stepID ||
		approval.SessionId != sessionID.String() || approval.Question != nil ||
		!approval.CreatedAt.AsTime().Equal(createdAt) ||
		approval.Batch == nil || approval.Batch.UnmaterializedCount != 1 ||
		!slices.Equal(approval.Batch.ToolCallIds, batch.ToolCallIDs) {
		t.Fatalf("Approval lost answer metadata: %+v", approval)
	}
	native, err := protoapi.PendingApprovalFromApproval(approval)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(native.AccessTargets, source.prompts.(taskPromptSnapshots)[1].Request.AccessTargets) ||
		len(native.Options) != 2 || native.Options[0].Decision != clientui.ApprovalDecisionDeny ||
		native.Options[1].Decision != clientui.ApprovalDecisionAllowOnce {
		t.Fatalf("Approval lost access targets or ordered decisions: %+v", native)
	}
}

func TestTaskPromptProjectionRejectsInvalidAnswerIdentity(t *testing.T) {
	for _, approval := range []bool{false, true} {
		source := workflowViewPendingPromptSource{prompts: taskPromptSnapshots{{
			Request: tools.AskQuestionRequest{ToolCallID: "prompt", StepID: "invalid", Approval: approval},
		}}}
		if _, err := source.ListPendingPrompts(runtimeids.NewSessionID().String()); err == nil {
			t.Fatalf("invalid Step identity accepted for approval=%t", approval)
		}
	}
}
