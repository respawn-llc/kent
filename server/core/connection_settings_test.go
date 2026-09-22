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
	"core/server/metadata"
	"core/server/session"
	"core/shared/config"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
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
model = "gpt-5"
[connections.work]
protocol = "responses"
endpoint = "http://127.0.0.1:1/v1"
[reviewer]
frequency = "off"
[subagents.worker]
model = "gpt-5-mini"
%s
[subagents.unavailable]
model = "gpt-5-mini"
connection = "missing"
`, currentDefault, roleSelection)), 0o600); err != nil {
					t.Fatal(err)
				}
				cfg, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
				if err != nil {
					t.Fatal(err)
				}
				binding, err := metadata.RegisterBinding(t.Context(), cfg.PersistenceRoot, cfg.WorkspaceRoot)
				if err != nil {
					t.Fatal(err)
				}
				app := newCoreTestApp(t, cfg, auth.EmptyState())
				store := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
				if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: textutil.Value("worker")}); err != nil {
					t.Fatal(err)
				}
				if err := store.SetConnectionID("work"); err != nil {
					t.Fatal(err)
				}
				if locked {
					if err := store.MarkModelDispatchLocked(session.LockedContract{
						Model: "gpt-5", HasEnabledTools: true, EnabledTools: []string{"ask_question"}, WebSearchMode: "none",
						ProviderContract: session.LockedProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true},
					}); err != nil {
						t.Fatal(err)
					}
				}
				target := &chatsettingspb.SessionTarget{SessionId: store.Meta().SessionID}
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
					if choice.Role == "unavailable" && choice.GetModel() != "gpt-5-mini" {
						t.Fatalf("known configured model was omitted: %+v", choice)
					}
					if invalidDefault && choice.Role == config.BuiltInSubagentRoleFast && (choice.Model != nil || choice.Thinking != nil) {
						t.Fatalf("provider-dependent facts were invented: %+v", choice)
					}
				}
				if !found {
					t.Fatal("unavailable alternative was hidden")
				}
				if _, err := launch.PrepareChatAgentCatalog(cfg, false); err == nil {
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
				before, err := app.MetadataStore().ResolvePersistedSession(t.Context(), target.SessionId)
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
						after, err := app.MetadataStore().ResolvePersistedSession(t.Context(), target.SessionId)
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

func TestEquivalentSessionAgentRepairsCompleteDefaultSettings(t *testing.T) {
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
			testSessionAgentDefaultRepair(t, scenario.locked, scenario.distinctConnection)
		})
	}
}

func testSessionAgentDefaultRepair(t *testing.T, locked, distinctConnection bool) {
	root, workspace := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "config.toml")
	writeConfig := func(model string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(fmt.Sprintf(`
connection = "work"
model = "gpt-5"
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
	writeConfig("gpt-5-mini")
	cfg, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := metadata.RegisterBinding(t.Context(), root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	app := newCoreTestApp(t, cfg, auth.EmptyState())
	store := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
	state, err := session.ChatSettingsStateFromCompleteSettings("worker", session.ChatSettings{
		Supervisor: "edits", Thinking: "high", Fast: true, Questions: false, AutoCompaction: false,
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
			Model: "gpt-5", HasEnabledTools: true, EnabledTools: []string{"ask_question"}, WebSearchMode: "none",
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
	writeConfig("gpt-5")
	target := &chatsettingspb.SessionTarget{SessionId: before.SessionID}
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
	if locked || distinctConnection {
		if projected.SelectedAgent.Role != "worker" || projected.SelectedAgent.Thinking != "high" ||
			projected.Questions.Enabled || projected.AutoCompaction.Stored {
			t.Fatalf("non-equivalent or locked selection was repaired: %+v", projected)
		}
		persisted, err := app.MetadataStore().ResolvePersistedSession(t.Context(), before.SessionID)
		if err != nil || !reflect.DeepEqual(storedBefore.Meta, persisted.Meta) {
			t.Fatalf("read changed stored selection: %v", err)
		}
		return
	}
	if projected.SelectedAgent.Role != "default" || projected.SelectedAgent.Thinking != "medium" ||
		!projected.Questions.Enabled || !projected.AutoCompaction.Stored ||
		projected.Supervisor.Value != projected.Supervisor.Baseline {
		t.Fatalf("complete default baseline not projected: %+v", projected)
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
		t.Fatalf("repair mutation: %+v %v", result, err)
	}
	persisted, err = app.MetadataStore().ResolvePersistedSession(t.Context(), before.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := session.ChatSettingsStateFromMeta(*persisted.Meta)
	if err != nil {
		t.Fatal(err)
	}
	want, err := session.ChatSettingsStateFromCompleteSettings("default", session.ChatSettings{
		Supervisor: "off", Thinking: "medium", Questions: true, AutoCompaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want.ConnectionID = state.ConnectionID
	if !reflect.DeepEqual(repaired, want) {
		t.Fatalf("repair = %+v, want %+v", repaired, want)
	}
	if got := read(); got.SelectedAgent.Role != "default" || !got.Questions.Enabled || !got.AutoCompaction.Stored {
		t.Fatalf("reread lost repair: %+v", got)
	}
	reopened, err := session.Open(store.Dir(), app.MetadataStore().AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	reopenedState, err := session.ChatSettingsStateFromMeta(reopened.Meta())
	if err != nil || !reflect.DeepEqual(reopenedState, want) {
		t.Fatalf("reopen lost complete repair: %+v %v", reopenedState, err)
	}
}
