package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"core/cli/app/internal/authui"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/emptypb"
)

type stubAuthBootstrapClient struct {
	status        *authpb.BootstrapStatus
	completeReq   *authpb.CompleteBootstrapRequest
	completeCalls int
	completeErr   error
	completeResp  *authpb.BootstrapCompletion
}

func (c *stubAuthBootstrapClient) GetBootstrapStatus(context.Context, *authpb.GetBootstrapStatusRequest) (*authpb.BootstrapStatus, error) {
	return c.status, nil
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

	if err := ensureRemoteAuthReady(t.Context(), remote, config.Settings{Connection: config.SingleConnection(connection)}, newHeadlessAuthInteractor()); err != nil {
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

	if err := ensureRemoteAuthReady(t.Context(), remote, config.Settings{Connection: config.SingleConnection(connection)}, newHeadlessAuthInteractor()); err != nil {
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

	err := ensureRemoteAuthReady(t.Context(), remote, config.Settings{Connection: config.SingleConnection(connection)}, newHeadlessAuthInteractor())
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

	err := ensureRemoteAuthReady(t.Context(), remote, config.Settings{Connection: config.SingleConnection(connection)}, nil)
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

func TestRemoteAuthBootstrapMapsProviderDeviceGrantToCompletion(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":"device-1","user_code":"CODE-1","interval":1}`))
		case "/api/accounts/deviceauth/token":
			_, _ = w.Write([]byte(`{"authorization_code":"authorization-1","code_verifier":"verifier-1"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer provider.Close()

	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		AuthRequired:   true,
		SupportedModes: []authpb.BootstrapMode{authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE},
		Oauth: &authpb.BootstrapOAuthConfig{
			Issuer:   &provider.URL,
			ClientId: ptrString("client-1"),
		},
	}}
	interactor := &interactiveAuthInteractor{
		pickMethod: func(authInteraction) (authMethodPickerResult, error) {
			return authMethodPickerResult{Choice: authMethodChoiceDevice}, nil
		},
	}

	request, err := interactor.collectRemoteBootstrapRequest(t.Context(), "dark", authMethodChoiceDevice, remote.status)
	if err != nil {
		t.Fatal(err)
	}
	if request.Mode != authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE ||
		request.GetDeviceAuthorizationCode() != "authorization-1" ||
		request.GetDeviceCodeVerifier() != "verifier-1" {
		t.Fatalf("unexpected completion request: %+v", request)
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
			listener := &stubOAuthCallbackListener{callback: authui.OAuthBrowserCallback{Code: "code-1"}}
			remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
				AuthReady:    false,
				AuthRequired: true,
				SupportedModes: []authpb.BootstrapMode{
					authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
				},
			}}
			interactor := &interactiveAuthInteractor{
				pickMethod: func(authInteraction) (authMethodPickerResult, error) {
					return authMethodPickerResult{Choice: authMethodChoiceBrowserAuto}, nil
				},
				startCallbackListener: func() (oauthCallbackListener, error) { return listener, nil },
				openBrowser:           func(string) error { return nil },
				runCallbackPage:       tt.runPage,
			}

			request, err := interactor.collectRemoteBootstrapRequest(t.Context(), "dark", authMethodChoiceBrowserAuto, remote.status)
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
	listener := &stubOAuthCallbackListener{}
	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		AuthReady:    false,
		AuthRequired: true,
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
		startCallbackListener: func() (oauthCallbackListener, error) { return listener, nil },
		openBrowser:           func(string) error { return nil },
		runCallbackPage: func(context.Context, authCallbackPageData, func(context.Context) (authui.OAuthBrowserCallback, error), func(context.Context, string) error) (authCallbackPageResult, error) {
			return authCallbackPageResult{Canceled: true}, nil
		},
	}

	_, err := interactor.collectRemoteBootstrapRequest(t.Context(), "dark", authMethodChoiceBrowserAuto, remote.status)
	if err == nil || !errors.Is(err, ErrAuthCanceledByUser) {
		t.Fatalf("expected auth cancel, got %v", err)
	}
	if listener.closed == 0 {
		t.Fatal("expected listener to close")
	}
}

func TestRemoteAuthBootstrapRejectsMismatchedOAuthState(t *testing.T) {
	listener := &stubOAuthCallbackListener{}
	remote := &stubAuthBootstrapClient{status: &authpb.BootstrapStatus{
		AuthReady:    false,
		AuthRequired: true,
		SupportedModes: []authpb.BootstrapMode{
			authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
		},
	}}
	interactor := &interactiveAuthInteractor{
		startCallbackListener: func() (oauthCallbackListener, error) { return listener, nil },
		openBrowser:           func(string) error { return nil },
		runCallbackPage: func(ctx context.Context, _ authCallbackPageData, _ func(context.Context) (authui.OAuthBrowserCallback, error), complete func(context.Context, string) error) (authCallbackPageResult, error) {
			input := "http://localhost/callback?code=pasted&state=wrong"
			return authCallbackPageResult{CallbackInput: input}, complete(ctx, input)
		},
	}

	_, err := interactor.collectRemoteBootstrapRequest(t.Context(), "dark", authMethodChoiceBrowserAuto, remote.status)
	if !errors.Is(err, ErrOAuthStateMismatch) {
		t.Fatalf("flow error = %v, want oauth state mismatch", err)
	}
}
