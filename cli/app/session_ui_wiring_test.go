package app

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"core/internal/testharness/pty/appfixture"
	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/lifecyclecontract"
)

type sessionUITestServer struct {
	sessionTranscript apicontract.SessionTranscriptService
	sessionViews      apicontract.SessionViewService
	runtimeControl    apicontract.RuntimeControlService
}

func (s sessionUITestServer) SessionUIClients() sessionUIClients {
	return sessionUIClients{
		RuntimeControls:   s.runtimeControl,
		SessionTranscript: s.sessionTranscript,
		SessionViews:      s.sessionViews,
	}
}

func TestSessionUIRequiresTranscriptService(t *testing.T) {
	server := sessionUITestServer{}
	_, err := prepareSharedRuntime(context.Background(), server, sessionLaunchPlan{SessionID: "session-1"}, io.Discard, "test")
	if err == nil {
		t.Fatal("expected missing transcript service error")
	}
}

func TestSessionUIUsesTranscriptBellHook(t *testing.T) {
	sessionViews := &countingSessionViewClient{}
	settings := config.DefaultOnboardingSettings()
	settings.Reviewer.ModelContextWindow = settings.ModelContextWindow
	server := sessionUITestServer{
		sessionTranscript: &recordingTranscriptSubscriber{subs: []*scriptedTranscriptSubscription{{}}},
		sessionViews:      sessionViews,
		runtimeControl:    &reconnectRetryRuntimeControlClient{},
	}

	plan, err := prepareSharedRuntime(context.Background(), server, sessionLaunchPlan{SessionID: "session-1", ActiveSettings: settings}, io.Discard, "test")
	if err != nil {
		t.Fatalf("prepareSharedRuntime: %v", err)
	}
	if plan.Wiring.eventDispatcher == nil || plan.Wiring.eventDispatcher.transcriptEvents == nil {
		t.Fatal("runtime wiring omitted the transcript event dispatcher")
	}
	promptHook, promptOK := plan.Wiring.promptAttention.(*bellHooks)
	turnHook, turnOK := plan.Wiring.turnQueueHook.(*bellHooks)
	if !promptOK || !turnOK || promptHook != turnHook {
		t.Fatal("runtime wiring did not make the bell hook authoritative for prompt activation")
	}
	plan.Close()
	if got := sessionViews.mainViewCount.Load(); got != 0 {
		t.Fatalf("startup main-view reads = %d, want 0 before feed hydration", got)
	}
}

func TestSessionUIKeepsPromptActivationIndependentFromLifecycleHooks(t *testing.T) {
	recordPath := filepath.Join(t.TempDir(), "lifecycle.jsonl")
	command, err := lifecycleHookProductRecorderCommand(
		recordPath,
		appfixture.LifecycleHookBehaviorSuccess,
		nil,
	)
	if err != nil {
		t.Fatalf("lifecycle recorder command: %v", err)
	}
	server := sessionUITestServer{
		sessionTranscript: &recordingTranscriptSubscriber{subs: []*scriptedTranscriptSubscription{{}}},
		sessionViews:      &countingSessionViewClient{},
		runtimeControl:    &reconnectRetryRuntimeControlClient{},
	}
	sessionID := ongoingTestSessionID().String()
	settings := config.DefaultOnboardingSettings()
	settings.Reviewer.ModelContextWindow = settings.ModelContextWindow
	wiring, stop, err := prepareSharedRuntimeWiring(t.Context(), server.SessionUIClients(), sessionLaunchPlan{
		SessionID:                  sessionID,
		ActiveSettings:             settings,
		ClientLifecycleCommand:     command,
		ClientLifecycleOpeningKind: lifecyclecontract.OpeningKindResumed,
	})
	if err != nil {
		t.Fatalf("prepareSharedRuntimeWiring: %v", err)
	}
	t.Cleanup(stop)

	promptHook, promptOK := wiring.promptAttention.(*bellHooks)
	turnHooks, turnOK := wiring.turnQueueHook.(*turnQueueHooks)
	if !promptOK || !turnOK || turnHooks.notifications != promptHook {
		t.Fatal("lifecycle hooks changed native prompt-activation ownership")
	}
}
