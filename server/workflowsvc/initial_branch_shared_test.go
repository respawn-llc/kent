package workflowsvc

import (
	"context"
	"errors"

	"core/server/session"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowruntime"
	"core/server/workflowstore"
)

type initialBranchControllerRunner struct{}

type pendingAgentControllerRunner struct {
	prepare func(context.Context, []workflowstore.CurrentNodeStartContext) ([]workflowstore.PlannedCurrentNodeSession, error)
}

func (r pendingAgentControllerRunner) PrepareCurrentNode(ctx context.Context, input workflowstore.CurrentNodeStartContext, _ workflowruntime.TaskPromptDelivery) (workflowexecution.CurrentNodePreparation, error) {
	if r.prepare != nil {
		sessions, err := r.prepare(ctx, []workflowstore.CurrentNodeStartContext{input})
		if err != nil {
			return workflowexecution.CurrentNodePreparation{}, err
		}
		return workflowexecution.CurrentNodePreparation{Session: &sessions[0], Assignment: initialBranchControllerSteer{}}, nil
	}
	if input.CurrentNode.SessionID == nil {
		return workflowexecution.CurrentNodePreparation{}, errors.New("pending runner requires an existing Session")
	}
	return workflowexecution.CurrentNodePreparation{
		Session:    &workflowstore.PlannedCurrentNodeSession{CurrentNode: input.CurrentNode.Reference, SessionID: *input.CurrentNode.SessionID},
		Assignment: initialBranchControllerSteer{},
	}, nil
}

func (initialBranchControllerRunner) PrepareCurrentNode(context.Context, workflowstore.CurrentNodeStartContext, workflowruntime.TaskPromptDelivery) (workflowexecution.CurrentNodePreparation, error) {
	return workflowexecution.CurrentNodePreparation{}, errors.New("runner must not prepare after branch preparation failure")
}

func (pendingAgentControllerRunner) PrepareScriptPublication(
	context.Context,
	workflow.CurrentNodeReference,
	workflowruntime.Controller,
) (workflowexecution.CurrentNodeScriptPublication, error) {
	return nil, nil
}

func (pendingAgentControllerRunner) StartAgentCurrentNode(
	ctx context.Context,
	_ workflow.CurrentNodeReference,
	_ workflowruntime.TaskPromptDelivery,
	_ workflowexecution.CurrentNodeAssignmentSteer,
	_ func(),
	_ workflowruntime.Controller,
) (sessionruntime.ExecutionHandle, error) {
	// Keep the admitted start pending until controller cleanup. A failed start
	// would re-interrupt the node and make a later Resume legitimately apply.
	<-ctx.Done()
	return nil, ctx.Err()
}

func (initialBranchControllerRunner) StartAgentCurrentNode(
	context.Context,
	workflow.CurrentNodeReference,
	workflowruntime.TaskPromptDelivery,
	workflowexecution.CurrentNodeAssignmentSteer,
	func(),
	workflowruntime.Controller,
) (sessionruntime.ExecutionHandle, error) {
	return nil, errors.New("runner must not start after branch preparation failure")
}

func (initialBranchControllerRunner) PrepareScriptPublication(
	context.Context,
	workflow.CurrentNodeReference,
	workflowruntime.Controller,
) (workflowexecution.CurrentNodeScriptPublication, error) {
	return nil, errors.New("runner must not prepare publication after branch preparation failure")
}

type initialBranchControllerSteerer struct{}

func (initialBranchControllerSteerer) SteerCurrentNodeAssignment(
	context.Context,
	workflow.CurrentNodeReference,
) (workflowexecution.CurrentNodeAssignmentSteer, error) {
	return initialBranchControllerSteer{}, nil
}

type initialBranchControllerSteer struct{}

func (initialBranchControllerSteer) Wait(context.Context) (session.CommitReceipt, error) {
	return session.CommitReceipt{Committed: true}, nil
}
