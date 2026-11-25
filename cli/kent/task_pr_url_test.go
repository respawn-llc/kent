package main

import (
	"bytes"
	"testing"

	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
)

func TestTaskEditPRURL(t *testing.T) {
	f := newWorktreeCommandFixture(t)
	service := f.core.WorkflowClient()
	created, err := service.CreateAndLinkWorkflowToProject(t.Context(), &workflowpb.CreateAndLinkProjectRequest{
		ProjectId: f.a.ProjectID, Name: "Review", DefaultPolicy: workflowpb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_ALWAYS.Enum(),
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.CreateWorkflowTask(t.Context(), &taskpb.CreateRequest{
		ProjectId: f.a.ProjectID, WorkflowId: proto.String(created.Workflow.Id), Title: "Review", SourceUrl: proto.String("https://git.example/issues/1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	url := "https://git.example/pull/1"
	var stdout, stderr bytes.Buffer
	if code := taskEditSubcommand([]string{task.Task.Id, "--pr-url", url}, &stdout, &stderr); code != 0 {
		t.Fatalf("edit exit=%d: %s", code, &stderr)
	}
	detail, err := service.GetWorkflowTask(t.Context(), &taskpb.GetRequest{TaskId: proto.String(task.Task.Id)})
	if err != nil || detail.Task.GetPrUrl() != url {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	for _, tc := range []struct {
		args []string
		want *string
		code int
	}{
		{[]string{"--pr-url", "https://self.example/merge_requests/2"}, proto.String("https://self.example/merge_requests/2"), 0},
		{[]string{"--title", "Renamed"}, proto.String("https://self.example/merge_requests/2"), 0},
		{[]string{"--pr-url", "file:///tmp/pr"}, proto.String("https://self.example/merge_requests/2"), 1},
		{[]string{"--pr-url", "ftp://example.com/merge-requests/1"}, proto.String("https://self.example/merge_requests/2"), 1},
		{[]string{"--pr-url", "https://bad host/pull/1"}, proto.String("https://self.example/merge_requests/2"), 1},
		{[]string{"--pr-url", "/repo/pull/1"}, proto.String("https://self.example/merge_requests/2"), 1},
		{[]string{"--pr-url", ""}, nil, 0},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := taskEditSubcommand(append([]string{task.Task.Id}, tc.args...), &stdout, &stderr); code != tc.code {
			t.Fatalf("%v exit=%d: %s", tc.args, code, &stderr)
		}
		detail, err := service.GetWorkflowTask(t.Context(), &taskpb.GetRequest{TaskId: proto.String(task.Task.Id)})
		if err != nil {
			t.Fatal(err)
		}
		actual := detail.Task.PrUrl
		if (actual == nil) != (tc.want == nil) || actual != nil && *actual != *tc.want {
			t.Fatalf("%v saved = %v, want %v", tc.args, actual, tc.want)
		}
		if detail.Task.GetSourceUrl() != "https://git.example/issues/1" {
			t.Fatal("source changed")
		}
	}
}
