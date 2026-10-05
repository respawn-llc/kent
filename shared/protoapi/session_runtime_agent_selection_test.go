package protoapi_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/shared/config"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"core/shared/textutil"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestRoleDeclarationsSurviveSettingsTransportUnderEnvironmentOverride(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[subagents.worker]\nmodel = \"declared-model\"\n"), 0o600); err != nil {
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
}

func TestToolSelectionGeneratedOriginValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*sessionlaunchpb.ConfigOrigin)
		valid  bool
	}{
		{name: "CLI", valid: true},
		{name: "environment", valid: true, change: func(o *sessionlaunchpb.ConfigOrigin) {
			o.Source = &sessionlaunchpb.ConfigOrigin_Environment{Environment: "KENT_TOOLS"}
		}},
		{name: "default", change: func(o *sessionlaunchpb.ConfigOrigin) {
			o.Source = &sessionlaunchpb.ConfigOrigin_DefaultValue{DefaultValue: &emptypb.Empty{}}
		}},
		{name: "file", change: func(o *sessionlaunchpb.ConfigOrigin) {
			o.Source = &sessionlaunchpb.ConfigOrigin_File{File: &sessionlaunchpb.ConfigFileSource{Layer: sessionlaunchpb.ConfigFileLayer_CONFIG_FILE_LAYER_GLOBAL, Path: "/config.toml"}}
		}},
		{name: "input", change: func(o *sessionlaunchpb.ConfigOrigin) {
			o.Source = &sessionlaunchpb.ConfigOrigin_Input{Input: &emptypb.Empty{}}
		}},
		{name: "session", change: func(o *sessionlaunchpb.ConfigOrigin) {
			o.Source = &sessionlaunchpb.ConfigOrigin_Session{Session: &emptypb.Empty{}}
		}},
		{name: "wrong key", change: func(o *sessionlaunchpb.ConfigOrigin) { o.Property.Key = "model" }},
		{name: "role", change: func(o *sessionlaunchpb.ConfigOrigin) { o.Property.Role = textutil.Value("worker") }},
		{name: "blank CLI", change: func(o *sessionlaunchpb.ConfigOrigin) {
			o.Source = &sessionlaunchpb.ConfigOrigin_CliOption{CliOption: " "}
		}},
		{name: "blank environment", change: func(o *sessionlaunchpb.ConfigOrigin) {
			o.Source = &sessionlaunchpb.ConfigOrigin_Environment{Environment: "\t"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			origin := &sessionlaunchpb.ConfigOrigin{Property: &sessionlaunchpb.ConfigPropertyAddress{Key: "tools"}, Source: &sessionlaunchpb.ConfigOrigin_CliOption{CliOption: "--tools"}}
			if test.change != nil {
				test.change(origin)
			}
			err := protoapi.Validate(&sessionlaunchpb.ToolSelection{Origin: origin})
			if (err == nil) != test.valid {
				t.Fatalf("generated validation: valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestRuntimeAgentSelectionRolePresence(t *testing.T) {
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
			encoded, err := proto.Marshal(protoapi.SessionRuntimeAgentSelectionToProto(&serverapi.SessionRuntimeAgentSelection{
				AgentRole: test.role,
				Baseline:  serverapi.SessionRuntimeChatSettings{Supervisor: "off", Thinking: "low"},
			}))
			if err != nil {
				t.Fatal(err)
			}
			var wire sessionlaunchpb.SessionRuntimeAgentSelection
			if err := proto.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			selection, err := protoapi.SessionRuntimeAgentSelectionFromProto(&wire)
			if !test.valid {
				if err == nil {
					t.Fatal("present invalid role accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !textutil.EqualOptional(selection.AgentRole, test.role) {
				t.Fatalf("role presence changed: got %v, want %v", selection.AgentRole, test.role)
			}
		})
	}
}
