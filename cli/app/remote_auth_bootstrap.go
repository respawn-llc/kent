package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"core/cli/app/internal/authui"
	serverauth "core/server/auth"
	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/serverapi"

	"github.com/charmbracelet/lipgloss"
)

var (
	ErrAuthCanceledByUser = errors.New("auth canceled by user")
	ErrOAuthStateMismatch = errors.New("oauth state mismatch")
)

func ensureRemoteAuthReady(ctx context.Context, remote onboardingConnectionClient, settings config.Settings, interactor authInteractor) error {
	if remote == nil {
		return errors.New("auth bootstrap client is required")
	}
	if settings.Connection == nil {
		return nil
	}
	target := protoapi.ExistingConnectionTarget(*settings.Connection)
	status, err := remote.GetBootstrapStatus(ctx, &authpb.GetBootstrapStatusRequest{Target: target})
	if err != nil {
		return err
	}
	if status.AuthReady {
		return nil
	}
	if interactor == nil {
		return serverapi.ErrServerAuthRequired
	}
	return interactor.authenticateRemote(ctx, remote, settings, status)
}

func (*headlessAuthInteractor) authenticateRemote(ctx context.Context, remote onboardingConnectionClient, settings config.Settings, status *authpb.BootstrapStatus) error {
	if !status.AuthRequired {
		return nil
	}
	if status.Method != authpb.AuthMethod_AUTH_METHOD_API_KEY {
		return fmt.Errorf("connection %s requires sign-in: %w", status.ConnectionId, serverapi.ErrServerAuthRequired)
	}
	resp, err := remote.CompleteBootstrap(ctx, &authpb.CompleteBootstrapRequest{
		Mode:   authpb.BootstrapMode_BOOTSTRAP_MODE_API_KEY,
		Target: protoapi.ExistingConnectionTarget(config.ConnectionID(status.ConnectionId)),
	})
	if err != nil {
		return err
	}
	if !resp.AuthReady {
		return serverapi.ErrServerAuthRequired
	}
	return nil
}

func (i *interactiveAuthInteractor) authenticateRemote(ctx context.Context, remote onboardingConnectionClient, settings config.Settings, status *authpb.BootstrapStatus) error {
	if status.Method == authpb.AuthMethod_AUTH_METHOD_API_KEY {
		catalog, err := remote.GetConnections(ctx, &authpb.GetConnectionsRequest{})
		if err != nil {
			return err
		}
		for _, value := range catalog.Connections {
			if value.Id == status.ConnectionId {
				id, definition, err := protoapi.ConnectionFromProto(value)
				if err != nil {
					return err
				}
				return editConnectionReference(ctx, remote, string(settings.Theme), id, definition)
			}
		}
		return &config.ConnectionReferenceError{Connection: settings.Connection}
	}
	return i.completeRemoteAuthBootstrap(ctx, remote, settings, protoapi.ExistingConnectionTarget(*settings.Connection), status, false)
}

func (i *interactiveAuthInteractor) completeRemoteAuthBootstrap(ctx context.Context, remote apicontract.AuthBootstrapService, settings config.Settings, target *authpb.ConnectionTarget, status *authpb.BootstrapStatus, force bool) error {
	if i == nil {
		return errors.New("interactive auth interactor is required")
	}
	req := authInteraction{
		Theme: string(settings.Theme),
	}
	for {
		choice, err := i.chooseMethod(req)
		if err != nil {
			return err
		}
		completeReq, err := i.collectRemoteBootstrapRequest(ctx, remote, target, req.Theme, choice, status)
		if err != nil {
			if errors.Is(err, ErrAuthCanceledByUser) {
				return err
			}
			log.Printf("collect connection sign-in: %v", err)
			req.FlowErr = err
			continue
		}
		completeReq.Force = force
		completeReq.Target = target
		resp, err := runConnectionOperation(ctx, req.Theme, func() (*authpb.BootstrapCompletion, error) {
			return remote.CompleteBootstrap(ctx, completeReq)
		})
		if err != nil {
			if errors.Is(err, ErrAuthCanceledByUser) {
				return err
			}
			log.Printf("complete connection sign-in: %v", err)
			req.FlowErr = err
			continue
		}
		if resp.Method == authpb.AuthMethod_AUTH_METHOD_NONE {
			i.printAuthSection(req.Theme, "Connection", []string{fmt.Sprintf("%s does not require sign-in.", resp.ConnectionId)})
			return nil
		}
		if !resp.AuthReady {
			req.FlowErr = serverapi.ErrServerAuthRequired
			continue
		}
		i.printAuthSection(req.Theme, "Connection signed in", []string{lipgloss.NewStyle().Foreground(uiPalette(req.Theme).muted).Faint(true).Render(resp.ConnectionId)})
		return nil
	}
}

func signInConnection(ctx context.Context, remote apicontract.AuthBootstrapService, selectedTheme string, target *authpb.ConnectionTarget, force bool) error {
	status, err := runConnectionOperation(ctx, selectedTheme, func() (*authpb.BootstrapStatus, error) {
		return remote.GetBootstrapStatus(ctx, &authpb.GetBootstrapStatusRequest{Target: target})
	})
	if err != nil {
		return err
	}
	if status.AuthReady && !force {
		return nil
	}
	interactor := newInteractiveAuthInteractor()
	return interactor.completeRemoteAuthBootstrap(ctx, remote, config.Settings{Theme: selectedTheme}, target, status, force)
}

func (i *interactiveAuthInteractor) collectRemoteBootstrapRequest(ctx context.Context, remote apicontract.AuthBootstrapService, target *authpb.ConnectionTarget, theme string, choice authMethodChoice, status *authpb.BootstrapStatus) (*authpb.CompleteBootstrapRequest, error) {
	if status == nil {
		return nil, errors.New("auth bootstrap status is required")
	}
	if !supportsBootstrapMode(status.SupportedModes, choice) {
		return nil, fmt.Errorf("auth method %q is not supported by this server", choice)
	}
	switch choice {
	case authMethodChoiceBrowserAuto:
		return i.collectRemoteBrowserAuto(ctx, remote, target, theme)
	case authMethodChoiceDevice:
		return i.collectRemoteDevice(ctx, remote, target, theme)
	default:
		return nil, fmt.Errorf("unsupported auth method %q", choice)
	}
}

func (i *interactiveAuthInteractor) collectRemoteBrowserAuto(ctx context.Context, remote apicontract.AuthBootstrapService, target *authpb.ConnectionTarget, theme string) (*authpb.CompleteBootstrapRequest, error) {
	startListener := i.startCallbackListener
	if startListener == nil {
		startListener = func() (oauthCallbackListener, error) {
			return serverauth.StartOAuthCallbackListener()
		}
	}
	openBrowser := i.openBrowser
	if openBrowser == nil {
		openBrowser = serverauth.OpenBrowser
	}
	listener, err := startListener()
	if err != nil {
		return nil, err
	}
	defer func() { _ = listener.Close() }()
	redirect := listener.RedirectURI()
	start, err := runConnectionOperation(ctx, theme, func() (*authpb.BootstrapStart, error) {
		return remote.StartBootstrap(ctx, &authpb.StartBootstrapRequest{
			Target: target, Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL, RedirectUri: &redirect,
		})
	})
	if err != nil {
		return nil, err
	}
	session := start.GetContinuation().GetBrowser()
	if session == nil || start.GetAuthorizationUrl() == "" {
		return nil, errors.New("server returned no browser sign-in instructions")
	}
	openErr := openBrowser(start.GetAuthorizationUrl())
	runPage := i.runCallbackPage
	if runPage == nil {
		runPage = runAuthCallbackPage
	}
	result, err := runPage(ctx, authCallbackPageData{
		Theme:        theme,
		AuthorizeURL: start.GetAuthorizationUrl(),
		OpenErr:      openErr,
	}, func(waitCtx context.Context) (authui.OAuthBrowserCallback, error) {
		return listener.Wait(waitCtx, 0)
	}, func(_ context.Context, input string) error {
		parsed, err := serverauth.ParseOAuthCallbackInput(input)
		if err != nil {
			return err
		}
		sessionState := strings.TrimSpace(session.State)
		parsedState := strings.TrimSpace(parsed.State)
		if sessionState != "" && parsedState != "" && parsedState != sessionState {
			return ErrOAuthStateMismatch
		}
		if strings.TrimSpace(parsed.Code) == "" {
			return errors.New("oauth callback is missing code")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Canceled {
		return nil, ErrAuthCanceledByUser
	}
	if result.Err != nil {
		return nil, result.Err
	}
	return &authpb.CompleteBootstrapRequest{
		Mode:          authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
		CallbackInput: &result.CallbackInput,
		Continuation:  start.Continuation,
	}, nil
}

func (i *interactiveAuthInteractor) collectRemoteDevice(ctx context.Context, remote apicontract.AuthBootstrapService, target *authpb.ConnectionTarget, theme string) (*authpb.CompleteBootstrapRequest, error) {
	start, err := runConnectionOperation(ctx, theme, func() (*authpb.BootstrapStart, error) {
		return remote.StartBootstrap(ctx, &authpb.StartBootstrapRequest{
			Target: target, Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
		})
	})
	if err != nil {
		return nil, err
	}
	code := start.GetDevice()
	if code == nil || start.GetContinuation().GetDevice() == nil {
		return nil, errors.New("server returned no device sign-in instructions")
	}
	i.printAuthSection(theme, authMethodDisplayTitle(authMethodChoiceDevice), []string{
		lipgloss.NewStyle().Foreground(uiPalette(theme).primary).Underline(true).Render(code.VerificationUrl),
		lipgloss.NewStyle().Foreground(uiPalette(theme).foreground).Render("Code: ") + lipgloss.NewStyle().Foreground(uiPalette(theme).secondary).Bold(true).Render(code.UserCode),
		lipgloss.NewStyle().Foreground(uiPalette(theme).muted).Faint(true).Render("Waiting for authorization..."),
	})
	return &authpb.CompleteBootstrapRequest{
		Mode:         authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
		Continuation: start.Continuation,
	}, nil
}

func supportsBootstrapMode(modes []authpb.BootstrapMode, choice authMethodChoice) bool {
	need := authpb.BootstrapMode_BOOTSTRAP_MODE_UNSPECIFIED
	switch choice {
	case authMethodChoiceBrowserAuto:
		need = authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL
	case authMethodChoiceDevice:
		need = authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE
	default:
		return false
	}
	for _, mode := range modes {
		if mode == need {
			return true
		}
	}
	return false
}
