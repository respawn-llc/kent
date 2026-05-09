package transport

import (
	"builder/server/auth"
	serverbootstrap "builder/server/bootstrap"
	"builder/server/core"
	"builder/server/metadata"
	"builder/server/session"
	shelltool "builder/server/tools/shell"
	remoteclient "builder/shared/client"
	"builder/shared/config"
	"builder/shared/protocol"
	"builder/shared/rpccontract"
	"builder/shared/rpcwire"
	"builder/shared/serverapi"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/websocket"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func registerGatewayWorkspace(t *testing.T, workspace string) {
	t.Helper()
	configureGatewayTestServerPort(t)
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	if _, err := metadata.RegisterBinding(context.Background(), resolved.Config.PersistenceRoot, resolved.Config.WorkspaceRoot); err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
}

func configureGatewayTestServerPort(t *testing.T) {
	t.Helper()
	port := 56000 + int(gatewayTestPortCounter.Add(1))
	t.Setenv("BUILDER_SERVER_HOST", "127.0.0.1")
	t.Setenv("BUILDER_SERVER_PORT", strconv.Itoa(port))
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

func TestProtocolErrorMapsContextCanceled(t *testing.T) {
	code, message := protocolError(context.Canceled)
	if code != protocol.ErrCodeRequestCanceled {
		t.Fatalf("protocol error code = %d, want %d", code, protocol.ErrCodeRequestCanceled)
	}
	if message != "request canceled by client" {
		t.Fatalf("protocol error message = %q, want request canceled by client", message)
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
			req := event.Frame.Request()
			switch req.Method {
			case protocol.MethodHandshake:
				resp := protocol.NewSuccessResponse(req.ID, protocol.HandshakeResponse{Identity: protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"}})
				if err := conn.Send(ctx, rpcwire.FrameFromResponse(resp)); err != nil {
					reportGatewayHandlerError(handlerErrs, "send handshake: %w", err)
					return
				}
			case protocol.MethodProjectList:
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

	_, err = remote.ListProjects(context.Background(), serverapi.ProjectListRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListProjects error = %v, want context.Canceled", err)
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
		if _, err := authSupport.AuthManager.SwitchMethod(context.Background(), auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "test-key"},
		}, true); err != nil {
			t.Fatalf("SwitchMethod: %v", err)
		}
	}
	return authSupport
}

func activateGatewayController(t *testing.T, appCore *core.Core, sessionID string) string {
	t.Helper()
	settings := appCore.Config().Settings
	if strings.TrimSpace(settings.Model) == "" {
		settings.Model = "gpt-5"
	}
	if strings.TrimSpace(settings.ProviderOverride) == "" && strings.TrimSpace(settings.OpenAIBaseURL) == "" {
		settings.ProviderOverride = "openai"
	}
	resp, err := appCore.SessionRuntimeClient().ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "activate-" + strings.TrimSpace(sessionID),
		SessionID:       strings.TrimSpace(sessionID),
		ActiveSettings:  settings,
		Source:          appCore.Config().Source,
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime: %v", err)
	}
	return resp.LeaseID
}

func releaseGatewayController(t *testing.T, appCore *core.Core, sessionID string, leaseID string) {
	t.Helper()
	if strings.TrimSpace(leaseID) == "" {
		return
	}
	if _, err := appCore.SessionRuntimeClient().ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "release-" + strings.TrimSpace(sessionID),
		SessionID:       strings.TrimSpace(sessionID),
		LeaseID:         strings.TrimSpace(leaseID),
	}); err != nil {
		t.Fatalf("ReleaseSessionRuntime: %v", err)
	}
}

func TestGatewayHandshakeAndProjectList(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	registerGatewayWorkspace(t, workspace)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	authSupport := newGatewayTestAuthSupport(t, true)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolved.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	t.Cleanup(func() { _ = runtimeSupport.Background.Close() })
	appCore, err := core.New(resolved.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, err := websocket.Dial(wsURL, "", server.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := websocket.JSON.Send(conn, protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: "1", Method: protocol.MethodHandshake, Params: mustJSON(t, protocol.HandshakeRequest{ProtocolVersion: protocol.Version})}); err != nil {
		t.Fatalf("send handshake: %v", err)
	}
	var handshakeResp protocol.Response
	if err := websocket.JSON.Receive(conn, &handshakeResp); err != nil {
		t.Fatalf("receive handshake: %v", err)
	}
	if handshakeResp.Error != nil {
		t.Fatalf("handshake error: %+v", handshakeResp.Error)
	}
	var handshake protocol.HandshakeResponse
	if err := json.Unmarshal(handshakeResp.Result, &handshake); err != nil {
		t.Fatalf("decode handshake result: %v", err)
	}
	if handshake.Identity.ProtocolVersion != protocol.Version || handshake.Identity.ServerID != "server-1" {
		t.Fatalf("unexpected handshake: %+v", handshake.Identity)
	}

	if err := websocket.JSON.Send(conn, protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: "2", Method: protocol.MethodProjectList, Params: mustJSON(t, serverapi.ProjectListRequest{})}); err != nil {
		t.Fatalf("send project list: %v", err)
	}
	var projectListResp protocol.Response
	if err := websocket.JSON.Receive(conn, &projectListResp); err != nil {
		t.Fatalf("receive project list: %v", err)
	}
	if projectListResp.Error != nil {
		t.Fatalf("project list error: %+v", projectListResp.Error)
	}
	var projects serverapi.ProjectListResponse
	if err := json.Unmarshal(projectListResp.Result, &projects); err != nil {
		t.Fatalf("decode project list: %v", err)
	}
	if len(projects.Projects) != 1 || projects.Projects[0].ProjectID != appCore.ProjectID() {
		t.Fatalf("unexpected project list: %+v", projects.Projects)
	}
}

func TestGatewayRejectsMethodsBeforeHandshake(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	registerGatewayWorkspace(t, workspace)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	authSupport := newGatewayTestAuthSupport(t, true)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolved.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	t.Cleanup(func() { _ = runtimeSupport.Background.Close() })
	appCore, err := core.New(resolved.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, err := websocket.Dial(wsURL, "", server.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := websocket.JSON.Send(conn, protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: "1", Method: protocol.MethodProjectList, Params: mustJSON(t, serverapi.ProjectListRequest{})}); err != nil {
		t.Fatalf("send project list: %v", err)
	}
	var resp protocol.Response
	if err := websocket.JSON.Receive(conn, &resp); err != nil {
		t.Fatalf("receive response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrCodeInvalidRequest {
		t.Fatalf("expected handshake-required error, got %+v", resp.Error)
	}
}

func TestGatewayPreAuthMethodPolicy(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		requiresAuth bool
	}{
		{name: "handshake", method: protocol.MethodHandshake, requiresAuth: false},
		{name: "bootstrap status", method: protocol.MethodAuthGetBootstrapStatus, requiresAuth: false},
		{name: "bootstrap complete", method: protocol.MethodAuthCompleteBootstrap, requiresAuth: false},
		{name: "project list", method: protocol.MethodProjectList, requiresAuth: false},
		{name: "project binding plan", method: protocol.MethodProjectPlanWorkspaceBinding, requiresAuth: false},
		{name: "project attach workspace", method: protocol.MethodProjectAttachWorkspace, requiresAuth: true},
		{name: "attach project", method: protocol.MethodAttachProject, requiresAuth: false},
		{name: "attach session", method: protocol.MethodAttachSession, requiresAuth: false},
		{name: "session transcript page", method: protocol.MethodSessionGetTranscriptPage, requiresAuth: false},
		{name: "process list", method: protocol.MethodProcessList, requiresAuth: false},
		{name: "run get", method: protocol.MethodRunGet, requiresAuth: false},
		{name: "session plan", method: protocol.MethodSessionPlan, requiresAuth: true},
		{name: "persist input draft", method: protocol.MethodSessionPersistInputDraft, requiresAuth: true},
		{name: "runtime submit", method: protocol.MethodRuntimeSubmitUserMessage, requiresAuth: true},
		{name: "run prompt", method: protocol.MethodRunPrompt, requiresAuth: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Gateway{}
			got := g.methodRequiresServerAuth(tt.method)
			if got != tt.requiresAuth {
				t.Fatalf("methodRequiresServerAuth(%q) = %t, want %t", tt.method, got, tt.requiresAuth)
			}
		})
	}
}

func TestGatewaySubscriptionHandlersCoverRouteContract(t *testing.T) {
	for _, route := range rpccontract.Routes() {
		if route.Kind != rpccontract.KindSubscription {
			continue
		}
		if _, ok := gatewaySubscriptionHandlers[route.Method]; !ok {
			t.Fatalf("subscription route %q missing gateway handler", route.Method)
		}
		if !isSubscriptionMethod(route.Method) {
			t.Fatalf("subscription route %q not classified as subscription", route.Method)
		}
	}
	for method := range gatewaySubscriptionHandlers {
		route, ok := rpccontract.RouteByMethod(method)
		if !ok {
			t.Fatalf("gateway subscription handler %q missing route contract", method)
		}
		if route.Kind != rpccontract.KindSubscription {
			t.Fatalf("gateway subscription handler %q route kind = %q, want subscription", method, route.Kind)
		}
	}
}

func TestGatewayProgressHandlersCoverRouteContract(t *testing.T) {
	for _, route := range rpccontract.Routes() {
		if route.Kind != rpccontract.KindProgress {
			continue
		}
		if _, ok := gatewayProgressHandlers[route.Method]; !ok {
			t.Fatalf("progress route %q missing gateway handler", route.Method)
		}
	}
	for method := range gatewayProgressHandlers {
		route, ok := rpccontract.RouteByMethod(method)
		if !ok {
			t.Fatalf("gateway progress handler %q missing route contract", method)
		}
		if route.Kind != rpccontract.KindProgress {
			t.Fatalf("gateway progress handler %q route kind = %q, want progress", method, route.Kind)
		}
	}
}

func TestGatewayAuthBootstrapStatusAllowedBeforeAttach(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	registerGatewayWorkspace(t, workspace)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	authSupport := newGatewayTestAuthSupport(t, false)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolved.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	t.Cleanup(func() { _ = runtimeSupport.Background.Close() })
	appCore, err := core.New(resolved.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)

	var status serverapi.AuthGetBootstrapStatusResponse
	callGateway(t, conn, "status-1", protocol.MethodAuthGetBootstrapStatus, serverapi.AuthGetBootstrapStatusRequest{}, &status)
	if status.AuthReady {
		t.Fatal("expected unauthenticated bootstrap status")
	}
	if !status.AuthBootstrapSupported {
		t.Fatal("expected auth bootstrap to be supported")
	}
	if !containsString(status.AllowedPreAuthMethods, protocol.MethodProjectList) {
		t.Fatalf("allowed pre-auth methods = %+v, want %q", status.AllowedPreAuthMethods, protocol.MethodProjectList)
	}
	if !containsString(status.AllowedPreAuthMethods, protocol.MethodAuthCompleteBootstrap) {
		t.Fatalf("allowed pre-auth methods = %+v, want %q", status.AllowedPreAuthMethods, protocol.MethodAuthCompleteBootstrap)
	}
	if !sameStringSet(status.AllowedPreAuthMethods, rpccontract.AllowedPreAuthMethods()) {
		t.Fatalf("allowed pre-auth methods = %+v, want %+v", status.AllowedPreAuthMethods, rpccontract.AllowedPreAuthMethods())
	}
}

func TestGatewayAuthBootstrapAPIKeyCompletionEnablesAuthRequiredMethods(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	registerGatewayWorkspace(t, workspace)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	authSupport := newGatewayTestAuthSupport(t, false)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolved.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	t.Cleanup(func() { _ = runtimeSupport.Background.Close() })
	appCore, err := core.New(resolved.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)

	callGateway(t, conn, "attach-project", protocol.MethodAttachProject, protocol.AttachProjectRequest{ProjectID: appCore.ProjectID()}, nil)
	if err := websocket.JSON.Send(conn, protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: "run-1", Method: protocol.MethodRunPrompt, Params: mustJSON(t, serverapi.RunPromptRequest{})}); err != nil {
		t.Fatalf("send run.prompt: %v", err)
	}
	var runResp protocol.Response
	if err := websocket.JSON.Receive(conn, &runResp); err != nil {
		t.Fatalf("receive run.prompt: %v", err)
	}
	if runResp.Error == nil || runResp.Error.Code != protocol.ErrCodeAuthRequired {
		t.Fatalf("run.prompt error = %+v, want auth required", runResp.Error)
	}

	callGateway(t, conn, "complete-1", protocol.MethodAuthCompleteBootstrap, serverapi.AuthCompleteBootstrapRequest{
		Mode:   serverapi.AuthBootstrapModeAPIKey,
		APIKey: "server-key",
	}, nil)
	var status serverapi.AuthGetBootstrapStatusResponse
	callGateway(t, conn, "status-2", protocol.MethodAuthGetBootstrapStatus, serverapi.AuthGetBootstrapStatusRequest{}, &status)
	if !status.AuthReady {
		t.Fatal("expected bootstrap completion to configure server auth")
	}
	state, err := authSupport.AuthManager.StoredState(context.Background())
	if err != nil {
		t.Fatalf("StoredState: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "server-key" {
		t.Fatalf("unexpected stored auth method: %+v", state.Method)
	}

	if err := websocket.JSON.Send(conn, protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: "complete-2", Method: protocol.MethodAuthCompleteBootstrap, Params: mustJSON(t, serverapi.AuthCompleteBootstrapRequest{Mode: serverapi.AuthBootstrapModeAPIKey, APIKey: "server-key-2"})}); err != nil {
		t.Fatalf("send second auth.completeBootstrap: %v", err)
	}
	var secondCompleteResp protocol.Response
	if err := websocket.JSON.Receive(conn, &secondCompleteResp); err != nil {
		t.Fatalf("receive second auth.completeBootstrap: %v", err)
	}
	if secondCompleteResp.Error != nil {
		t.Fatalf("second auth.completeBootstrap error = %+v, want success", secondCompleteResp.Error)
	}
	var secondComplete serverapi.AuthCompleteBootstrapResponse
	if err := json.Unmarshal(secondCompleteResp.Result, &secondComplete); err != nil {
		t.Fatalf("decode second auth.completeBootstrap result: %v", err)
	}
	if !secondComplete.AuthReady || secondComplete.MethodType != string(auth.MethodAPIKey) {
		t.Fatalf("unexpected second auth.completeBootstrap result: %+v", secondComplete)
	}
	state, err = authSupport.AuthManager.StoredState(context.Background())
	if err != nil {
		t.Fatalf("StoredState after second complete: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "server-key" {
		t.Fatalf("unexpected stored auth method after retry: %+v", state.Method)
	}
}

func TestGatewayRejectsProjectWorkspaceMutationBeforeServerAuthReady(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	registerGatewayWorkspace(t, workspace)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	authSupport := newGatewayTestAuthSupport(t, false)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolved.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	t.Cleanup(func() { _ = runtimeSupport.Background.Close() })
	appCore, err := core.New(resolved.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	callGateway(t, conn, "attach-project", protocol.MethodAttachProject, protocol.AttachProjectRequest{ProjectID: appCore.ProjectID()}, nil)

	if err := websocket.JSON.Send(conn, protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: "attach-workspace", Method: protocol.MethodProjectAttachWorkspace, Params: mustJSON(t, serverapi.ProjectAttachWorkspaceRequest{ProjectID: appCore.ProjectID(), WorkspaceRoot: "/tmp/workspace"})}); err != nil {
		t.Fatalf("send project.attachWorkspace: %v", err)
	}
	var resp protocol.Response
	if err := websocket.JSON.Receive(conn, &resp); err != nil {
		t.Fatalf("receive project.attachWorkspace: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrCodeAuthRequired {
		t.Fatalf("project.attachWorkspace error = %+v, want auth required", resp.Error)
	}
}

func TestGatewayRejectsSessionActivitySubscriptionBeforeServerAuthReady(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	registerGatewayWorkspace(t, workspace)

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	authSupport := newGatewayTestAuthSupport(t, false)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolved.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	t.Cleanup(func() { _ = runtimeSupport.Background.Close() })
	appCore, err := core.New(resolved.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	store := createGatewayAuthoritativeSession(t, appCore)
	appCore.RegisterSessionStore(store)
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)

	callGateway(t, conn, "attach-project", protocol.MethodAttachProject, protocol.AttachProjectRequest{ProjectID: appCore.ProjectID()}, nil)
	callGateway(t, conn, "attach-session", protocol.MethodAttachSession, protocol.AttachSessionRequest{SessionID: store.Meta().SessionID}, nil)
	if err := websocket.JSON.Send(conn, protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: "subscribe", Method: protocol.MethodSessionSubscribeActivity, Params: mustJSON(t, serverapi.SessionActivitySubscribeRequest{SessionID: store.Meta().SessionID})}); err != nil {
		t.Fatalf("send session activity subscribe: %v", err)
	}
	var resp protocol.Response
	if err := websocket.JSON.Receive(conn, &resp); err != nil {
		t.Fatalf("receive session activity subscribe: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrCodeAuthRequired {
		t.Fatalf("session activity subscribe error = %+v, want auth required", resp.Error)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func sameStringSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, item := range left {
		counts[item]++
	}
	for _, item := range right {
		counts[item]--
		if counts[item] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func TestGatewayRejectsSessionAccessOutsideAttachedProject(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	configureGatewayTestServerPort(t)

	resolvedA, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceA})
	if err != nil {
		t.Fatalf("ResolveConfig A: %v", err)
	}
	bindingA, err := metadata.RegisterBinding(context.Background(), resolvedA.Config.PersistenceRoot, resolvedA.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding A: %v", err)
	}
	resolvedB, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceB})
	if err != nil {
		t.Fatalf("ResolveConfig B: %v", err)
	}
	bindingB, err := metadata.RegisterBinding(context.Background(), resolvedB.Config.PersistenceRoot, resolvedB.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding B: %v", err)
	}
	metadataStore, err := metadata.Open(resolvedA.Config.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	foreignSession, err := session.Create(
		config.ProjectSessionsRoot(resolvedB.Config, bindingB.ProjectID),
		"workspace-b",
		resolvedB.Config.WorkspaceRoot,
		metadataStore.SessionStoreOptions()...,
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

	authSupport := newGatewayTestAuthSupport(t, true)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolvedA.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	defer func() { _ = runtimeSupport.Background.Close() }()
	appCore, err := core.New(resolvedA.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	remote, err := remoteclient.DialRemoteURLForProject(context.Background(), "ws"+server.URL[len("http"):], bindingA.ProjectID)
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	defer func() { _ = remote.Close() }()

	if _, err := remote.GetSessionMainView(context.Background(), serverapi.SessionMainViewRequest{SessionID: foreignSession.Meta().SessionID}); err == nil {
		t.Fatal("expected foreign-project session view access to be rejected")
	}
	if _, err := remote.PersistInputDraft(context.Background(), serverapi.SessionPersistInputDraftRequest{ClientRequestID: "persist-foreign", SessionID: foreignSession.Meta().SessionID, ControllerLeaseID: "lease-foreign", Input: "should fail"}); err == nil {
		t.Fatal("expected foreign-project session mutation to be rejected")
	}
	if _, err := remote.RetargetSessionWorkspace(context.Background(), serverapi.SessionRetargetWorkspaceRequest{ClientRequestID: "retarget-foreign", SessionID: foreignSession.Meta().SessionID, WorkspaceRoot: resolvedA.Config.WorkspaceRoot}); err == nil {
		t.Fatal("expected foreign-project session retarget to be rejected")
	}
	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	callGateway(t, conn, "attach-project-for-foreign-goal-checks", protocol.MethodAttachProject, protocol.AttachProjectRequest{ProjectID: bindingA.ProjectID}, nil)
	assertForeignGoalAccessRejected(t, conn, foreignSession.Meta().SessionID)
	if bindingA.ProjectID == bindingB.ProjectID {
		t.Fatalf("expected distinct project ids, both=%q", bindingA.ProjectID)
	}
}

func assertForeignGoalAccessRejected(t *testing.T, conn *websocket.Conn, sessionID string) {
	t.Helper()
	wantMessage := "session " + strconv.Quote(sessionID) + " not available"
	for _, tc := range []struct {
		name   string
		method string
		params any
	}{
		{name: "show", method: protocol.MethodRuntimeGoalShow, params: serverapi.RuntimeGoalShowRequest{SessionID: sessionID}},
		{name: "set", method: protocol.MethodRuntimeGoalSet, params: serverapi.RuntimeGoalSetRequest{ClientRequestID: "foreign-goal-set", SessionID: sessionID, Objective: "ship", Actor: "user"}},
		{name: "pause", method: protocol.MethodRuntimeGoalPause, params: serverapi.RuntimeGoalStatusRequest{ClientRequestID: "foreign-goal-pause", SessionID: sessionID, Actor: "user"}},
		{name: "resume", method: protocol.MethodRuntimeGoalResume, params: serverapi.RuntimeGoalStatusRequest{ClientRequestID: "foreign-goal-resume", SessionID: sessionID, Actor: "user"}},
		{name: "complete", method: protocol.MethodRuntimeGoalComplete, params: serverapi.RuntimeGoalStatusRequest{ClientRequestID: "foreign-goal-complete", SessionID: sessionID, Actor: "agent"}},
		{name: "clear", method: protocol.MethodRuntimeGoalClear, params: serverapi.RuntimeGoalClearRequest{ClientRequestID: "foreign-goal-clear", SessionID: sessionID, Actor: "user"}},
	} {
		err := callGatewayExpectError(t, conn, "foreign-goal-"+tc.name, tc.method, tc.params)
		if err.Code != protocol.ErrCodeInternalError || err.Message != wantMessage {
			t.Fatalf("foreign goal %s error = code %d message %q, want code %d message %q", tc.name, err.Code, err.Message, protocol.ErrCodeInternalError, wantMessage)
		}
	}
}

func TestGatewayAllowsUnscopedSessionRetargetOutsideServerDefaultProject(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	configureGatewayTestServerPort(t)

	resolvedA, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceA})
	if err != nil {
		t.Fatalf("ResolveConfig A: %v", err)
	}
	bindingA, err := metadata.RegisterBinding(context.Background(), resolvedA.Config.PersistenceRoot, resolvedA.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding A: %v", err)
	}
	resolvedB, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceB})
	if err != nil {
		t.Fatalf("ResolveConfig B: %v", err)
	}
	bindingB, err := metadata.RegisterBinding(context.Background(), resolvedB.Config.PersistenceRoot, resolvedB.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding B: %v", err)
	}
	metadataStore, err := metadata.Open(resolvedA.Config.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = metadataStore.Close() }()
	foreignSession, err := session.Create(
		config.ProjectSessionsRoot(resolvedB.Config, bindingB.ProjectID),
		"workspace-b",
		resolvedB.Config.WorkspaceRoot,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create foreign: %v", err)
	}
	if err := foreignSession.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable foreign: %v", err)
	}

	authSupport := newGatewayTestAuthSupport(t, true)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolvedA.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	defer func() { _ = runtimeSupport.Background.Close() }()
	appCore, err := core.New(resolvedA.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
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

	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	binding, err := metadata.ResolveBinding(context.Background(), resolved.Config.PersistenceRoot, resolved.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("ResolveBinding: %v", err)
	}
	authSupport := newGatewayTestAuthSupport(t, true)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolved.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	defer func() { _ = runtimeSupport.Background.Close() }()
	appCore, err := core.New(resolved.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	remote, err := remoteclient.DialRemoteURLForProject(context.Background(), "ws"+server.URL[len("http"):], binding.ProjectID)
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	defer func() { _ = remote.Close() }()

	initialInput, err := remote.GetInitialInput(context.Background(), serverapi.SessionInitialInputRequest{TransitionInput: "draft text"})
	if err != nil {
		t.Fatalf("GetInitialInput: %v", err)
	}
	if initialInput.Input != "draft text" {
		t.Fatalf("initial input = %q, want draft text", initialInput.Input)
	}

	resolvedTransition, err := remote.ResolveTransition(context.Background(), serverapi.SessionResolveTransitionRequest{
		ClientRequestID: "new-session-no-current-session",
		Transition: serverapi.SessionTransition{
			Action:        "new_session",
			InitialPrompt: "hello",
		},
	})
	if err != nil {
		t.Fatalf("ResolveTransition: %v", err)
	}
	if !resolvedTransition.ShouldContinue || !resolvedTransition.ForceNewSession {
		t.Fatalf("unexpected transition response: %+v", resolvedTransition)
	}
	if resolvedTransition.InitialPrompt != "hello" {
		t.Fatalf("initial prompt = %q, want hello", resolvedTransition.InitialPrompt)
	}
}

func TestGatewayProjectReattachClearsStaleSessionAttachment(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	configureGatewayTestServerPort(t)

	resolvedA, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceA})
	if err != nil {
		t.Fatalf("ResolveConfig A: %v", err)
	}
	bindingA, err := metadata.RegisterBinding(context.Background(), resolvedA.Config.PersistenceRoot, resolvedA.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding A: %v", err)
	}
	resolvedB, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceB})
	if err != nil {
		t.Fatalf("ResolveConfig B: %v", err)
	}
	bindingB, err := metadata.RegisterBinding(context.Background(), resolvedB.Config.PersistenceRoot, resolvedB.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding B: %v", err)
	}

	authSupport := newGatewayTestAuthSupport(t, true)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolvedA.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	defer func() { _ = runtimeSupport.Background.Close() }()
	appCore, err := core.New(resolvedA.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	storeA := createGatewayAuthoritativeSession(t, appCore)
	appCore.RegisterSessionStore(storeA)
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	callGateway(t, conn, "attach-project-a", protocol.MethodAttachProject, protocol.AttachProjectRequest{ProjectID: bindingA.ProjectID}, nil)
	callGateway(t, conn, "attach-session-a", protocol.MethodAttachSession, protocol.AttachSessionRequest{SessionID: storeA.Meta().SessionID}, nil)
	callGateway(t, conn, "attach-project-b", protocol.MethodAttachProject, protocol.AttachProjectRequest{ProjectID: bindingB.ProjectID}, nil)

	if err := websocket.JSON.Send(conn, protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: "subscribe", Method: protocol.MethodSessionSubscribeActivity, Params: mustJSON(t, serverapi.SessionActivitySubscribeRequest{SessionID: storeA.Meta().SessionID})}); err != nil {
		t.Fatalf("send subscribe: %v", err)
	}
	var resp protocol.Response
	if err := websocket.JSON.Receive(conn, &resp); err != nil {
		t.Fatalf("receive subscribe response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrCodeInvalidRequest {
		t.Fatalf("expected session-attach-required error after project reattach, got %+v", resp.Error)
	}
}

func TestGatewayRejectsAttachProjectWorkspaceOutsideProject(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	configureGatewayTestServerPort(t)

	resolvedA, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceA})
	if err != nil {
		t.Fatalf("ResolveConfig A: %v", err)
	}
	bindingA, err := metadata.RegisterBinding(context.Background(), resolvedA.Config.PersistenceRoot, resolvedA.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding A: %v", err)
	}
	resolvedB, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceB})
	if err != nil {
		t.Fatalf("ResolveConfig B: %v", err)
	}
	if _, err := metadata.RegisterBinding(context.Background(), resolvedB.Config.PersistenceRoot, resolvedB.Config.WorkspaceRoot); err != nil {
		t.Fatalf("RegisterBinding B: %v", err)
	}

	authSupport := newGatewayTestAuthSupport(t, true)
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(resolvedA.Config)
	if err != nil {
		t.Fatalf("BuildRuntimeSupport: %v", err)
	}
	defer func() { _ = runtimeSupport.Background.Close() }()
	appCore, err := core.New(resolvedA.Config, authSupport, runtimeSupport)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	defer func() { _ = appCore.Close() }()
	gateway, err := NewGateway(appCore, protocol.ServerIdentity{ProtocolVersion: protocol.Version, ServerID: "server-1"})
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	if err := websocket.JSON.Send(conn, protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: "attach-project", Method: protocol.MethodAttachProject, Params: mustJSON(t, protocol.AttachProjectRequest{ProjectID: bindingA.ProjectID, WorkspaceRoot: resolvedB.Config.WorkspaceRoot})}); err != nil {
		t.Fatalf("send attach-project: %v", err)
	}
	var resp protocol.Response
	if err := websocket.JSON.Receive(conn, &resp); err != nil {
		t.Fatalf("receive attach-project: %v", err)
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "not bound to project") {
		t.Fatalf("expected workspace/project mismatch error, got %+v", resp.Error)
	}
}
