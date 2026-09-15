package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/runtimewire"
	askquestion "core/server/tools"
	"core/shared/clientui"
	"core/shared/textutil"
)

func testOutsideWorkspaceApprovalResolution(
	decision askquestion.AskQuestionApprovalDecision,
	commentary *string,
) askquestion.AskQuestionApproval {
	return askquestion.AskQuestionApproval{
		Decision:   decision,
		Commentary: commentary,
	}
}

func testFileAccessApprovalRequest() askquestion.FileAccessApprovalRequest {
	return askquestion.FileAccessApprovalRequest{
		WorkingDirectory: "/tmp/w",
		Targets: []askquestion.FileAccessTarget{{
			RequestedPath: "../x.txt",
			ResolvedPath:  "/tmp/x.txt",
		}},
	}
}

func testFileAccessApprovalContext(toolCallID string) context.Context {
	ctx := askquestion.WithExecutionIdentity(context.Background(), askquestion.ExecutionIdentity{
		RunID:      "11111111-1111-4111-8111-111111111111",
		StepID:     "22222222-2222-4222-8222-222222222222",
		ToolCallID: clientui.ToolCallID(toolCallID),
	})
	return askquestion.WithApprovalLifecycle(ctx, askquestion.NewApprovalLifecycle())
}

func TestOutsideWorkspaceApprovalFromResolution(t *testing.T) {
	approvedCommentary := "approved, but keep it small"
	deniedCommentary := "no because this is protected"
	tests := []struct {
		name       string
		resolution askquestion.AskQuestionResolution
		want       askquestion.FileAccessApproval
	}{
		{name: "allow once", resolution: testOutsideWorkspaceApprovalResolution(askquestion.AskQuestionApprovalDecisionAllowOnce, nil), want: askquestion.FileAccessApproval{Kind: askquestion.FileAccessApprovalAllowOnce}},
		{name: "allow session", resolution: testOutsideWorkspaceApprovalResolution(askquestion.AskQuestionApprovalDecisionAllowSession, nil), want: askquestion.FileAccessApproval{Kind: askquestion.FileAccessApprovalAllowSession}},
		{name: "deny", resolution: testOutsideWorkspaceApprovalResolution(askquestion.AskQuestionApprovalDecisionDeny, nil), want: askquestion.FileAccessApproval{Kind: askquestion.FileAccessApprovalDeny}},
		{name: "allow once with commentary", resolution: testOutsideWorkspaceApprovalResolution(askquestion.AskQuestionApprovalDecisionAllowOnce, &approvedCommentary), want: askquestion.FileAccessApproval{Kind: askquestion.FileAccessApprovalAllowOnce, Commentary: &approvedCommentary}},
		{name: "deny with commentary", resolution: testOutsideWorkspaceApprovalResolution(askquestion.AskQuestionApprovalDecisionDeny, &deniedCommentary), want: askquestion.FileAccessApproval{Kind: askquestion.FileAccessApprovalDeny, Commentary: &deniedCommentary}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runtimewire.OutsideWorkspaceApprovalFromResolution(tc.resolution)
			if err != nil {
				t.Fatalf("parse approval response: %v", err)
			}
			if got.Kind != tc.want.Kind ||
				!textutil.EqualOptional(got.Commentary, tc.want.Commentary) {
				t.Fatalf("decision mismatch: got %v want %v", got, tc.want)
			}
		})
	}
}

func TestOutsideWorkspaceApprovalFromResolutionRejectsMissingOrInvalidPayload(t *testing.T) {
	blankCommentary := ""
	tests := []struct {
		name       string
		resolution askquestion.AskQuestionResolution
	}{
		{name: "missing payload"},
		{name: "invalid decision", resolution: askquestion.AskQuestionApproval{Decision: "maybe"}},
		{name: "blank commentary", resolution: askquestion.AskQuestionApproval{
			Decision:   askquestion.AskQuestionApprovalDecisionAllowOnce,
			Commentary: &blankCommentary,
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := runtimewire.OutsideWorkspaceApprovalFromResolution(tc.resolution); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestOutsideWorkspaceApproverCachesSessionDecision(t *testing.T) {
	broker := askquestion.NewAskQuestionBroker()
	askCalls := 0
	broker.SetAskHandler(func(_ context.Context, req askquestion.AskQuestionRequest) (askquestion.AskQuestionResolution, error) {
		askCalls++
		if !req.Approval {
			t.Fatalf("expected approval=true for outside-workspace ask")
		}
		if len(req.Suggestions) != 0 {
			t.Fatalf("expected structured approval options instead of suggestions, got %+v", req.Suggestions)
		}
		if len(req.ApprovalOptions) != 3 {
			t.Fatalf("expected 3 approval options, got %+v", req.ApprovalOptions)
		}
		return testOutsideWorkspaceApprovalResolution(askquestion.AskQuestionApprovalDecisionAllowSession, nil), nil
	})

	approver := runtimewire.NewOutsideWorkspaceApprover(broker)
	req := testFileAccessApprovalRequest()

	first, err := approver.Approve(testFileAccessApprovalContext("cache-session"), req)
	if err != nil {
		t.Fatalf("approve first call: %v", err)
	}
	if first.Kind != askquestion.FileAccessApprovalAllowSession {
		t.Fatalf("unexpected first decision: %v", first)
	}
	second, err := approver.Approve(context.Background(), req)
	if err != nil {
		t.Fatalf("approve second call: %v", err)
	}
	if second.Kind != askquestion.FileAccessApprovalSessionCached {
		t.Fatalf("unexpected second decision: %v", second)
	}
	if askCalls != 1 {
		t.Fatalf("expected one ask call, got %d", askCalls)
	}
}

func TestOutsideWorkspaceApproverPropagatesAskError(t *testing.T) {
	broker := askquestion.NewAskQuestionBroker()
	broker.SetAskHandler(func(context.Context, askquestion.AskQuestionRequest) (askquestion.AskQuestionResolution, error) {
		return nil, errors.New("ask failed")
	})

	approver := runtimewire.NewOutsideWorkspaceApprover(broker)
	_, err := approver.Approve(
		testFileAccessApprovalContext("propagate-error"),
		testFileAccessApprovalRequest(),
	)
	if err == nil {
		t.Fatal("expected ask error")
	}
}

func TestOutsideWorkspaceApproverWaitsForOwnerApproval(t *testing.T) {
	broker := askquestion.NewAskQuestionBroker()
	requests := make(chan askquestion.AskQuestionRequest, 1)
	answers := make(chan askquestion.AskQuestionResolution, 1)
	ctx, cancel := context.WithTimeout(testFileAccessApprovalContext("owner-deny"), 2*time.Second)
	defer cancel()
	broker.SetAskHandler(func(ctx context.Context, req askquestion.AskQuestionRequest) (askquestion.AskQuestionResolution, error) {
		requests <- req
		select {
		case answer := <-answers:
			return answer, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	approver := runtimewire.NewOutsideWorkspaceApprover(broker)
	req := testFileAccessApprovalRequest()
	type out struct {
		approval askquestion.FileAccessApproval
		err      error
	}
	done := make(chan out, 1)

	go func() {
		approval, err := approver.Approve(ctx, req)
		done <- out{approval: approval, err: err}
	}()

	var request askquestion.AskQuestionRequest
	select {
	case request = <-requests:
	case <-ctx.Done():
		t.Fatal("timed out waiting for owner approval request")
	}
	if !request.Approval {
		t.Fatalf("expected approval-backed request, got %+v", request)
	}
	if len(request.Suggestions) != 0 {
		t.Fatalf("expected no suggestion list for approval request, got %+v", request.Suggestions)
	}
	if len(request.ApprovalOptions) != 3 {
		t.Fatalf("expected three approval options, got %+v", request.ApprovalOptions)
	}
	if request.ApprovalOptions[0].Decision != askquestion.AskQuestionApprovalDecisionAllowOnce || request.ApprovalOptions[1].Decision != askquestion.AskQuestionApprovalDecisionAllowSession || request.ApprovalOptions[2].Decision != askquestion.AskQuestionApprovalDecisionDeny {
		t.Fatalf("unexpected approval options: %+v", request.ApprovalOptions)
	}
	select {
	case result := <-done:
		t.Fatalf("approval returned before submission: %+v", result)
	default:
	}

	answers <- testOutsideWorkspaceApprovalResolution(askquestion.AskQuestionApprovalDecisionDeny, textutil.Value("no"))

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("approve: %v", result.err)
		}
		if result.approval.Kind != askquestion.FileAccessApprovalDeny {
			t.Fatalf("unexpected approval decision: %+v", result.approval)
		}
		if result.approval.Commentary == nil || *result.approval.Commentary != "no" {
			t.Fatalf("unexpected approval commentary: %+v", result.approval)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for owner approval result")
	}
}
