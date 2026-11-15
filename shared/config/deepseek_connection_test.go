package config

import (
	"os"
	"strings"
	"testing"
)

func TestDeepSeekConnectionDefaultsToOfficialEndpoint(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	writeConfigTestFile(t, path, `
connection = "deepseek-1"

[connections.deepseek-1]
protocol = "deepseek-api-key"
environment_variable = "DEEPSEEK_API_KEY"
`)
	app := loadConfigTestApp(t, workspace, LoadOptions{})
	connection := app.Settings.Connections["deepseek-1"]
	if connection.Protocol != ConnectionDeepSeekAPIKey || connection.Endpoint != nil || connection.EnvironmentVariable == nil || *connection.EnvironmentVariable != "DEEPSEEK_API_KEY" {
		t.Fatalf("connection = %+v", connection)
	}
	if err := connection.Validate(); err != nil {
		t.Fatal(err)
	}
	missing := connection
	missing.EnvironmentVariable = nil
	if err := missing.Validate(); err == nil {
		t.Fatal("accepted a DeepSeek connection without an environment variable")
	}
	blank := ""
	missing.Endpoint = &blank
	missing.EnvironmentVariable = connection.EnvironmentVariable
	if err := missing.Validate(); err == nil {
		t.Fatal("accepted an empty DeepSeek endpoint")
	}
}

func TestDeepSeekLoadNormalizesBaseWithoutRewritingFile(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint string
		want     *string
	}{
		{name: "official with space and slash", endpoint: "  https://api.deepseek.com/  "},
		{name: "custom with space and slash", endpoint: " https://models.example.test/v1/ ", want: ptr("https://models.example.test/v1")},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, workspace, path := newConfigTestFile(t)
			source := `
[connections.deepseek-1]
protocol = "deepseek-api-key"
endpoint = "` + test.endpoint + `"
environment_variable = "DEEPSEEK_API_KEY"
`
			writeConfigTestFile(t, path, source)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			app := loadConfigTestApp(t, workspace, LoadOptions{})
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("loading rewrote the settings file")
			}
			connection := app.Settings.Connections["deepseek-1"]
			switch {
			case test.want == nil && connection.Endpoint != nil:
				t.Fatalf("endpoint = %q, want none", *connection.Endpoint)
			case test.want != nil && (connection.Endpoint == nil || *connection.Endpoint != *test.want):
				t.Fatalf("endpoint = %v, want %q", connection.Endpoint, *test.want)
			}
		})
	}
}

func TestDeepSeekReferenceEditStatesTheBase(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := `
[connections.deepseek-1]
protocol = "deepseek-api-key"
endpoint = "https://models.example.test/v1"
environment_variable = "OLD_KEY"

[connections.api]
protocol = "responses"
endpoint = "http://127.0.0.1:8000/v1"
environment_variable = "OTHER_KEY"
`
	writeConfigTestFile(t, path, source)
	if err := SetProviderConnectionEnvironment(path, "deepseek-1", "DEEPSEEK_API_KEY"); err != nil {
		t.Fatal(err)
	}
	app := loadConfigTestApp(t, workspace, LoadOptions{})
	connection := app.Settings.Connections["deepseek-1"]
	if connection.Endpoint == nil || *connection.Endpoint != "https://models.example.test/v1" || connection.EnvironmentVariable == nil || *connection.EnvironmentVariable != "DEEPSEEK_API_KEY" {
		t.Fatalf("environment-only edit = %+v", connection)
	}
	if err := SetDeepSeekConnectionReference(path, "api", "OTHER_KEY", DeepSeekEndpointChoice{Endpoint: "https://models.example.test/other"}); err == nil {
		t.Fatal("accepted a DeepSeek endpoint change for a Responses connection")
	}
	if err := SetDeepSeekConnectionReference(path, "deepseek-1", "DEEPSEEK_API_KEY", DeepSeekEndpointChoice{}); err == nil {
		t.Fatal("accepted an empty DeepSeek base")
	}
	if err := SetDeepSeekConnectionReference(path, "deepseek-1", "DEEPSEEK_API_KEY", DeepSeekEndpointChoice{Endpoint: "not-a-url"}); err == nil {
		t.Fatal("accepted a non-absolute DeepSeek base")
	}
	app = loadConfigTestApp(t, workspace, LoadOptions{})
	connection = app.Settings.Connections["deepseek-1"]
	if connection.Endpoint == nil || *connection.Endpoint != "https://models.example.test/v1" {
		t.Fatalf("rejected edit changed endpoint to %+v", connection.Endpoint)
	}
	if err := SetDeepSeekConnectionReference(path, "deepseek-1", "DEEPSEEK_API_KEY", DeepSeekEndpointChoice{Official: true}); err != nil {
		t.Fatal(err)
	}
	app = loadConfigTestApp(t, workspace, LoadOptions{})
	connection = app.Settings.Connections["deepseek-1"]
	if connection.Endpoint != nil {
		t.Fatalf("official choice stored %q", *connection.Endpoint)
	}
	if err := SetDeepSeekConnectionReference(path, "deepseek-1", "DEEPSEEK_API_KEY", DeepSeekEndpointChoice{Endpoint: " https://models.example.test/other/ "}); err != nil {
		t.Fatal(err)
	}
	app = loadConfigTestApp(t, workspace, LoadOptions{})
	connection = app.Settings.Connections["deepseek-1"]
	if connection.Endpoint == nil || *connection.Endpoint != "https://models.example.test/other" {
		t.Fatalf("custom edit = %+v", connection.Endpoint)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "endpoint = 'https://models.example.test/other'") || strings.Contains(string(saved), "https://models.example.test/other/") {
		t.Fatalf("stored endpoint was not the trimmed base:\n%s", saved)
	}
	api := app.Settings.Connections["api"]
	if api.Endpoint == nil || *api.Endpoint != "http://127.0.0.1:8000/v1" {
		t.Fatalf("unrelated endpoint = %+v", api.Endpoint)
	}
}

func TestDeepSeekReferenceEditRewritesRawEndpointText(t *testing.T) {
	for _, test := range []struct {
		name     string
		before   string
		choice   DeepSeekEndpointChoice
		wantLine string
	}{
		{
			name:     "official text",
			before:   "  https://api.deepseek.com/  ",
			choice:   DeepSeekEndpointChoice{Official: true},
			wantLine: "",
		},
		{
			name:     "custom spacing only",
			before:   " https://models.example.test/v1/ ",
			choice:   DeepSeekEndpointChoice{Endpoint: " https://models.example.test/v1/ "},
			wantLine: "endpoint = 'https://models.example.test/v1'",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, workspace, path := newConfigTestFile(t)
			writeConfigTestFile(t, path, `
[connections.deepseek-1]
protocol = "deepseek-api-key"
endpoint = "`+test.before+`"
environment_variable = "DEEPSEEK_API_KEY"
`)
			if err := SetDeepSeekConnectionReference(path, "deepseek-1", "DEEPSEEK_API_KEY", test.choice); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(saved)
			if strings.Contains(text, test.before) {
				t.Fatalf("raw endpoint text remained:\n%s", text)
			}
			if test.wantLine == "" {
				if strings.Contains(text, "endpoint") || strings.Contains(text, "api.deepseek.com") {
					t.Fatalf("official base remained in the file:\n%s", text)
				}
			} else if !strings.Contains(text, test.wantLine) {
				t.Fatalf("stored endpoint = \n%s", text)
			}
			app := loadConfigTestApp(t, workspace, LoadOptions{})
			connection := app.Settings.Connections["deepseek-1"]
			if test.wantLine == "" && connection.Endpoint != nil {
				t.Fatalf("loaded endpoint = %q", *connection.Endpoint)
			}
			if test.wantLine != "" && (connection.Endpoint == nil || *connection.Endpoint != "https://models.example.test/v1") {
				t.Fatalf("loaded endpoint = %+v", connection.Endpoint)
			}
		})
	}
}

func ptr(value string) *string { return &value }
