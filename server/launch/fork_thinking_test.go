package launch

import (
	"testing"

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
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := config.DefaultOnboardingSettings()
			settings.Model = test.model
			id := config.ConnectionID("test")
			settings.Connection = &id
			definition := config.ProviderConnection{Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://api.openai.com/v1")}
			if test.endpoint != "" {
				definition.Endpoint = &test.endpoint
			}
			if test.oauth {
				definition = config.ProviderConnection{Protocol: config.ConnectionChatGPT}
			}
			settings.Connections = map[config.ConnectionID]config.ProviderConnection{id: definition}
			for _, override := range []*string{nil, textutil.Value("low")} {
				meta := session.Meta{ChatSettings: &session.ChatSettingsOverrides{Thinking: override}}
				got, err := ResolveForkThinking(config.App{Settings: settings}, meta, false)
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
