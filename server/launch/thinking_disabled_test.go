package launch

import (
	"testing"

	"core/server/session"
	"core/shared/config"
)

func TestDisabledThinkingPreparedChatSettings(t *testing.T) {
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna"} {
		t.Run(model, func(t *testing.T) {
			settings := config.DefaultOnboardingSettings()
			settings.Model = model
			settings.ThinkingLevel = ""
			prepared, err := PrepareChatSettingsForPreparedTarget(PreparedBaseTarget{Settings: settings}, false)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Baseline.Thinking != "none" {
				t.Fatalf("disabled launch Thinking = %q", prepared.Baseline.Thinking)
			}
			resolved, err := ResolveSessionChatSettings(session.Meta{}, settings)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Thinking != "none" {
				t.Fatalf("configured disabled Thinking resolved to %q", resolved.Thinking)
			}
		})
	}
}
