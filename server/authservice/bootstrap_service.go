package authservice

import (
	"context"
	"errors"

	"core/server/auth"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/textutil"
)

type BootstrapService struct {
	connections  *ConnectionResolver
	oauthOptions auth.OpenAIOAuthOptions
}

func NewBootstrapService(connections *ConnectionResolver, oauthOptions auth.OpenAIOAuthOptions) *BootstrapService {
	return &BootstrapService{connections: connections, oauthOptions: oauthOptions}
}

func (s *BootstrapService) GetBootstrapStatus(ctx context.Context, req *authpb.GetBootstrapStatusRequest) (*authpb.BootstrapStatus, error) {
	if req == nil {
		return nil, errors.New("connection bootstrap status request is required")
	}
	snapshot, err := s.connections.snapshot(ctx, req.ConnectionId)
	if err != nil {
		return nil, err
	}
	method := connectionAuthMethod(snapshot.connection.Definition)
	status := &authpb.BootstrapStatus{
		ConnectionId: string(snapshot.connection.ID), Method: method,
		AuthReady: snapshot.failure == nil, AuthRequired: method != authpb.AuthMethod_AUTH_METHOD_NONE,
		AuthBootstrapSupported: true,
	}
	switch method {
	case authpb.AuthMethod_AUTH_METHOD_OAUTH:
		status.SupportedModes = []authpb.BootstrapMode{
			authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
			authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_CODE,
			authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
		}
		status.Oauth = &authpb.BootstrapOAuthConfig{
			Issuer: textutil.OptionalTrimmedString(s.oauthOptions.Issuer), ClientId: textutil.OptionalTrimmedString(s.oauthOptions.ClientID),
		}
	case authpb.AuthMethod_AUTH_METHOD_API_KEY:
		status.SupportedModes = []authpb.BootstrapMode{authpb.BootstrapMode_BOOTSTRAP_MODE_API_KEY}
	case authpb.AuthMethod_AUTH_METHOD_NONE:
		status.SupportedModes = []authpb.BootstrapMode{authpb.BootstrapMode_BOOTSTRAP_MODE_NONE}
	}
	return status, nil
}

func (s *BootstrapService) CompleteBootstrap(ctx context.Context, req *authpb.CompleteBootstrapRequest) (*authpb.BootstrapCompletion, error) {
	if req == nil {
		return nil, errors.New("connection bootstrap request is required")
	}
	snapshot, err := s.connections.snapshot(ctx, req.ConnectionId)
	if err != nil {
		return nil, err
	}
	method := connectionAuthMethod(snapshot.connection.Definition)
	if method == authpb.AuthMethod_AUTH_METHOD_OAUTH && (snapshot.oauth == nil || req.Force) {
		if s.connections.manager == nil {
			return nil, auth.ErrAuthNotConfigured
		}
		var credential auth.Method
		switch req.Mode {
		case authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL, authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_CODE:
			credential, err = auth.CompleteOpenAIBrowserFlow(ctx, s.oauthOptions, auth.BrowserAuthSession{
				RedirectURI: req.GetRedirectUri(), State: req.GetOauthState(), CodeVerifier: req.GetOauthCodeVerifier(),
			}, req.GetCallbackInput())
		case authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE:
			credential, err = auth.CompleteOpenAIDeviceAuthorizationGrant(ctx, s.oauthOptions, req.GetDeviceAuthorizationCode(), req.GetDeviceCodeVerifier())
		default:
			return nil, auth.ErrInvalidAuthMethod
		}
		if err != nil {
			return nil, err
		}
		if err := s.connections.manager.SaveOAuth(ctx, snapshot.connection.ID, *credential.OAuth); err != nil {
			return nil, err
		}
		snapshot.oauth, snapshot.failure = credential.OAuth, nil
	} else if method != authpb.AuthMethod_AUTH_METHOD_OAUTH {
		expected := authpb.BootstrapMode_BOOTSTRAP_MODE_NONE
		if method == authpb.AuthMethod_AUTH_METHOD_API_KEY {
			expected = authpb.BootstrapMode_BOOTSTRAP_MODE_API_KEY
		}
		if req.Mode != expected {
			return nil, auth.ErrInvalidAuthMethod
		}
	}
	if snapshot.failure != nil {
		return nil, snapshot.failure
	}
	result := &authpb.BootstrapCompletion{ConnectionId: string(snapshot.connection.ID), Method: method, AuthReady: true}
	if snapshot.oauth != nil {
		result.AccountId = textutil.OptionalTrimmedString(snapshot.oauth.AccountID)
		result.Email = textutil.OptionalTrimmedString(snapshot.oauth.Email)
	}
	return result, nil
}

func connectionAuthMethod(connection config.ProviderConnection) authpb.AuthMethod {
	if connection.Protocol == config.ConnectionChatGPT {
		return authpb.AuthMethod_AUTH_METHOD_OAUTH
	}
	if connection.EnvironmentVariable != nil {
		return authpb.AuthMethod_AUTH_METHOD_API_KEY
	}
	return authpb.AuthMethod_AUTH_METHOD_NONE
}
