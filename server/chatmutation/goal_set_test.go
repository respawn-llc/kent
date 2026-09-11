package chatmutation

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/sessionruntime"
	"core/shared/clientui"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestServiceSetGoalRejectsNewChatExecutionIdentityBeforeResolution(t *testing.T) {
	resolver := &goalSetCountingResolver{}
	service := NewService(nil, resolver, nil, nil, nil)
	runID := "run-id"
	request := &runtimepb.GoalSetRequest{
		Target: &chatpb.ChatTarget{
			Target: &chatpb.ChatTarget_NewChat{NewChat: validNewChatTarget()},
		},
		Objective:       "ship the feature",
		Actor:           "user",
		ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
		RunId:           &runID,
	}

	_, err := service.SetGoal(context.Background(), request)
	if err == nil {
		t.Fatal("SetGoal succeeded for an invalid New Chat execution identity")
	}
	if resolver.calls != 0 {
		t.Fatalf("target resolver calls = %d, want zero", resolver.calls)
	}
}

func TestGoalSetRequestSchemaValidatesTargetPolicyMatrix(t *testing.T) {
	sessionID := runtimeids.NewSessionID().String()
	runID, stepID := "run-id", "step-id"
	initialDraft := "composer draft"
	tests := []struct {
		name    string
		request *runtimepb.GoalSetRequest
		wantErr bool
	}{
		{
			name: "New Chat user starts or continues",
			request: newGoalSetRequest(&chatpb.ChatTarget{
				Target: &chatpb.ChatTarget_NewChat{NewChat: validNewChatTarget()},
			}, runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE),
		},
		{
			name: "New Chat rejects preserve runtime state",
			request: newGoalSetRequest(&chatpb.ChatTarget{
				Target: &chatpb.ChatTarget_NewChat{NewChat: validNewChatTarget()},
			}, runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE),
			wantErr: true,
		},
		{
			name: "New Chat rejects agent actor",
			request: func() *runtimepb.GoalSetRequest {
				request := newGoalSetRequest(&chatpb.ChatTarget{
					Target: &chatpb.ChatTarget_NewChat{NewChat: validNewChatTarget()},
				}, runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE)
				request.Actor = "agent"
				return request
			}(),
			wantErr: true,
		},
		{
			name: "exact Session user starts or continues",
			request: newGoalSetRequest(&chatpb.ChatTarget{
				Target: &chatpb.ChatTarget_Session{
					Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
				},
			}, runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE),
		},
		{
			name: "exact Session user preserves runtime state",
			request: newGoalSetRequest(&chatpb.ChatTarget{
				Target: &chatpb.ChatTarget_Session{
					Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
				},
			}, runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE),
		},
		{
			name: "exact Session agent preserves runtime state with Step",
			request: func() *runtimepb.GoalSetRequest {
				request := newGoalSetRequest(&chatpb.ChatTarget{
					Target: &chatpb.ChatTarget_Session{
						Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
					},
				}, runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE)
				request.Actor, request.RunId, request.StepId = "agent", &runID, &stepID
				return request
			}(),
		},
		{
			name: "exact Session agent requires Run and Step",
			request: func() *runtimepb.GoalSetRequest {
				request := newGoalSetRequest(&chatpb.ChatTarget{
					Target: &chatpb.ChatTarget_Session{
						Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
					},
				}, runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE)
				request.Actor = "agent"
				return request
			}(),
			wantErr: true,
		},
		{
			name: "exact Session start or continue rejects execution identity",
			request: func() *runtimepb.GoalSetRequest {
				request := newGoalSetRequest(&chatpb.ChatTarget{
					Target: &chatpb.ChatTarget_Session{
						Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
					},
				}, runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE)
				request.RunId = &runID
				return request
			}(),
			wantErr: true,
		},
		{
			name: "exact Session rejects creation draft",
			request: func() *runtimepb.GoalSetRequest {
				request := newGoalSetRequest(&chatpb.ChatTarget{
					Target: &chatpb.ChatTarget_Session{
						Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
					},
				}, runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE)
				request.InitialInputDraft = &initialDraft
				return request
			}(),
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := protoapi.Validate(test.request)
			if (err != nil) != test.wantErr {
				t.Fatalf("Goal Set schema validation error = %v, want error %t", err, test.wantErr)
			}
		})
	}
}

func newGoalSetRequest(
	target *chatpb.ChatTarget,
	policy runtimepb.GoalExecutionPolicy,
) *runtimepb.GoalSetRequest {
	return &runtimepb.GoalSetRequest{
		Target:          target,
		Objective:       "ship the feature",
		Actor:           "user",
		ExecutionPolicy: policy,
	}
}

func TestServiceSetGoalDeliversResolvedNewChatSessionWithAuthoritativeGoal(t *testing.T) {
	owner := newTestOperationOwner()
	t.Cleanup(func() { _ = owner.Close() })
	sessionID := runtimeids.NewSessionID()
	draft := "continue from the composer"
	attachment := &goalSetTestAttachment{
		sessionID: sessionID,
		released:  make(chan sessionruntime.RuntimeReleasePolicy, 1),
	}
	goals := &goalSetTestGoalService{
		response: serverapi.RuntimeGoalMutationResponse{
			Result: clientui.GoalMutationResult{
				Kind: clientui.GoalMutationResultAuthoritativeGoal,
				Goal: &clientui.Goal{
					ID:        "goal-1",
					Objective: "ship the feature",
					Status:    clientui.RuntimeGoalStatusActive,
					CreatedAt: time.Unix(10, 0),
					UpdatedAt: time.Unix(10, 0),
				},
			},
		},
	}
	service := NewService(
		owner,
		goalSetTestResolver{target: ResolvedTarget{SessionID: sessionID, Created: true}},
		goalSetTestRuntime{attachment: attachment},
		nil,
		goals,
	)
	success, err := service.SetGoal(context.Background(), &runtimepb.GoalSetRequest{
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
	if success.GetSession().GetSessionId() != sessionID.String() {
		t.Fatalf("resolved Session = %q, want %q", success.GetSession().GetSessionId(), sessionID)
	}
	if success.GetMutation().GetGoal().GetObjective() != "ship the feature" {
		t.Fatalf("Goal result = %+v, want authoritative objective", success.GetMutation())
	}
	if goals.request.SessionID != sessionID.String() ||
		goals.request.ExecutionPolicy != serverapi.RuntimeGoalExecutionPolicyStartOrContinue {
		t.Fatalf("Goal request = %+v", goals.request)
	}
	if got := <-attachment.released; got != sessionruntime.RuntimeReleaseDetach {
		t.Fatalf("attachment release policy = %v, want detach", got)
	}
}

func TestServiceSetGoalCallerDisconnectDoesNotCancelServerOperation(t *testing.T) {
	owner := newTestOperationOwner()
	t.Cleanup(func() { _ = owner.Close() })
	sessionID := runtimeids.NewSessionID()
	attachment := &goalSetTestAttachment{
		sessionID: sessionID,
		released:  make(chan sessionruntime.RuntimeReleasePolicy, 1),
	}
	goals := &goalSetBlockingService{
		started:  make(chan context.Context, 1),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
		response: serverapi.RuntimeGoalMutationResponse{
			Result: clientui.GoalMutationResult{
				Kind: clientui.GoalMutationResultAuthoritativeGoal,
				Goal: &clientui.Goal{
					ID:        "goal-1",
					Objective: "ship the feature",
					Status:    clientui.RuntimeGoalStatusActive,
					CreatedAt: time.Unix(10, 0),
					UpdatedAt: time.Unix(10, 0),
				},
			},
		},
	}
	service := NewService(
		owner,
		goalSetTestResolver{target: ResolvedTarget{SessionID: sessionID}},
		goalSetTestRuntime{attachment: attachment},
		nil,
		goals,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := service.SetGoal(ctx, &runtimepb.GoalSetRequest{
			Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
			}},
			Objective:       "ship the feature",
			Actor:           "user",
			ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
		})
		result <- err
	}()
	operationContext := <-goals.started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller result = %v, want context cancellation", err)
	}
	select {
	case <-operationContext.Done():
		t.Fatal("caller disconnect canceled server-owned Goal operation")
	default:
	}
	close(goals.release)
	select {
	case <-goals.finished:
	case <-time.After(time.Second):
		t.Fatal("server-owned Goal operation did not finish")
	}
	if got := <-attachment.released; got != sessionruntime.RuntimeReleaseDetach {
		t.Fatalf("attachment release policy = %v, want detach", got)
	}
}

func TestServiceSetGoalPreservesDormantRuntimeForPreservePolicy(t *testing.T) {
	owner := newTestOperationOwner()
	t.Cleanup(func() { _ = owner.Close() })
	sessionID := runtimeids.NewSessionID()
	goals := &goalSetTestGoalService{
		response: serverapi.RuntimeGoalMutationResponse{
			Result: clientui.GoalMutationResult{
				Kind: clientui.GoalMutationResultAuthoritativeGoal,
				Goal: &clientui.Goal{
					ID:        "goal-1",
					Objective: "ship the feature",
					Status:    clientui.RuntimeGoalStatusActive,
					CreatedAt: time.Unix(10, 0),
					UpdatedAt: time.Unix(10, 0),
				},
			},
		},
	}
	runtimes := &goalSetFailingRuntime{err: errors.New("dormant Session must not open Runtime")}
	service := NewService(
		owner,
		goalSetTestResolver{target: ResolvedTarget{SessionID: sessionID}},
		runtimes,
		nil,
		goals,
	)
	success, err := service.SetGoal(context.Background(), &runtimepb.GoalSetRequest{
		Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
			Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
		}},
		Objective:       "ship the feature",
		Actor:           "user",
		ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE,
	})
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if success.GetSession().GetSessionId() != sessionID.String() {
		t.Fatalf("resolved Session = %q, want %q", success.GetSession().GetSessionId(), sessionID)
	}
	if success.GetMutation().GetGoal().GetObjective() != "ship the feature" {
		t.Fatalf("Goal result = %+v, want authoritative objective", success.GetMutation())
	}
	if runtimes.calls != 0 {
		t.Fatalf("Runtime opening calls = %d, want zero", runtimes.calls)
	}
}

type goalSetTestResolver struct {
	target ResolvedTarget
}

func (r goalSetTestResolver) Resolve(context.Context, TargetResolutionRequest) (ResolvedTarget, error) {
	return r.target, nil
}

type goalSetCountingResolver struct {
	calls int
}

func (r *goalSetCountingResolver) Resolve(context.Context, TargetResolutionRequest) (ResolvedTarget, error) {
	r.calls++
	return ResolvedTarget{}, nil
}

type goalSetTestRuntime struct {
	attachment RuntimeAttachment
}

func (r goalSetTestRuntime) Open(context.Context, runtimeids.SessionID) (RuntimeAttachment, error) {
	return r.attachment, nil
}

type goalSetFailingRuntime struct {
	err   error
	calls int
}

func (r *goalSetFailingRuntime) Open(context.Context, runtimeids.SessionID) (RuntimeAttachment, error) {
	r.calls++
	return nil, r.err
}

type goalSetTestAttachment struct {
	sessionID runtimeids.SessionID
	released  chan sessionruntime.RuntimeReleasePolicy
}

func (a *goalSetTestAttachment) SessionID() runtimeids.SessionID {
	return a.sessionID
}

func (a *goalSetTestAttachment) Release(
	_ context.Context,
	policy sessionruntime.RuntimeReleasePolicy,
) error {
	a.released <- policy
	return nil
}

type goalSetTestGoalService struct {
	request  serverapi.RuntimeGoalSetRequest
	response serverapi.RuntimeGoalMutationResponse
}

type goalSetBlockingService struct {
	started  chan context.Context
	release  chan struct{}
	finished chan struct{}
	response serverapi.RuntimeGoalMutationResponse
}

func (s *goalSetBlockingService) SetGoal(
	ctx context.Context,
	_ serverapi.RuntimeGoalSetRequest,
) (serverapi.RuntimeGoalMutationResponse, error) {
	s.started <- ctx
	<-s.release
	close(s.finished)
	return s.response, nil
}

func (s *goalSetTestGoalService) SetGoal(
	_ context.Context,
	request serverapi.RuntimeGoalSetRequest,
) (serverapi.RuntimeGoalMutationResponse, error) {
	s.request = request
	return s.response, nil
}
