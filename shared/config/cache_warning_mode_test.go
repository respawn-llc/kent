package config

import (
	"errors"
	"testing"
)

func TestValidateCacheWarningMode(t *testing.T) {
	settings := configRegistry.defaultState().Settings
	settings.CacheWarningMode = CacheWarningMode("loud")
	err := configRegistry.validate(settingsState{Settings: settings}, map[string]string{"model": "default"})
	if !errors.Is(err, errInvalidCacheWarningMode) {
		t.Fatalf("expected cache_warning_mode validation error, got %v", err)
	}
}
