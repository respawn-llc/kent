package client

import (
	"testing"

	sessionpb "core/shared/protoapi/gen/kent/api/session"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
)

func TestRemoteSessionExecutionEnvironmentRoundTripsAuthApplicability(t *testing.T) {
	tests := []struct {
		name string
		auth *sessionpb.ExecutionAuthField
	}{
		{
			name: "explicit no auth",
			auth: &sessionpb.ExecutionAuthField{Result: &sessionpb.ExecutionAuthField_Available{
				Available: &sessionpb.ExecutionAuth{Provider: "openai", Method: sessionpb.ExecutionAuthMethod_EXECUTION_AUTH_METHOD_NONE},
			}},
		},
		{
			name: "provider not applicable",
			auth: &sessionpb.ExecutionAuthField{Result: &sessionpb.ExecutionAuthField_Unavailable{
				Unavailable: sessionpb.ExecutionAuthUnavailableReason_EXECUTION_AUTH_UNAVAILABLE_REASON_NOT_APPLICABLE,
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := sessionExecutionEnvironmentTransportResponse("environment-session", test.auth)
			method := bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "ReadService", "GetExecutionEnvironment")
			server := newRemoteTestServer(t, func(ws *websocket.Conn) {
				acceptRemoteHandshake(t, ws)
				acceptRemoteProjectAttachment(t, ws, "workspace-1", "/workspace")
				request := &sessionpb.ExecutionEnvironmentRequest{}
				correlation := receiveRemoteDescriptorCall(t, ws, method, request)
				if request.SessionId != want.Environment.SessionId {
					t.Fatalf("requested Session = %q", request.SessionId)
				}
				sendRemoteDescriptorResult(t, ws, method, correlation, &sessionpb.ExecutionEnvironmentResult{
					Outcome: &sessionpb.ExecutionEnvironmentResult_Success{Success: want},
				})
			})
			remote, err := DialRemoteURLForProject(t.Context(), "ws"+server.URL[len("http"):], "project-1")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = remote.Close() }()
			response, err := remote.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{SessionId: want.Environment.SessionId})
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(response.Environment.Auth, test.auth) {
				t.Fatalf("auth applicability changed: got %v, want %v", response.Environment.Auth, test.auth)
			}
		})
	}
}

func TestRemoteSessionExecutionEnvironmentRejectsMismatchedSession(t *testing.T) {
	method := bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "ReadService", "GetExecutionEnvironment")
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		acceptRemoteProjectAttachment(t, ws, "workspace-1", "/workspace")
		correlation := receiveRemoteDescriptorCall(t, ws, method, &sessionpb.ExecutionEnvironmentRequest{})
		sendRemoteDescriptorResult(t, ws, method, correlation, &sessionpb.ExecutionEnvironmentResult{
			Outcome: &sessionpb.ExecutionEnvironmentResult_Success{Success: sessionExecutionEnvironmentTransportResponse(
				"another-session",
				&sessionpb.ExecutionAuthField{Result: &sessionpb.ExecutionAuthField_Unavailable{
					Unavailable: sessionpb.ExecutionAuthUnavailableReason_EXECUTION_AUTH_UNAVAILABLE_REASON_NOT_APPLICABLE,
				}},
			)},
		})
	})
	remote, err := DialRemoteURLForProject(t.Context(), "ws"+server.URL[len("http"):], "project-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remote.Close() }()
	if _, err := remote.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{SessionId: "environment-session"}); err == nil {
		t.Fatal("accepted environment for a different Session")
	}
}

func sessionExecutionEnvironmentTransportResponse(sessionID string, auth *sessionpb.ExecutionAuthField) *sessionpb.ExecutionEnvironmentSuccess {
	return &sessionpb.ExecutionEnvironmentSuccess{Environment: &sessionpb.ExecutionEnvironment{
		SessionId: sessionID,
		Workspace: &sessionpb.ExecutionWorkspaceField{Result: &sessionpb.ExecutionWorkspaceField_Unavailable{
			Unavailable: sessionpb.ExecutionWorkspaceUnavailableReason_EXECUTION_WORKSPACE_UNAVAILABLE_REASON_NOT_CONFIGURED,
		}},
		Branch: &sessionpb.ExecutionBranchField{Result: &sessionpb.ExecutionBranchField_Unavailable{
			Unavailable: sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_NOT_GIT_REPOSITORY,
		}},
		Auth: auth,
		Model: &sessionpb.ExecutionModelField{Result: &sessionpb.ExecutionModelField_Unavailable{
			Unavailable: sessionpb.ExecutionModelUnavailableReason_EXECUTION_MODEL_UNAVAILABLE_REASON_NOT_CONFIGURED,
		}},
	}}
}
