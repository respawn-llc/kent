package auth

import (
	"errors"
	"fmt"

	sharedauth "core/shared/auth"
	"core/shared/config"
)

var (
	ErrAuthNotConfigured     = sharedauth.ErrAuthNotConfigured
	ErrInvalidAuthMethod     = sharedauth.ErrInvalidAuthMethod
	ErrOAuthRefreshFailed    = sharedauth.ErrOAuthRefreshFailed
	ErrDeviceCodeUnsupported = sharedauth.ErrDeviceCodeUnsupported
)

type State struct {
	Connections map[config.ConnectionID]OAuthMethod `json:"connections"`
}

func (s State) Validate() error {
	if s.Connections == nil {
		return errors.New("connection-keyed OAuth store is required; upgrade the server credential file and sign in again")
	}
	for id, credential := range s.Connections {
		if _, err := config.ParseConnectionID(string(id)); err != nil {
			return err
		}
		if err := credential.Validate(); err != nil {
			return fmt.Errorf("connection %s: %w", id, err)
		}
	}
	return nil
}

type OAuthMethod = sharedauth.OAuthMethod

func EmptyState() State {
	return State{Connections: make(map[config.ConnectionID]OAuthMethod)}
}
