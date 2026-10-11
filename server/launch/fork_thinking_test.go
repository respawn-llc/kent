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
		{name: "model without native updates", model: "gpt-5.6-sol"},
		{name: "OAuth default", model: "gpt-6-astra", oauth: true, supported: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := config.DefaultOnboardingSettings()
			settings.Model = test.model
			id := config.ConnectionID("test")
			settings.Connection = config.SingleConnection(id)
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

func TestPrepareInteractiveForkUsesFreshTargetAndCurrentDesiredSelection(t *testing.T) {
	for _, test := range []struct {
		name        string
		targetModel string
		sourceModel string
		native      bool
	}{
		{name: "fresh target supports updates", targetModel: "gpt-6-astra", sourceModel: "gpt-5.6-sol", native: true},
		{name: "fresh target does not support updates", targetModel: "gpt-5.6-sol", sourceModel: "gpt-6-astra"},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := config.DefaultOnboardingSettings()
			settings.Model = test.targetModel
			id := config.ConnectionID("interactive")
			settings.Connection = config.SingleConnection(id)
			settings.Connections = map[config.ConnectionID]config.ProviderConnection{
				id: {Protocol: config.ConnectionResponses, Endpoint: textutil.Value("https://api.openai.com/v1")},
			}
			meta := session.Meta{
				Locked:           &session.LockedContract{Model: test.sourceModel},
				Continuation:     &session.ContinuationContext{AgentRole: new("workflow-agent")},
				ConnectionID:     new(config.ConnectionID("deleted-workflow-connection")),
				WorkflowSnapshot: &session.WorkflowSnapshot{},
				ChatSettings:     &session.ChatSettingsOverrides{Thinking: new("low")},
			}
			target, err := PrepareInteractiveFork(config.App{Settings: settings}, meta, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := target.Thinking
			if got.Desired != "low" || got.PreserveNativeUpdates != test.native {
				t.Fatalf("interactive fork Thinking = %+v, want low / native %v", got, test.native)
			}
			meta.ChatSettings = nil
			meta.ConnectionID = &id
			target, err = PrepareInteractiveFork(config.App{Settings: settings}, meta, nil)
			if err != nil {
				t.Fatal(err)
			}
			got = target.Thinking
			if got.Desired != settings.ThinkingLevel || got.PreserveNativeUpdates != test.native {
				t.Fatalf("unpersisted desired Thinking = %+v, want %s / native %v", got, settings.ThinkingLevel, test.native)
			}
		})
	}
}

func TestPrepareInteractiveForkUsesAllocatedConnectionInsteadOfFirstMember(t *testing.T) {
	settings := config.DefaultOnboardingSettings()
	settings.Model = "gpt-6-astra"
	selection := config.ConnectionSelection{"official", "custom"}
	settings.Connection = &selection
	settings.ConnectionOrder = []config.ConnectionID{"official", "custom"}
	settings.Connections = map[config.ConnectionID]config.ProviderConnection{
		"official": {Protocol: config.ConnectionResponses, Endpoint: new("https://api.openai.com/v1")},
		"custom":   {Protocol: config.ConnectionResponses, Endpoint: new("https://example.test/v1")},
	}
	source := session.Meta{
		ConnectionID: new(config.ConnectionID("official")),
		ChatSettings: &session.ChatSettingsOverrides{Thinking: new("low")},
	}
	rotation := new(ConnectionRotation)
	for _, expected := range []struct {
		id     config.ConnectionID
		native bool
	}{{"official", true}, {"custom", false}, {"official", true}} {
		target, err := PrepareInteractiveFork(config.App{Settings: settings}, source, rotation)
		if err != nil {
			t.Fatal(err)
		}
		if target.ConnectionID != expected.id || target.Thinking.Desired != "low" || target.Thinking.PreserveNativeUpdates != expected.native {
			t.Fatalf("fresh fork target = %+v, want %s / low / native %v", target, expected.id, expected.native)
		}
	}
}
