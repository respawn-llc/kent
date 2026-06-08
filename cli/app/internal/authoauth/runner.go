package authoauth

import (
	"context"
	"errors"
	"net/url"
	"time"

	"builder/cli/app/internal/oauthadapter"
	serverauth "builder/server/auth"
)

type CallbackListener interface {
	RedirectURI() string
	Wait(ctx context.Context, timeoutSeconds time.Duration) (oauthadapter.BrowserCallback, error)
	Close() error
}

type BeginBrowserFlowFunc func(oauthadapter.OpenAIOAuthOptions, string) (oauthadapter.BrowserAuthSession, error)
type CompleteBrowserFlowFunc func(context.Context, oauthadapter.OpenAIOAuthOptions, oauthadapter.BrowserAuthSession, string) (oauthadapter.Method, error)
type OpenBrowserFunc func(string) error
type StartCallbackListenerFunc func() (CallbackListener, error)
type RunDeviceFlowFunc func(context.Context, oauthadapter.OpenAIOAuthOptions, func(oauthadapter.DeviceCode)) (oauthadapter.Method, error)
type PromptFunc func(string) (string, error)
type BrowserCallbackPageFunc func(context.Context, oauthadapter.OpenAIOAuthOptions, oauthadapter.BrowserAuthSession, error, CallbackListener, CompleteBrowserFlowFunc) (oauthadapter.Method, error)

type Presenter interface {
	ShowBrowserAuto(session oauthadapter.BrowserAuthSession, openErr error)
	ShowBrowserPaste(session oauthadapter.BrowserAuthSession, openErr error)
	ShowDeviceCode(code oauthadapter.DeviceCode)
}

type Runner struct {
	BeginBrowserFlow      BeginBrowserFlowFunc
	CompleteBrowserFlow   CompleteBrowserFlowFunc
	OpenBrowser           OpenBrowserFunc
	StartCallbackListener StartCallbackListenerFunc
	RunDeviceFlow         RunDeviceFlowFunc
	Prompt                PromptFunc
	Presenter             Presenter
	BrowserCallbackPage   BrowserCallbackPageFunc
}

func (r Runner) BrowserAuto(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions) (oauthadapter.Method, error) {
	startCallbackListener := r.StartCallbackListener
	if startCallbackListener == nil {
		startCallbackListener = func() (CallbackListener, error) {
			return serverauth.StartOAuthCallbackListener()
		}
	}
	listener, err := startCallbackListener()
	if err != nil {
		return oauthadapter.Method{}, err
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
		return oauthadapter.Method{}, err
	}
	openBrowser := r.OpenBrowser
	if openBrowser == nil {
		openBrowser = serverauth.OpenBrowser
	}
	openErr := openBrowser(session.AuthorizeURL)
	if r.Presenter != nil {
		r.Presenter.ShowBrowserAuto(session, openErr)
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
		return oauthadapter.Method{}, err
	}
	query := callbackQuery(callback)
	return completeBrowserFlow(ctx, opts, session, query.Encode())
}

func (r Runner) BrowserPaste(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions) (oauthadapter.Method, error) {
	beginBrowserFlow := r.BeginBrowserFlow
	if beginBrowserFlow == nil {
		beginBrowserFlow = serverauth.BeginOpenAIBrowserFlow
	}
	session, err := beginBrowserFlow(opts, "")
	if err != nil {
		return oauthadapter.Method{}, err
	}
	openBrowser := r.OpenBrowser
	if openBrowser == nil {
		openBrowser = serverauth.OpenBrowser
	}
	openErr := openBrowser(session.AuthorizeURL)
	if r.Presenter != nil {
		r.Presenter.ShowBrowserPaste(session, openErr)
	}
	prompt := r.Prompt
	if prompt == nil {
		prompt = func(string) (string, error) {
			return "", errors.New("oauth prompt is required")
		}
	}
	callbackInput, err := prompt("Paste callback URL or code: ")
	if err != nil {
		return oauthadapter.Method{}, err
	}
	completeBrowserFlow := r.CompleteBrowserFlow
	if completeBrowserFlow == nil {
		completeBrowserFlow = serverauth.CompleteOpenAIBrowserFlow
	}
	return completeBrowserFlow(ctx, opts, session, callbackInput)
}

func callbackQuery(callback oauthadapter.BrowserCallback) url.Values {
	return url.Values{
		"code":  []string{callback.Code},
		"state": []string{callback.State},
	}
}

func (r Runner) Device(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions) (oauthadapter.Method, error) {
	runDeviceFlow := r.RunDeviceFlow
	if runDeviceFlow == nil {
		runDeviceFlow = serverauth.RunOpenAIDeviceCodeFlow
	}
	return runDeviceFlow(ctx, opts, func(code oauthadapter.DeviceCode) {
		if r.Presenter != nil {
			r.Presenter.ShowDeviceCode(code)
		}
	})
}
