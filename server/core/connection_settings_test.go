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
model = "gpt-5"
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
					_, err := app.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
						Session:   target,
						Operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: "unavailable"}},
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
			})
		}
	}
}
