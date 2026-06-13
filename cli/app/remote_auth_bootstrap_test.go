package app

import (
	"context"
	"errors"
	"testing"

	"core/cli/app/internal/oauthadapter"
	"core/shared/config"
	"core/shared/serverapi"
)

type stubAuthBootstrapClient struct {
	status      serverapi.AuthGetBootstrapStatusResponse
	completeReq serverapi.AuthCompleteBootstrapRequest
}

func (c *stubAuthBootstrapClient) GetAuthBootstrapStatus(context.Context, serverapi.AuthGetBootstrapStatusRequest) (serverapi.AuthGetBootstrapStatusResponse, error) {
	return c.status, nil
}

func (c *stubAuthBootstrapClient) CompleteAuthBootstrap(_ context.Context, req serverapi.AuthCompleteBootstrapRequest) (serverapi.AuthCompleteBootstrapResponse, error) {
	c.completeReq = req
	return serverapi.AuthCompleteBootstrapResponse{AuthReady: true, MethodType: "oauth"}, nil
}

func TestRemoteAuthBootstrapHybridBrowserAcceptsCallbackOrPaste(t *testing.T) {
	tests := []struct {
		name      string
		runPage   func(context.Context, authCallbackPageData, func(context.Context) (oauthadapter.BrowserCallback, error), func(context.Context, string) (oauthadapter.Method, error)) (authCallbackPageResult, error)
		wantInput string
	}{
		{
			name: "listener callback",
			runPage: func(ctx context.Context, _ authCallbackPageData, waitCallback func(context.Context) (oauthadapter.BrowserCallback, error), complete func(context.Context, string) (oauthadapter.Method, error)) (authCallbackPageResult, error) {
				callback, err := waitCallback(ctx)
				if err != nil {
					return authCallbackPageResult{}, err
				}
				input := browserCallbackInput(callback)
				method, err := complete(ctx, input)
				return authCallbackPageResult{Method: method, CallbackInput: input}, err
			},
			wantInput: "code=code-1&state=",
		},
		{
			name: "pasted callback",
			runPage: func(ctx context.Context, _ authCallbackPageData, _ func(context.Context) (oauthadapter.BrowserCallback, error), complete func(context.Context, string) (oauthadapter.Method, error)) (authCallbackPageResult, error) {
				input := "http://localhost/callback?code=pasted"
				method, err := complete(ctx, input)
				return authCallbackPageResult{Method: method, CallbackInput: input}, err
			},
			wantInput: "http://localhost/callback?code=pasted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener := &stubOAuthCallbackListener{callback: oauthadapter.BrowserCallback{Code: "code-1"}}
			remote := &stubAuthBootstrapClient{status: serverapi.AuthGetBootstrapStatusResponse{
				AuthReady:    false,
				AuthRequired: true,
				SupportedModes: []serverapi.AuthBootstrapMode{
					serverapi.AuthBootstrapModeBrowserCallbackURL,
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

			if err := ensureRemoteAuthReady(context.Background(), remote, config.Settings{}, interactor, true); err != nil {
				t.Fatalf("ensureRemoteAuthReady: %v", err)
			}
			if remote.completeReq.CallbackInput != tt.wantInput {
				t.Fatalf("callback input=%q, want %q", remote.completeReq.CallbackInput, tt.wantInput)
			}
			if listener.closed == 0 {
				t.Fatal("expected listener to close")
			}
		})
	}
}

func TestRemoteAuthBootstrapHybridBrowserCancelClosesListener(t *testing.T) {
	listener := &stubOAuthCallbackListener{}
	remote := &stubAuthBootstrapClient{status: serverapi.AuthGetBootstrapStatusResponse{
		AuthReady:    false,
		AuthRequired: true,
		SupportedModes: []serverapi.AuthBootstrapMode{
			serverapi.AuthBootstrapModeBrowserCallbackURL,
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
		runCallbackPage: func(context.Context, authCallbackPageData, func(context.Context) (oauthadapter.BrowserCallback, error), func(context.Context, string) (oauthadapter.Method, error)) (authCallbackPageResult, error) {
			return authCallbackPageResult{Canceled: true}, nil
		},
	}

	err := ensureRemoteAuthReady(context.Background(), remote, config.Settings{}, interactor, true)
	if err == nil || !errors.Is(err, ErrAuthCanceledByUser) {
		t.Fatalf("expected auth cancel, got %v", err)
	}
	if listener.closed == 0 {
		t.Fatal("expected listener to close")
	}
}

func TestRemoteAuthBootstrapRejectsMismatchedOAuthState(t *testing.T) {
	listener := &stubOAuthCallbackListener{}
	remote := &stubAuthBootstrapClient{status: serverapi.AuthGetBootstrapStatusResponse{
		AuthReady:    false,
		AuthRequired: true,
		SupportedModes: []serverapi.AuthBootstrapMode{
			serverapi.AuthBootstrapModeBrowserCallbackURL,
		},
	}}
	pickCalls := 0
	var flowErr error
	interactor := &interactiveAuthInteractor{
		pickMethod: func(req authInteraction) (authMethodPickerResult, error) {
			pickCalls++
			if pickCalls > 1 {
				flowErr = req.FlowErr
				return authMethodPickerResult{Canceled: true}, nil
			}
			return authMethodPickerResult{Choice: authMethodChoiceBrowserAuto}, nil
		},
		startCallbackListener: func() (oauthCallbackListener, error) { return listener, nil },
		openBrowser:           func(string) error { return nil },
		runCallbackPage: func(ctx context.Context, _ authCallbackPageData, _ func(context.Context) (oauthadapter.BrowserCallback, error), complete func(context.Context, string) (oauthadapter.Method, error)) (authCallbackPageResult, error) {
			input := "http://localhost/callback?code=pasted&state=wrong"
			method, err := complete(ctx, input)
			return authCallbackPageResult{Method: method, CallbackInput: input}, err
		},
	}

	err := ensureRemoteAuthReady(context.Background(), remote, config.Settings{}, interactor, true)
	if err == nil || !errors.Is(err, ErrAuthCanceledByUser) {
		t.Fatalf("expected auth cancel, got %v", err)
	}
	if flowErr == nil || !errors.Is(flowErr, ErrOAuthStateMismatch) {
		t.Fatalf("flow error = %v, want oauth state mismatch", flowErr)
	}
}
