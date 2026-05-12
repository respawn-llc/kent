package oauthadapter

import (
	"context"

	"builder/cli/app/internal/serverbridge"
	"builder/shared/auth"
)

type OpenAIOAuthOptions = serverbridge.OAuthOptions
type DeviceCode = serverbridge.DeviceCode
type BrowserCallback = serverbridge.BrowserCallback
type BrowserAuthSession = serverbridge.BrowserAuthSession
type DeviceAuthorizationGrant = serverbridge.DeviceAuthorizationGrant
type Method = auth.Method

func OpenBrowser(url string) error {
	return serverbridge.OpenBrowser(url)
}

func StartOAuthCallbackListener() (*serverbridge.OAuthCallbackListener, error) {
	return serverbridge.StartOAuthCallbackListener()
}

func RunOpenAIDeviceCodeFlow(ctx context.Context, opts OpenAIOAuthOptions, onCode func(DeviceCode)) (Method, error) {
	return serverbridge.RunOpenAIDeviceCodeFlow(ctx, opts, onCode)
}

func BeginOpenAIBrowserFlow(opts OpenAIOAuthOptions, redirectURI string) (BrowserAuthSession, error) {
	return serverbridge.BeginOpenAIBrowserFlow(opts, redirectURI)
}

func CompleteOpenAIBrowserFlow(ctx context.Context, opts OpenAIOAuthOptions, session BrowserAuthSession, callbackInput string) (Method, error) {
	return serverbridge.CompleteOpenAIBrowserFlow(ctx, opts, session, callbackInput)
}

func CollectOpenAIDeviceAuthorizationGrant(ctx context.Context, opts OpenAIOAuthOptions, onCode func(DeviceCode)) (DeviceAuthorizationGrant, error) {
	return serverbridge.CollectOpenAIDeviceAuthorizationGrant(ctx, opts, onCode)
}
