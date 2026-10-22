package protoapi_test

import (
	configurationpb "core/shared/protoapi/gen/kent/api/configuration"
	"os"
	"path/filepath"
	"testing"

	"core/shared/config"
	"core/shared/protoapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestSourceReportRequiresUniqueSourceKeys(t *testing.T) {
	fact := testConfigSourceFact()
	report := &configurationpb.SourceReport{Sources: []*configurationpb.SourceFact{fact}}
	if err := protoapi.Validate(report); err != nil {
		t.Fatal(err)
	}
	report.Sources = append(report.Sources, proto.Clone(fact).(*configurationpb.SourceFact))
	if err := protoapi.Validate(report); err == nil {
		t.Fatal("duplicate source keys accepted")
	}
}

func TestSubagentRoleRequiresUniqueSourceKeys(t *testing.T) {
	settings := testSessionSettings(t)
	fact := testConfigSourceFact()
	role := &configurationpb.SubagentRole{Settings: settings, Sources: []*configurationpb.SourceFact{fact}}
	if err := protoapi.Validate(role); err != nil {
		t.Fatal(err)
	}
	role.Sources = append(role.Sources, proto.Clone(fact).(*configurationpb.SourceFact))
	if err := protoapi.Validate(role); err == nil {
		t.Fatal("duplicate subagent source keys accepted")
	}
}

func testSessionSettings(t *testing.T) *configurationpb.Settings {
	t.Helper()
	workspace := t.TempDir()
	app, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := protoapi.SessionSettingsToProto(app.Settings)
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestSessionSettingsRequiresUniqueEnabledTools(t *testing.T) {
	settings := testSessionSettings(t)
	if len(settings.EnabledTools) == 0 {
		t.Fatal("enabled-tool fixture is empty")
	}
	settings.EnabledTools = append(settings.EnabledTools, proto.Clone(settings.EnabledTools[0]).(*configurationpb.ToolEnabledFact))
	if err := protoapi.Validate(settings); err == nil {
		t.Fatal("duplicate enabled tool IDs accepted")
	}
}

func TestSessionSettingsRequiresUniqueSkillToggles(t *testing.T) {
	settings := testSessionSettings(t)
	fact := &configurationpb.BooleanFact{Key: "skill", Value: true}
	settings.SkillToggles = []*configurationpb.BooleanFact{fact}
	if err := protoapi.Validate(settings); err != nil {
		t.Fatal(err)
	}
	settings.SkillToggles = append(settings.SkillToggles, proto.Clone(fact).(*configurationpb.BooleanFact))
	if err := protoapi.Validate(settings); err == nil {
		t.Fatal("duplicate skill keys accepted")
	}
}

func TestSessionSettingsRequiresUniqueSubagentNames(t *testing.T) {
	nested := testSessionSettings(t)
	settings := proto.Clone(nested).(*configurationpb.Settings)
	role := &configurationpb.NamedSubagentRole{
		Name: "assistant", Role: &configurationpb.SubagentRole{Settings: nested},
	}
	settings.Subagents = []*configurationpb.NamedSubagentRole{role}
	if err := protoapi.Validate(settings); err != nil {
		t.Fatal(err)
	}
	settings.Subagents = append(settings.Subagents, proto.Clone(role).(*configurationpb.NamedSubagentRole))
	if err := protoapi.Validate(settings); err == nil {
		t.Fatal("duplicate subagent names accepted")
	}
}

func TestSessionSettingsConnectionIdentityContract(t *testing.T) {
	settings := testSessionSettings(t)
	for _, id := range []string{"local", "local_1-server", "a", "1local", "Local", "local host", "local/", "élan", ""} {
		t.Run(id, func(t *testing.T) {
			settings.Connection = &id
			_, sourceErr := config.ParseConnectionID(id)
			if err := protoapi.Validate(settings); (err == nil) != (sourceErr == nil) {
				t.Fatalf("connection acceptance differs from source grammar: %v", err)
			}
		})
	}
}

func TestReviewerConnectionIdentityContract(t *testing.T) {
	settings := testSessionSettings(t)
	for _, id := range []string{"local", "local_1-server", "1local", "Local", "local host", "élan", ""} {
		t.Run(id, func(t *testing.T) {
			settings.Reviewer.Connection = &id
			_, sourceErr := config.ParseConnectionID(id)
			if err := protoapi.Validate(settings); (err == nil) != (sourceErr == nil) {
				t.Fatalf("reviewer connection acceptance differs from source grammar: %v", err)
			}
		})
	}
}

func TestSubagentConnectionUsesRoleSelection(t *testing.T) {
	role := &configurationpb.SubagentRole{Settings: testSessionSettings(t)}
	if err := protoapi.Validate(role); err != nil {
		t.Fatal(err)
	}
	connection := "local"
	role.Settings.Connection = &connection
	if err := protoapi.Validate(role); err == nil {
		t.Fatal("subagent Settings connection bypassed role selection")
	}
}

func testConfigSourceFact() *configurationpb.SourceFact {
	return &configurationpb.SourceFact{
		Key: "model",
		Origin: &configurationpb.ConfigOrigin{
			Property: &configurationpb.ConfigPropertyAddress{Key: "model"},
			Source:   &configurationpb.ConfigOrigin_DefaultValue{DefaultValue: &emptypb.Empty{}},
		},
	}
}

func TestSourceReportRequiresUniqueFileLayers(t *testing.T) {
	first := &configurationpb.ConfigFileReport{
		File: &configurationpb.ConfigFileSource{
			Layer: configurationpb.ConfigFileLayer_CONFIG_FILE_LAYER_GLOBAL,
			Path:  "/config.toml",
		},
	}
	report := &configurationpb.SourceReport{Files: []*configurationpb.ConfigFileReport{first}}
	if err := protoapi.Validate(report); err != nil {
		t.Fatal(err)
	}
	report.Files = append(report.Files, proto.Clone(first).(*configurationpb.ConfigFileReport))
	if err := protoapi.Validate(report); err == nil {
		t.Fatal("duplicate source file layers accepted")
	}
}

func TestNativeProgressSettingSurvivesSessionSettingsTransport(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		enabled bool
		trigger int
	}{
		{name: "default", enabled: true, trigger: 176_000},
		{name: "enabled", content: "tui_native_progress_bar = true\n", enabled: true, trigger: 176_000},
		{name: "disabled", content: "tui_native_progress_bar = false\n", trigger: 176_000},
		{name: "explicit lead", content: "pre_submit_compaction_lead_tokens = 35000\n", enabled: true, trigger: 165_000},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, workspace := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			app, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			wire, err := protoapi.SessionSettingsToProto(app.Settings)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := proto.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			received := new(configurationpb.Settings)
			if err := proto.Unmarshal(payload, received); err != nil {
				t.Fatal(err)
			}
			settings, err := protoapi.SessionSettingsFromProto(received)
			if err != nil {
				t.Fatal(err)
			}
			if settings.TUINativeProgressBar != test.enabled {
				t.Fatalf("native progress enabled after settings transport = %t, want %t", settings.TUINativeProgressBar, test.enabled)
			}
			if got := config.SpeculativeCompactionTriggerTokens(200_000, settings.PreSubmitCompactionLeadTokens); got != test.trigger {
				t.Fatalf("speculative trigger after settings transport = %d, want %d", got, test.trigger)
			}
		})
	}
}
