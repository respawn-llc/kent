package client

import (
	"context"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	projectpb "core/shared/protoapi/gen/kent/api/project"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestRemoteRejectsTextHandshakeResponse(t *testing.T) {
	closed := make(chan error, 1)
	method := connectionpb.File_kent_api_connection_connection_proto.Services().ByName("ConnectionService").Methods().ByName("Handshake")
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		receiveRemoteDescriptorCall(t, ws, method, &connectionpb.HandshakeRequest{})
		if err := websocket.Message.Send(ws, `{}`); err != nil {
			t.Error(err)
			return
		}
		var frame []byte
		closed <- websocket.Message.Receive(ws, &frame)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	remote, err := DialRemoteURL(ctx, "ws"+server.URL[len("http"):])
	if remote != nil {
		_ = remote.Close()
		t.Fatal("nonbinary handshake established a connection")
	}
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("setup must reject the frame without waiting for a deadline: %v", err)
	}
	select {
	case err := <-closed:
		if err == nil {
			t.Fatal("setup connection remained open")
		}
	case <-ctx.Done():
		t.Fatal("setup rejection did not close the connection")
	}
}

func TestRemoteControlIgnoresTextWhileCallsPending(t *testing.T) {
	var connections atomic.Int32
	method := projectpb.File_kent_api_project_project_proto.Services().ByName("ProjectCatalogService").Methods().ByName("List")
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		connections.Add(1)
		acceptRemoteHandshake(t, ws)
		first := receiveRemoteDescriptorCall(t, ws, method, &emptypb.Empty{})
		second := receiveRemoteDescriptorCall(t, ws, method, &emptypb.Empty{})
		// A text frame carrying a pending correlation must not consume it.
		if err := websocket.Message.Send(ws, fmt.Sprintf(`{"id":%q,"result":{}}`, *first)); err != nil {
			t.Error(err)
			return
		}
		for _, correlation := range []*string{second, first} {
			sendRemoteDescriptorResult(t, ws, method, correlation, &projectpb.ProjectListResult{
				Outcome: &projectpb.ProjectListResult_Success{Success: &projectpb.ProjectListSuccess{}},
			})
		}
		third := receiveRemoteDescriptorCall(t, ws, method, &emptypb.Empty{})
		sendRemoteDescriptorResult(t, ws, method, third, &projectpb.ProjectListResult{
			Outcome: &projectpb.ProjectListResult_Success{Success: &projectpb.ProjectListSuccess{}},
		})
	})
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	remote, err := DialRemoteURL(ctx, "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remote.Close() }()
	results := make(chan error, 2)
	for range 2 {
		go func() {
			result, err := remote.ListProjects(ctx, &emptypb.Empty{})
			if err == nil && result == nil {
				err = fmt.Errorf("missing binary result")
			}
			results <- err
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := remote.ListProjects(ctx, &emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if connections.Load() != 1 {
		t.Fatal("control connection was replaced")
	}
}
