package authoauth

import (
	"context"
	"errors"
	"net/url"
	"time"

	"builder/cli/app/internal/oauthadapter"
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
}

func (r Runner) BrowserAuto(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions) (oauthadapter.Method, error) {
	listener, err := r.startCallbackListener()()
	if err != nil {
		return oauthadapter.Method{}, err
	}
	defer func() {
		_ = listener.Close()
	}()
	session, err := r.beginBrowserFlow()(opts, listener.RedirectURI())
	if err != nil {
		return oauthadapter.Method{}, err
	}
	openErr := r.openBrowser()(session.AuthorizeURL)
	if r.Presenter != nil {
		r.Presenter.ShowBrowserAuto(session, openErr)
	}
	callback, err := listener.Wait(ctx, opts.PollTimeout)
	if err != nil {
		return oauthadapter.Method{}, err
	}
	query := url.Values{
		"code":  []string{callback.Code},
		"state": []string{callback.State},
	}
	return r.completeBrowserFlow()(ctx, opts, session, query.Encode())
}

func (r Runner) BrowserPaste(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions) (oauthadapter.Method, error) {
	session, err := r.beginBrowserFlow()(opts, "")
	if err != nil {
		return oauthadapter.Method{}, err
	}
	openErr := r.openBrowser()(session.AuthorizeURL)
	if r.Presenter != nil {
		r.Presenter.ShowBrowserPaste(session, openErr)
	}
	callbackInput, err := r.prompt()("Paste callback URL or code: ")
	if err != nil {
		return oauthadapter.Method{}, err
	}
	return r.completeBrowserFlow()(ctx, opts, session, callbackInput)
}

func (r Runner) Device(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions) (oauthadapter.Method, error) {
	return r.runDeviceFlow()(ctx, opts, func(code oauthadapter.DeviceCode) {
		if r.Presenter != nil {
			r.Presenter.ShowDeviceCode(code)
		}
	})
}

func (r Runner) beginBrowserFlow() BeginBrowserFlowFunc {
	if r.BeginBrowserFlow != nil {
		return r.BeginBrowserFlow
	}
	return oauthadapter.BeginOpenAIBrowserFlow
}

func (r Runner) completeBrowserFlow() CompleteBrowserFlowFunc {
	if r.CompleteBrowserFlow != nil {
		return r.CompleteBrowserFlow
	}
	return oauthadapter.CompleteOpenAIBrowserFlow
}

func (r Runner) openBrowser() OpenBrowserFunc {
	if r.OpenBrowser != nil {
		return r.OpenBrowser
	}
	return oauthadapter.OpenBrowser
}

func (r Runner) startCallbackListener() StartCallbackListenerFunc {
	if r.StartCallbackListener != nil {
		return r.StartCallbackListener
	}
	return func() (CallbackListener, error) {
		return oauthadapter.StartOAuthCallbackListener()
	}
}

func (r Runner) runDeviceFlow() RunDeviceFlowFunc {
	if r.RunDeviceFlow != nil {
		return r.RunDeviceFlow
	}
	return oauthadapter.RunOpenAIDeviceCodeFlow
}

func (r Runner) prompt() PromptFunc {
	if r.Prompt != nil {
		return r.Prompt
	}
	return func(string) (string, error) {
		return "", errors.New("oauth prompt is required")
	}
}
