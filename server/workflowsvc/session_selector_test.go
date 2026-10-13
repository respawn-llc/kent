package workflowsvc

import (
	"errors"
	"testing"

	"core/server/session"
	"core/server/workflow"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"google.golang.org/protobuf/proto"
)

func TestWorkflowInterruptResolvesSessionSelectors(t *testing.T) {
	ctx, service, binding, store := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	id := createPersistedWorkflowServiceSession(t, store, binding)
	record, err := store.ResolvePersistedSession(ctx, id.String())
	if err != nil {
		t.Fatal(err)
	}
	execution := newTaskMutationAuthorizationExecutionStub(service)
	service.currentNodeExecution = execution
	for _, selector := range []runtimeids.SessionSelector{id, *record.ShortID} {
		_, err := service.InterruptWorkflowTask(ctx, &taskpb.InterruptRequest{
			TaskId: task.Task.Id, SessionId: protoapi.SessionSelectorToProto(selector),
		})
		if err != nil {
			t.Fatal(err)
		}
		got := execution.interrupts[len(execution.interrupts)-1]
		if got.SessionID == nil || *got.SessionID != id {
			t.Fatalf("selector %s resolved to %+v", selector, got)
		}
	}
}

func TestWorkflowForcedCompletionResolvesSessionSelectors(t *testing.T) {
	for _, numeric := range []bool{false, true} {
		t.Run(map[bool]string{false: "uuid", true: "number"}[numeric], func(t *testing.T) {
			ctx, service, binding, store := newWorkflowServiceTestContextWithMetadata(t)
			workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
			linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
			task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
			started := startWorkflowServiceTask(t, ctx, service, task.Task.Id)
			id := bindWorkflowServiceSessionToTask(t, service, store, binding, workflow.TaskID(task.Task.Id), started.CurrentNodes[0])
			record, err := store.ResolvePersistedSession(ctx, id.String())
			if err != nil {
				t.Fatal(err)
			}
			var selector runtimeids.SessionSelector = id
			if numeric {
				selector = *record.ShortID
			}
			result, err := service.CompleteWorkflowTask(ctx, &taskpb.CompleteRequest{
				SessionId: protoapi.SessionSelectorToProto(selector), Force: true,
				ActorKind: taskpb.CompleteActorKind_COMPLETE_ACTOR_KIND_USER, TransitionId: proto.String("done"),
			})
			if err != nil || result.GetForcedMove().GetTaskId() != task.Task.Id {
				t.Fatalf("complete %s: %+v, %v", selector, result, err)
			}
		})
	}
}

func TestWorkflowMutationMissingNumericSession(t *testing.T) {
	ctx, service, binding, _ := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	short, err := runtimeids.NewSessionShortID(runtimeids.MaxSessionShortID)
	if err != nil {
		t.Fatal(err)
	}
	target := protoapi.SessionSelectorToProto(short)
	_, err = service.InterruptWorkflowTask(ctx, &taskpb.InterruptRequest{TaskId: task.Task.Id, SessionId: target})
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("interrupt missing: %v", err)
	}
	_, err = service.CompleteWorkflowTask(ctx, &taskpb.CompleteRequest{
		SessionId: target, Force: true, ActorKind: taskpb.CompleteActorKind_COMPLETE_ACTOR_KIND_USER,
	})
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("complete missing: %v", err)
	}
}
