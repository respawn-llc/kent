package runtimecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"core/server/llm"
	"core/server/metadata"
	"core/server/primaryrun"
	"core/server/requestmemo"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/toolspec"
)

type stubRuntimeResolver struct {
	engine *runtime.Engine
}

func (s stubRuntimeResolver) ResolveRuntime(context.Context, string) (*runtime.Engine, error) {
	return s.engine, nil
}

var runtimeControlPromptHistoryStores sync.Map

type runtimeControlPromptHistoryStore struct {
	mu         sync.Mutex
	records    []metadata.PromptHistoryRecord
	consumeErr error
}

func newRuntimeControlPromptHistoryStore(sessionID string) *runtimeControlPromptHistoryStore {
	store := &runtimeControlPromptHistoryStore{}
	runtimeControlPromptHistoryStores.Store(sessionID, store)
	return store
}

func (s *runtimeControlPromptHistoryStore) RecordPromptHistoryEntry(_ context.Context, entry metadata.PromptHistoryEntry) (metadata.PromptHistoryRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.records {
		if record.SessionID == entry.SessionID && record.Source == entry.Source && (record.SourceID == entry.SourceID || record.ClientRequestID == entry.ClientRequestID && entry.ClientRequestID != "") {
			if record.Text != entry.Text || record.SourceID != entry.SourceID && record.ClientRequestID == entry.ClientRequestID {
				return metadata.PromptHistoryRecord{}, false, metadata.ErrPromptHistoryConflict
			}
			return record, false, nil
		}
	}
	record := metadata.PromptHistoryRecord{
		Sequence:        int64(len(s.records) + 1),
		SessionID:       entry.SessionID,
		Source:          entry.Source,
		SourceID:        entry.SourceID,
		ClientRequestID: entry.ClientRequestID,
		QueueItemID:     entry.QueueItemID,
		QueueState:      entry.QueueState,
		Text:            entry.Text,
		CreatedAt:       entry.CreatedAt,
	}
	s.records = append(s.records, record)
	return record, true, nil
}

func (s *runtimeControlPromptHistoryStore) MarkPromptHistoryQueueState(_ context.Context, sessionID string, queueItemID string, state metadata.PromptHistoryQueueState) (metadata.PromptHistoryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, record := range s.records {
		if record.SessionID == sessionID && record.QueueItemID == queueItemID {
			s.records[i].QueueState = state
			return s.records[i], nil
		}
	}
	return metadata.PromptHistoryRecord{}, errors.New("queued prompt history not found")
}

func (s *runtimeControlPromptHistoryStore) MarkPromptHistoryQueueItemsConsumed(_ context.Context, sessionID string, queueItemIDs []string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumeErr != nil {
		return 0, s.consumeErr
	}
	ids := map[string]bool{}
	for _, raw := range queueItemIDs {
		id := strings.TrimSpace(raw)
		if id != "" {
			ids[id] = true
		}
	}
	var updated int64
	for i, record := range s.records {
		if record.SessionID == sessionID && ids[record.QueueItemID] && record.QueueState != metadata.PromptHistoryQueueStateDiscarded {
			s.records[i].QueueState = metadata.PromptHistoryQueueStateConsumed
			updated++
		}
	}
	return updated, nil
}

func (s *runtimeControlPromptHistoryStore) SetConsumeError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consumeErr = err
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

type cancelObservingRuntimeControlClient struct {
	started     chan struct{}
	release     chan struct{}
	ctxCanceled chan struct{}
}

func newCancelObservingRuntimeControlClient() *cancelObservingRuntimeControlClient {
	return &cancelObservingRuntimeControlClient{
		started:     make(chan struct{}),
		release:     make(chan struct{}),
		ctxCanceled: make(chan struct{}),
	}
}

func (c *cancelObservingRuntimeControlClient) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	_ = req
	select {
	case <-c.started:
	default:
		close(c.started)
	}
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			close(c.ctxCanceled)
		}()
	}
	<-c.release
	if err := ctx.Err(); err != nil {
		return llm.Response{}, err
	}
	return llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}, nil
}

func (c *cancelObservingRuntimeControlClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{}, nil
}

type fakeShellHandler struct{}

func (fakeShellHandler) Call(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{Output: json.RawMessage(`{"output":"ok","exit_code":0,"truncated":false}`)}, nil
}

func newRuntimeControlTestEngine(t *testing.T, client llm.Client, registry *tools.Registry, cfg runtime.Config, opts ...session.StoreOption) (*session.Store, *runtime.Engine) {
	t.Helper()
	store, err := session.Create(t.TempDir(), "workspace-x", "/tmp/workspace-x", opts...)
	if err != nil {
		t.Fatalf("create session store: %v", err)
	}
	if client == nil {
		client = &runtimeControlFakeClient{}
	}
	if registry == nil {
		registry = tools.NewRegistry()
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-5"
	}
	engine, err := runtime.New(store, client, registry, cfg)
	if err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return store, engine
}

func newRuntimeControlTestService(t *testing.T, client llm.Client, registry *tools.Registry, cfg runtime.Config, opts ...session.StoreOption) (*session.Store, *runtime.Engine, *Service) {
	t.Helper()
	store, engine := newRuntimeControlTestEngine(t, client, registry, cfg, opts...)
	history := newRuntimeControlPromptHistoryStore(store.Meta().SessionID)
	return store, engine, NewService(stubRuntimeResolver{engine: engine}, nil).WithPromptHistoryStore(history)
}

func finalResponseRuntimeControlClient() *runtimeControlFakeClient {
	return &runtimeControlFakeClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
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

func TestServiceSubmitUserMessageDetachesRunFromCallerCancellation(t *testing.T) {
	client := newCancelObservingRuntimeControlClient()
	store, _, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := service.SubmitUserMessage(ctx, runtimeControlUserMessageRequest(store, "req-detached", "hello"))
		done <- err
	}()

	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for submit to start")
	}
	cancel()
	select {
	case <-client.ctxCanceled:
		t.Fatal("runtime context was canceled by caller cancellation")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case err := <-done:
		t.Fatalf("submit returned before runtime was released: %v", err)
	default:
	}

	close(client.release)
	if err := <-done; err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
}

func TestServiceSubmitUserMessageStillCancelsOnExplicitInterrupt(t *testing.T) {
	client := newCancelObservingRuntimeControlClient()
	store, engine, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	done := make(chan error, 1)
	go func() {
		_, err := service.SubmitUserMessage(context.Background(), runtimeControlUserMessageRequest(store, "req-interrupt", "hello"))
		done <- err
	}()

	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for submit to start")
	}
	if err := engine.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	select {
	case <-client.ctxCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("runtime context was not canceled by explicit interrupt")
	}
	close(client.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("SubmitUserMessage error = %v, want context canceled", err)
	}
}

func TestServiceGoalCommandsDoNotRequireControllerLease(t *testing.T) {
	store, engine := newRuntimeControlTestEngine(t, nil, nil, runtime.Config{EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})
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
	store, _, service := newRuntimeControlTestService(t, &blockingRuntimeControlClient{}, nil, runtime.Config{EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})

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

func TestServiceSetGoalAllowsAgentWithoutExistingGoal(t *testing.T) {
	store, _, service := newRuntimeControlTestService(t, &blockingRuntimeControlClient{}, nil, runtime.Config{EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})

	resp, err := service.SetGoal(context.Background(), serverapi.RuntimeGoalSetRequest{
		ClientRequestID: "agent-goal-set",
		SessionID:       store.Meta().SessionID,
		Objective:       "agent self-goal",
		Actor:           "agent",
	})
	if err != nil {
		t.Fatalf("SetGoal agent: %v", err)
	}
	if resp.Goal == nil || resp.Goal.Objective != "agent self-goal" || resp.Goal.Status != "active" {
		t.Fatalf("agent set response = %+v", resp.Goal)
	}
}

func TestServiceSetGoalRejectsAgentOverwrite(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status session.GoalStatus
	}{
		{name: "active", status: session.GoalStatusActive},
		{name: "paused", status: session.GoalStatusPaused},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, engine, service := newRuntimeControlTestService(t, &blockingRuntimeControlClient{}, nil, runtime.Config{})
			if _, err := engine.SetGoal("existing goal\n\n- keep markdown", session.GoalActorUser); err != nil {
				t.Fatalf("SetGoal initial: %v", err)
			}
			if tt.status == session.GoalStatusPaused {
				if _, err := engine.SetGoalStatus(session.GoalStatusPaused, session.GoalActorUser); err != nil {
					t.Fatalf("SetGoalStatus paused: %v", err)
				}
			}

			_, err := service.SetGoal(context.Background(), serverapi.RuntimeGoalSetRequest{
				ClientRequestID: "agent-goal-overwrite-" + tt.name,
				SessionID:       store.Meta().SessionID,
				Objective:       "agent replacement",
				Actor:           "agent",
			})
			var denied goalAgentOverwriteDeniedError
			if !errors.As(err, &denied) {
				t.Fatalf("agent overwrite error = %v, want goalAgentOverwriteDeniedError", err)
			}
			if denied.Objective != "existing goal\n\n- keep markdown" {
				t.Fatalf("denied objective = %q, want existing goal text", denied.Objective)
			}
			if denied.Status != string(tt.status) {
				t.Fatalf("denied status = %q, want %q", denied.Status, string(tt.status))
			}
			if goal := store.Meta().Goal; goal == nil || goal.Objective != "existing goal\n\n- keep markdown" || goal.Status != tt.status {
				t.Fatalf("goal after rejected overwrite = %+v", goal)
			}
		})
	}
}

func TestServiceSetGoalAllowsAgentAfterCompletedGoal(t *testing.T) {
	store, engine, service := newRuntimeControlTestService(t, &blockingRuntimeControlClient{}, nil, runtime.Config{EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})
	completed, err := engine.SetGoal("completed goal", session.GoalActorUser)
	if err != nil {
		t.Fatalf("SetGoal initial: %v", err)
	}
	if _, err := engine.SetGoalStatus(session.GoalStatusComplete, session.GoalActorAgent); err != nil {
		t.Fatalf("SetGoalStatus complete: %v", err)
	}
	if goal := store.Meta().Goal; goal == nil || goal.ID != completed.ID || goal.Status != session.GoalStatusComplete {
		t.Fatalf("goal before follow-up set = %+v, want completed goal %q", goal, completed.ID)
	}

	resp, err := service.SetGoal(context.Background(), serverapi.RuntimeGoalSetRequest{
		ClientRequestID: "agent-goal-after-complete",
		SessionID:       store.Meta().SessionID,
		Objective:       "next goal",
		Actor:           "agent",
	})
	if err != nil {
		t.Fatalf("SetGoal after complete: %v", err)
	}
	if resp.Goal == nil || resp.Goal.Objective != "next goal" || resp.Goal.Status != "active" {
		t.Fatalf("set goal response = %+v", resp.Goal)
	}
	if resp.Goal.ID == completed.ID {
		t.Fatalf("next goal reused completed goal id %q", completed.ID)
	}
	if goal := store.Meta().Goal; goal == nil || goal.ID != resp.Goal.ID || goal.Objective != "next goal" || goal.Status != session.GoalStatusActive {
		t.Fatalf("persisted replacement goal = %+v, want response goal %+v", goal, resp.Goal)
	}
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	foundReplacement := false
	for _, event := range events {
		if event.Kind != "goal_set" {
			continue
		}
		var payload session.GoalSetEvent
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode goal_set event: %v", err)
		}
		if payload.Goal.ID == resp.Goal.ID {
			foundReplacement = true
			if payload.ReplacedGoalID != completed.ID {
				t.Fatalf("replacement replaced_goal_id = %q, want completed goal %q", payload.ReplacedGoalID, completed.ID)
			}
		}
	}
	if !foundReplacement {
		t.Fatalf("replacement goal_set event for goal %q not found in %+v", resp.Goal.ID, events)
	}
}

func TestServiceSetGoalPropagatesGoalLoopStartError(t *testing.T) {
	store, _, service := newRuntimeControlTestService(t, nil, nil, runtime.Config{})

	_, err := service.SetGoal(context.Background(), serverapi.RuntimeGoalSetRequest{
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
			store, engine := newRuntimeControlTestEngine(t, nil, nil, runtime.Config{EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})
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
	store, engine := newRuntimeControlTestEngine(t, nil, nil, runtime.Config{})
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
	store, engine, service := newRuntimeControlTestService(t, nil, nil, runtime.Config{EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})
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

func TestServiceCompleteGoalFeedbackIncludesCookDuration(t *testing.T) {
	now := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	store, engine, service := newRuntimeControlTestService(t, nil, nil, runtime.Config{EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}}, session.WithClock(func() time.Time {
		return now
	}))
	if _, err := engine.SetGoal("ship goal mode", session.GoalActorUser); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	now = now.Add(5*time.Hour + 32*time.Minute + 9*time.Second)

	if _, err := service.CompleteGoal(context.Background(), serverapi.RuntimeGoalStatusRequest{ClientRequestID: "complete-duration", SessionID: store.Meta().SessionID, Actor: "agent"}); err != nil {
		t.Fatalf("CompleteGoal: %v", err)
	}

	messages := runtimeControlGoalDeveloperMessages(t, store)
	if len(messages) != 2 {
		t.Fatalf("goal developer messages len = %d, want set+complete", len(messages))
	}
	if got, want := messages[1].CompactContent, "Goal complete. Cooked for 5h32m9s"; got != want {
		t.Fatalf("complete compact content = %q, want %q", got, want)
	}
}

func TestServiceSetSessionNameReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	store, engine := newRuntimeControlTestEngine(t, nil, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeShellHandler{}}), runtime.Config{})
	if err := store.SetName("before"); err != nil {
		t.Fatalf("persist initial session name: %v", err)
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
	client := finalResponseRuntimeControlClient()
	store, _, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	req := runtimeControlUserMessageRequest(store, "req-1", "hello")

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
	if got := countPromptHistoryEvents(t, store, "hello"); got != 1 {
		t.Fatalf("prompt history count = %d, want 1", got)
	}
}

func TestServiceSubmitUserMessageReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	client := finalResponseRuntimeControlClient()
	store, engine := newRuntimeControlTestEngine(t, client, nil, runtime.Config{})
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).
		WithControllerLeaseVerifier(verifier)
	req := runtimeControlUserMessageRequest(store, "req-1", "hello")

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
	client := finalResponseRuntimeControlClient()
	store, _, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	first := runtimeControlUserMessageRequest(store, "req-1", "hello")
	if _, err := service.SubmitUserMessage(context.Background(), first); err != nil {
		t.Fatalf("SubmitUserMessage first: %v", err)
	}
	second := first
	second.Text = "different"
	if _, err := service.SubmitUserMessage(context.Background(), second); !errors.Is(err, requestmemo.ErrClientRequestIDReused) {
		t.Fatalf("SubmitUserMessage mismatch error = %v, want request id payload mismatch", err)
	}
	if client.calls != 1 {
		t.Fatalf("generate call count = %d, want 1", client.calls)
	}
}

func TestServiceSubmitUserTurnRecordsPromptHistoryAndSubmits(t *testing.T) {
	client := finalResponseRuntimeControlClient()
	store, _, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})

	resp, err := service.SubmitUserTurn(context.Background(), runtimeControlUserTurnRequest(store, "req-1", "hello"))
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
	client := finalResponseRuntimeControlClient()
	store, _, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	req := runtimeControlUserTurnRequest(store, "req-1", "hello")

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
	store, _, service := newRuntimeControlTestService(t, nil, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeShellHandler{}}), runtime.Config{})
	req := runtimeControlShellCommandRequest(store, "req-1", "pwd")

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
	store, engine := newRuntimeControlTestEngine(t, nil, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeShellHandler{}}), runtime.Config{})
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).
		WithControllerLeaseVerifier(verifier)
	req := runtimeControlShellCommandRequest(store, "req-1", "pwd")

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
	store, _, service := newRuntimeControlTestService(t, nil, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: fakeShellHandler{}}), runtime.Config{})
	first := runtimeControlShellCommandRequest(store, "req-1", "pwd")
	if err := service.SubmitUserShellCommand(context.Background(), first); err != nil {
		t.Fatalf("SubmitUserShellCommand first: %v", err)
	}
	second := first
	second.Command = "ls"
	if err := service.SubmitUserShellCommand(context.Background(), second); !errors.Is(err, requestmemo.ErrClientRequestIDReused) {
		t.Fatalf("SubmitUserShellCommand mismatch error = %v, want request id payload mismatch", err)
	}
	if got := countDirectShellCommandMessages(t, store, "pwd"); got != 1 {
		t.Fatalf("direct shell message count = %d, want 1", got)
	}
}

func TestServiceQueueUserMessageDedupesSuccessfulRetry(t *testing.T) {
	client := finalResponseRuntimeControlClient()
	store, engine, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	req := runtimeControlQueueUserMessageRequest(store, "req-1", "hello")

	firstQueue, err := service.QueueUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("QueueUserMessage first: %v", err)
	}
	secondQueue, err := service.QueueUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("QueueUserMessage replay: %v", err)
	}
	if firstQueue.QueueItemID == "" || secondQueue.QueueItemID != firstQueue.QueueItemID {
		t.Fatalf("queue ids = (%q, %q), want stable non-empty id", firstQueue.QueueItemID, secondQueue.QueueItemID)
	}
	if firstQueue.QueueItemID != "req-1" {
		t.Fatalf("queue id = %q, want request-id-derived queue id", firstQueue.QueueItemID)
	}
	if got := countPromptHistoryEvents(t, store, "hello"); got != 1 {
		t.Fatalf("queued prompt history count = %d, want 1 immediately after queue acceptance", got)
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

func TestServiceQueueUserMessageReplaysPendingPromptAfterColdReopen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()
	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	meta, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	binding, err := meta.RegisterWorkspaceBinding(ctx, cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := config.ProjectSessionsRoot(cfg, binding.ProjectID)
	store, err := session.Create(containerDir, filepath.Base(containerDir), cfg.WorkspaceRoot, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	firstEngine, err := runtime.New(store, &runtimeControlFakeClient{}, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create first runtime engine: %v", err)
	}
	firstService := NewService(stubRuntimeResolver{engine: firstEngine}, nil).WithPromptHistoryStore(meta)
	req := runtimeControlQueueUserMessageRequest(store, "req-cold", "hello after reopen")

	firstQueue, err := firstService.QueueUserMessage(ctx, req)
	if err != nil {
		t.Fatalf("QueueUserMessage first: %v", err)
	}
	if err := firstEngine.Close(); err != nil {
		t.Fatalf("close first runtime engine: %v", err)
	}
	reopened, err := session.OpenByID(cfg.PersistenceRoot, store.Meta().SessionID, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.OpenByID: %v", err)
	}
	secondClient := finalResponseRuntimeControlClient()
	secondEngine, err := runtime.New(reopened, secondClient, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create second runtime engine: %v", err)
	}
	t.Cleanup(func() { _ = secondEngine.Close() })
	secondService := NewService(stubRuntimeResolver{engine: secondEngine}, nil).WithPromptHistoryStore(meta)

	secondQueue, err := secondService.QueueUserMessage(ctx, req)
	if err != nil {
		t.Fatalf("QueueUserMessage replay after reopen: %v", err)
	}
	if firstQueue.QueueItemID != "req-cold" || secondQueue.QueueItemID != firstQueue.QueueItemID {
		t.Fatalf("queue ids first=%q second=%q, want stable request-derived id", firstQueue.QueueItemID, secondQueue.QueueItemID)
	}
	if _, err := secondEngine.SubmitQueuedUserMessages(ctx); err != nil {
		t.Fatalf("SubmitQueuedUserMessages after reopen: %v", err)
	}
	if got := countUserMessagesWithContent(t, reopened, "hello after reopen"); got != 1 {
		t.Fatalf("queued user message count after reopen = %d, want 1", got)
	}
	history, err := meta.ReadPromptHistory(ctx, store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ReadPromptHistory: %v", err)
	}
	if len(history) != 1 || history[0] != "hello after reopen" {
		t.Fatalf("prompt history after reopen = %+v, want one queued entry", history)
	}
}

func TestServiceQueueUserMessageRepairsConsumedPromptAfterColdReopen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()
	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	meta, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	binding, err := meta.RegisterWorkspaceBinding(ctx, cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	containerDir := config.ProjectSessionsRoot(cfg, binding.ProjectID)
	store, err := session.Create(containerDir, filepath.Base(containerDir), cfg.WorkspaceRoot, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	firstClient := finalResponseRuntimeControlClient()
	firstEngine, err := runtime.New(store, firstClient, tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create first runtime engine: %v", err)
	}
	firstService := NewService(stubRuntimeResolver{engine: firstEngine}, nil).WithPromptHistoryStore(meta)
	req := runtimeControlQueueUserMessageRequest(store, "req-consumed", "hello consumed")

	firstQueue, err := firstService.QueueUserMessage(ctx, req)
	if err != nil {
		t.Fatalf("QueueUserMessage first: %v", err)
	}
	if _, err := firstEngine.SubmitQueuedUserMessages(ctx); err != nil {
		t.Fatalf("SubmitQueuedUserMessages first: %v", err)
	}
	if err := firstEngine.Close(); err != nil {
		t.Fatalf("close first runtime engine: %v", err)
	}
	reopened, err := session.OpenByID(cfg.PersistenceRoot, store.Meta().SessionID, meta.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.OpenByID: %v", err)
	}
	secondEngine, err := runtime.New(reopened, finalResponseRuntimeControlClient(), tools.NewRegistry(), runtime.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("create second runtime engine: %v", err)
	}
	t.Cleanup(func() { _ = secondEngine.Close() })
	secondService := NewService(stubRuntimeResolver{engine: secondEngine}, nil).WithPromptHistoryStore(meta)

	secondQueue, err := secondService.QueueUserMessage(ctx, req)
	if err != nil {
		t.Fatalf("QueueUserMessage replay after consumed flush: %v", err)
	}
	if secondQueue.QueueItemID != firstQueue.QueueItemID {
		t.Fatalf("queue ids first=%q second=%q, want stable id", firstQueue.QueueItemID, secondQueue.QueueItemID)
	}
	if secondEngine.HasQueuedUserWork() {
		t.Fatal("did not expect consumed queued prompt to be re-enqueued")
	}
	record, err := meta.MarkPromptHistoryQueueState(ctx, store.Meta().SessionID, firstQueue.QueueItemID, metadata.PromptHistoryQueueStatePending)
	if err != nil {
		t.Fatalf("read repaired queue state: %v", err)
	}
	if record.QueueState != metadata.PromptHistoryQueueStateConsumed {
		t.Fatalf("queue state = %q, want consumed", record.QueueState)
	}
	if _, err := secondEngine.SubmitQueuedUserMessages(ctx); err != nil {
		t.Fatalf("SubmitQueuedUserMessages after repair: %v", err)
	}
	if got := countUserMessagesWithContent(t, reopened, "hello consumed"); got != 1 {
		t.Fatalf("queued user message count after consumed replay = %d, want 1", got)
	}
	history, err := meta.ReadPromptHistory(ctx, store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ReadPromptHistory: %v", err)
	}
	if len(history) != 1 || history[0] != "hello consumed" {
		t.Fatalf("prompt history after consumed repair = %+v, want one queued entry", history)
	}
}

func TestServiceQueueUserMessageDoesNotReenqueueWhenConsumedRepairFails(t *testing.T) {
	ctx := context.Background()
	sessionStore, engine, firstService := newRuntimeControlTestService(t, finalResponseRuntimeControlClient(), nil, runtime.Config{})
	history := firstService.promptStore.(*runtimeControlPromptHistoryStore)
	req := serverapi.RuntimeQueueUserMessageRequest{
		ClientRequestID:   "req-consume-failure",
		SessionID:         sessionStore.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              "hello once",
	}
	if _, err := firstService.QueueUserMessage(ctx, req); err != nil {
		t.Fatalf("QueueUserMessage first: %v", err)
	}
	if _, err := engine.SubmitQueuedUserMessages(ctx); err != nil {
		t.Fatalf("SubmitQueuedUserMessages: %v", err)
	}
	boom := errors.New("consume repair failed")
	history.SetConsumeError(boom)
	retryService := NewService(stubRuntimeResolver{engine: engine}, nil).WithPromptHistoryStore(history)

	if _, err := retryService.QueueUserMessage(ctx, req); !errors.Is(err, boom) {
		t.Fatalf("QueueUserMessage repair error = %v, want %v", err, boom)
	}
	if engine.HasQueuedUserWork() {
		t.Fatal("did not expect consumed prompt to be re-enqueued after repair failure")
	}
}

func TestServiceQueueUserMessageReplaysSuccessfulRetryAfterLeaseInvalidation(t *testing.T) {
	client := finalResponseRuntimeControlClient()
	store, engine := newRuntimeControlTestEngine(t, client, nil, runtime.Config{})
	verifier := &stubRuntimeLeaseVerifier{}
	service := NewService(stubRuntimeResolver{engine: engine}, nil).
		WithControllerLeaseVerifier(verifier)
	req := runtimeControlQueueUserMessageRequest(store, "req-1", "hello")

	firstQueue, err := service.QueueUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("QueueUserMessage first: %v", err)
	}
	verifier.err = serverapi.ErrInvalidControllerLease
	secondQueue, err := service.QueueUserMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("QueueUserMessage replay: %v", err)
	}
	if firstQueue.QueueItemID == "" || secondQueue.QueueItemID != firstQueue.QueueItemID {
		t.Fatalf("queue ids = (%q, %q), want stable non-empty id", firstQueue.QueueItemID, secondQueue.QueueItemID)
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
	client := finalResponseRuntimeControlClient()
	store, engine, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	first := runtimeControlQueueUserMessageRequest(store, "req-1", "hello")
	if _, err := service.QueueUserMessage(context.Background(), first); err != nil {
		t.Fatalf("QueueUserMessage first: %v", err)
	}
	second := first
	second.Text = "different"
	if _, err := service.QueueUserMessage(context.Background(), second); !errors.Is(err, requestmemo.ErrClientRequestIDReused) {
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
		if msg.Role != llm.RoleAssistant {
			continue
		}
		for _, call := range msg.ToolCalls {
			if call.Name != string(toolspec.ToolExecCommand) {
				continue
			}
			var in struct {
				Cmd           string `json:"cmd"`
				UserInitiated bool   `json:"user_initiated"`
			}
			if err := json.Unmarshal(call.Input, &in); err != nil {
				continue
			}
			if in.UserInitiated && in.Cmd == command {
				count++
			}
		}
	}
	return count
}

func runtimeControlGoalDeveloperMessages(t *testing.T, store *session.Store) []llm.Message {
	t.Helper()
	events, err := store.ReadEvents()
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	out := []llm.Message{}
	for _, evt := range events {
		if evt.Kind != "message" {
			continue
		}
		var msg llm.Message
		if err := json.Unmarshal(evt.Payload, &msg); err != nil {
			t.Fatalf("decode message event: %v", err)
		}
		if msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeGoal {
			out = append(out, msg)
		}
	}
	return out
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

func runtimeControlUserMessageRequest(store *session.Store, requestID string, text string) serverapi.RuntimeSubmitUserMessageRequest {
	return serverapi.RuntimeSubmitUserMessageRequest{
		ClientRequestID:   requestID,
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              text,
	}
}

func runtimeControlUserTurnRequest(store *session.Store, requestID string, text string) serverapi.RuntimeSubmitUserTurnRequest {
	return serverapi.RuntimeSubmitUserTurnRequest{
		ClientRequestID:   requestID,
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              text,
	}
}

func runtimeControlShellCommandRequest(store *session.Store, requestID string, command string) serverapi.RuntimeSubmitUserShellCommandRequest {
	return serverapi.RuntimeSubmitUserShellCommandRequest{
		ClientRequestID:   requestID,
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Command:           command,
	}
}

func runtimeControlQueueUserMessageRequest(store *session.Store, requestID string, text string) serverapi.RuntimeQueueUserMessageRequest {
	return serverapi.RuntimeQueueUserMessageRequest{
		ClientRequestID:   requestID,
		SessionID:         store.Meta().SessionID,
		ControllerLeaseID: "lease-1",
		Text:              text,
	}
}
