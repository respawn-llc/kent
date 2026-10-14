package config

import (
	"path/filepath"
	"testing"
)

func TestNanoAbsent(t *testing.T) {
	_, workspace := newConfigTestEnv(t)
	app := loadConfigTestApp(t, workspace, LoadOptions{})
	if app.Settings.Nano != nil {
		t.Fatal("absent nano section became available")
	}
}

func TestNanoConfigurationLayering(t *testing.T) {
	_, workspace, global := newConfigTestFile(t)
	writeConfigTestFile(t, global, nanoConnection+"[nano]\nconnection = \"local\"\nmodel = \"original\"\nconcurrency = 2\n")
	writeConfigTestFile(t, filepath.Join(workspace, ConfigDirName, "config.toml"), "[nano]\nmodel = \"override\"\nmodel_context_window = 2500\nthinking_level = \"low\"\nmodel_verbosity = \"high\"\n[nano.features]\nsession_naming = false\n")
	app := loadConfigTestApp(t, workspace, LoadOptions{})
	n := app.Settings.Nano
	if *n.Model != "override" || *n.ThinkingLevel != "low" || *n.ModelVerbosity != ModelVerbosityHigh || n.SessionNaming || app.Settings.NanoConcurrency != 2 {
		t.Fatalf("layered nano = %+v, concurrency = %d", n, app.Settings.NanoConcurrency)
	}
	assertConfigSource(t, app, "nano.model", SourceKind("file"))
	writeConfigTestFile(t, filepath.Join(workspace, ConfigDirName, "config.toml"), "[nano]\nconcurrency = 3\n")
	if _, err := Load(workspace, workspace, LoadOptions{}); err == nil {
		t.Fatal("workspace concurrency override accepted")
	}
}

func TestNanoConfigurationRoundTrip(t *testing.T) {
	_, _, app := loadConfigTestFileApp(t, nanoConnection+"[nano]\nconnection = \"local\"\nmodel = \"local-model\"\nmodel_context_window = 2500\n", LoadOptions{})
	// Reuse the same provider definitions; the settings renderer handles roles.
	_, _, restored := loadConfigTestFileApp(t, nanoConnection+settingsTOMLWithRenderingOptions(app.Settings, true, nil, nil), LoadOptions{})
	if restored.Settings.Nano == nil || restored.Settings.Nano.Model == nil || *restored.Settings.Nano.Model != *app.Settings.Nano.Model {
		t.Fatal("configured nano was lost during rendering")
	}
}

func TestNanoConfigurationInvalid(t *testing.T) {
	for _, body := range []string{
		"[nano]\n",
		"[nano]\nmodel = \"local\"\n",
		"[nano]\nconnection = \"local\"\n",
		"[nano]\nconnection = \"missing\"\nmodel = \"local\"\n",
		"[nano]\nconnection = []\nmodel = \"local\"\n",
		"[nano]\nconnection = \"local\"\nmodel = \"\"\n",
		"[nano]\nconnection = \"local\"\nmodel = \"local\"\nmodel_context_window = 2499\n",
		"[nano]\nconnection = \"local\"\nmodel = \"local\"\ntimeout_seconds = 0\n",
		"[nano]\nconnection = \"local\"\nmodel = \"local\"\nconcurrency = 0\n",
	} {
		t.Run(body, func(t *testing.T) {
			if err := loadConfigTestFileError(t, nanoConnection+body, LoadOptions{}); err == nil {
				t.Fatal("invalid nano accepted")
			}
		})
	}
}

const nanoConnection = "[connections.local]\nprotocol = \"responses\"\nendpoint = \"http://localhost:1234/v1\"\n"

func TestNanoConfigurationDefaults(t *testing.T) {
	_, _, app := loadConfigTestFileApp(t, nanoConnection+"[nano]\nconnection = [\"local\"]\nmodel = \"local-model\"\nmodel_context_window = 2500\n", LoadOptions{})
	n := app.Settings.Nano
	if n == nil || n.Model == nil || *n.Model != "local-model" || n.ModelContextWindow == nil || *n.ModelContextWindow != 2500 {
		t.Fatalf("nano model/context = %+v", n)
	}
	if n.TimeoutSeconds != 120 || !n.SessionNaming || app.Settings.NanoConcurrency != 5 {
		t.Fatalf("nano defaults = %+v, concurrency = %d", n, app.Settings.NanoConcurrency)
	}
	if n.ThinkingLevel != nil || n.ModelVerbosity != nil || n.SupportsReasoningEffort != nil || n.SupportsVisionInputs != nil {
		t.Fatal("nano inherited main generation controls")
	}
}
