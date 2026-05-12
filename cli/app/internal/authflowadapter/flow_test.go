package authflowadapter

import (
	"context"
	"testing"

	"builder/shared/auth"
)

func TestWrapStoreWithEnvAPIKeyOverridePrefersEnvWhenConfigured(t *testing.T) {
	base := &memoryStore{state: auth.State{
		Method:              auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "stored-key"}},
		EnvAPIKeyPreference: auth.EnvAPIKeyPreferencePreferEnv,
	}}
	store := WrapStoreWithEnvAPIKeyOverride(base, func(key string) string {
		if key == "OPENAI_API_KEY" {
			return "env-key"
		}
		return ""
	})

	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if state.Method.APIKey == nil || state.Method.APIKey.Key != "env-key" {
		t.Fatalf("expected env api key override, got %+v", state.Method)
	}
}

type memoryStore struct {
	state auth.State
}

func (s *memoryStore) Load(context.Context) (auth.State, error) {
	return s.state, nil
}

func (s *memoryStore) Save(_ context.Context, state auth.State) error {
	s.state = state
	return nil
}
