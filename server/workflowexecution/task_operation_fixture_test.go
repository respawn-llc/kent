package workflowexecution

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/metadata"
	"core/server/session"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

type controllerTaskFixture struct {
	store     *workflowstore.Store
	metadata  *metadata.Store
	binding   metadata.Binding
	task      workflowstore.TaskRecord
	candidate *workflowstore.ExecutionTargetCandidate
	branches  []workflow.NodeID
	creations map[runtimeids.SessionID]session.CreationPlan
	mutations *TaskMutationCoordinator
}

func newControllerTaskFixture(t *testing.T, branches int) controllerTaskFixture {
	t.Helper()
	return controllerTaskFixtureInStore(t, testsetup.OpenStore(t, t.TempDir()), t.TempDir(), branches)
}

func controllerTaskFixtureInStore(t *testing.T, meta *metadata.Store, workspace string, branches int) controllerTaskFixture {
	t.Helper()
	ctx := context.Background()
	binding, err := meta.RegisterWorkspaceBinding(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.SetProjectKey(ctx, binding.ProjectID, "CTL"); err != nil {
		t.Fatal(err)
	}
	store, err := workflowstore.New(meta, workflowstore.WithRoleResolver(testsetup.QuestionsEnabled("coder")))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Controller operations"})
	if err != nil {
		t.Fatal(err)
	}
	def, record, err := store.GetDefinition(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := workflowstore.NewWorkflowGraphSaveRequest(def, record.Version)
	var start, done workflow.NodeID
	for _, node := range def.Nodes {
		switch node.Kind() {
		case workflow.NodeKindStart:
			start = workflow.NodeIDOf(node)
		case workflow.NodeKindTerminal:
			done = workflow.NodeIDOf(node)
		}
	}
	addAgent := func(key string) workflow.NodeID {
		id := workflow.NodeID(runtimeids.NewGraphEntityID())
		req.Nodes = append(req.Nodes, workflowstore.NodeRecord{ID: id, WorkflowID: record.ID, Key: workflow.ModelKey(key), Kind: workflow.NodeKindAgent, DisplayName: key, SubagentRole: "coder"})
		return id
	}
	addGroup := func(source workflow.NodeID, key string) workflow.TransitionGroupID {
		id := workflow.TransitionGroupID(runtimeids.NewGraphEntityID())
		req.TransitionGroups = append(req.TransitionGroups, workflowstore.TransitionGroupRecord{ID: id, WorkflowID: record.ID, SourceNodeID: source, TransitionID: workflow.TransitionID(key), DisplayName: key})
		return id
	}
	join := done
	if branches > 1 {
		join = workflow.NodeID(runtimeids.NewGraphEntityID())
		req.Nodes = append(req.Nodes, workflowstore.NodeRecord{ID: join, WorkflowID: record.ID, Key: "join", Kind: workflow.NodeKindJoin, DisplayName: "Join"})
	}
	addEdge := func(group workflow.TransitionGroupID, target workflow.NodeID, key string) {
		var prompt string
		if target != done && target != join {
			prompt = "Perform the work."
		}
		req.Edges = append(req.Edges, workflowstore.EdgeRecord{ID: workflow.EdgeID(runtimeids.NewGraphEntityID()), WorkflowID: record.ID, TransitionGroupID: group, Key: workflow.ModelKey(key), TargetNodeID: target,
			AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured, ContextMode: workflow.ContextModeNewSession, PromptTemplate: prompt})
	}
	seed := addAgent("seed")
	addEdge(addGroup(start, "start"), seed, "start")
	split := addGroup(seed, "split")
	ids := make([]workflow.NodeID, branches)
	for index := range ids {
		key := fmt.Sprintf("branch_%d", index)
		ids[index] = addAgent(key)
		addEdge(split, ids[index], key)
		addEdge(addGroup(ids[index], key+"_done"), join, key+"_done")
	}
	if branches > 1 {
		addEdge(addGroup(join, "done"), done, "done")
	}
	saved, err := store.SaveWorkflowGraph(ctx, req)
	if err != nil || !saved.Saved {
		t.Fatalf("save controller workflow: %v, %v", saved.ValidationErrors, err)
	}
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, record.ID, true); err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, SourceWorkspaceID: binding.WorkspaceID, Title: "Controller operation"})
	if err != nil {
		t.Fatal(err)
	}
	return controllerTaskFixture{
		store: store, metadata: meta, binding: binding, task: task, branches: ids, creations: make(map[runtimeids.SessionID]session.CreationPlan), mutations: NewTaskMutationCoordinator(),
		candidate: &workflowstore.ExecutionTargetCandidate{
			Snapshot: workflowstore.ExecutionTargetSnapshot{Mode: workflow.ExecutionTargetModeNone, Provenance: workflowstore.ExecutionTargetProvenanceResolved},
			Root:     workflowstore.ExecutionRoot{SourceWorkspaceID: binding.WorkspaceID, SourceWorkspaceRoot: binding.CanonicalRoot},
		},
	}
}

func (f controllerTaskFixture) prepareSession(ctx context.Context, input workflowstore.CurrentNodeStartContext) (CurrentNodePreparation, error) {
	if input.Node.Kind == workflow.NodeKindScript {
		return CurrentNodePreparation{}, nil
	}
	planned := &workflowstore.PlannedCurrentNodeSession{CurrentNode: input.CurrentNode.Reference}
	if input.CurrentNode.SessionID != nil {
		planned.SessionID = *input.CurrentNode.SessionID
	} else {
		id := runtimeids.NewSessionID()
		descriptor, err := session.NewCreateSessionDescriptor(id, filepath.Join(f.metadata.PersistenceRoot(), "projects", f.binding.ProjectID, "sessions"), "workspace", f.binding.CanonicalRoot, sessioncontract.SessionCategoryMain)
		if err != nil {
			return CurrentNodePreparation{}, err
		}
		creation, err := session.PrepareCreation(session.CreationRequest{Descriptor: descriptor})
		if err != nil {
			return CurrentNodePreparation{}, err
		}
		snapshot, err := f.metadata.PrepareSessionSnapshot(ctx, creation.Snapshot(), metadata.SessionExecutionTargetUpdate{
			SessionID: id.String(), Workspace: &metadata.SessionExecutionTargetUpdateWorkspace{ID: f.binding.WorkspaceID}, CwdRelpath: ".",
		})
		if err != nil {
			return CurrentNodePreparation{}, err
		}
		planned.SessionID, planned.Snapshot = id, &snapshot
		f.creations[id] = creation
	}
	return CurrentNodePreparation{Session: planned, Assignment: completedCurrentNodeAssignmentSteer{receipt: session.CommitReceipt{Committed: true}}}, nil
}

func (f controllerTaskFixture) reference(t *testing.T, index int) workflow.CurrentNodeReference {
	t.Helper()
	var branch *workflow.TransitionBranchKey
	if len(f.branches) > 1 {
		key := workflow.TransitionBranchKey(fmt.Sprintf("branch_%d", index))
		branch = &key
	}
	reference, err := workflow.NewCurrentNodeReference(f.task.ID, f.branches[index], branch)
	if err != nil {
		t.Fatal(err)
	}
	return reference
}

// Each Task has a real seed -> branches -> Join graph. Queue tests choose the
// executable kinds before starting the seed, then enter through completion or
// Approval rather than manufacturing durable outcomes.
type controllerQueueFixture struct {
	t     *testing.T
	tasks []controllerTaskFixture
	kinds map[workflow.NodeID]workflow.NodeKind
}

func newControllerQueueFixture(t *testing.T, branchCounts ...int) *controllerQueueFixture {
	t.Helper()
	meta := testsetup.OpenStore(t, t.TempDir())
	workspace := t.TempDir()
	f := &controllerQueueFixture{t: t, kinds: make(map[workflow.NodeID]workflow.NodeKind)}
	for _, count := range branchCounts {
		f.tasks = append(f.tasks, controllerTaskFixtureInStore(t, meta, workspace, count))
	}
	return f
}

func (f *controllerQueueFixture) store() *currentNodeControllerStore {
	return &currentNodeControllerStore{Store: f.tasks[0].store, queueFixture: f}
}

func (f *controllerQueueFixture) prepare(ctx context.Context, input workflowstore.CurrentNodeStartContext) (CurrentNodePreparation, error) {
	for _, task := range f.tasks {
		if task.task.ID == input.Task.ID {
			return task.prepareSession(ctx, input)
		}
	}
	return CurrentNodePreparation{}, errors.New("queue fixture has no matching Task")
}

func (f *controllerQueueFixture) seed(ctx context.Context, controller *CurrentNodeController, taskID workflow.TaskID, approval bool) (workflow.CurrentNodeReference, error) {
	var task controllerTaskFixture
	for _, candidate := range f.tasks {
		if candidate.task.ID == taskID {
			task = candidate
		}
	}
	if task.store == nil {
		return workflow.CurrentNodeReference{}, errors.New("queue fixture has no matching Task")
	}
	def, record, err := task.store.GetDefinition(ctx, task.task.WorkflowID)
	if err != nil {
		return workflow.CurrentNodeReference{}, err
	}
	req := workflowstore.NewWorkflowGraphSaveRequest(def, record.Version)
	runner := controller.runner.(currentNodeTestPublicationRunner)
	for index := range task.branches {
		ref := task.reference(f.t, index)
		kind := runner.nodeKind(ref)
		if chosen, ok := f.kinds[ref.NodeID]; ok {
			kind = chosen
		}
		for n := range req.Nodes {
			if req.Nodes[n].ID == ref.NodeID && kind == workflow.NodeKindScript {
				req.Nodes[n].Kind = kind
				req.Nodes[n].SubagentRole = ""
				req.Nodes[n].ScriptPath = "/usr/bin/true"
			}
		}
		for e := range req.Edges {
			if req.Edges[e].TargetNodeID == ref.NodeID {
				req.Edges[e].RequiresApproval = approval
				if kind == workflow.NodeKindScript {
					req.Edges[e].PromptTemplate = ""
				}
			}
		}
	}
	saved, err := task.store.SaveWorkflowGraph(ctx, req)
	if err != nil {
		return workflow.CurrentNodeReference{}, err
	}
	if !saved.Saved {
		return workflow.CurrentNodeReference{}, fmt.Errorf("queue graph validation: %+v", saved.ValidationErrors)
	}
	plan, err := task.store.PlanTaskStart(ctx, task.task.ID, task.candidate)
	if err != nil {
		return workflow.CurrentNodeReference{}, err
	}
	var sessions []workflowstore.PlannedCurrentNodeSession
	for _, input := range plan.StartContexts() {
		prepared, err := task.prepareSession(ctx, input)
		if err != nil {
			return workflow.CurrentNodeReference{}, err
		}
		if prepared.Session != nil {
			sessions = append(sessions, *prepared.Session)
		}
	}
	started, err := task.store.CommitTaskStart(ctx, plan, sessions)
	if err != nil {
		return workflow.CurrentNodeReference{}, err
	}
	return started.Mutation.Created[0].Reference, nil
}

func (f *controllerQueueFixture) automaticIntents(controller *CurrentNodeController, intents []CurrentNodeAutomaticIntent) {
	f.t.Helper()
	references := make([]workflow.CurrentNodeReference, 0, len(intents))
	for _, intent := range intents {
		f.kinds[intent.CurrentNode.NodeID] = intent.NodeKind
		references = append(references, intent.CurrentNode)
	}
	f.automatic(controller, references...)
}

func (f *controllerQueueFixture) approve(ctx context.Context, controller *CurrentNodeController, taskID workflow.TaskID) error {
	source, err := f.seed(ctx, controller, taskID, true)
	if err != nil {
		return err
	}
	plan, err := f.tasks[0].store.PlanCurrentNodeCompletion(ctx, workflowstore.CurrentNodeCompletionRequest{Source: source, TransitionID: "split"})
	if err != nil {
		return err
	}
	result, err := f.tasks[0].store.CommitCurrentNodeCompletion(ctx, plan, nil)
	if err != nil {
		return err
	}
	if result.PendingApproval == nil {
		return errors.New("queue fixture did not create an Approval")
	}
	_, err = controller.ApplyPendingApproval(ctx, result.PendingApproval.ID)
	return err
}

func (f *controllerQueueFixture) automatic(controller *CurrentNodeController, references ...workflow.CurrentNodeReference) {
	f.t.Helper()
	seen := make(map[workflow.TaskID]bool)
	for _, reference := range references {
		if seen[reference.TaskID] {
			continue
		}
		source, err := f.seed(context.Background(), controller, reference.TaskID, false)
		if err != nil {
			f.t.Fatal(err)
		}
		seen[reference.TaskID] = true
		completed := make(chan error, 1)
		handle := startLiveTestWorkflowScript(f.t, controller, controller.authority, source, sessionruntime.ScriptExecutionRequest{
			Command: sessionruntime.ScriptCommand{Path: "/bin/sh", Args: []string{"-c", "true"}},
			Finalize: func(ctx context.Context, scope sessionruntime.ExecutionScope, _ sessionruntime.ScriptResult, runErr error) error {
				if runErr == nil {
					result, err := controller.CompleteScriptCurrentNode(ctx, workflowruntime.ScriptCompletionRequest{ScopeID: scope.ID(), TransitionID: "split"})
					runErr = err
					if err == nil {
						runErr = result.Continuation.Continue(ctx, nil)
					}
				}
				completed <- runErr
				return runErr
			},
		})
		if err := <-completed; err != nil {
			f.t.Fatal(err)
		}
		if _, err := handle.Wait(context.Background()); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f controllerTaskFixture) branchPlan(t *testing.T) workflowstore.ManualMovePlan {
	t.Helper()
	prepared, err := f.store.PrepareManualMove(context.Background(), workflowstore.ManualMoveRequest{TaskID: f.task.ID, TargetNodeID: f.branches[0]})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.store.PlanManualMove(context.Background(), prepared, f.candidate)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func (f controllerTaskFixture) interruptBranches(t *testing.T, plan workflowstore.ManualMovePlan) []workflow.CurrentNode {
	t.Helper()
	ctx := context.Background()
	for _, node := range f.moveBranches(t, plan) {
		if err := f.store.InterruptCurrentNode(ctx, node.Reference, workflow.CurrentNodeInterruptionReasonUserInterrupt, workflow.NewCurrentNodeInterruptionDetail(string(workflow.CurrentNodeInterruptionReasonUserInterrupt), nil)); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := f.store.ListCurrentNodes(ctx, f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

func (f controllerTaskFixture) moveBranches(t *testing.T, plan workflowstore.ManualMovePlan) []workflow.CurrentNode {
	t.Helper()
	ctx := context.Background()
	var sessions []workflowstore.PlannedCurrentNodeSession
	for _, input := range plan.StartContexts() {
		prepared, err := f.prepareSession(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, *prepared.Session)
	}
	moved, err := f.store.CommitManualMove(ctx, plan, sessions)
	if err != nil {
		t.Fatal(err)
	}
	return moved.Mutation.Created
}

type controllerTaskRunner struct {
	fixture controllerTaskFixture
	prepare func(context.Context, workflowstore.CurrentNodeStartContext, workflowruntime.TaskPromptDelivery) error
	start   func(context.Context, workflow.CurrentNodeReference, workflowruntime.TaskPromptDelivery) (sessionruntime.ExecutionHandle, error)
}

func (r controllerTaskRunner) PrepareCurrentNode(ctx context.Context, input workflowstore.CurrentNodeStartContext, delivery workflowruntime.TaskPromptDelivery) (CurrentNodePreparation, error) {
	if r.prepare != nil {
		if err := r.prepare(ctx, input, delivery); err != nil {
			return CurrentNodePreparation{}, err
		}
	}
	return r.fixture.prepareSession(ctx, input)
}
func (r controllerTaskRunner) PrepareScriptPublication(context.Context, workflow.CurrentNodeReference, workflowruntime.Controller) (CurrentNodeScriptPublication, error) {
	return nil, nil
}
func (r controllerTaskRunner) StartAgentCurrentNode(ctx context.Context, reference workflow.CurrentNodeReference, delivery workflowruntime.TaskPromptDelivery, _ CurrentNodeAssignmentSteer, _ func(), _ workflowruntime.Controller) (sessionruntime.ExecutionHandle, error) {
	if r.start != nil {
		return r.start(ctx, reference, delivery)
	}
	<-ctx.Done()
	return nil, errors.New("controller test startup stopped")
}

func (f controllerTaskFixture) controller(t *testing.T, runner CurrentNodePublicationRunner) *CurrentNodeController {
	t.Helper()
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller, err := NewCurrentNodeController(f.store, runner, authority, f.mutations, CurrentNodeControllerConfig{AgentConcurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Error(err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return controller
}
