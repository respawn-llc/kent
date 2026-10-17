package chatmutation

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/promptcommands"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestServiceSetGoalNewChatDeliversResolvedSessionAndPreservesCapturedInputs(t *testing.T) {
	owner := newTestOperationOwner()
	t.Cleanup(func() { _ = owner.Close() })
	sessionID := runtimeids.NewSessionID()
	draft := "composer draft"
	resolver := &goalSetTestResolver{
		target: ResolvedTarget{SessionID: sessionID, Created: true},
	}
	invoker := &goalSetTestInvoker{response: committedGoalSetCommit("ship the feature")}
	service := NewService(owner, resolver, &goalSetTestRuntime{}, nil, invoker)

	result, err := service.SetGoal(context.Background(), &runtimepb.GoalSetRequest{
		Target: &chatpb.ChatTarget{
			Target: &chatpb.ChatTarget_NewChat{NewChat: validNewChatTarget()},
		},
		Objective:         "ship the feature",
		Actor:             "user",
		ExecutionPolicy:   runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
		InitialInputDraft: &draft,
	})
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if got := result.GetSession().GetSessionId(); got != sessionID.String() {
		t.Fatalf("resolved Session = %q, want %q", got, sessionID)
	}
	if got := result.GetMutation().GetGoal().GetObjective(); got != "ship the feature" {
		t.Fatalf("Goal objective = %q, want committed objective", got)
	}
	if resolver.request.InitialDraft == nil || *resolver.request.InitialDraft != draft {
		t.Fatalf("target resolver draft = %v, want %q", resolver.request.InitialDraft, draft)
	}
	if got := invoker.request.GetTarget().GetSession().GetSessionId().GetSessionId(); got != sessionID.String() {
		t.Fatalf("resolved invocation Session = %q, want %q", got, sessionID)
	}
	if invoker.request.GetExecutionPolicy() != runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE {
		t.Fatalf("resolved invocation policy = %v, want start_or_continue", invoker.request.GetExecutionPolicy())
	}
}

func TestServiceSetGoalAcceptsAgentExecutionIdentityByStepID(t *testing.T) {
	owner := newTestOperationOwner()
	t.Cleanup(func() { _ = owner.Close() })
	sessionID := runtimeids.NewSessionID()
	stepID := "11111111-1111-4111-8111-111111111111"
	invoker := &goalSetTestInvoker{response: committedGoalSetCommit("ship the feature")}
	service := NewService(
		owner,
		&goalSetTestResolver{target: ResolvedTarget{SessionID: sessionID}},
		&goalSetTestRuntime{},
		nil,
		invoker,
	)
	request := newExactGoalSetRequest(
		sessionID,
		runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE,
	)
	request.Actor = "agent"
	request.StepId = &stepID

	result, err := service.SetGoal(context.Background(), request)
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if result.GetMutation() == nil {
		t.Fatalf("result = %+v, want committed Goal mutation", result)
	}
	if invoker.request == nil || invoker.request.GetStepId() != stepID {
		t.Fatalf("resolved execution identity = %+v, want Step %q", invoker.request, stepID)
	}
}

func TestGoalSetProtocolPreservesStepIdentityRestrictions(t *testing.T) {
	stepID := "11111111-1111-4111-8111-111111111111"
	sessionID := runtimeids.NewSessionID()
	agentWithoutStep := newExactGoalSetRequest(
		sessionID,
		runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE,
	)
	agentWithoutStep.Actor = "agent"

	humanWithStep := newExactGoalSetRequest(
		sessionID,
		runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
	)
	humanWithStep.StepId = &stepID

	draft := "composer draft"
	newChatWithStep := &runtimepb.GoalSetRequest{
		Target: &chatpb.ChatTarget{
			Target: &chatpb.ChatTarget_NewChat{NewChat: validNewChatTarget()},
		},
		Objective:         "ship the feature",
		Actor:             "user",
		ExecutionPolicy:   runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
		InitialInputDraft: &draft,
		StepId:            &stepID,
	}

	for _, test := range []struct {
		name    string
		request *runtimepb.GoalSetRequest
	}{
		{name: "agent without execution Step", request: agentWithoutStep},
		{name: "human with execution Step", request: humanWithStep},
		{name: "new Chat with execution Step", request: newChatWithStep},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := protoapi.Validate(test.request); err == nil {
				t.Fatalf("invalid Goal Set request unexpectedly validated: %+v", test.request)
			}
		})
	}
}

func TestServiceSetGoalReturnsSessionBearingRejectionAfterCreatedSessionRuntimeFailure(t *testing.T) {
	owner := newTestOperationOwner()
	t.Cleanup(func() { _ = owner.Close() })
	sessionID := runtimeids.NewSessionID()
	runtimeErr := errors.New("runtime preparation failed")
	service := NewService(
		owner,
		&goalSetTestResolver{target: ResolvedTarget{SessionID: sessionID, Created: true}},
		&goalSetTestRuntime{err: runtimeErr},
		nil,
		&goalSetTestInvoker{},
	)

	result, err := service.SetGoal(context.Background(), newExactGoalSetRequest(
		sessionID,
		runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
	))
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if got := result.GetSession().GetSessionId(); got != sessionID.String() {
		t.Fatalf("rejection Session = %q, want %q", got, sessionID)
	}
	if result.GetRejected() == nil {
		t.Fatalf("result = %+v, want Session-bearing rejection", result)
	}
	if result.GetRejected().GetInternalFailure().GetCause() == "" {
		t.Fatalf("rejection = %+v, want failure evidence", result.GetRejected())
	}
}

func TestServiceSetGoalPreservesCommittedDiagnostic(t *testing.T) {
	owner := newTestOperationOwner()
	t.Cleanup(func() { _ = owner.Close() })
	sessionID := runtimeids.NewSessionID()
	invoker := &goalSetTestInvoker{
		response: serverapi.ResolvedGoalSetCommit{
			Mutation:   committedGoalSetCommit("ship the feature").Mutation,
			Diagnostic: errors.New("goal notice failed"),
		},
	}
	service := NewService(
		owner,
		&goalSetTestResolver{target: ResolvedTarget{SessionID: sessionID}},
		&goalSetTestRuntime{},
		nil,
		invoker,
	)

	result, err := service.SetGoal(context.Background(), newExactGoalSetRequest(
		sessionID,
		runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
	))
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if result.GetMutation().GetGoal().GetObjective() != "ship the feature" {
		t.Fatalf("Goal result = %+v, want committed Goal", result.GetMutation())
	}
	if result.GetDiagnostic() == nil || result.GetDiagnostic().Code != "internal_failure" {
		t.Fatalf("diagnostic = %+v, want one committed warning", result.GetDiagnostic())
	}
	if got := result.GetDiagnostic().GetInternalFailure().GetCause(); got == "" {
		t.Fatalf("diagnostic = %+v, want joined failure cause", result.GetDiagnostic())
	}
}

func TestServiceSetGoalPreserveRuntimeStateDoesNotOpenRuntime(t *testing.T) {
	owner := newTestOperationOwner()
	t.Cleanup(func() { _ = owner.Close() })
	sessionID := runtimeids.NewSessionID()
	runtimes := &goalSetTestRuntime{}
	invoker := &goalSetTestInvoker{response: committedGoalSetCommit("ship the feature")}
	service := NewService(
		owner,
		&goalSetTestResolver{target: ResolvedTarget{SessionID: sessionID}},
		runtimes,
		nil,
		invoker,
	)

	result, err := service.SetGoal(context.Background(), newExactGoalSetRequest(
		sessionID,
		runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE,
	))
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if result.GetMutation() == nil || result.GetMutation().GetGoal().GetObjective() != "ship the feature" {
		t.Fatalf("result = %+v, want committed Goal", result)
	}
	if runtimes.calls != 0 {
		t.Fatalf("Runtime opening calls = %d, want zero", runtimes.calls)
	}
}

func newExactGoalSetRequest(
	sessionID runtimeids.SessionID,
	policy runtimepb.GoalExecutionPolicy,
) *runtimepb.GoalSetRequest {
	return &runtimepb.GoalSetRequest{
		Target: &chatpb.ChatTarget{
			Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionRequestTarget{SessionId: protoapi.SessionSelectorToProto(sessionID)},
			},
		},
		Objective:       "ship the feature",
		Actor:           "user",
		ExecutionPolicy: policy,
	}
}

func committedGoalSetCommit(objective string) serverapi.ResolvedGoalSetCommit {
	now := timestamppb.New(time.Unix(10, 0))
	return serverapi.ResolvedGoalSetCommit{
		Mutation: &runtimepb.GoalMutationSuccess{
			Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL,
			Goal: &runtimepb.Goal{
				Id: "goal-1", Objective: objective,
				Status:    runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE,
				CreatedAt: now, UpdatedAt: now,
			},
		},
	}
}

type goalSetTestResolver struct {
	target  ResolvedTarget
	request TargetResolutionRequest
}

func (*goalSetTestResolver) SelectPlacement(_ context.Context, target ResolvedTarget, _ promptcommands.Placement) (ResolvedTarget, error) {
	return target, nil
}

func (r *goalSetTestResolver) Resolve(_ context.Context, request TargetResolutionRequest) (ResolvedTarget, error) {
	r.request = request
	return r.target, nil
}

type goalSetTestRuntime struct {
	err   error
	calls int
}

func (r *goalSetTestRuntime) Open(_ context.Context, _ runtimeids.SessionID, _ *serverapi.SessionRuntimePreparationOptions) error {
	r.calls++
	return r.err
}

type goalSetTestInvoker struct {
	request  *runtimepb.GoalSetRequest
	response serverapi.ResolvedGoalSetCommit
	err      error
}

func (i *goalSetTestInvoker) SetResolvedGoal(
	_ context.Context,
	request *runtimepb.GoalSetRequest,
) (serverapi.ResolvedGoalSetCommit, error) {
	i.request = request
	if i.err != nil {
		return serverapi.ResolvedGoalSetCommit{}, i.err
	}
	if i.response.Mutation == nil {
		i.response = committedGoalSetCommit(request.GetObjective())
	}
	return i.response, nil
}
