package authbootstrap

import (
	"context"
	"strings"

	"builder/server/auth"
	"builder/server/authpolicy"
	"builder/shared/config"
	"builder/shared/serverapi"
)

type Service struct {
	manager        *auth.Manager
	oauthOptions   auth.OpenAIOAuthOptions
	authRequired   bool
	allowedPreAuth []string
	supportedModes []serverapi.AuthBootstrapMode
}

func NewService(manager *auth.Manager, oauthOptions auth.OpenAIOAuthOptions, settings config.Settings, allowedPreAuthMethods []string) *Service {
	return &Service{
		manager:        manager,
		oauthOptions:   oauthOptions,
		authRequired:   authpolicy.RequiresStartupAuth(settings),
		allowedPreAuth: append([]string(nil), allowedPreAuthMethods...),
		supportedModes: []serverapi.AuthBootstrapMode{
			serverapi.AuthBootstrapModeNone,
			serverapi.AuthBootstrapModeBrowserCallbackURL,
			serverapi.AuthBootstrapModeBrowserCallbackCode,
			serverapi.AuthBootstrapModeDeviceCode,
			serverapi.AuthBootstrapModeAPIKey,
		},
	}
}

func (s *Service) GetBootstrapStatus(ctx context.Context, _ serverapi.AuthGetBootstrapStatusRequest) (serverapi.AuthGetBootstrapStatusResponse, error) {
	ready, err := s.authReady(ctx)
	if err != nil {
		return serverapi.AuthGetBootstrapStatusResponse{}, err
	}
	return serverapi.AuthGetBootstrapStatusResponse{
		AuthReady:              ready,
		AuthRequired:           s.authRequired,
		AuthBootstrapSupported: true,
		AllowedPreAuthMethods:  append([]string(nil), s.allowedPreAuth...),
		SupportedModes:         append([]serverapi.AuthBootstrapMode(nil), s.supportedModes...),
		OAuth: serverapi.AuthBootstrapOAuthConfig{
			Issuer:   strings.TrimSpace(s.oauthOptions.Issuer),
			ClientID: strings.TrimSpace(s.oauthOptions.ClientID),
		},
	}, nil
}

func (s *Service) CompleteBootstrap(ctx context.Context, req serverapi.AuthCompleteBootstrapRequest) (serverapi.AuthCompleteBootstrapResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.AuthCompleteBootstrapResponse{}, err
	}
	if s == nil || s.manager == nil {
		return serverapi.AuthCompleteBootstrapResponse{}, serverapi.ErrServerAuthRequired
	}
	state, err := s.manager.Load(ctx)
	if err != nil {
		return serverapi.AuthCompleteBootstrapResponse{}, err
	}
	if req.Mode == serverapi.AuthBootstrapModeNone {
		if s.authRequired {
			return serverapi.AuthCompleteBootstrapResponse{}, serverapi.ErrServerAuthRequired
		}
		state, err = s.manager.ClearMethod(ctx, true)
		if err != nil {
			return serverapi.AuthCompleteBootstrapResponse{}, err
		}
		return s.bootstrapResponseFromState(state), nil
	}
	if auth.EvaluateStartupGate(state).Ready {
		return s.bootstrapResponseFromState(state), nil
	}
	var (
		method      auth.Method
		completeErr error
	)
	switch req.Mode {
	case serverapi.AuthBootstrapModeAPIKey:
		method = auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: strings.TrimSpace(req.APIKey)}}
	case serverapi.AuthBootstrapModeBrowserCallbackURL, serverapi.AuthBootstrapModeBrowserCallbackCode:
		method, completeErr = auth.CompleteOpenAIBrowserFlow(ctx, s.oauthOptions, auth.BrowserAuthSession{
			RedirectURI:  strings.TrimSpace(req.RedirectURI),
			State:        strings.TrimSpace(req.OAuthState),
			CodeVerifier: strings.TrimSpace(req.OAuthCodeVerifier),
		}, req.CallbackInput)
	case serverapi.AuthBootstrapModeDeviceCode:
		method, completeErr = auth.CompleteOpenAIDeviceAuthorizationGrant(ctx, s.oauthOptions, strings.TrimSpace(req.DeviceAuthorizationCode), strings.TrimSpace(req.DeviceCodeVerifier))
	default:
		return serverapi.AuthCompleteBootstrapResponse{}, req.Validate()
	}
	if completeErr != nil {
		return serverapi.AuthCompleteBootstrapResponse{}, completeErr
	}
	state, err = s.manager.SwitchMethodAndSetEnvAPIKeyPreference(ctx, method, auth.EnvAPIKeyPreferencePreferSaved, true, true)
	if err != nil {
		return serverapi.AuthCompleteBootstrapResponse{}, err
	}
	return s.bootstrapResponseFromState(state), nil
}

func (s *Service) authReady(ctx context.Context) (bool, error) {
	if s == nil || s.manager == nil {
		return false, nil
	}
	state, err := s.manager.Load(ctx)
	if err != nil {
		return false, err
	}
	return auth.EvaluateStartupGate(state).Ready, nil
}

func methodAccountID(method auth.Method) string {
	if method.Type == auth.MethodOAuth && method.OAuth != nil {
		return strings.TrimSpace(method.OAuth.AccountID)
	}
	return ""
}

func methodEmail(method auth.Method) string {
	if method.Type == auth.MethodOAuth && method.OAuth != nil {
		return strings.TrimSpace(method.OAuth.Email)
	}
	return ""
}

func (s *Service) bootstrapResponseFromState(state auth.State) serverapi.AuthCompleteBootstrapResponse {
	return serverapi.AuthCompleteBootstrapResponse{
		AuthReady:  !s.authRequired || auth.EvaluateStartupGate(state).Ready,
		MethodType: strings.TrimSpace(string(state.Method.Type)),
		AccountID:  methodAccountID(state.Method),
		Email:      methodEmail(state.Method),
	}
}

var _ serverapi.AuthBootstrapService = (*Service)(nil)
