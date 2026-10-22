package main

import (
	"errors"
	"strings"

	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

const executionTargetSelectorHelp = "none, head, default-branch, or ref:<revision>"

func parseTaskExecutionTargetSelector(raw string) (*taskpb.ExecutionTargetSelection, error) {
	trimmed := strings.TrimSpace(raw)
	switch trimmed {
	case "none":
		return &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE}, nil
	case "head":
		return &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD}, nil
	case "default-branch":
		return &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_DEFAULT_BRANCH}, nil
	}
	if revision, ok := strings.CutPrefix(trimmed, "ref:"); ok {
		revision = strings.TrimSpace(revision)
		if revision == "" {
			return nil, errors.New("execution target ref:<revision> requires a non-blank revision")
		}
		return &taskpb.ExecutionTargetSelection{
			Mode:      pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF,
			CustomRef: &revision,
		}, nil
	}
	return nil, errors.New("execution target must be " + executionTargetSelectorHelp)
}

func parseOptionalTaskExecutionTarget(raw string, provided bool) (*taskpb.ExecutionTargetSelection, error) {
	if !provided {
		return nil, nil
	}
	selection, err := parseTaskExecutionTargetSelector(raw)
	if err != nil {
		return nil, err
	}
	return selection, nil
}

func parseWorkflowExecutionTargetPolicySelector(raw string) (*pb.ExecutionTargetConfiguration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "ask-on-first-execution" {
		return &pb.ExecutionTargetConfiguration{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_ASK_ON_FIRST_EXECUTION}, nil
	}
	selection, err := parseTaskExecutionTargetSelector(trimmed)
	if err != nil {
		return nil, errors.New("workflow execution target must be ask-on-first-execution, " + executionTargetSelectorHelp)
	}
	return &pb.ExecutionTargetConfiguration{
		Mode:      selection.Mode,
		CustomRef: selection.CustomRef,
	}, nil
}

func workflowExecutionTargetPolicySelector(policy workflowExecutionTargetPolicyJSON) string {
	switch policy.Mode {
	case serverapi.WorkflowExecutionTargetModeAskOnFirstExecution:
		return "ask-on-first-execution"
	case serverapi.WorkflowExecutionTargetModeNone:
		return "none"
	case serverapi.WorkflowExecutionTargetModeHead:
		return "head"
	case serverapi.WorkflowExecutionTargetModeDefaultBranch:
		return "default-branch"
	case serverapi.WorkflowExecutionTargetModeCustomRef:
		if policy.CustomRef != nil {
			return "ref:" + *policy.CustomRef
		}
		return "ref:"
	default:
		return string(policy.Mode)
	}
}
