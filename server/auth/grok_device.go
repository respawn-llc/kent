package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"core/shared/config"
)

func BeginGrokDeviceFlow(ctx context.Context, client *http.Client) (DeviceCode, error) {
	client = grokOAuthHTTPClient(client)
	discovery, err := discoverGrokOAuth(ctx, client)
	if err != nil {
		return DeviceCode{}, err
	}
	values := url.Values{
		"client_id": {grokOAuthClientID}, "scope": {grokOAuthScopes}, "referrer": {config.Command},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discovery.DeviceAuthorizationEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return DeviceCode{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var response struct {
		DeviceCode              string  `json:"device_code"`
		UserCode                string  `json:"user_code"`
		VerificationURI         string  `json:"verification_uri"`
		VerificationURIComplete *string `json:"verification_uri_complete"`
		ExpiresIn               *int    `json:"expires_in"`
		Interval                *int    `json:"interval"`
	}
	if err := grokOAuthJSON(client, req, &response); err != nil {
		return DeviceCode{}, err
	}
	expiry, err := oauthExpiry(response.ExpiresIn)
	if err != nil {
		return DeviceCode{}, err
	}
	if expiry == nil || !expiry.After(time.Now()) || response.DeviceCode == "" || response.UserCode == "" {
		return DeviceCode{}, errors.New("Grok device authorization omitted code or valid expiry")
	}
	interval := 5 * time.Second // RFC 8628 default when the issuer omits interval.
	if response.Interval != nil {
		interval, err = oauthDuration(*response.Interval)
		if err != nil || interval == 0 {
			return DeviceCode{}, errors.New("Grok device authorization returned an invalid poll interval")
		}
	}
	verificationURL := response.VerificationURI
	if response.VerificationURIComplete != nil {
		verificationURL = *response.VerificationURIComplete
	}
	u, err := url.Parse(verificationURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return DeviceCode{}, errors.New("Grok device verification URL must be absolute HTTPS")
	}
	return DeviceCode{
		VerificationURL: verificationURL, UserCode: response.UserCode, Code: response.DeviceCode,
		PollInterval: interval, ExpiresAt: expiry,
	}, nil
}

func CompleteGrokDeviceFlow(ctx context.Context, client *http.Client, code DeviceCode) (OAuthMethod, error) {
	if code.Code == "" || code.ExpiresAt == nil || code.PollInterval <= 0 {
		return OAuthMethod{}, errors.New("Grok device authorization requires code, expiry, and poll interval")
	}
	ctx, cancel := context.WithDeadline(ctx, *code.ExpiresAt)
	defer cancel()
	client = grokOAuthHTTPClient(client)
	discovery, err := discoverGrokOAuth(ctx, client)
	if err != nil {
		return OAuthMethod{}, err
	}
	interval := code.PollInterval
	for {
		select {
		case <-ctx.Done():
			return OAuthMethod{}, fmt.Errorf("Grok device authorization ended: %w", ctx.Err())
		case <-time.After(interval):
		}
		token, err := grokOAuthTokenRequest(ctx, client, discovery.TokenEndpoint, url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {code.Code},
		})
		if err == nil {
			return grokCredential(token, OAuthMethod{})
		}
		var failure *GrokOAuthError
		if !errors.As(err, &failure) {
			return OAuthMethod{}, err
		}
		switch failure.Code {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		default:
			return OAuthMethod{}, err
		}
	}
}
