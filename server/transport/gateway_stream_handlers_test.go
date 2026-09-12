package transport

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"core/shared/apicontract"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/rpcwire"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPromptFollowUpSubscriptionInstallsBeforeSubscribeResponse(t *testing.T) {
	stepID, err := runtimeids.ParseStepID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("parse Step ID: %v", err)
	}
	conn := &promptFollowUpRegistrationConn{}
	fixture := newRoutePolicyFixture(t)
	fixture.gateway.deps = &gatewayFollowUpDependencies{
		GatewayDependencies: fixture.gateway.deps,
		control: gatewayFollowUpService{
			subscribe: func(context.Context, *promptpb.FollowUpWatchRequest) (serverapi.PromptFollowUpSubscription, error) {
				conn.installed = true
				return conn, nil
			},
		},
	}
	serveGeneratedStream(t, fixture.gateway, conn, &connectionState{attachedProject: fixture.bindingA.ProjectID},
		promptpb.File_kent_api_prompt_prompt_proto.Services().ByName("FollowUpService").Methods().ByName("Watch"),
		&promptpb.FollowUpWatchRequest{SessionId: fixture.ownSessionID, StepId: stepID.String(), ToolCallId: "prompt-1"},
	)
	if conn.sent != 2 {
		t.Fatalf("frames = %d, want SubscribeResponse and completion", conn.sent)
	}
}

func (*promptFollowUpRegistrationConn) Next(context.Context) (*promptpb.FollowUpEvent, error) {
	return nil, io.EOF
}

type gatewayFollowUpService struct {
	apicontract.PromptControlService
	subscribe func(context.Context, *promptpb.FollowUpWatchRequest) (serverapi.PromptFollowUpSubscription, error)
}

func (s gatewayFollowUpService) SubscribeFollowUp(ctx context.Context, req *promptpb.FollowUpWatchRequest) (serverapi.PromptFollowUpSubscription, error) {
	return s.subscribe(ctx, req)
}

type gatewayFollowUpDependencies struct {
	GatewayDependencies
	control apicontract.PromptControlService
}

func (d *gatewayFollowUpDependencies) PromptControlClient() apicontract.PromptControlService {
	return d.control
}

type promptFollowUpRegistrationConn struct {
	installed bool
	sent      int
}

func (c *promptFollowUpRegistrationConn) Send(context.Context, rpcwire.Frame) error {
	if c.sent == 0 && !c.installed {
		return errors.New("SubscribeResponse sent before watcher installation")
	}
	c.sent++
	return nil
}
func (*promptFollowUpRegistrationConn) Events() <-chan rpcwire.Event { return nil }
func (*promptFollowUpRegistrationConn) Closed() <-chan struct{}      { return nil }
func (*promptFollowUpRegistrationConn) Close() error                 { return nil }
func TestSessionTranscriptSubscriptionPublishesLiveRunFinishedWithoutRewritingSequence(t *testing.T) {
	answer := "done"
	now := time.Unix(1, 0).UTC()
	subscription := &scriptedGatewayTranscriptSubscription{
		messages: []*transcriptpb.Message{
			{Sequence: 7, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_LiveRunFinished{LiveRunFinished: &transcriptpb.LiveRunFinished{
				Status:        transcriptpb.LiveRunStatus_LIVE_RUN_STATUS_COMPLETED,
				ResultKind:    transcriptpb.LiveRunResultKind_LIVE_RUN_RESULT_KIND_ASSISTANT_FINAL_ANSWER,
				WorkPerformed: true,
				FinalAnswer:   &answer,
				StartedAt:     timestamppb.New(now),
				FinishedAt:    timestamppb.New(now),
			}}}},
			{Sequence: 8, Event: &transcriptpb.Event{Payload: &transcriptpb.Event_OperationalDiagnostic{OperationalDiagnostic: &transcriptpb.OperationalDiagnostic{
				Code:   transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_SLEEP_GUARD_FAILED,
				Detail: "sleep guard failed",
			}}}},
		},
	}
	conn := &recordingGatewayConn{}
	expected := append([]*transcriptpb.Message(nil), subscription.messages...)
	fixture := newRoutePolicyFixture(t)
	fixture.gateway.deps = &gatewayTranscriptDependencies{
		GatewayDependencies: fixture.gateway.deps,
		transcript: gatewayTranscriptServiceFunc(func(context.Context, *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
			return subscription, nil
		}),
	}
	sessionID, err := runtimeids.ParseSessionID(fixture.ownSessionID)
	if err != nil {
		t.Fatal(err)
	}
	method := transcriptpb.File_kent_api_transcript_transcript_proto.Services().ByName("StreamService").Methods().ByName("Subscribe")
	serveGeneratedStream(t, fixture.gateway, conn, &connectionState{attachedProject: fixture.bindingA.ProjectID, attachedSession: &sessionID},
		method, &transcriptpb.SubscribeRequest{SessionId: fixture.ownSessionID},
	)

	associated, err := protoapi.ResolveSubscriptionOperations(method)
	if err != nil {
		t.Fatal(err)
	}
	var messages []*transcriptpb.Message
	for _, frame := range conn.frames {
		envelope, err := protoapi.DecodeEnvelope(frame.Payload)
		if err != nil {
			t.Fatal(err)
		}
		event := envelope.GetNotificationEvent()
		if event.GetOperation() != associated.Event.Name {
			continue
		}
		message := &transcriptpb.Message{}
		if err := protoapi.Decode(event.Payload, message); err != nil {
			t.Fatalf("decode transcript event: %v", err)
		}
		messages = append(messages, message)
	}
	if len(messages) != 2 {
		t.Fatalf("transcript messages = %+v, want live-run-finished and diagnostic", messages)
	}
	for i, message := range messages {
		if !proto.Equal(message, expected[i]) {
			t.Fatalf("transcript message %d = %v, want %v", i, message, expected[i])
		}
	}
	if !subscription.closed {
		t.Fatal("subscription was not closed")
	}
}

type gatewayTranscriptServiceFunc func(context.Context, *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error)

func (f gatewayTranscriptServiceFunc) SubscribeSessionTranscript(ctx context.Context, req *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
	return f(ctx, req)
}

type gatewayTranscriptDependencies struct {
	GatewayDependencies
	transcript apicontract.SessionTranscriptService
}

func (d *gatewayTranscriptDependencies) SessionTranscriptClient() apicontract.SessionTranscriptService {
	return d.transcript
}

type recordingGatewayConn struct {
	frames []rpcwire.Frame
}

func (c *recordingGatewayConn) Send(_ context.Context, frame rpcwire.Frame) error {
	c.frames = append(c.frames, frame)
	return nil
}

func (*recordingGatewayConn) Events() <-chan rpcwire.Event { return nil }
func (*recordingGatewayConn) Closed() <-chan struct{}      { return nil }
func (*recordingGatewayConn) Close() error                 { return nil }

type scriptedGatewayTranscriptSubscription struct {
	messages []*transcriptpb.Message
	closed   bool
}

func (s *scriptedGatewayTranscriptSubscription) Next(context.Context) (*transcriptpb.Message, error) {
	if len(s.messages) == 0 {
		return nil, io.EOF
	}
	message := s.messages[0]
	s.messages = s.messages[1:]
	return message, nil
}

func (s *scriptedGatewayTranscriptSubscription) Close() error {
	s.closed = true
	return nil
}

func serveGeneratedStream(t *testing.T, gateway *Gateway, conn rpcwire.Conn, state *connectionState, method protoreflect.MethodDescriptor, request proto.Message) {
	t.Helper()
	bindings := make(map[string]gatewayBinaryBinding)
	if err := registerSessionStreamsGatewayBinaryBindings(bindings); err != nil {
		t.Fatal(err)
	}
	binding := bindings[gatewayOperationName(t, method)]
	payload, err := protoapi.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	gateway.serveBinaryRequest(conn, t.Context(), state, gatewayBinaryRequest{
		binding: binding,
		call:    &sharedpb.Call{Operation: binding.operation.Name, Correlation: proto.String("subscribe"), Payload: payload},
	})
}
