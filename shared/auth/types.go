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

type MethodType string

const MethodOAuth MethodType = "oauth"

type Method struct {
	Type  MethodType   `json:"type"`
	OAuth *OAuthMethod `json:"oauth"`
}

type OAuthMethod struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	AccountID    string    `json:"account_id,omitempty"`
	Email        string    `json:"email,omitempty"`
}

func (m Method) Validate() error {
	if m.Type != MethodOAuth || m.OAuth == nil {
		return fmt.Errorf("%w: OAuth credential is required", ErrInvalidAuthMethod)
	}
	if strings.TrimSpace(m.OAuth.AccessToken) == "" {
		return fmt.Errorf("%w: OAuth access token is empty", ErrInvalidAuthMethod)
	}
	return nil
}

func (m Method) AuthHeaderValue() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	return "Bearer " + m.OAuth.AccessToken, nil
}
