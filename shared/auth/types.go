package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrAuthNotConfigured     = errors.New("auth is not configured")
	ErrInvalidAuthMethod     = errors.New("invalid auth method")
	ErrOAuthRefreshFailed    = errors.New("oauth token refresh failed")
	ErrDeviceCodeUnsupported = errors.New("device code login is not enabled")
)

type OAuthMethod struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	AccountID    string    `json:"account_id,omitempty"`
	Email        string    `json:"email,omitempty"`
}

func (m OAuthMethod) Validate() error {
	if strings.TrimSpace(m.AccessToken) == "" {
		return fmt.Errorf("%w: OAuth access token is empty", ErrInvalidAuthMethod)
	}
	return nil
}

func (m OAuthMethod) AuthHeaderValue() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	return "Bearer " + m.AccessToken, nil
}
