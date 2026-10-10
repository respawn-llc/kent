package transport

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"core/shared/apicontract"
	remoteclient "core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/rpcwire"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"google.golang.org/protobuf/types/known/durationpb"
)

type rejectedSelectionRunPrompt struct {
	reason chatsettingspb.MutationRejectionReason
}

func (s rejectedSelectionRunPrompt) RunPrompt(context.Context, serverapi.RunPromptRequest, serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	return nil, &serverapi.RunSelectionRejectedError{Reason: s.reason}
}

func TestRunPromptBinaryPreservesSelectionRejection(t *testing.T) {
	core, _ := newGatewayTestCore(t, true, true)
	defer core.Close()
	reason := chatsettingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_THINKING_UNAVAILABLE
	gateway, err := NewGateway(runPromptTestDependencies{
		GatewayDependencies: core, run: rejectedSelectionRunPrompt{reason: reason},
	}, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(gateway.handleConn))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := remoteclient.DialRemoteURLForProject(ctx, "ws"+server.URL[len("http"):], core.ProjectID())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.RunPrompt(ctx, serverapi.RunPromptRequest{
		Intent: serverapi.OpenExistingSessionLaunchIntent(runtimeids.NewSessionID()),
		Prompt: "reject this selection", Overrides: serverapi.RunPromptOverrides{ThinkingLevel: "unsupported"},
	}, nil)
	var rejected *serverapi.RunSelectionRejectedError
	if !errors.As(err, &rejected) || rejected.Reason != reason {
		t.Fatalf("Run rejection did not survive transport: %v", err)
	}
}

func TestRunPromptBinaryReportsUnavailableConnectionSelection(t *testing.T) {
	for _, test := range []struct {
		name      string
		selection config.ConnectionSelection
	}{
		{name: "singleton", selection: config.ConnectionSelection{"qa-unknown"}},
		{name: "set", selection: config.ConnectionSelection{"qa-unknown", "qa-also-unknown"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			core, _ := newGatewayTestCore(t, true, true)
			defer core.Close()
			settings := config.Settings{Connection: &test.selection}
			_, selectionErr := settings.ConnectionMembers()
			if selectionErr == nil {
				t.Fatal("undefined selection was accepted")
			}
			gateway, err := NewGateway(runPromptTestDependencies{
				GatewayDependencies: core, run: rejectedRunPrompt{err: selectionErr},
			}, gatewayTestIdentity())
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(gateway.handleConn))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			client, err := remoteclient.DialRemoteURLForProject(ctx, "ws"+server.URL[len("http"):], core.ProjectID())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_, err = client.RunPrompt(ctx, serverapi.RunPromptRequest{
				Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
				Prompt: "report selection failure",
			}, nil)
			if err == nil || err.Error() != selectionErr.Error() {
				t.Fatalf("selection failure did not survive transport: %v; want %v", err, selectionErr)
			}
		})
	}
}

type controlledRunPrompt struct {
	release           <-chan struct{}
	sessionID         string
	selectionWarnings []runpromptpb.RunSelectionWarning
}

type rejectedRunPrompt struct {
	err error
}

func (s rejectedRunPrompt) RunPrompt(context.Context, serverapi.RunPromptRequest, serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	return nil, s.err
}

func (s controlledRunPrompt) RunPrompt(ctx context.Context, _ serverapi.RunPromptRequest, sink serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	sink.PublishRunPromptProgress(&runpromptpb.ProgressEvent{Payload: &runpromptpb.ProgressEvent_AssistantMessage{
		AssistantMessage: &runpromptpb.AssistantMessage{Phase: runpromptpb.MessagePhase_MESSAGE_PHASE_COMMENTARY, Content: "partial answer"},
	}})
	select {
	case <-s.release:
		return &runpromptpb.Success{
			SessionId: s.sessionID, SessionName: "Session", Result: "final answer",
			Duration: durationpb.New(time.Millisecond), SelectionWarnings: s.selectionWarnings,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type runPromptTestDependencies struct {
	GatewayDependencies
	run apicontract.RunPromptService
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
		run: controlledRunPrompt{
			release: release, sessionID: runtimeids.NewSessionID().String(),
			selectionWarnings: []runpromptpb.RunSelectionWarning{
				runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_AGENT_IGNORED_TO_PRESERVE_CACHE,
			},
		},
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
		if result.err != nil || result.response.Result != "final answer" ||
			!reflect.DeepEqual(result.response.SelectionWarnings, []runpromptpb.RunSelectionWarning{
				runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_AGENT_IGNORED_TO_PRESERVE_CACHE,
			}) {
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

func TestRunPromptBinaryPreservesLaunchDenials(t *testing.T) {
	core, _ := newGatewayTestCore(t, true, true)
	defer core.Close()
	target := "blocked"
	callerID := runtimeids.NewSessionID().String()
	for _, tc := range []struct {
		name   string
		denied serverapi.SubagentLaunchDeniedError
		caller *string
	}{
		{
			name:   "not callable",
			denied: serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialNotCallable, Target: &target},
		},
		{
			name: "missing target",
			denied: serverapi.SubagentLaunchDeniedError{
				Kind: serverapi.SubagentLaunchDenialTargetMissing, Target: &target, AvailableRoles: []string{"worker", "fast"},
			},
		},
		{
			name:   "invalid target",
			denied: serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialInvalidTarget},
		},
		{
			name:   "missing parent",
			denied: serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialParentMissing},
		},
		{
			name:   "missing caller",
			denied: serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialCallerMissing},
			caller: &callerID,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway, err := NewGateway(runPromptTestDependencies{
				GatewayDependencies: core,
				run:                 rejectedRunPrompt{err: &tc.denied},
			}, gatewayTestIdentity())
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(gateway.Handler())
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			client, err := remoteclient.DialRemoteURLForProject(ctx, "ws"+server.URL[len("http"):], core.ProjectID())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			response, err := client.RunPrompt(ctx, serverapi.RunPromptRequest{
				Intent:          serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
				CallerSessionID: tc.caller,
				Prompt:          "must not dispatch",
			}, nil)
			var denied *serverapi.SubagentLaunchDeniedError
			if response != nil || !errors.As(err, &denied) || !reflect.DeepEqual(*denied, tc.denied) {
				t.Fatalf("remote denial = %+v (%v), want %+v", denied, err, tc.denied)
			}
			if tc.caller != nil {
				var missing *sessioncontract.SessionNotFoundError
				if !errors.Is(err, sessioncontract.ErrSessionNotFound) || !errors.As(err, &missing) || missing.SessionID != *tc.caller {
					t.Fatalf("missing caller lost typed session metadata: %v", err)
				}
			}
		})
	}
}
