package client

import (
	"context"
	"testing"

	"builder/shared/protocol"
	"builder/shared/serverapi"
	"golang.org/x/net/websocket"
	"net/http/httptest"
)

func TestRemoteWorkflowListRoute(t *testing.T) {
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer func() { _ = ws.Close() }()
		var req protocol.Request
		if err := websocket.JSON.Receive(ws, &req); err != nil {
			t.Fatalf("receive handshake: %v", err)
		}
		if req.Method != protocol.MethodHandshake {
			t.Fatalf("handshake method = %q", req.Method)
		}
		if err := websocket.JSON.Send(ws, protocol.NewSuccessResponse(req.ID, protocol.HandshakeResponse{Identity: protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"}})); err != nil {
			t.Fatalf("send handshake response: %v", err)
		}
		if err := websocket.JSON.Receive(ws, &req); err != nil {
			t.Fatalf("receive workflow list: %v", err)
		}
		if req.Method != protocol.MethodWorkflowList {
			t.Fatalf("workflow list method = %q", req.Method)
		}
		resp := serverapi.WorkflowListResponse{Workflows: []serverapi.WorkflowRecord{{ID: "workflow-1", Name: "Workflow"}}}
		if err := websocket.JSON.Send(ws, protocol.NewSuccessResponse(req.ID, resp)); err != nil {
			t.Fatalf("send workflow list response: %v", err)
		}
	}))
	defer server.Close()

	remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()
	resp, err := remote.ListWorkflows(context.Background(), serverapi.WorkflowListRequest{})
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(resp.Workflows) != 1 || resp.Workflows[0].ID != "workflow-1" {
		t.Fatalf("response = %+v", resp)
	}
}
