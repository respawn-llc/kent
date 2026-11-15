package protoapi_test

import (
	configurationpb "core/shared/protoapi/gen/kent/api/configuration"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/shared/config"
	"core/shared/protoapi"
	invocationpb "core/shared/protoapi/gen/kent/api/invocation"
	"core/shared/serverapi"
	"core/shared/textutil"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestIdleResumePolicySurvivesRuntimeSettingsTransport(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	app, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, seconds := range []int{3600, 2, 0, 31_536_000} {
		settings := app.Settings
		settings.IdleResumeReminderSeconds = seconds
		wire, err := protoapi.SessionSettingsToProto(settings)
		if err != nil {
			t.Fatal(err)
		}
		data, err := proto.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		received := &configurationpb.Settings{}
		if err := proto.Unmarshal(data, received); err != nil {
			t.Fatal(err)
		}
		decoded, err := protoapi.SessionSettingsFromProto(received)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.IdleResumeReminderSeconds != seconds {
			t.Fatalf("runtime threshold after transport = %d, want %d", decoded.IdleResumeReminderSeconds, seconds)
		}
	}
}

func TestIdleResumeSettingsTransportRejectsOutOfRangeThreshold(t *testing.T) {
	for _, seconds := range []int64{-1, 31_536_001} {
		wire := testSessionSettings(t)
		wire.IdleResumeReminderSeconds = seconds
		if err := protoapi.Validate(wire); err == nil {
			t.Fatalf("out-of-range idle threshold %d accepted by Settings contract", seconds)
		}
	}
}

func TestRoleDeclarationsSurviveSettingsTransportUnderEnvironmentOverride(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[subagents.worker]\nmodel = \"declared-model\"\nconnection = [\"a\", \"b\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KENT_MODEL", "environment-model")
	app, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := protoapi.SessionSettingsToProto(app.Settings)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := protoapi.SessionSettingsFromProto(wire)
	if err != nil {
		t.Fatal(err)
	}
	role := decoded.Subagents["worker"]
	if decoded.Model != "environment-model" || role.Settings.Model != "declared-model" {
		t.Fatalf("transport changed root/declared models: %s / %s", decoded.Model, role.Settings.Model)
	}
	if !reflect.DeepEqual(role.Sources, app.Settings.Subagents["worker"].Sources) {
		t.Fatal("transport changed declaration origins or materialized omitted declarations")
	}
	if !reflect.DeepEqual(role.Settings.Connection, app.Settings.Subagents["worker"].Settings.Connection) {
		t.Fatalf("configured role selection was lost: %v", role.Settings.Connection)
	}
}

func TestToolSelectionGeneratedOriginValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*configurationpb.ConfigOrigin)
		valid  bool
	}{
		{name: "CLI", valid: true},
		{name: "environment", valid: true, change: func(o *configurationpb.ConfigOrigin) {
			o.Source = &configurationpb.ConfigOrigin_Environment{Environment: "KENT_TOOLS"}
		}},
		{name: "default", change: func(o *configurationpb.ConfigOrigin) {
			o.Source = &configurationpb.ConfigOrigin_DefaultValue{DefaultValue: &emptypb.Empty{}}
		}},
		{name: "file", change: func(o *configurationpb.ConfigOrigin) {
			o.Source = &configurationpb.ConfigOrigin_File{File: &configurationpb.ConfigFileSource{Layer: configurationpb.ConfigFileLayer_CONFIG_FILE_LAYER_GLOBAL, Path: "/config.toml"}}
		}},
		{name: "input", change: func(o *configurationpb.ConfigOrigin) {
			o.Source = &configurationpb.ConfigOrigin_Input{Input: &emptypb.Empty{}}
		}},
		{name: "session", change: func(o *configurationpb.ConfigOrigin) {
			o.Source = &configurationpb.ConfigOrigin_Session{Session: &emptypb.Empty{}}
		}},
		{name: "wrong key", change: func(o *configurationpb.ConfigOrigin) { o.Property.Key = "model" }},
		{name: "role", change: func(o *configurationpb.ConfigOrigin) { o.Property.Role = textutil.Value("worker") }},
		{name: "blank CLI", change: func(o *configurationpb.ConfigOrigin) {
			o.Source = &configurationpb.ConfigOrigin_CliOption{CliOption: " "}
		}},
		{name: "blank environment", change: func(o *configurationpb.ConfigOrigin) {
			o.Source = &configurationpb.ConfigOrigin_Environment{Environment: "\t"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			origin := &configurationpb.ConfigOrigin{Property: &configurationpb.ConfigPropertyAddress{Key: "tools"}, Source: &configurationpb.ConfigOrigin_CliOption{CliOption: "--tools"}}
			if test.change != nil {
				test.change(origin)
			}
			err := protoapi.Validate(&configurationpb.ToolSelection{Origin: origin})
			if (err == nil) != test.valid {
				t.Fatalf("generated validation: valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestInvocationAgentRolePresence(t *testing.T) {
	for _, test := range []struct {
		name  string
		role  *string
		valid bool
	}{
		{name: "interactive base", valid: true},
		{name: "headless default", role: textutil.Value("default"), valid: true},
		{name: "named role", role: textutil.Value("worker"), valid: true},
		{name: "empty role", role: textutil.Value("")},
		{name: "blank role", role: textutil.Value(" ")},
	} {
		t.Run(test.name, func(t *testing.T) {
			invocation, err := protoapi.RunPromptOverridesToProto(serverapi.RunPromptOverrides{AgentRole: test.role})
			if !test.valid {
				if err == nil {
					t.Fatal("present invalid role accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := proto.Marshal(invocation)
			if err != nil {
				t.Fatal(err)
			}
			var wire invocationpb.RunPromptOverrides
			if err := proto.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			err = protoapi.Validate(&wire)
			if err != nil {
				t.Fatal(err)
			}
			selection, err := protoapi.RunPromptOverridesFromProto(&wire)
			if err != nil {
				t.Fatal(err)
			}
			if !textutil.EqualOptional(selection.AgentRole, test.role) {
				t.Fatalf("role presence changed: got %v, want %v", selection.AgentRole, test.role)
			}
		})
	}
}
