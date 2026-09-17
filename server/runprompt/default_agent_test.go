package runprompt

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	modelstub "core/internal/testharness/pty/blackbox"
	"core/server/auth"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestHeadlessDefaultSelectionSurvivesRuntimeActivation(t *testing.T) {
	for _, initialRole := range []*string{nil, textutil.Value("worker")} {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			modelstub.WriteCompletedResponseStream(w, "done", 1, 1)
		}))
		t.Cleanup(provider.Close)
		root, workspace, containerDir := t.TempDir(), t.TempDir(), t.TempDir()
		cfg, err := config.Load(workspace, config.LoadOptions{ConfigRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		cfg.Settings.OpenAIBaseURL = provider.URL
		cfg.Settings.Reviewer.Frequency = "off"
		cfg.Settings.Subagents[config.DefaultSubagentRole] = config.SubagentRole{
			Settings: config.Settings{Model: "gpt-5-mini"},
			Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}},
		}
		cfg.Settings.Subagents["worker"] = config.SubagentRole{}
		persistence := sessiontest.NewPersistence()
		store, err := session.Create(containerDir, "selected", workspace, sessioncontract.SessionCategoryMain, persistence.Options()...)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: initialRole}); err != nil {
			t.Fatal(err)
		}
		authManager := auth.NewManager(auth.NewMemoryStore(auth.State{Method: auth.Method{
			Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"},
		}}), nil, time.Now)
		client := NewInProcessRunPromptClient(HeadlessBootstrap{
			SessionLaunch:    newTestHeadlessSessionLaunch(cfg, containerDir, authManager, persistence),
			RuntimeAuthority: newTestHeadlessRuntimeAuthority(root, authManager, nil, persistence.Options()...),
		})
		_, err = client.RunPrompt(t.Context(), serverapi.RunPromptRequest{
			Intent:    serverapi.OpenExistingSessionLaunchIntent(mustRunPromptSessionID(t, store.Meta().SessionID)),
			Prompt:    "finish",
			Overrides: serverapi.RunPromptOverrides{AgentRole: textutil.Value(config.DefaultSubagentRole)},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := session.Open(store.Dir(), persistence.Options()...)
		if err != nil {
			t.Fatal(err)
		}
		role := session.ContinuationAgentRole(reopened.Meta())
		if role == nil || *role != config.DefaultSubagentRole {
			t.Fatalf("activated headless default lost its role: %v", role)
		}
	}
}
