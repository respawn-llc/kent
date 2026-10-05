package client

import (
	"context"
	"errors"
	"testing"
	"time"

	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
	"core/shared/worktreecontract"
	"golang.org/x/net/websocket"
)

func TestRemoteWorkflowContextSelectionRestriction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		call := receiveRemoteGeneratedCall(t, ws, "TaskLifecycleService", "Resume", &taskpb.ResumeRequest{})
		sendRemoteGeneratedResult(t, ws, call, &taskpb.ResumeResult{Outcome: &taskpb.ResumeResult_Error{Error: &taskpb.ResumeError{
			Code:   "context_selection_required",
			Detail: &taskpb.ResumeError_ContextSelectionRequired{ContextSelectionRequired: &taskpb.ContextSelectionRequiredDetails{TaskId: "task-restricted"}},
		}}})
	})
	remote, err := DialRemoteURL(ctx, "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remote.Close() }()
	_, err = remote.ResumeWorkflowTask(ctx, &taskpb.ResumeRequest{
		TaskId: "task-restricted", SetupOperationId: worktreecontract.NewSetupOperationID().String(),
	})
	var restriction *serverapi.WorkflowTaskContextSelectionRequiredError
	if !errors.As(err, &restriction) || restriction.TaskID != "task-restricted" {
		t.Fatalf("restriction lost across transport: %v", err)
	}
}
