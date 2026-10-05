package workflowsvc

import (
	"context"
	"core/internal/testharness/workflowfixture"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"core/server/metadata"
	"core/server/session"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowstore"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"core/shared/worktreecontract"
	"google.golang.org/protobuf/proto"
)

func TestWorkflowSessionCannotStartItsOwnTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	started := startWorkflowServiceTask(t, ctx, service, task.Task.Id)
	sessionID := bindWorkflowServiceSessionToTask(
		t,
		service,
		metadataStore,
		binding,
		workflow.TaskID(task.Task.Id),
		started.CurrentNodes[0],
	)

	_, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
		SetupOperationId:  worktreecontract.NewSetupOperationID().String(),
	})
	var denied *serverapi.WorkflowTaskMutationSelfTargetError
	if !errors.As(err, &denied) || denied.TaskID != task.Task.Id {
		t.Fatalf("StartWorkflowTask error = %v, want self-target denial for %q", err, task.Task.Id)
	}
}

func TestWorkflowSessionCannotMoveItsOwnTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	started := startWorkflowServiceTask(t, ctx, service, task.Task.Id)
	sessionID := bindWorkflowServiceSessionToTask(
		t,
		service,
		metadataStore,
		binding,
		workflow.TaskID(task.Task.Id),
		started.CurrentNodes[0],
	)

	_, err := service.MoveWorkflowTask(ctx, &taskpb.MoveRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
		TargetNodeId:      "node-target",
	})
	var denied *serverapi.WorkflowTaskMutationSelfTargetError
	if !errors.As(err, &denied) || denied.TaskID != task.Task.Id {
		t.Fatalf("MoveWorkflowTask error = %v, want self-target denial for %q", err, task.Task.Id)
	}
}

func TestWorkflowSessionCannotApproveItsOwnTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	requireWorkflowServiceEdgeApproval(t, ctx, service, workflowID, "done")
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	started := startWorkflowServiceTask(t, ctx, service, task.Task.Id)
	sessionID := bindWorkflowServiceSessionToTask(
		t,
		service,
		metadataStore,
		binding,
		workflow.TaskID(task.Task.Id),
		started.CurrentNodes[0],
	)
	source := workflowServiceCurrentNodeReference(t, workflow.TaskID(task.Task.Id), started.CurrentNodes[0])
	completed, err := workflowfixture.CompleteCurrentNode(t, ctx, metadataStore, service.store, workflowstore.CurrentNodeCompletionRequest{
		Source:       source,
		TransitionID: "done",
	})
	if err != nil || completed.PendingApproval == nil {
		t.Fatalf("CompleteCurrentNode = %+v, %v; want pending Approval", completed, err)
	}

	_, err = service.ApproveWorkflowTask(ctx, &taskpb.ApproveRequest{
		ApprovalId:        completed.PendingApproval.ID.String(),
		InvokingSessionId: proto.String(sessionID.String()),
	})
	var denied *serverapi.WorkflowTaskMutationSelfTargetError
	if !errors.As(err, &denied) || denied.TaskID != task.Task.Id {
		t.Fatalf("ApproveWorkflowTask error = %v, want self-target denial for %q", err, task.Task.Id)
	}
	if _, err := service.store.PendingApproval(ctx, completed.PendingApproval.ID); err != nil {
		t.Fatalf("pending Approval changed after denial: %v", err)
	}
}

func TestWorkflowSessionCannotInterruptOrResumeItsOwnTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	started := startWorkflowServiceTask(t, ctx, service, task.Task.Id)
	sessionID := bindWorkflowServiceSessionToTask(
		t,
		service,
		metadataStore,
		binding,
		workflow.TaskID(task.Task.Id),
		started.CurrentNodes[0],
	)
	execution := newTaskMutationAuthorizationExecutionStub(service)
	service.currentNodeExecution = execution

	_, interruptErr := service.InterruptWorkflowTask(ctx, &taskpb.InterruptRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
	})
	var interruptDenied *serverapi.WorkflowTaskMutationSelfTargetError
	if !errors.As(interruptErr, &interruptDenied) || interruptDenied.TaskID != task.Task.Id {
		t.Fatalf("InterruptWorkflowTask error = %v, want self-target denial for %q", interruptErr, task.Task.Id)
	}

	_, resumeErr := service.ResumeWorkflowTask(ctx, &taskpb.ResumeRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
		SetupOperationId:  worktreecontract.NewSetupOperationID().String(),
	})
	var resumeDenied *serverapi.WorkflowTaskMutationSelfTargetError
	if !errors.As(resumeErr, &resumeDenied) || resumeDenied.TaskID != task.Task.Id {
		t.Fatalf("ResumeWorkflowTask error = %v, want self-target denial for %q", resumeErr, task.Task.Id)
	}
	if len(execution.interrupts) != 0 || len(execution.resumedTaskIDs) != 0 {
		t.Fatalf("denied mutations reached execution: interrupts=%+v resumes=%+v", execution.interrupts, execution.resumedTaskIDs)
	}
}

func TestWorkflowSessionCanStartAnotherTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	ownedTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	started := startWorkflowServiceTask(t, ctx, service, ownedTask.Task.Id)
	sessionID := bindWorkflowServiceSessionToTask(
		t,
		service,
		metadataStore,
		binding,
		workflow.TaskID(ownedTask.Task.Id),
		started.CurrentNodes[0],
	)
	targetTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)

	response, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{
		TaskId:            targetTask.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
		SetupOperationId:  worktreecontract.NewSetupOperationID().String(),
		ExecutionTarget: &taskpb.ExecutionTargetSelection{
			Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE,
		},
	})
	if err != nil {
		t.Fatalf("StartWorkflowTask: %v", err)
	}
	if response.GetApplied() == nil {
		t.Fatalf("StartWorkflowTask response = %+v, want applied", response)
	}
}

func TestWorkflowSessionCanMoveAnotherTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	ownedTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	ownedStarted := startWorkflowServiceTask(t, ctx, service, ownedTask.Task.Id)
	sessionID := bindWorkflowServiceSessionToTask(
		t,
		service,
		metadataStore,
		binding,
		workflow.TaskID(ownedTask.Task.Id),
		ownedStarted.CurrentNodes[0],
	)
	targetTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	definition, err := service.GetWorkflow(ctx, &pb.GetRequest{WorkflowId: workflowID.String()})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	targetNodeID := workflowServiceNodeIDByKind(t, definition.Definition, "agent")

	response, err := service.MoveWorkflowTask(ctx, &taskpb.MoveRequest{
		TaskId:            targetTask.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
		TargetNodeId:      targetNodeID,
		ExecutionTarget: &taskpb.ExecutionTargetSelection{
			Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE,
		},
	})
	if err != nil {
		t.Fatalf("MoveWorkflowTask: %v", err)
	}
	if response.GetApplied() == nil {
		t.Fatalf("MoveWorkflowTask response = %+v, want applied", response)
	}
}

func TestWorkflowSessionCanApproveAnotherTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	requireWorkflowServiceEdgeApproval(t, ctx, service, workflowID, "done")
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	ownedTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	ownedStarted := startWorkflowServiceTask(t, ctx, service, ownedTask.Task.Id)
	sessionID := bindWorkflowServiceSessionToTask(
		t,
		service,
		metadataStore,
		binding,
		workflow.TaskID(ownedTask.Task.Id),
		ownedStarted.CurrentNodes[0],
	)
	targetTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	targetStarted := startWorkflowServiceTask(t, ctx, service, targetTask.Task.Id)
	source := workflowServiceCurrentNodeReference(t, workflow.TaskID(targetTask.Task.Id), targetStarted.CurrentNodes[0])
	completed, err := workflowfixture.CompleteCurrentNode(t, ctx, metadataStore, service.store, workflowstore.CurrentNodeCompletionRequest{
		Source:       source,
		TransitionID: "done",
	})
	if err != nil || completed.PendingApproval == nil {
		t.Fatalf("CompleteCurrentNode = %+v, %v; want pending Approval", completed, err)
	}

	response, err := service.ApproveWorkflowTask(ctx, &taskpb.ApproveRequest{
		ApprovalId:        completed.PendingApproval.ID.String(),
		InvokingSessionId: proto.String(sessionID.String()),
	})
	if err != nil {
		t.Fatalf("ApproveWorkflowTask: %v", err)
	}
	if response.GetApplied() == nil || response.GetApplied().TaskId != targetTask.Task.Id {
		t.Fatalf("ApproveWorkflowTask response = %+v, want target Task applied", response)
	}
}

func TestWorkflowSessionCanInterruptAndResumeAnotherTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	ownedTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	ownedStarted := startWorkflowServiceTask(t, ctx, service, ownedTask.Task.Id)
	sessionID := bindWorkflowServiceSessionToTask(
		t,
		service,
		metadataStore,
		binding,
		workflow.TaskID(ownedTask.Task.Id),
		ownedStarted.CurrentNodes[0],
	)
	targetTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	execution := newTaskMutationAuthorizationExecutionStub(service)
	service.currentNodeExecution = execution

	if _, err := service.InterruptWorkflowTask(ctx, &taskpb.InterruptRequest{
		TaskId:            targetTask.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
	}); err != nil {
		t.Fatalf("InterruptWorkflowTask: %v", err)
	}
	if _, err := service.ResumeWorkflowTask(ctx, &taskpb.ResumeRequest{
		TaskId:            targetTask.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
		SetupOperationId:  worktreecontract.NewSetupOperationID().String(),
		ExecutionTarget:   &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE},
	}); err != nil {
		t.Fatalf("ResumeWorkflowTask: %v", err)
	}
	if len(execution.interrupts) != 1 || execution.interrupts[0].TaskID != workflow.TaskID(targetTask.Task.Id) {
		t.Fatalf("interrupts = %+v, want target Task", execution.interrupts)
	}
	if len(execution.resumedTaskIDs) != 1 || execution.resumedTaskIDs[0] != workflow.TaskID(targetTask.Task.Id) {
		t.Fatalf("resumed Task IDs = %+v, want target Task", execution.resumedTaskIDs)
	}
}

func TestWorkflowTaskMutationRejectsUnknownInvokingSession(t *testing.T) {
	ctx, service, binding, _ := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	unknownSessionID, err := runtimeids.ParseSessionID("unknown-session")
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	execution := newTaskMutationAuthorizationExecutionStub(service)
	service.currentNodeExecution = execution

	if _, err := service.InterruptWorkflowTask(ctx, &taskpb.InterruptRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(unknownSessionID.String()),
	}); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("InterruptWorkflowTask error = %v, want unknown Session failure", err)
	}
	if _, err := service.ResumeWorkflowTask(ctx, &taskpb.ResumeRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(unknownSessionID.String()),
		SetupOperationId:  worktreecontract.NewSetupOperationID().String(),
	}); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("ResumeWorkflowTask error = %v, want unknown Session failure", err)
	}
	if len(execution.interrupts) != 0 || len(execution.resumedTaskIDs) != 0 {
		t.Fatalf("unknown Session mutations reached execution: interrupts=%+v resumes=%+v", execution.interrupts, execution.resumedTaskIDs)
	}

	_, err = service.StartWorkflowTask(ctx, &taskpb.StartRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(unknownSessionID.String()),
		SetupOperationId:  worktreecontract.NewSetupOperationID().String(),
	})
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("StartWorkflowTask error = %v, want unknown Session failure", err)
	}

	response, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{
		TaskId:           task.Task.Id,
		SetupOperationId: worktreecontract.NewSetupOperationID().String(),
		ExecutionTarget: &taskpb.ExecutionTargetSelection{
			Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE,
		},
	})
	if err != nil || response.GetApplied() == nil {
		t.Fatalf("retry after unknown Session = %+v, %v; want unchanged Task to start", response, err)
	}
}

func TestUnboundSessionCanMutateAnyTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	requireWorkflowServiceEdgeApproval(t, ctx, service, workflowID, "done")
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	sessionID := createPersistedWorkflowServiceSession(t, metadataStore, binding)
	execution := newTaskMutationAuthorizationExecutionStub(service)
	service.currentNodeExecution = execution

	if _, err := service.InterruptWorkflowTask(ctx, &taskpb.InterruptRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
	}); err != nil {
		t.Fatalf("InterruptWorkflowTask with unbound Session: %v", err)
	}
	if _, err := service.ResumeWorkflowTask(ctx, &taskpb.ResumeRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
		SetupOperationId:  worktreecontract.NewSetupOperationID().String(),
		ExecutionTarget:   &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE},
	}); err != nil {
		t.Fatalf("ResumeWorkflowTask with unbound Session: %v", err)
	}

	moveTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	definition, err := service.GetWorkflow(ctx, &pb.GetRequest{WorkflowId: workflowID.String()})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	moveTargetNodeID := workflowServiceNodeIDByKind(t, definition.Definition, "agent")
	moveResponse, err := service.MoveWorkflowTask(ctx, &taskpb.MoveRequest{
		TaskId:            moveTask.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
		TargetNodeId:      moveTargetNodeID,
		ExecutionTarget: &taskpb.ExecutionTargetSelection{
			Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE,
		},
	})
	if err != nil || moveResponse.GetApplied() == nil {
		t.Fatalf("MoveWorkflowTask with unbound Session = %+v, %v; want applied", moveResponse, err)
	}

	approvalTask := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	approvalStarted := startWorkflowServiceTask(t, ctx, service, approvalTask.Task.Id)
	approvalSource := workflowServiceCurrentNodeReference(
		t,
		workflow.TaskID(approvalTask.Task.Id),
		approvalStarted.CurrentNodes[0],
	)
	completed, err := workflowfixture.CompleteCurrentNode(t, ctx, metadataStore, service.store, workflowstore.CurrentNodeCompletionRequest{
		Source:       approvalSource,
		TransitionID: "done",
	})
	if err != nil || completed.PendingApproval == nil {
		t.Fatalf("CompleteCurrentNode = %+v, %v; want pending Approval", completed, err)
	}
	approvalResponse, err := service.ApproveWorkflowTask(ctx, &taskpb.ApproveRequest{
		ApprovalId:        completed.PendingApproval.ID.String(),
		InvokingSessionId: proto.String(sessionID.String()),
	})
	if err != nil || approvalResponse.GetApplied() == nil || approvalResponse.GetApplied().TaskId != approvalTask.Task.Id {
		t.Fatalf("ApproveWorkflowTask with unbound Session = %+v, %v; want target Task applied", approvalResponse, err)
	}

	response, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{
		TaskId:            task.Task.Id,
		InvokingSessionId: proto.String(sessionID.String()),
		SetupOperationId:  worktreecontract.NewSetupOperationID().String(),
		ExecutionTarget: &taskpb.ExecutionTargetSelection{
			Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE,
		},
	})
	if err != nil || response.GetApplied() == nil {
		t.Fatalf("StartWorkflowTask = %+v, %v; want unbound Session allowed", response, err)
	}
}

type taskMutationAuthorizationExecutionStub struct {
	currentNodeCompletionExecutionStub
	interrupts     []workflowexecution.InterruptSelector
	resumedTaskIDs []workflow.TaskID
}

func newTaskMutationAuthorizationExecutionStub(service *Service) *taskMutationAuthorizationExecutionStub {
	return &taskMutationAuthorizationExecutionStub{
		currentNodeCompletionExecutionStub: currentNodeCompletionExecutionStub{
			store:                 service.store,
			manualMoveAssignments: workflowServiceManualMoveAssignments(service),
			resumePreflight: workflowexecution.TaskResumePreflight{
				Outcome: workflowexecution.TaskResumePreflightResumable,
				CurrentNodes: []workflow.CurrentNode{{
					Reference: workflow.CurrentNodeReference{
						TaskID: workflow.TaskID("authorization-preflight"),
						NodeID: workflow.NodeID("authorization-preflight"),
					},
				}},
			},
		},
	}
}

func (s *taskMutationAuthorizationExecutionStub) Interrupt(_ context.Context, selector workflowexecution.InterruptSelector) error {
	s.interrupts = append(s.interrupts, selector)
	return nil
}

func (s *taskMutationAuthorizationExecutionStub) ResumeTask(_ context.Context, taskID workflow.TaskID, _ *workflowstore.ExecutionTargetCandidate) (workflowexecution.TaskResumeResult, error) {
	s.resumedTaskIDs = append(s.resumedTaskIDs, taskID)
	return workflowexecution.TaskResumeResult{
		Outcome: workflowexecution.TaskResumeApplied,
		CurrentNodes: []workflow.CurrentNode{{
			Reference: workflow.CurrentNodeReference{
				TaskID: taskID,
				NodeID: workflow.NodeID("authorized-resume"),
			},
		}},
	}, nil
}

func bindWorkflowServiceSessionToTask(
	t *testing.T,
	service *Service,
	metadataStore *metadata.Store,
	binding metadata.Binding,
	taskID workflow.TaskID,
	currentNode *taskpb.AttentionCurrentNode,
) runtimeids.SessionID {
	t.Helper()
	sessionID := createPersistedWorkflowServiceSession(t, metadataStore, binding)
	reference := workflowServiceCurrentNodeReference(t, taskID, currentNode)
	if _, err := service.store.AssociateTaskSession(t.Context(), workflowstore.TaskSessionAssociationRequest{
		SessionID:    sessionID,
		CurrentNode:  reference,
		AssociatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AssociateTaskSession: %v", err)
	}
	return sessionID
}

func workflowServiceCurrentNodeReference(
	t *testing.T,
	taskID workflow.TaskID,
	currentNode *taskpb.AttentionCurrentNode,
) workflow.CurrentNodeReference {
	t.Helper()
	var branchKey *workflow.TransitionBranchKey
	if currentNode.TransitionBranchKey != nil {
		value := workflow.TransitionBranchKey(*currentNode.TransitionBranchKey)
		branchKey = &value
	}
	reference, err := workflow.NewCurrentNodeReference(taskID, workflow.NodeID(currentNode.NodeId), branchKey)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference: %v", err)
	}
	return reference
}

func createPersistedWorkflowServiceSession(
	t *testing.T,
	metadataStore *metadata.Store,
	binding metadata.Binding,
) runtimeids.SessionID {
	t.Helper()
	sessionRoot := filepath.Join(metadataStore.PersistenceRoot(), "projects", binding.ProjectID, "sessions")
	sessionStore, err := session.Create(
		sessionRoot,
		binding.WorkspaceName,
		binding.CanonicalRoot,
		sessioncontract.SessionCategoryMain,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := sessionStore.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	sessionID, err := runtimeids.ParseSessionID(sessionStore.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	return sessionID
}
