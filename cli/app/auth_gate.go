package app

import (
	"context"
	"io"
	"os"
	"time"

	"core/cli/app/internal/authui"
	serverauth "core/server/auth"
	sharedauth "core/shared/auth"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
)

type authInteraction struct {
	Theme   string
	FlowErr error
}

type authInteractor interface {
	authenticateRemote(context.Context, onboardingConnectionClient, config.Settings, *authpb.BootstrapStatus) error
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
	startCallbackListener func(sharedauth.CallbackTransport) (oauthCallbackListener, error)
	runCallbackPage       func(context.Context, authCallbackPageData, func(context.Context) (authui.OAuthBrowserCallback, error), func(context.Context, string) error) (authCallbackPageResult, error)
	pickMethod            func(authInteraction) (authMethodPickerResult, error)
}

func newInteractiveAuthInteractor() *interactiveAuthInteractor {
	return &interactiveAuthInteractor{
		stderr:      os.Stderr,
		openBrowser: serverauth.OpenBrowser,
		startCallbackListener: func(transport sharedauth.CallbackTransport) (oauthCallbackListener, error) {
			return serverauth.StartOAuthCallbackListener(transport)
		},
		runCallbackPage: runAuthCallbackPage,
	}
}

func newHeadlessAuthInteractor() authInteractor {
	return &headlessAuthInteractor{}
}

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
