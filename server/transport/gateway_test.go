package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"core/internal/testharness/testsetup"
	"core/server/auth"
	serverbootstrap "core/server/bootstrap"
	"core/server/chatmutation"
	"core/server/core"
	"core/server/metadata"
	"core/server/promptcommands"
	"core/server/runtimecontrol"
	"core/server/session"
	"core/server/sessionruntime"
	shelltool "core/server/tools/shell"
	"core/shared/apicontract"
	remoteclient "core/shared/client"
	"core/shared/llmerrors"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/protocol"
	"core/shared/rpcwire"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

type gatewayChatLifecycleDependencies struct {
	GatewayDependencies
	chat apicontract.ChatMutationService
}

func (d *gatewayChatLifecycleDependencies) ChatMutationClient() apicontract.ChatMutationService {
	return d.chat
}

type gatewayChatLifecycleResolver struct {
	started chan context.Context
	proceed chan struct{}
	target  chatmutation.ResolvedTarget
}

func (*gatewayChatLifecycleResolver) SelectPlacement(_ context.Context, target chatmutation.ResolvedTarget, _ promptcommands.Placement) (chatmutation.ResolvedTarget, error) {
	return target, nil
}

func (r *gatewayChatLifecycleResolver) Resolve(
	ctx context.Context,
	_ chatmutation.TargetResolutionRequest,
) (chatmutation.ResolvedTarget, error) {
	r.started <- ctx
	select {
	case <-r.proceed:
		return r.target, nil
	case <-ctx.Done():
		return chatmutation.ResolvedTarget{}, context.Cause(ctx)
	}
}

type gatewayChatLifecyclePlanner struct {
	attachment chatmutation.RuntimeAttachment
}

func (p gatewayChatLifecyclePlanner) Open(
	context.Context,
	runtimeids.SessionID,
) (chatmutation.RuntimeAttachment, error) {
	return p.attachment, nil
}

type gatewayChatLifecycleAttachment struct {
	sessionID runtimeids.SessionID
	released  chan sessionruntime.RuntimeReleasePolicy
}

func (a *gatewayChatLifecycleAttachment) SessionID() runtimeids.SessionID {
	return a.sessionID
}

func (a *gatewayChatLifecycleAttachment) Release(
	_ context.Context,
	policy sessionruntime.RuntimeReleasePolicy,
) error {
	a.released <- policy
	return nil
}

type gatewayChatLifecycleAdmission struct {
	queueItemID runtimeids.QueueItemID
}

type gatewayChatLifecycleGoal struct{}

func (gatewayChatLifecycleGoal) SetResolvedGoal(
	context.Context,
	*runtimepb.GoalSetRequest,
) (serverapi.ResolvedGoalSetCommit, error) {
	return serverapi.ResolvedGoalSetCommit{}, errors.New("unexpected Goal Set")
}

func (gatewayChatLifecycleAdmission) PrepareUserTurn(ctx context.Context, sessionID string, input runtimeinput.Input) (runtimecontrol.PreparedUserTurn, error) {
	return (*runtimecontrol.Service)(nil).PrepareUserTurn(ctx, sessionID, input)
}

func (a gatewayChatLifecycleAdmission) AdmitChatUserTurn(
	context.Context,
	string,
	runtimecontrol.PreparedUserTurn,
) (serverapi.ChatInputAdmissionResult, error) {
	return serverapi.ChatInputAdmissionResult{
		QueueItemID: a.queueItemID,
		Accepted:    true,
	}, nil
}

func (gatewayChatLifecycleAdmission) AdmitChatQueuedUserInput(
	context.Context,
	string,
	runtimecontrol.PreparedUserTurn,
) (serverapi.ChatInputAdmissionResult, error) {
	panic("unexpected Queue admission")
}

func (gatewayChatLifecycleAdmission) AdmitManualCompaction(
	context.Context,
	*runtimepb.CompactContextRequest,
) (bool, error) {
	panic("unexpected compaction admission")
}

type gatewayChatAuthorizationService struct {
	sessionID string
}

func (s gatewayChatAuthorizationService) Steer(
	context.Context,
	*chatpb.SteerRequest,
) (*chatpb.InputMutationSuccess, error) {
	return &chatpb.InputMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: s.sessionID},
		Outcome: &chatpb.InputMutationSuccess_Accepted{
			Accepted: &chatpb.InputAccepted{
				QueueItem: &chatpb.QueueItemIdentity{Id: runtimeids.NewQueueItemID().String()},
			},
		},
	}, nil
}

func (gatewayChatAuthorizationService) Queue(
	context.Context,
	*chatpb.QueueRequest,
) (*chatpb.InputMutationSuccess, error) {
	panic("unexpected Queue")
}

func (gatewayChatAuthorizationService) Compact(
	context.Context,
	*chatpb.CompactRequest,
) (*chatpb.CompactionMutationSuccess, error) {
	panic("unexpected Compact")
}

func (gatewayChatAuthorizationService) SetGoal(
	context.Context,
	*runtimepb.GoalSetRequest,
) (*runtimepb.GoalSetSuccess, error) {
	return nil, errors.New("unexpected Set")
}

func TestGatewayAuthorizesChatTargetModes(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	gateway, err := NewGateway(
		&gatewayChatLifecycleDependencies{
			GatewayDependencies: fixture.appCore,
			chat: gatewayChatAuthorizationService{
				sessionID: fixture.ownSessionID,
			},
		},
		gatewayTestIdentity(),
	)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	t.Cleanup(server.Close)
	method := chatpb.File_kent_api_chat_chat_proto.Services().
		ByName("ChatService").
		Methods().
		ByName("Steer")

	for _, test := range []struct {
		name     string
		target   *chatpb.ChatTarget
		attached bool
		wantCode string
	}{
		{
			name: "projectless existing Session outside startup Project",
			target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionTarget{SessionId: fixture.foreignSessionID},
			}},
		},
		{
			name: "attached Project existing Session",
			target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionTarget{SessionId: fixture.ownSessionID},
			}},
			attached: true,
		},
		{
			name: "attached Project rejects foreign Session",
			target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionTarget{SessionId: fixture.foreignSessionID},
			}},
			attached: true,
			wantCode: "session_not_found",
		},
		{
			name:     "exact New Chat binding",
			target:   routePolicyNewChatTarget(fixture.bindingA.ProjectID, fixture.bindingA.WorkspaceID),
			attached: true,
		},
		{
			name:     "mismatched New Chat binding",
			target:   routePolicyNewChatTarget(fixture.bindingB.ProjectID, fixture.bindingB.WorkspaceID),
			attached: true,
			wantCode: "workspace_not_registered",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := dialGateway(t, server)
			t.Cleanup(func() { _ = conn.Close() })
			handshakeGateway(t, conn)
			if test.attached {
				requireGatewayProjectAttachment(
					t,
					conn,
					"attach-project",
					&connectionpb.AttachProjectRequest{ProjectId: fixture.bindingA.ProjectID},
				)
			}
			result := &chatpb.SteerResult{}
			callGatewayDescriptor(
				t,
				conn,
				"authorize-chat",
				method,
				&chatpb.SteerRequest{
					Target: test.target,
					Activation: &chatpb.Activation{
						Input: &chatpb.Activation_Text{Text: "continue"},
					},
				},
				result,
			)
			classified, err := protoapi.ClassifyResult(result)
			if err != nil {
				t.Fatalf("classify Chat result: %v", err)
			}
			if test.wantCode == "" {
				if classified.Outcome != protoapi.OperationSuccess {
					t.Fatalf("outcome = %+v, want success", classified)
				}
				return
			}
			if classified.Failure == nil || classified.Failure.Code != test.wantCode {
				t.Fatalf("failure = %+v, want code %q", classified.Failure, test.wantCode)
			}
		})
	}
}

func TestGatewayDisconnectStopsDeliveryWithoutCancelingChatOperation(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	t.Cleanup(func() { _ = appCore.Close() })
	store := createGatewayAuthoritativeSession(t, appCore)
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("parse Session ID: %v", err)
	}
	owner, err := chatmutation.NewOperationOwner(time.Second)
	if err != nil {
		t.Fatalf("NewOperationOwner: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	resolver := &gatewayChatLifecycleResolver{
		started: make(chan context.Context, 1),
		proceed: make(chan struct{}),
		target:  chatmutation.ResolvedTarget{SessionID: sessionID},
	}
	attachment := &gatewayChatLifecycleAttachment{
		sessionID: sessionID,
		released:  make(chan sessionruntime.RuntimeReleasePolicy, 1),
	}
	service := chatmutation.NewService(
		owner,
		resolver,
		gatewayChatLifecyclePlanner{attachment: attachment},
		gatewayChatLifecycleAdmission{queueItemID: runtimeids.NewQueueItemID()},
		gatewayChatLifecycleGoal{},
	)
	gateway, err := NewGateway(
		&gatewayChatLifecycleDependencies{GatewayDependencies: appCore, chat: service},
		gatewayTestIdentity(),
	)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	t.Cleanup(server.Close)
	conn := dialGateway(t, server)
	handshakeGateway(t, conn)

	method := chatpb.File_kent_api_chat_chat_proto.Services().
		ByName("ChatService").
		Methods().
		ByName("Steer")
	operation, err := protoapi.OperationFromDescriptor(method)
	if err != nil {
		t.Fatalf("resolve Chat operation: %v", err)
	}
	payload, err := protoapi.Marshal(&chatpb.SteerRequest{
		Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
			Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
		}},
		Activation: &chatpb.Activation{
			Input: &chatpb.Activation_Text{Text: "continue"},
		},
	})
	if err != nil {
		t.Fatalf("marshal Chat request: %v", err)
	}
	correlation := "disconnect-chat"
	envelope, err := protoapi.EncodeEnvelope(&sharedpb.Envelope{
		Frame: &sharedpb.Envelope_Call{Call: &sharedpb.Call{
			Operation:   operation.Name,
			Correlation: &correlation,
			Payload:     payload,
		}},
	})
	if err != nil {
		t.Fatalf("encode Chat request: %v", err)
	}
	if err := websocket.Message.Send(conn, envelope); err != nil {
		t.Fatalf("send Chat request: %v", err)
	}

	operationCtx := <-resolver.started
	if err := conn.Close(); err != nil {
		t.Fatalf("close caller connection: %v", err)
	}
	select {
	case <-operationCtx.Done():
		t.Fatalf("Gateway disconnect canceled Chat operation: %v", context.Cause(operationCtx))
	default:
	}
	close(resolver.proceed)
	select {
	case policy := <-attachment.released:
		if policy != sessionruntime.RuntimeReleaseDetach {
			t.Fatalf("release policy = %v, want detach", policy)
		}
	case <-time.After(time.Second):
		t.Fatal("Chat operation did not finish after Gateway disconnect")
	}
}

func gatewaySessionExecutionTarget(t *testing.T, conn *websocket.Conn, requestID, sessionID string) *worktreepb.SessionExecutionTarget {
	t.Helper()
	var response sessionpb.MainViewResult
	callGatewayDescriptor(
		t,
		conn,
		requestID,
		sessionpb.File_kent_api_session_session_proto.Services().ByName("ReadService").Methods().ByName("GetMainView"),
		&sessionpb.MainViewRequest{SessionId: sessionID},
		&response,
	)
	return response.GetSuccess().GetMainView().GetSession().GetExecutionTarget()
}

func registerGatewayWorkspace(t *testing.T, workspace string) {
	t.Helper()
	configureGatewayTestServerPort(t)
	resolved := resolveGatewayTestConfig(t, workspace)
	registerGatewayTestBinding(t, resolved.Config)
}

func configureGatewayTestServerPort(t *testing.T) {
	t.Helper()
	port := 56000 + int(gatewayTestPortCounter.Add(1))
	t.Setenv("KENT_SERVER_HOST", "127.0.0.1")
	t.Setenv("KENT_SERVER_PORT", strconv.Itoa(port))
}

var gatewayTestPortCounter atomic.Uint32

func reportGatewayHandlerError(errs chan<- error, format string, args ...any) {
	select {
	case errs <- fmt.Errorf(format, args...):
	default:
	}
}

func requireNoGatewayHandlerError(t *testing.T, errs <-chan error) {
	t.Helper()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}

func TestProtocolErrorMapsRuntimeUnavailable(t *testing.T) {
	code, _ := protocolError(serverapi.ErrRuntimeUnavailable)
	if code != protocol.ErrCodeRuntimeUnavailable {
		t.Fatalf("protocol error code = %d, want %d", code, protocol.ErrCodeRuntimeUnavailable)
	}
}

func TestProtocolErrorMapsWorkflowTaskNotFound(t *testing.T) {
	code, _ := protocolError(serverapi.ErrWorkflowTaskNotFound)
	if code != protocol.ErrCodeWorkflowTaskNotFound {
		t.Fatalf("protocol error code = %d, want %d", code, protocol.ErrCodeWorkflowTaskNotFound)
	}
}

func TestProtocolErrorMapsModelStreamStalled(t *testing.T) {
	code, _ := protocolError(fmt.Errorf("model generation failed after retries: %w", llmerrors.ErrModelStreamStalled))
	if code != protocol.ErrCodeModelStreamStalled {
		t.Fatalf("protocol error code = %d, want %d", code, protocol.ErrCodeModelStreamStalled)
	}
}

func TestProtocolErrorMapsContextCanceled(t *testing.T) {
	code, message := protocolError(context.Canceled)
	if code != protocol.ErrCodeRequestCanceled {
		t.Fatalf("protocol error code = %d, want %d", code, protocol.ErrCodeRequestCanceled)
	}
	if message != canceledByClientMessage {
		t.Fatalf("protocol error message = %q, want %q", message, canceledByClientMessage)
	}
}

func TestProtocolErrorMapsStreamFailureAsStreamFailure(t *testing.T) {
	source := serverapi.ErrStreamFailed
	code, message := protocolError(source)
	if code != protocol.ErrCodeStreamFailed {
		t.Fatalf("protocol error code = %d, want %d", code, protocol.ErrCodeStreamFailed)
	}
	if message != source.Error() {
		t.Fatalf("protocol error message = %q, want %q", message, source.Error())
	}
}

func TestResponseForErrorSurfacesIrreconcilableRecoveryEvidence(t *testing.T) {
	detail := &session.IrreconcilableRecoveryDetail{
		SessionID:             "session-1",
		Operation:             "recover_append_transaction",
		RecoveryPath:          "/sessions/session-1/append-recovery.json",
		EventsPath:            "/sessions/session-1/events.jsonl",
		CurrentMetadataSHA256: "current",
		PreMetadataSHA256:     "pre",
		PostMetadataSHA256:    "post",
		Phase:                 "committed",
		Conflict:              session.IrreconcilableRecoveryConflictCommittedSuffix,
		Suffix: &session.IrreconcilableRecoverySuffixIdentity{
			StartOffset:   101,
			EndOffset:     202,
			EventCount:    2,
			FirstSequence: 7,
			LastSequence:  8,
			SHA256:        "suffix",
		},
	}

	response := responseForError("recovery-conflict", detail)
	if response.Error == nil {
		t.Fatal("recovery-conflict response did not include an error")
	}
	if response.Error.Message != detail.Error() {
		t.Fatalf("recovery-conflict message = %q, want projected detail %q", response.Error.Message, detail.Error())
	}
}

func TestStreamCompleteParamsMapsTerminalErrors(t *testing.T) {
	for _, err := range []error{nil, io.EOF, context.Canceled, context.DeadlineExceeded} {
		params := streamCompleteParams(err)
		if params.Code != 0 || params.Message != "" {
			t.Fatalf("streamCompleteParams(%v) = %+v, want empty completion", err, params)
		}
	}
	params := streamCompleteParams(serverapi.ErrStreamFailed)
	if params.Code != protocol.ErrCodeStreamFailed || params.Message != serverapi.ErrStreamFailed.Error() {
		t.Fatalf("streamCompleteParams(stream failed) = %+v, want stream-failed code/message", params)
	}
	transcriptErr := serverapi.NewTranscriptStreamError(serverapi.TranscriptCloseReasonSubscriberOverflow, serverapi.ErrStreamGap)
	params = streamCompleteParams(transcriptErr)
	if params.Code != protocol.ErrCodeStreamGap || params.TranscriptCloseReason != string(serverapi.TranscriptCloseReasonSubscriberOverflow) {
		t.Fatalf("streamCompleteParams(transcript overflow) = %+v, want stream gap plus typed transcript reason", params)
	}
}

func TestNewGatewayRejectsTypedNilDependencies(t *testing.T) {
	var appCore *core.Core

	gateway, err := NewGateway(appCore, gatewayTestIdentity())
	if err == nil {
		t.Fatal("expected typed nil dependencies to be rejected")
	}
	if gateway != nil {
		t.Fatalf("gateway = %+v, want nil", gateway)
	}
	if !errors.Is(err, ErrGatewayDependenciesRequired) {
		t.Fatalf("error = %q, want ErrGatewayDependenciesRequired", err.Error())
	}
}

func TestCancellationMessageRoundTripsThroughRemoteClient(t *testing.T) {
	code, message := protocolError(&shelltool.PollingCanceledError{SessionID: "1000", Active: true})
	if code != protocol.ErrCodeRequestCanceled {
		t.Fatalf("protocol error code = %d, want %d", code, protocol.ErrCodeRequestCanceled)
	}

	handlerErrs := make(chan error, 8)
	server := httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(func(ctx context.Context, conn rpcwire.Conn) {
		for event := range conn.Events() {
			if event.Err != nil {
				return
			}
			if event.Frame.Kind == rpcwire.FrameBinary {
				if err := serveGatewayRemoteTestHandshake(ctx, conn, event.Frame); err != nil {
					reportGatewayHandlerError(handlerErrs, "send handshake: %v", err)
					return
				}
				continue
			}
			req, err := event.Frame.DecodeRequest()
			if err != nil {
				reportGatewayHandlerError(handlerErrs, "decode request: %v", err)
				return
			}
			switch req.Method {
			case protocol.MethodWorkflowList:
				resp := protocol.NewErrorResponse(req.ID, code, message)
				if err := conn.Send(ctx, rpcwire.FrameFromResponse(resp)); err != nil {
					reportGatewayHandlerError(handlerErrs, "send project list error: %w", err)
				}
				return
			default:
				reportGatewayHandlerError(handlerErrs, "unexpected method %q", req.Method)
				return
			}
		}
	}))
	defer server.Close()

	remote, err := remoteclient.DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()

	_, err = remote.ListWorkflows(
		context.Background(),
		serverapi.WorkflowListRequest{},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListWorkflows error = %v, want context.Canceled", err)
	}
	if err == nil || err.Error() != message {
		t.Fatalf("expected cancellation message %q, got %v", message, err)
	}
	if message == context.Canceled.Error() {
		t.Fatalf("test precondition failed: expected normalized message, got %q", message)
	}
	requireNoGatewayHandlerError(t, handlerErrs)
}

func newGatewayTestAuthSupport(t *testing.T, ready bool) serverbootstrap.AuthSupport {
	t.Helper()
	store := auth.NewMemoryStore(auth.EmptyState())
	authSupport, err := serverbootstrap.BuildAuthSupport(store, nil, nil)
	if err != nil {
		t.Fatalf("BuildAuthSupport: %v", err)
	}
	if ready {
		if err := authSupport.AuthManager.SaveOAuth(context.Background(), "test", auth.OAuthMethod{
			AccessToken: "test-token",
		}); err != nil {
			t.Fatalf("SaveOAuth: %v", err)
		}
	}
	return authSupport
}

func activateGatewayController(t *testing.T, appCore *core.Core, sessionID string) serverapi.SessionRuntimeAttachment {
	t.Helper()
	settings := appCore.Config().Settings
	if strings.TrimSpace(settings.Model) == "" {
		settings.Model = "gpt-5"
	}
	settings = testsetup.WriteProviderSettings(t, appCore.Config().PersistenceRoot, settings)
	response, err := appCore.SessionRuntimeClient().ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		SessionID:             strings.TrimSpace(sessionID),
		OwnerID:               "gateway-test-owner",
		ActiveSettings:        settings,
		QuestionsEnabled:      textutil.Value(true),
		AutoCompactionEnabled: textutil.Value(true),
		Source:                appCore.Config().Source,
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime: %v", err)
	}
	if err := response.ValidateForSession(sessionID); err != nil {
		t.Fatalf("validate activation response: %v", err)
	}
	return response
}

func releaseGatewayController(t *testing.T, appCore *core.Core, attachment serverapi.SessionRuntimeAttachment) {
	t.Helper()
	if _, err := appCore.SessionRuntimeClient().ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		Attachment: attachment,
		OwnerID:    "gateway-test-owner",
	}); err != nil {
		t.Fatalf("ReleaseSessionRuntime: %v", err)
	}
}

func gatewayRuntimeActivateRequest(appCore *core.Core, sessionID string) serverapi.SessionRuntimeActivateRequest {
	settings := appCore.Config().Settings
	if strings.TrimSpace(settings.Model) == "" {
		settings.Model = "gpt-5"
	}
	settings = testsetup.ProviderSettings(settings)
	return serverapi.SessionRuntimeActivateRequest{
		SessionID:             strings.TrimSpace(sessionID),
		ActiveSettings:        settings,
		QuestionsEnabled:      textutil.Value(true),
		AutoCompactionEnabled: textutil.Value(true),
		Source:                appCore.Config().Source,
	}
}

func waitForGatewayCondition(t *testing.T, label string, condition func() bool) {
	t.Helper()
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, condition, "timed out waiting for %s", label)
}

type countingSessionRuntimeClient struct {
	apicontract.SessionRuntimeService
	releaseCount        atomic.Int32
	activateRequests    chan serverapi.SessionRuntimeActivateRequest
	releaseRequests     chan serverapi.SessionRuntimeReleaseRequest
	activateAttachments []*serverapi.SessionRuntimeAttachment
	releaseResponse     *sessionlaunchpb.SessionRuntimeReleaseSuccess
}

func (c *countingSessionRuntimeClient) ActivateSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeAttachment, error) {
	if c.activateRequests != nil {
		c.activateRequests <- req
	}
	if len(c.activateAttachments) != 0 {
		attachment := c.activateAttachments[0]
		c.activateAttachments = c.activateAttachments[1:]
		if attachment == nil {
			return serverapi.SessionRuntimeAttachment{}, nil
		}
		value := *attachment
		value.SessionID = req.SessionID
		return value, nil
	}
	return c.SessionRuntimeService.ActivateSessionRuntime(ctx, req)
}

func (c *countingSessionRuntimeClient) ReleaseSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeReleaseRequest) (*sessionlaunchpb.SessionRuntimeReleaseSuccess, error) {
	defer func() {
		c.releaseCount.Add(1)
		if c.releaseRequests != nil {
			c.releaseRequests <- req
		}
	}()
	if c.releaseResponse != nil {
		return c.releaseResponse, nil
	}
	return c.SessionRuntimeService.ReleaseSessionRuntime(ctx, req)
}

type gatewayRuntimeClientOverride struct {
	*core.Core
	runtimeClient apicontract.SessionRuntimeService
}

func (d *gatewayRuntimeClientOverride) SessionRuntimeClient() apicontract.SessionRuntimeService {
	return d.runtimeClient
}

func TestGatewayConnectionCloseDetachesOwnedRuntime(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	counter := &countingSessionRuntimeClient{
		SessionRuntimeService: appCore.SessionRuntimeClient(),
		activateRequests:      make(chan serverapi.SessionRuntimeActivateRequest, 2),
		releaseRequests:       make(chan serverapi.SessionRuntimeReleaseRequest, 3),
		activateAttachments:   []*serverapi.SessionRuntimeAttachment{{Generation: 1}, {Generation: 2}},
		releaseResponse:       &sessionlaunchpb.SessionRuntimeReleaseSuccess{Released: true},
	}
	gateway, err := NewGateway(&gatewayRuntimeClientOverride{Core: appCore, runtimeClient: counter}, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)

	conn, err := remoteclient.DialRemoteURL(t.Context(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	request := gatewayRuntimeActivateRequest(appCore, store.Meta().SessionID)
	request.OwnerID = "client-spoof"
	request.AgentSelection = &serverapi.SessionRuntimeAgentSelection{
		AgentRole: textutil.Value("worker"),
		Baseline: serverapi.SessionRuntimeChatSettings{
			Supervisor: "off", Thinking: "high", Fast: true, Questions: false, AutoCompaction: true,
		},
	}
	activation, err := conn.ActivateSessionRuntime(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	var activationRequest serverapi.SessionRuntimeActivateRequest
	select {
	case activationRequest = <-counter.activateRequests:
		if activationRequest.OwnerID == "" || activationRequest.OwnerID == "client-spoof" {
			t.Fatalf("gateway did not inject connection owner id: %+v", activationRequest)
		}
		if activationRequest.AgentSelection == nil ||
			!textutil.EqualOptional(activationRequest.AgentSelection.AgentRole, request.AgentSelection.AgentRole) ||
			activationRequest.AgentSelection.Baseline != request.AgentSelection.Baseline {
			t.Fatalf("planned Agent selection changed: %v", activationRequest.AgentSelection)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for activation request")
	}
	successor, err := conn.ActivateSessionRuntime(t.Context(), gatewayRuntimeActivateRequest(appCore, store.Meta().SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if (<-counter.activateRequests).AgentSelection != nil {
		t.Fatal("absent Agent selection became present")
	}
	_, err = conn.ReleaseSessionRuntime(t.Context(), serverapi.SessionRuntimeReleaseRequest{
		Attachment:  activation,
		OwnerID:     "client-spoof",
		DropOwner:   true,
		ClosePolicy: serverapi.SessionRuntimeReleaseClosePolicyDetachOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-counter.releaseRequests:
		if request.Attachment != activation || request.OwnerID != activationRequest.OwnerID {
			t.Fatalf("explicit stale release request = %+v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stale explicit release")
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close gateway connection: %v", err)
	}
	select {
	case request := <-counter.releaseRequests:
		if request.Attachment != successor {
			t.Fatalf("disconnect release attachment = %+v, want successor %+v", request.Attachment, successor)
		}
		if request.OwnerID != activationRequest.OwnerID || !request.DropOwner || request.ClosePolicy != serverapi.SessionRuntimeReleaseClosePolicyDetachOnly {
			t.Fatalf("disconnect release request = %+v, want exact detach-only owner drop", request)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for disconnect runtime release")
	}
	select {
	case request := <-counter.releaseRequests:
		t.Fatalf("disconnect released stale attachment too: %+v", request.Attachment)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestGatewayDetachOnlyReleaseInjectsOwnerAndSkipsDisconnectRelease(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	counter := &countingSessionRuntimeClient{
		SessionRuntimeService: appCore.SessionRuntimeClient(),
		releaseRequests:       make(chan serverapi.SessionRuntimeReleaseRequest, 4),
		activateAttachments:   []*serverapi.SessionRuntimeAttachment{{Generation: 1}},
		releaseResponse:       &sessionlaunchpb.SessionRuntimeReleaseSuccess{Active: true},
	}
	gateway, err := NewGateway(&gatewayRuntimeClientOverride{Core: appCore, runtimeClient: counter}, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)

	conn := dialGatewayRemote(t, server)
	activation, err := conn.ActivateSessionRuntime(t.Context(), gatewayRuntimeActivateRequest(appCore, store.Meta().SessionID))
	if err != nil {
		t.Fatal(err)
	}
	release, err := conn.ReleaseSessionRuntime(t.Context(), serverapi.SessionRuntimeReleaseRequest{
		Attachment:  activation,
		OwnerID:     "client-spoof",
		DropOwner:   true,
		ClosePolicy: serverapi.SessionRuntimeReleaseClosePolicyDetachOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	if release.Released || !release.Active {
		t.Fatalf("detach-only release response = %+v, want active unreleased response", release)
	}
	select {
	case req := <-counter.releaseRequests:
		if req.OwnerID == "" || req.OwnerID == "client-spoof" {
			t.Fatalf("gateway did not inject connection owner id: %+v", req)
		}
		if !req.DropOwner || req.ClosePolicy != serverapi.SessionRuntimeReleaseClosePolicyDetachOnly {
			t.Fatalf("gateway release request = %+v, want detach-only drop owner", req)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for explicit detach-only release")
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close gateway connection: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := counter.releaseCount.Load(); got != 1 {
		t.Fatalf("runtime release call count = %d, want only explicit detach-only release", got)
	}
}

func TestGatewayCloseIfIdleReleasePropagatesPolicy(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	counter := &countingSessionRuntimeClient{
		SessionRuntimeService: appCore.SessionRuntimeClient(),
		releaseRequests:       make(chan serverapi.SessionRuntimeReleaseRequest, 4),
		activateAttachments:   []*serverapi.SessionRuntimeAttachment{{Generation: 1}},
		releaseResponse:       &sessionlaunchpb.SessionRuntimeReleaseSuccess{Released: true},
	}
	gateway, err := NewGateway(&gatewayRuntimeClientOverride{Core: appCore, runtimeClient: counter}, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)

	conn := dialGatewayRemote(t, server)
	activation, err := conn.ActivateSessionRuntime(t.Context(), gatewayRuntimeActivateRequest(appCore, store.Meta().SessionID))
	if err != nil {
		t.Fatal(err)
	}
	release, err := conn.ReleaseSessionRuntime(t.Context(), serverapi.SessionRuntimeReleaseRequest{
		Attachment:  activation,
		DropOwner:   true,
		ClosePolicy: serverapi.SessionRuntimeReleaseClosePolicyCloseIfIdle,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !release.Released {
		t.Fatalf("close-if-idle release response = %+v, want released", release)
	}
	select {
	case req := <-counter.releaseRequests:
		if req.OwnerID == "" {
			t.Fatalf("gateway did not inject owner id: %+v", req)
		}
		if req.Attachment != activation || !req.DropOwner || req.ClosePolicy != serverapi.SessionRuntimeReleaseClosePolicyCloseIfIdle {
			t.Fatalf("gateway release request = %+v, want explicit close-if-idle drop owner", req)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for explicit close-if-idle release")
	}
}

func TestGatewayDisconnectKeepsRuntimeAvailableUntilExplicitCloseIfIdle(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	counter := &countingSessionRuntimeClient{SessionRuntimeService: appCore.SessionRuntimeClient()}
	gateway, err := NewGateway(&gatewayRuntimeClientOverride{Core: appCore, runtimeClient: counter}, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)

	first := dialGatewayRemote(t, server)
	firstActivation, err := first.ActivateSessionRuntime(t.Context(), gatewayRuntimeActivateRequest(appCore, store.Meta().SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first gateway connection: %v", err)
	}
	waitForGatewayCondition(t, "completed first connection owner release", func() bool {
		return counter.releaseCount.Load() == 1
	})

	second := dialGatewayRemote(t, server)
	defer second.Close()
	secondActivation, err := second.ActivateSessionRuntime(t.Context(), gatewayRuntimeActivateRequest(appCore, store.Meta().SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if secondActivation.Generation != firstActivation.Generation {
		t.Fatalf(
			"disconnect replaced the runtime: first generation %d, second generation %d",
			firstActivation.Generation,
			secondActivation.Generation,
		)
	}

	release, err := second.ReleaseSessionRuntime(t.Context(),
		serverapi.SessionRuntimeReleaseRequest{
			Attachment:  secondActivation,
			DropOwner:   true,
			ClosePolicy: serverapi.SessionRuntimeReleaseClosePolicyCloseIfIdle,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !release.Released || release.Active {
		t.Fatalf("explicit close-if-idle release = %+v, want released inactive runtime", release)
	}
}

func TestGatewayMissingActivationAttachmentDoesNotRecordRuntimeOwnership(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	counter := &countingSessionRuntimeClient{
		SessionRuntimeService: appCore.SessionRuntimeClient(),
		activateAttachments:   []*serverapi.SessionRuntimeAttachment{nil},
	}
	gateway, err := NewGateway(&gatewayRuntimeClientOverride{Core: appCore, runtimeClient: counter}, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)

	conn := dialGatewayRemote(t, server)
	if _, err := conn.ActivateSessionRuntime(t.Context(), gatewayRuntimeActivateRequest(appCore, store.Meta().SessionID)); err == nil {
		t.Fatal("activation without an attachment succeeded")
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close gateway connection: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := counter.releaseCount.Load(); got != 0 {
		t.Fatalf("runtime release call count after invalid activation response = %d, want 0", got)
	}
}

func dialGatewayRemote(t *testing.T, server *httptest.Server) *remoteclient.Remote {
	t.Helper()
	remote, err := remoteclient.DialRemoteURL(t.Context(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	return remote
}

func TestGatewayHandshakeAndProjectList(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()

	handshakeGateway(t, conn)

	malformedCorrelation := "malformed-call"
	malformedCall, err := protoapi.Marshal(&sharedpb.Envelope{
		Frame: &sharedpb.Envelope_Call{Call: &sharedpb.Call{
			Correlation: &malformedCorrelation,
		}},
	})
	if err != nil {
		t.Fatalf("marshal validation-invalid call: %v", err)
	}
	if err := websocket.Message.Send(conn, malformedCall); err != nil {
		t.Fatalf("send validation-invalid call: %v", err)
	}
	var failureFrame []byte
	if err := websocket.Message.Receive(conn, &failureFrame); err != nil {
		t.Fatalf("receive validation-invalid call failure: %v", err)
	}
	failureEnvelope, err := protoapi.DecodeEnvelope(failureFrame)
	if err != nil {
		t.Fatalf("decode validation-invalid call failure: %v", err)
	}
	failure := failureEnvelope.GetTransportFailure()
	if failure == nil ||
		failure.Code != sharedpb.TransportFailureCode_TRANSPORT_FAILURE_CODE_MALFORMED_ENVELOPE ||
		failure.GetCorrelation() != malformedCorrelation {
		t.Fatalf("validation-invalid call failure = %+v", failure)
	}

	projectCatalog := projectpb.File_kent_api_project_project_proto.Services().ByName("ProjectCatalogService")
	if projectCatalog == nil {
		t.Fatal("Project Catalog descriptor is required")
	}
	projectList := projectCatalog.Methods().ByName("List")
	if projectList == nil {
		t.Fatal("Project List descriptor is required")
	}
	result := &projectpb.ProjectListResult{}
	callGatewayDescriptor(t, conn, "2", projectList, &emptypb.Empty{}, result)
	if result.GetError() != nil {
		t.Fatalf("Project List error: %+v", result.GetError())
	}
	projects := result.GetSuccess().GetProjects()
	if len(projects) != 1 || projects[0].ProjectId != appCore.ProjectID() {
		t.Fatalf("unexpected project list: %+v", projects)
	}
}

func TestGatewayHandshakeRejectsProtocolVersionMismatch(t *testing.T) {
	_, server := newGatewayTestServer(t)
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()

	result := handshakeGatewayVersion(t, conn, "46")
	failure := result.GetError()
	if failure == nil ||
		failure.Code != "protocol_version_mismatch" ||
		failure.GetProtocolVersionMismatch().RequiredProtocolVersion != protocol.Version {
		t.Fatalf("expected unsupported protocol version error, got %+v", failure)
	}
}

func TestGatewayTaskSearchDispatchesIndexedResponseAndTypedValidationError(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	task := createGatewaySearchableTask(t, appCore)

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)

	request := serverapi.TaskSearchRequest{
		Mode:     serverapi.TaskSearchModeLiteral,
		Query:    "needle",
		Context:  serverapi.TaskSearchDefaultContext,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	}
	var response serverapi.TaskSearchResponse
	callGateway(t, conn, "search", protocol.MethodWorkflowTaskSearch, request, &response)
	if err := response.Validate(); err != nil {
		t.Fatalf("search response validation: %v", err)
	}
	if response.Mode != request.Mode ||
		len(response.Groups) != 1 ||
		response.Groups[0].TaskID != task.ID ||
		response.Groups[0].TotalHitCount != 1 ||
		len(response.Groups[0].Hits) != 1 ||
		response.Groups[0].Hits[0].Source.Kind != serverapi.TaskSearchSourceKindBody ||
		response.Groups[0].Hits[0].Literal == nil ||
		response.NextOffset != nil {
		t.Fatalf("indexed search response = %+v", response)
	}

	responseError := callGatewayExpectError(t, conn, "short", protocol.MethodWorkflowTaskSearch, serverapi.TaskSearchRequest{
		Mode:     serverapi.TaskSearchModeLiteral,
		Query:    "ab",
		Context:  serverapi.TaskSearchDefaultContext,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	})
	if responseError.Code != protocol.ErrCodeWorkflowTaskSearch {
		t.Fatalf("short literal error = %+v, want task search code", responseError)
	}
	decoded := serverapi.DecodeTaskSearchError(responseError.Data, responseError.Message)
	var typed *serverapi.TaskSearchError
	if !errors.As(decoded, &typed) || typed.Reason != serverapi.TaskSearchErrorReasonNormalizedTooShort {
		t.Fatalf("short literal decoded error = %T %v", decoded, decoded)
	}
}

func TestGatewayRemoteTaskSearchRoundsTripIndexedResponse(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	task := createGatewaySearchableTask(t, appCore)

	remote, err := remoteclient.DialRemoteURLForProject(
		context.Background(),
		"ws"+server.URL[len("http"):],
		appCore.ProjectID(),
	)
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	defer func() { _ = remote.Close() }()

	response, err := remote.SearchWorkflowTasks(context.Background(), serverapi.TaskSearchRequest{
		Mode:     serverapi.TaskSearchModeFTS5,
		Query:    "body:needle",
		Context:  serverapi.TaskSearchDefaultContext,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("SearchWorkflowTasks: %v", err)
	}
	if err := response.Validate(); err != nil {
		t.Fatalf("validate remote task-search response: %v", err)
	}
	if response.Mode != serverapi.TaskSearchModeFTS5 ||
		len(response.Groups) != 1 ||
		response.Groups[0].TaskID != task.ID ||
		len(response.Groups[0].Hits) != 1 ||
		response.Groups[0].Hits[0].Source.Kind != serverapi.TaskSearchSourceKindBody ||
		response.Groups[0].Hits[0].FTS5 == nil {
		t.Fatalf("remote indexed task-search response = %+v", response)
	}
}

func TestGatewayRemoteWorkflowTaskSessionsRoundsTripPage(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	task := createGatewaySearchableTask(t, appCore)

	remote, err := remoteclient.DialRemoteURLForProject(
		context.Background(),
		"ws"+server.URL[len("http"):],
		appCore.ProjectID(),
	)
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	defer func() { _ = remote.Close() }()

	response, err := remote.ListWorkflowTaskSessions(context.Background(), serverapi.WorkflowTaskOffsetPageRequest{
		TaskID: task.ID,
	})
	if err != nil {
		t.Fatalf("ListWorkflowTaskSessions: %v", err)
	}
	if response.TaskID != task.ID || response.Items == nil || len(response.Items) != 0 || response.NextOffset != nil {
		t.Fatalf("response = %+v", response)
	}
}

func createGatewaySearchableTask(t *testing.T, appCore *core.Core) serverapi.WorkflowTaskSummary {
	t.Helper()
	ctx := context.Background()
	workflows := appCore.WorkflowClient()
	created, err := workflows.CreateWorkflow(ctx, serverapi.WorkflowCreateRequest{Name: "Search Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	definition, err := workflows.GetWorkflow(ctx, serverapi.WorkflowGetRequest{WorkflowID: created.Workflow.ID})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	startID, terminalID := "", ""
	for _, node := range definition.Definition.Nodes {
		switch node.Kind {
		case "start":
			startID = node.ID
		case "terminal":
			terminalID = node.ID
		}
	}
	if startID == "" || terminalID == "" {
		t.Fatalf("workflow definition lacks start/terminal Nodes: %+v", definition.Definition.Nodes)
	}
	agentID := runtimeids.NewGraphEntityID()
	reviewID := runtimeids.NewGraphEntityID()
	startGroupID := runtimeids.NewGraphEntityID()
	doneGroupID := runtimeids.NewGraphEntityID()
	finishGroupID := runtimeids.NewGraphEntityID()
	graph := serverapi.WorkflowGraphDraftFromDefinition(definition.Definition)
	graph.Nodes = append(graph.Nodes,
		serverapi.WorkflowGraphDraftNode{ID: agentID, Key: "agent", Kind: "agent", DisplayName: "Agent", SubagentRole: "coder"},
		serverapi.WorkflowGraphDraftNode{ID: reviewID, Key: "review", Kind: "agent", DisplayName: "Review", SubagentRole: "coder"},
	)
	graph.TransitionGroups = append(graph.TransitionGroups,
		serverapi.WorkflowGraphDraftTransitionGroup{ID: startGroupID, SourceNodeID: startID, TransitionID: "start", DisplayName: "Start"},
		serverapi.WorkflowGraphDraftTransitionGroup{ID: doneGroupID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"},
		serverapi.WorkflowGraphDraftTransitionGroup{ID: finishGroupID, SourceNodeID: reviewID, TransitionID: "finish", DisplayName: "Finish"},
	)
	graph.Edges = append(graph.Edges,
		serverapi.WorkflowGraphDraftEdge{ID: runtimeids.NewGraphEntityID(), TransitionGroupID: startGroupID, Key: "start", TargetNodeID: agentID, AssigneeSelection: "configured", ThinkingSelection: "configured", ContextMode: "new_session", PromptTemplate: "Search work."},
		serverapi.WorkflowGraphDraftEdge{ID: runtimeids.NewGraphEntityID(), TransitionGroupID: doneGroupID, Key: "done", TargetNodeID: reviewID, AssigneeSelection: "configured", ThinkingSelection: "configured", ContextMode: "new_session", PromptTemplate: "Review the search work."},
		serverapi.WorkflowGraphDraftEdge{ID: runtimeids.NewGraphEntityID(), TransitionGroupID: finishGroupID, Key: "finish", TargetNodeID: terminalID, AssigneeSelection: "configured", ThinkingSelection: "configured", ContextMode: "new_session"},
	)
	saved, err := workflows.SaveWorkflowGraph(ctx, serverapi.WorkflowGraphSaveRequest{
		WorkflowID: created.Workflow.ID, ExpectedVersion: definition.Definition.Workflow.Version, Graph: graph,
	})
	if err != nil || !saved.Saved {
		t.Fatalf("SaveWorkflowGraph searchable task fixture = %+v, err = %v", saved, err)
	}
	if _, err := workflows.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{
		ProjectID:     appCore.ProjectID(),
		WorkflowID:    created.Workflow.ID,
		DefaultPolicy: serverapi.WorkflowProjectLinkDefaultAlways,
	}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := workflows.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{
		ProjectID: appCore.ProjectID(),
		Title:     "Search Task",
		Body:      "needle body",
	})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	return task.Task
}

func TestGatewayRejectsMethodsBeforeHandshake(t *testing.T) {
	_, server := newGatewayTestServer(t)
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()

	sendGatewayRequest(t, conn, "1", protocol.MethodWorkflowList, map[string]any{"project_id": "project-1"})
	var response protocol.Response
	if err := websocket.JSON.Receive(conn, &response); err == nil {
		t.Fatalf("pre-handshake application traffic unexpectedly received %+v", response)
	}
}

func TestGatewayPreAuthMethodPolicy(t *testing.T) {
	registration, err := productionGatewayRegistration()
	if err != nil {
		t.Fatalf("production Gateway registration: %v", err)
	}
	if err := registration.Validate(); err != nil {
		t.Fatalf("validate production Gateway registration: %v", err)
	}
	executor := newRoutePolicyExecutor(&Gateway{registration: registration})
	for name, operation := range registration.operations {
		activeIdentity := name
		if route, legacy := registration.legacy[name]; legacy {
			activeIdentity = route.Method
		}
		got := executor.requiresServerAuth(activeIdentity)
		want := operation.Options.AuthenticationStage == sharedpb.AuthenticationStage_AUTHENTICATION_STAGE_SERVER
		if got != want {
			t.Fatalf("requiresServerAuth(%q) = %t, want %t from descriptor", activeIdentity, got, want)
		}
	}
}

func TestGatewayAuthBootstrapAuthlessConnectionIsReady(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach-project", &connectionpb.AttachProjectRequest{ProjectId: appCore.ProjectID()})
	complete := callGatewayAuthCompleteBootstrap(t, conn, "complete-no-auth", &authpb.CompleteBootstrapRequest{
		ConnectionId: proto.String("test"),
		Mode:         authpb.BootstrapMode_BOOTSTRAP_MODE_NONE,
	})
	if !complete.AuthReady || complete.Method != authpb.AuthMethod_AUTH_METHOD_NONE {
		t.Fatalf("CompleteBootstrap auth-less = %+v", complete)
	}

	plan := callGatewaySessionPlan(t, conn, "plan-after-no-auth", gatewaySessionPlanRequest(t))
	target := gatewaySessionExecutionTarget(t, conn, "main-view-after-no-auth", plan.Plan.SessionId)
	if strings.TrimSpace(target.EffectiveWorkdir) == "" {
		t.Fatalf("typed session target after no-auth has empty effective workdir: %+v", target)
	}
}

func TestGatewayRejectsProjectWorkspaceMutationBeforeServerAuthReady(t *testing.T) {
	appCore, server, _ := newGatewayTestServerWithAuth(t, false)
	defer func() { _ = appCore.Close() }()
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach-project", &connectionpb.AttachProjectRequest{ProjectId: appCore.ProjectID()})

	method := projectpb.File_kent_api_project_project_proto.Services().
		ByName("ProjectCatalogService").Methods().ByName("AttachWorkspace")
	var result projectpb.AttachWorkspaceResult
	callGatewayDescriptor(t, conn, "attach-workspace", method, &projectpb.AttachWorkspaceRequest{
		ProjectId: appCore.ProjectID(), WorkspaceRoot: "/tmp/workspace",
	}, &result)
	if failure := result.GetError(); failure == nil || failure.Code != "auth_required" {
		t.Fatalf("Project AttachWorkspace result = %+v, want auth_required", &result)
	}
}

func TestGatewaySessionTranscriptSubscriptionReturnsHydrationOnDedicatedRoute(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	store := createGatewayAuthoritativeSession(t, appCore)
	attachment := activateGatewayController(t, appCore, store.Meta().SessionID)
	defer releaseGatewayController(t, appCore, attachment)
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach-project", &connectionpb.AttachProjectRequest{ProjectId: appCore.ProjectID()})
	requireGatewaySessionAttachment(t, conn, "attach-session", store.Meta().SessionID)
	service := transcriptpb.File_kent_api_transcript_transcript_proto.Services().ByName("StreamService")
	var result transcriptpb.SubscribeResult
	callGatewayDescriptor(t, conn, "subscribe-transcript", service.Methods().ByName("Subscribe"),
		&transcriptpb.SubscribeRequest{SessionId: store.Meta().SessionID}, &result)
	if result.GetSuccess() == nil {
		t.Fatalf("subscribe: %v", &result)
	}
	var message transcriptpb.Message
	receiveGatewayDescriptorNotification(t, conn, service.Methods().ByName("Event"), &message)
	if message.Sequence != 1 || message.GetEvent().GetHydration() == nil {
		t.Fatalf("transcript message = %+v, want seq=1 hydration", &message)
	}
}

func TestGatewayQuestionHistorySubscriptionPassesAttachedSessionPreflight(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)
	if _, err := store.MaterializeEventLog(); err != nil {
		t.Fatalf("materialize Question-history event log: %v", err)
	}

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach-project", &connectionpb.AttachProjectRequest{ProjectId: appCore.ProjectID()})
	requireGatewaySessionAttachment(t, conn, "attach-session", store.Meta().SessionID)
	service := sessionpb.File_kent_api_session_session_proto.Services().ByName("QuestionHistoryService")
	var result sessionpb.QuestionHistorySubscribeResult
	callGatewayDescriptor(t, conn, "subscribe-question-history", service.Methods().ByName("Subscribe"),
		&sessionpb.QuestionHistorySubscribeRequest{SessionId: store.Meta().SessionID, MaxHandoffs: 1}, &result)
	if result.GetSuccess() == nil {
		t.Fatalf("subscribe Question history: %v", &result)
	}
	var started sessionpb.QuestionHistoryEvent
	receiveGatewayDescriptorNotification(t, conn, service.Methods().ByName("Event"), &started)
	if started.GetStarted() == nil {
		t.Fatalf("expected Question history started: %v", &started)
	}
	var completed sessionpb.QuestionHistoryEvent
	receiveGatewayDescriptorNotification(t, conn, service.Methods().ByName("Event"), &completed)
	if completed.GetCompleted() == nil {
		t.Fatalf("expected Question history completed: %v", &completed)
	}
	receiveGatewayDescriptorNotification(t, conn, service.Methods().ByName("Complete"), &sharedpb.StreamCompletion{})
}

func gatewaySessionPlanRequest(t *testing.T) *sessionlaunchpb.SessionPlanRequest {
	t.Helper()
	intent, err := protoapi.SessionLaunchIntentToProto(
		serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	)
	if err != nil {
		t.Fatalf("encode Session launch intent: %v", err)
	}
	return &sessionlaunchpb.SessionPlanRequest{
		Mode:   sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
		Intent: intent,
	}
}

func callGatewaySessionPlanResult(
	t *testing.T,
	conn *websocket.Conn,
	correlation string,
	request *sessionlaunchpb.SessionPlanRequest,
) *sessionlaunchpb.SessionPlanResult {
	t.Helper()
	method := sessionlaunchpb.File_kent_api_session_launch_session_launch_proto.Services().
		ByName("SessionLaunchService").Methods().ByName("Plan")
	result := &sessionlaunchpb.SessionPlanResult{}
	callGatewayDescriptor(t, conn, correlation, method, request, result)
	return result
}

func callGatewaySessionPlan(
	t *testing.T,
	conn *websocket.Conn,
	correlation string,
	request *sessionlaunchpb.SessionPlanRequest,
) *sessionlaunchpb.SessionPlanSuccess {
	t.Helper()
	result := callGatewaySessionPlanResult(t, conn, correlation, request)
	if failure := result.GetError(); failure != nil {
		t.Fatalf("Session Plan failed: %+v", failure)
	}
	return result.GetSuccess()
}

func TestGatewayRejectsSessionAccessOutsideAttachedProject(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	configureGatewayTestServerPort(t)

	resolvedA := resolveGatewayTestConfig(t, workspaceA)
	bindingA := registerGatewayTestBinding(t, resolvedA.Config)
	resolvedB := resolveGatewayTestConfig(t, workspaceB)
	bindingB := registerGatewayTestBinding(t, resolvedB.Config)
	metadataStore, err := metadata.Open(resolvedA.Config.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	foreignSession, err := session.Create(
		filepath.Join(filepath.Join(resolvedB.Config.PersistenceRoot, "projects"), bindingB.ProjectID, "sessions"),
		"workspace-b",
		resolvedB.Config.WorkspaceRoot,
		sessioncontract.SessionCategoryMain,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create foreign: %v", err)
	}
	if err := foreignSession.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable foreign: %v", err)
	}
	if _, err := metadataStore.ResolveSessionExecutionTarget(context.Background(), foreignSession.Meta().SessionID); err != nil {
		t.Fatalf("ResolveSessionExecutionTarget precondition: %v", err)
	}
	record, err := metadataStore.ResolvePersistedSession(context.Background(), foreignSession.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolvePersistedSession precondition: %v", err)
	}
	opened, err := session.Open(record.SessionDir, metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Open precondition: %v", err)
	}
	_ = opened

	_, server := newGatewayTestServerForConfig(t, resolvedA.Config)

	remote, err := remoteclient.DialRemoteURLForProject(context.Background(), "ws"+server.URL[len("http"):], bindingA.ProjectID)
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	defer func() { _ = remote.Close() }()

	if _, err := remote.GetSessionMainView(context.Background(), &sessionpb.MainViewRequest{SessionId: foreignSession.Meta().SessionID}); err == nil {
		t.Fatal("expected foreign-project session view access to be rejected")
	}
	if _, err := remote.GetLatestCommittedAssistantFinalAnswer(context.Background(), &transcriptpb.LatestFinalAnswerRequest{SessionId: foreignSession.Meta().SessionID}); err == nil {
		t.Fatal("expected foreign-project final answer access to be rejected")
	}
	if _, err := remote.PersistInputDraft(context.Background(), &sessionlaunchpb.SessionPersistInputDraftRequest{SessionId: foreignSession.Meta().SessionID, Input: "should fail"}); err == nil {
		t.Fatal("expected foreign-project session mutation to be rejected")
	}
	if _, err := remote.RetargetSessionWorkspace(context.Background(), &sessionlaunchpb.SessionRetargetWorkspaceRequest{SessionId: foreignSession.Meta().SessionID, WorkspaceRoot: resolvedA.Config.WorkspaceRoot}); err == nil {
		t.Fatal("expected foreign-project session retarget to be rejected")
	}
	foreignSessionID, err := runtimeids.ParseSessionID(foreignSession.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID foreign: %v", err)
	}
	if _, err := remote.ReadChatSettings(context.Background(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: foreignSessionID.String()}},
	}); err == nil {
		t.Fatal("expected foreign-project Chat settings access to be rejected")
	}
	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach-project-for-foreign-goal-checks", &connectionpb.AttachProjectRequest{ProjectId: bindingA.ProjectID})
	assertForeignGoalAccessRejected(t, conn, foreignSession.Meta().SessionID)
	if bindingA.ProjectID == bindingB.ProjectID {
		t.Fatalf("expected distinct project ids, both=%q", bindingA.ProjectID)
	}
}

func TestGatewayAuthorizesNewChatAndSessionSettingsTargets(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	workspace, err := appCore.MetadataStore().ResolveProjectSourceWorkspace(
		t.Context(),
		appCore.ProjectID(),
	)
	if err != nil {
		t.Fatalf("ResolveProjectSourceWorkspace: %v", err)
	}
	remote, err := remoteclient.DialRemoteURLForProject(
		t.Context(),
		"ws"+server.URL[len("http"):],
		appCore.ProjectID(),
	)
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	defer func() { _ = remote.Close() }()

	newChat, err := remote.ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_NewChat{NewChat: &chatsettingspb.NewChatTarget{ProjectId: appCore.ProjectID(), WorkspaceId: workspace.ID}},
	})
	if err != nil {
		t.Fatalf("ReadChatSettings New Chat: %v", err)
	}
	if newChat.GetSession() != nil {
		t.Fatalf("New Chat response has Session facts: %+v", newChat.GetSession())
	}
	sessionSettings, err := remote.ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: sessionID.String()}},
	})
	if err != nil {
		t.Fatalf("ReadChatSettings Session: %v", err)
	}
	if sessionSettings.GetSession() == nil || sessionSettings.GetSession().Session.SessionId != sessionID.String() {
		t.Fatalf("Session settings response = %+v", sessionSettings.GetSession())
	}

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach-project-chat-settings", &connectionpb.AttachProjectRequest{ProjectId: appCore.ProjectID()})
	mismatched := &chatsettingspb.ReadResult{}
	callGatewayDescriptor(
		t,
		conn,
		"chat-settings-project-mismatch",
		chatsettingspb.File_kent_api_chat_settings_chat_settings_proto.Services().ByName("ChatSettingsService").Methods().ByName("Read"),
		&chatsettingspb.ReadRequest{
			Target: &chatsettingspb.ReadRequest_NewChat{NewChat: &chatsettingspb.NewChatTarget{
				ProjectId: "project-foreign", WorkspaceId: workspace.ID,
			}},
		},
		mismatched,
	)
	if mismatched.GetError().GetWorkspaceNotRegistered() == nil {
		t.Fatalf("New Chat project mismatch unexpectedly succeeded")
	}
}

func assertForeignGoalAccessRejected(t *testing.T, conn *websocket.Conn, sessionID string) {
	t.Helper()
	for _, tc := range []struct {
		method protoreflect.Name
		params proto.Message
		result proto.Message
	}{
		{method: "Show", params: &runtimepb.GoalShowRequest{SessionId: sessionID}, result: &runtimepb.GoalShowResult{}},
		{
			method: "Set",
			params: &runtimepb.GoalSetRequest{
				Target: &chatpb.ChatTarget{
					Target: &chatpb.ChatTarget_Session{
						Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
					},
				},
				Objective:       "ship",
				Actor:           "user",
				ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
			},
			result: &runtimepb.GoalSetResult{},
		},
		{method: "Pause", params: &runtimepb.GoalMutationRequest{SessionId: sessionID, Actor: "user"}, result: &runtimepb.GoalPauseResult{}},
		{method: "Resume", params: &runtimepb.GoalMutationRequest{SessionId: sessionID, Actor: "user"}, result: &runtimepb.GoalResumeResult{}},
		{method: "Complete", params: &runtimepb.GoalMutationRequest{SessionId: sessionID, Actor: "user"}, result: &runtimepb.GoalCompleteResult{}},
		{method: "Clear", params: &runtimepb.GoalClearRequest{SessionId: sessionID, Actor: "user"}, result: &runtimepb.GoalClearResult{}},
	} {
		method := runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService").Methods().ByName(tc.method)
		callGatewayDescriptor(t, conn, "foreign-goal-"+string(tc.method), method, tc.params, tc.result)
		classification, err := protoapi.ClassifyResult(tc.result)
		if err != nil || classification.Outcome == protoapi.OperationSuccess {
			t.Fatalf("foreign Goal %s accepted: %v (%v)", tc.method, tc.result, err)
		}
	}
}

func TestGatewayAllowsUnscopedSessionRetargetOutsideServerDefaultProject(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	configureGatewayTestServerPort(t)

	resolvedA := resolveGatewayTestConfig(t, workspaceA)
	bindingA := registerGatewayTestBinding(t, resolvedA.Config)
	resolvedB := resolveGatewayTestConfig(t, workspaceB)
	bindingB := registerGatewayTestBinding(t, resolvedB.Config)
	metadataStore, err := metadata.Open(resolvedA.Config.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	foreignSession, err := session.Create(
		filepath.Join(filepath.Join(resolvedB.Config.PersistenceRoot, "projects"), bindingB.ProjectID, "sessions"),
		"workspace-b",
		resolvedB.Config.WorkspaceRoot, sessioncontract.SessionCategoryMain, metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create foreign: %v", err)
	}
	if err := foreignSession.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable foreign: %v", err)
	}

	authSupport := newGatewayTestAuthSupport(t, true)
	background, err := serverbootstrap.BuildShellManager(resolvedA.Config)
	if err != nil {
		t.Fatalf("BuildShellManager: %v", err)
	}
	defer func() { _ = background.Close() }()
	appCore, err := core.New(resolvedA.Config, authSupport, background)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	gateway, err := NewGateway(appCore, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	if err := gateway.requireSessionInAttachedProject(context.Background(), &connectionState{}, foreignSession.Meta().SessionID); err != nil {
		t.Fatalf("requireSessionInAttachedProject unscoped: %v", err)
	}
	if err := gateway.requireSessionInAttachedProject(context.Background(), &connectionState{attachedProject: bindingA.ProjectID}, foreignSession.Meta().SessionID); err == nil {
		t.Fatal("expected attached project scope to reject foreign session retarget")
	}
}

func TestGatewayAllowsOptionalSessionLifecycleRequestsWithoutSessionID(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	registerGatewayWorkspace(t, workspace)

	resolved := resolveGatewayTestConfig(t, workspace)
	binding, err := metadata.ResolveBinding(context.Background(), resolved.Config.PersistenceRoot, resolved.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("ResolveBinding: %v", err)
	}
	_, server := newGatewayTestServerForConfig(t, resolved.Config)

	remote, err := remoteclient.DialRemoteURLForProject(context.Background(), "ws"+server.URL[len("http"):], binding.ProjectID)
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	defer func() { _ = remote.Close() }()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach-initial-input", &connectionpb.AttachProjectRequest{ProjectId: binding.ProjectID})
	initialInput := &sessionlaunchpb.SessionInitialInputResult{}
	callGatewayDescriptor(t, conn, "initial-input",
		sessionlaunchpb.File_kent_api_session_launch_session_lifecycle_proto.Services().ByName("SessionLifecycleService").Methods().ByName("GetInitialInput"),
		&sessionlaunchpb.SessionInitialInputRequest{TransitionInput: "draft text"}, initialInput)
	if initialInput.GetSuccess().GetInput() != "draft text" {
		t.Fatalf("initial input = %v, want draft text", initialInput)
	}

	resolvedTransition, err := remote.ResolveTransition(context.Background(), &sessionlaunchpb.SessionResolveTransitionRequest{
		Transition: &sessionlaunchpb.SessionTransition{
			Action:        sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_NEW_SESSION,
			InitialPrompt: "hello",
		},
	})
	if err != nil {
		t.Fatalf("ResolveTransition: %v", err)
	}
	intent, err := protoapi.SessionLaunchIntentFromProto(resolvedTransition.GetLaunch().GetIntent())
	if err != nil || intent.Kind() != serverapi.SessionLaunchIntentCreateNew {
		t.Fatalf("unexpected transition response: %+v", resolvedTransition)
	}
	preparation := resolvedTransition.GetLaunch().GetPreparation()
	if preparation == nil {
		t.Fatal("transition response omitted launch preparation")
	}
	prompt := preparation.InitialPrompt
	if prompt == nil || prompt.Text != "hello" {
		t.Fatalf("initial prompt = %+v, want hello", prompt)
	}
}

func TestGatewayComposerDraftRoundTripKeepsServerAvailable(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	store := createGatewayAuthoritativeSession(t, appCore)

	remote, err := remoteclient.DialRemoteURLForProject(
		context.Background(),
		"ws"+server.URL[len("http"):],
		appCore.ProjectID(),
	)
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	defer func() { _ = remote.Close() }()

	if _, err := remote.PersistInputDraft(context.Background(), &sessionlaunchpb.SessionPersistInputDraftRequest{
		SessionId: store.Meta().SessionID,
		Input:     "visible draft",
	}); err != nil {
		t.Fatalf("PersistInputDraft: %v", err)
	}
	initialInput, err := remote.GetInitialInput(context.Background(), &sessionlaunchpb.SessionInitialInputRequest{
		SessionId: proto.String(store.Meta().SessionID),
	})
	if err != nil {
		t.Fatalf("GetInitialInput: %v", err)
	}
	if initialInput.Input != "visible draft" {
		t.Fatalf("initial input = %+v, want visible draft", initialInput)
	}
	projects, err := remote.ListProjects(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("follow-up ListProjects: %v", err)
	}
	if len(projects.Projects) == 0 {
		t.Fatal("follow-up ListProjects returned no projects")
	}
}

func TestGatewayProjectReattachClearsStaleSessionAttachment(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	configureGatewayTestServerPort(t)

	resolvedA := resolveGatewayTestConfig(t, workspaceA)
	bindingA := registerGatewayTestBinding(t, resolvedA.Config)
	resolvedB := resolveGatewayTestConfig(t, workspaceB)
	bindingB := registerGatewayTestBinding(t, resolvedB.Config)

	appCore, server := newGatewayTestServerForConfig(t, resolvedA.Config)
	storeA := createGatewayAuthoritativeSession(t, appCore)

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach-project-a", &connectionpb.AttachProjectRequest{ProjectId: bindingA.ProjectID})
	requireGatewaySessionAttachment(t, conn, "attach-session-a", storeA.Meta().SessionID)
	requireGatewayProjectAttachment(t, conn, "attach-project-b", &connectionpb.AttachProjectRequest{ProjectId: bindingB.ProjectID})

	var result transcriptpb.SubscribeResult
	callGatewayDescriptor(t, conn, "subscribe", transcriptpb.File_kent_api_transcript_transcript_proto.Services().ByName("StreamService").Methods().ByName("Subscribe"),
		&transcriptpb.SubscribeRequest{SessionId: storeA.Meta().SessionID}, &result)
	if result.GetError() == nil {
		t.Fatalf("expected session-attach-required error after project reattach, got %+v", &result)
	}
}

func TestGatewayPreservesAttachProjectWorkspaceNotRegistered(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	configureGatewayTestServerPort(t)

	resolvedA := resolveGatewayTestConfig(t, workspaceA)
	bindingA := registerGatewayTestBinding(t, resolvedA.Config)
	resolvedB := resolveGatewayTestConfig(t, workspaceB)

	_, server := newGatewayTestServerForConfig(t, resolvedA.Config)

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	result := attachGatewayProject(t, conn, "attach-project", &connectionpb.AttachProjectRequest{
		ProjectId: bindingA.ProjectID,
		Workspace: &connectionpb.AttachProjectRequest_WorkspaceRoot{WorkspaceRoot: resolvedB.Config.WorkspaceRoot},
	})
	failure := result.GetError()
	if failure == nil || failure.Code != "workspace_not_registered" || failure.GetWorkspaceNotRegistered() == nil {
		t.Fatalf("workspace-not-registered attachment result = %+v", result)
	}
}
