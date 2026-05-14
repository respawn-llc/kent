package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"builder/cli/app/internal/authflowadapter"
	"builder/cli/app/internal/authinteraction"
	"builder/cli/app/internal/authoauth"
	"builder/cli/app/internal/authview"
	"builder/cli/app/internal/oauthadapter"
)

type authInteraction = authflowadapter.InteractionRequest

type authInteractor interface {
	WrapStore(base authflowadapter.Store) authflowadapter.Store
	NeedsInteraction(req authInteraction) bool
	Interact(ctx context.Context, req authInteraction) (authflowadapter.InteractionOutcome, error)
	LookupEnv(key string) string
	Interactive() bool
}

type headlessAuthInteractor struct {
	lookupEnv func(string) string
}

type oauthCallbackListener interface {
	RedirectURI() string
	Wait(ctx context.Context, timeoutSeconds time.Duration) (oauthadapter.BrowserCallback, error)
	Close() error
}

type interactiveAuthInteractor struct {
	stdin                 io.Reader
	stderr                io.Writer
	lookupEnv             func(string) string
	openBrowser           func(string) error
	startCallbackListener func() (oauthCallbackListener, error)
	runDeviceFlow         func(context.Context, oauthadapter.OpenAIOAuthOptions, func(oauthadapter.DeviceCode)) (authflowadapter.Method, error)
	runCallbackPage       func(context.Context, authCallbackPageData, func(context.Context) (oauthadapter.BrowserCallback, error), func(context.Context, string) (oauthadapter.Method, error)) (authCallbackPageResult, error)
	pickMethod            func(authInteraction) (authMethodPickerResult, error)
	pickConflict          func(authInteraction) (authConflictPickerResult, error)
	showSuccess           func(authSuccessScreenData) error
	promptReader          *bufio.Reader
}

func newInteractiveAuthInteractor() authInteractor {
	return &interactiveAuthInteractor{
		stdin:       os.Stdin,
		stderr:      os.Stderr,
		lookupEnv:   os.Getenv,
		openBrowser: oauthadapter.OpenBrowser,
		startCallbackListener: func() (oauthCallbackListener, error) {
			return oauthadapter.StartOAuthCallbackListener()
		},
		runDeviceFlow:   oauthadapter.RunOpenAIDeviceCodeFlow,
		runCallbackPage: runAuthCallbackPage,
	}
}

func newHeadlessAuthInteractor() authInteractor {
	return &headlessAuthInteractor{lookupEnv: os.Getenv}
}

func (i *interactiveAuthInteractor) WrapStore(base authflowadapter.Store) authflowadapter.Store {
	return authflowadapter.WrapStoreWithEnvAPIKeyOverride(base, i.lookupEnv)
}

func (i *headlessAuthInteractor) WrapStore(base authflowadapter.Store) authflowadapter.Store {
	return authflowadapter.WrapStoreWithEnvAPIKeyOverride(base, i.lookupEnv)
}

func (i *interactiveAuthInteractor) LookupEnv(key string) string {
	if i == nil || i.lookupEnv == nil {
		return os.Getenv(key)
	}
	return i.lookupEnv(key)
}

func (i *interactiveAuthInteractor) Interactive() bool { return true }

func (i *headlessAuthInteractor) LookupEnv(key string) string {
	if i == nil || i.lookupEnv == nil {
		return os.Getenv(key)
	}
	return i.lookupEnv(key)
}

func (i *headlessAuthInteractor) Interactive() bool { return false }

func (i *headlessAuthInteractor) NeedsInteraction(req authInteraction) bool {
	return authinteraction.HeadlessNeedsInteraction(req)
}

func (i *interactiveAuthInteractor) NeedsInteraction(req authInteraction) bool {
	return authinteraction.InteractiveNeedsInteraction(req)
}

func (i *headlessAuthInteractor) Interact(ctx context.Context, req authInteraction) (authflowadapter.InteractionOutcome, error) {
	if req.StartupErr != nil {
		return authflowadapter.InteractionOutcome{}, req.StartupErr
	}
	return authflowadapter.InteractionOutcome{}, authflowadapter.EnsureEmptyStartupReady()
}

func (i *interactiveAuthInteractor) Interact(ctx context.Context, req authInteraction) (authflowadapter.InteractionOutcome, error) {
	if authinteraction.NeedsEnvConflictResolution(req) {
		return authflowadapter.InteractionOutcome{}, i.resolveEnvAPIKeyConflict(ctx, req)
	}

	for {
		choice, err := i.chooseMethod(req)
		if err != nil {
			return authflowadapter.InteractionOutcome{}, err
		}
		req.FlowErr = nil

		var method authflowadapter.Method
		switch choice {
		case authMethodChoiceSkip:
			if err := persistSkipAuthSelection(ctx, req); err != nil {
				return authflowadapter.InteractionOutcome{}, err
			}
			return authflowadapter.InteractionOutcome{ProceedWithoutAuth: true}, nil
		case authMethodChoiceEnvAPIKey:
			if !req.HasEnvAPIKey {
				return authflowadapter.InteractionOutcome{}, errors.New("OPENAI_API_KEY is not available")
			}
			_, err = req.Manager.SetEnvAPIKeyPreference(ctx, authflowadapter.EnvAPIKeyPreferencePreferEnv, true)
			if err != nil {
				return authflowadapter.InteractionOutcome{}, fmt.Errorf("save env api key preference: %w", err)
			}
			if err := i.showAuthSuccess(ctx, req); err != nil {
				return authflowadapter.InteractionOutcome{}, err
			}
			return authflowadapter.InteractionOutcome{}, nil
		case authMethodChoiceBrowserAuto:
			method, err = i.runOAuthBrowserAuto(ctx, req.OAuthOptions, req.Theme)
		case authMethodChoiceBrowserPaste:
			method, err = i.runOAuthBrowserPaste(ctx, req.OAuthOptions, req.Theme)
		case authMethodChoiceDevice:
			method, err = i.authOAuthRunner(req.Theme).Device(ctx, req.OAuthOptions)
		default:
			return authflowadapter.InteractionOutcome{}, fmt.Errorf("unknown auth method %q", choice)
		}
		if err != nil {
			req.FlowErr = err
			continue
		}
		preference := req.State.EnvAPIKeyPreference
		setPreference := false
		if req.HasEnvAPIKey && preference == authflowadapter.EnvAPIKeyPreferenceUnspecified {
			preference = authflowadapter.EnvAPIKeyPreferencePreferSaved
			setPreference = true
		}
		if _, err := req.Manager.SwitchMethodAndSetEnvAPIKeyPreference(ctx, method, preference, setPreference, true); err != nil {
			return authflowadapter.InteractionOutcome{}, fmt.Errorf("save auth method: %w", err)
		}
		if err := i.showAuthSuccess(ctx, req); err != nil {
			return authflowadapter.InteractionOutcome{}, err
		}
		return authflowadapter.InteractionOutcome{}, nil
	}
}

func persistSkipAuthSelection(ctx context.Context, req authInteraction) error {
	if _, err := req.Manager.SwitchMethodAndSetEnvAPIKeyPreference(
		ctx,
		authflowadapter.Method{Type: authflowadapter.MethodNone},
		authflowadapter.EnvAPIKeyPreferencePreferSaved,
		true,
		true,
	); err != nil {
		return fmt.Errorf("save no-auth preference: %w", err)
	}
	return nil
}

func (i *interactiveAuthInteractor) resolveEnvAPIKeyConflict(ctx context.Context, req authInteraction) error {
	run := i.pickConflict
	if run == nil {
		run = runAuthConflictPicker
	}
	picked, err := run(req)
	if err != nil {
		return err
	}
	if picked.Canceled {
		return errors.New("auth canceled by user")
	}
	preference := authflowadapter.EnvAPIKeyPreferencePreferSaved
	if picked.Choice == authConflictChoiceEnvAPIKey {
		preference = authflowadapter.EnvAPIKeyPreferencePreferEnv
	}
	if _, err := req.Manager.SetEnvAPIKeyPreference(ctx, preference, true); err != nil {
		return fmt.Errorf("save env api key preference: %w", err)
	}
	return nil
}

func (i *interactiveAuthInteractor) showAuthSuccess(ctx context.Context, req authInteraction) error {
	run := i.showSuccess
	if run == nil {
		run = runAuthSuccessScreen
	}
	state, err := req.Manager.Load(ctx)
	if err != nil {
		return fmt.Errorf("load auth state for success screen: %w", err)
	}
	return run(authSuccessScreenData{
		Theme: req.Theme,
		Title: authview.SuccessTitle(state.Method),
	})
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

func (i *interactiveAuthInteractor) runOAuthBrowserAuto(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions, theme string) (authflowadapter.Method, error) {
	return i.authOAuthRunner(theme).BrowserAuto(ctx, opts)
}

func (i *interactiveAuthInteractor) runOAuthBrowserPaste(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions, theme string) (authflowadapter.Method, error) {
	return i.authOAuthRunner(theme).BrowserPaste(ctx, opts)
}

func (i *interactiveAuthInteractor) authOAuthRunner(theme string) authoauth.Runner {
	runDeviceFlow := i.runDeviceFlow
	if runDeviceFlow == nil {
		runDeviceFlow = oauthadapter.RunOpenAIDeviceCodeFlow
	}
	openBrowser := i.openBrowser
	if openBrowser == nil {
		openBrowser = oauthadapter.OpenBrowser
	}
	startListener := i.startCallbackListener
	if startListener == nil {
		startListener = func() (oauthCallbackListener, error) {
			return oauthadapter.StartOAuthCallbackListener()
		}
	}
	runner := authoauth.Runner{
		OpenBrowser: openBrowser,
		StartCallbackListener: func() (authoauth.CallbackListener, error) {
			return startListener()
		},
		RunDeviceFlow: func(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions, onCode func(oauthadapter.DeviceCode)) (oauthadapter.Method, error) {
			return runDeviceFlow(ctx, opts, onCode)
		},
		Prompt: func(label string) (string, error) {
			return i.prompt(authPromptStyle(theme).Render(label))
		},
		Presenter: interactiveAuthOAuthPresenter{interactor: i, theme: theme},
	}
	if i.runCallbackPage != nil {
		runner.BrowserCallbackPage = func(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions, session oauthadapter.BrowserAuthSession, openErr error, listener authoauth.CallbackListener, complete authoauth.CompleteBrowserFlowFunc) (oauthadapter.Method, error) {
			return i.runAuthBrowserHybridPage(ctx, theme, opts, session, openErr, listener, complete)
		}
	}
	return runner
}
