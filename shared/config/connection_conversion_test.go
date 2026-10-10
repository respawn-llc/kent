package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
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

func TestConvertGlobalConnectionPreservesAuthoredSource(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := `# Keep this user-authored context.
model = 'local-model' # preserve this spacing and quote
provider_identifier = "my-agent" # preserve this unrelated setting
provider_override = "openai"
openai_base_url = 'http://localhost:1234/v1'
`
	unchanged := `# Keep this user-authored context.
model = 'local-model' # preserve this spacing and quote
provider_identifier = "my-agent" # preserve this unrelated setting
`
	writeConfigTestFile(t, path, source)

	selection := LegacyConnectionAnonymous
	changed, err := ConvertGlobalConnections(path, &selection)
	if err != nil || !changed {
		t.Fatalf("convert: changed=%v error=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < len(unchanged) || string(got[:len(unchanged)]) != unchanged {
		t.Fatalf("conversion changed unrelated authored source: %q", got)
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	connection, err := app.Settings.SelectedConnection()
	if err != nil {
		t.Fatal(err)
	}
	if connection.Protocol != ConnectionResponses || connection.Endpoint == nil ||
		*connection.Endpoint != "http://localhost:1234/v1" || app.Settings.Model != "local-model" ||
		app.Settings.ProviderIdentifier != "my-agent" {
		t.Fatalf("converted settings = connection:%+v model:%q provider:%q", connection, app.Settings.Model, app.Settings.ProviderIdentifier)
	}
}

func TestConvertGlobalConnectionNoopPreservesUTF8BOM(t *testing.T) {
	_, _, path := newConfigTestFile(t)
	source := "\uFEFF" + `connection = 'existing'
[connections.existing]
protocol = 'responses'
endpoint = "http://localhost:1234/v1"
`
	writeConfigTestFile(t, path, source)

	selection := LegacyConnectionAnonymous
	changed, err := ConvertGlobalConnections(path, &selection)
	if err != nil || changed {
		t.Fatalf("convert already-current file: changed=%v error=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != source {
		t.Fatalf("no-op conversion changed the authored source: got %q want %q", got, source)
	}
}

func TestConvertGlobalConnectionsPreservesConfiguredSets(t *testing.T) {
	anonymous, apiKey, oauth := LegacyConnectionAnonymous, LegacyConnectionAPIKey, LegacyConnectionOAuth
	for _, auth := range []*LegacyConnectionAuth{nil, &anonymous, &apiKey, &oauth} {
		_, workspace, path := newConfigTestFile(t)
		source := `connection = ["qa-a", "qa-z", "qa-a", "unknown"]
model = "local-model"
[connections.qa-z]
protocol = "responses"
endpoint = "http://localhost:8000/v1"
[connections.qa-a]
protocol = "responses"
endpoint = "http://localhost:8000/v1"
[subagents.worker]
connection = ["qa-a", "qa-z"]
model = "local-model"
`
		writeConfigTestFile(t, path, source)
		changed, err := ConvertGlobalConnections(path, auth)
		if err != nil || changed {
			t.Fatalf("convert configured sets with auth %v: changed=%v error=%v", auth, changed, err)
		}
		app := loadConfigTestApp(t, workspace, LoadOptions{})
		members, err := app.Settings.ConnectionMembers()
		if err != nil || !reflect.DeepEqual(members, []ConnectionID{"qa-z", "qa-a"}) {
			t.Fatalf("loaded members = %v, %v", members, err)
		}
		if !reflect.DeepEqual(*app.Settings.Subagents["worker"].Settings.Connection, ConnectionSelection{"qa-a", "qa-z"}) {
			t.Fatal("role selection changed during startup conversion")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != source {
			t.Fatalf("conversion modified authored configuration: %v", err)
		}
	}
}

func TestConvertGlobalRoleAndReviewerConnectionsPreservesAuthoredSource(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := `# Root config retains its authored representation.
model = 'main-model' # keep the model source
connection = 'base'

[connections.base]
protocol = 'responses'
endpoint = "http://localhost:1234/v1" # named endpoint note

# Supervisor preferences remain in place.
[reviewer]
model = 'supervisor-model' # preserve this setting
openai_base_url = "http://localhost:5678/v1"

# Role-specific preferences remain in place.
[subagents.child]
model = 'child-model' # preserve this setting
openai_base_url = 'http://localhost:9012/v1'
`
	prefix := `# Root config retains its authored representation.
model = 'main-model' # keep the model source
connection = 'base'

[connections.base]
protocol = 'responses'
endpoint = "http://localhost:1234/v1" # named endpoint note
`
	writeConfigTestFile(t, path, source)

	selection := LegacyConnectionAnonymous
	changed, err := ConvertGlobalConnections(path, &selection)
	if err != nil || !changed {
		t.Fatalf("convert: changed=%v error=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < len(prefix) || string(got[:len(prefix)]) != prefix {
		t.Fatalf("role conversion changed unrelated authored source: %q", got)
	}
	for _, preserved := range []string{
		"# Supervisor preferences remain in place.\n[reviewer]\nmodel = 'supervisor-model' # preserve this setting\n",
		"# Role-specific preferences remain in place.\n[subagents.child]\nmodel = 'child-model' # preserve this setting\n",
	} {
		if !strings.Contains(string(got), preserved) {
			t.Fatalf("role conversion changed surviving authored scope source %q: %q", preserved, got)
		}
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	if app.Settings.Model != "main-model" || app.Settings.Reviewer.Model != "supervisor-model" ||
		app.Settings.Subagents["child"].Settings.Model != "child-model" {
		t.Fatal("role conversion changed unrelated model settings")
	}
	if app.Settings.Reviewer.Connection == nil {
		t.Fatal("Supervisor connection was not converted")
	}
	supervisorConnection := app.Settings.Connections[(*app.Settings.Reviewer.Connection)[0]]
	if supervisorConnection.Endpoint == nil || *supervisorConnection.Endpoint != "http://localhost:5678/v1" {
		t.Fatalf("Supervisor connection = %+v", supervisorConnection)
	}
	child, _, err := OverlaySubagentRoleSettings(app, app.Settings.Subagents["child"], true)
	if err != nil {
		t.Fatal(err)
	}
	childConnection, err := child.SelectedConnection()
	if err != nil || childConnection.Endpoint == nil || *childConnection.Endpoint != "http://localhost:9012/v1" {
		t.Fatalf("child connection = %+v error=%v", childConnection, err)
	}
	if len(app.Settings.Connections) != 3 {
		t.Fatalf("converted connection catalog has %d entries: %+v", len(app.Settings.Connections), app.Settings.Connections)
	}
}

func TestConvertGlobalConnectionReusesNamedDefinitionSource(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := `connection  = 'openai-1' # keep this selected reference
provider_override = "openai"
openai_base_url = 'http://localhost:1234/v1'

# Keep the named definition and its documentation.
[connections.openai-1]
protocol = 'responses'
endpoint   = "http://localhost:1234/v1" # user-authored endpoint note
`
	prefix := `connection  = 'openai-1' # keep this selected reference
`
	suffix := `# Keep the named definition and its documentation.
[connections.openai-1]
protocol = 'responses'
endpoint   = "http://localhost:1234/v1" # user-authored endpoint note
`
	writeConfigTestFile(t, path, source)

	selection := LegacyConnectionAnonymous
	changed, err := ConvertGlobalConnections(path, &selection)
	if err != nil || !changed {
		t.Fatalf("convert: changed=%v error=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < len(prefix)+len(suffix) || string(got[:len(prefix)]) != prefix ||
		string(got[len(got)-len(suffix):]) != suffix {
		t.Fatalf("conversion changed the selected reference or reused definition: %q", got)
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	connection, err := app.Settings.SelectedConnection()
	if err != nil || app.Settings.Connection == nil || !reflect.DeepEqual(*app.Settings.Connection, ConnectionSelection{"openai-1"}) ||
		connection.Protocol != ConnectionResponses || connection.Endpoint == nil ||
		*connection.Endpoint != "http://localhost:1234/v1" || len(app.Settings.Connections) != 1 {
		t.Fatalf("named definition was not reused: connection=%+v settings=%+v error=%v", connection, app.Settings, err)
	}
}

func TestConvertGlobalRoleSupervisorConnectionPreservesAuthoredSource(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := `connection = 'base'
[connections.base]
protocol = 'responses'
endpoint = "http://localhost:1234/v1" # retain the base endpoint

# Keep root Supervisor preferences.
[reviewer]
model = 'supervisor-model' # preserve this model
openai_base_url = "http://localhost:5678/v1"

# Keep the child role preferences.
[subagents.child]
model = 'child-model' # preserve this model
openai_base_url = "http://localhost:6789/v1"

# Keep child Supervisor preferences.
[subagents.child.reviewer]
model = 'child-supervisor-model' # preserve this model
openai_base_url = "http://localhost:7890/v1"
`
	prefix := `connection = 'base'
[connections.base]
protocol = 'responses'
endpoint = "http://localhost:1234/v1" # retain the base endpoint
`
	writeConfigTestFile(t, path, source)

	selection := LegacyConnectionAnonymous
	changed, err := ConvertGlobalConnections(path, &selection)
	if err != nil || !changed {
		t.Fatalf("convert: changed=%v error=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < len(prefix) || string(got[:len(prefix)]) != prefix {
		t.Fatalf("role Supervisor conversion changed unrelated authored source: %q", got)
	}
	for _, preserved := range []string{
		"# Keep root Supervisor preferences.\n[reviewer]\nmodel = 'supervisor-model' # preserve this model\n",
		"# Keep the child role preferences.\n[subagents.child]\nmodel = 'child-model' # preserve this model\n",
		"# Keep child Supervisor preferences.\n[subagents.child.reviewer]\nmodel = 'child-supervisor-model' # preserve this model\n",
	} {
		if !strings.Contains(string(got), preserved) {
			t.Fatalf("role Supervisor conversion changed surviving authored scope source %q: %q", preserved, got)
		}
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	if app.Settings.Reviewer.Model != "supervisor-model" {
		t.Fatalf("root Supervisor model = %q", app.Settings.Reviewer.Model)
	}
	if app.Settings.Reviewer.Connection == nil {
		t.Fatal("root Supervisor connection was not converted")
	}
	supervisorConnection := app.Settings.Connections[(*app.Settings.Reviewer.Connection)[0]]
	if supervisorConnection.Endpoint == nil || *supervisorConnection.Endpoint != "http://localhost:5678/v1" {
		t.Fatalf("root Supervisor connection = %+v", supervisorConnection)
	}

	child, _, err := OverlaySubagentRoleSettings(app, app.Settings.Subagents["child"], true)
	if err != nil {
		t.Fatal(err)
	}
	if child.Model != "child-model" || child.Reviewer.Model != "child-supervisor-model" {
		t.Fatalf("child settings changed: %+v", child)
	}
	if child.Connection == nil || child.Reviewer.Connection == nil {
		t.Fatalf("child or child Supervisor connection was not converted: %+v", child)
	}
	childConnection := app.Settings.Connections[(*child.Connection)[0]]
	childReviewerConnection := app.Settings.Connections[(*child.Reviewer.Connection)[0]]
	if childConnection.Endpoint == nil || *childConnection.Endpoint != "http://localhost:6789/v1" ||
		childReviewerConnection.Endpoint == nil || *childReviewerConnection.Endpoint != "http://localhost:7890/v1" {
		t.Fatalf("child connections = role:%+v Supervisor:%+v", childConnection, childReviewerConnection)
	}
	if len(app.Settings.Connections) != 4 {
		t.Fatalf("converted connection catalog has %d entries: %+v", len(app.Settings.Connections), app.Settings.Connections)
	}
}

func TestConvertGlobalConnectionRemovesCapabilityTableAtEOF(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	prefix := `# Keep this selected reference and inline catalog.
connection  = 'openai-1' # preserve its quote and spacing
connections = { "openai-1" = { protocol = 'responses', endpoint = "http://localhost:1234/v1", provider_capabilities = { provider_id = 'openai-compatible', supports_responses_api = true } } } # preserve catalog note
model = 'local-model' # preserve model source
`
	shellSource := `# This unrelated comment remains attached to the shell settings.
[shell]
max_concurrent = 3 # preserve shell source
`
	source := prefix + `# This comment is attached to the removed provider setting.
provider_override = "openai" # old provider selection
openai_base_url = "http://localhost:1234/v1"

` + shellSource + `
# This documentation is attached to the removed capability table.
[provider_capabilities] # old capabilities
provider_id = 'openai-compatible'
supports_responses_api = true
`
	writeConfigTestFile(t, path, source)

	selection := LegacyConnectionAnonymous
	changed, err := ConvertGlobalConnections(path, &selection)
	if err != nil || !changed {
		t.Fatalf("convert: changed=%v error=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < len(prefix) || string(got[:len(prefix)]) != prefix || !strings.Contains(string(got), shellSource) {
		t.Fatalf("conversion changed unrelated source around the removed capability table: %q", got)
	}
	var persisted map[string]any
	if _, err := toml.Decode(string(got), &persisted); err != nil {
		t.Fatal(err)
	}
	for _, key := range legacyAccessKeys {
		if _, present := persisted[key]; present {
			t.Fatalf("legacy root setting %q remains after conversion", key)
		}
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	connection, err := app.Settings.SelectedConnection()
	if err != nil || !reflect.DeepEqual(*app.Settings.Connection, ConnectionSelection{"openai-1"}) ||
		connection.Endpoint == nil || *connection.Endpoint != "http://localhost:1234/v1" ||
		connection.Capabilities.ProviderID != "openai-compatible" || !connection.Capabilities.SupportsResponsesAPI {
		t.Fatalf("converted connection = %+v error=%v", connection, err)
	}
	if app.Settings.Model != "local-model" || app.Settings.Shell.MaxConcurrent != 3 ||
		len(app.Settings.Connections) != 1 {
		t.Fatalf("unrelated settings or named definitions changed: %+v", app.Settings)
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
	if len(app.Settings.Connections) != 2 || !reflect.DeepEqual(app.Settings.Connection, equal.Connection) ||
		reflect.DeepEqual(distinct.Connection, equal.Connection) || !reflect.DeepEqual(distinct.Connection, app.Settings.Reviewer.Connection) {
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
	if len(app.Settings.Connections) != 1 || !reflect.DeepEqual(child.Connection, app.Settings.Connection) || !reflect.DeepEqual(app.Settings.Reviewer.Connection, app.Settings.Connection) {
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
	if err != nil || connection.EnvironmentVariable == nil || *connection.EnvironmentVariable != "MY_SERVER_KEY" || !reflect.DeepEqual(*app.Settings.Connection, ConnectionSelection{"api"}) {
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
