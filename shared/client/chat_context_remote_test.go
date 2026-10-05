package client

import (
	"context"
	"errors"
	"testing"

	"buf.build/go/protovalidate"
	"core/shared/apicontract"
	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
	"core/shared/runtimeids"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
)

func TestRemoteGetChatContextUsesSoleContractAndValidatesResponse(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	want := &contextpb.GetSuccess{Context: &contextpb.Context{
		ContextWindowTokens:      100,
		UsedTokens:               125,
		RemainingTokens:          -25,
		AutomaticThresholdTokens: 80,
		AutoCompactionEnabled:    true,
		CompactionMode:           contextpb.CompactionMode_COMPACTION_MODE_PROVIDER_NATIVE,
		CompletedCompactionCount: 3,
		CompactionRunning:        true,
	}}
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		decoded := &contextpb.GetRequest{}
		request := receiveRemoteGeneratedCall(t, ws, "ChatContextService", "Get", decoded)
		if got := decoded.GetTarget().GetSession().GetSessionId(); got != sessionID.String() {
			t.Errorf("Session target = %s, want %s", got, sessionID)
			return
		}
		sendRemoteGeneratedResult(t, ws, request, &contextpb.GetResult{Outcome: &contextpb.GetResult_Success{Success: want}})
	})
	remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()

	got, err := remote.GetChatContext(t.Context(), &contextpb.GetRequest{Target: &contextpb.Target{Target: &contextpb.Target_Session{
		Session: &contextpb.SessionTarget{SessionId: sessionID.String()},
	}}})
	if err != nil {
		t.Fatalf("GetChatContext: %v", err)
	}
	if !proto.Equal(got, want) {
		t.Fatalf("response = %+v, want %+v", got, want)
	}
}

func TestRemoteGetChatContextRejectsInvalidResponse(t *testing.T) {
	response := &contextpb.GetSuccess{Context: &contextpb.Context{
		ContextWindowTokens:      100,
		UsedTokens:               40,
		RemainingTokens:          61,
		AutomaticThresholdTokens: 80,
		CompactionMode:           contextpb.CompactionMode_COMPACTION_MODE_LOCAL,
	}}
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		request := receiveRemoteGeneratedCall(t, ws, "ChatContextService", "Get", &contextpb.GetRequest{})
		// The helper marshals without validating so rejection occurs at the client boundary.
		sendRemoteGeneratedResult(t, ws, request, &contextpb.GetResult{Outcome: &contextpb.GetResult_Success{Success: response}})
	})
	remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()

	_, err = remote.GetChatContext(t.Context(), &contextpb.GetRequest{Target: &contextpb.Target{Target: &contextpb.Target_Session{
		Session: &contextpb.SessionTarget{SessionId: runtimeids.NewSessionID().String()},
	}}})
	var invalidResponse *protovalidate.ValidationError
	if err == nil || !errors.As(err, &invalidResponse) {
		t.Fatalf("GetChatContext error = %v, want generated validation error", err)
	}
}

var _ apicontract.ChatContextService = (*Remote)(nil)
