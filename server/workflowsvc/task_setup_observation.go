package workflowsvc

import (
	"errors"
	"fmt"
	"strings"

	"core/server/workflow"
	"core/server/worktree"
	"core/shared/protoapi"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"
	"core/shared/worktreecontract"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type taskSetupObservation struct {
	setupOperationID worktreecontract.SetupOperationID
	executionTarget  *worktreepb.SetupExecutionTargetSelection
	publisher        workflowTaskSetupEventPublisher
}

func newTaskSetupObservation(setupOperationID worktreecontract.SetupOperationID, executionTarget workflow.ExecutionTargetSelection, publisher workflowTaskSetupEventPublisher) (*taskSetupObservation, error) {
	if err := setupOperationID.Validate(); err != nil {
		return nil, err
	}
	if publisher == nil {
		return nil, errors.New("Workflow Task setup event publisher is required")
	}
	target, err := worktreeSetupExecutionTarget(executionTarget)
	if err != nil {
		return nil, err
	}
	return &taskSetupObservation{
		setupOperationID: setupOperationID,
		executionTarget:  target,
		publisher:        publisher,
	}, nil
}

func (o *taskSetupObservation) finish(prepared preparedInitiatingActionTarget, preparationErr error) {
	var unavailable *serverapi.WorkflowLockedExecutionTargetError
	if errors.As(preparationErr, &unavailable) {
		return
	}
	event := &worktreepb.SetupEvent{SetupOperationId: o.setupOperationID.String()}
	if preparationErr == nil {
		switch {
		case prepared.setupResult == nil:
			event.Phase = &worktreepb.SetupEvent_NotRequired{NotRequired: &worktreepb.SetupNotRequired{
				Reason:                   worktreepb.SetupNotRequiredReason_WORKTREE_SETUP_NOT_REQUIRED_REASON_NO_TARGET_PREPARATION,
				RetainedPreviousWorktree: prepared.retainedPreviousWorktree,
			}}
		case prepared.setupResult.Completed != nil:
			completed := proto.Clone(prepared.setupResult.Completed).(*worktreepb.SetupCompleted)
			if prepared.retainedPreviousWorktree != nil {
				completed.RetainedPreviousWorktree = prepared.retainedPreviousWorktree
			}
			event.Phase = &worktreepb.SetupEvent_Completed{Completed: completed}
		case prepared.setupResult.NotRequired != nil:
			notRequired := proto.Clone(prepared.setupResult.NotRequired).(*worktreepb.SetupNotRequired)
			if prepared.retainedPreviousWorktree != nil {
				notRequired.RetainedPreviousWorktree = prepared.retainedPreviousWorktree
			}
			event.Phase = &worktreepb.SetupEvent_NotRequired{NotRequired: notRequired}
		default:
			panic("successful Task preparation has a failed or invalid setup result")
		}
	} else {
		failed := preparationFailurePayload(prepared.setupResult, prepared.retainedWorktree, prepared.retainedPreviousWorktree, preparationErr)
		failed.ExecutionTarget = proto.Clone(o.executionTarget).(*worktreepb.SetupExecutionTargetSelection)
		event.Phase = &worktreepb.SetupEvent_Failed{Failed: failed}
	}
	o.publisher.PublishWorkflowTaskSetupEvent(event)
}

func preparationFailurePayload(result *worktree.WorktreeSetupResult, retainedWorktree *worktreepb.RegisteredFacts, retainedPreviousWorktree *worktreepb.RetainedPreviousWorktree, cause error) *worktreepb.SetupFailed {
	if result != nil && result.Failed != nil {
		failed := proto.Clone(result.Failed).(*worktreepb.SetupFailed)
		if failed.RetainedWorktree == nil && retainedWorktree != nil {
			failed.RetainedWorktree = retainedWorktree
		}
		if retainedPreviousWorktree != nil {
			failed.RetainedPreviousWorktree = retainedPreviousWorktree
		}
		return failed
	}
	return &worktreepb.SetupFailed{
		RecoveryDisposition: worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_RETRY_EXISTING,
		RetryReadiness:      worktreepb.SetupRetryReadiness_WORKTREE_SETUP_RETRY_READY,
		Cause: &worktreepb.SetupFailureCause{
			Cause: &worktreepb.SetupFailureCause_TargetPreparation{TargetPreparation: &emptypb.Empty{}},
		},
		Diagnostic:               preparationDiagnostic(cause),
		RetainedWorktree:         retainedWorktree,
		RetainedPreviousWorktree: retainedPreviousWorktree,
	}
}

func preparationDiagnostic(err error) string {
	if err == nil || strings.TrimSpace(err.Error()) == "" {
		return "Workflow Task preparation failed"
	}
	return err.Error()
}

func worktreeSetupExecutionTarget(selection workflow.ExecutionTargetSelection) (*worktreepb.SetupExecutionTargetSelection, error) {
	target := &worktreepb.SetupExecutionTargetSelection{CustomRef: selection.CustomRef}
	switch selection.Mode {
	case workflow.ExecutionTargetModeNone:
		target.Mode = worktreepb.SetupExecutionTargetMode_WORKTREE_SETUP_EXECUTION_TARGET_MODE_NONE
	case workflow.ExecutionTargetModeHead:
		target.Mode = worktreepb.SetupExecutionTargetMode_WORKTREE_SETUP_EXECUTION_TARGET_MODE_HEAD
	case workflow.ExecutionTargetModeDefaultBranch:
		target.Mode = worktreepb.SetupExecutionTargetMode_WORKTREE_SETUP_EXECUTION_TARGET_MODE_DEFAULT_BRANCH
	case workflow.ExecutionTargetModeCustomRef:
		target.Mode = worktreepb.SetupExecutionTargetMode_WORKTREE_SETUP_EXECUTION_TARGET_MODE_CUSTOM_REF
	case workflow.ExecutionTargetModeAskOnFirstExecution:
		target.Mode = worktreepb.SetupExecutionTargetMode_WORKTREE_SETUP_EXECUTION_TARGET_MODE_ASK_ON_FIRST_EXECUTION
	default:
		return nil, fmt.Errorf("worktree setup execution target mode %q is invalid", selection.Mode)
	}
	if err := protoapi.Validate(target); err != nil {
		return nil, err
	}
	return target, nil
}
