package protoapi_test

import (
	"os"
	"path/filepath"
	"testing"

	"core/shared/config"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"

	"google.golang.org/protobuf/proto"
)

func TestNativeProgressSettingSurvivesSessionSettingsTransport(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		enabled bool
	}{
		{name: "default", enabled: true},
		{name: "enabled", content: "tui_native_progress_bar = true\n", enabled: true},
		{name: "disabled", content: "tui_native_progress_bar = false\n"},
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
			received := new(sessionlaunchpb.Settings)
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
		})
	}
}
