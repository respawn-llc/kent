package llm

import (
	"os"
	"path/filepath"
	"testing"

	"core/shared/config"
)

func TestNanoResolvedModelControls(t *testing.T) {
	root := t.TempDir()
	body := `connection = "local"
thinking_level = "high"
model_verbosity = "high"
[model_capabilities]
supports_reasoning_effort = false
supports_vision_inputs = false
[connections.local]
protocol = "responses"
endpoint = "http://localhost:1234/v1"
[connections.subscription]
protocol = "chatgpt-codex"
[nano]
connection = ["local", "subscription"]
model = "gpt-6-sol"
`
	path := filepath.Join(root, "config.toml")
	resolve := func(body string) []NanoTarget {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		targets, err := ResolveNanoTargets(app.Settings)
		if err != nil {
			t.Fatal(err)
		}
		return targets
	}
	targets := resolve(body)
	model, _ := LookupModelCapabilityContract("gpt-6-sol")
	if len(targets) != 2 {
		t.Fatalf("resolved members = %d", len(targets))
	}
	for _, target := range targets {
		if target.Settings.ModelContextWindow != 272000 || target.Settings.ThinkingLevel != *model.DefaultReasoningEffort || target.Settings.ModelVerbosity != "" {
			t.Fatalf("unexpected context/generation defaults: %+v", target.Settings)
		}
		if !target.Settings.ModelCapabilities.SupportsReasoningEffort || !target.SupportsVisionInputs {
			t.Fatal("main capability overrides leaked into nano")
		}
	}
	for _, target := range resolve(body + "model_context_window = 2500\nthinking_level = \"low\"\nmodel_verbosity = \"low\"\n[nano.model_capabilities]\nsupports_reasoning_effort = false\nsupports_vision_inputs = false\n") {
		if target.Settings.ModelContextWindow != 2500 || target.Settings.ThinkingLevel != "low" || target.Settings.ModelVerbosity != config.ModelVerbosityLow ||
			target.Settings.ModelCapabilities.SupportsReasoningEffort || target.SupportsVisionInputs {
			t.Fatal("explicit nano overrides not applied")
		}
	}
}
