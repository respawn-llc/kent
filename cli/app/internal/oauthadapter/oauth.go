package oauthadapter

import (
	"context"

	"builder/server/auth"
)

type OpenAIOAuthOptions = auth.OpenAIOAuthOptions
type DeviceCode = auth.DeviceCode
type BrowserCallback = auth.BrowserCallback
type BrowserAuthSession = auth.BrowserAuthSession
type DeviceAuthorizationGrant = auth.DeviceAuthorizationGrant
type Method = auth.Method

func OpenBrowser(url string) error {
	return auth.OpenBrowser(url)
}

func StartOAuthCallbackListener() (*auth.OAuthCallbackListener, error) {
	return auth.StartOAuthCallbackListener()
}

func RunOpenAIDeviceCodeFlow(ctx context.Context, opts OpenAIOAuthOptions, onCode func(DeviceCode)) (Method, error) {
	return auth.RunOpenAIDeviceCodeFlow(ctx, opts, onCode)
}

func BeginOpenAIBrowserFlow(opts OpenAIOAuthOptions, redirectURI string) (BrowserAuthSession, error) {
	return auth.BeginOpenAIBrowserFlow(opts, redirectURI)
}

func CompleteOpenAIBrowserFlow(ctx context.Context, opts OpenAIOAuthOptions, session BrowserAuthSession, callbackInput string) (Method, error) {
	return auth.CompleteOpenAIBrowserFlow(ctx, opts, session, callbackInput)
}

func CollectOpenAIDeviceAuthorizationGrant(ctx context.Context, opts OpenAIOAuthOptions, onCode func(DeviceCode)) (DeviceAuthorizationGrant, error) {
	return auth.CollectOpenAIDeviceAuthorizationGrant(ctx, opts, onCode)
}
