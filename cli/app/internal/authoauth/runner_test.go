package authoauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"builder/cli/app/internal/oauthadapter"
)

type fakeListener struct {
	redirectURI string
	callback    oauthadapter.BrowserCallback
	waitTimeout time.Duration
	closed      bool
}

func (l *fakeListener) RedirectURI() string { return l.redirectURI }

func (l *fakeListener) Wait(ctx context.Context, timeout time.Duration) (oauthadapter.BrowserCallback, error) {
	l.waitTimeout = timeout
	return l.callback, nil
}

func (l *fakeListener) Close() error {
	l.closed = true
	return nil
}

type recordingPresenter struct {
	autoSession  oauthadapter.BrowserAuthSession
	autoOpenErr  error
	pasteSession oauthadapter.BrowserAuthSession
	pasteOpenErr error
	deviceCode   oauthadapter.DeviceCode
}

func (p *recordingPresenter) ShowBrowserAuto(session oauthadapter.BrowserAuthSession, openErr error) {
	p.autoSession = session
	p.autoOpenErr = openErr
}

func (p *recordingPresenter) ShowBrowserPaste(session oauthadapter.BrowserAuthSession, openErr error) {
	p.pasteSession = session
	p.pasteOpenErr = openErr
}

func (p *recordingPresenter) ShowDeviceCode(code oauthadapter.DeviceCode) {
	p.deviceCode = code
}

func TestRunnerBrowserAutoUsesListenerRedirectAndCallbackQuery(t *testing.T) {
	listener := &fakeListener{
		redirectURI: "http://127.0.0.1/callback",
		callback:    oauthadapter.BrowserCallback{Code: "code-1", State: "state-1"},
	}
	presenter := &recordingPresenter{}
	var beginRedirect string
	var openedURL string
	var completedInput string
	_, err := (Runner{
		StartCallbackListener: func() (CallbackListener, error) { return listener, nil },
		BeginBrowserFlow: func(_ oauthadapter.OpenAIOAuthOptions, redirectURI string) (oauthadapter.BrowserAuthSession, error) {
			beginRedirect = redirectURI
			return oauthadapter.BrowserAuthSession{AuthorizeURL: "https://auth.example/authorize", State: "state-1"}, nil
		},
		OpenBrowser: func(url string) error {
			openedURL = url
			return errors.New("open failed")
		},
		CompleteBrowserFlow: func(_ context.Context, _ oauthadapter.OpenAIOAuthOptions, _ oauthadapter.BrowserAuthSession, callbackInput string) (oauthadapter.Method, error) {
			completedInput = callbackInput
			return oauthadapter.Method{}, nil
		},
		Presenter: presenter,
	}).BrowserAuto(context.Background(), oauthadapter.OpenAIOAuthOptions{PollTimeout: 7 * time.Second})
	if err != nil {
		t.Fatalf("browser auto: %v", err)
	}
	if beginRedirect != listener.redirectURI {
		t.Fatalf("begin redirect = %q, want %q", beginRedirect, listener.redirectURI)
	}
	if openedURL != "https://auth.example/authorize" {
		t.Fatalf("opened URL = %q", openedURL)
	}
	if completedInput != "code=code-1&state=state-1" {
		t.Fatalf("completed input = %q", completedInput)
	}
	if presenter.autoSession.AuthorizeURL != openedURL || presenter.autoOpenErr == nil {
		t.Fatalf("presenter did not receive browser auto event: %+v err=%v", presenter.autoSession, presenter.autoOpenErr)
	}
	if listener.waitTimeout != 7*time.Second || !listener.closed {
		t.Fatalf("listener wait/close mismatch: timeout=%v closed=%v", listener.waitTimeout, listener.closed)
	}
}

func TestRunnerBrowserAutoDelegatesToHybridPageAndClosesListener(t *testing.T) {
	tests := []struct {
		name      string
		page      BrowserCallbackPageFunc
		wantInput string
		wantErr   string
	}{
		{
			name: "listener callback succeeds",
			page: func(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions, session oauthadapter.BrowserAuthSession, _ error, listener CallbackListener, complete CompleteBrowserFlowFunc) (oauthadapter.Method, error) {
				callback, err := listener.Wait(ctx, opts.PollTimeout)
				if err != nil {
					return oauthadapter.Method{}, err
				}
				return complete(ctx, opts, session, callbackQuery(callback).Encode())
			},
			wantInput: "code=code-1&state=state-1",
		},
		{
			name: "pasted callback succeeds",
			page: func(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions, session oauthadapter.BrowserAuthSession, _ error, _ CallbackListener, complete CompleteBrowserFlowFunc) (oauthadapter.Method, error) {
				return complete(ctx, opts, session, "http://localhost/callback?code=pasted&state=state-1")
			},
			wantInput: "http://localhost/callback?code=pasted&state=state-1",
		},
		{
			name: "esc cancel returns error",
			page: func(context.Context, oauthadapter.OpenAIOAuthOptions, oauthadapter.BrowserAuthSession, error, CallbackListener, CompleteBrowserFlowFunc) (oauthadapter.Method, error) {
				return oauthadapter.Method{}, errors.New("auth canceled by user")
			},
			wantErr: "auth canceled by user",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener := &fakeListener{
				redirectURI: "http://127.0.0.1/callback",
				callback:    oauthadapter.BrowserCallback{Code: "code-1", State: "state-1"},
			}
			var completedInput string
			_, err := (Runner{
				StartCallbackListener: func() (CallbackListener, error) { return listener, nil },
				BeginBrowserFlow: func(_ oauthadapter.OpenAIOAuthOptions, redirectURI string) (oauthadapter.BrowserAuthSession, error) {
					return oauthadapter.BrowserAuthSession{AuthorizeURL: "https://auth.example/authorize", RedirectURI: redirectURI, State: "state-1"}, nil
				},
				OpenBrowser: func(string) error { return nil },
				CompleteBrowserFlow: func(_ context.Context, _ oauthadapter.OpenAIOAuthOptions, _ oauthadapter.BrowserAuthSession, callbackInput string) (oauthadapter.Method, error) {
					completedInput = callbackInput
					return oauthadapter.Method{}, nil
				},
				BrowserCallbackPage: tt.page,
			}).BrowserAuto(context.Background(), oauthadapter.OpenAIOAuthOptions{PollTimeout: 7 * time.Second})
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("BrowserAuto error=%v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("BrowserAuto: %v", err)
			}
			if completedInput != tt.wantInput {
				t.Fatalf("completed input=%q, want %q", completedInput, tt.wantInput)
			}
			if !listener.closed {
				t.Fatal("expected listener to close")
			}
		})
	}
}

func TestRunnerBrowserPastePromptsForCallbackInput(t *testing.T) {
	presenter := &recordingPresenter{}
	var beginRedirect string
	var promptLabel string
	var completedInput string
	_, err := (Runner{
		BeginBrowserFlow: func(_ oauthadapter.OpenAIOAuthOptions, redirectURI string) (oauthadapter.BrowserAuthSession, error) {
			beginRedirect = redirectURI
			return oauthadapter.BrowserAuthSession{AuthorizeURL: "https://auth.example/paste"}, nil
		},
		OpenBrowser: func(string) error { return nil },
		Prompt: func(label string) (string, error) {
			promptLabel = label
			return "http://localhost/callback?code=manual", nil
		},
		CompleteBrowserFlow: func(_ context.Context, _ oauthadapter.OpenAIOAuthOptions, _ oauthadapter.BrowserAuthSession, callbackInput string) (oauthadapter.Method, error) {
			completedInput = callbackInput
			return oauthadapter.Method{}, nil
		},
		Presenter: presenter,
	}).BrowserPaste(context.Background(), oauthadapter.OpenAIOAuthOptions{})
	if err != nil {
		t.Fatalf("browser paste: %v", err)
	}
	if beginRedirect != "" {
		t.Fatalf("manual browser flow redirect = %q, want empty fallback", beginRedirect)
	}
	if promptLabel != "Paste callback URL or code: " {
		t.Fatalf("prompt label = %q", promptLabel)
	}
	if completedInput != "http://localhost/callback?code=manual" {
		t.Fatalf("completed input = %q", completedInput)
	}
	if presenter.pasteSession.AuthorizeURL != "https://auth.example/paste" {
		t.Fatalf("presenter did not receive paste session: %+v", presenter.pasteSession)
	}
}

func TestRunnerDevicePresentsCode(t *testing.T) {
	presenter := &recordingPresenter{}
	_, err := (Runner{
		RunDeviceFlow: func(_ context.Context, _ oauthadapter.OpenAIOAuthOptions, onCode func(oauthadapter.DeviceCode)) (oauthadapter.Method, error) {
			onCode(oauthadapter.DeviceCode{VerificationURL: "https://verify.example", UserCode: "ABCD"})
			return oauthadapter.Method{}, nil
		},
		Presenter: presenter,
	}).Device(context.Background(), oauthadapter.OpenAIOAuthOptions{})
	if err != nil {
		t.Fatalf("device: %v", err)
	}
	if presenter.deviceCode.VerificationURL != "https://verify.example" || presenter.deviceCode.UserCode != "ABCD" {
		t.Fatalf("presenter device code = %+v", presenter.deviceCode)
	}
}
