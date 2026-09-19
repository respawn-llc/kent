package workflowexecution

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"core/server/session"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/server/workflowstore"
)

func TestCurrentNodeControllerStartWaitsForPreparationBeforeAtomicCutover(t *testing.T) {
	f := newControllerTaskFixture(t, 1)
	before, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	runner := controllerTaskRunner{fixture: f, prepare: func(ctx context.Context, _ workflowstore.CurrentNodeStartContext, _ workflowruntime.TaskPromptDelivery) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}}
	controller := f.controller(t, runner)
	t.Cleanup(unblock)
	completed := make(chan error, 1)
	go func() {
		_, err := controller.StartTask(context.Background(), f.task.ID, f.candidate)
		completed <- err
	}()
	select {
	case <-entered:
	case err := <-completed:
		t.Fatalf("Start returned without preparation: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not prepare")
	}
	select {
	case err := <-completed:
		t.Fatalf("Start returned before preparation completed: %v", err)
	default:
	}
	during, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil || !reflect.DeepEqual(before, during) {
		t.Fatalf("preparation published Current Nodes: %+v, %v", during, err)
	}
	target, err := f.store.GetTaskExecutionTargetContext(t.Context(), f.task.ID)
	if err != nil || target.Task.ExecutionTarget != nil {
		t.Fatalf("preparation locked target: %+v, %v", target, err)
	}
	deleteCheck := make(chan error, 1)
	go func() {
		deleteCheck <- f.mutations.Run(context.Background(), f.task.ID, func(context.Context) error {
			return controller.EnsureTaskQuiescent(f.task.ID)
		})
	}()
	select {
	case err := <-deleteCheck:
		t.Fatalf("delete check crossed accepted preparation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not finish")
	}
	after, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil || len(after) != 1 || after[0].SessionID == nil || after[0].Scheduling.State != workflow.CurrentNodeSchedulingAdmitted {
		t.Fatalf("cutover missing exact Agent: %+v, %v", after, err)
	}
	owner, err := f.store.TaskIDForSession(t.Context(), *after[0].SessionID)
	if err != nil || owner == nil || *owner != f.task.ID {
		t.Fatalf("cutover Session owner = %v, %v", owner, err)
	}
	association, err := f.store.LatestTaskSessionForNode(t.Context(), after[0].Reference)
	if err != nil || association.SessionID != *after[0].SessionID {
		t.Fatalf("cutover provenance = %+v, %v", association, err)
	}
	if err := controller.EnsureTaskQuiescent(f.task.ID); !errors.Is(err, ErrTaskExecutionNotQuiescent) {
		t.Fatalf("committed pending startup appeared quiescent: %v", err)
	}
	select {
	case err := <-deleteCheck:
		if !errors.Is(err, ErrTaskExecutionNotQuiescent) {
			t.Fatalf("delete check observed a gap between cutover and startup: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("delete check did not finish after cutover")
	}
}

func TestCurrentNodeControllerStartPreparationFailureDoesNotCommitAndCanRetry(t *testing.T) {
	f := newControllerTaskFixture(t, 1)
	cause := errors.New("assignment could not be prepared")
	attemptFailure := cause
	started := make(chan struct{}, 1)
	controller := f.controller(t, controllerTaskRunner{
		fixture: f,
		prepare: func(context.Context, workflowstore.CurrentNodeStartContext, workflowruntime.TaskPromptDelivery) error {
			return attemptFailure
		},
		start: func(ctx context.Context, _ workflow.CurrentNodeReference, _ workflowruntime.TaskPromptDelivery) (sessionruntime.ExecutionHandle, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, context.Cause(ctx)
		},
	})
	before, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.StartTask(t.Context(), f.task.ID, f.candidate); !errors.Is(err, cause) {
		t.Fatalf("Start error = %v", err)
	}
	after, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed preparation committed: %+v, %v", after, err)
	}
	select {
	case <-started:
		t.Fatal("failed preparation started execution")
	default:
	}
	attemptFailure = nil
	if _, err := controller.StartTask(t.Context(), f.task.ID, f.candidate); err != nil {
		t.Fatalf("explicit retry: %v", err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("explicit retry did not start")
	}
}

func TestCurrentNodeControllerResumePreparesAllBranchesBeforeCutoverAndPreservesSessions(t *testing.T) {
	f := newControllerTaskFixture(t, 2)
	before := f.interruptBranches(t, f.branchPlan(t))
	cause := errors.New("second branch assignment unavailable")
	fail := true
	var deliveries []workflowruntime.TaskPromptDelivery
	started := make(chan workflow.CurrentNodeReference, 2)
	controller := f.controller(t, controllerTaskRunner{
		fixture: f,
		prepare: func(_ context.Context, input workflowstore.CurrentNodeStartContext, delivery workflowruntime.TaskPromptDelivery) error {
			deliveries = append(deliveries, delivery)
			if fail && input.CurrentNode.Reference.Equal(before[1].Reference) {
				return cause
			}
			return nil
		},
		start: func(ctx context.Context, reference workflow.CurrentNodeReference, delivery workflowruntime.TaskPromptDelivery) (sessionruntime.ExecutionHandle, error) {
			if delivery != workflowruntime.TaskPromptDeliveryResume {
				return nil, errors.New("existing Session did not receive Resume delivery")
			}
			started <- reference
			<-ctx.Done()
			return nil, context.Cause(ctx)
		},
	})
	if _, err := controller.ResumeTask(t.Context(), f.task.ID, nil); !errors.Is(err, cause) {
		t.Fatalf("Resume failure = %v", err)
	}
	after, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed Resume partially committed: %+v, %v", after, err)
	}
	select {
	case <-started:
		t.Fatal("partially prepared Resume started a branch")
	default:
	}
	fail = false
	resumed, err := controller.ResumeTask(t.Context(), f.task.ID, nil)
	if err != nil || resumed.Outcome != TaskResumeApplied || len(resumed.CurrentNodes) != 2 {
		t.Fatalf("Resume retry = %+v, %v", resumed, err)
	}
	for _, delivery := range deliveries {
		if delivery != workflowruntime.TaskPromptDeliveryResume {
			t.Fatalf("preparation delivery = %q", delivery)
		}
	}
	for _, original := range before {
		var matched bool
		for _, current := range resumed.CurrentNodes {
			if current.Reference.Equal(original.Reference) {
				matched = current.SessionID != nil && *current.SessionID == *original.SessionID && current.Scheduling.State == workflow.CurrentNodeSchedulingAdmitted
			}
		}
		if !matched {
			t.Fatalf("Resume replaced exact Session for %+v", original)
		}
	}
	for range before {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("resumed sibling did not start independently")
		}
	}
}

func TestReactivateWorkflowSessionReturnsAdmissionFailure(t *testing.T) {
	f := newControllerTaskFixture(t, 2)
	nodes := f.interruptBranches(t, f.branchPlan(t))
	cause := errors.New("retained Session startup failed")
	controller := f.controller(t, controllerTaskRunner{fixture: f, start: func(context.Context, workflow.CurrentNodeReference, workflowruntime.TaskPromptDelivery) (sessionruntime.ExecutionHandle, error) {
		return nil, cause
	}})
	if _, err := controller.ReactivateWorkflowSession(t.Context(), *nodes[0].SessionID); !errors.Is(err, cause) {
		t.Fatalf("reactivation failure = %v", err)
	}
	after, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range after {
		if node.Reference.Equal(nodes[1].Reference) && !reflect.DeepEqual(node, nodes[1]) {
			t.Fatalf("reactivation changed sibling: %+v", node)
		}
		if node.Reference.Equal(nodes[0].Reference) && (node.Scheduling == nil || node.Scheduling.State != workflow.CurrentNodeSchedulingInterrupted) {
			t.Fatalf("failed startup remained admitted: %+v", node)
		}
	}
}

func TestReactivateWorkflowSessionReturnsOwnQueuedStartHandle(t *testing.T) {
	runtime := newCurrentNodeQuestionFixture(t)
	f := controllerTaskFixtureInStore(t, runtime.metadata, runtime.cfg.WorkspaceRoot, 2)
	plan := f.branchPlan(t)
	nodes := f.interruptBranches(t, plan)
	selected, sessionID := nodes[0].Reference, *nodes[0].SessionID
	if _, err := session.MaterializeCommittedCreation(t.Context(), f.creations[sessionID], f.metadata.AuthoritativeSessionStoreOptions()...); err != nil {
		t.Fatal(err)
	}
	initial, _, _ := runtime.startAgentExecutionForSession(t, selected, sessionID, currentNodeQuestionLLMClient{}, func(ctx context.Context, _ sessionruntime.ExecutionScope, _ sessionruntime.AgentRuntimeBridge) error {
		<-ctx.Done()
		return context.Cause(ctx)
	})
	attachment, err := runtime.authority.OpenRuntime(t.Context(), sessionruntime.RuntimeOpenRequest{SessionID: sessionID, OwnerID: "retained-reactivation-test"})
	if err != nil {
		t.Fatal(err)
	}
	initial.RequestStop()
	if err := initial.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	// The runtime fixture publishes exact executions in this scope.
	store := &currentNodeControllerStore{Store: f.store, interrupted: nodes, sessionTaskID: &f.task.ID,
		sessionAssociation: &workflowstore.TaskSessionAssociation{SessionID: sessionID, CurrentNode: selected}}
	controller, err := NewCurrentNodeController(store, retainedSessionReactivationPublicationRunner{authority: runtime.authority, sessionID: sessionID}, runtime.authority, NewTaskMutationCoordinator(), CurrentNodeControllerConfig{AgentConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Error(err)
		}
		if _, err := attachment.Release(context.Background(), sessionruntime.RuntimeReleaseClose); err != nil {
			t.Error(err)
		}
	})
	handle, err := controller.ReactivateWorkflowSession(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		handle.RequestStop()
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	resource, resourceOK := handle.Scope().Resource()
	scope, workflowOK := handle.Scope().Workflow()
	if !resourceOK || resource.SessionID() != sessionID || !workflowOK || !scope.CurrentNode.Equal(selected) {
		t.Fatalf("reactivated scope = %+v", handle.Scope())
	}
	after, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range after {
		if node.Reference.Equal(selected) {
			continue
		}
		for _, original := range nodes {
			if original.Reference.Equal(node.Reference) && !reflect.DeepEqual(original, node) {
				t.Fatalf("reactivation changed sibling: %+v", node)
			}
		}
		if hasLiveCurrentNode(runtime.authority, node.Reference) {
			t.Fatalf("sibling became live: %+v", node.Reference)
		}
	}
}
