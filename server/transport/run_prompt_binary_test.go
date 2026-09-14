package transport

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/core"
	"core/server/session"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/apicontract"
	remoteclient "core/shared/client"
	"core/shared/protoapi"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/rpcwire"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"google.golang.org/protobuf/types/known/durationpb"
)

type controlledRunPrompt struct {
	release   <-chan struct{}
	sessionID string
}

func (s controlledRunPrompt) RunPrompt(ctx context.Context, _ serverapi.RunPromptRequest, sink serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	sink.PublishRunPromptProgress(&runpromptpb.ProgressEvent{Payload: &runpromptpb.ProgressEvent_AssistantMessage{
		AssistantMessage: &runpromptpb.AssistantMessage{Phase: runpromptpb.MessagePhase_MESSAGE_PHASE_COMMENTARY, Content: "partial answer"},
	}})
	select {
	case <-s.release:
		return &runpromptpb.Success{SessionId: s.sessionID, SessionName: "Session", Result: "final answer", Duration: durationpb.New(time.Millisecond)}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type runPromptTestDependencies struct {
	GatewayDependencies
	run apicontract.RunPromptService
}

type runtimeControlTestDependencies struct {
	GatewayDependencies
	runtime apicontract.RuntimeControlService
}

func (d runtimeControlTestDependencies) RuntimeControlClient() apicontract.RuntimeControlService {
	return d.runtime
}

func (d runPromptTestDependencies) RunPromptClientForProjectWorkspace(context.Context, string, string) (apicontract.RunPromptService, error) {
	return d.run, nil
}

func (d runPromptTestDependencies) RunPromptClientForProjectWorkspaceID(context.Context, string, string) (apicontract.RunPromptService, error) {
	return d.run, nil
}

type runPromptWireFrames struct {
	mu     sync.Mutex
	frames []rpcwire.Frame
}

type observedRunPromptConn struct {
	rpcwire.Conn
	wire *runPromptWireFrames
}

func (c observedRunPromptConn) Send(ctx context.Context, frame rpcwire.Frame) error {
	c.wire.mu.Lock()
	c.wire.frames = append(c.wire.frames, frame)
	c.wire.mu.Unlock()
	return c.Conn.Send(ctx, frame)
}

func TestRunPromptBinaryDeliversProgressBeforeFinalAnswer(t *testing.T) {
	core, _ := newGatewayTestCore(t, true, true)
	defer core.Close()
	release := make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	gateway, err := NewGateway(runPromptTestDependencies{
		GatewayDependencies: core,
		run:                 controlledRunPrompt{release: release, sessionID: runtimeids.NewSessionID().String()},
	}, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	wire := &runPromptWireFrames{}
	server := httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(func(ctx context.Context, conn rpcwire.Conn) {
		gateway.handleConn(ctx, observedRunPromptConn{Conn: conn, wire: wire})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := remoteclient.DialRemoteURLForProject(ctx, "ws"+server.URL[len("http"):], core.ProjectID())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	progress := make(chan *runpromptpb.ProgressEvent, 1)
	type result struct {
		response *runpromptpb.Success
		err      error
	}
	done := make(chan result, 1)
	go func() {
		response, err := client.RunPrompt(ctx, serverapi.RunPromptRequest{
			Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
			Prompt: "continue",
		}, serverapi.RunPromptProgressFunc(func(event *runpromptpb.ProgressEvent) { progress <- event }))
		done <- result{response: response, err: err}
	}()
	select {
	case event := <-progress:
		if event.GetAssistantMessage() == nil || event.GetAssistantMessage().Content != "partial answer" {
			t.Fatalf("progress content: %v", event)
		}
	case result := <-done:
		t.Fatalf("finished before progress: %v (%v)", result.response, result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case result := <-done:
		t.Fatalf("final result escaped its release gate: %v (%v)", result.response, result.err)
	default:
	}
	service := runpromptpb.File_kent_api_run_prompt_run_prompt_proto.Services().ByName("RunService")
	eventOperation := gatewayOperationName(t, service.Methods().ByName("Progress"))
	wire.mu.Lock()
	frames := append([]rpcwire.Frame(nil), wire.frames...)
	wire.mu.Unlock()
	var generatedProgress *runpromptpb.ProgressEvent
	for _, frame := range frames {
		if frame.Kind != rpcwire.FrameBinary {
			continue
		}
		envelope, err := protoapi.DecodeEnvelope(frame.Payload)
		if err != nil {
			t.Fatal(err)
		}
		event := envelope.GetNotificationEvent()
		if event == nil || event.Operation != eventOperation {
			continue
		}
		generatedProgress = &runpromptpb.ProgressEvent{}
		if err := protoapi.Decode(event.Payload, generatedProgress); err != nil {
			t.Fatal(err)
		}
	}
	if generatedProgress == nil || generatedProgress.GetAssistantMessage().GetContent() != "partial answer" {
		t.Fatalf("generated progress content was not delivered: %v", generatedProgress)
	}
	finish()
	select {
	case result := <-done:
		if result.err != nil || result.response.Result != "final answer" {
			t.Fatalf("final answer: %v (%v)", result.response, result.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	wire.mu.Lock()
	frames = append([]rpcwire.Frame(nil), wire.frames...)
	wire.mu.Unlock()
	var finalResult *runpromptpb.Result
	for _, frame := range frames {
		if frame.Kind != rpcwire.FrameBinary {
			continue
		}
		envelope, err := protoapi.DecodeEnvelope(frame.Payload)
		if err != nil {
			t.Fatal(err)
		}
		response := envelope.GetResult()
		if response == nil || response.Operation != gatewayOperationName(t, service.Methods().ByName("Prompt")) {
			continue
		}
		if response.GetCorrelation() != "run-prompt" {
			t.Fatalf("Run Prompt result correlation = %q", response.GetCorrelation())
		}
		finalResult = &runpromptpb.Result{}
		if err := protoapi.Decode(response.Payload, finalResult); err != nil {
			t.Fatal(err)
		}
	}
	if finalResult.GetSuccess().GetResult() != "final answer" {
		t.Fatalf("generated final result = %v", finalResult)
	}
}

func TestRunPromptBinaryDeliversWorkflowContinuationRejection(t *testing.T) {
	core, _ := newGatewayTestCore(t, true, true)
	defer core.Close()
	taskID, sessionID := createGatewayPendingApprovalFixture(t, core)
	gateway, err := NewGateway(runPromptTestDependencies{
		GatewayDependencies: core,
		run:                 core.RunPromptClient(),
	}, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(gateway.handleConn))
	defer server.Close()
	client, err := remoteclient.DialRemoteURLForProject(
		context.Background(),
		"ws"+server.URL[len("http"):],
		core.ProjectID(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.RunPrompt(context.Background(), serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(sessionID),
		Prompt: "continue",
	}, nil)
	var rejection *serverapi.WorkflowContinuationRejectionError
	if !errors.As(err, &rejection) ||
		rejection.TaskID != string(taskID) ||
		rejection.Reason != serverapi.WorkflowContinuationWaitingForApproval {
		t.Fatalf("RunPrompt error = %T %v, want typed continuation rejection", err, err)
	}
}

func TestSubmitUserTurnBinaryDeliversWorkflowContinuationRejection(t *testing.T) {
	core, _ := newGatewayTestCore(t, true, true)
	defer core.Close()
	taskID, sessionID := createGatewayPendingApprovalFixture(t, core)
	gateway, err := NewGateway(runtimeControlTestDependencies{
		GatewayDependencies: core,
		runtime:             core.RuntimeControlClient(),
	}, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(gateway.handleConn))
	defer server.Close()
	client, err := remoteclient.DialRemoteURLForProject(
		context.Background(),
		"ws"+server.URL[len("http"):],
		core.ProjectID(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.SubmitUserTurn(context.Background(), &runtimepb.SubmitUserTurnRequest{
		SessionId: sessionID.String(),
		Input:     &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: "continue"}},
	})
	var rejection *serverapi.WorkflowContinuationRejectionError
	if !errors.As(err, &rejection) ||
		rejection.TaskID != string(taskID) ||
		rejection.Reason != serverapi.WorkflowContinuationWaitingForApproval {
		t.Fatalf("SubmitUserTurn error = %T %v, want typed continuation rejection", err, err)
	}
}

func createGatewayPendingApprovalFixture(
	t *testing.T,
	appCore *core.Core,
) (workflow.TaskID, runtimeids.SessionID) {
	t.Helper()
	ctx := context.Background()
	workflows := appCore.WorkflowClient()
	created, err := workflows.CreateWorkflow(ctx, serverapi.WorkflowCreateRequest{Name: "Continuation rejection Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	definition, err := workflows.GetWorkflow(ctx, serverapi.WorkflowGetRequest{WorkflowID: created.Workflow.ID})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	startID, terminalID := "", ""
	for _, node := range definition.Definition.Nodes {
		switch node.Kind {
		case "start":
			startID = node.ID
		case "terminal":
			terminalID = node.ID
		}
	}
	agentID := runtimeids.NewGraphEntityID()
	reviewID := runtimeids.NewGraphEntityID()
	startGroupID := runtimeids.NewGraphEntityID()
	reviewGroupID := runtimeids.NewGraphEntityID()
	doneGroupID := runtimeids.NewGraphEntityID()
	graph := serverapi.WorkflowGraphDraftFromDefinition(definition.Definition)
	graph.Nodes = append(graph.Nodes,
		serverapi.WorkflowGraphDraftNode{
			ID: agentID, Key: "agent", Kind: "agent", DisplayName: "Agent",
			SubagentRole: "coder", CompletionMode: "structured_output",
		},
		serverapi.WorkflowGraphDraftNode{
			ID: reviewID, Key: "review", Kind: "agent", DisplayName: "Review",
			SubagentRole: "reviewer", CompletionMode: "structured_output",
		},
	)
	graph.TransitionGroups = append(graph.TransitionGroups,
		serverapi.WorkflowGraphDraftTransitionGroup{ID: startGroupID, SourceNodeID: startID, TransitionID: "start", DisplayName: "Start"},
		serverapi.WorkflowGraphDraftTransitionGroup{ID: reviewGroupID, SourceNodeID: agentID, TransitionID: "review", DisplayName: "Review"},
		serverapi.WorkflowGraphDraftTransitionGroup{ID: doneGroupID, SourceNodeID: reviewID, TransitionID: "done", DisplayName: "Done"},
	)
	graph.Edges = append(graph.Edges,
		serverapi.WorkflowGraphDraftEdge{
			ID: runtimeids.NewGraphEntityID(), TransitionGroupID: startGroupID, Key: "start",
			TargetNodeID: agentID, AssigneeSelection: "configured", ThinkingSelection: "configured",
			ContextMode: "new_session", PromptTemplate: "Do the work.",
		},
		serverapi.WorkflowGraphDraftEdge{
			ID: runtimeids.NewGraphEntityID(), TransitionGroupID: reviewGroupID, Key: "review",
			TargetNodeID: reviewID, AssigneeSelection: "configured", ThinkingSelection: "configured",
			RequiresApproval: true, ContextMode: "continue_session",
			ContextSource:  serverapi.WorkflowContextSource{Kind: "immediate_source"},
			PromptTemplate: "Review the work.",
		},
		serverapi.WorkflowGraphDraftEdge{
			ID: runtimeids.NewGraphEntityID(), TransitionGroupID: doneGroupID, Key: "done",
			TargetNodeID: terminalID, AssigneeSelection: "configured", ThinkingSelection: "configured",
			ContextMode: "new_session",
		},
	)
	saved, err := workflows.SaveWorkflowGraph(ctx, serverapi.WorkflowGraphSaveRequest{
		WorkflowID:      created.Workflow.ID,
		ExpectedVersion: definition.Definition.Workflow.Version,
		Graph:           graph,
	})
	if err != nil || !saved.Saved {
		t.Fatalf("SaveWorkflowGraph continuation fixture = %+v, err = %v", saved, err)
	}
	if _, err := workflows.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{
		ProjectID:     appCore.ProjectID(),
		WorkflowID:    created.Workflow.ID,
		DefaultPolicy: serverapi.WorkflowProjectLinkDefaultAlways,
	}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	createdTask, err := workflows.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{
		ProjectID: appCore.ProjectID(),
		Title:     "Continuation rejection Task",
		Body:      "Reject invalid continuation.",
	})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	workflowStore, err := workflowstore.New(
		appCore.MetadataStore(),
		workflowstore.WithRoleResolver(testsetup.QuestionsEnabled("coder", "reviewer")),
	)
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, workflow.TaskID(createdTask.Task.ID))
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if len(started.Mutation.Created) != 1 {
		t.Fatalf("StartTask Current Nodes = %+v, want one", started.Mutation.Created)
	}
	sessionStore, err := session.Create(
		filepath.Join(filepath.Join(appCore.Config().PersistenceRoot, "projects"), appCore.ProjectID(), "sessions"),
		filepath.Base(appCore.Config().WorkspaceRoot),
		appCore.Config().WorkspaceRoot,
		sessioncontract.SessionCategorySubagent,
		appCore.MetadataStore().AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := sessionStore.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	sessionID, err := runtimeids.ParseSessionID(sessionStore.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	source := started.Mutation.Created[0].Reference
	if _, err := workflowStore.BindSessionToCurrentNode(ctx, workflowstore.CurrentNodeSessionBindingRequest{
		Association: workflowstore.TaskSessionAssociationRequest{
			SessionID: sessionID, CurrentNode: source, AssociatedAt: time.Now().UTC(),
		},
	}); err != nil {
		t.Fatalf("BindSessionToCurrentNode: %v", err)
	}
	completed, err := workflowStore.CompleteCurrentNode(ctx, workflowstore.CurrentNodeCompletionRequest{
		Source:       source,
		TransitionID: "review",
		Commentary:   "ready for review",
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	if completed.PendingApproval == nil {
		t.Fatalf("CompleteCurrentNode result = %+v, want pending Approval", completed)
	}
	return workflow.TaskID(createdTask.Task.ID), sessionID
}
