package runtimewire

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"core/server/auth"
	"core/server/llm"
	"core/server/session"
	"core/shared/config"
	"core/shared/transcript"
)

func TestRuntimeOpensWithExpiredPresentConnectionCredentials(t *testing.T) {
	var refreshes atomic.Int32
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(issuer.Close)
	now := time.Now()
	manager := auth.NewManager(auth.NewMemoryStore(auth.EmptyState()), auth.NewOpenAIOAuthRefresher(
		auth.OpenAIOAuthOptions{Issuer: issuer.URL, HTTPClient: issuer.Client()}, func() time.Time { return now }, time.Minute,
	))
	id := config.ConnectionID("expired")
	if err := manager.SaveOAuth(t.Context(), id, auth.OAuthMethod{
		AccessToken: "expired-access", RefreshToken: "expired-refresh", Expiry: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	active := runtimeWireShellSettings(config.ShellPostprocessingModeBuiltin, nil)
	active.Model = "gpt-6-astra"
	active.ThinkingLevel = "high"
	active.Connection = &id
	active.Connections = map[config.ConnectionID]config.ProviderConnection{id: {Protocol: config.ConnectionChatGPT}}
	root := t.TempDir()
	store := newRuntimeWireSession(t, root, "expired-auth")
	wiring, err := newTestRuntimeWiringWithBackground(t, store, materializedRuntimeWireEventLog(t, store),
		active, nil, manager, nil, nil, requiredRuntimeWireTestOptions(RuntimeWiringOptions{
			FilesystemContext: runtimeWireFilesystemContext(t, root),
		}))
	if err != nil {
		t.Fatalf("opening Session with saved expired credentials: %v", err)
	}
	t.Cleanup(func() { _ = wiring.Close() })
	if got := refreshes.Load(); got != 0 {
		t.Fatalf("opening Session refreshed credentials %d times", got)
	}
	_, err = wiring.Engine.SubmitUserMessage(t.Context(), "send with expired credentials")
	if !errors.Is(err, auth.ErrOAuthRefreshFailed) || !llm.IsNonRetriableModelError(err) {
		t.Fatalf("request failure = %v, want non-retriable OAuth refresh failure", err)
	}
	if got := refreshes.Load(); got != 1 {
		t.Fatalf("failed request refreshed credentials %d times, want once", got)
	}
	message := llm.UserFacingError(err)
	if message == "" {
		t.Fatal("refresh failure has no sign-in guidance")
	}
	window, err := materializedRuntimeWireEventLog(t, store).ReadNewestSegmentBackward(nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range window.Records {
		payload, err := record.Payload()
		if err != nil {
			t.Fatal(err)
		}
		if entry, ok := payload.(session.LocalEntryRecord); ok && entry.Role == string(transcript.EntryRoleDeveloperErrorFeedback) {
			if entry.Text != nil && *entry.Text == message {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("request failure was not persisted as transcript error feedback")
	}
}
