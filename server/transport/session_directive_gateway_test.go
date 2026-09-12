package transport

import (
	"context"
	"net/http/httptest"
	"testing"

	"core/server/core"
	"core/shared/apicontract"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"google.golang.org/protobuf/types/known/emptypb"
)

type lifecycleResultGatewayService struct {
	last *sessionlaunchpb.SessionResolveTransitionRequest
}

func (s *lifecycleResultGatewayService) Close() error {
	return nil
}

func (s *lifecycleResultGatewayService) GetInitialInput(context.Context, *sessionlaunchpb.SessionInitialInputRequest) (*sessionlaunchpb.SessionInitialInputSuccess, error) {
	return &sessionlaunchpb.SessionInitialInputSuccess{}, nil
}

func (s *lifecycleResultGatewayService) PersistInputDraft(context.Context, *sessionlaunchpb.SessionPersistInputDraftRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (s *lifecycleResultGatewayService) RetargetSessionWorkspace(context.Context, *sessionlaunchpb.SessionRetargetWorkspaceRequest) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	return &sessionlaunchpb.SessionRetargetWorkspaceSuccess{}, nil
}

func (s *lifecycleResultGatewayService) ResolveTransition(_ context.Context, req *sessionlaunchpb.SessionResolveTransitionRequest) (*sessionlaunchpb.SessionDirective, error) {
	s.last = req
	return &sessionlaunchpb.SessionDirective{Directive: &sessionlaunchpb.SessionDirective_SelectSession{
		SelectSession: &sessionlaunchpb.SessionSelectDirective{Auth: sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH},
	}}, nil
}

func (*lifecycleResultGatewayService) ArchiveSession(context.Context, *sessionlaunchpb.SessionArchiveRequest) (*sessionlaunchpb.SessionArchiveSuccess, error) {
	return nil, nil
}

func (*lifecycleResultGatewayService) DeleteSession(context.Context, *sessionlaunchpb.SessionDeleteRequest) (*sessionlaunchpb.SessionDeleteSuccess, error) {
	return nil, nil
}

type lifecycleResultGatewayDependencies struct {
	*core.Core
	lifecycle apicontract.SessionLifecycleService
}

func (d *lifecycleResultGatewayDependencies) SessionLifecycleClient() apicontract.SessionLifecycleService {
	return d.lifecycle
}

func TestGatewaySessionLifecycleResultRoundTrip(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	service := &lifecycleResultGatewayService{}
	deps := &lifecycleResultGatewayDependencies{
		Core:      appCore,
		lifecycle: service,
	}
	gateway, err := NewGateway(deps, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)

	result := &sessionlaunchpb.SessionResolveTransitionResult{}
	callGatewayDescriptor(t, conn, "typed-lifecycle-result",
		sessionlaunchpb.File_kent_api_session_launch_session_lifecycle_proto.Services().ByName("SessionLifecycleService").Methods().ByName("ResolveTransition"),
		&sessionlaunchpb.SessionResolveTransitionRequest{
			Transition: &sessionlaunchpb.SessionTransition{Action: sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_RESUME},
		}, result)
	if result.GetSuccess().GetSelectSession().GetAuth() != sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH {
		t.Fatalf("result = %v, want select Session with current auth", result)
	}
	if service.last.Transition.Action != sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_RESUME {
		t.Fatalf("forwarded action = %q, want resume", service.last.Transition.Action)
	}
}

var _ GatewayDependencies = (*lifecycleResultGatewayDependencies)(nil)
var _ apicontract.SessionLifecycleService = (*lifecycleResultGatewayService)(nil)
