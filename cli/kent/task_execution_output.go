package main

import (
	"errors"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

type taskCurrentNodesJSON struct {
	CurrentNodes []*taskpb.AttentionCurrentNode `json:"current_nodes"`
}

type taskExecutionActionJSON struct {
	Outcome                    string                `json:"outcome"`
	Applied                    *taskCurrentNodesJSON `json:"applied,omitempty"`
	SelectionRequired          *taskSelectionJSON    `json:"selection_required,omitempty"`
	UnsatisfiedDependencyCount *int32                `json:"unsatisfied_dependency_count,omitempty"`
}

type taskSelectionJSON struct {
	Reason              string                    `json:"reason"`
	ConfiguredTarget    *taskConfiguredTargetJSON `json:"configured_target,omitempty"`
	UnavailableCause    *string                   `json:"unavailable_cause,omitempty"`
	OriginalTargetCause *string                   `json:"original_target_cause,omitempty"`
}

type taskConfiguredTargetJSON struct {
	Mode         string  `json:"mode"`
	RequestedRef *string `json:"requested_ref,omitempty"`
}

func taskSelectionOutput(selection *taskpb.SelectionRequired) (*taskSelectionJSON, error) {
	if selection == nil {
		return nil, errors.New("execution target selection is required")
	}
	switch reason := selection.Reason.(type) {
	case *taskpb.SelectionRequired_PolicyRequiresSelection:
		return &taskSelectionJSON{Reason: "policy_requires_selection"}, nil
	case *taskpb.SelectionRequired_OriginalTargetUnavailable:
		cause, err := serverapi.WorkflowLockedTargetCause(reason.OriginalTargetUnavailable.Cause)
		if err != nil {
			return nil, err
		}
		name := string(cause)
		return &taskSelectionJSON{Reason: "original_target_unavailable", OriginalTargetCause: &name}, nil
	case *taskpb.SelectionRequired_ConfiguredTargetUnavailable:
		detail := reason.ConfiguredTargetUnavailable
		mode, err := protoapi.WorkflowExecutionTargetMode.Decode(detail.Mode)
		if err != nil {
			return nil, err
		}
		cause, err := serverapi.WorkflowUnavailableTargetCause(detail.Cause)
		if err != nil {
			return nil, err
		}
		name := string(cause)
		return &taskSelectionJSON{
			Reason: "configured_target_unavailable", UnavailableCause: &name,
			ConfiguredTarget: &taskConfiguredTargetJSON{Mode: mode, RequestedRef: detail.RequestedRef},
		}, nil
	default:
		return nil, errors.New("invalid execution target selection")
	}
}

func taskStartOutput(response *taskpb.StartSuccess) (taskExecutionActionJSON, error) {
	switch outcome := response.GetOutcome().(type) {
	case *taskpb.StartSuccess_Applied:
		return taskExecutionActionJSON{
			Outcome: "applied", Applied: &taskCurrentNodesJSON{CurrentNodes: outcome.Applied.CurrentNodes},
		}, nil
	case *taskpb.StartSuccess_SelectionRequired:
		selection, err := taskSelectionOutput(outcome.SelectionRequired)
		return taskExecutionActionJSON{Outcome: "selection_required", SelectionRequired: selection}, err
	case *taskpb.StartSuccess_DependencyConfirmationRequired:
		return taskExecutionActionJSON{
			Outcome:                    "dependency_confirmation_required",
			UnsatisfiedDependencyCount: &outcome.DependencyConfirmationRequired.UnsatisfiedDependencyCount,
		}, nil
	default:
		return taskExecutionActionJSON{}, errors.New("invalid Task Start outcome")
	}
}
