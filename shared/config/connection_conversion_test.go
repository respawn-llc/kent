package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConvertGlobalConnectionPreservesSettings(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	writeConfigTestFile(t, path, `
model = "local-model"
provider_override = "openai"
openai_base_url = "http://localhost:1234/v1"
provider_identifier = "my-agent"
shell_output_max_chars = 8192
[provider_capabilities]
provider_id = "openai-compatible"
supports_responses_api = true
[shell]
max_concurrent = 3
`)
	selection := LegacyConnectionAnonymous
	changed, err := ConvertGlobalConnections(path, &selection)
	if err != nil || !changed {
		t.Fatalf("convert: changed=%v error=%v", changed, err)
	}
	app := loadConfigTestApp(t, workspace, LoadOptions{})
	connection, err := app.Settings.SelectedConnection()
	if err != nil {
		t.Fatal(err)
	}
	if connection.Protocol != ConnectionResponses || connection.Endpoint == nil || *connection.Endpoint != "http://localhost:1234/v1" || connection.EnvironmentVariable != nil {
		t.Fatalf("converted access = %+v", connection)
	}
	if !connection.Capabilities.SupportsResponsesAPI || connection.Capabilities.ProviderID != "openai-compatible" {
		t.Fatalf("capabilities = %+v", connection.Capabilities)
	}
	if app.Settings.Model != "local-model" || app.Settings.ProviderIdentifier != "my-agent" || app.Settings.ShellOutputMaxChars != 8192 || app.Settings.Shell.MaxConcurrent != 3 {
		t.Fatalf("unrelated settings changed: %+v", app.Settings)
	}
}

func TestConvertGlobalConnectionFailedWritePreservesOriginal(t *testing.T) {
	_, _, path := newConfigTestFile(t)
	body := `provider_override = "openai"`
	writeConfigTestFile(t, path, body)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Error(err)
		}
	})
	selection := LegacyConnectionAnonymous
	if changed, err := ConvertGlobalConnections(path, &selection); err == nil || changed {
		t.Fatalf("failed write reported success: changed=%v error=%v", changed, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != body {
		t.Fatalf("failed write changed original: %v", err)
	}
}

func TestConvertGlobalRoleAndSupervisorConnections(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	writeConfigTestFile(t, path, `
model = "main-model"
provider_override = "openai"
openai_base_url = "http://localhost:1234/v1"
provider_identifier = "test-agent"
[provider_capabilities]
provider_id = "openai-compatible"
supports_responses_api = true
[subagents.equal]
model = "child-model"
openai_base_url = "http://localhost:1234/v1"
[subagents.distinct]
model_context_window = 64000
openai_base_url = "http://localhost:5678/v1"
[reviewer]
model = "supervisor-model"
openai_base_url = "http://localhost:5678/v1"
[reviewer.provider_capabilities]
provider_id = "openai-compatible"
supports_responses_api = true
`)
	selection := LegacyConnectionAnonymous
	if _, err := ConvertGlobalConnections(path, &selection); err != nil {
		t.Fatal(err)
	}
	app := loadConfigTestApp(t, workspace, LoadOptions{})
	equal, _, err := OverlaySubagentRoleSettings(app, app.Settings.Subagents["equal"], true)
	if err != nil {
		t.Fatal(err)
	}
	distinct, _, err := OverlaySubagentRoleSettings(app, app.Settings.Subagents["distinct"], true)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Settings.Connections) != 2 || *app.Settings.Connection != *equal.Connection ||
		*distinct.Connection == *equal.Connection || *distinct.Connection != *app.Settings.Reviewer.Connection {
		t.Fatalf("connections not deduplicated: main=%v equal=%v distinct=%v supervisor=%v catalog=%+v",
			app.Settings.Connection, equal.Connection, distinct.Connection, app.Settings.Reviewer.Connection, app.Settings.Connections)
	}
	if equal.Model != "child-model" || distinct.ModelContextWindow != 64000 || app.Settings.Reviewer.Model != "supervisor-model" || app.Settings.ProviderIdentifier != "test-agent" {
		t.Fatal("conversion changed model, context, or unrelated settings")
	}
}

func TestConvertGlobalEmptyRoleAccessInheritsMain(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	writeConfigTestFile(t, path, `
provider_override = "openai"
openai_base_url = "http://localhost:1234/v1"
[subagents.child]
provider_override = ""
openai_base_url = ""
[reviewer]
provider_override = ""
openai_base_url = ""
`)
	selection := LegacyConnectionAnonymous
	if _, err := ConvertGlobalConnections(path, &selection); err != nil {
		t.Fatal(err)
	}
	app := loadConfigTestApp(t, workspace, LoadOptions{})
	child, _, err := OverlaySubagentRoleSettings(app, app.Settings.Subagents["child"], true)
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Settings.Connections) != 1 || *child.Connection != *app.Settings.Connection || *app.Settings.Reviewer.Connection != *app.Settings.Connection {
		t.Fatal("empty inherited access settings changed the provider endpoint")
	}
}

func TestConvertGlobalConnectionPreservesNamedDefinitions(t *testing.T) {
	for _, endpoint := range []string{"http://localhost:1234/v1", "http://localhost:5678/v1"} {
		t.Run(endpoint, func(t *testing.T) {
			_, workspace, path := newConfigTestFile(t)
			writeConfigTestFile(t, path, `
model = "local-model"
provider_override = "openai"
openai_base_url = "`+endpoint+`"
[connections.openai-1]
protocol = "responses"
endpoint = "http://localhost:1234/v1"
`)
			selection := LegacyConnectionAnonymous
			if _, err := ConvertGlobalConnections(path, &selection); err != nil {
				t.Fatal(err)
			}
			app := loadConfigTestApp(t, workspace, LoadOptions{})
			selected, err := app.Settings.SelectedConnection()
			if err != nil || selected.Endpoint == nil || *selected.Endpoint != endpoint {
				t.Fatalf("selected=%+v error=%v", selected, err)
			}
			wantCount := 2
			if endpoint == "http://localhost:1234/v1" {
				wantCount = 1
			}
			if len(app.Settings.Connections) != wantCount || *app.Settings.Connections["openai-1"].Endpoint != "http://localhost:1234/v1" {
				t.Fatalf("named definitions changed: %+v", app.Settings.Connections)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			changed, err := ConvertGlobalConnections(path, &selection)
			after, readErr := os.ReadFile(path)
			if err != nil || readErr != nil || changed || !reflect.DeepEqual(before, after) {
				t.Fatalf("already-converted file rewritten: %v %v %v", changed, err, readErr)
			}
		})
	}
}

func TestConvertGlobalConnectionRejectsAmbiguousAccessWithoutWriting(t *testing.T) {
	anonymous, oauth, apiKey := LegacyConnectionAnonymous, LegacyConnectionOAuth, LegacyConnectionAPIKey
	for _, tc := range []struct {
		name      string
		body      string
		selection *LegacyConnectionAuth
	}{
		{"conflicting reference", `connection = "local"
provider_override = "openai"
openai_base_url = "http://localhost:5678/v1"
[connections.local]
protocol = "responses"
endpoint = "http://localhost:1234/v1"
`, &anonymous},
		{"unknown auth", `provider_override = "openai"`, nil},
		{"missing explicit key reference", `provider_override = "openai"`, &apiKey},
		{"unsupported provider", `provider_override = "anthropic"`, &anonymous},
		{"invalid endpoint", `openai_base_url = "not-a-url"`, &anonymous},
		{"subscription with endpoint", `openai_base_url = "http://localhost:1234/v1"`, &oauth},
		{"malformed file", `provider_override = [`, &anonymous},
		{"malformed declaration", `provider_override = 123`, &anonymous},
		{"malformed capabilities", `[provider_capabilities]
supports_responses_api = true`, &anonymous},
		{"conflicting role", `provider_override = "openai"
[connections.existing]
protocol = "responses"
endpoint = "http://localhost:1234/v1"
[subagents.child]
connection = "existing"
openai_base_url = "http://localhost:5678/v1"`, &anonymous},
		{"unsupported supervisor", `provider_override = "openai"
[reviewer]
provider_override = "anthropic"`, &anonymous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, path := newConfigTestFile(t)
			writeConfigTestFile(t, path, tc.body)
			if changed, err := ConvertGlobalConnections(path, tc.selection); err == nil || changed {
				t.Fatalf("ambiguous conversion accepted: %v %v", changed, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != tc.body {
				t.Fatalf("original file changed: %v", err)
			}
		})
	}
}

func TestWorkspaceConnectionConversionRequiresManualEdit(t *testing.T) {
	for _, filename := range []string{"config.toml", "config.local.toml"} {
		t.Run(filename, func(t *testing.T) {
			_, workspace, global := newConfigTestFile(t)
			writeConfigTestFile(t, global, `model = "local-model"`)
			path := filepath.Join(workspace, ConfigDirName, filename)
			body := "[subagents.child]\nopenai_base_url = \"http://localhost:1234/v1\"\n"
			writeConfigTestFile(t, path, body)
			_, err := Load(workspace, workspace, LoadOptions{})
			var remediation *ConnectionConversionRequiredError
			if !errors.As(err, &remediation) || remediation.Path != path || !reflect.DeepEqual(remediation.Keys, []string{"subagents.child.openai_base_url"}) {
				t.Fatalf("missing source-scoped remediation: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != body {
				t.Fatalf("workspace settings rewritten: %v", err)
			}
		})
	}
}

func TestConvertGlobalAPIConnectionPreservesExplicitReference(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	writeConfigTestFile(t, path, `
connection = "api"
provider_override = "openai"
openai_base_url = "http://localhost:1234/v1"
[connections.api]
protocol = "responses"
endpoint = "http://localhost:1234/v1"
environment_variable = "MY_SERVER_KEY"
[subagents.child]
openai_base_url = "http://localhost:5678/v1"
`)
	selection := LegacyConnectionAPIKey
	if _, err := ConvertGlobalConnections(path, &selection); err != nil {
		t.Fatal(err)
	}
	app := loadConfigTestApp(t, workspace, LoadOptions{})
	connection, err := app.Settings.SelectedConnection()
	if err != nil || connection.EnvironmentVariable == nil || *connection.EnvironmentVariable != "MY_SERVER_KEY" || *app.Settings.Connection != "api" {
		t.Fatalf("explicit reference changed: %+v %v", connection, err)
	}
	child, _, err := OverlaySubagentRoleSettings(app, app.Settings.Subagents["child"], true)
	if err != nil {
		t.Fatal(err)
	}
	access, err := child.SelectedConnection()
	if err != nil || *access.EnvironmentVariable != "MY_SERVER_KEY" || *access.Endpoint != "http://localhost:5678/v1" || len(app.Settings.Connections) != 2 {
		t.Fatalf("inherited API access changed: %+v %v", access, err)
	}
}
