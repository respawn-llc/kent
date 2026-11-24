package workflowsvc

import (
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestCreatedMarkdownPreservesIndentation(t *testing.T) {
	ctx, service, binding := newWorkflowServiceTestContext(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	body := "    code\n\n![attachment](https://example.com/image.png)\n"
	created, err := service.CreateWorkflowTask(ctx, &taskpb.CreateRequest{ProjectId: binding.ProjectID, Title: "Markdown", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.GetWorkflowTask(ctx, &taskpb.GetRequest{TaskId: proto.String(created.Task.Id)})
	if err != nil {
		t.Fatal(err)
	}
	if task.Task.Body != body {
		t.Fatalf("body changed: %q", task.Task.Body)
	}
	_, err = service.AddWorkflowTaskComment(ctx, &taskpb.CommentAddRequest{TaskId: created.Task.Id, Body: body, Author: taskpb.CommentAuthorKind_COMMENT_AUTHOR_KIND_USER, AuthorId: proto.String("github-author")})
	if err != nil {
		t.Fatal(err)
	}
	comments, err := service.ListWorkflowTaskComments(ctx, &taskpb.TaskOffsetPageRequest{TaskId: created.Task.Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments.Items) != 1 || comments.Items[0].Body != body || comments.Items[0].GetAuthorId() != "github-author" {
		t.Fatalf("%+v", comments.Items)
	}
}
