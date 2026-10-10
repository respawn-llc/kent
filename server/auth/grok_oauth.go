package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"core/server/httpcompression"
)

const (
	grokOAuthIssuer   = "https://auth.x.ai"
	grokOAuthClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	grokOAuthScopes   = "openid profile email offline_access grok-cli:access api:access"
)

type grokDiscovery struct {
	Issuer                      string `json:"issuer"`
	TokenEndpoint               string `json:"token_endpoint"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}

// GrokOAuthError retains the issuer's structured failure, including device
// polling outcomes, without interpreting human-readable diagnostics.
type GrokOAuthError struct {
	StatusCode  int
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *GrokOAuthError) Error() string {
	return fmt.Sprintf("Grok OAuth: HTTP %d, %s: %s", e.StatusCode, e.Code, e.Description)
}

func grokOAuthHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return httpcompression.NewClient(&http.Client{Timeout: 30 * time.Second})
}

func discoverGrokOAuth(ctx context.Context, client *http.Client) (grokDiscovery, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, grokOAuthIssuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return grokDiscovery{}, err
	}
	var discovery grokDiscovery
	if err := grokOAuthJSON(client, req, &discovery); err != nil {
		return grokDiscovery{}, err
	}
	if discovery.Issuer != grokOAuthIssuer {
		return grokDiscovery{}, errors.New("Grok OAuth discovery returned an unexpected issuer")
	}
	for _, endpoint := range []string{discovery.TokenEndpoint, discovery.DeviceAuthorizationEndpoint} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return grokDiscovery{}, errors.New("Grok OAuth discovery requires absolute HTTPS endpoints")
		}
	}
	return discovery, nil
}

func grokOAuthJSON(client *http.Client, req *http.Request, target any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		failure := &GrokOAuthError{StatusCode: resp.StatusCode}
		if err := json.NewDecoder(resp.Body).Decode(failure); err != nil {
			return fmt.Errorf("Grok OAuth HTTP %d: decode failure: %w", resp.StatusCode, err)
		}
		return failure
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode Grok OAuth response: %w", err)
	}
	return nil
}

func grokOAuthTokenRequest(ctx context.Context, client *http.Client, endpoint string, values url.Values) (oauthTokenResponse, error) {
	values.Set("client_id", grokOAuthClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return oauthTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token oauthTokenResponse
	if err := grokOAuthJSON(client, req, &token); err != nil {
		return oauthTokenResponse{}, err
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return oauthTokenResponse{}, errors.New("Grok OAuth response omitted access token")
	}
	return token, nil
}

func RefreshGrokAuthToken(ctx context.Context, client *http.Client, method OAuthMethod) (OAuthMethod, error) {
	if err := method.Validate(); err != nil {
		return OAuthMethod{}, err
	}
	if strings.TrimSpace(method.RefreshToken) == "" {
		return OAuthMethod{}, fmt.Errorf("%w: missing Grok refresh token", ErrOAuthRefreshFailed)
	}
	client = grokOAuthHTTPClient(client)
	discovery, err := discoverGrokOAuth(ctx, client)
	if err != nil {
		return OAuthMethod{}, fmt.Errorf("%w: %w", ErrOAuthRefreshFailed, err)
	}
	token, err := grokOAuthTokenRequest(ctx, client, discovery.TokenEndpoint, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {method.RefreshToken},
	})
	if err != nil {
		return OAuthMethod{}, fmt.Errorf("%w: %w", ErrOAuthRefreshFailed, err)
	}
	method, err = grokCredential(token, method)
	if err != nil {
		return OAuthMethod{}, fmt.Errorf("%w: %w", ErrOAuthRefreshFailed, err)
	}
	return method, nil
}

func grokCredential(token oauthTokenResponse, method OAuthMethod) (OAuthMethod, error) {
	method.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		method.RefreshToken = token.RefreshToken
	}
	expiry, err := oauthExpiry(token.ExpiresIn)
	if err != nil {
		return OAuthMethod{}, err
	}
	method.Expiry = expiry
	return method, nil
}
