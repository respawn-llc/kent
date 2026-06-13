package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"core/cli/app/internal/authflowadapter"
	"core/cli/app/internal/authinteraction"
	"core/cli/app/internal/authoauth"
	"core/cli/app/internal/authview"
	"core/cli/app/internal/oauthadapter"
	serverauth "core/server/auth"
	serverauthflow "core/server/authflow"

	"github.com/charmbracelet/lipgloss"
)

type authInteraction = authflowadapter.InteractionRequest

type authInteractor interface {
	WrapStore(base authflowadapter.Store) authflowadapter.Store
	NeedsInteraction(req authInteraction) bool
	Interact(ctx context.Context, req authInteraction) (authflowadapter.InteractionOutcome, error)
	LookupEnv(key string) string
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
		openBrowser: serverauth.OpenBrowser,
		startCallbackListener: func() (oauthCallbackListener, error) {
			return serverauth.StartOAuthCallbackListener()
		},
		runDeviceFlow:   serverauth.RunOpenAIDeviceCodeFlow,
		runCallbackPage: runAuthCallbackPage,
	}
}

func newHeadlessAuthInteractor() authInteractor {
	return &headlessAuthInteractor{lookupEnv: os.Getenv}
}

func (i *interactiveAuthInteractor) WrapStore(base authflowadapter.Store) authflowadapter.Store {
	return serverauthflow.WrapStoreWithEnvAPIKeyOverride(base, i.lookupEnv)
}

func (i *headlessAuthInteractor) WrapStore(base authflowadapter.Store) authflowadapter.Store {
	return serverauthflow.WrapStoreWithEnvAPIKeyOverride(base, i.lookupEnv)
}

func (i *interactiveAuthInteractor) LookupEnv(key string) string {
	if i == nil || i.lookupEnv == nil {
		return os.Getenv(key)
	}
	return i.lookupEnv(key)
}

func (i *headlessAuthInteractor) LookupEnv(key string) string {
	if i == nil || i.lookupEnv == nil {
		return os.Getenv(key)
	}
	return i.lookupEnv(key)
}

func (i *headlessAuthInteractor) NeedsInteraction(req authInteraction) bool {
	return req.AuthRequired && !req.Gate.Ready
}

func (i *interactiveAuthInteractor) NeedsInteraction(req authInteraction) bool {
	if !req.AuthRequired && !req.PromptOptional {
		return authinteraction.NeedsEnvConflictResolution(req)
	}
	if req.Gate.Ready {
		return authinteraction.NeedsEnvConflictResolution(req)
	}
	return req.AuthRequired || !req.State.IsNoAuthSelected() || authinteraction.NeedsEnvConflictResolution(req)
}

func (i *headlessAuthInteractor) Interact(ctx context.Context, req authInteraction) (authflowadapter.InteractionOutcome, error) {
	if req.StartupErr != nil {
		return authflowadapter.InteractionOutcome{}, req.StartupErr
	}
	return authflowadapter.InteractionOutcome{}, serverauth.EnsureStartupReady(serverauth.EmptyState())
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
			method, err = i.authOAuthRunner(req.Theme).BrowserAuto(ctx, req.OAuthOptions)
		case authMethodChoiceBrowserPaste:
			method, err = i.authOAuthRunner(req.Theme).BrowserPaste(ctx, req.OAuthOptions)
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

func (i *interactiveAuthInteractor) authOAuthRunner(theme string) authoauth.Runner {
	runDeviceFlow := i.runDeviceFlow
	if runDeviceFlow == nil {
		runDeviceFlow = serverauth.RunOpenAIDeviceCodeFlow
	}
	openBrowser := i.openBrowser
	if openBrowser == nil {
		openBrowser = serverauth.OpenBrowser
	}
	startListener := i.startCallbackListener
	if startListener == nil {
		startListener = func() (oauthCallbackListener, error) {
			return serverauth.StartOAuthCallbackListener()
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
			return i.prompt(lipgloss.NewStyle().Foreground(uiPalette(theme).primary).Bold(true).Render(label))
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
