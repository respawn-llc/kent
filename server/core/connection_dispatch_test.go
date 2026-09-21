package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	modelstub "core/internal/testharness/pty/blackbox"
	"core/internal/testharness/testsetup"
	"core/server/auth"
	"core/server/metadata"
	"core/server/session"
	"core/shared/config"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
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
			definition := settings.Connections[*settings.Connection]
			definition.EnvironmentVariable = &keyName
			settings.Connections[*settings.Connection] = definition
			selected := *settings.Connection
			if bound {
				other := config.ConnectionID("other")
				settings.Connections[other] = config.ProviderConnection{
					Protocol: config.ConnectionResponses, Endpoint: textutil.Value("http://127.0.0.1:1/v1"),
				}
				settings.Connection = &other
			}
			settings.Model, settings.ModelVerbosity = "operator-alias", config.ModelVerbosityHigh
			settings.Reviewer.Frequency = "off"
			cfg := testsetup.ProgrammaticConfig(t, settings)
			binding, err := metadata.RegisterBinding(t.Context(), cfg.PersistenceRoot, cfg.WorkspaceRoot)
			if err != nil {
				t.Fatal(err)
			}
			app := newCoreTestApp(t, cfg, auth.EmptyState())
			store := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
			if bound {
				if err := store.SetConnectionID(selected); err != nil {
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
			_, err = app.SessionRuntimeClient().ActivateSessionRuntime(t.Context(), serverapi.SessionRuntimeActivateRequest{
				SessionID: store.Meta().SessionID, OwnerID: "contract-test",
				ActiveSettings: cfg.Settings, Source: cfg.Source,
				QuestionsEnabled: textutil.Value(true), AutoCompactionEnabled: textutil.Value(false),
			})
			if err != nil {
				t.Fatal(err)
			}
			input, err := protoapi.UserTurnInputToProto(runtimeinput.Text("continue"))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if _, err := app.RuntimeControlClient().SubmitUserTurn(ctx, &runtimepb.SubmitUserTurnRequest{
				SessionId: store.Meta().SessionID, Input: input,
			}); err != nil {
				t.Fatal(err)
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
			if record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != selected {
				t.Fatalf("dispatch binding = %v", record.Meta.ConnectionID)
			}
		})
	}
}
