package main

import (
	"bytes"
	"testing"

	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/sessionenv"
	"google.golang.org/protobuf/proto"
)

func TestAgentCommentUsesAuthorSessionAttachmentAcrossProjects(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	t.Setenv(sessionenv.SessionIDEnv, fixture.sessionID)
	service := fixture.core.WorkflowClient()
	workflow, err := service.CreateAndLinkWorkflowToProject(t.Context(), &workflowpb.CreateAndLinkProjectRequest{
		ProjectId: fixture.b.ProjectID, Name: "Comments",
		DefaultPolicy: workflowpb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_ALWAYS.Enum(),
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.CreateWorkflowTask(t.Context(), &taskpb.CreateRequest{
		ProjectId: fixture.b.ProjectID, WorkflowId: proto.String(workflow.Workflow.Id), Title: "Comment target",
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := taskCommentAddSubcommand([]string{
		task.Task.Id, "--project", fixture.b.ProjectID, "--body", "Blocked on setup", "--author", "user",
	}, &stdout, &stderr); code != 0 {
		t.Fatalf("agent comment failed (%d): %s", code, &stderr)
	}
	comments, err := service.ListWorkflowTaskComments(t.Context(), &taskpb.TaskOffsetPageRequest{
		TaskId: task.Task.Id, Limit: proto.Int32(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments.Items) != 1 || comments.Items[0].Author != taskpb.CommentAuthorKind_COMMENT_AUTHOR_KIND_AGENT {
		t.Fatalf("comment attribution = %+v", comments.Items)
	}
}
