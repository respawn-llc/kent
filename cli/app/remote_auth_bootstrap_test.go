package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/cli/app/internal/authui"
	sharedauth "core/shared/auth"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/emptypb"
)

type stubAuthBootstrapClient struct {
	protocol      *authpb.ConnectionProtocol
	status        *authpb.BootstrapStatus
	completeReq   *authpb.CompleteBootstrapRequest
	completeCalls int
	completeErr   error
	completeResp  *authpb.BootstrapCompletion
}

func (c *stubAuthBootstrapClient) GetBootstrapStatus(context.Context, *authpb.GetBootstrapStatusRequest) (*authpb.BootstrapStatus, error) {
	return c.status, nil
}

func (c *stubAuthBootstrapClient) StartBootstrap(_ context.Context, req *authpb.StartBootstrapRequest) (*authpb.BootstrapStart, error) {
	result := &authpb.BootstrapStart{Continuation: &authpb.BootstrapContinuation{Protocol: authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT}}
	if c.protocol != nil {
		result.Continuation.Protocol = *c.protocol
	}
	if req.Mode == authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE {
		result.Instructions = &authpb.BootstrapStart_Device{Device: &authpb.BootstrapDeviceInstructions{VerificationUrl: "https://provider.example/device", UserCode: "CODE-1"}}
		result.Continuation.Grant = &authpb.BootstrapContinuation_Device{Device: &authpb.DeviceBootstrapContinuation{Code: "device-1", UserCode: "CODE-1", PollIntervalSeconds: 1}}
	} else {
		result.Instructions = &authpb.BootstrapStart_AuthorizationUrl{AuthorizationUrl: "https://provider.example/authorize"}
		result.Continuation.Grant = &authpb.BootstrapContinuation_Browser{Browser: &authpb.BrowserBootstrapContinuation{RedirectUri: req.GetRedirectUri(), State: "state-1", CodeVerifier: "verifier-1"}}
	}
	return result, nil
}

func (*stubAuthBootstrapClient) GetConnections(context.Context, *authpb.GetConnectionsRequest) (*authpb.ConnectionCatalog, error) {
	return &authpb.ConnectionCatalog{}, nil
}

func (*stubAuthBootstrapClient) ConfigureConnection(context.Context, *authpb.ConfigureConnectionRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (c *stubAuthBootstrapClient) CompleteBootstrap(_ context.Context, req *authpb.CompleteBootstrapRequest) (*authpb.BootstrapCompletion, error) {
	c.completeCalls++
	c.completeReq = req
	if c.completeErr != nil {
		err := c.completeErr
		c.completeErr = nil
		return nil, err
	}
	if c.completeResp != nil {
		return c.completeResp, nil
	}
	return &authpb.BootstrapCompletion{AuthReady: true, Method: authpb.AuthMethod_AUTH_METHOD_OAUTH, ConnectionId: "test"}, nil
}

func TestRemoteAuthReadinessAllowsOptionalAuthForHeadlessClients(t *testing.T) {
	connection := config.ConnectionID("connection")
	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		Method:       authpb.AuthMethod_AUTH_METHOD_NONE,
		AuthRequired: false,
	}}

	if err := ensureRemoteAuthReady(t.Context(), remote, config.Settings{Connection: &connection}, newHeadlessAuthInteractor()); err != nil {
		t.Fatalf("ensureRemoteAuthReady: %v", err)
	}
	if remote.completeCalls != 0 {
		t.Fatalf("bootstrap calls = %d, want no authentication attempt", remote.completeCalls)
	}
}

func TestRemoteAuthReadinessBootstrapsAPIKeyForHeadlessClients(t *testing.T) {
	connection := config.ConnectionID("connection")
	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		Method:       authpb.AuthMethod_AUTH_METHOD_API_KEY,
		ConnectionId: string(connection),
		AuthRequired: true,
	}}

	if err := ensureRemoteAuthReady(t.Context(), remote, config.Settings{Connection: &connection}, newHeadlessAuthInteractor()); err != nil {
		t.Fatalf("ensureRemoteAuthReady: %v", err)
	}
	if remote.completeCalls != 1 {
		t.Fatalf("bootstrap calls = %d, want exactly one API-key bootstrap", remote.completeCalls)
	}
	if remote.completeReq.GetMode() != authpb.BootstrapMode_BOOTSTRAP_MODE_API_KEY ||
		remote.completeReq.GetTarget().GetConnectionId() != string(connection) {
		t.Fatalf("unexpected API-key bootstrap request: %+v", remote.completeReq)
	}
}

func TestRemoteAuthReadinessReportsUnavailableOAuthForHeadlessClients(t *testing.T) {
	connection := config.ConnectionID("connection")
	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		Method:         authpb.AuthMethod_AUTH_METHOD_OAUTH,
		ConnectionId:   "connection",
		AuthRequired:   true,
		SupportedModes: []authpb.BootstrapMode{authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE},
	}}

	err := ensureRemoteAuthReady(t.Context(), remote, config.Settings{Connection: &connection}, newHeadlessAuthInteractor())
	if !errors.Is(err, serverapi.ErrServerAuthRequired) {
		t.Fatalf("ensureRemoteAuthReady error = %v, want unavailable interaction error", err)
	}
	if remote.completeCalls != 0 {
		t.Fatalf("bootstrap calls = %d, want no headless OAuth attempt", remote.completeCalls)
	}
}

func TestRemoteAuthReadinessRequiresInteractorForUnreadyAuth(t *testing.T) {
	connection := config.ConnectionID("connection")
	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		Method:       authpb.AuthMethod_AUTH_METHOD_NONE,
		AuthRequired: false,
	}}

	err := ensureRemoteAuthReady(t.Context(), remote, config.Settings{Connection: &connection}, nil)
	if !errors.Is(err, serverapi.ErrServerAuthRequired) {
		t.Fatalf("ensureRemoteAuthReady error = %v, want interaction-required error", err)
	}
	if remote.completeCalls != 0 {
		t.Fatalf("bootstrap calls = %d, want no authentication attempt", remote.completeCalls)
	}
}

type stubOAuthCallbackListener struct {
	callback authui.OAuthBrowserCallback
	waitErr  error
	closed   int
}

func (l *stubOAuthCallbackListener) RedirectURI() string {
	return "http://127.0.0.1:0/callback"
}

func (l *stubOAuthCallbackListener) Wait(context.Context, time.Duration) (authui.OAuthBrowserCallback, error) {
	if l.waitErr != nil {
		return authui.OAuthBrowserCallback{}, l.waitErr
	}
	return l.callback, nil
}

func (l *stubOAuthCallbackListener) Close() error {
	l.closed++
	return nil
}

func TestRemoteAuthBootstrapForwardsDeviceContinuation(t *testing.T) {
	useStartupTestTerminal(t)
	for _, protocol := range []authpb.ConnectionProtocol{
		authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT,
		authpb.ConnectionProtocol_CONNECTION_PROTOCOL_GROK_CLI_PROXY,
		authpb.ConnectionProtocol_CONNECTION_PROTOCOL_GROK_OAUTH_API,
	} {
		t.Run(protocol.String(), func(t *testing.T) {
			testRemoteDeviceContinuation(t, protocol)
		})
	}
}

func testRemoteDeviceContinuation(t *testing.T, protocol authpb.ConnectionProtocol) {
	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		AuthRequired:   true,
		SupportedModes: []authpb.BootstrapMode{authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE},
	}, protocol: &protocol}
	opened := false
	interactor := &interactiveAuthInteractor{
		openBrowser: func(value string) error {
			opened = true
			if value != "https://provider.example/device" {
				t.Fatalf("device verification URL changed: %q", value)
			}
			return nil
		},
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			return authMethodPickerResult{Choice: authMethodChoiceDevice}, nil
		},
	}

	request, err := interactor.collectRemoteBootstrapRequest(t.Context(), remote, protoapi.ExistingConnectionTarget("test"), "dark", authMethodChoiceDevice, remote.status)
	if err != nil {
		t.Fatal(err)
	}
	if request.Mode != authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE ||
		request.GetContinuation().GetDevice().GetCode() != "device-1" {
		t.Fatalf("unexpected completion request: %+v", request)
	}
	if opened != (protocol != authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT) {
		t.Fatalf("automatic browser launch = %v for %v", opened, protocol)
	}
}

func TestRemoteAuthBootstrapHybridBrowserAcceptsCallbackOrPaste(t *testing.T) {
	tests := []struct {
		name      string
		runPage   func(context.Context, authCallbackPageData, func(context.Context) (authui.OAuthBrowserCallback, error), func(context.Context, string) error) (authCallbackPageResult, error)
		wantInput string
	}{
		{
			name: "listener callback",
			runPage: func(ctx context.Context, _ authCallbackPageData, waitCallback func(context.Context) (authui.OAuthBrowserCallback, error), complete func(context.Context, string) error) (authCallbackPageResult, error) {
				callback, err := waitCallback(ctx)
				if err != nil {
					return authCallbackPageResult{}, err
				}
				input := browserCallbackInput(callback)
				return authCallbackPageResult{CallbackInput: input}, complete(ctx, input)
			},
			wantInput: "code=code-1&state=",
		},
		{
			name: "pasted callback",
			runPage: func(ctx context.Context, _ authCallbackPageData, _ func(context.Context) (authui.OAuthBrowserCallback, error), complete func(context.Context, string) error) (authCallbackPageResult, error) {
				input := "http://localhost/callback?code=pasted"
				return authCallbackPageResult{CallbackInput: input}, complete(ctx, input)
			},
			wantInput: "http://localhost/callback?code=pasted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useStartupTestTerminal(t)
			listener := &stubOAuthCallbackListener{callback: authui.OAuthBrowserCallback{Code: "code-1"}}
			remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
				AuthReady:         false,
				AuthRequired:      true,
				CallbackTransport: &authpb.BootstrapCallbackTransport{BindAddress: "127.0.0.1:1455", RedirectHost: "localhost", CallbackPath: "/auth/callback"},
				SupportedModes: []authpb.BootstrapMode{
					authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
				},
			}}
			interactor := &interactiveAuthInteractor{
				pickMethod: func(authInteraction) (authMethodPickerResult, error) {
					return authMethodPickerResult{Choice: authMethodChoiceBrowserAuto}, nil
				},
				startCallbackListener: func(sharedauth.CallbackTransport) (oauthCallbackListener, error) { return listener, nil },
				openBrowser:           func(string) error { return nil },
				runCallbackPage:       tt.runPage,
			}

			request, err := interactor.collectRemoteBootstrapRequest(t.Context(), remote, protoapi.ExistingConnectionTarget("test"), "dark", authMethodChoiceBrowserAuto, remote.status)
			if err != nil {
				t.Fatal(err)
			}
			if request.GetCallbackInput() != tt.wantInput {
				t.Fatalf("callback input=%q, want %q", request.GetCallbackInput(), tt.wantInput)
			}
			if listener.closed == 0 {
				t.Fatal("expected listener to close")
			}
		})
	}
}

func TestRemoteAuthBootstrapHybridBrowserCancelClosesListener(t *testing.T) {
	useStartupTestTerminal(t)
	listener := &stubOAuthCallbackListener{}
	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		AuthReady:         false,
		AuthRequired:      true,
		CallbackTransport: &authpb.BootstrapCallbackTransport{BindAddress: "127.0.0.1:1455", RedirectHost: "localhost", CallbackPath: "/auth/callback"},
		SupportedModes: []authpb.BootstrapMode{
			authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
		},
	}}
	pickCalls := 0
	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			pickCalls++
			if pickCalls > 1 {
				return authMethodPickerResult{Canceled: true}, nil
			}
			return authMethodPickerResult{Choice: authMethodChoiceBrowserAuto}, nil
		},
		startCallbackListener: func(sharedauth.CallbackTransport) (oauthCallbackListener, error) { return listener, nil },
		openBrowser:           func(string) error { return nil },
		runCallbackPage: func(context.Context, authCallbackPageData, func(context.Context) (authui.OAuthBrowserCallback, error), func(context.Context, string) error) (authCallbackPageResult, error) {
			return authCallbackPageResult{Canceled: true}, nil
		},
	}

	_, err := interactor.collectRemoteBootstrapRequest(t.Context(), remote, protoapi.ExistingConnectionTarget("test"), "dark", authMethodChoiceBrowserAuto, remote.status)
	if err == nil || !errors.Is(err, ErrAuthCanceledByUser) {
		t.Fatalf("expected auth cancel, got %v", err)
	}
	if listener.closed == 0 {
		t.Fatal("expected listener to close")
	}
}

func TestRemoteAuthBootstrapRejectsMismatchedOAuthState(t *testing.T) {
	useStartupTestTerminal(t)
	listener := &stubOAuthCallbackListener{}
	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		AuthReady:         false,
		AuthRequired:      true,
		CallbackTransport: &authpb.BootstrapCallbackTransport{BindAddress: "127.0.0.1:1455", RedirectHost: "localhost", CallbackPath: "/auth/callback"},
		SupportedModes: []authpb.BootstrapMode{
			authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
		},
	}}
	interactor := &interactiveAuthInteractor{
		startCallbackListener: func(sharedauth.CallbackTransport) (oauthCallbackListener, error) { return listener, nil },
		openBrowser:           func(string) error { return nil },
		runCallbackPage: func(ctx context.Context, _ authCallbackPageData, _ func(context.Context) (authui.OAuthBrowserCallback, error), complete func(context.Context, string) error) (authCallbackPageResult, error) {
			input := "http://localhost/callback?code=pasted&state=wrong"
			return authCallbackPageResult{CallbackInput: input}, complete(ctx, input)
		},
	}

	_, err := interactor.collectRemoteBootstrapRequest(t.Context(), remote, protoapi.ExistingConnectionTarget("test"), "dark", authMethodChoiceBrowserAuto, remote.status)
	if !errors.Is(err, ErrOAuthStateMismatch) {
		t.Fatalf("flow error = %v, want oauth state mismatch", err)
	}
}
