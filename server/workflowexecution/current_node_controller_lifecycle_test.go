package workflowexecution

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/runtimeids"
)

func TestEnsureTaskQuiescentRejectsPublishedExactExecution(t *testing.T) {
	t.Run("Agent", func(t *testing.T) {
		fixture := newCurrentNodeQuestionFixture(t)
		reference := currentNodeReferenceForControllerTest(t, "task-live-agent", "node-agent")
		handle, _, _ := fixture.startAgentExecution(
			t,
			reference,
			func(ctx context.Context, _ sessionruntime.ExecutionScope, _ sessionruntime.AgentRuntimeBridge) error {
				<-ctx.Done()
				return context.Cause(ctx)
			},
		)
		t.Cleanup(func() {
			handle.RequestStop()
			if err := handle.Close(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("close live Agent execution: %v", err)
			}
		})
		waitForRunningCurrentNode(t, fixture.authority, reference)

		if err := fixture.controller.EnsureTaskQuiescent(reference.TaskID); !errors.Is(err, ErrTaskExecutionNotQuiescent) {
			t.Fatalf("EnsureTaskQuiescent error = %v, want %v", err, ErrTaskExecutionNotQuiescent)
		}
	})

	t.Run("Script", func(t *testing.T) {
		shellPath, err := exec.LookPath("sh")
		if err != nil {
			t.Skipf("sh executable unavailable: %v", err)
		}
		fixture := newCurrentNodeQuestionFixture(t)
		reference := currentNodeReferenceForControllerTest(t, "task-live-script", "node-script")
		handle := startLiveTestWorkflowScript(t, fixture.controller, fixture.authority, reference, sessionruntime.ScriptExecutionRequest{
			Command: sessionruntime.ScriptCommand{
				Path: shellPath,
				Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
			},
		})
		t.Cleanup(func() {
			handle.RequestStop()
			if err := handle.Close(context.Background()); err != nil {
				t.Errorf("close live Script execution: %v", err)
			}
		})
		waitForRunningCurrentNode(t, fixture.authority, reference)

		if err := fixture.controller.EnsureTaskQuiescent(reference.TaskID); !errors.Is(err, ErrTaskExecutionNotQuiescent) {
			t.Fatalf("EnsureTaskQuiescent error = %v, want %v", err, ErrTaskExecutionNotQuiescent)
		}
	})

	t.Run("finalizing Script", func(t *testing.T) {
		shellPath, err := exec.LookPath("sh")
		if err != nil {
			t.Skipf("sh executable unavailable: %v", err)
		}
		fixture := newCurrentNodeQuestionFixture(t)
		reference := currentNodeReferenceForControllerTest(t, "task-finalizing-script", "node-script")
		finalizeEntered := make(chan struct{})
		releaseFinalize := make(chan struct{})
		handle := startLiveTestWorkflowScript(t, fixture.controller, fixture.authority, reference, sessionruntime.ScriptExecutionRequest{
			Command: sessionruntime.ScriptCommand{Path: shellPath, Args: []string{"-c", "exit 0"}},
			Finalize: func(context.Context, sessionruntime.ExecutionScope, sessionruntime.ScriptResult, error) error {
				close(finalizeEntered)
				<-releaseFinalize
				return nil
			},
		})
		t.Cleanup(func() {
			close(releaseFinalize)
			if err := handle.Close(context.Background()); err != nil {
				t.Errorf("close finalizing Script execution: %v", err)
			}
		})
		select {
		case <-finalizeEntered:
		case <-time.After(3 * time.Second):
			t.Fatal("Script execution did not enter finalization")
		}

		if err := fixture.controller.EnsureTaskQuiescent(reference.TaskID); !errors.Is(err, ErrTaskExecutionNotQuiescent) {
			t.Fatalf("EnsureTaskQuiescent error = %v, want %v", err, ErrTaskExecutionNotQuiescent)
		}
	})
}

func TestResumePreflightLeavesLiveTaskAdmissionUntouched(t *testing.T) {
	fixture := newCurrentNodeQuestionFixture(t)
	reference := currentNodeReferenceForControllerTest(t, "task-resume-live", "node-implementation")
	release := make(chan struct{})
	handle, sessionID := fixture.startQuestionExecution(
		t,
		reference,
		func(context.Context, sessionruntime.ExecutionScope, sessionruntime.AgentRuntimeBridge) error {
			<-release
			return nil
		},
	)
	t.Cleanup(func() {
		close(release)
		if err := handle.Close(context.Background()); err != nil {
			t.Errorf("close live execution: %v", err)
		}
	})
	fixture.store.currentNodes = []workflow.CurrentNode{{
		Reference:  reference,
		SessionID:  &sessionID,
		Scheduling: &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingAdmitted},
	}}
	preflight, err := fixture.controller.PreflightTaskResume(context.Background(), reference.TaskID)
	if err != nil {
		t.Fatalf("PreflightTaskResume: %v", err)
	}
	if preflight.Outcome != TaskResumePreflightNoOp {
		t.Fatalf("resume preflight = %+v, want no-op for live task", preflight)
	}
	if fixture.store.currentNodes[0].Scheduling.State != workflow.CurrentNodeSchedulingAdmitted {
		t.Fatalf("live task node = %+v, want unchanged admission", fixture.store.currentNodes[0])
	}
}

func TestResumeTaskReturnsConflictBeforeMutationWhenRetainedSessionExecutionIsActive(t *testing.T) {
	fixture := newCurrentNodeQuestionFixture(t)
	reference := currentNodeReferenceForControllerTest(t, "task-resume-active-session", "node-implementation")
	release := make(chan struct{})
	handle, sessionID := fixture.startQuestionExecution(
		t,
		reference,
		func(context.Context, sessionruntime.ExecutionScope, sessionruntime.AgentRuntimeBridge) error {
			<-release
			return nil
		},
	)
	t.Cleanup(func() {
		close(release)
		if err := handle.Close(context.Background()); err != nil {
			t.Errorf("close active retained Session execution: %v", err)
		}
	})
	fixture.store.interrupted = []workflow.CurrentNode{{
		Reference: reference,
		SessionID: &sessionID,
	}}

	resumed, err := fixture.controller.ResumeTask(context.Background(), reference.TaskID, nil)
	if !errors.Is(err, ErrTaskExecutionNotQuiescent) {
		t.Fatalf("resume error = %v, want %v", err, ErrTaskExecutionNotQuiescent)
	}
	if len(resumed.CurrentNodes) != 0 {
		t.Fatalf("resumed Current Nodes = %+v, want none", resumed)
	}
}

type retainedSessionReactivationPublicationRunner struct {
	authority *sessionruntime.Authority
	sessionID runtimeids.SessionID
}

func (r retainedSessionReactivationPublicationRunner) PrepareCurrentNode(_ context.Context, input workflowstore.CurrentNodeStartContext, _ workflowruntime.TaskPromptDelivery) (CurrentNodePreparation, error) {
	return CurrentNodePreparation{Session: &workflowstore.PlannedCurrentNodeSession{CurrentNode: input.CurrentNode.Reference, SessionID: r.sessionID}}, nil
}

func (retainedSessionReactivationPublicationRunner) PrepareScriptPublication(
	context.Context,
	workflow.CurrentNodeReference,
	workflowruntime.Controller,
) (CurrentNodeScriptPublication, error) {
	return nil, nil
}

func (r retainedSessionReactivationPublicationRunner) StartAgentCurrentNode(
	ctx context.Context,
	reference workflow.CurrentNodeReference,
	_ workflowruntime.TaskPromptDelivery,
	_ CurrentNodeAssignmentSteer,
	onRetire func(),
	controller workflowruntime.Controller,
) (sessionruntime.ExecutionHandle, error) {
	descriptor, err := session.NewOpenSessionDescriptor(r.sessionID)
	if err != nil {
		return nil, err
	}
	return r.authority.StartAgentExecution(ctx, sessionruntime.AgentExecutionRequest{
		Descriptor: descriptor,
		Workflow: &sessionruntime.WorkflowAgentExecution{
			Reference: sessionruntime.WorkflowExecutionRef{
				ProjectID:   "project-test",
				WorkflowID:  currentNodeControllerTestWorkflowID,
				CurrentNode: reference,
			},
			Config: &workflowruntime.CurrentNodeExecutionConfig{
				Contract:       workflowruntime.CompletionContract{Transitions: []workflowruntime.CompletionTransition{{ID: "next"}}},
				CompletionMode: workflowruntime.CompletionModeTool,
				Controller:     controller,
				Instructions: workflowruntime.TaskInstructions{
					WorkflowID:  currentNodeControllerTestWorkflowID,
					CurrentNode: reference,
				},
			},
			OnRetire: onRetire,
		},
		Resource: sessionruntime.CurrentAgentResource{},
		Runner: func(ctx context.Context, _ sessionruntime.ExecutionScope, _ sessionruntime.AgentRuntimeBridge) error {
			<-ctx.Done()
			return context.Cause(ctx)
		},
	})
}

func TestReactivateWorkflowSessionRejectsConcurrentExplicitAdmission(t *testing.T) {
	queue := newControllerQueueFixture(t, 1)
	reference := queue.tasks[0].reference(t, 0)
	store := queue.store()
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	admissionFailure := errors.New("blocked current node setup released")
	runner := &blockingCurrentNodeRunner{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		cause:   admissionFailure,
	}
	controller := newCurrentNodeControllerForTest(t, store, runner, authority, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(runner.release) }) }
	t.Cleanup(func() {
		release()
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})

	if err := queue.approve(context.Background(), controller, reference.TaskID); err != nil {
		t.Fatalf("approve retained Session admission: %v", err)
	}
	select {
	case <-runner.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("explicit Resume admission did not begin")
	}
	nodes, err := store.Store.ListCurrentNodes(context.Background(), reference.TaskID)
	if err != nil || len(nodes) != 1 || nodes[0].SessionID == nil {
		t.Fatalf("committed target Session: %+v, %v", nodes, err)
	}
	_, err = controller.ReactivateWorkflowSession(context.Background(), *nodes[0].SessionID)
	var conflict *TaskResumeConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ReactivateWorkflowSession error = %v, want Task Resume conflict", err)
	}
	release()
}

func TestReactivateWorkflowSessionReturnsAlreadyPublishedWorkflowExecution(t *testing.T) {
	fixture := newCurrentNodeQuestionFixture(t)
	reference := currentNodeReferenceForControllerTest(
		t,
		"task-reactivate-published-resume",
		"node-reactivate-published-resume",
	)
	release := make(chan struct{})
	handle, sessionID := fixture.startQuestionExecution(
		t,
		reference,
		func(context.Context, sessionruntime.ExecutionScope, sessionruntime.AgentRuntimeBridge) error {
			<-release
			return nil
		},
	)
	t.Cleanup(func() {
		close(release)
		if err := handle.Close(context.Background()); err != nil {
			t.Errorf("close published Workflow execution: %v", err)
		}
	})
	taskID := reference.TaskID
	fixture.store.mu.Lock()
	fixture.store.sessionTaskID = &taskID
	fixture.store.sessionAssociation = &workflowstore.TaskSessionAssociation{
		SessionID:   sessionID,
		CurrentNode: reference,
	}
	fixture.store.currentNodes = []workflow.CurrentNode{{
		Reference:  reference,
		SessionID:  &sessionID,
		Scheduling: &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingAdmitted},
	}}
	fixture.store.mu.Unlock()

	reactivated, err := fixture.controller.ReactivateWorkflowSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ReactivateWorkflowSession: %v", err)
	}
	if reactivated != handle {
		t.Fatalf("reactivated execution = %v, want already-published handle %v", reactivated, handle)
	}
	workflowRef, workflowScoped := handle.Scope().Workflow()
	if !workflowScoped || !workflowRef.CurrentNode.Equal(reference) {
		t.Fatalf("published execution scope = %+v, want Workflow Current Node %v", handle.Scope(), reference)
	}
}

func TestObserveWorkflowTaskExecutionsIgnoresLatchedWorkerFailure(t *testing.T) {
	cause := errors.New("automatic assignment failed")
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := &CurrentNodeController{
		authority: authority,
		workerErr: cause,
	}
	t.Cleanup(func() {
		_ = authority.Close(context.Background())
	})
	taskID := workflow.TaskID("task-status-read")

	observation, err := controller.ObserveWorkflowTaskExecutions([]workflow.TaskID{taskID})
	if err != nil {
		t.Fatalf("ObserveWorkflowTaskExecutions: %v", err)
	}
	if !observation.Quiescence[taskID] {
		t.Fatalf("task quiescence = %+v, want quiescent observation", observation.Quiescence)
	}
}

func TestCurrentNodeControllerAgentCompletionRequiresMatchingActiveProvenance(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(workflowruntime.AgentCompletionProvenance) workflowruntime.AgentCompletionProvenance
		wantOK bool
	}{
		{name: "matching", mutate: func(value workflowruntime.AgentCompletionProvenance) workflowruntime.AgentCompletionProvenance {
			return value
		}, wantOK: true},
		{name: "stale scope", mutate: func(value workflowruntime.AgentCompletionProvenance) workflowruntime.AgentCompletionProvenance {
			value.ScopeID = runtimeids.NewExecutionScopeID()
			return value
		}},
		{name: "stale run", mutate: func(value workflowruntime.AgentCompletionProvenance) workflowruntime.AgentCompletionProvenance {
			value.RunID = mustAgentCompletionRunID(t, "11111111-1111-4111-8111-111111111111")
			return value
		}},
		{name: "stale step", mutate: func(value workflowruntime.AgentCompletionProvenance) workflowruntime.AgentCompletionProvenance {
			value.StepID = mustAgentCompletionStepID(t, "22222222-2222-4222-8222-222222222222")
			return value
		}},
		{name: "duplicate", mutate: func(value workflowruntime.AgentCompletionProvenance) workflowruntime.AgentCompletionProvenance {
			return value
		}, wantOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCurrentNodeQuestionFixture(t)
			task := controllerTaskFixtureInStore(t, fixture.metadata, fixture.cfg.WorkspaceRoot, 1)
			def, record, err := task.store.GetDefinition(t.Context(), task.task.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			graph := workflowstore.NewWorkflowGraphSaveRequest(def, record.Version)
			for index := range graph.TransitionGroups {
				if graph.TransitionGroups[index].SourceNodeID == task.branches[0] {
					graph.TransitionGroups[index].TransitionID = "next"
				}
			}
			if saved, err := task.store.SaveWorkflowGraph(t.Context(), graph); err != nil || !saved.Saved {
				t.Fatalf("save completion graph: %+v, %v", saved.ValidationErrors, err)
			}
			current := task.moveBranches(t, task.branchPlan(t))[0]
			source := current.Reference
			fixture.store.Store = task.store
			fixture.store.queueFixture = &controllerQueueFixture{tasks: []controllerTaskFixture{task}}
			if _, err := session.MaterializeCommittedCreation(t.Context(), task.creations[*current.SessionID], fixture.metadata.AuthoritativeSessionStoreOptions()...); err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			start := make(chan struct{})
			completionResult := make(chan error, 1)
			var releaseOnce sync.Once
			var generateOnce sync.Once
			var (
				scopeID   runtimeids.ExecutionScopeID
				sessionID runtimeids.SessionID
				bridge    sessionruntime.AgentRuntimeBridge
			)
			client := callbackCurrentNodeLLMClient{generate: func(context.Context, llm.Request) (llm.Response, error) {
				firstGenerate := false
				generateOnce.Do(func() { firstGenerate = true })
				if !firstGenerate {
					return llm.Response{}, context.Canceled
				}
				var active *runtime.RunSnapshot
				if err := bridge.WithEngine(context.Background(), func(_ context.Context, engine *runtime.Engine) error {
					active = engine.ActiveRun()
					return nil
				}); err != nil {
					releaseOnce.Do(func() { close(release) })
					return llm.Response{}, err
				}
				if active == nil {
					releaseOnce.Do(func() { close(release) })
					return llm.Response{}, errors.New("Agent completion test has no active Run")
				}
				provenance := test.mutate(workflowruntime.AgentCompletionProvenance{
					ScopeID: scopeID,
					RunID:   mustAgentCompletionRunID(t, active.RunID),
					StepID:  mustAgentCompletionStepID(t, active.StepID),
				})
				_, err := fixture.controller.CompleteAgentCurrentNode(context.Background(), workflowruntime.AgentCompletionRequest{
					Provenance:   provenance,
					SessionID:    sessionID,
					TransitionID: "next",
				})
				if test.name == "duplicate" && err == nil {
					_, err = fixture.controller.CompleteAgentCurrentNode(context.Background(), workflowruntime.AgentCompletionRequest{
						Provenance:   provenance,
						SessionID:    sessionID,
						TransitionID: "next",
					})
					if err == nil {
						err = errors.New("duplicate Agent completion unexpectedly succeeded")
					}
				}
				select {
				case completionResult <- err:
				default:
				}
				releaseOnce.Do(func() { close(release) })
				if err != nil && test.name != "duplicate" {
					return llm.Response{}, context.Canceled
				}
				return llm.Response{}, nil
			}}
			handle, assignedSessionID, _ := fixture.startAgentExecutionForSession(
				t,
				source,
				*current.SessionID,
				client,
				func(ctx context.Context, scope sessionruntime.ExecutionScope, runtimeBridge sessionruntime.AgentRuntimeBridge) error {
					<-start
					scopeID = scope.ID()
					bridge = runtimeBridge
					err := bridge.WithEngine(ctx, func(ctx context.Context, engine *runtime.Engine) error {
						_, submitErr := engine.SubmitWorkflowTurn(ctx)
						return submitErr
					})
					<-release
					return err
				},
			)
			sessionID = assignedSessionID
			close(start)
			err = func() error {
				_, waitErr := handle.Wait(context.Background())
				return waitErr
			}()
			completionErr := <-completionResult
			if test.wantOK {
				if err != nil {
					t.Fatalf("matching Agent completion: %v", err)
				}
				if test.name == "duplicate" && completionErr == nil {
					t.Fatal("duplicate Agent completion unexpectedly succeeded")
				}
				if calls := fixture.store.completionCount(); calls != 1 {
					t.Fatalf("completion calls = %d, want 1", calls)
				}
				return
			}
			if completionErr == nil {
				t.Fatal("stale Agent completion unexpectedly succeeded")
			}
			if calls := fixture.store.completionCount(); calls != 0 {
				t.Fatalf("completion calls = %d, want 0", calls)
			}
		})
	}
}

func mustAgentCompletionRunID(t *testing.T, raw string) runtimeids.RunID {
	t.Helper()
	value, err := runtimeids.ParseRunID(raw)
	if err != nil {
		t.Fatalf("parse Run ID: %v", err)
	}
	return value
}

func mustAgentCompletionStepID(t *testing.T, raw string) runtimeids.StepID {
	t.Helper()
	value, err := runtimeids.ParseStepID(raw)
	if err != nil {
		t.Fatalf("parse Step ID: %v", err)
	}
	return value
}

func TestCurrentNodeControllerRejectsProtocolViolationsAfterScopeRetires(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	store := &currentNodeControllerStore{}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller := newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		_ = controller.Close()
		_ = authority.Close(context.Background())
	})
	request := workflowruntime.ViolationRequest{
		ScopeID:   runtimeids.NewExecutionScopeID(),
		SessionID: &sessionID,
		Kind:      workflowruntime.ViolationKindInvalidCompletion,
		MaxCount:  2,
	}

	if _, err := controller.RecordProtocolViolation(context.Background(), request); !errors.Is(err, sessionruntime.ErrExecutionNoLongerLive) {
		t.Fatalf("record retired-scope protocol violation error = %v, want %v", err, sessionruntime.ErrExecutionNoLongerLive)
	}
	if err := controller.ResetProtocolViolationBudget(context.Background(), workflowruntime.ViolationResetRequest{
		ScopeID:   request.ScopeID,
		SessionID: &sessionID,
	}); !errors.Is(err, sessionruntime.ErrExecutionNoLongerLive) {
		t.Fatalf("reset retired-scope protocol violation error = %v, want %v", err, sessionruntime.ErrExecutionNoLongerLive)
	}
}

func TestCurrentNodeControllerSteersApprovalTargetBeforeStartingIt(t *testing.T) {
	queue := newControllerQueueFixture(t, 1)
	target := queue.tasks[0].reference(t, 0)
	store := queue.store()
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &countingCurrentNodeRunner{}
	steerer := &recordingCurrentNodeAssignmentSteerer{}
	store.assignment = steerer
	controller := newCurrentNodeControllerWithConfigForTest(t, store, runner, authority, NewTaskMutationCoordinator(), CurrentNodeControllerConfig{
		AgentConcurrency: 1,
	})
	t.Cleanup(func() {
		_ = controller.Close()
		_ = authority.Close(context.Background())
	})

	if err := queue.approve(context.Background(), controller, target.TaskID); err != nil {
		t.Fatalf("ApplyPendingApproval: %v", err)
	}
	if got := steerer.references(); len(got) != 1 || !got[0].Equal(target) {
		t.Fatalf("steered assignments = %+v, want %v", got, target)
	}
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		return len(runner.promptDeliveries()) == 1
	}, "approval target did not reach runner")
	if deliveries := runner.promptDeliveries(); len(deliveries) != 1 ||
		deliveries[0] != workflowruntime.TaskPromptDeliveryAssignment {
		t.Fatalf("fresh target prompt deliveries = %+v, want Assignment", deliveries)
	}
}

func TestCurrentNodeControllerDoesNotMakeUnassignedApprovalTargetResumable(t *testing.T) {
	queue := newControllerQueueFixture(t, 1)
	target := queue.tasks[0].reference(t, 0)
	store := queue.store()
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	runner := &countingCurrentNodeRunner{}
	cause := errors.New("assignment append failed")
	store.assignment = &recordingCurrentNodeAssignmentSteerer{err: cause}
	controller := newCurrentNodeControllerWithConfigForTest(t, store, runner, authority, NewTaskMutationCoordinator(), CurrentNodeControllerConfig{
		AgentConcurrency: 1,
	})
	t.Cleanup(func() {
		_ = controller.Close()
		_ = authority.Close(context.Background())
	})

	if err := queue.approve(context.Background(), controller, target.TaskID); !errors.Is(err, cause) {
		t.Fatalf("ApplyPendingApproval error = %v, want %v", err, cause)
	}
	time.Sleep(50 * time.Millisecond)
	if starts := runner.starts(); starts != 0 {
		t.Fatalf("runner starts = %d, want none after steering failure", starts)
	}
	if interruption, interrupted := store.interruption(target); interrupted {
		t.Fatalf("unassigned approval target was made resumable: %+v", interruption)
	}
	approvals, err := store.Store.ListPendingApprovals(context.Background(), target.TaskID)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("preparation failure lost the pending Approval: %+v, %v", approvals, err)
	}
	nodes, err := store.Store.ListCurrentNodes(context.Background(), target.TaskID)
	if err != nil || len(nodes) != 1 || nodes[0].Reference.Equal(target) {
		t.Fatalf("preparation failure changed the source placement: %+v, %v", nodes, err)
	}
}
