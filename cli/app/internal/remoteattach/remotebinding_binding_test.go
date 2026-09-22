package remoteattach

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"core/shared/client"
	"core/shared/protoapi"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	serverpb "core/shared/protoapi/gen/kent/api/server"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/protocol"
	"core/shared/rpcwire"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestBindProjectWorkspaceFailuresKeepCurrentUsableAndCloseCreatedSuccessor(t *testing.T) {
	tests := []struct {
		name     string
		rootID   string
		dialErr  error
		next     remoteBindingServerOptions
		wantRoot bool
	}{
		{name: "dial", dialErr: errors.New("dial failed")},
		{name: "root pin", rootID: "required-root", next: remoteBindingServerOptions{rootID: "other-root"}, wantRoot: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentServer := newRemoteBindingServer(t, remoteBindingServerOptions{rootID: "current-root"})
			current := currentServer.dial(t)
			t.Cleanup(func() { _ = current.Close() })

			var nextServer *remoteBindingServer
			var next *client.Remote
			if test.dialErr == nil {
				nextServer = newRemoteBindingServer(t, test.next)
				next = nextServer.dial(t)
			}
			_, err := bindProjectWorkspace(context.Background(), current, "project-1", "", test.rootID, func(context.Context, string, string) (*client.Remote, error) {
				return next, test.dialErr
			})
			if test.wantRoot {
				if !errors.Is(err, client.ErrServerRootMismatch) {
					t.Fatalf("error = %v, want ErrServerRootMismatch", err)
				}
			} else if err == nil {
				t.Fatal("expected binding failure")
			}
			requireRemoteUsable(t, current)
			if nextServer != nil {
				nextServer.requireClosed(t)
			}
		})
	}
}

func TestBindSessionReplacesTheProjectScopedConnection(t *testing.T) {
	currentServer := newRemoteBindingServer(t, remoteBindingServerOptions{rootID: "root"})
	current := currentServer.dial(t)
	nextServer := newRemoteBindingServer(t, remoteBindingServerOptions{rootID: "root"})
	next := nextServer.dial(t)
	t.Cleanup(func() { _ = next.Close() })

	var selectedSessionID string
	bound, err := bindSession(context.Background(), current, " session-b ", "root", func(_ context.Context, sessionID string) (*client.Remote, error) {
		selectedSessionID = sessionID
		return next, nil
	})
	if err != nil {
		t.Fatalf("bind Session: %v", err)
	}
	if bound != next || selectedSessionID != "session-b" {
		t.Fatalf("bound remote/session = %p/%q, want %p/session-b", bound, selectedSessionID, next)
	}
	currentServer.requireClosed(t)
	requireRemoteUsable(t, bound)
}

func TestBindSessionPromotesPreparedHandoffWithoutDialing(t *testing.T) {
	server := newRemoteBindingServer(t, remoteBindingServerOptions{
		rootID:        "root",
		projectID:     "project-1",
		workspaceID:   "workspace-1",
		workspaceRoot: "/workspace",
	})
	current, err := client.DialRemoteURLForProject(
		context.Background(),
		"ws"+server.URL[len("http"):],
		"project-1",
	)
	if err != nil {
		t.Fatalf("dial Project remote: %v", err)
	}
	t.Cleanup(func() { _ = current.Close() })
	subscription, err := current.SubscribeSessionTranscript(context.Background(), &transcriptpb.SubscribeRequest{
		SessionId: "session-1",
	})
	if err != nil {
		t.Fatalf("SubscribeSessionTranscript: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })

	var dialed atomic.Bool
	bound, err := bindSession(context.Background(), current, "session-1", "root", func(context.Context, string) (*client.Remote, error) {
		dialed.Store(true)
		return nil, errors.New("Session dialer must not run when a prepared handoff exists")
	})
	if err != nil {
		t.Fatalf("bind Session from prepared handoff: %v", err)
	}
	t.Cleanup(func() { _ = bound.Close() })
	if dialed.Load() {
		t.Fatal("bind Session dialed instead of promoting the prepared handoff")
	}
	if bound == current {
		t.Fatal("bind Session retained the Project-scoped remote")
	}
	requireRemoteUsable(t, bound)
}

type remoteBindingServerOptions struct {
	rootID        string
	projectID     string
	workspaceID   string
	workspaceRoot string
}

type remoteBindingServer struct {
	*httptest.Server
	closed     chan struct{}
	closedOnce sync.Once
	closeCount atomic.Int32
}

func newRemoteBindingServer(t *testing.T, options remoteBindingServerOptions) *remoteBindingServer {
	t.Helper()
	server := &remoteBindingServer{closed: make(chan struct{})}
	server.Server = httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(func(ctx context.Context, conn rpcwire.Conn) {
		defer func() {
			server.closeCount.Add(1)
			server.closedOnce.Do(func() { close(server.closed) })
		}()
		handshaken := false
		for event := range conn.Events() {
			if event.Err != nil {
				return
			}
			if event.Frame.Kind != rpcwire.FrameBinary {
				return
			}
			handshakeCompleted, err := serveRemoteBindingBinary(ctx, conn, event.Frame, handshaken, options)
			if err != nil {
				return
			}
			handshaken = handshaken || handshakeCompleted
		}
	}))
	t.Cleanup(server.Server.Close)
	return server
}

func serveRemoteBindingBinary(
	ctx context.Context,
	conn rpcwire.Conn,
	frame rpcwire.Frame,
	handshaken bool,
	options remoteBindingServerOptions,
) (bool, error) {
	envelope, err := protoapi.DecodeEnvelope(frame.Payload)
	if err != nil {
		return false, err
	}
	call := envelope.GetCall()
	if call == nil || call.Correlation == nil {
		return false, errors.New("correlated binary call is required")
	}
	handshakeMethod := connectionpb.File_kent_api_connection_connection_proto.Services().
		ByName("ConnectionService").Methods().ByName("Handshake")
	handshakeOperation, err := protoapi.OperationFromDescriptor(handshakeMethod)
	if err != nil {
		return false, err
	}
	if call.Operation == handshakeOperation.Name {
		if handshaken {
			return false, errors.New("Handshake may only be called once")
		}
		var request connectionpb.HandshakeRequest
		if err := protoapi.Decode(call.Payload, &request); err != nil {
			return false, err
		}
		identity := &connectionpb.ServerIdentity{
			ProtocolVersion: protocol.Version,
			ServerId:        "binding-test",
			Pid:             1,
		}
		if options.rootID != "" {
			identity.PersistenceRootId = &options.rootID
		}
		result := &connectionpb.HandshakeResult{
			Outcome: &connectionpb.HandshakeResult_Success{
				Success: &connectionpb.HandshakeSuccess{Identity: identity},
			},
		}
		return true, sendRemoteBindingBinaryResult(ctx, conn, call, result)
	}
	if !handshaken {
		return false, errors.New("Handshake must be the first binary operation")
	}

	subscribeMethod := transcriptpb.File_kent_api_transcript_transcript_proto.Services().
		ByName("StreamService").Methods().ByName("Subscribe")
	subscribeOperation, err := protoapi.OperationFromDescriptor(subscribeMethod)
	if err != nil {
		return false, err
	}
	if call.Operation == subscribeOperation.Name {
		var request transcriptpb.SubscribeRequest
		if err := protoapi.Decode(call.Payload, &request); err != nil {
			return false, err
		}
		return false, sendRemoteBindingBinaryResult(ctx, conn, call, &transcriptpb.SubscribeResult{
			Outcome: &transcriptpb.SubscribeResult_Success{Success: &emptypb.Empty{}},
		})
	}

	attachProjectMethod := connectionpb.File_kent_api_connection_connection_proto.Services().
		ByName("ConnectionService").Methods().ByName("AttachProject")
	attachProjectOperation, err := protoapi.OperationFromDescriptor(attachProjectMethod)
	if err != nil {
		return false, err
	}
	if call.Operation == attachProjectOperation.Name {
		var request connectionpb.AttachProjectRequest
		if err := protoapi.Decode(call.Payload, &request); err != nil {
			return false, err
		}
		result := &connectionpb.AttachProjectResult{
			Outcome: &connectionpb.AttachProjectResult_Success{
				Success: &connectionpb.AttachmentSuccess{
					Attachment: &connectionpb.AttachmentSuccess_Project{
						Project: &connectionpb.ProjectAttachment{
							ProjectId:     options.projectID,
							WorkspaceId:   options.workspaceID,
							WorkspaceRoot: options.workspaceRoot,
						},
					},
				},
			},
		}
		return false, sendRemoteBindingBinaryResult(ctx, conn, call, result)
	}

	attachSessionMethod := connectionpb.File_kent_api_connection_connection_proto.Services().
		ByName("ConnectionService").Methods().ByName("AttachSession")
	attachSessionOperation, err := protoapi.OperationFromDescriptor(attachSessionMethod)
	if err != nil {
		return false, err
	}
	if call.Operation == attachSessionOperation.Name {
		var request connectionpb.AttachSessionRequest
		if err := protoapi.Decode(call.Payload, &request); err != nil {
			return false, err
		}
		result := &connectionpb.AttachSessionResult{
			Outcome: &connectionpb.AttachSessionResult_Success{
				Success: &connectionpb.AttachmentSuccess{
					Attachment: &connectionpb.AttachmentSuccess_Session{
						Session: &connectionpb.SessionAttachment{
							ProjectId:          options.projectID,
							WorkspaceId:        options.workspaceID,
							WorkspaceRoot:      options.workspaceRoot,
							SessionId:          request.SessionId,
							ReattachCapability: "reattach-capability",
						},
					},
				},
			},
		}
		return false, sendRemoteBindingBinaryResult(ctx, conn, call, result)
	}

	readinessMethod := serverpb.File_kent_api_server_server_proto.Services().
		ByName("ServerService").Methods().ByName("GetReadiness")
	readinessOperation, err := protoapi.OperationFromDescriptor(readinessMethod)
	if err != nil {
		return false, err
	}
	if call.Operation == readinessOperation.Name {
		result := &serverpb.GetReadinessResult{
			Outcome: &serverpb.GetReadinessResult_Success{Success: &serverpb.GetReadinessSuccess{
				Readiness: &serverpb.Readiness{
					Ready:           true,
					ServerId:        "binding-test",
					ServerVersion:   "test",
					ProtocolVersion: protocol.Version,
				},
			}},
		}
		return false, sendRemoteBindingBinaryResult(ctx, conn, call, result)
	}

	return false, errors.New("unexpected binary test operation")
}

func sendRemoteBindingBinaryResult(ctx context.Context, conn rpcwire.Conn, call *sharedpb.Call, result proto.Message) error {
	payload, err := protoapi.Encode(result)
	if err != nil {
		return err
	}
	encoded, err := protoapi.EncodeEnvelope(&sharedpb.Envelope{
		Frame: &sharedpb.Envelope_Result{Result: &sharedpb.Result{
			Operation: call.Operation, Correlation: call.Correlation, Payload: payload,
		}},
	})
	if err != nil {
		return err
	}
	return conn.Send(ctx, rpcwire.Frame{Kind: rpcwire.FrameBinary, Payload: encoded})
}

func (s *remoteBindingServer) dial(t *testing.T) *client.Remote {
	t.Helper()
	remote, err := client.DialRemoteURL(context.Background(), "ws"+s.URL[len("http"):])
	if err != nil {
		t.Fatalf("dial remote: %v", err)
	}
	return remote
}

func (s *remoteBindingServer) requireClosed(t *testing.T) {
	t.Helper()
	select {
	case <-s.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("underlying connection did not close")
	}
	if count := s.closeCount.Load(); count != 1 {
		t.Fatalf("underlying connection close count = %d, want 1", count)
	}
}

func requireRemoteUsable(t *testing.T, remote *client.Remote) {
	t.Helper()
	response, err := remote.GetReadiness(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("remote is not usable: %v", err)
	}
	if !response.GetReadiness().GetReady() {
		t.Fatal("remote readiness = false")
	}
}
