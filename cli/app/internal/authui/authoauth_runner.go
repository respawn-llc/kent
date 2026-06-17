package authui

import (
	"context"
	"errors"
	"net/url"
	"time"

	serverauth "core/server/auth"
)

type OAuthCallbackListener interface {
	RedirectURI() string
	Wait(ctx context.Context, timeoutSeconds time.Duration) (OAuthBrowserCallback, error)
	Close() error
}

type OAuthBeginBrowserFlowFunc func(OAuthOptions, string) (OAuthBrowserSession, error)
type OAuthCompleteBrowserFlowFunc func(context.Context, OAuthOptions, OAuthBrowserSession, string) (AuthMethod, error)
type OAuthOpenBrowserFunc func(string) error
type OAuthStartCallbackListenerFunc func() (OAuthCallbackListener, error)
type OAuthRunDeviceFlowFunc func(context.Context, OAuthOptions, func(OAuthDeviceCode)) (AuthMethod, error)
type PromptFunc func(string) (string, error)
type OAuthBrowserCallbackPageFunc func(context.Context, OAuthOptions, OAuthBrowserSession, error, OAuthCallbackListener, OAuthCompleteBrowserFlowFunc) (AuthMethod, error)

type OAuthPresenter interface {
	ShowBrowserAuto(session OAuthBrowserSession, openErr error)
	ShowBrowserPaste(session OAuthBrowserSession, openErr error)
	ShowDeviceCode(code OAuthDeviceCode)
}

type OAuthRunner struct {
	BeginBrowserFlow      OAuthBeginBrowserFlowFunc
	CompleteBrowserFlow   OAuthCompleteBrowserFlowFunc
	OpenBrowser           OAuthOpenBrowserFunc
	StartCallbackListener OAuthStartCallbackListenerFunc
	RunDeviceFlow         OAuthRunDeviceFlowFunc
	Prompt                PromptFunc
	OAuthPresenter        OAuthPresenter
	BrowserCallbackPage   OAuthBrowserCallbackPageFunc
}

func (r OAuthRunner) BrowserAuto(ctx context.Context, opts OAuthOptions) (AuthMethod, error) {
	startCallbackListener := r.StartCallbackListener
	if startCallbackListener == nil {
		startCallbackListener = func() (OAuthCallbackListener, error) {
			return serverauth.StartOAuthCallbackListener()
		}
	}
	listener, err := startCallbackListener()
	if err != nil {
		return AuthMethod{}, err
	}
	defer func() {
		_ = listener.Close()
	}()
	beginBrowserFlow := r.BeginBrowserFlow
	if beginBrowserFlow == nil {
		beginBrowserFlow = serverauth.BeginOpenAIBrowserFlow
	}
	session, err := beginBrowserFlow(opts, listener.RedirectURI())
	if err != nil {
		return AuthMethod{}, err
	}
	openBrowser := r.OpenBrowser
	if openBrowser == nil {
		openBrowser = serverauth.OpenBrowser
	}
	openErr := openBrowser(session.AuthorizeURL)
	if r.OAuthPresenter != nil {
		r.OAuthPresenter.ShowBrowserAuto(session, openErr)
	}
	completeBrowserFlow := r.CompleteBrowserFlow
	if completeBrowserFlow == nil {
		completeBrowserFlow = serverauth.CompleteOpenAIBrowserFlow
	}
	if r.BrowserCallbackPage != nil {
		return r.BrowserCallbackPage(ctx, opts, session, openErr, listener, completeBrowserFlow)
	}
	callback, err := listener.Wait(ctx, opts.PollTimeout)
	if err != nil {
		return AuthMethod{}, err
	}
	query := callbackQuery(callback)
	return completeBrowserFlow(ctx, opts, session, query.Encode())
}

func (r OAuthRunner) BrowserPaste(ctx context.Context, opts OAuthOptions) (AuthMethod, error) {
	beginBrowserFlow := r.BeginBrowserFlow
	if beginBrowserFlow == nil {
		beginBrowserFlow = serverauth.BeginOpenAIBrowserFlow
	}
	session, err := beginBrowserFlow(opts, "")
	if err != nil {
		return AuthMethod{}, err
	}
	openBrowser := r.OpenBrowser
	if openBrowser == nil {
		openBrowser = serverauth.OpenBrowser
	}
	openErr := openBrowser(session.AuthorizeURL)
	if r.OAuthPresenter != nil {
		r.OAuthPresenter.ShowBrowserPaste(session, openErr)
	}
	prompt := r.Prompt
	if prompt == nil {
		prompt = func(string) (string, error) {
			return "", errors.New("oauth prompt is required")
		}
	}
	callbackInput, err := prompt("Paste callback URL or code: ")
	if err != nil {
		return AuthMethod{}, err
	}
	completeBrowserFlow := r.CompleteBrowserFlow
	if completeBrowserFlow == nil {
		completeBrowserFlow = serverauth.CompleteOpenAIBrowserFlow
	}
	return completeBrowserFlow(ctx, opts, session, callbackInput)
}

func callbackQuery(callback OAuthBrowserCallback) url.Values {
	return url.Values{
		"code":  []string{callback.Code},
		"state": []string{callback.State},
	}
}

func (r OAuthRunner) Device(ctx context.Context, opts OAuthOptions) (AuthMethod, error) {
	runDeviceFlow := r.RunDeviceFlow
	if runDeviceFlow == nil {
		runDeviceFlow = serverauth.RunOpenAIDeviceCodeFlow
	}
	return runDeviceFlow(ctx, opts, func(code OAuthDeviceCode) {
		if r.OAuthPresenter != nil {
			r.OAuthPresenter.ShowDeviceCode(code)
		}
	})
}
