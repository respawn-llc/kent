package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"builder/cli/app/internal/oauthadapter"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
)

func ensureRemoteAuthReady(ctx context.Context, remote client.AuthBootstrapClient, settings config.Settings, interactor authInteractor) error {
	if remote == nil {
		return errors.New("auth bootstrap client is required")
	}
	status, err := remote.GetAuthBootstrapStatus(ctx, serverapi.AuthGetBootstrapStatusRequest{})
	if err != nil {
		return err
	}
	if status.AuthReady {
		return nil
	}
	if interactor == nil {
		return serverapi.ErrServerAuthRequired
	}
	if !status.AuthRequired && !interactor.Interactive() {
		return nil
	}
	if interactive, ok := interactor.(*interactiveAuthInteractor); ok {
		return interactive.completeRemoteAuthBootstrap(ctx, remote, settings, status)
	}
	apiKey := strings.TrimSpace(interactor.LookupEnv("OPENAI_API_KEY"))
	if apiKey == "" {
		return serverapi.ErrServerAuthRequired
	}
	resp, err := remote.CompleteAuthBootstrap(ctx, serverapi.AuthCompleteBootstrapRequest{
		Mode:   serverapi.AuthBootstrapModeAPIKey,
		APIKey: apiKey,
	})
	if err != nil {
		return err
	}
	if !resp.AuthReady {
		return serverapi.ErrServerAuthRequired
	}
	return nil
}

func (i *interactiveAuthInteractor) completeRemoteAuthBootstrap(ctx context.Context, remote client.AuthBootstrapClient, settings config.Settings, status serverapi.AuthGetBootstrapStatusResponse) error {
	if i == nil {
		return errors.New("interactive auth interactor is required")
	}
	req := authInteraction{
		Theme:          string(settings.Theme),
		AuthRequired:   status.AuthRequired,
		PromptOptional: !status.AuthRequired,
		HasEnvAPIKey:   strings.TrimSpace(i.LookupEnv("OPENAI_API_KEY")) != "",
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
		resp, err := remote.CompleteAuthBootstrap(ctx, completeReq)
		if err != nil {
			req.FlowErr = err
			continue
		}
		if !resp.AuthReady {
			req.FlowErr = serverapi.ErrServerAuthRequired
			continue
		}
		i.printAuthSection(req.Theme, "Server Auth Ready", []string{authMetaStyle(req.Theme).Render("Builder configured auth on the server.")})
		return nil
	}
}

func (i *interactiveAuthInteractor) collectRemoteBootstrapRequest(ctx context.Context, theme string, choice authMethodChoice, status serverapi.AuthGetBootstrapStatusResponse) (serverapi.AuthCompleteBootstrapRequest, error) {
	if !supportsBootstrapMode(status.SupportedModes, choice) {
		return serverapi.AuthCompleteBootstrapRequest{}, fmt.Errorf("auth method %q is not supported by this server", choice)
	}
	oauthOpts := oauthadapter.OpenAIOAuthOptions{Issuer: status.OAuth.Issuer, ClientID: status.OAuth.ClientID}
	switch choice {
	case authMethodChoiceSkip:
		return serverapi.AuthCompleteBootstrapRequest{Mode: serverapi.AuthBootstrapModeNone}, nil
	case authMethodChoiceEnvAPIKey:
		apiKey := strings.TrimSpace(i.LookupEnv("OPENAI_API_KEY"))
		if apiKey == "" {
			return serverapi.AuthCompleteBootstrapRequest{}, errors.New("OPENAI_API_KEY is not available")
		}
		return serverapi.AuthCompleteBootstrapRequest{Mode: serverapi.AuthBootstrapModeAPIKey, APIKey: apiKey}, nil
	case authMethodChoiceBrowserAuto:
		return i.collectRemoteBrowserAuto(ctx, oauthOpts, theme)
	case authMethodChoiceBrowserPaste:
		return i.collectRemoteBrowserPaste(ctx, oauthOpts, theme)
	case authMethodChoiceDevice:
		return i.collectRemoteDevice(ctx, oauthOpts, theme)
	default:
		return serverapi.AuthCompleteBootstrapRequest{}, fmt.Errorf("unsupported auth method %q", choice)
	}
}

func (i *interactiveAuthInteractor) collectRemoteBrowserAuto(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions, theme string) (serverapi.AuthCompleteBootstrapRequest, error) {
	listener, err := i.startCallbackListener()
	if err != nil {
		return serverapi.AuthCompleteBootstrapRequest{}, err
	}
	defer func() { _ = listener.Close() }()
	session, err := oauthadapter.BeginOpenAIBrowserFlow(opts, listener.RedirectURI())
	if err != nil {
		return serverapi.AuthCompleteBootstrapRequest{}, err
	}
	lines := []string{authURLStyle(theme).Render(session.AuthorizeURL)}
	if err := i.openBrowser(session.AuthorizeURL); err != nil {
		lines = append(lines, authMetaStyle(theme).Render(fmt.Sprintf("Builder could not open your browser automatically (%v). Open the URL manually.", err)))
	} else {
		lines = append(lines, authMetaStyle(theme).Render("Builder opened your default browser. If nothing appeared, open the URL manually."))
	}
	lines = append(lines, authMetaStyle(theme).Render("Waiting for browser callback..."))
	i.printAuthSection(theme, authMethodDisplayTitle(authMethodChoiceBrowserAuto), lines)
	callback, err := listener.Wait(ctx, opts.PollTimeout)
	if err != nil {
		return serverapi.AuthCompleteBootstrapRequest{}, err
	}
	query := url.Values{"code": []string{callback.Code}, "state": []string{callback.State}}
	return serverapi.AuthCompleteBootstrapRequest{
		Mode:              serverapi.AuthBootstrapModeBrowserCallbackURL,
		CallbackInput:     query.Encode(),
		RedirectURI:       session.RedirectURI,
		OAuthState:        session.State,
		OAuthCodeVerifier: session.CodeVerifier,
	}, nil
}

func (i *interactiveAuthInteractor) collectRemoteBrowserPaste(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions, theme string) (serverapi.AuthCompleteBootstrapRequest, error) {
	session, err := oauthadapter.BeginOpenAIBrowserFlow(opts, "")
	if err != nil {
		return serverapi.AuthCompleteBootstrapRequest{}, err
	}
	lines := []string{authURLStyle(theme).Render(session.AuthorizeURL)}
	if err := i.openBrowser(session.AuthorizeURL); err != nil {
		lines = append(lines, authMetaStyle(theme).Render(fmt.Sprintf("Builder could not open your browser automatically (%v). Open the URL manually.", err)))
	} else {
		lines = append(lines, authMetaStyle(theme).Render("Builder opened your default browser. If nothing appeared, open the URL manually."))
	}
	lines = append(lines, authMetaStyle(theme).Render("After sign-in, paste the full callback URL or just the code below."))
	i.printAuthSection(theme, authMethodDisplayTitle(authMethodChoiceBrowserPaste), lines)
	callbackInput, err := i.prompt(authPromptStyle(theme).Render("Paste callback URL or code: "))
	if err != nil {
		return serverapi.AuthCompleteBootstrapRequest{}, err
	}
	return serverapi.AuthCompleteBootstrapRequest{
		Mode:              serverapi.AuthBootstrapModeBrowserCallbackCode,
		CallbackInput:     callbackInput,
		RedirectURI:       session.RedirectURI,
		OAuthState:        session.State,
		OAuthCodeVerifier: session.CodeVerifier,
	}, nil
}

func (i *interactiveAuthInteractor) collectRemoteDevice(ctx context.Context, opts oauthadapter.OpenAIOAuthOptions, theme string) (serverapi.AuthCompleteBootstrapRequest, error) {
	grant, err := oauthadapter.CollectOpenAIDeviceAuthorizationGrant(ctx, opts, func(code oauthadapter.DeviceCode) {
		i.printAuthSection(theme, authMethodDisplayTitle(authMethodChoiceDevice), []string{
			authURLStyle(theme).Render(code.VerificationURL),
			authBodyStyle(theme).Render("Code: ") + authCodeStyle(theme).Render(code.UserCode),
			authMetaStyle(theme).Render("Waiting for authorization..."),
		})
	})
	if err != nil {
		return serverapi.AuthCompleteBootstrapRequest{}, err
	}
	return serverapi.AuthCompleteBootstrapRequest{
		Mode:                    serverapi.AuthBootstrapModeDeviceCode,
		DeviceAuthorizationCode: grant.AuthorizationCode,
		DeviceCodeVerifier:      grant.CodeVerifier,
	}, nil
}

func supportsBootstrapMode(modes []serverapi.AuthBootstrapMode, choice authMethodChoice) bool {
	need := serverapi.AuthBootstrapMode("")
	switch choice {
	case authMethodChoiceSkip:
		need = serverapi.AuthBootstrapModeNone
	case authMethodChoiceEnvAPIKey:
		need = serverapi.AuthBootstrapModeAPIKey
	case authMethodChoiceBrowserAuto:
		need = serverapi.AuthBootstrapModeBrowserCallbackURL
	case authMethodChoiceBrowserPaste:
		need = serverapi.AuthBootstrapModeBrowserCallbackCode
	case authMethodChoiceDevice:
		need = serverapi.AuthBootstrapModeDeviceCode
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
