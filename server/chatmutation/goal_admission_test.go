package chatmutation

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/agentruntime"
	"core/server/llm"
	"core/server/promptcontrol"
	"core/server/runtimecontrol"
	"core/server/runtimewire"
	"core/server/sessionruntime"
	"core/server/sessionstore"
	"core/server/tools"
	"core/shared/clientui"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type goalAdmissionClient struct {
	mu         sync.Mutex
	calls      int
	lastCtx    context.Context
	started    chan int
	first      chan struct{}
	advance    chan struct{}
	questionAt *int
	shutdown   chan struct{}
}

type goalAdmissionPrompts chan tools.AskQuestionRequest

func (p goalAdmissionPrompts) PromptPendingScope(_ sessionruntime.ExecutionScope, request tools.AskQuestionRequest, _ time.Time) error {
	p <- request
	return nil
}

func (p goalAdmissionPrompts) PromptResolvedScope(sessionruntime.ExecutionScope, string) error {
	return nil
}

func (c *goalAdmissionClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{}, nil
}

func (c *goalAdmissionClient) Generate(ctx context.Context, _ llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	c.mu.Lock()
	c.calls++
	c.lastCtx = ctx
	call := c.calls
	question := c.questionAt != nil && call == *c.questionAt
	c.mu.Unlock()
	c.started <- call
	if question {
		return llm.Response{
			Assistant: llm.Message{Role: llm.RoleAssistant},
			ToolCalls: []llm.ToolCall{{
				ID: "goal-question", Name: string(toolspec.ToolAskQuestion),
				Input: json.RawMessage(`{"question":"Continue?"}`),
			}},
		}, nil
	}
	release := c.advance
	if call == 1 {
		release = c.first
	}
	select {
	case <-c.shutdown:
		return llm.Response{}, context.Canceled
	case <-release:
		return llm.Response{Assistant: llm.Message{
			Role: llm.RoleAssistant, Content: textutil.Value("continue"),
			Phase: textutil.Value(llm.MessagePhaseFinal),
		}}, nil
	case <-ctx.Done():
		return llm.Response{}, context.Cause(ctx)
	}
}

func TestChatAdmissionAcknowledgesInputsDuringContinuingGoal(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		queue          bool
		question       bool
		rounds         int
		batch          int
		worktree       bool
		stopAtBoundary bool
	}{
		{name: "steer", rounds: 1, batch: 12},
		{name: "queue", queue: true, rounds: 1, batch: 12},
		{name: "steer during Question", question: true, rounds: 1, batch: 12},
		{name: "queue during Question", queue: true, question: true, rounds: 1, batch: 12},
		{name: "steer across 500 continuations", rounds: 500, batch: 1},
		{name: "steer while Worktree command waits", rounds: 1, batch: 12, worktree: true},
		{name: "queue while Worktree command waits", rounds: 1, batch: 12, worktree: true, queue: true},
		{name: "steer after execution canceled at Goal boundary", rounds: 1, batch: 12, stopAtBoundary: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg, metadata, store, _ := newPlacementFixture(t)
			cfg.Settings.Reviewer.Frequency = "off"
			model := &goalAdmissionClient{started: make(chan int, 10), first: make(chan struct{}), advance: make(chan struct{}), shutdown: make(chan struct{})}
			if scenario.question {
				model.questionAt = textutil.Value(2)
			}
			var authority *sessionruntime.Authority
			var stopAtBoundary sync.Once
			stoppedAtBoundary := make(chan struct{})
			prompts := make(goalAdmissionPrompts, 1)
			failures := make(chan string, 10)
			filesystem, err := runtimewire.NewFilesystemContext(cfg.WorkspaceRoot, cfg.WorkspaceRoot, "test")
			if err != nil {
				t.Fatal(err)
			}
			plan, err := sessionruntime.NewAgentRuntimePlan(sessionruntime.AgentRuntimePlanOptions{
				MainWorkspaceRoot: cfg.WorkspaceRoot, Settings: cfg.Settings,
				EnabledTools:      []toolspec.ID{toolspec.ToolAskQuestion},
				FilesystemContext: filesystem, Client: model,
				QuestionsEnabled: textutil.Value(true), AutoCompactionEnabled: textutil.Value(false),
				OnEvent: func(event runtime.Event) {
					if scenario.stopAtBoundary && event.RunState != nil && event.RunState.ActiveKind == runtime.ActiveKindGoalLoop && event.RunState.Status == runtime.RunStatusCompleted {
						stopAtBoundary.Do(func() {
							if execution, ok := authority.SessionExecution(store.Identity().ID); ok {
								execution.RequestStop()
								close(stoppedAtBoundary)
							}
						})
					}
					if event.Error != "" {
						failures <- event.Error
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			authority = sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
				PersistenceRoot:               cfg.PersistenceRoot,
				StoreOptions:                  metadata.AuthoritativeSessionStoreOptions(),
				PromptFeed:                    prompts,
				ProjectWorkspaceContextReader: testsetup.OpenSessionProjectWorkspaceContextReader(t, store),
			})
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := authority.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			id := store.Identity().ID
			if _, err := authority.OpenRuntime(t.Context(), sessionruntime.RuntimeOpenRequest{SessionID: id, Runtime: &plan}); err != nil {
				t.Fatal(err)
			}
			control := runtimecontrol.NewService(authority, metadata).WithPersistedSessionResolver(metadata)
			owner := NewOperationOwner()
			t.Cleanup(func() {
				if err := owner.Close(); err != nil {
					t.Error(err)
				}
			})
			t.Cleanup(func() { close(model.shutdown) })
			service := NewService(owner,
				NewTargetResolver(metadata, nil, nil, nil, metadata, nil),
				NewRuntimePlanner(authority, nil, nil), control, control)
			target := &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionRequestTarget{SessionId: protoapi.SessionSelectorToProto(id)},
			}}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			started, err := service.Steer(ctx, &chatpb.SteerRequest{
				Target: target, QueueItemId: runtimeids.NewQueueItemID().String(),
				Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "start"}},
			})
			if err != nil || started.GetAccepted() == nil {
				t.Fatalf("initial input: %v, %v", started, err)
			}
			waitCall := func(want int) {
				t.Helper()
				select {
				case failure := <-failures:
					t.Fatal(failure)
				case call := <-model.started:
					if call != want {
						t.Fatalf("model call = %d, want %d", call, want)
					}
				case <-ctx.Done():
					t.Fatalf("%v; initial admission: %v", context.Cause(ctx), started)
				}
			}
			waitCall(1)
			if _, err := control.SetResolvedGoal(ctx, &runtimepb.GoalSetRequest{
				Target: target, Objective: "keep working",
				Actor:           string(session.GoalActorUser),
				ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
			}); err != nil {
				t.Fatal(err)
			}
			close(model.first)
			waitCall(2)
			beforeStop, ok := authority.SessionExecution(id)
			if !ok {
				t.Fatal("Goal continuation has no execution owner")
			}
			if scenario.stopAtBoundary {
				model.advance <- struct{}{}
				select {
				case <-stoppedAtBoundary:
				case <-ctx.Done():
					t.Fatal(context.Cause(ctx))
				}
			}
			worktreeRan := make(chan struct{})
			if scenario.worktree {
				operationID := clientui.NewWorktreeTransitionID()
				ack, err := authority.RunWorktreeTransition(ctx, id.String(), operationID,
					runtimeinput.PendingWorkWorktreeTransition{Transition: runtimeinput.PendingWorkWorktreeTransitionLeave},
					func(context.Context, sessionruntime.WorktreeTransitionAuthority, sessionruntime.WorktreeTransitionTargetSync, func(clientui.WorktreeTransitionOutcome) error) error {
						close(worktreeRan)
						return nil
					})
				if err != nil || ack.GetOperationId() != operationID.String() {
					t.Fatalf("Worktree scheduling: %v, %v", ack, err)
				}
			}
			var question tools.AskQuestionRequest
			if scenario.question {
				select {
				case question = <-prompts:
				case <-ctx.Done():
					t.Fatal(context.Cause(ctx))
				}
			}
			type submission struct {
				id     runtimeids.QueueItemID
				result *chatpb.InputMutationSuccess
				err    error
			}
			submitted := make(chan submission, scenario.batch)
			for round := range scenario.rounds {
				for range scenario.batch {
					go func() {
						id := runtimeids.NewQueueItemID()
						activation := &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "additional input"}}
						var result *chatpb.InputMutationSuccess
						var err error
						if !scenario.queue {
							result, err = service.Steer(ctx, &chatpb.SteerRequest{
								Target: target, QueueItemId: id.String(), Activation: activation,
							})
						} else {
							result, err = service.Queue(ctx, &chatpb.QueueRequest{
								Target: target, QueueItemId: id.String(), Activation: activation,
							})
						}
						submitted <- submission{id: id, result: result, err: err}
					}()
				}
				for range scenario.batch {
					select {
					case input := <-submitted:
						if input.err != nil || input.result.GetAccepted().GetQueueItem().GetId() != input.id.String() {
							t.Fatalf("input during active Goal: %v, %v", input.result, input.err)
						}
					case <-ctx.Done():
						if inspectErr := authority.WithCurrentRuntime(context.Background(), id, func(_ context.Context, engine *runtime.Engine) error {
							pending, err := engine.PendingWorkSnapshot()
							t.Logf("admission deadline: GoalLoop=%t queuedWorker=%t active=%+v pending=%+v error=%v", engine.GoalLoopRunning(), engine.HasScheduledQueuedUserWork(), engine.ActiveRun(), pending, err)
							return nil
						}); inspectErr != nil {
							t.Logf("inspect admission deadline: %v", inspectErr)
						}
						t.Fatal(context.Cause(ctx))
					}
				}
				if round+1 < scenario.rounds {
					select {
					case model.advance <- struct{}{}:
					case <-ctx.Done():
						t.Fatal(context.Cause(ctx))
					}
					waitCall(round + 3)
				}
			}
			answerQuestion := func(pending tools.AskQuestionRequest) {
				t.Helper()
				answer, err := promptcontrol.NewPromptControlService(authority, metadata, metadata).AnswerPromptBatch(ctx, &promptpb.AnswerBatchRequest{
					SessionId: protoapi.SessionSelectorToProto(id), StepId: pending.StepID,
					Entries: []*promptpb.AnswerBatchEntry{{
						ToolCallId: pending.ToolCallID,
						Answer:     &promptpb.AnswerBatchEntry_QuestionAnswer{QuestionAnswer: &promptpb.QuestionAnswer{Freeform: textutil.Value("continue")}},
					}},
				})
				if err != nil {
					t.Fatalf("answer Question after input admission: %v; %v", answer, err)
				}
			}
			if scenario.question {
				answerQuestion(question)
				waitCall(3)
			}
			if scenario.stopAtBoundary {
				afterStop, ok := authority.SessionExecution(id)
				if !ok || afterStop.Scope().ID() == beforeStop.Scope().ID() {
					t.Fatal("new input reused the canceled execution")
				}
				// A model call may already have started when cancellation reaches
				// the old execution. Ask only after the new inputs are admitted.
				currentCall := 2
				for {
					waitCall(currentCall + 1)
					currentCall++
					model.mu.Lock()
					ready := model.calls == currentCall && model.lastCtx.Err() == nil
					if ready {
						model.questionAt = textutil.Value(currentCall + 1)
					}
					model.mu.Unlock()
					if ready {
						break
					}
				}
				select {
				case model.advance <- struct{}{}:
				case <-ctx.Done():
					t.Fatal(context.Cause(ctx))
				}
				waitCall(currentCall + 1)
				select {
				case pending := <-prompts:
					answerQuestion(pending)
				case <-ctx.Done():
					t.Fatal(context.Cause(ctx))
				}
				waitCall(currentCall + 2)
			}
			if scenario.worktree {
				select {
				case <-worktreeRan:
					t.Fatal("Worktree command ran before active Step finished")
				default:
				}
			}
			if _, err := control.PauseGoal(ctx, &runtimepb.GoalMutationRequest{
				SessionId: protoapi.SessionSelectorToProto(id), Actor: string(session.GoalActorUser),
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := control.LiveStop(ctx, &runtimepb.LiveStopRequest{
				SessionId: protoapi.SessionSelectorToProto(id),
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
