package registry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	testharness "core/internal/testharness/testsetup"
	"core/server/agentruntime"
	"core/server/attentionnotify"
	"core/server/llm"
	askquestion "core/server/tools"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/shared/clientui"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"core/shared/textutil"

	"core/shared/workflowcontract"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestCompletedAssistantTurnPublishesAttentionWithoutTranscriptSubscriber(t *testing.T) {
	node, err := workflow.NewCurrentNodeReference("notification-task", "notification-node", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		status runtime.RunStatus
		kind   runtime.LiveRunResultKind
		work   bool
		want   bool
		config runtime.Config
	}{
		{"completed", runtime.RunStatusCompleted, runtime.LiveRunResultAssistantFinalAnswer, true, true, runtime.Config{}},
		{"few tools", runtime.RunStatusCompleted, runtime.LiveRunResultAssistantFinalAnswer, false, false, runtime.Config{}},
		{"interrupted", runtime.RunStatusInterrupted, runtime.LiveRunResultAssistantFinalAnswer, true, false, runtime.Config{}},
		{"no final", runtime.RunStatusCompleted, runtime.LiveRunResultNoFinalAnswer, true, false, runtime.Config{}},
		{"headless", runtime.RunStatusCompleted, runtime.LiveRunResultAssistantFinalAnswer, true, false, runtime.Config{HeadlessMode: true}},
		{"workflow", runtime.RunStatusCompleted, runtime.LiveRunResultAssistantFinalAnswer, true, false, runtime.Config{
			CurrentNodeExecution: &workflowruntime.CurrentNodeExecutionConfig{
				ScopeID:      runtimeids.NewExecutionScopeID(),
				Instructions: workflowruntime.TaskInstructions{CurrentNode: node, WorkflowID: runtimeids.NewWorkflowID()},
			},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			broker := attentionnotify.NewBroker()
			registry := NewRuntimeRegistry(testharness.OpenStore(t, t.TempDir())).WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
			cfg := test.config
			cfg.Model = "gpt-6-sol"
			engine := newRegistryRuntime(t, registryRuntimeFakeClient{}, askquestion.NewRegistry(), cfg, nil)
			if cfg.CurrentNodeExecution != nil {
				binding, err := engine.BindCurrentNodeExecution(cfg.CurrentNodeExecution)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = binding.Close() })
			}
			registerReady(t, registry, engine.SessionID(), engine)
			t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })
			sub, err := registry.SubscribeAttentionNotifications(t.Context(), &emptypb.Empty{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sub.Close() })
			stepID, err := runtimeids.ParseStepID(registryTestStepID)
			if err != nil {
				t.Fatal(err)
			}
			result := runtime.LiveRunResult{
				GroupID: runtimeids.NewLiveRunGroupID(), StepID: stepID,
				Status: test.status, ResultKind: test.kind, WorkPerformed: test.work,
				AssistantMessage: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("completed work")},
				StartedAt:        time.Now().Add(-time.Second), FinishedAt: time.Now(),
			}
			if err := registry.PublishAuthorityRuntimeEvent(registryTestResourceRef(engine.SessionID()), runtime.Event{
				Kind: runtime.EventLiveRunFinished, LiveRunResult: &result,
			}); err != nil {
				t.Fatal(err)
			}
			event, err := sub.Next(shortRegistryContext(t))
			if !test.want {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("excluded completion emitted attention: event=%+v err=%v", event, err)
				}
				return
			}
			if err != nil || event.GetSessionTurnEnd() == nil || event.GetSessionTurnEnd().SessionId != engine.SessionID() ||
				!event.GetSessionTurnEnd().CompletedAt.AsTime().Equal(result.FinishedAt) {
				t.Fatalf("completion attention = %+v, %v", event, err)
			}
		})
	}
}

func TestRuntimeRegistryDeliversGenericPromptAttentionToDesktopRootStream(t *testing.T) {
	broker := attentionnotify.NewBroker()
	registry := NewRuntimeRegistry(testharness.OpenStore(t, t.TempDir())).WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	engine := newRegistryTestRuntime(t, nil)
	registerReady(t, registry, engine.SessionID(), engine)
	t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })
	sessionSub, err := registry.SubscribeSessionAttentionNotifications(context.Background(), &attentionpb.SubscribeRequest{SessionId: engine.SessionID()})
	if err != nil {
		t.Fatalf("SubscribeSessionAttentionNotifications: %v", err)
	}
	desktopSub, err := registry.SubscribeAttentionNotifications(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("SubscribeAttentionNotifications: %v", err)
	}

	projectPendingPromptForTest(registry, engine.SessionID(), askquestion.AskQuestionRequest{ToolCallID: "ask-1", StepID: registryTestStepID, Question: "Proceed?"})
	pending := nextRegistryAttentionEvent(t, sessionSub).GetPending()
	if pending == nil || pending.Id.Kind != attentionpb.Kind_ATTENTION_KIND_QUESTION || pending.Id.Uuid != "ask-1" || pending.SessionId != engine.SessionID() {
		t.Fatalf("pending event = %+v", pending)
	}
	desktopPending := nextRegistryAttentionEvent(t, desktopSub)
	if desktopPending.GetPending().GetTarget().GetSessionPrompt().GetProjectId() != "project-1" ||
		desktopPending.GetPending().GetTarget().GetSessionPrompt().GetSessionId() != engine.SessionID() {
		t.Fatalf("desktop pending = %+v", desktopPending)
	}
	if question := desktopPending.GetPending().GetQuestion(); question == nil || question.DisplayCount != 1 {
		t.Fatalf("ordinary Session question state = %v", desktopPending)
	}
	resolvePendingPromptForTest(registry, engine.SessionID(), "ask-1")
	resolved := nextRegistryAttentionEvent(t, sessionSub).GetResolved()
	if resolved == nil || resolved.Id.Kind != attentionpb.Kind_ATTENTION_KIND_QUESTION || resolved.Id.Uuid != "ask-1" {
		t.Fatalf("resolved event = %+v", resolved)
	}
	for {
		event := nextRegistryAttentionEvent(t, desktopSub)
		if event.GetResolved() != nil {
			if event.GetResolved().GetId().GetUuid() != "ask-1" {
				t.Fatalf("desktop resolved = %+v", event)
			}
			break
		}
	}
}

func TestRuntimeRegistryPublishesGenericApprovalToSessionAttention(t *testing.T) {
	broker := attentionnotify.NewBroker()
	registry := NewRuntimeRegistry(testharness.OpenStore(t, t.TempDir())).WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	engine := newRegistryTestRuntime(t, nil)
	registerReady(t, registry, engine.SessionID(), engine)
	t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })
	sessionSub, err := registry.SubscribeSessionAttentionNotifications(context.Background(), &attentionpb.SubscribeRequest{SessionId: engine.SessionID()})
	if err != nil {
		t.Fatalf("SubscribeSessionAttentionNotifications: %v", err)
	}

	projectPendingPromptForTest(registry, engine.SessionID(), askquestion.AskQuestionRequest{
		ToolCallID:    "approval-1",
		StepID:        registryTestStepID,
		Approval:      true,
		AccessTargets: []askquestion.FileAccessTarget{{RequestedPath: "../outside.txt", ResolvedPath: "/outside.txt"}},
		ApprovalOptions: []askquestion.AskQuestionApprovalOption{
			{Decision: askquestion.AskQuestionApprovalDecisionAllowOnce},
			{Decision: askquestion.AskQuestionApprovalDecisionDeny},
		},
	})

	pending, err := sessionSub.Next(shortRegistryContext(t))
	if err != nil {
		t.Fatalf("generic approval attention: %v", err)
	}
	if pending.GetPending() == nil ||
		pending.GetPending().Id.Kind != attentionpb.Kind_ATTENTION_KIND_APPROVAL ||
		pending.GetPending().SessionId != engine.SessionID() ||
		pending.GetPending().GetApproval() == nil ||
		len(pending.GetPending().GetApproval().AccessTargets) != 1 ||
		pending.GetPending().GetApproval().AccessTargets[0].RequestedPath != "../outside.txt" {
		t.Fatalf("generic approval attention = %+v", pending)
	}
	newSub, err := registry.SubscribeSessionAttentionNotifications(context.Background(), &attentionpb.SubscribeRequest{SessionId: engine.SessionID()})
	if err != nil {
		t.Fatalf("SubscribeSessionAttentionNotifications: %v", err)
	}
	defer newSub.Close()
	if event, err := newSub.Next(shortRegistryContext(t)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("new subscription replayed a pending approval: event=%+v, err=%v", event, err)
	}
}

func TestRuntimeRegistryPublishesTaskQuestionBatchWithoutGenericResolve(t *testing.T) {
	broker := attentionnotify.NewBroker()
	registry := NewRuntimeRegistry(testharness.OpenStore(t, t.TempDir())).WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	engine := newRegistryTestRuntime(t, nil)
	registerReady(t, registry, engine.SessionID(), engine)
	t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })
	desktopSub, err := registry.SubscribeAttentionNotifications(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("SubscribeAttentionNotifications: %v", err)
	}
	req := taskBatchAskRequest(engine.SessionID(), "ask-1")
	projectPendingPromptForTest(registry, engine.SessionID(), req)

	pending := nextRegistryAttentionEvent(t, desktopSub)
	if pending.GetPending().GetId().GetUuid() != registryTestStepID ||
		pending.GetPending().GetId().GetKind() != taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_QUESTION {
		t.Fatalf("pending id = %v", pending.GetPending().GetId())
	}
	if pending.GetPending().GetQuestion().GetDisplayCount() != 2 {
		t.Fatalf("question state = %+v", pending)
	}
	resolvePendingPromptForTest(registry, engine.SessionID(), "ask-1")
	if event, err := desktopSub.Next(shortRegistryContext(t)); err == nil {
		t.Fatalf("task question resolved from prompt answer before durable clear: %+v", event)
	}
	skipped := *req.QuestionBatch
	skipped.ToolCallID = "ask-2"
	registry.MarkTaskQuestionSkipped(skipped)
	if event, err := desktopSub.Next(shortRegistryContext(t)); err == nil {
		t.Fatalf("skip published duplicate pending attention: %+v", event)
	}
	registry.MarkTaskQuestionCleared(*req.QuestionBatch, "ask-1")
	resolved := nextRegistryAttentionEvent(t, desktopSub)
	if !resolvedQuestionIDMatches(resolved, registryTestStepID) {
		t.Fatalf("durable clear resolved event = %+v", resolved)
	}
}

func TestRuntimeRegistryPublishesTaskApprovalPromptAsDurablyClearedQuestionAttention(t *testing.T) {
	broker := attentionnotify.NewBroker()
	registry := NewRuntimeRegistry(testharness.OpenStore(t, t.TempDir())).WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	engine := newRegistryTestRuntime(t, nil)
	registerReady(t, registry, engine.SessionID(), engine)
	t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })
	desktopSub, err := registry.SubscribeAttentionNotifications(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("SubscribeAttentionNotifications: %v", err)
	}
	target := clientui.AttentionNotificationTarget{
		Kind:       clientui.AttentionNotificationTargetWorkflowTask,
		ProjectID:  "project-1",
		WorkflowID: registryTestWorkflowID(),
		TaskID:     "task-1",
		SessionID:  engine.SessionID(),
		Focus: &clientui.AttentionNotificationTaskDetailFocus{
			Kind:   clientui.AttentionNotificationFocusQuestion,
			AskIDs: []string{"approval-1"},
		},
	}
	req := askquestion.AskQuestionRequest{
		ToolCallID:      "approval-1",
		StepID:          registryTestStepID,
		Question:        "Approve protected path?",
		Approval:        true,
		AttentionTarget: &target,
		ApprovalOptions: []askquestion.AskQuestionApprovalOption{
			{Decision: askquestion.AskQuestionApprovalDecisionAllowOnce},
			{Decision: askquestion.AskQuestionApprovalDecisionDeny},
		},
	}
	projectPendingPromptForTest(registry, engine.SessionID(), req)

	pending := nextRegistryAttentionEvent(t, desktopSub)
	if pending.GetPending() == nil ||
		pending.GetPending().GetId().GetKind() != taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_QUESTION ||
		pending.GetPending().GetTarget().GetWorkflowTask() == nil {
		t.Fatalf("pending approval question event = %+v", pending)
	}
	question := pending.GetPending().GetQuestion()
	if question == nil || question.Preview != req.Question || question.DisplayCount != 1 {
		t.Fatalf("pending approval question state = %+v", question)
	}
	if pending.GetPending().GetApproval() != nil {
		t.Fatalf("task-scoped approval prompt must not publish approval attention: %+v", pending)
	}
	resolvePendingPromptForTest(registry, engine.SessionID(), "approval-1")
	if event, err := desktopSub.Next(shortRegistryContext(t)); err == nil {
		t.Fatalf("prompt response resolved task approval before durable clear: %+v", event)
	}
	registry.MarkTaskApprovalQuestionCleared(target, "approval-1")
	resolved := nextRegistryAttentionEvent(t, desktopSub)
	if !resolvedQuestionIDMatches(resolved, "approval-1") {
		t.Fatalf("durable clear resolved event = %+v", resolved)
	}
}

type recordingWorkflowEventPublisher struct {
	events []workflowcontract.Event
}

func (p *recordingWorkflowEventPublisher) PublishWorkflowEvent(_ context.Context, event workflowcontract.Event) error {
	p.events = append(p.events, event)
	return nil
}

func TestRuntimeRegistrySkippedFirstTaskQuestionPreparesBatchBeforeMaterialization(t *testing.T) {
	broker := attentionnotify.NewBroker()
	registry := NewRuntimeRegistry(testharness.OpenStore(t, t.TempDir())).WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	engine := newRegistryTestRuntime(t, nil)
	registerReady(t, registry, engine.SessionID(), engine)
	t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })
	desktopSub, err := registry.SubscribeAttentionNotifications(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("SubscribeAttentionNotifications: %v", err)
	}
	first := taskBatchAskRequest(engine.SessionID(), "ask-1")
	if err := registry.PrepareTaskQuestionBatch(*first.QuestionBatch, engine.SessionID(), first.AttentionTarget, first.Question, time.Now().UTC()); err != nil {
		t.Fatalf("PrepareTaskQuestionBatch: %v", err)
	}
	registry.MarkTaskQuestionSkipped(*first.QuestionBatch)

	second := taskBatchAskRequest(engine.SessionID(), "ask-2")
	projectPendingPromptForTest(registry, engine.SessionID(), second)

	pending := nextRegistryAttentionEvent(t, desktopSub)
	if pending.GetPending() == nil || pending.GetPending().GetQuestion().GetDisplayCount() != 1 {
		t.Fatalf("pending after skipped first ask = %+v", pending)
	}
	resolvePendingPromptForTest(registry, engine.SessionID(), "ask-2")
	registry.MarkTaskQuestionCleared(*second.QuestionBatch, "ask-2")
	resolved := nextRegistryAttentionEvent(t, desktopSub)
	if !resolvedQuestionIDMatches(resolved, registryTestStepID) {
		t.Fatalf("resolved event = %+v", resolved)
	}
}

func TestRuntimeRegistrySessionAttentionDoesNotReplayPendingPrompts(t *testing.T) {
	broker := attentionnotify.NewBroker()
	registry := NewRuntimeRegistry(testharness.OpenStore(t, t.TempDir())).WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	engine := newRegistryTestRuntime(t, nil)
	registerReady(t, registry, engine.SessionID(), engine)
	t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })
	for i := 0; i < 65; i++ {
		projectPendingPromptForTest(registry, engine.SessionID(), askquestion.AskQuestionRequest{ToolCallID: fmt.Sprintf("ask-%d", i), StepID: registryTestStepID, Question: "Proceed?"})
	}

	sub, err := registry.SubscribeSessionAttentionNotifications(context.Background(), &attentionpb.SubscribeRequest{SessionId: engine.SessionID()})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if event, err := sub.Next(shortRegistryContext(t)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("subscription replayed pending prompts: event=%+v, err=%v", event, err)
	}
}

func TestRuntimeRegistrySessionAttentionOnlyReceivesNewTaskQuestionBatchEvents(t *testing.T) {
	broker := attentionnotify.NewBroker()
	registry := NewRuntimeRegistry(testharness.OpenStore(t, t.TempDir())).WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	engine := newRegistryTestRuntime(t, nil)
	registerReady(t, registry, engine.SessionID(), engine)
	t.Cleanup(func() { closeRuntime(registry, engine.SessionID(), engine) })
	req := taskBatchAskRequest(engine.SessionID(), "ask-1")
	projectPendingPromptForTest(registry, engine.SessionID(), req)

	sub, err := registry.SubscribeSessionAttentionNotifications(context.Background(), &attentionpb.SubscribeRequest{SessionId: engine.SessionID()})
	if err != nil {
		t.Fatalf("SubscribeSessionAttentionNotifications: %v", err)
	}
	defer sub.Close()
	if event, err := sub.Next(shortRegistryContext(t)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("subscription replayed a question batch: event=%+v, err=%v", event, err)
	}
	skipped := *req.QuestionBatch
	skipped.ToolCallID = "ask-2"
	registry.MarkTaskQuestionSkipped(skipped)
	if event, err := sub.Next(shortRegistryContext(t)); err == nil {
		t.Fatalf("skip published duplicate pending attention: %+v", event)
	}
	registry.MarkTaskQuestionCleared(*req.QuestionBatch, "ask-1")
	resolved := nextRegistryAttentionEvent(t, sub).GetResolved()
	if resolved == nil || resolved.Id.Kind != attentionpb.Kind_ATTENTION_KIND_QUESTION || resolved.Id.Uuid != registryTestStepID {
		t.Fatalf("resolved event = %+v", resolved)
	}
}

func nextRegistryAttentionEvent[T any](t *testing.T, sub interface {
	Next(context.Context) (T, error)
}) T {
	t.Helper()
	event, err := sub.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return event
}

func resolvedQuestionIDMatches(event *taskpb.AttentionNotificationEvent, id string) bool {
	return event.GetResolved().GetId().GetKind() == taskpb.AttentionNotificationKind_ATTENTION_NOTIFICATION_KIND_QUESTION &&
		event.GetResolved().GetId().GetUuid() == id
}

func registryTestWorkflowID() *runtimeids.WorkflowID {
	workflowID := testharness.WorkflowIDValue("registry-workflow-task")
	return &workflowID
}

func shortRegistryContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func taskBatchAskRequest(sessionID, id string) askquestion.AskQuestionRequest {
	currentNodeID := "node-1"
	return askquestion.AskQuestionRequest{
		ToolCallID: id,
		StepID:     registryTestStepID,
		Question:   "Proceed?",
		QuestionBatch: &askquestion.AskQuestionBatchMetadata{
			Origin:              askquestion.AskQuestionOriginModelTool,
			StepID:              registryTestStepID,
			ToolCallID:          id,
			BatchToolCallIDs:    []string{"ask-1", "ask-2"},
			CandidateOrdinal:    0,
			PreparedPromptCount: 2,
		},
		AttentionTarget: &clientui.AttentionNotificationTarget{
			Kind:          clientui.AttentionNotificationTargetWorkflowTask,
			WorkflowID:    registryTestWorkflowID(),
			TaskID:        "task-1",
			SessionID:     sessionID,
			CurrentNodeID: &currentNodeID,
			Focus: &clientui.AttentionNotificationTaskDetailFocus{
				Kind:   clientui.AttentionNotificationFocusQuestion,
				AskIDs: []string{"ask-1", "ask-2"},
			},
		},
	}
}
