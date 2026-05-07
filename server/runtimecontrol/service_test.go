package runtimecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"builder/server/llm"
	"builder/server/primaryrun"
	"builder/server/runtime"
	"builder/server/session"
	"builder/server/tools"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
)

type stubRuntimeResolver struct {
	engine *runtime.Engine
}

func (s stubRuntimeResolver) ResolveRuntime(context.Context, string) (*runtime.Engine, error) {
	return s.engine, nil
}

type stubRuntimeLeaseVerifier struct {
	calls int
	err   error
}

func (s *stubRuntimeLeaseVerifier) RequireControllerLease(context.Context, string, string) error {
	s.calls++
	return s.err
}

type stubPrimaryRunGate struct {
	err      error
	acquire  int
	release  int
	sessions []string
}

func (g *stubPrimaryRunGate) AcquirePrimaryRun(sessionID string) (primaryrun.Lease, error) {
	g.acquire++
	g.sessions = append(g.sessions, sessionID)
	if g.err != nil {
		return nil, g.err
	}
	return primaryrun.LeaseFunc(func() { g.release++ }), nil
}

type runtimeControlFakeClient struct {
	mu                  sync.Mutex
	responses           []llm.Response
	compactionResponses []llm.CompactionResponse
	capabilities        llm.ProviderCapabilities
	calls               int
	compactionCalls     int
}

type blockingRuntimeControlClient struct {
	runtimeControlFakeClient
}

func (c *blockingRuntimeControlClient) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	<-ctx.Done()
	return llm.Response{}, ctx.Err()
}

type fakeShellHandler struct{}

func (fakeShellHandler) Name() toolspec.ID { return toolspec.ToolExecCommand }

func (fakeShellHandler) Call(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{Output: json.RawMessage(`{"output":"ok","exit_code":0,"truncated":false}`)}, nil
}

func (c *runtimeControlFakeClient) Generate(context.Context, llm.Request) (llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if len(c.responses) == 0 {
		return llm.Response{}, nil
	}
	resp := c.responses[0]
	c.responses = c.responses[1:]
	return resp, nil
}

func (c *runtimeControlFakeClient) Compact(context.Context, llm.CompactionRequest) (llm.CompactionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.compactionCalls++
	if len(c.compactionResponses) == 0 {
		return llm.CompactionResponse{}, nil
	}
	resp := c.compactionResponses[0]
	c.compactionResponses = c.compactionResponses[1:]
	return resp, nil
}

func (c *runtimeControlFakeClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return c.capabilities, nil
}

func TestServiceSubmitUserMessageReturnsTypedRuntimeUnavailable(t *testing.T) {
	service := NewService(stubRuntimeResolver{}, nil)
	req := serverapi.RuntimeSubmitUserMessageRequest{
		ClientRequestID:   "req-1",
		SessionID:         "session-1",
		ControllerLeaseID: "lease-1",
		Text:              "hello",
	}

	_, err := service.SubmitUserMessage(context.Background(), req)
	if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("SubmitUserMessage error = %v, want ErrRuntimeUnavailable", err)
	}
}

func TestServiceGoalCommandsDoNotRequireControllerLease(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5", EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{err: serverapi.ErrInvalidControllerLease}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).WithControllerLeaseVerifier(verifier)

	setResp, err := service.SetGoal(context.Background(), serverapi.RuntimeGoalSetRequest{
		ClientRequestID: "goal-set-1",
		SessionID:       store.Meta().SessionID,
		Objective:       "ship goal mode",
		Actor:           "user",
	})
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if setResp.Goal == nil || setResp.Goal.Objective != "ship goal mode" || setResp.Goal.Status != "active" {
		t.Fatalf("set goal response = %+v", setResp.Goal)
	}
	showResp, err := service.ShowGoal(context.Background(), serverapi.RuntimeGoalShowRequest{SessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("ShowGoal: %v", err)
	}
	if showResp.Goal == nil || showResp.Goal.ID != setResp.Goal.ID {
		t.Fatalf("show goal response = %+v, want id %q", showResp.Goal, setResp.Goal.ID)
	}
	completeResp, err := service.CompleteGoal(context.Background(), serverapi.RuntimeGoalStatusRequest{
		ClientRequestID: "goal-complete-1",
		SessionID:       store.Meta().SessionID,
		Actor:           "agent",
	})
	if err != nil {
		t.Fatalf("CompleteGoal: %v", err)
	}
	if completeResp.Goal == nil || completeResp.Goal.Status != "complete" {
		t.Fatalf("complete goal response = %+v", completeResp.Goal)
	}
	if verifier.calls != 0 {
		t.Fatalf("lease verifier calls = %d, want 0", verifier.calls)
	}
}

func TestServiceSetGoalMemoNormalizesObjectiveWhitespace(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &blockingRuntimeControlClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5", EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	defer func() { _ = engine.Close() }()
	service := NewService(stubRuntimeResolver{engine: engine}, nil)

	req := serverapi.RuntimeGoalSetRequest{
		ClientRequestID: "goal-set-retry",
		SessionID:       store.Meta().SessionID,
		Objective:       "  ship memo goal  ",
		Actor:           "user",
	}
	first, err := service.SetGoal(context.Background(), req)
	if err != nil {
		t.Fatalf("SetGoal first: %v", err)
	}
	req.Objective = "ship memo goal"
	second, err := service.SetGoal(context.Background(), req)
	if err != nil {
		t.Fatalf("SetGoal equivalent retry: %v", err)
	}
	if first.Goal == nil || second.Goal == nil || first.Goal.ID != second.Goal.ID {
		t.Fatalf("retry goal = %+v, want same id as %+v", second.Goal, first.Goal)
	}
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	goalSetEvents := 0
	for _, evt := range events {
		if evt.Kind == "goal_set" {
			goalSetEvents++
		}
	}
	if goalSetEvents != 1 {
		t.Fatalf("goal_set event count = %d, want 1", goalSetEvents)
	}
}

func TestServiceSetGoalPropagatesGoalLoopStartError(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)

	_, err = service.SetGoal(context.Background(), serverapi.RuntimeGoalSetRequest{
		ClientRequestID: "goal-set-ask-disabled",
		SessionID:       store.Meta().SessionID,
		Objective:       "ship goal mode",
		Actor:           "user",
	})
	if !errors.Is(err, runtime.ErrGoalRequiresAskQuestion) {
		t.Fatalf("SetGoal error = %v, want ErrGoalRequiresAskQuestion", err)
	}
	if goal := store.Meta().Goal; goal != nil {
		t.Fatalf("goal persisted after failed preflight: %+v", goal)
	}
	events, readErr := store.ReadEvents()
	if readErr != nil {
		t.Fatalf("ReadEvents: %v", readErr)
	}
	if len(events) != 0 {
		t.Fatalf("events persisted after failed preflight: %+v", events)
	}
}

func TestServiceActiveGoalUpdatesDoNotUsePrimaryRunGate(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, engine *runtime.Engine)
		call       func(context.Context, *Service, string) error
		assertGoal func(t *testing.T, goal *session.GoalState)
	}{
		{
			name: "set",
			call: func(ctx context.Context, service *Service, sessionID string) error {
				_, err := service.SetGoal(ctx, serverapi.RuntimeGoalSetRequest{
					ClientRequestID: "goal-set-busy",
					SessionID:       sessionID,
					Objective:       "ship goal mode",
					Actor:           "user",
				})
				return err
			},
			assertGoal: func(t *testing.T, goal *session.GoalState) {
				t.Helper()
				if goal == nil || goal.Status != session.GoalStatusActive || goal.Objective != "ship goal mode" {
					t.Fatalf("goal after busy set = %+v, want active ship goal mode", goal)
				}
			},
		},
		{
			name: "resume",
			prepare: func(t *testing.T, engine *runtime.Engine) {
				t.Helper()
				if _, err := engine.SetGoal("ship goal mode", session.GoalActorUser); err != nil {
					t.Fatalf("SetGoal: %v", err)
				}
				if _, err := engine.SetGoalStatus(session.GoalStatusPaused, session.GoalActorUser); err != nil {
					t.Fatalf("pause goal: %v", err)
				}
			},
			call: func(ctx context.Context, service *Service, sessionID string) error {
				_, err := service.ResumeGoal(ctx, serverapi.RuntimeGoalStatusRequest{
					ClientRequestID: "goal-resume-busy",
					SessionID:       sessionID,
					Actor:           "user",
				})
				return err
			},
			assertGoal: func(t *testing.T, goal *session.GoalState) {
				t.Helper()
				if goal == nil || goal.Status != session.GoalStatusActive {
					t.Fatalf("goal after busy resume = %+v, want active", goal)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
			if err != nil {
				t.Fatalf("create session store: %v", err)
			}
			engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5", EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})
			if err != nil {
				t.Fatalf("create runtime engine: %v", err)
			}
			defer func() { _ = engine.Close() }()
			if tt.prepare != nil {
				tt.prepare(t, engine)
			}
			before, err := store.ReadEvents()
			if err != nil {
				t.Fatalf("ReadEvents before: %v", err)
			}
			gate := &stubPrimaryRunGate{err: primaryrun.ErrActivePrimaryRun}
			service := NewService(stubRuntimeResolver{engine: engine}, gate)

			err = tt.call(context.Background(), service, store.Meta().SessionID)
			if err != nil {
				t.Fatalf("goal update while primary run active: %v", err)
			}
			tt.assertGoal(t, store.Meta().Goal)
			after, err := store.ReadEvents()
			if err != nil {
				t.Fatalf("ReadEvents after: %v", err)
			}
			if len(after) <= len(before) {
				t.Fatalf("events after goal update = %d, want > %d", len(after), len(before))
			}
			if gate.acquire != 0 || gate.release != 0 {
				t.Fatalf("gate acquire/release = %d/%d, want 0/0", gate.acquire, gate.release)
			}
		})
	}
}

func TestServiceShowGoalReportsRuntimeSuspension(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	if _, err := engine.SetGoal("ship goal mode", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if err := engine.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)

	resp, err := service.ShowGoal(context.Background(), serverapi.RuntimeGoalShowRequest{SessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("ShowGoal: %v", err)
	}
	if resp.Goal == nil || !resp.Goal.Suspended {
		t.Fatalf("goal response = %+v, want suspended", resp.Goal)
	}
}

func TestServiceCompleteGoalAlreadyCompleteDoesNotDuplicateAudit(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5", EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	if _, err := engine.SetGoal("ship goal mode", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if _, err := service.CompleteGoal(context.Background(), serverapi.RuntimeGoalStatusRequest{ClientRequestID: "complete-1", SessionID: store.Meta().SessionID, Actor: "agent"}); err != nil {
		t.Fatalf("CompleteGoal first: %v", err)
	}
	before, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents before: %v", err)
	}
	if _, err := service.CompleteGoal(context.Background(), serverapi.RuntimeGoalStatusRequest{ClientRequestID: "complete-2", SessionID: store.Meta().SessionID, Actor: "agent"}); err != nil {
		t.Fatalf("CompleteGoal second: %v", err)
	}
	after, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("events after duplicate complete = %d, want %d", len(after), len(before))
	}
}

func TestServiceSetSessionNameReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	if err := store.SetName("before"); err != nil {
		t.Fatalf("persist initial session name: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(fakeShellHandler{}), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).
		WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeSetSessionNameRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Name:              "after",
	}

	if err := service.SetSessionName(context.Background(), req); err != nil {
		t.Fatalf("SetSessionName first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	if err := service.SetSessionName(context.Background(), req); err != nil {
		t.Fatalf("SetSessionName replay: %v", err)
	}
	fresh := req
	fresh.ClientRequestID = "req-2"
	fresh.Name = "new name"
	if err := service.SetSessionName(context.Background(), fresh); !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("SetSessionName fresh request = %v, want ErrInvalidControllerLease", err)
	}
	if verifier.calls != 2 {
		t.Fatalf("lease verifier call count = %d, want 2", verifier.calls)
	}
	if got := store.Meta().Name; got != "after" {
		t.Fatalf("session name = %q, want after", got)
	}
	if reopened, err := session.Open(store.Dir()); err != nil {
		t.Fatalf("reopen session store: %v", err)
	} else if got := reopened.Meta().Name; got != "after" {
		t.Fatalf("reopened session name = %q, want after", got)
	}
}

func TestServiceSubmitUserMessageDedupesSuccessfulRetry(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	req := serverapi.RuntimeSubmitUserMessageRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              "hello",
	}

	first, err := service.SubmitUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitUserMessage first: %v", err)
	}
	second, err := service.SubmitUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitUserMessage retry: %v", err)
	}
	if first.Message != "done" || second.Message != "done" {
		t.Fatalf("responses = (%q, %q), want both done", first.Message, second.Message)
	}
	if client.calls != 1 {
		t.Fatalf("generate call count = %d, want 1", client.calls)
	}
}

func TestServiceSubmitUserMessageReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).
		WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeSubmitUserMessageRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              "hello",
	}

	first, err := service.SubmitUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitUserMessage first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	second, err := service.SubmitUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitUserMessage replay: %v", err)
	}
	if first.Message != "done" || second.Message != "done" {
		t.Fatalf("responses = (%q, %q), want both done", first.Message, second.Message)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
	if client.calls != 1 {
		t.Fatalf("generate call count = %d, want 1", client.calls)
	}
}

func TestServiceSubmitUserMessageRejectsClientRequestIDPayloadMismatch(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	first := serverapi.RuntimeSubmitUserMessageRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              "hello",
	}
	if _, err := service.SubmitUserMessage(context.Background(), first); err != nil {
		t.Fatalf("SubmitUserMessage first: %v", err)
	}
	second := first
	second.Text = "different"
	if _, err := service.SubmitUserMessage(context.Background(), second); err == nil || err.Error() != "client_request_id \"req-1\" was reused with different parameters" {
		t.Fatalf("SubmitUserMessage mismatch error = %v, want request id payload mismatch", err)
	}
	if client.calls != 1 {
		t.Fatalf("generate call count = %d, want 1", client.calls)
	}
}

func TestServiceSubmitUserTurnRecordsPromptHistoryAndSubmits(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)

	resp, err := service.SubmitUserTurn(context.Background(), serverapi.RuntimeSubmitUserTurnRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              "hello",
	})
	if err != nil {
		t.Fatalf("SubmitUserTurn: %v", err)
	}
	if resp.Message != "done" {
		t.Fatalf("message = %q, want done", resp.Message)
	}
	if client.calls != 1 {
		t.Fatalf("generate call count = %d, want 1", client.calls)
	}
	if got := countPromptHistoryEvents(t, store, "hello"); got != 1 {
		t.Fatalf("prompt history count = %d, want 1", got)
	}
}

func TestServiceSubmitUserTurnDedupesSuccessfulRetry(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	req := serverapi.RuntimeSubmitUserTurnRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              "hello",
	}

	first, err := service.SubmitUserTurn(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitUserTurn first: %v", err)
	}
	second, err := service.SubmitUserTurn(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitUserTurn retry: %v", err)
	}
	if first.Message != "done" || second.Message != "done" {
		t.Fatalf("responses = (%q, %q), want both done", first.Message, second.Message)
	}
	if client.calls != 1 {
		t.Fatalf("generate call count = %d, want 1", client.calls)
	}
	if got := countPromptHistoryEvents(t, store, "hello"); got != 1 {
		t.Fatalf("prompt history count = %d, want 1", got)
	}
}

func TestServiceSubmitUserShellCommandDedupesSuccessfulRetry(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(fakeShellHandler{}), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	req := serverapi.RuntimeSubmitUserShellCommandRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Command:           "pwd",
	}

	if err := service.SubmitUserShellCommand(context.Background(), req); err != nil {
		t.Fatalf("SubmitUserShellCommand first: %v", err)
	}
	afterFirst := countDirectShellCommandMessages(t, store, "pwd")
	if afterFirst != 1 {
		t.Fatalf("direct shell message count after first call = %d, want 1", afterFirst)
	}
	if err := service.SubmitUserShellCommand(context.Background(), req); err != nil {
		t.Fatalf("SubmitUserShellCommand replay: %v", err)
	}
	afterReplay := countDirectShellCommandMessages(t, store, "pwd")
	if afterReplay != 1 {
		t.Fatalf("direct shell message count after replay = %d, want 1", afterReplay)
	}
}

func TestServiceSubmitUserShellCommandReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(fakeShellHandler{}), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).
		WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeSubmitUserShellCommandRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Command:           "pwd",
	}

	if err := service.SubmitUserShellCommand(context.Background(), req); err != nil {
		t.Fatalf("SubmitUserShellCommand first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	if err := service.SubmitUserShellCommand(context.Background(), req); err != nil {
		t.Fatalf("SubmitUserShellCommand replay: %v", err)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
	if got := countDirectShellCommandMessages(t, store, "pwd"); got != 1 {
		t.Fatalf("direct shell message count = %d, want 1", got)
	}
}

func TestServiceSubmitUserShellCommandRejectsClientRequestIDPayloadMismatch(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	engine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(fakeShellHandler{}), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	first := serverapi.RuntimeSubmitUserShellCommandRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Command:           "pwd",
	}
	if err := service.SubmitUserShellCommand(context.Background(), first); err != nil {
		t.Fatalf("SubmitUserShellCommand first: %v", err)
	}
	second := first
	second.Command = "ls"
	if err := service.SubmitUserShellCommand(context.Background(), second); err == nil || err.Error() != "client_request_id \"req-1\" was reused with different parameters" {
		t.Fatalf("SubmitUserShellCommand mismatch error = %v, want request id payload mismatch", err)
	}
	if got := countDirectShellCommandMessages(t, store, "pwd"); got != 1 {
		t.Fatalf("direct shell message count = %d, want 1", got)
	}
}

func TestServiceQueueUserMessageDedupesSuccessfulRetry(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	req := serverapi.RuntimeQueueUserMessageRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              "hello",
	}

	if err := service.QueueUserMessage(context.Background(), req); err != nil {
		t.Fatalf("QueueUserMessage first: %v", err)
	}
	if err := service.QueueUserMessage(context.Background(), req); err != nil {
		t.Fatalf("QueueUserMessage replay: %v", err)
	}
	if _, err := engine.SubmitQueuedUserMessages(context.Background()); err != nil {
		t.Fatalf("SubmitQueuedUserMessages: %v", err)
	}
	if got := countUserMessagesWithContent(t, store, "hello"); got != 1 {
		t.Fatalf("queued user message count = %d, want 1", got)
	}
	if got := countUserMessagesWithContent(t, store, "hello\n\nhello"); got != 0 {
		t.Fatalf("duplicate queued flush count = %d, want 0", got)
	}
}

func TestServiceQueueUserMessageReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).
		WithControllerLeaseVerifier(verifier)
	req := serverapi.RuntimeQueueUserMessageRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              "hello",
	}

	if err := service.QueueUserMessage(context.Background(), req); err != nil {
		t.Fatalf("QueueUserMessage first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	if err := service.QueueUserMessage(context.Background(), req); err != nil {
		t.Fatalf("QueueUserMessage replay: %v", err)
	}
	if verifier.calls != 1 {
		t.Fatalf("lease verifier call count = %d, want 1", verifier.calls)
	}
	if _, err := engine.SubmitQueuedUserMessages(context.Background()); err != nil {
		t.Fatalf("SubmitQueuedUserMessages: %v", err)
	}
	if got := countUserMessagesWithContent(t, store, "hello"); got != 1 {
		t.Fatalf("queued user message count = %d, want 1", got)
	}
}

func TestServiceQueueUserMessageRejectsClientRequestIDPayloadMismatch(t *testing.T) {
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x")
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	client := &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	engine, err := runtime.New(store, client, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	service := NewService(stubRuntimeResolver{engine: engine}, nil)
	first := serverapi.RuntimeQueueUserMessageRequest{
		ClientRequestID:   "req-1",
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              "hello",
	}
	if err := service.QueueUserMessage(context.Background(), first); err != nil {
		t.Fatalf("QueueUserMessage first: %v", err)
	}
	second := first
	second.Text = "different"
	if err := service.QueueUserMessage(context.Background(), second); err == nil || err.Error() != "client_request_id \"req-1\" was reused with different parameters" {
		t.Fatalf("QueueUserMessage mismatch error = %v, want request id payload mismatch", err)
	}
	if _, err := engine.SubmitQueuedUserMessages(context.Background()); err != nil {
		t.Fatalf("SubmitQueuedUserMessages: %v", err)
	}
	if got := countUserMessagesWithContent(t, store, "hello"); got != 1 {
		t.Fatalf("queued user message count = %d, want 1", got)
	}
	if got := countUserMessagesWithContent(t, store, "different"); got != 0 {
		t.Fatalf("mismatched queued user message count = %d, want 0", got)
	}
	if got := countUserMessagesWithContent(t, store, "hello\n\ndifferent"); got != 0 {
		t.Fatalf("mixed queued flush count = %d, want 0", got)
	}
}

func countDirectShellCommandMessages(t *testing.T, store *session.Store, command string) int {
	t.Helper()
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	count := 0
	for _, evt := range events {
		if evt.Kind != "message" {
			continue
		}
		var msg llm.Message
		if err := json.Unmarshal(evt.Payload, &msg); err != nil {
			t.Fatalf("decode message event: %v", err)
		}
		if msg.Role == llm.RoleDeveloper && msg.Content == "User ran shell command directly:\n"+command {
			count++
		}
	}
	return count
}

func countUserMessagesWithContent(t *testing.T, store *session.Store, content string) int {
	t.Helper()
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	count := 0
	for _, evt := range events {
		if evt.Kind != "message" {
			continue
		}
		var msg llm.Message
		if err := json.Unmarshal(evt.Payload, &msg); err != nil {
			t.Fatalf("decode message event: %v", err)
		}
		if msg.Role == llm.RoleUser && msg.Content == content {
			count++
		}
	}
	return count
}
