package launch

import (
	"testing"

	"core/server/auth"
	"core/server/session"
	"core/shared/config"
	"core/shared/textutil"
)

func TestResolveForkThinkingUsesCurrentSelectionAndTargetSupport(t *testing.T) {
	for _, test := range []struct {
		name      string
		model     string
		endpoint  string
		supported bool
		oauth     bool
	}{
		{name: "official", model: "gpt-6-astra", supported: true},
		{name: "custom", model: "gpt-6-astra", endpoint: "https://example.test/v1"},
		{name: "different model", model: "gpt-5.4"},
		{name: "OAuth default", model: "gpt-6-astra", oauth: true, supported: true},
		{name: "OAuth explicit Codex", model: "gpt-6-astra", endpoint: "https://chatgpt.com/backend-api/codex", oauth: true, supported: true},
		{name: "OAuth API endpoint uses compatible transport", model: "gpt-6-astra", endpoint: "https://api.openai.com/v1", oauth: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := config.DefaultOnboardingSettings()
			settings.Model, settings.OpenAIBaseURL = test.model, test.endpoint
			state := auth.EmptyState()
			if test.oauth {
				state.Method = auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{
					AccessToken: "test-access", RefreshToken: "test-refresh", TokenType: "Bearer",
				}}
			}
			manager := auth.NewManager(auth.NewMemoryStore(state), nil, nil)
			for _, override := range []*string{nil, textutil.Value("low")} {
				meta := session.Meta{ChatSettings: &session.ChatSettingsOverrides{Thinking: override}}
				got, err := ResolveForkThinking(t.Context(), config.App{Settings: settings}, meta, manager, false)
				if err != nil {
					t.Fatal(err)
				}
				want := settings.ThinkingLevel
				if override != nil {
					want = *override
				}
				if got.Desired != want || got.PreserveNativeUpdates != test.supported {
					t.Fatalf("fork Thinking = %+v, want %q / native %v", got, want, test.supported)
				}
			}
		})
	}
}
