package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/server/auth"
	"core/server/launch"
	"core/server/session"
	"core/shared/config"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func TestExistingSessionSettingsUseSavedConnectionBeforeCurrentCatalog(t *testing.T) {
	for _, locked := range []bool{false, true} {
		for _, invalidDefault := range []bool{false, true} {
			name := "role"
			if invalidDefault {
				name = "default"
			}
			if locked {
				name += "/locked"
			}
			t.Run(name, func(t *testing.T) {
				root, workspace := t.TempDir(), t.TempDir()
				currentDefault, roleSelection := "work", "connection = \"missing\""
				if invalidDefault {
					currentDefault, roleSelection = "missing", ""
				}
				if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(fmt.Sprintf(`
connection = %q
model = "gpt-6-sol"
[connections.work]
protocol = "responses"
endpoint = "http://127.0.0.1:1/v1"
[reviewer]
frequency = "off"
[subagents.worker]
model = "gpt-6-luna"
%s
[subagents.unavailable]
model = "gpt-6-luna"
connection = "missing"
`, currentDefault, roleSelection)), 0o600); err != nil {
					t.Fatal(err)
				}
				cfg, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
				if err != nil {
					t.Fatal(err)
				}
				app := newCoreTestApp(t, cfg, auth.EmptyState())
				binding, err := app.MetadataStore().RegisterWorkspaceBinding(t.Context(), cfg.WorkspaceRoot)
				if err != nil {
					t.Fatal(err)
				}
				store := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
				if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("worker")}); err != nil {
					t.Fatal(err)
				}
				if err := store.SetConnectionID("work"); err != nil {
					t.Fatal(err)
				}
				if locked {
					if err := store.MarkModelDispatchLocked(session.LockedContract{
						Model: "gpt-6-sol", HasEnabledTools: true, EnabledTools: []string{"ask_question"}, WebSearchMode: "none",
						ProviderContract: session.LockedProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true},
					}); err != nil {
						t.Fatal(err)
					}
				}
				target := &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: store.Meta().SessionID}}}
				read, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
					Target: &chatsettingspb.ReadRequest_Session{Session: target},
				})
				if err != nil {
					t.Fatalf("bound settings read: %v", err)
				}
				current := read.GetSession().Settings
				if current.SelectedAgent.Role != "worker" {
					t.Fatalf("selected Agent = %+v", current.SelectedAgent)
				}
				found := false
				for _, choice := range current.AgentChoices {
					found = found || choice.Role == "unavailable"
					if choice.Role == "unavailable" && choice.GetModel() != "gpt-6-luna" {
						t.Fatalf("known configured model was omitted: %+v", choice)
					}
					if invalidDefault && choice.Role == config.BuiltInSubagentRoleFast && (choice.Model != nil || choice.Thinking != nil) {
						t.Fatalf("provider-dependent facts were invented: %+v", choice)
					}
				}
				if !found {
					t.Fatal("unavailable alternative was hidden")
				}
				if _, err := launch.PrepareChatAgentCatalog(cfg, false, serverapi.RunPromptOverrides{}); err == nil {
					t.Fatal("New Chat accepted unavailable current selections")
				}
				mutated, err := app.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
					Session: target,
					Operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_QuestionsEnabled{
						QuestionsEnabled: !current.Questions.Enabled,
					}},
				})
				if err != nil || mutated.GetResult().GetApplied() == nil {
					t.Fatalf("bound non-Agent mutation: %+v, %v", mutated, err)
				}
				before, err := app.MetadataStore().ResolvePersistedSession(t.Context(), target.SessionId.GetSessionId())
				if err != nil {
					t.Fatal(err)
				}
				if before.Meta.ConnectionID == nil || *before.Meta.ConnectionID != "work" {
					t.Fatalf("non-Agent mutation changed binding: %v", before.Meta.ConnectionID)
				}
				if !locked {
					unavailable := []string{"unavailable"}
					if invalidDefault {
						unavailable = append(unavailable, config.BuiltInSubagentRoleFast)
					}
					for _, agent := range unavailable {
						_, err := app.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
							Session:   target,
							Operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: agent}},
						})
						var reference *config.ConnectionReferenceError
						if !errors.As(err, &reference) || reference.Connection == nil || *reference.Connection != "missing" {
							t.Fatalf("unavailable selection error = %v", err)
						}
						after, err := app.MetadataStore().ResolvePersistedSession(t.Context(), target.SessionId.GetSessionId())
						if err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(before.Meta, after.Meta) {
							t.Fatal("unavailable Agent selection changed the Session")
						}
					}
				}
			})
		}
	}
}

func TestEquivalentSessionAgentPreservesRoleAndSettings(t *testing.T) {
	for _, scenario := range []struct {
		name               string
		locked             bool
		distinctConnection bool
	}{
		{name: "equivalent"},
		{name: "locked", locked: true},
		{name: "distinct saved connection", distinctConnection: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			testSessionAgentIdentityPreserved(t, scenario.locked, scenario.distinctConnection)
		})
	}
}

func testSessionAgentIdentityPreserved(t *testing.T, locked, distinctConnection bool) {
	root, workspace := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "config.toml")
	writeConfig := func(model string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(fmt.Sprintf(`
connection = "work"
model = "gpt-6-sol"
thinking_level = "medium"
[connections.work]
protocol = "responses"
endpoint = "http://127.0.0.1:1/v1"
[connections.other]
protocol = "responses"
endpoint = "http://127.0.0.1:1/v1"
[reviewer]
frequency = "off"
[subagents.worker]
model = %q
`, model)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig("gpt-6-luna")
	cfg, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	app := newCoreTestApp(t, cfg, auth.EmptyState())
	binding, err := app.MetadataStore().RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	store := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
	state, err := session.ChatSettingsStateFromCompleteSettings("worker", session.ChatSettings{
		Supervisor: "edits", Thinking: "high", Questions: false, AutoCompaction: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	state.ConnectionID = textutil.Value(config.ConnectionID("work"))
	if distinctConnection {
		state.ConnectionID = textutil.Value(config.ConnectionID("other"))
	}
	if _, err := store.CommitChatSettingsState(state); err != nil {
		t.Fatal(err)
	}
	if locked {
		if err := store.MarkModelDispatchLocked(session.LockedContract{
			Model: "gpt-6-sol", HasEnabledTools: true, EnabledTools: []string{"ask_question"}, WebSearchMode: "none",
			ProviderContract: session.LockedProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true},
		}); err != nil {
			t.Fatal(err)
		}
	}
	before := store.Meta()
	storedBefore, err := app.MetadataStore().ResolvePersistedSession(t.Context(), before.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	writeConfig("gpt-6-sol")
	target := &chatsettingspb.SessionTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: before.SessionID}}}
	read := func() *chatsettingspb.Settings {
		t.Helper()
		response, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
			Target: &chatsettingspb.ReadRequest_Session{Session: target},
		})
		if err != nil {
			t.Fatal(err)
		}
		return response.GetSession().Settings
	}
	projected := read()
	if projected.SelectedAgent.Role != "worker" || projected.SelectedAgent.Thinking != "high" ||
		projected.Questions.Enabled || projected.AutoCompaction.Stored ||
		projected.Supervisor.Value != chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_AFTER_EDITS {
		t.Fatalf("equivalent role lost identity or settings: %+v", projected)
	}
	persisted, err := app.MetadataStore().ResolvePersistedSession(t.Context(), before.SessionID)
	if err != nil || !reflect.DeepEqual(storedBefore.Meta, persisted.Meta) {
		t.Fatalf("read changed stored settings: %v", err)
	}
	result, err := app.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
		Session: target, Operation: &chatsettingspb.MutationOperation{
			Operation: &chatsettingspb.MutationOperation_QuestionsEnabled{QuestionsEnabled: projected.Questions.Enabled},
		},
	})
	if err != nil || result.GetResult().GetApplied() == nil {
		t.Fatalf("settings mutation: %+v %v", result, err)
	}
	persisted, err = app.MetadataStore().ResolvePersistedSession(t.Context(), before.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	persistedState, err := session.ChatSettingsStateFromMeta(*persisted.Meta)
	if err != nil {
		t.Fatal(err)
	}
	want, err := session.ChatSettingsStateFromCompleteSettings("worker", session.ChatSettings{
		Supervisor: "edits", Thinking: "high", Questions: false, AutoCompaction: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	want.ConnectionID = state.ConnectionID
	if !reflect.DeepEqual(persistedState, want) {
		t.Fatalf("settings = %+v (%+v), want %+v (%+v)", persistedState, persistedState.Settings, want, want.Settings)
	}
	if got := read(); got.SelectedAgent.Role != "worker" || got.Questions.Enabled || got.AutoCompaction.Stored {
		t.Fatalf("reread lost selected role or settings: %+v", got)
	}
	reopened, err := session.Open(store.Dir(), app.MetadataStore().AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	reopenedState, err := session.ChatSettingsStateFromMeta(reopened.Meta())
	if err != nil || !reflect.DeepEqual(reopenedState, want) {
		t.Fatalf("reopen lost selected role or settings: %+v %v", reopenedState, err)
	}
}
