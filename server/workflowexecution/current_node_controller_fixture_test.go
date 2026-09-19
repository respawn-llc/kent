package workflowexecution

import (
	"context"
	"database/sql"
	"errors"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/llm"
	"core/server/metadata"
	"core/server/runtimewire"
	"core/server/session"
	"core/server/sessionruntime"
	askquestion "core/server/tools"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func newCurrentNodeControllerForTest(
	t *testing.T,
	store *currentNodeControllerStore,
	runner currentNodeTestRunner,
	authority *sessionruntime.Authority,
	concurrency int,
) *CurrentNodeController {
	return newCurrentNodeControllerWithAttentionForTest(t, store, runner, authority, concurrency, nil)
}

func newCurrentNodeControllerWithAttentionForTest(
	t *testing.T,
	store *currentNodeControllerStore,
	runner currentNodeTestRunner,
	authority *sessionruntime.Authority,
	concurrency int,
	attention CurrentNodeAttentionLifecycle,
) *CurrentNodeController {
	t.Helper()
	controller, err := NewCurrentNodeController(store, currentNodeTestPublicationRunner{
		runner: runner, authority: authority, store: store,
	}, authority, NewTaskMutationCoordinator(), CurrentNodeControllerConfig{
		AgentConcurrency: concurrency,
		Attention:        attention,
	})
	if err != nil {
		t.Fatalf("new current node controller: %v", err)
	}
	return controller
}

func newCurrentNodeControllerWithConfigForTest(
	t *testing.T,
	store *currentNodeControllerStore,
	runner currentNodeTestRunner,
	authority *sessionruntime.Authority,
	mutations *TaskMutationCoordinator,
	cfg CurrentNodeControllerConfig,
) *CurrentNodeController {
	t.Helper()
	controller, err := NewCurrentNodeController(store, currentNodeTestPublicationRunner{
		runner: runner, authority: authority, store: store,
	}, authority, mutations, cfg)
	if err != nil {
		t.Fatalf("new current node controller: %v", err)
	}
	return controller
}

type workflowExecutionStart struct {
	state *workflowExecutionStartState
}

type workflowExecutionStartState struct {
	reference workflow.CurrentNodeReference
	published func(sessionruntime.ExecutionHandle)
	handle    sessionruntime.ExecutionHandle
	onRetire  func()
}

type currentNodeTestRunner interface {
	PublishCurrentNode(
		context.Context,
		workflow.CurrentNodeReference,
		workflowruntime.TaskPromptDelivery,
		CurrentNodeAssignmentSteer,
		workflowExecutionStart,
		workflowruntime.Controller,
	) error
}

type preparingSiblingScriptRunner struct {
	recordingScriptRunner
	preparing workflow.CurrentNodeReference
	entered   chan struct{}
	release   <-chan struct{}
}

func (r *preparingSiblingScriptRunner) PrepareCurrentNode(ctx context.Context, reference workflow.CurrentNodeReference, _ workflowruntime.TaskPromptDelivery) error {
	if !reference.Equal(r.preparing) {
		return nil
	}
	close(r.entered)
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

type currentNodeTestPreparation interface {
	PrepareCurrentNode(context.Context, workflow.CurrentNodeReference, workflowruntime.TaskPromptDelivery) error
}

type currentNodeTestScriptRunner interface {
	UsesScriptPublication(workflow.CurrentNodeReference) bool
}

type currentNodeTestPublicationRunner struct {
	runner    currentNodeTestRunner
	authority *sessionruntime.Authority
	store     *currentNodeControllerStore
}

func (r currentNodeTestPublicationRunner) PrepareCurrentNode(ctx context.Context, input workflowstore.CurrentNodeStartContext, _ workflowruntime.TaskPromptDelivery) (CurrentNodePreparation, error) {
	prepared, err := r.store.queueFixture.prepare(ctx, input)
	if err != nil {
		return CurrentNodePreparation{}, err
	}
	if r.store.assignment != nil && input.Node.Kind == workflow.NodeKindAgent {
		prepared.Assignment, err = r.store.assignment.SteerCurrentNodeAssignment(ctx, input.CurrentNode.Reference)
	}
	return prepared, err
}

func (r currentNodeTestPublicationRunner) nodeKind(reference workflow.CurrentNodeReference) workflow.NodeKind {
	if script, ok := r.runner.(currentNodeTestScriptRunner); ok && script.UsesScriptPublication(reference) {
		return workflow.NodeKindScript
	}
	return workflow.NodeKindAgent
}

func (r currentNodeTestPublicationRunner) PrepareScriptPublication(
	ctx context.Context,
	reference workflow.CurrentNodeReference,
	controller workflowruntime.Controller,
) (CurrentNodeScriptPublication, error) {
	if preparation, ok := r.runner.(CurrentNodeScriptPublicationPreparation); ok {
		return preparation.PrepareScriptPublication(ctx, reference, controller)
	}
	if r.nodeKind(reference) == workflow.NodeKindScript {
		if preparation, ok := r.runner.(currentNodeTestPreparation); ok {
			if err := preparation.PrepareCurrentNode(
				ctx,
				reference,
				workflowruntime.TaskPromptDeliveryAssignment,
			); err != nil {
				return nil, err
			}
		}
		return &currentNodeTestScriptPublication{
			runner: r.runner, authority: r.authority, reference: reference,
			controller: controller,
		}, nil
	}
	return nil, nil
}

func (r currentNodeTestPublicationRunner) StartAgentCurrentNode(
	ctx context.Context,
	reference workflow.CurrentNodeReference,
	delivery workflowruntime.TaskPromptDelivery,
	assignment CurrentNodeAssignmentSteer,
	onRetire func(),
	controller workflowruntime.Controller,
) (sessionruntime.ExecutionHandle, error) {
	if preparation, ok := r.runner.(currentNodeTestPreparation); ok {
		if err := preparation.PrepareCurrentNode(ctx, reference, delivery); err != nil {
			return nil, err
		}
	}
	state := &workflowExecutionStartState{
		reference: reference,
		onRetire:  onRetire,
	}
	if err := r.runner.PublishCurrentNode(
		ctx,
		reference,
		delivery,
		assignment,
		workflowExecutionStart{state: state},
		controller,
	); err != nil {
		return nil, err
	}
	if state.handle == nil {
		shellPath, err := exec.LookPath("sh")
		if err != nil {
			return nil, err
		}
		if _, err := startTestWorkflowScript(r.authority, workflowExecutionStart{state: state}, sessionruntime.ScriptExecutionRequest{
			Command: sessionruntime.ScriptCommand{Path: shellPath, Args: []string{"-c", "exit 0"}},
		}); err != nil {
			return nil, err
		}
	}
	if state.onRetire != nil {
		go func() {
			_, _ = state.handle.Wait(context.Background())
			state.onRetire()
		}()
	}
	return state.handle, nil
}

type currentNodeTestScriptPublication struct {
	runner     currentNodeTestRunner
	authority  *sessionruntime.Authority
	reference  workflow.CurrentNodeReference
	controller workflowruntime.Controller
}

func (p *currentNodeTestScriptPublication) Publish(
	_ context.Context,
	admit func() error,
	published func(sessionruntime.ExecutionHandle),
) (sessionruntime.ExecutionHandle, func(), error) {
	if err := admit(); err != nil {
		return nil, nil, err
	}
	state := &workflowExecutionStartState{
		reference: p.reference,
		published: published,
	}
	if err := p.runner.PublishCurrentNode(
		context.Background(), p.reference, workflowruntime.TaskPromptDeliveryAssignment, nil,
		workflowExecutionStart{state: state}, p.controller,
	); err != nil {
		return nil, nil, err
	}
	if state.handle == nil {
		return nil, nil, errors.New("test Script publication returned no execution handle")
	}
	return state.handle, func() {}, nil
}

func (p *currentNodeTestScriptPublication) Cancel() {}

func startTestWorkflowScript(
	authority *sessionruntime.Authority,
	start workflowExecutionStart,
	request sessionruntime.ScriptExecutionRequest,
) (sessionruntime.ExecutionHandle, error) {
	if start.state == nil {
		return nil, errors.New("test Workflow execution start is required")
	}
	detached, err := authority.PrepareDetachedScriptExecution(context.Background(), sessionruntime.DetachedScriptExecutionRequest{
		Workflow: sessionruntime.WorkflowExecutionRef{
			ProjectID: "project-test", WorkflowID: currentNodeControllerTestWorkflowID,
			CurrentNode: start.state.reference,
		},
		Command: request.Command, Finalize: request.Finalize,
	})
	if err != nil {
		return nil, err
	}
	handle, launch, err := detached.Publish(context.Background(), func() error { return nil }, start.state.published)
	if err == nil {
		start.state.handle = handle
		launch()
	}
	return handle, err
}

func startLiveTestWorkflowScript(
	t *testing.T,
	controller *CurrentNodeController,
	authority *sessionruntime.Authority,
	reference workflow.CurrentNodeReference,
	request sessionruntime.ScriptExecutionRequest,
) sessionruntime.ExecutionHandle {
	t.Helper()
	detached, err := authority.PrepareDetachedScriptExecution(context.Background(), sessionruntime.DetachedScriptExecutionRequest{
		Workflow: sessionruntime.WorkflowExecutionRef{
			ProjectID: "project-test", WorkflowID: currentNodeControllerTestWorkflowID,
			CurrentNode: reference,
		},
		Command: request.Command, Finalize: request.Finalize,
	})
	if err != nil {
		t.Fatalf("prepare detached Script execution: %v", err)
	}
	handle, launch, err := detached.Publish(context.Background(), func() error { return nil }, nil)
	if err != nil {
		t.Fatalf("publish detached Script execution: %v", err)
	}
	launch()
	return handle
}

type completedCurrentNodeAssignmentSteer struct {
	receipt session.CommitReceipt
	err     error
}

func (s completedCurrentNodeAssignmentSteer) Wait(context.Context) (session.CommitReceipt, error) {
	return s.receipt, s.err
}

type recordingCurrentNodeAssignmentSteerer struct {
	mu          sync.Mutex
	steered     []workflow.CurrentNodeReference
	err         error
	waitReceipt session.CommitReceipt
	waitErr     error
}

func (s *recordingCurrentNodeAssignmentSteerer) SteerCurrentNodeAssignment(_ context.Context, reference workflow.CurrentNodeReference) (CurrentNodeAssignmentSteer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steered = append(s.steered, reference)
	if s.err != nil {
		return nil, s.err
	}
	receipt := s.waitReceipt
	if s.waitErr == nil {
		receipt.Committed = true
	}
	return completedCurrentNodeAssignmentSteer{receipt: receipt, err: s.waitErr}, nil
}

func (s *recordingCurrentNodeAssignmentSteerer) references() []workflow.CurrentNodeReference {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]workflow.CurrentNodeReference(nil), s.steered...)
}

func currentNodeReferenceForControllerTest(t *testing.T, taskID string, nodeID string) workflow.CurrentNodeReference {
	t.Helper()
	reference, err := workflow.NewCurrentNodeReference(workflow.TaskID(taskID), workflow.NodeID(nodeID), nil)
	if err != nil {
		t.Fatalf("new current node reference: %v", err)
	}
	return reference
}

func singleLiveScope(t *testing.T, authority *sessionruntime.Authority, reference workflow.CurrentNodeReference) runtimeids.ExecutionScopeID {
	t.Helper()
	handle, live := authority.ExecutionByCurrentNode("project-test", currentNodeControllerTestWorkflowID, reference)
	if live {
		return handle.Scope().ID()
	}
	t.Fatalf("no live execution for %v", reference)
	return runtimeids.ExecutionScopeID{}
}

func hasLiveCurrentNode(authority *sessionruntime.Authority, reference workflow.CurrentNodeReference) bool {
	_, live := authority.ExecutionByCurrentNode("project-test", currentNodeControllerTestWorkflowID, reference)
	return live
}

func waitForRunningCurrentNode(
	t *testing.T,
	authority *sessionruntime.Authority,
	reference workflow.CurrentNodeReference,
) {
	t.Helper()
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		snapshots, err := authority.CurrentWorkflowTaskExecutionSnapshots()
		if err != nil {
			return false
		}
		for _, execution := range snapshots[reference.TaskID].Executions {
			if execution.Ref.CurrentNode.Equal(reference) &&
				!execution.Queued &&
				len(execution.PendingPrompts) == 0 {
				return true
			}
		}
		return false
	}, "current node %v did not begin running", reference)
}

var currentNodeControllerTestWorkflowID = func() runtimeids.WorkflowID {
	workflowID, err := runtimeids.ParseWorkflowID("550e8400-e29b-41d4-a716-446655440201")
	if err != nil {
		panic(err)
	}
	return workflowID
}()

type currentNodeControllerStore struct {
	*workflowstore.Store
	queueFixture *controllerQueueFixture
	assignment   interface {
		SteerCurrentNodeAssignment(context.Context, workflow.CurrentNodeReference) (CurrentNodeAssignmentSteer, error)
	}
	mu                    sync.Mutex
	interrupted           []workflow.CurrentNode
	currentNodes          []workflow.CurrentNode
	admitted              []workflow.CurrentNodeReference
	admitStarted          chan struct{}
	admitRelease          chan struct{}
	admitSawCancellation  bool
	resumeClassifications []workflowstore.CurrentNodeResumeClassification
	preflightResumeCalls  int
	interruptions         map[workflow.CurrentNodeReferenceKey]currentNodeInterruptionRecord
	interruptionCalls     map[workflow.CurrentNodeReferenceKey]int
	completions           int
	sessionTaskID         *workflow.TaskID
	sessionAssociation    *workflowstore.TaskSessionAssociation
	bindingErr            error
	bindings              []currentNodeSessionBindingCall
	interruptStarted      chan struct{}
	interruptRelease      chan struct{}
	interruptOnce         sync.Once
	interruptionErr       error
	idleResolved          *workflow.CurrentNode
	idleResolvedSequence  []workflow.CurrentNode
}

type currentNodeAttentionRecorder struct {
	mu          sync.Mutex
	pending     []workflow.CurrentNodeReference
	resolutions []workflowstore.TaskAttentionResolution
}

func (r *currentNodeAttentionRecorder) PublishPendingInterruptedCurrentNode(_ context.Context, reference workflow.CurrentNodeReference) {
	r.mu.Lock()
	r.pending = append(r.pending, reference)
	r.mu.Unlock()
}

func (r *currentNodeAttentionRecorder) FinalizeTaskResolution(resolution workflowstore.TaskAttentionResolution) {
	r.mu.Lock()
	r.resolutions = append(r.resolutions, resolution)
	r.mu.Unlock()
}

func (r *currentNodeAttentionRecorder) pendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

func (*currentNodeControllerStore) TaskExecutionScope(context.Context, workflow.TaskID) (workflowstore.TaskExecutionScope, error) {
	return workflowstore.TaskExecutionScope{ProjectID: "project-test", WorkflowID: currentNodeControllerTestWorkflowID}, nil
}

func (s *currentNodeControllerStore) ListCurrentNodes(ctx context.Context, taskID workflow.TaskID) ([]workflow.CurrentNode, error) {
	if s.queueFixture != nil {
		return s.Store.ListCurrentNodes(ctx, taskID)
	}
	if s.currentNodes != nil {
		return append([]workflow.CurrentNode(nil), s.currentNodes...), nil
	}
	return append([]workflow.CurrentNode(nil), s.interrupted...), nil
}

func (s *currentNodeControllerStore) PreflightTaskResume(ctx context.Context, taskID workflow.TaskID) ([]workflowstore.CurrentNodeResumeClassification, error) {
	if s.queueFixture != nil {
		return s.Store.PreflightTaskResume(ctx, taskID)
	}
	s.preflightResumeCalls++
	if len(s.resumeClassifications) > 0 {
		return append([]workflowstore.CurrentNodeResumeClassification(nil), s.resumeClassifications...), nil
	}
	classifications := make([]workflowstore.CurrentNodeResumeClassification, 0, len(s.interrupted))
	for _, currentNode := range s.interrupted {
		classification := workflowstore.CurrentNodeResumeClassification{CurrentNode: currentNode}
		classifications = append(classifications, classification)
	}
	return classifications, nil
}

func (s *currentNodeControllerStore) PendingApproval(ctx context.Context, id workflow.ApprovalID) (workflow.PendingApproval, error) {
	return s.Store.PendingApproval(ctx, id)
}

type currentNodeInterruptionRecord struct {
	reason workflow.CurrentNodeInterruptionReason
	detail workflow.CurrentNodeInterruptionDetail
}

type currentNodeSessionBindingCall struct {
	sessionID runtimeids.SessionID
	reference workflow.CurrentNodeReference
}

func (s *currentNodeControllerStore) AdmitCurrentNode(ctx context.Context, reference workflow.CurrentNodeReference) (session.CommitReceipt, error) {
	if s.admitStarted != nil {
		close(s.admitStarted)
	}
	if s.admitRelease != nil {
		<-s.admitRelease
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.admitSawCancellation = context.Cause(ctx) != nil
	s.admitted = append(s.admitted, reference)
	if s.Store != nil {
		return s.Store.AdmitCurrentNode(ctx, reference)
	}
	return session.CommitReceipt{Committed: true}, nil
}

func (s *currentNodeControllerStore) InterruptAdmittedCurrentNode(ctx context.Context, reference workflow.CurrentNodeReference, reason workflow.CurrentNodeInterruptionReason, detail workflow.CurrentNodeInterruptionDetail) error {
	if s.interruptionErr != nil {
		return s.interruptionErr
	}
	key, err := reference.Key()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.interruptions == nil {
		s.interruptions = make(map[workflow.CurrentNodeReferenceKey]currentNodeInterruptionRecord)
	}
	if s.interruptionCalls == nil {
		s.interruptionCalls = make(map[workflow.CurrentNodeReferenceKey]int)
	}
	s.interruptions[key] = currentNodeInterruptionRecord{reason: reason, detail: detail}
	s.interruptionCalls[key]++
	if s.queueFixture != nil {
		return s.Store.InterruptCurrentNode(ctx, reference, reason, detail)
	}
	return nil
}

func (s *currentNodeControllerStore) InterruptCurrentNode(ctx context.Context, reference workflow.CurrentNodeReference, reason workflow.CurrentNodeInterruptionReason, detail workflow.CurrentNodeInterruptionDetail) error {
	if s.interruptStarted != nil {
		s.interruptOnce.Do(func() {
			close(s.interruptStarted)
		})
	}
	if s.interruptRelease != nil {
		select {
		case <-s.interruptRelease:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	return s.InterruptAdmittedCurrentNode(ctx, reference, reason, detail)
}

func (s *currentNodeControllerStore) ReconcileTaskResume(ctx context.Context, taskID workflow.TaskID) error {
	if s.queueFixture != nil {
		return s.Store.ReconcileTaskResume(ctx, taskID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, node := range s.currentNodes {
		if node.Reference.TaskID != taskID || node.Scheduling == nil {
			continue
		}
		switch node.Scheduling.State {
		case workflow.CurrentNodeSchedulingReady, workflow.CurrentNodeSchedulingAdmitted:
			node.Scheduling = &workflow.CurrentNodeScheduling{State: workflow.CurrentNodeSchedulingInterrupted}
			s.currentNodes[index] = node
			s.interrupted = append(s.interrupted, node)
		}
	}
	return nil
}

func (s *currentNodeControllerStore) ResolveIdleExecutableCurrentNode(context.Context, workflowstore.IdleCurrentNodeSelector) (workflow.CurrentNode, error) {
	if len(s.idleResolvedSequence) != 0 {
		resolved := s.idleResolvedSequence[0]
		s.idleResolvedSequence = s.idleResolvedSequence[1:]
		return resolved, nil
	}
	if s.idleResolved == nil {
		return workflow.CurrentNode{}, sql.ErrNoRows
	}
	return *s.idleResolved, nil
}

func (s *currentNodeControllerStore) CommitCurrentNodeCompletion(ctx context.Context, plan workflowstore.CurrentNodeCompletionPlan, sessions []workflowstore.PlannedCurrentNodeSession) (workflowstore.CurrentNodeCompletionOutcome, error) {
	s.mu.Lock()
	s.completions++
	s.mu.Unlock()
	return s.Store.CommitCurrentNodeCompletion(ctx, plan, sessions)
}

func (s *currentNodeControllerStore) ValidateCurrentNodeSessionBinding(ctx context.Context, sessionID runtimeids.SessionID, reference workflow.CurrentNodeReference) error {
	if s.queueFixture != nil {
		return s.Store.ValidateCurrentNodeSessionBinding(ctx, sessionID, reference)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings = append(s.bindings, currentNodeSessionBindingCall{sessionID: sessionID, reference: reference})
	return s.bindingErr
}

func (s *currentNodeControllerStore) ResolveCurrentSessionStartContext(
	ctx context.Context,
	sessionID runtimeids.SessionID,
) (workflowstore.CurrentNodeStartContext, error) {
	if s.queueFixture != nil {
		return s.Store.ResolveCurrentSessionStartContext(ctx, sessionID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionTaskID == nil || s.sessionAssociation == nil ||
		s.sessionAssociation.SessionID != sessionID {
		return workflowstore.CurrentNodeStartContext{}, workflowstore.ErrSessionNotCurrentWorkflowNode
	}
	for _, currentNode := range s.interrupted {
		if currentNode.Reference.Equal(s.sessionAssociation.CurrentNode) {
			return workflowstore.CurrentNodeStartContext{
				Task:        workflowstore.TaskRecord{ID: *s.sessionTaskID},
				CurrentNode: currentNode,
			}, nil
		}
	}
	for _, currentNode := range s.currentNodes {
		if currentNode.Reference.Equal(s.sessionAssociation.CurrentNode) {
			return workflowstore.CurrentNodeStartContext{
				Task:        workflowstore.TaskRecord{ID: *s.sessionTaskID},
				CurrentNode: currentNode,
			}, nil
		}
	}
	return workflowstore.CurrentNodeStartContext{}, workflowstore.ErrSessionNotCurrentWorkflowNode
}

func (s *currentNodeControllerStore) admitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.admitted)
}

func (s *currentNodeControllerStore) interruption(reference workflow.CurrentNodeReference) (currentNodeInterruptionRecord, bool) {
	key, err := reference.Key()
	if err != nil {
		return currentNodeInterruptionRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.interruptions[key]
	return value, ok
}

func (s *currentNodeControllerStore) interruptionCount(reference workflow.CurrentNodeReference) int {
	key, err := reference.Key()
	if err != nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interruptionCalls[key]
}

func (s *currentNodeControllerStore) setBindingError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindingErr = err
}

func (s *currentNodeControllerStore) bindingCalls() []currentNodeSessionBindingCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]currentNodeSessionBindingCall(nil), s.bindings...)
}

func (s *currentNodeControllerStore) completionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completions
}

type currentNodeQuestionFixture struct {
	cfg        config.App
	metadata   *metadata.Store
	authority  *sessionruntime.Authority
	controller *CurrentNodeController
	store      *currentNodeControllerStore
	sessionDir string
}

type currentNodePendingPrompt struct {
	handle    sessionruntime.ExecutionHandle
	sessionID runtimeids.SessionID
	result    <-chan currentNodePromptResult
}

type currentNodePromptResult struct {
	resolution askquestion.AskQuestionResolution
	err        error
}

func currentNodeQuestionAnswer(answer string) askquestion.AskQuestionAnswer {
	return askquestion.AskQuestionAnswer{
		Freeform: &answer,
	}
}

func (f currentNodeQuestionFixture) answerPromptBatch(
	ctx context.Context,
	pending currentNodePendingPrompt,
	stepID string,
	toolCallID string,
	answer askquestion.AskQuestionAnswer,
) (sessionruntime.PromptAnswerOutcome, error) {
	parsedStepID, err := runtimeids.ParseStepID(stepID)
	if err != nil {
		return "", err
	}
	results, err := f.authority.ResolvePromptBatch(ctx, pending.sessionID, parsedStepID, []sessionruntime.PromptAnswerCommand{{
		ToolCallID: clientui.ToolCallID(toolCallID),
		Payload:    sessionruntime.PromptQuestionAnswerCommand{Answer: answer},
	}})
	if err != nil {
		return "", err
	}
	if len(results) != 1 {
		return "", errors.New("one prompt answer result is required")
	}
	return results[0].Outcome, nil
}

type currentNodeQuestionLLMClient struct{}

func (currentNodeQuestionLLMClient) Generate(context.Context, llm.Request, llm.StreamCallbacks) (llm.Response, error) {
	return llm.Response{}, errors.New("question fixture model must not generate")
}

func (currentNodeQuestionLLMClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{
		ProviderID:           "openai",
		SupportsResponsesAPI: true,
		IsOpenAIFirstParty:   true,
	}, nil
}

type callbackCurrentNodeLLMClient struct {
	generate func(context.Context, llm.Request) (llm.Response, error)
}

func (c callbackCurrentNodeLLMClient) Generate(ctx context.Context, request llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	return c.generate(ctx, request)
}

func (callbackCurrentNodeLLMClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{
		ProviderID:           "openai",
		SupportsResponsesAPI: true,
		IsOpenAIFirstParty:   true,
	}, nil
}

func newCurrentNodeQuestionFixture(t *testing.T) currentNodeQuestionFixture {
	return newCurrentNodeQuestionFixtureWithPromptFeed(t, nil)
}

func newCurrentNodeQuestionFixtureWithPromptFeed(
	t *testing.T,
	promptFeed sessionruntime.ExecutionPromptFeed,
) currentNodeQuestionFixture {
	t.Helper()
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	appCfg, err := config.Load(workspace, workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	metadataStore := testsetup.OpenStore(t, appCfg.PersistenceRoot)
	binding, err := metadataStore.RegisterWorkspaceBinding(context.Background(), appCfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	store := &currentNodeControllerStore{}
	var controller *CurrentNodeController
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot: appCfg.PersistenceRoot,
		StoreOptions:    metadataStore.AuthoritativeSessionStoreOptions(),
		PromptFeed:      promptFeed,
	})
	controller = newCurrentNodeControllerForTest(t, store, &countingCurrentNodeRunner{}, authority, 1)
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("close controller: %v", err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Errorf("close authority: %v", err)
		}
	})
	return currentNodeQuestionFixture{
		cfg:        appCfg,
		metadata:   metadataStore,
		authority:  authority,
		controller: controller,
		store:      store,
		sessionDir: filepath.Join(appCfg.PersistenceRoot, "projects", binding.ProjectID, "sessions"),
	}
}

func (f currentNodeQuestionFixture) startPendingPrompt(t *testing.T, reference workflow.CurrentNodeReference, request askquestion.AskQuestionRequest) currentNodePendingPrompt {
	t.Helper()
	result := make(chan currentNodePromptResult, 1)
	handle, sessionID := f.startQuestionExecution(t, reference, func(ctx context.Context, scope sessionruntime.ExecutionScope, _ sessionruntime.AgentRuntimeBridge) error {
		resolution, askErr := f.authority.AwaitPromptResolution(ctx, scope.ID(), request)
		result <- currentNodePromptResult{resolution: resolution, err: askErr}
		return askErr
	})
	return currentNodePendingPrompt{handle: handle, sessionID: sessionID, result: result}
}

func (f currentNodeQuestionFixture) startQuestionExecution(
	t *testing.T,
	reference workflow.CurrentNodeReference,
	runner sessionruntime.AgentRunner,
) (sessionruntime.ExecutionHandle, runtimeids.SessionID) {
	handle, sessionID, _ := f.startAgentExecution(t, reference, runner)
	return handle, sessionID
}

func (f currentNodeQuestionFixture) startAgentExecution(
	t *testing.T,
	reference workflow.CurrentNodeReference,
	runner sessionruntime.AgentRunner,
) (sessionruntime.ExecutionHandle, runtimeids.SessionID, runtimeids.SessionResourceRef) {
	return f.startAgentExecutionWithClient(t, reference, currentNodeQuestionLLMClient{}, runner)
}

func (f currentNodeQuestionFixture) startAgentExecutionWithClient(
	t *testing.T,
	reference workflow.CurrentNodeReference,
	client llm.Client,
	runner sessionruntime.AgentRunner,
) (sessionruntime.ExecutionHandle, runtimeids.SessionID, runtimeids.SessionResourceRef) {
	t.Helper()
	store, err := session.Create(
		f.sessionDir,
		filepath.Base(f.sessionDir),
		f.cfg.WorkspaceRoot,
		sessioncontract.SessionCategorySubagent,
		f.metadata.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	return f.startAgentExecutionForSession(t, reference, sessionID, client, runner)
}

func (f currentNodeQuestionFixture) startAgentExecutionForSession(
	t *testing.T,
	reference workflow.CurrentNodeReference,
	sessionID runtimeids.SessionID,
	client llm.Client,
	runner sessionruntime.AgentRunner,
) (sessionruntime.ExecutionHandle, runtimeids.SessionID, runtimeids.SessionResourceRef) {
	t.Helper()
	descriptor, err := session.NewOpenSessionDescriptor(sessionID)
	if err != nil {
		t.Fatalf("NewOpenSessionDescriptor: %v", err)
	}
	settings := f.cfg.Settings
	settings.Model = "gpt-5"
	settings.ModelContextWindow = 200_000
	settings.Reviewer.Frequency = "off"
	plan, err := sessionruntime.NewAgentRuntimePlan(sessionruntime.AgentRuntimePlanOptions{
		MainWorkspaceRoot:     f.cfg.WorkspaceRoot,
		Settings:              settings,
		QuestionsEnabled:      textutil.Value(true),
		AutoCompactionEnabled: textutil.Value(true),
		FilesystemContext: func() askquestion.FilesystemContext {
			context, err := runtimewire.NewFilesystemContext(f.cfg.WorkspaceRoot, f.cfg.WorkspaceRoot, metadata.ProjectWorkspaceBoundary{ProjectID: "test"})
			if err != nil {
				t.Fatalf("NewFilesystemContext: %v", err)
			}
			return context
		}(),
		Client: client,
	})
	if err != nil {
		t.Fatalf("NewAgentRuntimePlan: %v", err)
	}
	handle, err := f.authority.StartAgentExecution(context.Background(), sessionruntime.AgentExecutionRequest{
		Descriptor: descriptor,
		Runtime:    &plan,
		Workflow: &sessionruntime.WorkflowAgentExecution{
			Reference: sessionruntime.WorkflowExecutionRef{
				ProjectID: "project-test", WorkflowID: currentNodeControllerTestWorkflowID,
				CurrentNode: reference,
			},
			Config: &workflowruntime.CurrentNodeExecutionConfig{
				Contract: workflowruntime.CompletionContract{
					Transitions: []workflowruntime.CompletionTransition{{ID: "next"}},
				},
				CompletionMode: workflowruntime.CompletionModeTool,
				Controller:     f.controller,
				Instructions:   workflowruntime.TaskInstructions{CurrentNode: reference},
			},
		},
		Resource: sessionruntime.OpenAgentResource{},
		Runner:   runner,
	})
	if err != nil {
		t.Fatalf("StartAgentExecution: %v", err)
	}
	scope := handle.Scope()
	resource, ok := scope.Resource()
	if !ok {
		t.Fatal("detached Agent execution has no Session Resource")
	}
	return handle, sessionID, resource
}

func (f currentNodeQuestionFixture) waitForPendingPrompt(t *testing.T, taskID workflow.TaskID, askID string) {
	t.Helper()
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		return f.pendingPromptCount(taskID, askID) >= 1
	}, "timed out waiting for workflow prompt %q on task %q", askID, taskID)
}

func (f currentNodeQuestionFixture) waitForAmbiguousPendingPrompt(t *testing.T, taskID workflow.TaskID, askID string) {
	t.Helper()
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		return f.pendingPromptCount(taskID, askID) >= 2
	}, "timed out waiting for ambiguous workflow prompt %q on task %q", askID, taskID)
}

func (f currentNodeQuestionFixture) pendingPromptCount(taskID workflow.TaskID, toolCallID string) int {
	snapshots, err := f.authority.CurrentWorkflowTaskExecutionSnapshots()
	if err != nil {
		return 0
	}
	snapshot, exists := snapshots[taskID]
	if !exists {
		return 0
	}
	count := 0
	for _, execution := range snapshot.Executions {
		for _, pending := range execution.PendingPrompts {
			if string(pending.ToolCallID) == toolCallID {
				count++
			}
		}
	}
	return count
}

type controlledScriptRunner struct {
	authority   *sessionruntime.Authority
	command     sessionruntime.ScriptCommand
	entered     chan struct{}
	startRunner chan struct{}
	registered  chan struct{}
	returnStart chan struct{}
	handles     chan sessionruntime.ExecutionHandle
}

func (*controlledScriptRunner) UsesScriptPublication(workflow.CurrentNodeReference) bool { return true }

type controlledScriptPublication struct {
	detached    *sessionruntime.DetachedScriptExecution
	registered  chan struct{}
	returnStart chan struct{}
	handles     chan sessionruntime.ExecutionHandle
}

func (p *controlledScriptPublication) Publish(
	ctx context.Context,
	admit func() error,
	published func(sessionruntime.ExecutionHandle),
) (sessionruntime.ExecutionHandle, func(), error) {
	close(p.registered)
	<-p.returnStart
	handle, launch, err := p.detached.Publish(ctx, admit, published)
	if err == nil {
		p.handles <- handle
	}
	return handle, launch, err
}

func (p *controlledScriptPublication) Cancel() {
	if p != nil {
		p.detached.Cancel()
	}
}

func (r *controlledScriptRunner) PrepareScriptPublication(
	ctx context.Context,
	reference workflow.CurrentNodeReference,
	_ workflowruntime.Controller,
) (CurrentNodeScriptPublication, error) {
	close(r.entered)
	<-r.startRunner
	detached, err := r.authority.PrepareDetachedScriptExecution(ctx, sessionruntime.DetachedScriptExecutionRequest{
		Workflow: sessionruntime.WorkflowExecutionRef{
			ProjectID:   "project-test",
			WorkflowID:  currentNodeControllerTestWorkflowID,
			CurrentNode: reference,
		},
		Command: r.command,
	})
	if err != nil {
		return nil, err
	}
	return &controlledScriptPublication{
		detached: detached, registered: r.registered,
		returnStart: r.returnStart, handles: r.handles,
	}, nil
}

func (r *controlledScriptRunner) PublishCurrentNode(
	context.Context,
	workflow.CurrentNodeReference,
	workflowruntime.TaskPromptDelivery,
	CurrentNodeAssignmentSteer,
	workflowExecutionStart,
	workflowruntime.Controller,
) error {
	return errors.New("generic Current Node publication must not be used for detached Script publication")
}

type failingCurrentNodeRunner struct {
	cause error
}

func (r failingCurrentNodeRunner) PrepareCurrentNode(context.Context, workflow.CurrentNodeReference, workflowruntime.TaskPromptDelivery) error {
	return r.cause
}

func (r failingCurrentNodeRunner) PublishCurrentNode(context.Context, workflow.CurrentNodeReference, workflowruntime.TaskPromptDelivery, CurrentNodeAssignmentSteer, workflowExecutionStart, workflowruntime.Controller) error {
	return nil
}

type blockingCurrentNodeRunner struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	cause   error
}

func (r *blockingCurrentNodeRunner) PublishCurrentNode(context.Context, workflow.CurrentNodeReference, workflowruntime.TaskPromptDelivery, CurrentNodeAssignmentSteer, workflowExecutionStart, workflowruntime.Controller) error {
	return nil
}

func (r *blockingCurrentNodeRunner) PrepareCurrentNode(context.Context, workflow.CurrentNodeReference, workflowruntime.TaskPromptDelivery) error {
	r.once.Do(func() {
		close(r.entered)
	})
	<-r.release
	if r.cause != nil {
		return r.cause
	}
	return errors.New("blocked current node setup released")
}

type countingCurrentNodeRunner struct {
	mu         sync.Mutex
	count      int
	deliveries []workflowruntime.TaskPromptDelivery
}

func (r *countingCurrentNodeRunner) PublishCurrentNode(_ context.Context, _ workflow.CurrentNodeReference, delivery workflowruntime.TaskPromptDelivery, _ CurrentNodeAssignmentSteer, _ workflowExecutionStart, _ workflowruntime.Controller) error {
	return nil
}

func (r *countingCurrentNodeRunner) PrepareCurrentNode(_ context.Context, _ workflow.CurrentNodeReference, delivery workflowruntime.TaskPromptDelivery) error {
	r.mu.Lock()
	r.count++
	r.deliveries = append(r.deliveries, delivery)
	r.mu.Unlock()
	return nil
}

func (r *countingCurrentNodeRunner) starts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

func (r *countingCurrentNodeRunner) promptDeliveries() []workflowruntime.TaskPromptDelivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]workflowruntime.TaskPromptDelivery(nil), r.deliveries...)
}

type recordingScriptRunner struct {
	authority *sessionruntime.Authority
	command   sessionruntime.ScriptCommand
	started   chan workflow.CurrentNodeReference
	scripts   map[workflow.CurrentNodeReference]struct{}
	agents    map[workflow.CurrentNodeReference]struct{}
}

func (r *recordingScriptRunner) UsesScriptPublication(reference workflow.CurrentNodeReference) bool {
	if _, ok := r.agents[reference]; ok {
		return false
	}
	if len(r.scripts) == 0 {
		return true
	}
	_, ok := r.scripts[reference]
	return ok
}

type firstAdmissionBlockingScriptRunner struct {
	authority *sessionruntime.Authority
	shellPath string
	entered   chan workflow.CurrentNodeReference
	release   chan struct{}
}

func (*firstAdmissionBlockingScriptRunner) UsesScriptPublication(workflow.CurrentNodeReference) bool {
	return true
}

type parallelExplicitRunner struct {
	authority      *sessionruntime.Authority
	shellPath      string
	blocked        workflow.CurrentNodeReference
	blockedEntered chan struct{}
	releaseBlocked chan struct{}
	siblingStarted chan workflow.CurrentNodeReference
	blockedOnce    sync.Once
}

func (*parallelExplicitRunner) UsesScriptPublication(workflow.CurrentNodeReference) bool { return true }

type boundedExplicitAdmissionRunner struct {
	entered chan workflow.CurrentNodeReference
	release chan struct{}
}

func (r *boundedExplicitAdmissionRunner) PublishCurrentNode(
	_ context.Context,
	reference workflow.CurrentNodeReference,
	_ workflowruntime.TaskPromptDelivery,
	_ CurrentNodeAssignmentSteer,
	_ workflowExecutionStart,
	_ workflowruntime.Controller,
) error {
	return nil
}

func (r *boundedExplicitAdmissionRunner) PrepareCurrentNode(
	_ context.Context,
	reference workflow.CurrentNodeReference,
	_ workflowruntime.TaskPromptDelivery,
) error {
	r.entered <- reference
	<-r.release
	return errors.New("explicit admission setup released")
}

type runningAndQueuedGateRunner struct {
	authority      *sessionruntime.Authority
	shellPath      string
	running        workflow.CurrentNodeReference
	runningStarted chan struct{}
	runningOnce    sync.Once
}

func (*runningAndQueuedGateRunner) UsesScriptPublication(workflow.CurrentNodeReference) bool {
	return false
}

type runningAndFinalizingScriptRunner struct {
	authority           *sessionruntime.Authority
	shellPath           string
	running             workflow.CurrentNodeReference
	finalizing          workflow.CurrentNodeReference
	finalizerEntered    chan struct{}
	releaseFinalizer    chan struct{}
	finalizerCompletion chan error
	successorStarted    chan struct{}
	finalizerOnce       sync.Once
	successorOnce       sync.Once
	finalize            func(context.Context, sessionruntime.ExecutionScope, *CurrentNodeController) error
	runningRetirement   <-chan struct{}
}

func (*runningAndFinalizingScriptRunner) UsesScriptPublication(workflow.CurrentNodeReference) bool {
	return true
}

func (r *runningAndFinalizingScriptRunner) PublishCurrentNode(
	_ context.Context,
	reference workflow.CurrentNodeReference,
	_ workflowruntime.TaskPromptDelivery,
	_ CurrentNodeAssignmentSteer,
	lease workflowExecutionStart,
	controller workflowruntime.Controller,
) error {
	switch {
	case reference.Equal(r.running):
		_, err := startTestWorkflowScript(r.authority, lease, sessionruntime.ScriptExecutionRequest{

			Command: sessionruntime.ScriptCommand{
				Path: r.shellPath,
				Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
			},
			Finalize: func(context.Context, sessionruntime.ExecutionScope, sessionruntime.ScriptResult, error) error {
				if r.runningRetirement != nil {
					<-r.runningRetirement
				}
				return nil
			},
		})
		return err
	case reference.Equal(r.finalizing):
		_, err := startTestWorkflowScript(r.authority, lease, sessionruntime.ScriptExecutionRequest{

			Command: sessionruntime.ScriptCommand{Path: r.shellPath, Args: []string{"-c", "exit 0"}},
			Finalize: func(ctx context.Context, scope sessionruntime.ExecutionScope, _ sessionruntime.ScriptResult, runErr error) error {
				if runErr != nil {
					r.finalizerCompletion <- runErr
					return runErr
				}
				r.finalizerOnce.Do(func() {
					close(r.finalizerEntered)
				})
				<-r.releaseFinalizer
				var completionErr error
				if r.finalize != nil {
					currentController, ok := controller.(*CurrentNodeController)
					if !ok {
						completionErr = errors.New("finalizing Script controller is not CurrentNodeController")
					} else {
						completionErr = r.finalize(ctx, scope, currentController)
					}
				} else {
					result, err := controller.CompleteScriptCurrentNode(ctx, workflowruntime.ScriptCompletionRequest{
						ScopeID:      scope.ID(),
						TransitionID: "branch_1_done",
					})
					completionErr = err
					if err == nil {
						completionErr = result.Continuation.Continue(ctx, nil)
					}
				}
				r.finalizerCompletion <- completionErr
				return completionErr
			},
		})
		return err
	default:
		r.successorOnce.Do(func() {
			r.successorStarted <- struct{}{}
		})
		_, err := startTestWorkflowScript(r.authority, lease, sessionruntime.ScriptExecutionRequest{

			Command: sessionruntime.ScriptCommand{
				Path: r.shellPath,
				Args: []string{"-c", "trap 'exit 0' TERM; while :; do sleep 1; done"},
			},
		})
		return err
	}
}

func (r *runningAndQueuedGateRunner) PublishCurrentNode(
	_ context.Context,
	reference workflow.CurrentNodeReference,
	_ workflowruntime.TaskPromptDelivery,
	_ CurrentNodeAssignmentSteer,
	lease workflowExecutionStart,
	_ workflowruntime.Controller,
) error {
	_, err := startTestWorkflowScript(r.authority, lease, sessionruntime.ScriptExecutionRequest{

		Command: sessionruntime.ScriptCommand{
			Path: r.shellPath,
			Args: []string{"-c", "while :; do sleep 1; done"},
		},
	})
	if err != nil {
		return err
	}
	if reference.Equal(r.running) {
		r.runningOnce.Do(func() {
			close(r.runningStarted)
		})
	}
	return nil
}

func (r *parallelExplicitRunner) PublishCurrentNode(
	_ context.Context,
	reference workflow.CurrentNodeReference,
	_ workflowruntime.TaskPromptDelivery,
	_ CurrentNodeAssignmentSteer,
	lease workflowExecutionStart,
	_ workflowruntime.Controller,
) error {
	_, err := startTestWorkflowScript(r.authority, lease, sessionruntime.ScriptExecutionRequest{
		Command: sessionruntime.ScriptCommand{
			Path: r.shellPath,
			Args: []string{"-c", "while :; do sleep 1; done"},
		},
	})
	if err == nil {
		r.siblingStarted <- reference
	}
	return err
}

func (r *parallelExplicitRunner) PrepareCurrentNode(
	_ context.Context,
	reference workflow.CurrentNodeReference,
	_ workflowruntime.TaskPromptDelivery,
) error {
	if reference.Equal(r.blocked) {
		r.blockedOnce.Do(func() {
			close(r.blockedEntered)
		})
		<-r.releaseBlocked
		return errors.New("first branch setup failed")
	}
	return nil
}

func (r *firstAdmissionBlockingScriptRunner) PublishCurrentNode(_ context.Context, reference workflow.CurrentNodeReference, _ workflowruntime.TaskPromptDelivery, _ CurrentNodeAssignmentSteer, lease workflowExecutionStart, _ workflowruntime.Controller) error {
	_, err := startTestWorkflowScript(r.authority, lease, sessionruntime.ScriptExecutionRequest{

		Command: sessionruntime.ScriptCommand{Path: r.shellPath, Args: []string{"-c", "while :; do sleep 1; done"}},
	})
	return err
}

func (r *firstAdmissionBlockingScriptRunner) PrepareCurrentNode(_ context.Context, reference workflow.CurrentNodeReference, _ workflowruntime.TaskPromptDelivery) error {
	r.entered <- reference
	<-r.release
	return nil
}

func (r *recordingScriptRunner) PublishCurrentNode(_ context.Context, reference workflow.CurrentNodeReference, _ workflowruntime.TaskPromptDelivery, _ CurrentNodeAssignmentSteer, lease workflowExecutionStart, _ workflowruntime.Controller) error {
	_, err := startTestWorkflowScript(r.authority, lease, sessionruntime.ScriptExecutionRequest{

		Command: r.command,
	})
	if err == nil {
		r.started <- reference
	}
	return err
}
