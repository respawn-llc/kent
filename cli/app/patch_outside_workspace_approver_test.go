package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"builder/server/runtimewire"
	"builder/server/tools/askquestion"
	patchtool "builder/server/tools/patch"
)

const (
	outsideWorkspaceAllowOnceSuggestion    = runtimewire.OutsideWorkspaceAllowOnceSuggestion
	outsideWorkspaceAllowSessionSuggestion = runtimewire.OutsideWorkspaceAllowSessionSuggestion
	outsideWorkspaceDenySuggestion         = runtimewire.OutsideWorkspaceDenySuggestion
)

type outsideWorkspaceApprover = runtimewire.OutsideWorkspaceApprover

func newOutsideWorkspaceApprover(broker *askquestion.Broker, actionVerb string) *outsideWorkspaceApprover {
	return runtimewire.NewOutsideWorkspaceApprover(broker, actionVerb)
}

func outsideWorkspaceApprovalFromResponse(resp askquestion.Response) (patchtool.OutsideWorkspaceApproval, error) {
	return runtimewire.OutsideWorkspaceApprovalFromResponse(resp)
}

func TestOutsideWorkspaceApprovalFromResponse(t *testing.T) {
	tests := []struct {
		name string
		resp askquestion.Response
		want patchtool.OutsideWorkspaceApproval
	}{
		{name: "allow once", resp: askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionAllowOnce}}, want: patchtool.OutsideWorkspaceApproval{Decision: patchtool.OutsideWorkspaceDecisionAllowOnce}},
		{name: "allow session", resp: askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionAllowSession}}, want: patchtool.OutsideWorkspaceApproval{Decision: patchtool.OutsideWorkspaceDecisionAllowSession}},
		{name: "deny", resp: askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionDeny}}, want: patchtool.OutsideWorkspaceApproval{Decision: patchtool.OutsideWorkspaceDecisionDeny}},
		{name: "allow once with commentary", resp: askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionAllowOnce, Commentary: "approved, but keep it small"}}, want: patchtool.OutsideWorkspaceApproval{Decision: patchtool.OutsideWorkspaceDecisionAllowOnce, Commentary: "approved, but keep it small"}},
		{name: "deny with commentary", resp: askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionDeny, Commentary: "no because this is protected"}}, want: patchtool.OutsideWorkspaceApproval{Decision: patchtool.OutsideWorkspaceDecisionDeny, Commentary: "no because this is protected"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := outsideWorkspaceApprovalFromResponse(tc.resp)
			if err != nil {
				t.Fatalf("parse approval response: %v", err)
			}
			if got != tc.want {
				t.Fatalf("decision mismatch: got %v want %v", got, tc.want)
			}
		})
	}
}

func TestOutsideWorkspaceApprovalFromResponseRejectsMissingOrInvalidPayload(t *testing.T) {
	tests := []struct {
		name string
		resp askquestion.Response
	}{
		{name: "missing payload", resp: askquestion.Response{}},
		{name: "invalid decision", resp: askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: "maybe"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := outsideWorkspaceApprovalFromResponse(tc.resp); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPatchOutsideWorkspaceApproverCachesSessionDecision(t *testing.T) {
	broker := askquestion.NewBroker()
	askCalls := 0
	broker.SetAskHandler(func(req askquestion.Request) (askquestion.Response, error) {
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
		if req.ApprovalOptions[0].Label != "Allow once" || req.ApprovalOptions[1].Label != "Allow for this session" || req.ApprovalOptions[2].Label != "Deny" {
			t.Fatalf("expected fixed built-in approval labels, got %+v", req.ApprovalOptions)
		}
		if strings.Contains(req.Question, "workspace:") || strings.Contains(req.Question, "requested path:") || strings.Contains(req.Question, "Patch requested an edit outside the workspace.") {
			t.Fatalf("approval prompt contains removed fields: %q", req.Question)
		}
		if !strings.Contains(req.Question, "Allow editing /tmp/x.txt (outside workspace dir)?") {
			t.Fatalf("unexpected approval question text: %q", req.Question)
		}
		return askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionAllowSession}}, nil
	})

	approver := newOutsideWorkspaceApprover(broker, "editing")
	req := patchtool.OutsideWorkspaceRequest{RequestedPath: "../x.txt", ResolvedPath: "/tmp/x.txt", WorkspaceRoot: "/tmp/w"}

	first, err := approver.Approve(context.Background(), req)
	if err != nil {
		t.Fatalf("approve first call: %v", err)
	}
	if first.Decision != patchtool.OutsideWorkspaceDecisionAllowSession {
		t.Fatalf("unexpected first decision: %v", first)
	}
	second, err := approver.Approve(context.Background(), req)
	if err != nil {
		t.Fatalf("approve second call: %v", err)
	}
	if second.Decision != patchtool.OutsideWorkspaceDecisionAllowSession {
		t.Fatalf("unexpected second decision: %v", second)
	}
	if askCalls != 1 {
		t.Fatalf("expected one ask call, got %d", askCalls)
	}
}

func TestPatchOutsideWorkspaceApproverPropagatesAskError(t *testing.T) {
	broker := askquestion.NewBroker()
	broker.SetAskHandler(func(askquestion.Request) (askquestion.Response, error) {
		return askquestion.Response{}, errors.New("ask failed")
	})

	approver := newOutsideWorkspaceApprover(broker, "editing")
	_, err := approver.Approve(context.Background(), patchtool.OutsideWorkspaceRequest{RequestedPath: "../x.txt", ResolvedPath: "/tmp/x.txt", WorkspaceRoot: "/tmp/w"})
	if err == nil {
		t.Fatal("expected ask error")
	}
}

func TestOutsideWorkspaceApproverUsesReadPromptText(t *testing.T) {
	broker := askquestion.NewBroker()
	askCalls := 0
	broker.SetAskHandler(func(req askquestion.Request) (askquestion.Response, error) {
		askCalls++
		if !strings.Contains(req.Question, "Allow reading /tmp/x.pdf (outside workspace dir)?") {
			t.Fatalf("unexpected read approval question text: %q", req.Question)
		}
		return askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionAllowOnce}}, nil
	})

	approver := newOutsideWorkspaceApprover(broker, "reading")
	approval, err := approver.Approve(context.Background(), patchtool.OutsideWorkspaceRequest{RequestedPath: "../x.pdf", ResolvedPath: "/tmp/x.pdf", WorkspaceRoot: "/tmp/w"})
	if err != nil {
		t.Fatalf("approve read call: %v", err)
	}
	if approval.Decision != patchtool.OutsideWorkspaceDecisionAllowOnce {
		t.Fatalf("unexpected approval decision: %v", approval)
	}
	if askCalls != 1 {
		t.Fatalf("expected one ask call, got %d", askCalls)
	}
}

func TestOutsideWorkspaceApproverQueuedApprovalBlocksUntilSubmitted(t *testing.T) {
	broker := askquestion.NewBroker()
	approver := newOutsideWorkspaceApprover(broker, "editing")
	req := patchtool.OutsideWorkspaceRequest{RequestedPath: "../x.txt", ResolvedPath: "/tmp/x.txt", WorkspaceRoot: "/tmp/w"}
	type out struct {
		approval patchtool.OutsideWorkspaceApproval
		err      error
	}
	done := make(chan out, 1)

	go func() {
		approval, err := approver.Approve(context.Background(), req)
		done <- out{approval: approval, err: err}
	}()

	pending := waitForPendingApprovals(t, broker, 1)
	if len(pending) != 1 {
		t.Fatalf("expected one pending approval, got %+v", pending)
	}
	if !pending[0].Approval {
		t.Fatalf("expected queued request to be approval-backed, got %+v", pending[0])
	}
	if len(pending[0].Suggestions) != 0 {
		t.Fatalf("expected no suggestion list for approval request, got %+v", pending[0].Suggestions)
	}
	if len(pending[0].ApprovalOptions) != 3 {
		t.Fatalf("expected three approval options, got %+v", pending[0].ApprovalOptions)
	}
	if pending[0].ApprovalOptions[0].Decision != askquestion.ApprovalDecisionAllowOnce || pending[0].ApprovalOptions[1].Decision != askquestion.ApprovalDecisionAllowSession || pending[0].ApprovalOptions[2].Decision != askquestion.ApprovalDecisionDeny {
		t.Fatalf("unexpected approval options: %+v", pending[0].ApprovalOptions)
	}
	if !strings.Contains(pending[0].Question, "Allow editing /tmp/x.txt (outside workspace dir)?") {
		t.Fatalf("unexpected queued approval question: %q", pending[0].Question)
	}

	select {
	case result := <-done:
		t.Fatalf("approval returned before submission: %+v", result)
	default:
	}

	if err := broker.Submit(pending[0].ID, askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionDeny, Commentary: "no"}}); err != nil {
		t.Fatalf("submit denial: %v", err)
	}
	if err := broker.Submit(pending[0].ID, askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionAllowOnce}}); err == nil {
		t.Fatal("expected duplicate approval resolution to fail")
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("approve: %v", result.err)
		}
		if result.approval.Decision != patchtool.OutsideWorkspaceDecisionDeny {
			t.Fatalf("unexpected approval decision: %+v", result.approval)
		}
		if result.approval.Commentary != "no" {
			t.Fatalf("unexpected approval commentary: %+v", result.approval)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queued approval result")
	}

	if pending := broker.Pending(); len(pending) != 0 {
		t.Fatalf("expected pending approvals cleared after completion, got %+v", pending)
	}
}

func TestOutsideWorkspaceApproverQueuedAllowSessionCachesWithoutSecondPrompt(t *testing.T) {
	broker := askquestion.NewBroker()
	approver := newOutsideWorkspaceApprover(broker, "editing")
	req := patchtool.OutsideWorkspaceRequest{RequestedPath: "../x.txt", ResolvedPath: "/tmp/x.txt", WorkspaceRoot: "/tmp/w"}
	type out struct {
		approval patchtool.OutsideWorkspaceApproval
		err      error
	}
	done := make(chan out, 1)

	go func() {
		approval, err := approver.Approve(context.Background(), req)
		done <- out{approval: approval, err: err}
	}()

	pending := waitForPendingApprovals(t, broker, 1)
	if err := broker.Submit(pending[0].ID, askquestion.Response{Approval: &askquestion.ApprovalPayload{Decision: askquestion.ApprovalDecisionAllowSession}}); err != nil {
		t.Fatalf("submit allow-session approval: %v", err)
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("approve: %v", result.err)
		}
		if result.approval.Decision != patchtool.OutsideWorkspaceDecisionAllowSession {
			t.Fatalf("unexpected first approval decision: %+v", result.approval)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for allow-session approval")
	}

	secondDone := make(chan out, 1)
	go func() {
		approval, err := approver.Approve(context.Background(), req)
		secondDone <- out{approval: approval, err: err}
	}()

	select {
	case result := <-secondDone:
		if result.err != nil {
			t.Fatalf("second approve: %v", result.err)
		}
		if result.approval.Decision != patchtool.OutsideWorkspaceDecisionAllowSession {
			t.Fatalf("unexpected cached approval decision: %+v", result.approval)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected cached allow-session approval to return immediately")
	}

	if pending := broker.Pending(); len(pending) != 0 {
		t.Fatalf("expected no second queued approval after allow-session cache, got %+v", pending)
	}
}

func waitForPendingApprovals(t *testing.T, broker *askquestion.Broker, want int) []askquestion.Request {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending := broker.Pending()
		if len(pending) == want {
			return pending
		}
		time.Sleep(5 * time.Millisecond)
	}
	return broker.Pending()
}
