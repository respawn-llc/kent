package app

import (
	"context"
	"io"
	"os"
	"time"

	"core/cli/app/internal/authui"
	serverauth "core/server/auth"
)

type authInteraction struct {
	Theme   string
	FlowErr error
}

type authInteractor interface {
	isAuthInteractor()
}

type headlessAuthInteractor struct{}

type oauthCallbackListener interface {
	RedirectURI() string
	Wait(ctx context.Context, timeoutSeconds time.Duration) (authui.OAuthBrowserCallback, error)
	Close() error
}

type interactiveAuthInteractor struct {
	stderr                io.Writer
	openBrowser           func(string) error
	startCallbackListener func() (oauthCallbackListener, error)
	runCallbackPage       func(context.Context, authCallbackPageData, func(context.Context) (authui.OAuthBrowserCallback, error), func(context.Context, string) error) (authCallbackPageResult, error)
	pickMethod            func(authInteraction) (authMethodPickerResult, error)
}

func newInteractiveAuthInteractor() authInteractor {
	return &interactiveAuthInteractor{
		stderr:      os.Stderr,
		openBrowser: serverauth.OpenBrowser,
		startCallbackListener: func() (oauthCallbackListener, error) {
			return serverauth.StartOAuthCallbackListener()
		},
		runCallbackPage: runAuthCallbackPage,
	}
}

func newHeadlessAuthInteractor() authInteractor {
	return &headlessAuthInteractor{}
}

func (*interactiveAuthInteractor) isAuthInteractor() {}
func (*headlessAuthInteractor) isAuthInteractor()    {}

func (i *interactiveAuthInteractor) chooseMethod(req authInteraction) (authMethodChoice, error) {
	run := i.pickMethod
	if run == nil {
		run = runAuthMethodPicker
	}
	picked, err := run(req)
	if err != nil {
		return "", err
	}
	if picked.Canceled {
		return "", ErrAuthCanceledByUser
	}
	return picked.Choice, nil
}
