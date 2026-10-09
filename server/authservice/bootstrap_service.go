package authservice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"core/server/auth"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/textutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type BootstrapService struct {
	ctx          context.Context
	writeMu      sync.Mutex
	finishMu     sync.Mutex
	pending      atomic.Pointer[pendingConnection]
	connections  *ConnectionResolver
	oauthOptions auth.OpenAIOAuthOptions
}

func NewBootstrapService(ctx context.Context, connections *ConnectionResolver, oauthOptions auth.OpenAIOAuthOptions) *BootstrapService {
	return &BootstrapService{ctx: ctx, connections: connections, oauthOptions: oauthOptions}
}

func (s *BootstrapService) GetBootstrapStatus(ctx context.Context, req *authpb.GetBootstrapStatusRequest) (*authpb.BootstrapStatus, error) {
	if req == nil {
		return nil, errors.New("connection bootstrap status request is required")
	}
	snapshot, err := s.targetSnapshot(ctx, req.Target)
	if err != nil {
		return nil, err
	}
	method := connectionAuthMethod(snapshot.connection.Definition)
	status := &authpb.BootstrapStatus{
		ConnectionId: string(snapshot.connection.ID), Method: method,
		AuthReady: snapshot.failure == nil, AuthRequired: method != authpb.AuthMethod_AUTH_METHOD_NONE,
	}
	switch method {
	case authpb.AuthMethod_AUTH_METHOD_OAUTH:
		status.SupportedModes = []authpb.BootstrapMode{
			authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL,
			authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_CODE,
			authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
		}
		transport, err := auth.CallbackTransport(snapshot.connection.Definition.Protocol)
		if err != nil {
			return nil, err
		}
		status.CallbackTransport = protoapi.CallbackTransportToProto(transport)
	case authpb.AuthMethod_AUTH_METHOD_API_KEY:
		status.SupportedModes = []authpb.BootstrapMode{authpb.BootstrapMode_BOOTSTRAP_MODE_API_KEY}
	case authpb.AuthMethod_AUTH_METHOD_NONE:
		status.SupportedModes = []authpb.BootstrapMode{authpb.BootstrapMode_BOOTSTRAP_MODE_NONE}
	}
	return status, nil
}

func (s *BootstrapService) StartBootstrap(_ context.Context, req *authpb.StartBootstrapRequest) (*authpb.BootstrapStart, error) {
	if req == nil {
		return nil, errors.New("connection bootstrap start request is required")
	}
	snapshot, err := s.targetSnapshot(s.ctx, req.Target)
	if err != nil {
		return nil, err
	}
	protocol := snapshot.connection.Definition.Protocol
	if !protocol.IsSubscription() {
		return nil, auth.ErrInvalidAuthMethod
	}
	result := &authpb.BootstrapStart{Continuation: &authpb.BootstrapContinuation{
		Protocol: protoapi.ConnectionToProto(snapshot.connection.ID, snapshot.connection.Definition).Protocol,
	}}
	switch req.Mode {
	case authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL, authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_CODE:
		if err := auth.ValidateCallbackRedirect(protocol, req.GetRedirectUri()); err != nil {
			return nil, err
		}
		var browser auth.BrowserAuthSession
		if protocol == config.ConnectionChatGPT {
			browser, err = auth.BeginOpenAIBrowserFlow(s.oauthOptions, req.GetRedirectUri())
		} else {
			browser, err = auth.BeginGrokBrowserFlow(s.ctx, s.oauthOptions.HTTPClient, req.GetRedirectUri())
		}
		if err != nil {
			return nil, err
		}
		result.Instructions = &authpb.BootstrapStart_AuthorizationUrl{AuthorizationUrl: browser.AuthorizeURL}
		result.Continuation.Grant = &authpb.BootstrapContinuation_Browser{Browser: &authpb.BrowserBootstrapContinuation{
			RedirectUri: browser.RedirectURI, State: browser.State, CodeVerifier: browser.CodeVerifier,
		}}
	case authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE:
		var code auth.DeviceCode
		if protocol == config.ConnectionChatGPT {
			code, err = auth.BeginOpenAIDeviceFlow(s.ctx, s.oauthOptions)
		} else {
			code, err = auth.BeginGrokDeviceFlow(s.ctx, s.oauthOptions.HTTPClient)
		}
		if err != nil {
			return nil, err
		}
		continuation := &authpb.DeviceBootstrapContinuation{
			Code: code.Code, UserCode: code.UserCode, PollIntervalSeconds: int64(code.PollInterval / time.Second),
		}
		if code.ExpiresAt != nil {
			continuation.ExpiresAt = timestamppb.New(*code.ExpiresAt)
		}
		result.Continuation.Grant = &authpb.BootstrapContinuation_Device{Device: continuation}
		result.Instructions = &authpb.BootstrapStart_Device{Device: &authpb.BootstrapDeviceInstructions{
			VerificationUrl: code.VerificationURL, UserCode: code.UserCode,
		}}
	default:
		return nil, auth.ErrInvalidAuthMethod
	}
	return result, nil
}

func (s *BootstrapService) CompleteBootstrap(_ context.Context, req *authpb.CompleteBootstrapRequest) (*authpb.BootstrapCompletion, error) {
	if req == nil {
		return nil, errors.New("connection bootstrap request is required")
	}
	ctx := s.ctx
	snapshot, err := s.targetSnapshot(ctx, req.Target)
	if err != nil {
		return nil, err
	}
	method := connectionAuthMethod(snapshot.connection.Definition)
	if method == authpb.AuthMethod_AUTH_METHOD_OAUTH && (snapshot.oauth == nil || req.Force) {
		if s.connections.manager == nil {
			return nil, auth.ErrAuthNotConfigured
		}
		protocol := snapshot.connection.Definition.Protocol
		if req.Continuation == nil || req.Continuation.Protocol != protoapi.ConnectionToProto(snapshot.connection.ID, snapshot.connection.Definition).Protocol {
			return nil, errors.New("OAuth continuation does not match the selected connection protocol")
		}
		var credential auth.OAuthMethod
		switch req.Mode {
		case authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL, authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_CODE:
			browser := req.Continuation.GetBrowser()
			if browser == nil {
				return nil, auth.ErrInvalidAuthMethod
			}
			if err := auth.ValidateCallbackRedirect(protocol, browser.RedirectUri); err != nil {
				return nil, err
			}
			session := auth.BrowserAuthSession{RedirectURI: browser.RedirectUri, State: browser.State, CodeVerifier: browser.CodeVerifier}
			if protocol == config.ConnectionChatGPT {
				credential, err = auth.CompleteOpenAIBrowserFlow(ctx, s.oauthOptions, session, req.GetCallbackInput())
			} else {
				credential, err = auth.CompleteGrokBrowserFlow(ctx, s.oauthOptions.HTTPClient, session, req.GetCallbackInput())
			}
		case authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE:
			device := req.Continuation.GetDevice()
			if device == nil || device.PollIntervalSeconds <= 0 || device.PollIntervalSeconds > int64((1<<63-1)/time.Second) {
				return nil, auth.ErrInvalidAuthMethod
			}
			code := auth.DeviceCode{Code: device.Code, UserCode: device.UserCode, PollInterval: time.Duration(device.PollIntervalSeconds) * time.Second}
			if device.ExpiresAt != nil {
				if err := device.ExpiresAt.CheckValid(); err != nil {
					return nil, err
				}
				code.ExpiresAt = textutil.Value(device.ExpiresAt.AsTime())
			}
			if protocol == config.ConnectionChatGPT {
				credential, err = auth.CompleteOpenAIDeviceFlow(ctx, s.oauthOptions, code)
			} else {
				credential, err = auth.CompleteGrokDeviceFlow(ctx, s.oauthOptions.HTTPClient, code)
			}
		default:
			return nil, auth.ErrInvalidAuthMethod
		}
		if err != nil {
			return nil, err
		}
		if err := s.commitOAuth(req.Target, snapshot, credential); err != nil {
			return nil, err
		}
		snapshot.oauth, snapshot.failure = &credential, nil
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
	if connection.Protocol.IsSubscription() {
		return authpb.AuthMethod_AUTH_METHOD_OAUTH
	}
	if connection.EnvironmentVariable != nil {
		return authpb.AuthMethod_AUTH_METHOD_API_KEY
	}
	return authpb.AuthMethod_AUTH_METHOD_NONE
}
