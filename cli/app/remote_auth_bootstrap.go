package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/cli/app/internal/authui"
	serverauth "core/server/auth"
	"core/shared/apicontract"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/serverapi"

	"github.com/charmbracelet/lipgloss"
)

var (
	ErrAuthCanceledByUser = errors.New("auth canceled by user")
	ErrOAuthStateMismatch = errors.New("oauth state mismatch")
)

func ensureRemoteAuthReady(ctx context.Context, remote apicontract.AuthBootstrapService, settings config.Settings, interactor authInteractor, interactive bool) error {
	if remote == nil {
		return errors.New("auth bootstrap client is required")
	}
	status, err := remote.GetBootstrapStatus(ctx, &authpb.GetBootstrapStatusRequest{ConnectionId: (*string)(settings.Connection)})
	if err != nil {
		return err
	}
	if status.AuthReady {
		return nil
	}
	if interactor == nil {
		return serverapi.ErrServerAuthRequired
	}
	if !status.AuthRequired && !interactive {
		return nil
	}
	if interactive, ok := interactor.(*interactiveAuthInteractor); ok {
		return interactive.completeRemoteAuthBootstrap(ctx, remote, settings, status, false)
	}
	if status.Method != authpb.AuthMethod_AUTH_METHOD_API_KEY {
		return fmt.Errorf("connection %s requires sign-in: %w", status.ConnectionId, serverapi.ErrServerAuthRequired)
	}
	resp, err := remote.CompleteBootstrap(ctx, &authpb.CompleteBootstrapRequest{
		Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_API_KEY, ConnectionId: &status.ConnectionId,
	})
	if err != nil {
		return err
	}
	if !resp.AuthReady {
		return serverapi.ErrServerAuthRequired
	}
	return nil
}

func (i *interactiveAuthInteractor) completeRemoteAuthBootstrap(ctx context.Context, remote apicontract.AuthBootstrapService, settings config.Settings, status *authpb.BootstrapStatus, force bool) error {
	if i == nil {
		return errors.New("interactive auth interactor is required")
	}
	req := authInteraction{
		Theme:        string(settings.Theme),
		HasEnvAPIKey: status.Method == authpb.AuthMethod_AUTH_METHOD_API_KEY,
	}
	for {
		choice, err := i.chooseMethod(req)
		if err != nil {
			return err
		}
		completeReq, err := i.collectRemoteBootstrapRequest(ctx, req.Theme, choice, status)
		if err != nil {
			req.FlowErr = err
			continue
		}
		completeReq.Force = force
		completeReq.ConnectionId = &status.ConnectionId
		resp, err := remote.CompleteBootstrap(ctx, completeReq)
		if err != nil {
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
		i.printAuthSection(req.Theme, "Server Auth Ready", []string{lipgloss.NewStyle().Foreground(uiPalette(req.Theme).muted).Faint(true).Render("Kent configured auth on the server.")})
		return nil
	}
}

func (i *interactiveAuthInteractor) collectRemoteBootstrapRequest(ctx context.Context, theme string, choice authMethodChoice, status *authpb.BootstrapStatus) (*authpb.CompleteBootstrapRequest, error) {
	if status == nil {
		return nil, errors.New("auth bootstrap status is required")
	}
	if !supportsBootstrapMode(status.SupportedModes, choice) {
		return nil, fmt.Errorf("auth method %q is not supported by this server", choice)
	}
	oauthOpts := authui.OAuthOptions{Issuer: status.GetOauth().GetIssuer(), ClientID: status.GetOauth().GetClientId()}
	switch choice {
	case authMethodChoiceSkip:
		return &authpb.CompleteBootstrapRequest{Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_NONE}, nil
	case authMethodChoiceEnvAPIKey:
		return &authpb.CompleteBootstrapRequest{Mode: authpb.BootstrapMode_BOOTSTRAP_MODE_API_KEY}, nil
	case authMethodChoiceBrowserAuto:
		return i.collectRemoteBrowserAuto(ctx, oauthOpts, theme)
	case authMethodChoiceDevice:
		return i.collectRemoteDevice(ctx, oauthOpts, theme)
	default:
		return nil, fmt.Errorf("unsupported auth method %q", choice)
	}
}

func (i *interactiveAuthInteractor) collectRemoteBrowserAuto(ctx context.Context, opts authui.OAuthOptions, theme string) (*authpb.CompleteBootstrapRequest, error) {
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
	session, err := serverauth.BeginOpenAIBrowserFlow(opts, listener.RedirectURI())
	if err != nil {
		return nil, err
	}
	openErr := openBrowser(session.AuthorizeURL)
	runPage := i.runCallbackPage
	if runPage == nil {
		runPage = runAuthCallbackPage
	}
	result, err := runPage(ctx, authCallbackPageData{
		Theme:        theme,
		AuthorizeURL: session.AuthorizeURL,
		OpenErr:      openErr,
	}, func(waitCtx context.Context) (authui.OAuthBrowserCallback, error) {
		return listener.Wait(waitCtx, opts.PollTimeout)
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
		Mode:              authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
		CallbackInput:     &result.CallbackInput,
		RedirectUri:       &session.RedirectURI,
		OauthState:        &session.State,
		OauthCodeVerifier: &session.CodeVerifier,
	}, nil
}

func (i *interactiveAuthInteractor) collectRemoteDevice(ctx context.Context, opts authui.OAuthOptions, theme string) (*authpb.CompleteBootstrapRequest, error) {
	grant, err := serverauth.CollectOpenAIDeviceAuthorizationGrant(ctx, opts, func(code authui.OAuthDeviceCode) {
		i.printAuthSection(theme, authMethodDisplayTitle(authMethodChoiceDevice), []string{
			lipgloss.NewStyle().Foreground(uiPalette(theme).primary).Underline(true).Render(code.VerificationURL),
			lipgloss.NewStyle().Foreground(uiPalette(theme).foreground).Render("Code: ") + lipgloss.NewStyle().Foreground(uiPalette(theme).secondary).Bold(true).Render(code.UserCode),
			lipgloss.NewStyle().Foreground(uiPalette(theme).muted).Faint(true).Render("Waiting for authorization..."),
		})
	})
	if err != nil {
		return nil, err
	}
	return &authpb.CompleteBootstrapRequest{
		Mode:                    authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
		DeviceAuthorizationCode: &grant.AuthorizationCode,
		DeviceCodeVerifier:      &grant.CodeVerifier,
	}, nil
}

func supportsBootstrapMode(modes []authpb.BootstrapMode, choice authMethodChoice) bool {
	need := authpb.BootstrapMode_BOOTSTRAP_MODE_UNSPECIFIED
	switch choice {
	case authMethodChoiceSkip:
		need = authpb.BootstrapMode_BOOTSTRAP_MODE_NONE
	case authMethodChoiceEnvAPIKey:
		need = authpb.BootstrapMode_BOOTSTRAP_MODE_API_KEY
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
