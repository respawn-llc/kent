package sessionlaunch

import (
	"slices"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/launch"
	"core/server/sessionstore"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"

	"google.golang.org/protobuf/proto"
)

func TestNewChatCatalogCarriesCompletePreparedBaselines(t *testing.T) {
	app := testNewChatSettingsApp(t)
	prepared, err := launch.PrepareChatAgentCatalog(app, false, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := NewService(launch.Planner{Config: app}, ChatSettingsOwner{}, nil).NewChatSettings(t.Context(), serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	catalog := response.GetNewChat()
	if catalog == nil || response.GetSession() != nil {
		t.Fatalf("New Chat response = %+v", response)
	}
	entries := prepared.Entries()
	if len(catalog.Choices) != len(entries) {
		t.Fatalf("choices = %d, want %d", len(catalog.Choices), len(entries))
	}
	for i, choice := range catalog.Choices {
		entry := entries[i]
		supervisor := protoapi.ChatSettingsSupervisorFromProto(choice.Baseline.Supervisor)
		if !proto.Equal(choice.Agent, entry.Choice) ||
			choice.Baseline.AgentRole != entry.Choice.Role ||
			supervisor != entry.Settings.Baseline.Supervisor ||
			choice.Baseline.GetQuestionsEnabled() != entry.Settings.Baseline.Questions ||
			choice.Baseline.GetAutoCompactionEnabled() != entry.Settings.Baseline.AutoCompaction ||
			(choice.Thinking != nil) != (choice.Baseline.Thinking != nil) ||
			(choice.Fast != nil) != (choice.Baseline.Fast != nil) ||
			choice.AutoCompaction.Policy != chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_DISABLED {
			t.Fatalf("choice does not preserve prepared baseline: %+v, entry %+v", choice, entry)
		}
		if choice.Baseline.Thinking != nil && *choice.Baseline.Thinking != entry.Settings.Baseline.Thinking {
			t.Fatalf("Thinking baseline = %s", *choice.Baseline.Thinking)
		}
		if choice.Baseline.Fast != nil && *choice.Baseline.Fast != entry.Settings.Baseline.Fast {
			t.Fatalf("Fast baseline = %v", *choice.Baseline.Fast)
		}
	}
	if !proto.Equal(catalog.InitialSettings, catalog.Choices[0].Baseline) {
		t.Fatalf("initial selection differs from default baseline")
	}
}

func TestNewChatCatalogRejectsIncompleteAgentSelection(t *testing.T) {
	app := testNewChatSettingsApp(t)
	response, err := NewService(launch.Planner{Config: app}, ChatSettingsOwner{}, nil).NewChatSettings(t.Context(), serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if err := protoapi.Validate(response); err != nil {
		t.Fatal(err)
	}
	for _, choice := range response.GetNewChat().Choices {
		if choice.Fast != nil {
			choice.Baseline.Fast = nil
			if err := protoapi.Validate(response); err == nil {
				t.Fatal("catalog accepted a capable Agent without its Fast baseline selection")
			}
			return
		}
	}
	t.Fatal("fixture requires a Fast-capable Agent")
}

func TestNewChatCatalogUsesInvocationCapabilitiesAndSelectedRole(t *testing.T) {
	app := testNewChatSettingsApp(t)
	service := NewService(launch.Planner{Config: app}, ChatSettingsOwner{}, nil)
	response, err := service.NewChatSettings(t.Context(), serverapi.RunPromptOverrides{
		AgentRole: textutil.Value(config.BuiltInSubagentRoleFast),
		Model:     "gpt-5.6-sol", ThinkingLevel: "ultra",
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog := response.GetNewChat()
	if catalog.InitialSettings.AgentRole != config.BuiltInSubagentRoleFast || catalog.InitialSettings.GetThinking() != "ultra" {
		t.Fatalf("invocation selection = %+v", catalog.InitialSettings)
	}
	for _, choice := range catalog.Choices {
		if choice.Agent.Role != config.BuiltInSubagentRoleFast {
			continue
		}
		if choice.Agent.GetModel() != "gpt-5.6-sol" || choice.Thinking == nil || !slices.Contains(choice.Thinking.Values, "ultra") ||
			choice.ConnectionId == nil || *choice.ConnectionId != "test" {
			t.Fatalf("invocation choices = %+v", choice)
		}
		return
	}
	t.Fatal("selected invocation role missing from choices")
}

func TestProjectChatSettingsAuthoritativeReadSemantics(t *testing.T) {
	catalog := testChatSettingsCatalog(t)
	baseline, _ := catalog.Lookup(config.DefaultSubagentRole)

	repaired, err := ProjectChatSettings(ChatSettingsProjectionInput{
		Catalog: catalog,
		Agent:   "removed",
		Settings: session.ChatSettings{
			Supervisor: "all", Thinking: "high", Fast: true, Questions: false,
		},
		WorkflowLocked: true,
	})
	if err != nil {
		t.Fatalf("repair unavailable Agent: %v", err)
	}
	if repaired.SelectedAgent.Role != "default" ||
		repaired.SelectedAgent.Thinking != baseline.Settings.Baseline.Thinking ||
		repaired.AgentEditability != chatsettingspb.Editability_EDITABILITY_WORKFLOW_LOCK ||
		!repaired.AutoCompaction.Effective ||
		repaired.AutoCompaction.Policy != chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_REQUIRED {
		t.Fatalf("repaired projection = %+v", repaired)
	}

	custom, err := ProjectChatSettings(ChatSettingsProjectionInput{
		Catalog: catalog,
		Agent:   "no-questions",
		Settings: session.ChatSettings{
			Supervisor: "edits", Thinking: "provider-depth", Fast: true, Questions: true,
		},
	})
	if err != nil {
		t.Fatalf("project custom settings: %v", err)
	}
	if custom.Thinking == nil ||
		custom.Thinking.Kind != chatsettingspb.ThinkingKind_THINKING_KIND_CUSTOM ||
		custom.Fast == nil || !custom.Fast.Value ||
		custom.Questions.Capable ||
		!custom.Questions.Enabled {
		t.Fatalf("custom projection = %+v", custom)
	}

	locked, err := ProjectChatSettings(ChatSettingsProjectionInput{
		Catalog: catalog,
		Agent:   "historical",
		Settings: session.ChatSettings{
			Supervisor: "off", Thinking: "high", Fast: true, Questions: true,
			AutoCompaction: true,
		},
		Locked: &session.LockedContract{
			Model:        "gpt-6-sol",
			EnabledTools: []string{},
			ProviderContract: session.LockedProviderCapabilities{
				ProviderID: "anthropic",
			},
		},
	})
	if err != nil {
		t.Fatalf("project caching lock: %v", err)
	}
	if locked.SelectedAgent.Role != "historical" ||
		locked.SelectedAgent.Model != "gpt-6-sol" ||
		locked.AgentEditability != chatsettingspb.Editability_EDITABILITY_EDITABLE ||
		locked.AgentLocked || !locked.CachingLocked ||
		locked.Questions.Capable || !locked.Questions.Enabled ||
		locked.Fast == nil || !locked.Fast.Value {
		t.Fatalf("locked projection = %+v", locked)
	}

	disabled, err := ProjectChatSettings(ChatSettingsProjectionInput{
		Catalog:        catalog,
		Agent:          "default",
		Settings:       baseline.Settings.Baseline,
		CompactionMode: config.CompactionModeNone,
	})
	if err != nil {
		t.Fatalf("project disabled compaction: %v", err)
	}
	if disabled.AutoCompaction.Policy != chatsettingspb.AutoCompactionPolicy_AUTO_COMPACTION_POLICY_DISABLED ||
		disabled.AutoCompaction.Effective ||
		disabled.AutoCompaction.Stored != baseline.Settings.Baseline.AutoCompaction ||
		disabled.AutoCompaction.Editability != chatsettingspb.Editability_EDITABILITY_POLICY_DISABLED {
		t.Fatalf("disabled compaction = %+v", disabled.AutoCompaction)
	}
	if got := choiceRoles(disabled.AgentChoices); !slices.Equal(
		got,
		[]string{"default", "fast", "no-questions"},
	) {
		t.Fatalf("choice roles = %v", got)
	}
}

func testChatSettingsCatalog(t *testing.T) launch.PreparedChatAgentCatalog {
	t.Helper()
	catalog, err := launch.PrepareChatAgentCatalog(testChatSettingsApp(t), true, serverapi.RunPromptOverrides{})
	if err != nil {
		t.Fatalf("PrepareChatAgentCatalog: %v", err)
	}
	return catalog
}

func testChatSettingsApp(t *testing.T) config.App {
	settings := config.DefaultOnboardingSettings()
	settings.Model = "gpt-6-sol"
	settings.ThinkingLevel = "medium"
	settings.EnabledTools = map[toolspec.ID]bool{
		toolspec.ToolAskQuestion: true,
		toolspec.ToolExecCommand: true,
	}
	settings.Subagents = map[string]config.SubagentRole{
		config.BuiltInSubagentRoleFast: {
			Settings: config.Settings{Model: "gpt-6-sol", ThinkingLevel: "low"},
			Sources:  map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}, "thinking_level": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "thinking_level"}}},
		},
		"no-questions": {
			Settings: config.Settings{
				EnabledTools: map[toolspec.ID]bool{toolspec.ToolAskQuestion: false},
			},
			Sources: map[string]config.Origin{"tools.ask_question": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "tools.ask_question"}}},
		},
	}
	return testsetup.ProgrammaticConfig(t, settings)
}

func testNewChatSettingsApp(t *testing.T) config.App {
	t.Helper()
	app := loadSessionLaunchTestConfig(t, t.TempDir(), t.TempDir())
	settings := testChatSettingsApp(t).Settings
	app.Settings.Model = settings.Model
	app.Settings.ThinkingLevel = settings.ThinkingLevel
	app.Settings.Subagents = settings.Subagents
	app.Settings.EnabledTools = settings.EnabledTools
	app.Settings.CompactionMode = config.CompactionModeNone
	return app
}

func choiceRoles(choices []*chatsettingspb.AgentChoice) []string {
	roles := make([]string, len(choices))
	for i, choice := range choices {
		roles[i] = choice.Role
	}
	return roles
}
