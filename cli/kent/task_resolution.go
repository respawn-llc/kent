package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"core/shared/apicontract"
	"core/shared/config"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type workflowTaskNotFoundError struct{ error }

func (e workflowTaskNotFoundError) Unwrap() error { return serverapi.ErrWorkflowTaskNotFound }

type workflowTaskSelectorKind uint8

const (
	workflowTaskSelectorTaskID workflowTaskSelectorKind = iota
	workflowTaskSelectorShortID
)

type workflowTaskSelector struct {
	kind  workflowTaskSelectorKind
	value string
}

func classifyWorkflowTaskSelector(ref string) (workflowTaskSelector, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return workflowTaskSelector{}, errors.New("task id is required")
	}
	if taskID, err := runtimeids.ParseTaskID(trimmed); err == nil {
		return workflowTaskSelector{kind: workflowTaskSelectorTaskID, value: taskID}, nil
	}
	return workflowTaskSelector{kind: workflowTaskSelectorShortID, value: trimmed}, nil
}

func workflowTaskList(ctx context.Context, remote apicontract.WorkflowService, req *taskpb.ListRequest) (*taskpb.ListSuccess, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	return remote.ListWorkflowTasks(rpcCtx, req)
}

func resolveWorkflowTaskID(
	ctx context.Context,
	cfg config.App,
	projects apicontract.ProjectViewService,
	workflows apicontract.WorkflowService,
	projectRef string,
	ref string,
) (string, error) {
	task, err := resolveWorkflowTask(ctx, cfg, projects, workflows, projectRef, ref)
	if err != nil {
		return "", err
	}
	return task.Summary.Id, nil
}

func resolveWorkflowTask(
	ctx context.Context,
	cfg config.App,
	projects apicontract.ProjectViewService,
	workflows apicontract.WorkflowService,
	projectRef string,
	ref string,
) (*taskpb.TaskDetail, error) {
	selector, err := classifyWorkflowTaskSelector(ref)
	if err != nil {
		return nil, err
	}
	if selector.kind == workflowTaskSelectorTaskID {
		detail, err := getWorkflowTaskByID(ctx, workflows, selector.value)
		if err != nil && isWorkflowTaskNotFound(err) {
			return nil, workflowTaskNotFoundError{err}
		}
		return detail, err
	}
	projectID, err := resolveWorkflowProjectID(ctx, cfg, projects, projectRef)
	if err != nil {
		return nil, err
	}
	detail, err := getWorkflowTaskByProjectShortID(ctx, workflows, projectID, selector.value)
	if err != nil {
		if errors.Is(err, serverapi.ErrWorkflowTaskNotFound) || errors.Is(err, sql.ErrNoRows) {
			return nil, workflowTaskNotFoundError{fmt.Errorf("task %q not found in project %s", selector.value, projectID)}
		}
		return nil, err
	}
	return detail, nil
}

func isWorkflowTaskNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, serverapi.ErrWorkflowTaskNotFound)
}

func getWorkflowTaskByID(ctx context.Context, remote apicontract.WorkflowService, taskID string) (*taskpb.TaskDetail, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowTask(rpcCtx, &taskpb.GetRequest{TaskId: &taskID})
	if err != nil {
		return nil, err
	}
	return resp.Task, nil
}

func getWorkflowTaskByProjectShortID(ctx context.Context, remote apicontract.WorkflowService, projectID string, shortID string) (*taskpb.TaskDetail, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowTask(rpcCtx, &taskpb.GetRequest{ProjectId: &projectID, ShortId: &shortID})
	if err != nil {
		return nil, err
	}
	return resp.Task, nil
}

func getWorkflowTaskByShortID(ctx context.Context, remote apicontract.WorkflowService, shortID string) (*taskpb.TaskDetail, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowTask(rpcCtx, &taskpb.GetRequest{ShortId: &shortID})
	if err != nil {
		return nil, err
	}
	return resp.Task, nil
}
