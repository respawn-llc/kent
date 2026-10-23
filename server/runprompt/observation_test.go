package runprompt

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"core/server/agentruntime"
	"core/server/launch"
	"core/server/llm"
	"core/server/runtimewire"
	"core/server/sessionlaunch"
	"core/server/sessionruntime"
	"core/server/sessionstore"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

type queuedDeadlineClient struct {
	firstStarted  chan struct{}
	releaseFirst  chan struct{}
	queuedStarted chan struct{}
}

func (c *queuedDeadlineClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true}, nil
}

func (c *queuedDeadlineClient) Generate(ctx context.Context, _ llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	select {
	case <-c.firstStarted:
		close(c.queuedStarted)
		<-ctx.Done()
		return llm.Response{}, context.Cause(ctx)
	default:
		close(c.firstStarted)
		<-c.releaseFirst
		return llm.Response{Assistant: llm.Message{
			Role: llm.RoleAssistant, Phase: textutil.Value(llm.MessagePhaseFinal), Content: textutil.Value("done"),
		}}, nil
	}
}

func TestRunPromptTimerStopsPostFinalQueuedExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newSelectedRunPromptFixture(t, "http://127.0.0.1:1", nil)
		launcher := fixture.client.(*inProcessRunPromptService).launcher
		id := mustRunPromptSessionID(t, fixture.store.Meta().SessionID)
		planned, err := launcher.boot.SessionLaunch.PlanLaunchSession(t.Context(), sessionlaunch.PlanRequest{
			Mode: launch.ModeHeadless, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
		})
		if err != nil {
			t.Fatal(err)
		}
		prepared, err := launcher.runtimeLaunchRequest(planned.Plan, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &queuedDeadlineClient{make(chan struct{}), make(chan struct{}), make(chan struct{})}
		filesystem, err := runtimewire.NewFilesystemContext(
			planned.Plan.ExecutionTarget.EffectiveWorkdir, planned.Plan.ExecutionTarget.WorkspaceRoot, planned.Plan.ProjectID,
		)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := sessionruntime.NewAgentRuntimePlan(sessionruntime.AgentRuntimePlanOptions{
			ConnectionScope:   planned.Plan.ConnectionScope,
			MainWorkspaceRoot: planned.Plan.ExecutionTarget.WorkspaceRoot,
			Settings:          planned.Plan.ActiveSettings, EnabledTools: planned.Plan.EnabledTools,
			FilesystemContext: filesystem, Headless: true,
			QuestionsEnabled:      textutil.Value(planned.Plan.QuestionsEnabled),
			AutoCompactionEnabled: textutil.Value(planned.Plan.AutoCompactionEnabled),
			Client:                client,
		})
		if err != nil {
			t.Fatal(err)
		}
		prepared.request.Runtime = &plan
		handle, err := fixture.authority.StartAgentExecution(t.Context(), prepared.request)
		if err != nil {
			t.Fatal(err)
		}
		prepared.plan.handle = handle
		run := &headlessPromptRuntime{
			identity: planned.Identity, plan: prepared.plan, ctx: t.Context(), prompt: "start",
			liveRunObserver: launcher.boot.LiveRunObserver,
		}
		done := make(chan error, 1)
		started := time.Now()
		go func() {
			_, err := run.Complete(2 * time.Second)
			done <- err
		}()
		select {
		case <-client.firstStarted:
		case err := <-done:
			t.Fatalf("RunPrompt completed before first request: %v", err)
		}
		descriptor, err := session.NewScopedOpenSessionDescriptor(id, filepath.Dir(fixture.store.Dir()))
		if err != nil {
			t.Fatal(err)
		}
		err = fixture.authority.RunCurrentTurn(t.Context(), descriptor,
			func(commit func() (bool, error)) (bool, error) { return commit() },
			func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
				_, err := engine.QueueUserInputWithAcceptance(ctx, runtime.QueuedUserInput{
					ID: runtimeids.NewQueueItemID().String(), ExecutionText: "later", CanonicalPresentation: "later",
				}, accept)
				return err
			})
		if err != nil {
			t.Fatal(err)
		}
		close(client.releaseFirst)
		select {
		case <-client.queuedStarted:
		case err := <-done:
			t.Fatalf("RunPrompt completed before queued work started: %v", err)
		}
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("RunPrompt error = %v, want deadline exceeded", err)
		}
		if elapsed := time.Since(started); elapsed != 2*time.Second {
			t.Fatalf("RunPrompt completed after %v, want two-second deadline", elapsed)
		}
		if _, active := fixture.authority.SessionExecution(id); active {
			t.Fatal("timer expiry retained the queued execution")
		}
		if t.Context().Err() != nil {
			t.Fatal("parent context was canceled")
		}
	})
}
