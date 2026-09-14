package transport

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	testharness "core/internal/testharness/testsetup"
	"core/server/attentionnotify"
	"core/server/core"
	"core/server/registry"
	askquestion "core/server/tools"
	"core/shared/apicontract"
	remoteclient "core/shared/client"
	"core/shared/clientui"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func TestGatewayRemoteAttentionDesktopRouteIsRootGlobalAndKeepsQuestionsLiveOnly(t *testing.T) {
	appCore, _, broker, server := newGatewayAttentionTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()

	sessionOne := createGatewayAuthoritativeSession(t, appCore)
	activateGatewayController(t, appCore, sessionOne.Meta().SessionID)
	sessionTwo := createGatewayAuthoritativeSession(t, appCore)
	activateGatewayController(t, appCore, sessionTwo.Meta().SessionID)

	beginGatewayPendingPrompt(t, broker, sessionOne.Meta().SessionID, gatewayTaskBatchAskRequest("old-ask", "project-old", "task-old", sessionOne.Meta().SessionID))
	remote, err := remoteclient.DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()
	desktop, err := remote.SubscribeAttentionNotifications(context.Background(), serverapi.AttentionNotificationSubscribeRequest{})
	if err != nil {
		t.Fatalf("SubscribeAttentionNotifications: %v", err)
	}
	if event, err := desktop.Next(shortGatewayAttentionContext(t)); err == nil {
		t.Fatalf("desktop subscription replayed old pending attention: %+v", event)
	}

	beginGatewayPendingPrompt(t, broker, sessionOne.Meta().SessionID, gatewayTaskBatchAskRequest("ask-a", "project-a", "task-a", sessionOne.Meta().SessionID))
	beginGatewayPendingPrompt(t, broker, sessionTwo.Meta().SessionID, gatewayTaskBatchAskRequest("ask-b", "project-b", "task-b", sessionTwo.Meta().SessionID))
	first := nextGatewayAttentionEvent(t, desktop)
	second := nextGatewayAttentionEvent(t, desktop)
	if first.Pending.Target.ProjectID != "project-a" || second.Pending.Target.ProjectID != "project-b" {
		t.Fatalf("desktop cross-project events = %+v then %+v", first, second)
	}

	beginGatewayPendingPrompt(t, broker, sessionOne.Meta().SessionID, askquestion.AskQuestionRequest{ToolCallID: "generic-ask", StepID: gatewayAttentionStepID, Question: "Generic?"})
	if event := nextGatewayAttentionEvent(t, desktop); event.Pending == nil || event.Pending.Target.ProjectID != "project-1" || event.Pending.Target.SessionID != sessionOne.Meta().SessionID {
		t.Fatalf("desktop generic session prompt = %+v", event)
	}
}

func TestGatewaySessionAttentionRouteRequiresAttachedSession(t *testing.T) {
	appCore, _, _, server := newGatewayAttentionTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	sessionOne := createGatewayAuthoritativeSession(t, appCore)
	activateGatewayController(t, appCore, sessionOne.Meta().SessionID)
	sessionTwo := createGatewayAuthoritativeSession(t, appCore)
	activateGatewayController(t, appCore, sessionTwo.Meta().SessionID)

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	method := attentionpb.File_kent_api_attention_attention_proto.Services().ByName("SessionService").Methods().ByName("Subscribe")
	var missing attentionpb.StartResult
	callGatewayDescriptor(t, conn, "attention-without-attach", method, &attentionpb.SubscribeRequest{SessionId: sessionOne.Meta().SessionID}, &missing)
	if missing.GetError() == nil {
		t.Fatalf("missing attach accepted: %+v", &missing)
	}
	_ = conn.Close()

	conn = dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	if result := attachGatewaySession(t, conn, "attach-session-one", sessionOne.Meta().SessionID); result.GetSuccess() == nil {
		t.Fatalf("attach Session one failed: %+v", result.GetError())
	}
	var wrong attentionpb.StartResult
	callGatewayDescriptor(t, conn, "attention-wrong-session", method, &attentionpb.SubscribeRequest{SessionId: sessionTwo.Meta().SessionID}, &wrong)
	if wrong.GetError() == nil {
		t.Fatalf("wrong attach accepted: %+v", &wrong)
	}
}

func TestGatewayRemoteSessionAttentionReceivesAuthorizedGenericPrompt(t *testing.T) {
	appCore, _, broker, server := newGatewayAttentionTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	sessionStore := createGatewayAuthoritativeSession(t, appCore)
	activateGatewayController(t, appCore, sessionStore.Meta().SessionID)

	remote, err := remoteclient.DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()
	sub, err := remote.SubscribeSessionAttentionNotifications(context.Background(), &attentionpb.SubscribeRequest{SessionId: sessionStore.Meta().SessionID})
	if err != nil {
		t.Fatalf("SubscribeSessionAttentionNotifications: %v", err)
	}
	beginGatewayPendingPrompt(t, broker, sessionStore.Meta().SessionID, askquestion.AskQuestionRequest{ToolCallID: "generic-ask", StepID: gatewayAttentionStepID, Question: "Generic?"})
	pending, err := sub.Next(shortGatewayAttentionContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if pending.GetPending() == nil || pending.GetPending().SessionId != sessionStore.Meta().SessionID {
		t.Fatalf("session prompt pending = %+v", pending)
	}
}

type gatewayAttentionDependencies struct {
	*core.Core
	attention apicontract.AttentionNotificationService
}

func (d *gatewayAttentionDependencies) AttentionNotificationClient() apicontract.AttentionNotificationService {
	return d.attention
}

func newGatewayAttentionTestServer(t *testing.T) (*core.Core, *registry.RuntimeRegistry, *attentionnotify.Broker, *httptest.Server) {
	t.Helper()
	appCore, _ := newGatewayTestCore(t, true, true)
	broker := attentionnotify.NewBroker()
	prompts := registry.NewRuntimeRegistry().WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	gateway, err := NewGateway(
		&gatewayAttentionDependencies{Core: appCore, attention: prompts},
		gatewayTestIdentity(),
	)
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	return appCore, prompts, broker, httptest.NewServer(gateway.Handler())
}

func beginGatewayPendingPrompt(t *testing.T, broker *attentionnotify.Broker, sessionID string, request askquestion.AskQuestionRequest) {
	t.Helper()
	kind := clientui.AttentionNotificationKindQuestion
	if request.Approval && !request.IsTaskScopedApprovalQuestion() {
		kind = clientui.AttentionNotificationKindApproval
	}
	target := clientui.AttentionNotificationTarget{Kind: clientui.AttentionNotificationTargetSessionPrompt, ProjectID: "project-1", SessionID: sessionID}
	scope := attentionnotify.RoutingScope{Kind: attentionnotify.RoutingSessionPrompt, SessionID: sessionID}
	if request.AttentionTarget != nil && request.AttentionTarget.Kind == clientui.AttentionNotificationTargetWorkflowTask {
		target = *request.AttentionTarget
		scope = attentionnotify.RoutingScope{Kind: attentionnotify.RoutingWorkflowTask, TaskID: target.TaskID, SessionID: sessionID}
	}
	notification := clientui.AttentionNotification{
		ID:   clientui.AttentionNotificationID{Kind: kind, UUID: request.ToolCallID},
		Kind: kind, OccurredAt: time.Now().UTC(), Revision: 1, Target: target,
	}
	if kind == clientui.AttentionNotificationKindApproval {
		notification.Approval = &clientui.AttentionNotificationApprovalState{Message: textutil.Value(request.Question)}
	} else {
		notification.Question = &clientui.AttentionNotificationQuestionState{
			PreparedAskIDs: []string{request.ToolCallID}, MaterializedAskIDs: []string{request.ToolCallID},
			CurrentUnresolvedAskIDs: []string{request.ToolCallID}, Preview: request.Question,
			DisplayCount: 1, MaterializedCount: 1,
		}
	}
	if err := broker.PublishPending(scope, notification); err != nil {
		t.Fatalf("publish pending prompt: %v", err)
	}
}

func gatewayPromptResource(sessionID string) runtimeids.SessionResourceRef {
	id, err := runtimeids.ParseSessionID(sessionID)
	if err != nil {
		panic(err)
	}
	resource, err := runtimeids.NewSessionResourceRef(id, 1)
	if err != nil {
		panic(err)
	}
	return resource
}

func gatewayTaskBatchAskRequest(askID string, projectID string, taskID string, sessionID string) askquestion.AskQuestionRequest {
	currentNodeID := "node-" + taskID
	workflowID := runtimeids.NewWorkflowID()
	return askquestion.AskQuestionRequest{
		ToolCallID: askID,
		StepID:     gatewayAttentionStepID,
		Question:   "Task question?",
		QuestionBatch: &askquestion.AskQuestionBatchMetadata{
			Origin:              askquestion.AskQuestionOriginModelTool,
			RunID:               "run-" + taskID,
			StepID:              gatewayAttentionStepID,
			ToolCallID:          askID,
			BatchToolCallIDs:    []string{askID},
			CandidateOrdinal:    0,
			PreparedPromptCount: 1,
		},
		AttentionTarget: &clientui.AttentionNotificationTarget{
			Kind:          clientui.AttentionNotificationTargetWorkflowTask,
			ProjectID:     projectID,
			WorkflowID:    &workflowID,
			TaskID:        taskID,
			SessionID:     sessionID,
			CurrentNodeID: &currentNodeID,
			Focus: &clientui.AttentionNotificationTaskDetailFocus{
				Kind:   clientui.AttentionNotificationFocusQuestion,
				AskIDs: []string{askID},
			},
		},
	}
}

const gatewayAttentionStepID = "11111111-1111-4111-8111-111111111111"

func nextGatewayAttentionEvent(t *testing.T, sub serverapi.AttentionNotificationSubscription) clientui.AttentionNotificationEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return event
}

func shortGatewayAttentionContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}
