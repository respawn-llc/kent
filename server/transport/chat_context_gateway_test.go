package transport

import (
	"context"
	"testing"

	"core/server/chatcontext"
	"core/shared/protoapi"
	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"google.golang.org/protobuf/proto"
)

type chatContextSessionOwnerFixture struct {
	context   *contextpb.Context
	err       error
	calls     int
	sessionID runtimeids.SessionID
}

func (o *chatContextSessionOwnerFixture) ReadSessionChatContext(_ context.Context, sessionID runtimeids.SessionID) (*contextpb.Context, error) {
	o.calls++
	o.sessionID = sessionID
	return o.context, o.err
}

type chatContextGatewayDependencies struct {
	GatewayDependencies
	sessionOwner chatcontext.SessionOwner
	sessionCalls int
}

func (d *chatContextGatewayDependencies) SessionChatContextOwner() chatcontext.SessionOwner {
	d.sessionCalls++
	return d.sessionOwner
}

func TestGatewayChatContextDispatchesOnlySessionOwner(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	want := validGatewayChatContext()
	sessionOwner := &chatContextSessionOwnerFixture{context: want}
	deps := &chatContextGatewayDependencies{
		GatewayDependencies: appCore,
		sessionOwner:        sessionOwner,
	}
	store := createGatewayAuthoritativeSession(t, appCore)
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("parse Session id: %v", err)
	}
	registration, err := productionGatewayRegistration()
	if err != nil {
		t.Fatalf("production Gateway registration: %v", err)
	}
	method := contextpb.File_kent_api_chat_context_chat_context_proto.Services().ByName("ChatContextService").Methods().ByName("Get")
	binding := registration.binary[gatewayOperationName(t, method)]
	payload, err := protoapi.Encode(&contextpb.GetRequest{Target: &contextpb.Target{Target: &contextpb.Target_Session{
		Session: &contextpb.SessionTarget{SessionId: sessionID.String()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	_, response, failure := (&Gateway{deps: deps, registration: registration}).dispatchBinary(
		t.Context(),
		&connectionState{handshakeDone: true, attachedProject: appCore.ProjectID()},
		gatewayBinaryRequest{binding: binding, call: &sharedpb.Call{
			Operation: binding.operation.Name, Correlation: proto.String("chat-context"), Payload: payload,
		}},
		nil,
	)
	if failure != nil {
		t.Fatalf("transport failure: %v", failure)
	}
	encoded, err := protoapi.Encode(response)
	if err != nil {
		t.Fatal(err)
	}
	got := &contextpb.GetResult{}
	if err := protoapi.Decode(encoded, got); err != nil {
		t.Fatal(err)
	}
	if got.GetSuccess() == nil {
		t.Fatalf("Context result = %v, want success", response)
	}
	if !proto.Equal(got.GetSuccess().Context, want) {
		t.Fatalf("Context = %+v, want %+v", got.GetSuccess().Context, want)
	}
	if sessionOwner.calls != 1 || sessionOwner.sessionID != sessionID || deps.sessionCalls != 1 {
		t.Fatalf("calls = Session owner %d (%s), Session resolver %d", sessionOwner.calls, sessionOwner.sessionID, deps.sessionCalls)
	}
}

func TestGatewayChatContextValidatesTargetBeforeSessionDispatch(t *testing.T) {
	appCore, server, _ := newGatewayTestServerWithAuth(t, false)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)
	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)

	method := contextpb.File_kent_api_chat_context_chat_context_proto.Services().ByName("ChatContextService").Methods().ByName("Get")
	malformed, err := protoapi.Marshal(&contextpb.GetRequest{Target: &contextpb.Target{}})
	if err != nil {
		t.Fatal(err)
	}
	envelope := callGatewayDescriptorPayload(t, conn, "malformed-context", method, malformed)
	if got := envelope.GetTransportFailure(); got == nil || got.Code != sharedpb.TransportFailureCode_TRANSPORT_FAILURE_CODE_INVALID_PAYLOAD {
		t.Fatalf("malformed Context error = %+v, want invalid payload", envelope)
	}

	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("parse Session id: %v", err)
	}
	sessionResponse := &contextpb.GetResult{}
	callGatewayDescriptor(t, conn, "session-context", method, &contextpb.GetRequest{Target: &contextpb.Target{Target: &contextpb.Target_Session{
		Session: &contextpb.SessionTarget{SessionId: sessionID.String()},
	}}}, sessionResponse)
	if sessionResponse.GetSuccess() == nil {
		t.Fatalf("pre-auth Session Context response: %v", sessionResponse)
	}
}

func validGatewayChatContext() *contextpb.Context {
	return &contextpb.Context{
		ContextWindowTokens:      100,
		UsedTokens:               40,
		RemainingTokens:          60,
		AutomaticThresholdTokens: 80,
		AutoCompactionEnabled:    true,
		CompactionMode:           contextpb.CompactionMode_COMPACTION_MODE_LOCAL,
		CompletedCompactionCount: 2,
		ManualCompactAvailable:   true,
	}
}

var _ GatewayDependencies = (*chatContextGatewayDependencies)(nil)
