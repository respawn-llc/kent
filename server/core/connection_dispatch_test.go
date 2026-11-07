package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	modelstub "core/internal/testharness/pty/blackbox"

	"core/internal/testharness/testsetup"
	"core/server/authmanager"
	"core/server/sessionstore"
	"core/shared/config"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"core/shared/runtimeids"

	"core/shared/textutil"
)

func TestConnectionDispatchPreservesEstablishedGenerationContract(t *testing.T) {
	for _, bound := range []bool{true, false} {
		name := "first binding"
		if bound {
			name = "saved binding"
		}
		t.Run(name, func(t *testing.T) {
			type observation struct {
				model         string
				verbosity     string
				authorization string
				account       string
				path          string
				err           error
			}
			observed := make(chan observation, 1)
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Model string `json:"model"`
					Text  struct {
						Verbosity string `json:"verbosity"`
					} `json:"text"`
				}
				err := json.NewDecoder(r.Body).Decode(&payload)
				observed <- observation{
					model: payload.Model, verbosity: payload.Text.Verbosity,
					authorization: r.Header.Get("Authorization"), account: r.Header.Get("ChatGPT-Account-Id"),
					path: r.URL.Path, err: err,
				}
				modelstub.WriteCompletedResponseStream(w, "done", 1, 1)
			}))
			t.Cleanup(endpoint.Close)
			keyName := "CONTRACT_TEST_KEY"
			t.Setenv(keyName, "contract-key")
			settings := testsetup.WithResponsesProvider(config.DefaultOnboardingSettings(), endpoint.URL)
			selected, err := settings.Connection.ConcreteID()
			if err != nil {
				t.Fatal(err)
			}
			definition := settings.Connections[*selected]
			definition.EnvironmentVariable = &keyName
			settings.Connections[*selected] = definition
			if bound {
				other := config.ConnectionID("other")
				settings.Connections[other] = config.ProviderConnection{
					Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://127.0.0.1:1/v1"),
				}
				settings.Connection = config.SingleConnection(other)
			}
			settings.Model, settings.ModelVerbosity = "operator-alias", config.ModelVerbosityHigh
			settings.Reviewer.Frequency = "off"
			cfg := testsetup.ProgrammaticConfig(t, settings)
			workspaceConfig := filepath.Join(cfg.WorkspaceRoot, config.ConfigDirName)
			if err := os.MkdirAll(workspaceConfig, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspaceConfig, "config.toml"), []byte("model = \"operator-alias\"\nmodel_verbosity = \"high\"\n[reviewer]\nfrequency = \"off\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			meta := testsetup.OpenStore(t, cfg.PersistenceRoot)
			binding, err := meta.RegisterWorkspaceBinding(t.Context(), cfg.WorkspaceRoot)
			if err != nil {
				t.Fatal(err)
			}
			app := newCoreTestAppWithOptions(t, cfg, auth.EmptyState(), Options{MetadataStore: meta})
			store := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
			if bound {
				if err := store.SetConnectionID(*selected); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.MarkModelDispatchLocked(session.LockedContract{
				Model: "operator-alias", HasEnabledTools: true, WebSearchMode: "none",
				ProviderContract: session.LockedProviderCapabilities{
					ProviderID: "chatgpt-codex", SupportsResponsesAPI: true,
					SupportsProviderVerbosity: textutil.Value(true),
				},
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result, err := app.ChatMutationClient().Steer(ctx, &chatpb.SteerRequest{
				QueueItemId: runtimeids.NewQueueItemID().String(), Target: chatCommandTarget(store.Meta().SessionID),
				Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "continue"}},
			})
			if err != nil || result.GetAccepted() == nil {
				t.Fatalf("Chat action: %v, %v", result, err)
			}
			select {
			case request := <-observed:
				if request.err != nil || request.model != "operator-alias" || request.verbosity != "high" ||
					request.authorization != "Bearer contract-key" || request.account != "" || request.path != "/responses" {
					t.Fatalf("connection dispatch = %+v", request)
				}
			case <-ctx.Done():
				t.Fatal("request did not reach the selected connection")
			}
			record, err := app.MetadataStore().ResolvePersistedSession(t.Context(), store.Meta().SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != *selected {
				t.Fatalf("dispatch binding = %v", record.Meta.ConnectionID)
			}
		})
	}
}
