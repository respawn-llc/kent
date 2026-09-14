package transport

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"core/shared/apicontract"
	remoteclient "core/shared/client"
	"core/shared/protoapi"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/rpcwire"
	"core/shared/runtimeids"
	"core/shared/serverapi"
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

type rejectedRunPrompt struct {
	taskID string
}

func (r rejectedRunPrompt) RunPrompt(context.Context, serverapi.RunPromptRequest, serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	return nil, &serverapi.WorkflowContinuationRejectionError{
		TaskID: r.taskID,
		Reason: serverapi.WorkflowContinuationWaitingForApproval,
	}
}

type rejectedRuntimeControl struct {
	apicontract.RuntimeControlService
	taskID string
}

func (r rejectedRuntimeControl) SubmitUserTurn(context.Context, *runtimepb.SubmitUserTurnRequest) (*runtimepb.SubmitUserTurnSuccess, error) {
	return nil, serverapi.NewRuntimeCommandNotAcceptedError(&serverapi.WorkflowContinuationRejectionError{
		TaskID: r.taskID,
		Reason: serverapi.WorkflowContinuationWaitingForApproval,
	})
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
	const taskID = "task-run-prompt-binary-rejection"
	gateway, err := NewGateway(runPromptTestDependencies{
		GatewayDependencies: core,
		run:                 rejectedRunPrompt{taskID: taskID},
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
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		Prompt: "continue",
	}, nil)
	var rejection *serverapi.WorkflowContinuationRejectionError
	if !errors.As(err, &rejection) ||
		rejection.TaskID != taskID ||
		rejection.Reason != serverapi.WorkflowContinuationWaitingForApproval {
		t.Fatalf("RunPrompt error = %T %v, want typed continuation rejection", err, err)
	}
}

func TestSubmitUserTurnBinaryDeliversWorkflowContinuationRejection(t *testing.T) {
	core, _ := newGatewayTestCore(t, true, true)
	defer core.Close()
	const taskID = "task-submit-user-turn-binary-rejection"
	store := createGatewayAuthoritativeSession(t, core)
	gateway, err := NewGateway(runtimeControlTestDependencies{
		GatewayDependencies: core,
		runtime: rejectedRuntimeControl{
			RuntimeControlService: core.RuntimeControlClient(),
			taskID:                taskID,
		},
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
		SessionId: store.Meta().SessionID,
		Input:     &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: "continue"}},
	})
	var rejection *serverapi.WorkflowContinuationRejectionError
	if !errors.As(err, &rejection) ||
		rejection.TaskID != taskID ||
		rejection.Reason != serverapi.WorkflowContinuationWaitingForApproval {
		t.Fatalf("SubmitUserTurn error = %T %v, want typed continuation rejection", err, err)
	}
}
