package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSetProviderConnectionEnvironmentPreservesAuthoredSource(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := `# Keep the file's authored layout.
connection   = 'api' # selected connection
model = "gpt-4o" # unrelated setting

# API connection notes stay attached to the definition.
[connections.api] # connection header note
protocol = 'responses'
endpoint = "http://localhost:1234/v1" # local endpoint
environment_variable   = 'OLD_KEY' # key reference

[shell]
max_concurrent = 3
`
	want := `# Keep the file's authored layout.
connection   = 'api' # selected connection
model = "gpt-4o" # unrelated setting

# API connection notes stay attached to the definition.
[connections.api] # connection header note
protocol = 'responses'
endpoint = "http://localhost:1234/v1" # local endpoint
environment_variable   = 'NEW_KEY' # key reference

[shell]
max_concurrent = 3
`
	writeConfigTestFile(t, path, source)

	if err := SetProviderConnectionEnvironment(path, "api", "NEW_KEY"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("saved TOML differs from the authored source outside the requested reference:\n got: %q\nwant: %q", got, want)
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	connection, present := app.Settings.Connections["api"]
	if !present || connection.EnvironmentVariable == nil || *connection.EnvironmentVariable != "NEW_KEY" {
		t.Fatalf("loaded connection environment reference = %+v", connection.EnvironmentVariable)
	}
	if app.Settings.Model != "gpt-4o" || app.Settings.Shell.MaxConcurrent != 3 {
		t.Fatalf("unrelated settings changed: model=%q shell=%+v", app.Settings.Model, app.Settings.Shell)
	}
}

func TestSetDefaultProviderConnectionPreservesAuthoredSource(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := `# Preserve the selected connection's context.
connection  = 'first' # selected connection

[connections.first]
protocol = "responses"
endpoint = "http://localhost:1234/v1"

[connections.second]
protocol = 'responses'
endpoint = "http://localhost:5678/v1" # second endpoint
`
	want := `# Preserve the selected connection's context.
connection  = 'second' # selected connection

[connections.first]
protocol = "responses"
endpoint = "http://localhost:1234/v1"

[connections.second]
protocol = 'responses'
endpoint = "http://localhost:5678/v1" # second endpoint
`
	writeConfigTestFile(t, path, source)

	if err := SetDefaultProviderConnection(path, "second"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("saved TOML differs from the authored source outside the selected reference:\n got: %q\nwant: %q", got, want)
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	if app.Settings.Connection == nil || !reflect.DeepEqual(*app.Settings.Connection, ConnectionSelection{"second"}) {
		t.Fatalf("loaded default connection = %v", app.Settings.Connection)
	}
}

func TestSetDefaultProviderConnectionPreservesUTF8BOM(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := "\uFEFF" + `connection = 'first'
[connections.first]
protocol = 'responses'
endpoint = "http://localhost:1234/v1"

[connections.second]
protocol = 'responses'
endpoint = "http://localhost:5678/v1"
`
	want := strings.Replace(source, "connection = 'first'", "connection = 'second'", 1)
	writeConfigTestFile(t, path, source)

	if err := SetDefaultProviderConnection(path, "second"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("saved TOML differs from the authored source outside the selected reference:\n got: %q\nwant: %q", got, want)
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	if app.Settings.Connection == nil || !reflect.DeepEqual(*app.Settings.Connection, ConnectionSelection{"second"}) {
		t.Fatalf("loaded default connection = %v", app.Settings.Connection)
	}
}

func TestSetProviderConnectionEnvironmentSameValueDoesNotRewrite(t *testing.T) {
	_, _, path := newConfigTestFile(t)
	source := `connection = 'api'
[connections.api]
protocol = 'responses'
endpoint = "http://localhost:1234/v1"
environment_variable = 'OLD_KEY' # keep this authored reference
`
	writeConfigTestFile(t, path, source)

	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetProviderConnectionEnvironment(path, "api", "OLD_KEY"); err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != source || !os.SameFile(beforeInfo, afterInfo) {
		t.Fatalf("same-value reference edit rewrote the source: %q", after)
	}
}

func TestSetDefaultProviderConnectionSameValueDoesNotRewrite(t *testing.T) {
	_, _, path := newConfigTestFile(t)
	source := `connection  = 'api' # keep the selected connection

[connections.api]
protocol = 'responses'
endpoint = "http://localhost:1234/v1"
`
	writeConfigTestFile(t, path, source)

	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetDefaultProviderConnection(path, "api"); err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != source || !os.SameFile(beforeInfo, afterInfo) {
		t.Fatalf("same-value default edit rewrote the source: %q", after)
	}
}

func TestAddFirstProviderConnectionPreservesAuthoredSource(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := `# First-run global configuration.
model = 'gpt-4o' # retain this authored setting
`
	writeConfigTestFile(t, path, source)

	endpoint := "http://localhost:1234/v1"
	definition := ProviderConnection{Protocol: ConnectionResponses, Endpoint: &endpoint}
	if err := AddProviderConnection(path, "first", definition); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < len(source) || string(got[:len(source)]) != source {
		t.Fatalf("added connection changed authored source: %q", got)
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	if app.Settings.Connection == nil || !reflect.DeepEqual(*app.Settings.Connection, ConnectionSelection{"first"}) {
		t.Fatalf("first added connection was not selected: %v", app.Settings.Connection)
	}
	connection, err := app.Settings.SelectedConnection()
	if err != nil {
		t.Fatal(err)
	}
	if connection.Protocol != definition.Protocol || connection.Endpoint == nil || *connection.Endpoint != endpoint {
		t.Fatalf("added connection = %+v", connection)
	}
	if app.Settings.Model != "gpt-4o" {
		t.Fatalf("authored model changed: %q", app.Settings.Model)
	}
}

func TestAddProviderConnectionAlongsideExistingDefinitionsPreservesAuthoredSource(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := strings.ReplaceAll(`# Existing connection uses authored dotted keys.
connection = 'work'
connections.work.protocol = 'responses'
connections.work.endpoint = "http://localhost:1234/v1" # retain this comment
model = 'gpt-4o'
`, "\n", "\r\n")
	writeConfigTestFile(t, path, source)

	endpoint := "http://localhost:5678/v1"
	environmentVariable := "SERVER_API_KEY"
	definition := ProviderConnection{
		Protocol:            ConnectionResponses,
		Endpoint:            &endpoint,
		EnvironmentVariable: &environmentVariable,
		Capabilities: ProviderCapabilitiesOverride{
			ProviderID:           "openai-compatible",
			SupportsResponsesAPI: true,
		},
	}
	if err := AddProviderConnection(path, "new-api", definition); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < len(source) || string(got[:len(source)]) != source {
		t.Fatalf("adding a definition changed authored source: %q", got)
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	if app.Settings.Connection == nil || !reflect.DeepEqual(*app.Settings.Connection, ConnectionSelection{"work"}) {
		t.Fatalf("adding a named definition changed the default: %v", app.Settings.Connection)
	}
	existing := app.Settings.Connections["work"]
	if existing.Protocol != ConnectionResponses || existing.Endpoint == nil || *existing.Endpoint != "http://localhost:1234/v1" {
		t.Fatalf("existing definition changed: %+v", existing)
	}
	added := app.Settings.Connections["new-api"]
	if added.Protocol != definition.Protocol || added.Endpoint == nil || *added.Endpoint != endpoint ||
		added.EnvironmentVariable == nil || *added.EnvironmentVariable != environmentVariable {
		t.Fatalf("added definition = %+v", added)
	}
	if added.Capabilities != definition.Capabilities {
		t.Fatalf("added definition capabilities = %+v", added.Capabilities)
	}
}

func TestAddProviderConnectionIntoInlineCatalogPreservesOtherSource(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	prefix := `# Retain the selected connection.
connection = 'work'
`
	inlineCatalog := `connections = { "work" = { protocol = 'responses', endpoint = "http://localhost:1234/v1" } } # catalog note
`
	suffix := `# Keep this unrelated section verbatim.
shell.max_concurrent = 3 # shell formatting
model = 'gpt-4o'
`
	source := prefix + inlineCatalog + suffix
	writeConfigTestFile(t, path, source)

	endpoint := "http://localhost:5678/v1"
	definition := ProviderConnection{Protocol: ConnectionResponses, Endpoint: &endpoint}
	if err := AddProviderConnection(path, "new-api", definition); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), prefix) || !strings.HasSuffix(string(got), suffix) ||
		!strings.Contains(string(got), "# catalog note") {
		t.Fatalf("adding to the inline catalog changed unrelated authored source: %q", got)
	}
	inlineDefinition := `"work" = { protocol = 'responses', endpoint = "http://localhost:1234/v1" }`
	if !strings.Contains(string(got), inlineDefinition) {
		t.Fatalf("adding to the inline catalog changed the existing definition source %q: %q", inlineDefinition, got)
	}

	app := loadConfigTestApp(t, workspace, LoadOptions{})
	if app.Settings.Connection == nil || !reflect.DeepEqual(*app.Settings.Connection, ConnectionSelection{"work"}) {
		t.Fatalf("adding a named definition changed the default: %v", app.Settings.Connection)
	}
	existing := app.Settings.Connections["work"]
	if existing.Protocol != ConnectionResponses || existing.Endpoint == nil || *existing.Endpoint != "http://localhost:1234/v1" {
		t.Fatalf("existing definition changed: %+v", existing)
	}
	added := app.Settings.Connections["new-api"]
	if added.Protocol != definition.Protocol || added.Endpoint == nil || *added.Endpoint != endpoint {
		t.Fatalf("added definition = %+v", added)
	}
	if app.Settings.Model != "gpt-4o" || app.Settings.Shell.MaxConcurrent != 3 {
		t.Fatalf("unrelated settings changed: model=%q shell=%+v", app.Settings.Model, app.Settings.Shell)
	}
}
