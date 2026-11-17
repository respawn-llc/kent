package workflowview

import (
	"testing"

	"core/server/workflowstore"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
)

func TestTaskPRURLDetailAndBoard(t *testing.T) {
	f := newCurrentNodeViewFixture(t, false)
	task, err := f.store.CreateTask(f.ctx, workflowstore.CreateTaskRequest{ProjectID: f.binding.ProjectID, WorkflowID: &f.workflowID, Title: "Review", SourceURL: "https://git.example/issues/1"})
	if err != nil {
		t.Fatal(err)
	}
	f.startExistingTask(t, task)
	for _, value := range []*string{proto.String("https://git.example/pull/1"), proto.String("https://self.example/merge_requests/2"), nil} {
		if _, err := f.store.UpdateTask(f.ctx, workflowstore.UpdateTaskRequest{TaskID: task.ID, PRURLChange: &workflowstore.TaskPRURLChange{URL: value}}); err != nil {
			t.Fatal(err)
		}
		detail, err := f.detail.GetTask(f.ctx, string(task.ID))
		if err != nil {
			t.Fatal(err)
		}
		page, err := f.board.ListNodeCards(f.ctx, &taskpb.BoardNodeCardsListRequest{
			ProjectId: f.binding.ProjectID, WorkflowId: f.workflowID.String(), NodeId: string(f.agentNodeID),
			LabelFilter: noLabelFilter(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Cards) != 1 {
			t.Fatalf("cards = %d", len(page.Cards))
		}
		for _, actual := range []*string{detail.PrUrl, page.Cards[0].PrUrl} {
			if (actual == nil) != (value == nil) || actual != nil && *actual != *value {
				t.Fatalf("PR URL = %v, want %v", actual, value)
			}
		}
		if detail.GetSourceUrl() != "https://git.example/issues/1" {
			t.Fatal("source URL changed")
		}
	}
}
