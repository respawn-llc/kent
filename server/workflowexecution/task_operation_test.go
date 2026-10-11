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
	"core/shared/runtimeids"
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

func TestCurrentNodeControllerStartLaterBranchPreparationFailureKeepsBacklog(t *testing.T) {
	f := newControllerTaskStartFanoutFixture(t, 2)
	before, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("later branch could not be prepared")
	var prepared []workflow.CurrentNodeReference
	var starts int
	controller := f.controller(t, controllerTaskRunner{
		fixture: f,
		prepare: func(_ context.Context, input workflowstore.CurrentNodeStartContext, _ workflowruntime.TaskPromptDelivery) error {
			prepared = append(prepared, input.CurrentNode.Reference)
			if len(prepared) == 2 {
				return cause
			}
			return nil
		},
		start: func(context.Context, workflow.CurrentNodeReference, workflowruntime.TaskPromptDelivery) (sessionruntime.ExecutionHandle, error) {
			starts++
			return nil, errors.New("execution started before Start preparation completed")
		},
	})

	if _, err := controller.StartTask(t.Context(), f.task.ID, f.candidate); !errors.Is(err, cause) {
		t.Fatalf("Start error = %v, want later-branch preparation failure", err)
	}
	after, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed preparation changed Backlog: before=%+v after=%+v err=%v", before, after, err)
	}
	if len(prepared) != 2 {
		t.Fatalf("prepared branch references = %v, want both branches attempted", prepared)
	}
	if starts != 0 {
		t.Fatalf("branch executions started = %d, want none before successful preparation", starts)
	}
	target, err := f.store.GetTaskExecutionTargetContext(t.Context(), f.task.ID)
	if err != nil || target.Task.ExecutionTarget != nil {
		t.Fatalf("failed preparation changed the Task execution target: %+v, %v", target, err)
	}
}

func TestCurrentNodeControllerStartBindsFreshAgentSessionsToSharedRoot(t *testing.T) {
	f := newControllerTaskStartFanoutFixture(t, 2)
	var roots []workflowstore.ExecutionRoot
	controller := f.controller(t, controllerTaskRunner{
		fixture: f,
		prepare: func(_ context.Context, input workflowstore.CurrentNodeStartContext, _ workflowruntime.TaskPromptDelivery) error {
			if input.ExecutionRoot == nil {
				return errors.New("Start branch has no execution root")
			}
			roots = append(roots, *input.ExecutionRoot)
			return nil
		},
	})

	started, err := controller.StartTask(t.Context(), f.task.ID, f.candidate)
	if err != nil {
		t.Fatalf("Start direct fan-out: %v", err)
	}
	if len(started.Mutation.Created) != 2 || len(roots) != 2 {
		t.Fatalf("Start result has %d Current Nodes and prepared %d roots, want two branches", len(started.Mutation.Created), len(roots))
	}
	for _, root := range roots {
		if !reflect.DeepEqual(root, f.candidate.Root) {
			t.Fatalf("prepared branch execution root = %+v, want shared root %+v", root, f.candidate.Root)
		}
	}

	nodes, err := f.store.ListCurrentNodes(t.Context(), f.task.ID)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("Current Nodes after Start = %+v, %v; want both branches", nodes, err)
	}
	sessionIDs := make(map[runtimeids.SessionID]bool, len(nodes))
	for _, node := range nodes {
		if node.SessionID == nil {
			t.Fatalf("Agent branch %q has no prepared Session", node.Reference.NodeID)
		}
		if sessionIDs[*node.SessionID] {
			t.Fatalf("Agent branches share Session %q", *node.SessionID)
		}
		sessionIDs[*node.SessionID] = true
		if _, fresh := f.creations[*node.SessionID]; !fresh {
			t.Fatalf("Agent branch Session %q was not freshly prepared", *node.SessionID)
		}
		owner, err := f.store.TaskIDForSession(t.Context(), *node.SessionID)
		if err != nil || owner == nil || *owner != f.task.ID {
			t.Fatalf("Session %q owner = %v, %v; want Task %q", *node.SessionID, owner, err, f.task.ID)
		}
		association, err := f.store.LatestTaskSessionForNode(t.Context(), node.Reference)
		if err != nil || association.SessionID != *node.SessionID {
			t.Fatalf("Agent branch Session association = %+v, %v", association, err)
		}
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
