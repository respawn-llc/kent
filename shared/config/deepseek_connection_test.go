package config

import (
	"os"
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

func TestDeepSeekReferenceEditCanReplaceEndpoint(t *testing.T) {
	_, workspace, path := newConfigTestFile(t)
	source := `
[connections.deepseek-1]
protocol = "deepseek-api-key"
endpoint = "https://models.example.test/v1"
environment_variable = "OLD_KEY"
`
	writeConfigTestFile(t, path, source)
	official := DefaultDeepSeekResponsesEndpoint
	if err := SetProviderConnectionEnvironment(path, "deepseek-1", "DEEPSEEK_API_KEY", &official); err != nil {
		t.Fatal(err)
	}
	app := loadConfigTestApp(t, workspace, LoadOptions{})
	connection := app.Settings.Connections["deepseek-1"]
	if connection.Endpoint != nil || connection.EnvironmentVariable == nil || *connection.EnvironmentVariable != "DEEPSEEK_API_KEY" {
		t.Fatalf("official edit = %+v", connection)
	}
	custom := "https://models.example.test/other/"
	if err := SetProviderConnectionEnvironment(path, "deepseek-1", "DEEPSEEK_API_KEY", &custom); err != nil {
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
	if SuggestConnectionID(ConnectionDeepSeekAPIKey, app.Settings.Connections) != "deepseek-2" {
		t.Fatalf("next id with existing deepseek-1 is %s", SuggestConnectionID(ConnectionDeepSeekAPIKey, app.Settings.Connections))
	}
	if len(saved) == 0 {
		t.Fatal("settings file was erased")
	}
}
