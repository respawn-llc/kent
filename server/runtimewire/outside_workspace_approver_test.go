package runtimewire

import (
	"context"
	"errors"
	"reflect"
	"testing"

	askquestion "core/server/tools"
)

func TestOutsideWorkspaceApprovalDecisionAndCache(t *testing.T) {
	for _, lifecycle := range []bool{false, true} {
		name := "ordinary"
		if lifecycle {
			name = "lifecycle"
		}
		t.Run(name, func(t *testing.T) {
			for _, decision := range []struct {
				answer askquestion.AskQuestionApprovalDecision
				kind   askquestion.FileAccessApprovalKind
			}{
				{askquestion.AskQuestionApprovalDecisionAllowOnce, askquestion.FileAccessApprovalAllowOnce},
				{askquestion.AskQuestionApprovalDecisionAllowSession, askquestion.FileAccessApprovalAllowSession},
				{askquestion.AskQuestionApprovalDecisionDeny, askquestion.FileAccessApprovalDeny},
			} {
				t.Run(string(decision.answer), func(t *testing.T) {
					commentary := "approval commentary"
					answer := askquestion.AskQuestionApproval{Decision: decision.answer, Commentary: &commentary}
					calls := 0
					broker := askquestion.NewAskQuestionBroker()
					handler := func(_ context.Context, request askquestion.AskQuestionRequest) (askquestion.AskQuestionResolution, error) {
						calls++
						if lifecycle {
							return answer, request.AcceptApproval(answer)
						}
						return answer, nil
					}
					if lifecycle {
						broker.SetLifecycleAskHandler(handler)
					} else {
						broker.SetAskHandler(handler)
					}
					approver := NewOutsideWorkspaceApprover(broker)
					for call := 1; call <= 2; call++ {
						result, err := approver.Approve(outsideWorkspaceApprovalContext(), outsideWorkspaceApprovalRequest())
						want := askquestion.FileAccessApproval{Kind: decision.kind, Commentary: &commentary}
						wantCalls := call
						if call == 2 && decision.kind == askquestion.FileAccessApprovalAllowSession {
							want = askquestion.FileAccessApproval{Kind: askquestion.FileAccessApprovalSessionCached}
							wantCalls = 1
						}
						if err != nil || !reflect.DeepEqual(result, want) || calls != wantCalls {
							t.Fatalf("Approve #%d = %+v, %v; prompts = %d; want %+v, prompts = %d", call, result, err, calls, want, wantCalls)
						}
					}
				})
			}
		})
	}
}

func TestOutsideWorkspaceApprovalFailureDoesNotCache(t *testing.T) {
	cause := errors.New("approval handler failed")
	for _, failure := range []struct {
		name string
		err  error
	}{
		{name: "invalid decision"},
		{name: "handler failure", err: cause},
	} {
		t.Run(failure.name, func(t *testing.T) {
			broker := askquestion.NewAskQuestionBroker()
			calls := 0
			broker.SetAskHandler(func(context.Context, askquestion.AskQuestionRequest) (askquestion.AskQuestionResolution, error) {
				calls++
				return askquestion.AskQuestionApproval{Decision: "invalid"}, failure.err
			})
			approver := NewOutsideWorkspaceApprover(broker)
			for call := 1; call <= 2; call++ {
				result, err := approver.Approve(outsideWorkspaceApprovalContext(), outsideWorkspaceApprovalRequest())
				if err == nil || result.Kind != askquestion.FileAccessApprovalDeny || calls != call {
					t.Fatalf("Approve #%d = %+v, %v; prompts = %d", call, result, err, calls)
				}
				if failure.err != nil && !errors.Is(err, failure.err) {
					t.Fatalf("Approve error = %v, want %v", err, failure.err)
				}
			}
		})
	}
}

func outsideWorkspaceApprovalContext() context.Context {
	return askquestion.WithApprovalLifecycle(
		askquestion.WithExecutionIdentity(context.Background(), askquestion.ExecutionIdentity{
			RunID:      "11111111-1111-4111-8111-111111111111",
			StepID:     "22222222-2222-4222-8222-222222222222",
			ToolCallID: "call-edit",
		}),
		askquestion.NewApprovalLifecycle(),
	)
}

func outsideWorkspaceApprovalRequest() askquestion.FileAccessApprovalRequest {
	return askquestion.FileAccessApprovalRequest{Targets: []askquestion.FileAccessTarget{{
		RequestedPath: "/outside/file",
		ResolvedPath:  "/real/outside/file",
	}}}
}

func TestOutsideWorkspaceApprovalRetainsExecutingToolIdentity(t *testing.T) {
	broker := askquestion.NewAskQuestionBroker()
	var received askquestion.AskQuestionRequest
	broker.SetAskHandler(func(_ context.Context, request askquestion.AskQuestionRequest) (askquestion.AskQuestionResolution, error) {
		received = request
		return askquestion.AskQuestionApproval{Decision: askquestion.AskQuestionApprovalDecisionAllowOnce}, nil
	})
	approver := NewOutsideWorkspaceApprover(broker)
	targets := []askquestion.FileAccessTarget{{
		RequestedPath: "/outside/file",
		ResolvedPath:  "/real/outside/file",
	}}

	_, err := approver.Approve(
		askquestion.WithApprovalLifecycle(askquestion.WithExecutionIdentity(context.Background(), askquestion.ExecutionIdentity{
			RunID:      "11111111-1111-4111-8111-111111111111",
			StepID:     "22222222-2222-4222-8222-222222222222",
			ToolCallID: "call-edit",
		}), askquestion.NewApprovalLifecycle()),
		askquestion.FileAccessApprovalRequest{Targets: targets},
	)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if received.RunID != "11111111-1111-4111-8111-111111111111" ||
		received.StepID != "22222222-2222-4222-8222-222222222222" ||
		received.ToolCallID != "call-edit" {
		t.Fatalf("approval identity = run %q step %q tool %q", received.RunID, received.StepID, received.ToolCallID)
	}
	if !reflect.DeepEqual(received.AccessTargets, targets) {
		t.Fatalf("approval targets = %+v, want %+v", received.AccessTargets, targets)
	}
	if received.Question != "" {
		t.Fatalf("server materialized access Approval copy %q", received.Question)
	}
}
