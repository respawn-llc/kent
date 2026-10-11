package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"core/shared/config"
	"core/shared/textutil"
)

var managerTestNow = time.Date(2026, time.January, 1, 10, 0, 0, 0, time.UTC)

func TestConnectionRefreshUsesSelectedProvider(t *testing.T) {
	state := EmptyState()
	for _, protocol := range []config.ConnectionProtocol{config.ConnectionChatGPT, config.ConnectionGrokCLIProxy, config.ConnectionGrokOAuthAPI} {
		state.Connections[config.ConnectionID(protocol)] = OAuthMethod{
			AccessToken: string(protocol), RefreshToken: string(protocol),
			Expiry: textutil.Value(managerTestNow.Add(-time.Minute)),
		}
	}
	openAI := NewOAuthRefresher(func() time.Time { return managerTestNow }, time.Minute, func(_ context.Context, method OAuthMethod) (OAuthMethod, error) {
		if method.RefreshToken != string(config.ConnectionChatGPT) {
			t.Fatal("Grok credential reached OpenAI")
		}
		method.AccessToken = "openai-refreshed"
		return method, nil
	})
	grok := NewOAuthRefresher(func() time.Time { return managerTestNow }, time.Minute, func(_ context.Context, method OAuthMethod) (OAuthMethod, error) {
		if method.RefreshToken == string(config.ConnectionChatGPT) {
			t.Fatal("OpenAI credential reached Grok")
		}
		method.AccessToken = "grok-refreshed"
		return method, nil
	})
	manager := NewManager(NewMemoryStore(state), map[config.ConnectionProtocol]*OAuthRefresher{
		config.ConnectionChatGPT: openAI, config.ConnectionGrokCLIProxy: grok, config.ConnectionGrokOAuthAPI: grok,
	})
	for id := range state.Connections {
		got, err := manager.CurrentOAuth(t.Context(), id, config.ConnectionProtocol(id))
		if err != nil {
			t.Fatal(err)
		}
		expected := "grok-refreshed"
		if id == config.ConnectionID(config.ConnectionChatGPT) {
			expected = "openai-refreshed"
		}
		if got.AccessToken != expected || got.RefreshToken != string(id) {
			t.Fatalf("connection %s refreshed wrong credential: %+v", id, got)
		}
	}
}

func TestConnectionCredentialReplacementIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	manager := NewManager(NewFileStore(path), nil)
	for id, token := range map[config.ConnectionID]string{"work": "work-token", "personal": "personal-token"} {
		if err := manager.SaveOAuth(t.Context(), id, OAuthMethod{AccessToken: token, AccountID: string(id)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.SaveOAuth(t.Context(), "work", OAuthMethod{AccessToken: "replacement", AccountID: "new-work"}); err != nil {
		t.Fatal(err)
	}
	reopened := NewManager(NewFileStore(path), nil)
	work, err := reopened.CurrentOAuth(t.Context(), "work", config.ConnectionChatGPT)
	if err != nil || work.AccessToken != "replacement" || work.AccountID != "new-work" {
		t.Fatalf("replacement = %+v, %v", work, err)
	}
	personal, err := reopened.CurrentOAuth(t.Context(), "personal", config.ConnectionChatGPT)
	if err != nil || personal.AccessToken != "personal-token" || personal.AccountID != "personal" {
		t.Fatalf("other connection changed = %+v, %v", personal, err)
	}
}

func TestCurrentOAuthSurfacesRefreshFailureWithoutChangingCredentials(t *testing.T) {
	initial := testOAuthState()
	credential := initial.Connections["work"]
	credential.Expiry = textutil.Value(managerTestNow.Add(-time.Minute))
	initial.Connections["work"] = credential
	store := NewMemoryStore(initial)
	refreshErr := errors.New("refresh failed")
	manager := NewManager(store, map[config.ConnectionProtocol]*OAuthRefresher{config.ConnectionChatGPT: NewOAuthRefresher(
		func() time.Time { return managerTestNow }, 30*time.Second,
		func(context.Context, OAuthMethod) (OAuthMethod, error) {
			return OAuthMethod{}, errors.Join(ErrOAuthRefreshFailed, refreshErr)
		},
	)})
	if _, err := manager.CurrentOAuth(t.Context(), "work", config.ConnectionChatGPT); !errors.Is(err, refreshErr) {
		t.Fatalf("refresh error = %v", err)
	}
	persisted := requireAuthState(t, store.Load)
	if persisted.Connections["work"] != credential {
		t.Fatal("failed refresh changed saved credentials")
	}
}

func TestCurrentOAuthRefreshesAndPersistsSelectedConnection(t *testing.T) {
	initial := testOAuthState()
	credential := initial.Connections["work"]
	credential.Expiry = textutil.Value(managerTestNow.Add(-time.Minute))
	initial.Connections["work"] = credential
	initial.Connections["personal"] = OAuthMethod{AccessToken: "personal-token"}
	store := NewMemoryStore(initial)
	refreshed := credential
	refreshed.AccessToken = "fresh-token"
	refreshed.Expiry = textutil.Value(managerTestNow.Add(time.Hour))
	manager := NewManager(store, map[config.ConnectionProtocol]*OAuthRefresher{config.ConnectionChatGPT: NewOAuthRefresher(
		func() time.Time { return managerTestNow }, 30*time.Second,
		func(context.Context, OAuthMethod) (OAuthMethod, error) {
			return refreshed, nil
		},
	)})
	current, err := manager.CurrentOAuth(t.Context(), "work", config.ConnectionChatGPT)
	if err != nil || current != refreshed {
		t.Fatalf("refreshed credential = %+v, %v", current, err)
	}
	persisted := requireAuthState(t, store.Load)
	if persisted.Connections["work"] != refreshed || persisted.Connections["personal"] != initial.Connections["personal"] {
		t.Fatal("refresh must replace only the selected connection")
	}
}

func TestCredentialStatusDoesNotWaitForRefresh(t *testing.T) {
	initial := testOAuthState()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	manager := NewManager(NewMemoryStore(initial), map[config.ConnectionProtocol]*OAuthRefresher{config.ConnectionChatGPT: NewOAuthRefresher(
		func() time.Time { return managerTestNow.Add(2 * time.Hour) }, 30*time.Second,
		func(ctx context.Context, method OAuthMethod) (OAuthMethod, error) {
			close(started)
			select {
			case <-release:
				return method, nil
			case <-ctx.Done():
				return OAuthMethod{}, ctx.Err()
			}
		},
	)})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _, _ = manager.CurrentOAuth(ctx, "work", config.ConnectionChatGPT) }()
	<-started
	status := make(chan State, 1)
	go func() {
		state, err := manager.Load(t.Context())
		if err != nil {
			t.Error(err)
		}
		status <- state
	}()
	select {
	case state := <-status:
		if state.Connections["work"] != initial.Connections["work"] {
			t.Fatal("status must report latest saved credentials during refresh")
		}
	case <-time.After(time.Second):
		t.Fatal("status waited for a refreshing connection")
	}
}
