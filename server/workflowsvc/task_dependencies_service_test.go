package workflowsvc

import (
	"context"
	"core/shared/apicontract"
	"core/shared/protoapi"
	"errors"
	"testing"
	"time"

	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"

	"google.golang.org/protobuf/proto"
)

func TestServiceTaskDependencyMutationEventsAreTypedAndIdempotent(t *testing.T) {
	ctx, service, projectID, workflowID, _ := newWorkflowServiceOrdinaryTaskFixture(t)
	createdBlocker, err := service.CreateWorkflowTask(ctx, &taskpb.CreateRequest{
		ProjectId:  projectID,
		WorkflowId: proto.String(workflowID.String()),
		Title:      "blocker",
		LabelIds:   []string{},
	})
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	createdBlocked, err := service.CreateWorkflowTask(ctx, &taskpb.CreateRequest{
		ProjectId:  projectID,
		WorkflowId: proto.String(workflowID.String()),
		Title:      "blocked",
		LabelIds:   []string{},
	})
	if err != nil {
		t.Fatalf("create blocked: %v", err)
	}
	sub, err := service.SubscribeWorkflowProject(ctx, &pb.ProjectSubscribeRequest{ProjectId: proto.String(projectID)})
	if err != nil {
		t.Fatalf("subscribe project: %v", err)
	}
	defer func() { _ = sub.Close() }()

	added, err := service.AddWorkflowTaskDependency(ctx, &taskpb.DependencyAddRequest{
		BlockerTaskId: createdBlocker.Task.Id,
		BlockedTaskId: createdBlocked.Task.Id,
	})
	if err != nil {
		t.Fatalf("add dependency: %v", err)
	}
	if added.Outcome != taskpb.DependencyMutationOutcome_DEPENDENCY_MUTATION_OUTCOME_ADDED {
		t.Fatalf("add response = %+v, want added", added)
	}
	event := nextWorkflowProjectEvent(t, sub)
	if event.Resource != pb.ProjectEventResource_WORKFLOW_PROJECT_EVENT_RESOURCE_TASK ||
		event.Action != pb.ProjectEventAction_WORKFLOW_PROJECT_EVENT_ACTION_DEPENDENCIES_CHANGED ||
		event.PrimaryEntityId != createdBlocker.Task.Id ||
		len(event.RelatedIds) != 1 || event.RelatedIds[0] != createdBlocked.Task.Id {
		t.Fatalf("dependency event = %+v", event)
	}
	list, err := service.ListWorkflowTaskDependencies(ctx, &taskpb.DependencyListRequest{TaskId: createdBlocked.Task.Id})
	if err != nil {
		t.Fatalf("list dependencies: %v", err)
	}
	if err := protoapi.Validate(list); err != nil {
		t.Fatalf("list Validate: %v", err)
	}
	if len(list.Directions) != 1 ||
		list.Directions[0].Direction != taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKED_BY {
		t.Fatalf("list response = %+v, want one non-empty blocked-by direction", list)
	}

	idempotent, err := service.AddWorkflowTaskDependency(ctx, &taskpb.DependencyAddRequest{
		BlockerTaskId: createdBlocker.Task.Id,
		BlockedTaskId: createdBlocked.Task.Id,
	})
	if err != nil {
		t.Fatalf("idempotent add: %v", err)
	}
	if idempotent.Outcome != taskpb.DependencyMutationOutcome_DEPENDENCY_MUTATION_OUTCOME_ALREADY_PRESENT {
		t.Fatalf("idempotent add response = %+v", idempotent)
	}
	assertNoWorkflowEvent(t, sub)

	removed, err := service.RemoveWorkflowTaskDependency(ctx, &taskpb.DependencyRemoveRequest{
		BlockerTaskId: createdBlocker.Task.Id,
		BlockedTaskId: createdBlocked.Task.Id,
	})
	if err != nil {
		t.Fatalf("remove dependency: %v", err)
	}
	if removed.Outcome != taskpb.DependencyMutationOutcome_DEPENDENCY_MUTATION_OUTCOME_REMOVED {
		t.Fatalf("remove response = %+v, want removed", removed)
	}
	event = nextWorkflowProjectEvent(t, sub)
	if event.Action != pb.ProjectEventAction_WORKFLOW_PROJECT_EVENT_ACTION_DEPENDENCIES_CHANGED {
		t.Fatalf("remove event = %+v", event)
	}

	absent, err := service.RemoveWorkflowTaskDependency(ctx, &taskpb.DependencyRemoveRequest{
		BlockerTaskId: createdBlocker.Task.Id,
		BlockedTaskId: createdBlocked.Task.Id,
	})
	if err != nil {
		t.Fatalf("idempotent remove: %v", err)
	}
	if absent.Outcome != taskpb.DependencyMutationOutcome_DEPENDENCY_MUTATION_OUTCOME_ALREADY_ABSENT {
		t.Fatalf("idempotent remove response = %+v", absent)
	}
	assertNoWorkflowEvent(t, sub)
}

func TestServiceTaskCreatePublishesOneDependencyEventForEveryAffectedTask(t *testing.T) {
	ctx, service, projectID, workflowID, _ := newWorkflowServiceOrdinaryTaskFixture(t)
	blocked, err := service.CreateWorkflowTask(ctx, &taskpb.CreateRequest{
		ProjectId: projectID, WorkflowId: proto.String(workflowID.String()), Title: "blocked", LabelIds: []string{},
	})
	if err != nil {
		t.Fatalf("create blocked: %v", err)
	}
	blocker, err := service.CreateWorkflowTask(ctx, &taskpb.CreateRequest{
		ProjectId: projectID, WorkflowId: proto.String(workflowID.String()), Title: "blocker", LabelIds: []string{},
	})
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	sub, err := service.SubscribeWorkflowProject(ctx, &pb.ProjectSubscribeRequest{ProjectId: proto.String(projectID)})
	if err != nil {
		t.Fatalf("subscribe project: %v", err)
	}
	defer func() { _ = sub.Close() }()

	created, err := service.CreateWorkflowTask(ctx, &taskpb.CreateRequest{
		ProjectId:  projectID,
		WorkflowId: proto.String(workflowID.String()),
		Title:      "mixed",
		LabelIds:   []string{},
		DependencyIntents: []*taskpb.DependencyCreateIntent{
			{RelatedTaskId: blocked.Task.Id, NewTaskRole: taskpb.DependencyRole_DEPENDENCY_ROLE_BLOCKER},
			{RelatedTaskId: blocker.Task.Id, NewTaskRole: taskpb.DependencyRole_DEPENDENCY_ROLE_BLOCKED},
		},
	})
	if err != nil {
		t.Fatalf("create with dependencies: %v", err)
	}
	createdEvent := nextWorkflowProjectEvent(t, sub)
	if createdEvent.Action != pb.ProjectEventAction_WORKFLOW_PROJECT_EVENT_ACTION_CREATED ||
		createdEvent.PrimaryEntityId != created.Task.Id {
		t.Fatalf("created event = %+v", createdEvent)
	}
	dependencyEvent := nextWorkflowProjectEvent(t, sub)
	if dependencyEvent.Action != pb.ProjectEventAction_WORKFLOW_PROJECT_EVENT_ACTION_DEPENDENCIES_CHANGED ||
		dependencyEvent.PrimaryEntityId != created.Task.Id {
		t.Fatalf("dependency event = %+v", dependencyEvent)
	}
	gotRelated := map[string]bool{}
	for _, taskID := range dependencyEvent.RelatedIds {
		gotRelated[taskID] = true
	}
	if len(gotRelated) != 2 || !gotRelated[blocked.Task.Id] || !gotRelated[blocker.Task.Id] {
		t.Fatalf("dependency event related IDs = %+v, want both affected Tasks", dependencyEvent.RelatedIds)
	}
	assertNoWorkflowEvent(t, sub)
}

func assertNoWorkflowEvent(t *testing.T, sub apicontract.WorkflowEventSubscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := sub.Next(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Next() error = %v, want deadline exceeded", err)
	}
}
