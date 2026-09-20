package core

import (
	"context"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/auth"
	"core/server/llm"
	"core/server/metadata"
	"core/server/runtimewire"
	"core/shared/config"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"
	"core/shared/runtimeinput"
	"core/shared/textutil"
)

func TestConnectionReplacementIsHydratedAfterCoreActivation(t *testing.T) {
	settings := config.DefaultOnboardingSettings()
	settings.Reviewer.Frequency = "off"
	cfg := testsetup.ProgrammaticConfig(t, settings)
	binding, err := metadata.RegisterBinding(t.Context(), cfg.PersistenceRoot, cfg.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	model := newChatSettingsBoundaryLLMClient()
	t.Cleanup(model.unblock)
	app := newCoreTestAppWithOptions(t, cfg, auth.EmptyState(), Options{
		RuntimeClientFactory: runtimewire.RuntimeClientFactoryFunc(func(context.Context, runtimewire.RuntimeClientRequest) (llm.Client, error) {
			return model, nil
		}),
	})
	store := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
	if err := store.SetConnectionID("removed"); err != nil {
		t.Fatal(err)
	}
	_, err = app.SessionRuntimeClient().ActivateSessionRuntime(t.Context(), serverapi.SessionRuntimeActivateRequest{
		SessionID: store.Meta().SessionID, OwnerID: "connection-notice",
		ActiveSettings: cfg.Settings, Source: cfg.Source,
		QuestionsEnabled: textutil.Value(true), AutoCompactionEnabled: textutil.Value(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	subscription, err := app.SessionTranscriptClient().SubscribeSessionTranscript(ctx, &transcriptpb.SubscribeRequest{SessionId: store.Meta().SessionID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	message, err := subscription.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	notice := message.GetEvent().GetHydration().GetConnectionReplacement()
	if notice == nil || notice.PreviousId != "removed" || notice.CurrentId != string(*cfg.Settings.Connection) {
		t.Fatalf("activation replacement notice = %+v", notice)
	}
	record, err := app.MetadataStore().ResolvePersistedSession(ctx, store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != *cfg.Settings.Connection {
		t.Fatalf("announced replacement was not saved: %v", record.Meta.ConnectionID)
	}
	select {
	case <-model.started:
		t.Fatal("model dispatched before the replacement was observed")
	default:
	}
	input, err := protoapi.UserTurnInputToProto(runtimeinput.Text("continue"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.RuntimeControlClient().SubmitUserTurn(ctx, &runtimepb.SubmitUserTurnRequest{
		SessionId: store.Meta().SessionID, Input: input,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.started:
	case <-ctx.Done():
		t.Fatal("the observed replacement did not permit the next request")
	}
	model.unblock()
}
