package testsetup

import (
	"testing"

	"core/shared/config"
)

// ProgrammaticConfig supplies declaration evidence for a fully specified Go
// fixture. These values are explicit inputs, not inferred configuration defaults.
func ProgrammaticConfig(t *testing.T, settings config.Settings) config.App {
	t.Helper()
	workspace := t.TempDir()
	app, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	app.Settings = settings
	for key := range app.Source.Sources {
		app.Source.Sources[key] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: key}}
	}
	for key := range settings.SkillToggles {
		address := "skills." + key
		app.Source.Sources[address] = config.Origin{Kind: config.SourceInput, Property: config.PropertyAddress{Key: address}}
	}
	return app
}
