package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"core/server/launch"
	"core/server/sessionlaunch"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func TestNewChatAppliesOriginalInvocationModel(t *testing.T) {
	app, source, _, requests := newRunThinkingRecordingSession(t)
	binding, err := app.MetadataStore().ResolveSessionNavigationBinding(t.Context(), source.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	target := newChatCommandTarget(t, app, binding.ProjectId, binding.WorkspaceId)
	target.GetNewChat().Invocation, err = protoapi.RunPromptOverridesToProto(serverapi.RunPromptOverrides{Model: "gpt-5-mini"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(), Target: target,
		Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "use invocation model"}},
	})
	if err != nil || result.GetAccepted() == nil {
		t.Fatalf("Chat admission = %v, %v", result, err)
	}
	select {
	case request := <-requests:
		if request.Model != "gpt-5-mini" {
			t.Fatalf("new Chat ignored invocation model: %+v", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("new Chat did not reach the provider")
	}
}

func TestChatActionUsesAgentSavedAfterSessionOpening(t *testing.T) {
	app, store, _, requests := newRunThinkingRecordingSession(t)
	id := store.Identity().ID
	service, err := (chatSettingsService{core: app}).sessionSettingsService(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	opened, err := service.PlanLaunchSession(t.Context(), sessionlaunch.PlanRequest{
		Mode: launch.ModeInteractive, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Plan.ActiveSettings.Model != "gpt-5" {
		t.Fatalf("opening model = %s", opened.Plan.ActiveSettings.Model)
	}
	changed, err := app.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
		Session: &chatsettingspb.SessionTarget{SessionId: protoapi.SessionSelectorToProto(id)},
		Operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_AgentRole{
			AgentRole: "worker",
		}},
	})
	if err != nil || changed.GetResult().GetApplied() == nil {
		t.Fatalf("save Agent: %v, %v", changed, err)
	}
	target := chatCommandTarget(id.String())
	result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(), Target: target,
		Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "use current Agent"}},
	})
	if err != nil || result.GetAccepted() == nil {
		t.Fatalf("Chat admission = %v, %v", result, err)
	}
	select {
	case request := <-requests:
		if request.Model != "gpt-5-mini" || request.Reasoning.Effort != "low" {
			t.Fatalf("Chat used stale opening settings: %+v", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Chat did not reach the provider")
	}
}

func TestChatActionSettingsTiming(t *testing.T) {
	for _, test := range []struct {
		name      string
		agent     *string
		overrides serverapi.RunPromptOverrides
		live      bool
		invalid   bool
		model     string
		thinking  string
	}{
		{name: "current config", model: "gpt-5.6-sol"},
		{name: "current Agent definition", agent: textutil.Value("worker"), model: "gpt-5.6-sol"},
		{name: "explicit invocation wins", overrides: serverapi.RunPromptOverrides{Model: "gpt-5-mini"}, model: "gpt-5-mini"},
		{name: "live Runtime frozen", live: true, model: "gpt-5"},
		{name: "live Runtime ignores model override and invalid config", live: true, invalid: true,
			overrides: serverapi.RunPromptOverrides{Model: "gpt-5-mini"}, model: "gpt-5"},
		{name: "live Runtime ignores theme override and invalid config", live: true, invalid: true,
			overrides: serverapi.RunPromptOverrides{Theme: "light"}, model: "gpt-5"},
		{name: "live Thinking applies without replanning", live: true, invalid: true,
			overrides: serverapi.RunPromptOverrides{ThinkingLevel: "high"}, model: "gpt-5", thinking: "high"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, store, _, requests := newRunThinkingRecordingSession(t)
			id := store.Identity().ID
			if test.agent != nil {
				changed, err := app.ChatSettingsClient().MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
					Session: &chatsettingspb.SessionTarget{SessionId: protoapi.SessionSelectorToProto(id)},
					Operation: &chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_AgentRole{
						AgentRole: *test.agent,
					}},
				})
				if err != nil || changed.GetResult().GetApplied() == nil {
					t.Fatalf("save Agent: %v, %v", changed, err)
				}
			}
			service, err := (chatSettingsService{core: app}).sessionSettingsService(t.Context(), id.String())
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.PlanLaunchSession(t.Context(), sessionlaunch.PlanRequest{
				Mode: launch.ModeInteractive, Intent: serverapi.OpenExistingSessionLaunchIntent(id), Overrides: test.overrides,
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.live {
				result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
					QueueItemId: runtimeids.NewQueueItemID().String(), Target: chatCommandTarget(id.String()),
					Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "establish the frozen Session Contract"}},
				})
				if err != nil || result.GetAccepted() == nil {
					t.Fatalf("prepare live Runtime: %v, %v", result, err)
				}
				select {
				case <-requests:
				case <-time.After(5 * time.Second):
					t.Fatal("initial live turn did not reach the provider")
				}
			}
			privateDir := filepath.Join(store.Meta().WorkspaceRoot, ".kent")
			if err := os.MkdirAll(privateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			configuration := "model = \"gpt-5.6-sol\"\n[subagents.worker]\nmodel = \"gpt-5.6-sol\"\n"
			if test.invalid {
				configuration = "model = 123\n"
			}
			if err := os.WriteFile(filepath.Join(privateDir, "config.local.toml"), []byte(configuration), 0o600); err != nil {
				t.Fatal(err)
			}
			target := chatCommandTarget(id.String())
			target.GetSession().Invocation, err = protoapi.RunPromptOverridesToProto(test.overrides)
			if err != nil {
				t.Fatal(err)
			}
			result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
				QueueItemId: runtimeids.NewQueueItemID().String(), Target: target,
				Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "use action-time settings"}},
			})
			if err != nil || result.GetAccepted() == nil {
				t.Fatalf("Chat admission = %v, %v", result, err)
			}
			select {
			case request := <-requests:
				if request.Model != test.model {
					t.Fatalf("provider model = %s, want %s", request.Model, test.model)
				}
				if test.thinking != "" && request.Reasoning.Effort != test.thinking {
					t.Fatalf("provider Thinking = %s, want %s", request.Reasoning.Effort, test.thinking)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Chat did not reach the provider")
			}
		})
	}
}

func TestChatActionRetriesCurrentConfigurationAfterPreparationFailure(t *testing.T) {
	app, store, _, requests := newRunThinkingRecordingSession(t)
	id := store.Identity().ID
	service, err := (chatSettingsService{core: app}).sessionSettingsService(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PlanLaunchSession(t.Context(), sessionlaunch.PlanRequest{
		Mode: launch.ModeInteractive, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
	}); err != nil {
		t.Fatal(err)
	}
	privateDir := filepath.Join(store.Meta().WorkspaceRoot, ".kent")
	if err := os.MkdirAll(privateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(privateDir, "config.local.toml")
	if err := os.WriteFile(path, []byte("model = 123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dispatch := func() (*chatpb.InputMutationSuccess, error) {
		return app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
			QueueItemId: runtimeids.NewQueueItemID().String(), Target: chatCommandTarget(id.String()),
			Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "retry explicitly"}},
		})
	}
	failed, err := dispatch()
	if err != nil || failed.GetNotAccepted() == nil || failed.Session.SessionId != id.String() {
		t.Fatalf("preparation failure lost Session or accepted input: %v, %v", failed, err)
	}
	select {
	case request := <-requests:
		t.Fatalf("failed preparation reached provider: %+v", request)
	default:
	}
	if err := os.WriteFile(path, []byte("model = \"gpt-5-mini\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	retried, err := dispatch()
	if err != nil || retried.GetAccepted() == nil || retried.Session.SessionId != id.String() {
		t.Fatalf("explicit retry failed: %v, %v", retried, err)
	}
	select {
	case request := <-requests:
		if request.Model != "gpt-5-mini" {
			t.Fatalf("retry used stale configuration: %+v", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retry did not reach provider")
	}
}

func TestChatActionRejectsUnsupportedLiveThinkingWithoutChangingSettings(t *testing.T) {
	app, store, _, requests := newRunThinkingRecordingSession(t)
	id := store.Identity().ID
	dispatch := func(overrides serverapi.RunPromptOverrides) (*chatpb.InputMutationSuccess, error) {
		target := chatCommandTarget(id.String())
		var err error
		target.GetSession().Invocation, err = protoapi.RunPromptOverridesToProto(overrides)
		if err != nil {
			t.Fatal(err)
		}
		return app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
			QueueItemId: runtimeids.NewQueueItemID().String(), Target: target,
			Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "keep valid Thinking"}},
		})
	}
	initial, err := dispatch(serverapi.RunPromptOverrides{Model: "gpt-6-sol", ThinkingLevel: "medium"})
	if err != nil || initial.GetAccepted() == nil {
		t.Fatalf("initial Chat admission: %v, %v", initial, err)
	}
	select {
	case <-requests:
	case <-time.After(5 * time.Second):
		t.Fatal("initial turn did not reach provider")
	}
	before, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := dispatch(serverapi.RunPromptOverrides{ThinkingLevel: "unsupported-future-effort"})
	if err != nil || rejected.GetNotAccepted() == nil {
		t.Fatalf("unsupported Thinking was admitted: %v, %v", rejected, err)
	}
	after, err := app.MetadataStore().ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Meta.ChatSettings, after.Meta.ChatSettings) {
		t.Fatalf("rejected Thinking changed saved settings: before=%+v after=%+v", before.Meta.ChatSettings, after.Meta.ChatSettings)
	}
	next, err := dispatch(serverapi.RunPromptOverrides{})
	if err != nil || next.GetAccepted() == nil {
		t.Fatalf("next Chat admission: %v, %v", next, err)
	}
	select {
	case request := <-requests:
		if request.Reasoning.Effort != "medium" {
			t.Fatalf("rejected Thinking poisoned live Runtime: %+v", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("next turn did not reach provider")
	}
}
