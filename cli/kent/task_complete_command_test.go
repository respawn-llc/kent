package main

import (
	"context"
	"strings"
	"testing"

	"core/shared/client"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/sessionenv"
)

func TestTaskCompleteSelectorForms(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantRequest serverapi.WorkflowTaskCompleteRequest
	}{
		{
			name: "run",
			args: []string{"task", "complete", "--force", "--run", "run-1"},
			wantRequest: serverapi.WorkflowTaskCompleteRequest{
				ActorKind: serverapi.WorkflowTaskCompleteActorUser,
				Force:     true,
				RunID:     "run-1",
			},
		},
		{
			name: "session",
			args: []string{"task", "complete", "--force", "--session", "session-1"},
			wantRequest: serverapi.WorkflowTaskCompleteRequest{
				ActorKind: serverapi.WorkflowTaskCompleteActorUser,
				Force:     true,
				SessionID: "session-1",
			},
		},
		{
			name: "durable task id",
			args: []string{"task", "complete", "--force", "--task", "task-1"},
			wantRequest: serverapi.WorkflowTaskCompleteRequest{
				ActorKind: serverapi.WorkflowTaskCompleteActorUser,
				Force:     true,
				TaskID:    "task-1",
			},
		},
		{
			name: "task short id",
			args: []string{"task", "complete", "--force", "--project", ".", "--task", "BLD-1"},
			wantRequest: serverapi.WorkflowTaskCompleteRequest{
				ActorKind: serverapi.WorkflowTaskCompleteActorUser,
				Force:     true,
				ProjectID: "project-1",
				ShortID:   "BLD-1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(sessionenv.SessionIDEnv, "")
			cfg := config.App{WorkspaceRoot: "/workspace"}
			remote := &taskCompleteCaptureRemote{
				projectID: "project-1",
				response: serverapi.WorkflowTaskCompleteResponse{
					TaskID:       "task-1",
					RunID:        "run-1",
					TransitionID: "transition-1",
					State:        "applied",
				},
			}
			restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
			defer restore()

			if _, stderr, code := runWorkflowRootCommand(tt.args...); code != 0 {
				t.Fatalf("%v exit=%d stderr=%q", tt.args, code, stderr)
			}
			req := remote.requireSingleRequest(t)
			if req.ActorKind != tt.wantRequest.ActorKind ||
				req.Force != tt.wantRequest.Force ||
				req.RunID != tt.wantRequest.RunID ||
				req.SessionID != tt.wantRequest.SessionID ||
				req.TaskID != tt.wantRequest.TaskID ||
				req.ProjectID != tt.wantRequest.ProjectID ||
				req.ShortID != tt.wantRequest.ShortID {
				t.Fatalf("request = %+v, want selector fields %+v", req, tt.wantRequest)
			}
		})
	}
}

type taskCompleteCaptureRemote struct {
	client.WorkflowClient
	projectID string
	response  serverapi.WorkflowTaskCompleteResponse
	err       error
	requests  []serverapi.WorkflowTaskCompleteRequest
}

func (r *taskCompleteCaptureRemote) Close() error { return nil }

func (r *taskCompleteCaptureRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	if strings.TrimSpace(r.projectID) == "" {
		return serverapi.ProjectResolvePathResponse{}, nil
	}
	return serverapi.ProjectResolvePathResponse{Binding: &serverapi.ProjectBinding{ProjectID: r.projectID}}, nil
}

func (r *taskCompleteCaptureRemote) CompleteWorkflowTask(_ context.Context, req serverapi.WorkflowTaskCompleteRequest) (serverapi.WorkflowTaskCompleteResponse, error) {
	r.requests = append(r.requests, req)
	if r.err != nil {
		return serverapi.WorkflowTaskCompleteResponse{}, r.err
	}
	return r.response, nil
}

func (r *taskCompleteCaptureRemote) requireSingleRequest(t *testing.T) serverapi.WorkflowTaskCompleteRequest {
	t.Helper()
	if len(r.requests) != 1 {
		t.Fatalf("completion requests = %+v, want exactly one", r.requests)
	}
	return r.requests[0]
}

var _ workflowCommandRemote = (*taskCompleteCaptureRemote)(nil)
