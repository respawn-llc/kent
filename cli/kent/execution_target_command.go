package main

import (
	"errors"
	"strings"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/serverapi"
)

const executionTargetSelectorHelp = "none, head, default-branch, or ref:<revision>"

func parseTaskExecutionTargetSelector(raw string) (serverapi.WorkflowExecutionTargetSelection, error) {
	trimmed := strings.TrimSpace(raw)
	switch trimmed {
	case "none":
		return serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeNone}, nil
	case "head":
		return serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeHead}, nil
	case "default-branch":
		return serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeDefaultBranch}, nil
	}
	if revision, ok := strings.CutPrefix(trimmed, "ref:"); ok {
		revision = strings.TrimSpace(revision)
		if revision == "" {
			return serverapi.WorkflowExecutionTargetSelection{}, errors.New("execution target ref:<revision> requires a non-blank revision")
		}
		return serverapi.WorkflowExecutionTargetSelection{
			Mode:      serverapi.WorkflowExecutionTargetModeCustomRef,
			CustomRef: &revision,
		}, nil
	}
	return serverapi.WorkflowExecutionTargetSelection{}, errors.New("execution target must be " + executionTargetSelectorHelp)
}

func parseOptionalTaskExecutionTarget(raw string, provided bool) (*serverapi.WorkflowExecutionTargetSelection, error) {
	if !provided {
		return nil, nil
	}
	selection, err := parseTaskExecutionTargetSelector(raw)
	if err != nil {
		return nil, err
	}
	return &selection, nil
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
	mode, err := protoapi.WorkflowExecutionTargetMode.Encode(string(selection.Mode))
	if err != nil {
		return nil, err
	}
	return &pb.ExecutionTargetConfiguration{
		Mode:      mode,
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
