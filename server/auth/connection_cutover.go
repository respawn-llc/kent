package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"core/shared/config"
)

// LegacyConnectionSelection only inspects the obsolete selection, never its
// tokens. The bootstrap owner converts config before replacing this old store.
func (s *FileStore) LegacyConnectionSelection(ctx context.Context) (*config.LegacyConnectionAuth, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := ensureSecureAuthStatePermissions(s.path); err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", s.path, err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", s.path, err)
	}
	if _, keyed := envelope["connections"]; keyed {
		var state State
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, false, fmt.Errorf("parse %s: %w", s.path, err)
		}
		return nil, false, state.Validate()
	}
	var legacy struct {
		Scope  string `json:"scope"`
		Method struct {
			Type string `json:"type"`
		} `json:"method"`
		Preference *string `json:"env_api_key_preference"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, false, fmt.Errorf("parse old credential selection %s: %w", s.path, err)
	}
	if legacy.Scope != "global" {
		return nil, false, fmt.Errorf("%s: unsupported old credential scope", s.path)
	}
	if legacy.Preference != nil {
		switch *legacy.Preference {
		case "prefer_env_api_key":
			selection := config.LegacyConnectionAPIKey
			return &selection, true, nil
		case "", "prefer_saved_auth":
		default:
			return nil, false, fmt.Errorf("%s: unsupported old credential preference", s.path)
		}
	}
	var selection config.LegacyConnectionAuth
	switch legacy.Method.Type {
	case "oauth":
		selection = config.LegacyConnectionOAuth
	case "api_key":
		selection = config.LegacyConnectionAPIKey
	case "":
		if legacy.Preference == nil || *legacy.Preference != "prefer_saved_auth" {
			return nil, true, nil
		}
		selection = config.LegacyConnectionAnonymous
	default:
		return nil, false, fmt.Errorf("%s: unsupported old credential method", s.path)
	}
	return &selection, true, nil
}
