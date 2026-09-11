package sessionservice

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"core/server/llm"
	"core/server/metadata"
	"core/server/runtime"
	"core/server/session"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/worktreecontract"

	"github.com/google/uuid"
)

type rebindContinuationClient func(context.Context, llm.Request) (llm.Response, error)

func (c rebindContinuationClient) Generate(ctx context.Context, req llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	return c(ctx, req)
}

func (rebindContinuationClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.InferProviderCapabilities("openai")
}

func TestCrossProjectSelfRebindContinuesWithoutAnotherUserMessage(t *testing.T) {
	fixture := newRealSessionRetargetFixture(t, false)
	retargeter := fixture.retargeter(fixture.metadata, retargetProcessSource{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	continued := make(chan string, 1)
	var engine *runtime.Engine
	calls := 0
	engine = fixture.openRuntimeWithClient(t, rebindContinuationClient(func(ctx context.Context, req llm.Request) (llm.Response, error) {
		calls++
		if calls == 1 {
			active := engine.ActiveRun()
			if active == nil {
				return llm.Response{}, errors.New("originating model Step is missing")
			}
			targetProject := fixture.targetProject.ProjectID
			_, err := retargeter.ScheduleWorkspaceRetarget(ctx, metadata.SessionWorkspaceRetargetRequest{
				SessionID: fixture.childID.String(), WorkspaceRoot: fixture.targetWorkspaceRoot, ProjectID: &targetProject,
			}, &sessionlaunchpb.RuntimeStepOrigin{RunId: active.RunID, StepId: active.StepID}, worktreecontract.NewOperationID())
			if err != nil {
				return llm.Response{}, err
			}
			return llm.Response{
				Assistant: llm.Message{Role: llm.RoleAssistant},
				ToolCalls: []llm.ToolCall{{
					ID: "rebind-tool", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"pwd"}`),
				}},
				Usage: llm.Usage{WindowTokens: 200000},
			}, nil
		}
		if !requestHasRebindReminder(req) {
			t.Error("model continued without the Session move reminder")
		}
		continued <- engine.TranscriptWorkingDir()
		return llm.Response{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("moved and continued"), Phase: textutil.Value(llm.MessagePhaseFinal)},
			Usage:     llm.Usage{WindowTokens: 200000},
		}, nil
	}))
	descriptor, err := session.NewOpenSessionDescriptor(fixture.childID)
	if err != nil {
		t.Fatal(err)
	}
	err = fixture.authority.RunCurrentAgentExecution(ctx, descriptor, func(ctx context.Context, current *runtime.Engine) error {
		_, err := current.SubmitUserMessage(ctx, "move and keep working")
		return err
	})
	if err != nil {
		t.Fatalf("rebind interrupted the originating execution: %v", err)
	}
	select {
	case workdir := <-continued:
		if canonicalRetargetTestPath(t, workdir) != canonicalRetargetTestPath(t, fixture.targetWorkspaceRoot) {
			t.Fatalf("model continued in %q instead of destination %q", workdir, fixture.targetWorkspaceRoot)
		}
	case <-ctx.Done():
		t.Fatal("rebind required another user message before the model could continue")
	}
	if !fixture.runtimeAvailable(t) {
		t.Fatal("successful rebind retired the Session runtime")
	}
}

func TestHumanRebindPreservesExecutionAndQueuedInputAfterCallerDisconnects(t *testing.T) {
	fixture := newRealSessionRetargetFixture(t, false)
	service := NewGlobalSessionLifecycleService(fixture.metadata.PersistenceRoot(), fixture.authority, nil).
		WithWorkspaceRetargeter(fixture.retargeter(fixture.metadata, retargetProcessSource{}))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	requests := make(chan llm.Request, 8)
	sourceDir := fixture.child.Dir()
	var engine *runtime.Engine
	calls := 0
	engine = fixture.openRuntimeWithClient(t, rebindContinuationClient(func(ctx context.Context, req llm.Request) (llm.Response, error) {
		calls++
		if calls == 1 {
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return llm.Response{}, context.Cause(ctx)
			}
			return llm.Response{
				Assistant: llm.Message{Role: llm.RoleAssistant},
				ToolCalls: []llm.ToolCall{{ID: "human-rebind-tool", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"pwd"}`)}},
				Usage:     llm.Usage{WindowTokens: 200000},
			}, nil
		}
		if canonicalRetargetTestPath(t, engine.TranscriptWorkingDir()) != canonicalRetargetTestPath(t, fixture.targetWorkspaceRoot) {
			t.Error("successor model request used the source Working Directory")
		}
		if !requestHasRebindReminder(req) {
			t.Error("model continued without the Session move reminder")
		}
		requests <- req
		return llm.Response{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("continued"), Phase: textutil.Value(llm.MessagePhaseFinal)},
			Usage:     llm.Usage{WindowTokens: 200000},
		}, nil
	}))
	descriptor, err := session.NewOpenSessionDescriptor(fixture.childID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- fixture.authority.RunCurrentAgentExecution(ctx, descriptor, func(ctx context.Context, current *runtime.Engine) error {
			_, err := current.SubmitUserMessage(ctx, "keep working")
			return err
		})
	}()
	select {
	case <-firstStarted:
	case <-ctx.Done():
		t.Fatal(context.Cause(ctx))
	}
	callerCtx, disconnect := context.WithCancel(ctx)
	targetProject := fixture.targetProject.ProjectID
	response, err := service.RetargetSessionWorkspace(callerCtx, &sessionlaunchpb.SessionRetargetWorkspaceRequest{
		SessionId: fixture.childID.String(), WorkspaceRoot: fixture.targetWorkspaceRoot, ProjectId: &targetProject,
	})
	disconnect()
	if err != nil || response.Scheduled == nil || response.Binding != nil {
		t.Fatalf("human rebind was not scheduled during execution: %+v, %v", response, err)
	}
	if fixture.child.Dir() != sourceDir {
		t.Fatal("rebind moved artifacts before the running Step finished")
	}
	const queuedInput = "verify the destination after moving"
	err = fixture.authority.RunCurrentTurn(ctx, descriptor,
		func(commit func() (bool, error)) (bool, error) { return commit() },
		func(ctx context.Context, current *runtime.Engine, accept runtime.CommandAcceptance) error {
			_, accepted, err := current.QueueUserMessageForActiveRunWithAcceptance(ctx, queuedInput, accept)
			if err == nil && !accepted {
				return errors.New("queued input was rejected")
			}
			return err
		})
	if err != nil {
		t.Fatal(err)
	}
	close(releaseFirst)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("rebind interrupted execution: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(context.Cause(ctx))
	}
	close(requests)
	foundInput := false
	for req := range requests {
		for _, item := range req.Items {
			if item.Role != nil && *item.Role == llm.RoleUser && item.Content != nil && *item.Content == queuedInput {
				foundInput = true
			}
		}
	}
	if !foundInput {
		t.Fatal("queued user input did not reach the model after rebind")
	}
	if err := fixture.authority.WithCurrentRuntime(ctx, fixture.childID, func(_ context.Context, current *runtime.Engine) error {
		if current != engine {
			return errors.New("rebind replaced the running agent")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sourceDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source artifact directory was recreated: %v", err)
	}
}

func requestHasRebindReminder(req llm.Request) bool {
	for _, item := range req.Items {
		if item.MessageType != nil && *item.MessageType == llm.MessageTypeSessionRebind {
			return true
		}
	}
	return false
}

func TestSelfRebindRejectsAStaleOriginWithoutInterruptingExecution(t *testing.T) {
	fixture := newRealSessionRetargetFixture(t, false)
	retargeter := fixture.retargeter(fixture.metadata, retargetProcessSource{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	sourceDir := fixture.child.Dir()
	rejected := make(chan error, 1)
	fixture.openRuntimeWithClient(t, rebindContinuationClient(func(ctx context.Context, _ llm.Request) (llm.Response, error) {
		targetProject := fixture.targetProject.ProjectID
		_, err := retargeter.ScheduleWorkspaceRetarget(ctx, metadata.SessionWorkspaceRetargetRequest{
			SessionID: fixture.childID.String(), WorkspaceRoot: fixture.targetWorkspaceRoot, ProjectID: &targetProject,
		}, &sessionlaunchpb.RuntimeStepOrigin{RunId: uuid.NewString(), StepId: uuid.NewString()}, worktreecontract.NewOperationID())
		rejected <- err
		return llm.Response{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("continued"), Phase: textutil.Value(llm.MessagePhaseFinal)},
			Usage:     llm.Usage{WindowTokens: 200000},
		}, nil
	}))
	descriptor, err := session.NewOpenSessionDescriptor(fixture.childID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.authority.RunCurrentAgentExecution(ctx, descriptor, func(ctx context.Context, current *runtime.Engine) error {
		_, err := current.SubmitUserMessage(ctx, "keep working")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-rejected; !errors.Is(err, runtime.ErrActiveStepInactive) {
		t.Fatalf("stale origin error = %v", err)
	}
	if fixture.child.Dir() != sourceDir || !fixture.runtimeAvailable(t) {
		t.Fatal("stale self-rebind changed the Session location or retired its runtime")
	}
}
