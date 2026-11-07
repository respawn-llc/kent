package workflowview

import (
	"strings"
	"testing"
	"unicode/utf8"

	"core/server/workflowstore"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

func TestTaskDetailProjectsFullUnicodeTaskBodyPreview(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	body := "a" + strings.Repeat("я", 60)
	detail := projectTaskBodyPreview(t, fixture, body)
	if got := detail.Summary.GetBodyPreview(); got != body {
		t.Fatalf("Task body preview = %q, want the full valid UTF-8 description %q", got, body)
	}
}

func TestTaskDetailProjectsTaskBodyPreviewBoundaries(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "ASCII below limit",
			body: strings.Repeat("a", 95),
			want: strings.Repeat("a", 95),
		},
		{
			name: "ASCII at limit",
			body: strings.Repeat("a", 96),
			want: strings.Repeat("a", 96),
		},
		{
			name: "ASCII above limit",
			body: strings.Repeat("a", 97),
			want: strings.Repeat("a", 96),
		},
		{
			name: "Cyrillic below limit",
			body: strings.Repeat("я", 95),
			want: strings.Repeat("я", 95),
		},
		{
			name: "Cyrillic at limit",
			body: strings.Repeat("я", 96),
			want: strings.Repeat("я", 96),
		},
		{
			name: "Cyrillic above limit",
			body: strings.Repeat("я", 97),
			want: strings.Repeat("я", 96),
		},
		{
			name: "emoji below limit",
			body: strings.Repeat("😀", 95),
			want: strings.Repeat("😀", 95),
		},
		{
			name: "emoji at limit",
			body: strings.Repeat("😀", 96),
			want: strings.Repeat("😀", 96),
		},
		{
			name: "emoji above limit",
			body: strings.Repeat("😀", 97),
			want: strings.Repeat("😀", 96),
		},
		{
			name: "mixed Unicode at truncation boundary",
			body: "a" + strings.Repeat("я", 95) + "😀",
			want: "a" + strings.Repeat("я", 95),
		},
		{
			name: "surrounding whitespace is trimmed",
			body: " \t" + strings.Repeat("я", 96) + "\n ",
			want: strings.Repeat("я", 96),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail := projectTaskBodyPreview(t, fixture, test.body)
			if got := detail.Summary.GetBodyPreview(); got != test.want {
				t.Fatalf("Task body preview = %q, want %q", got, test.want)
			}
		})
	}
}

func projectTaskBodyPreview(t *testing.T, fixture currentNodeViewFixture, body string) *taskpb.TaskDetail {
	t.Helper()
	task, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  fixture.binding.ProjectID,
		WorkflowID: &fixture.workflowID,
		Title:      "Unicode preview",
		Body:       body,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	detail, err := fixture.detail.GetTask(fixture.ctx, string(task.ID))
	if err != nil {
		t.Fatalf("TaskDetail.GetTask: %v", err)
	}
	if detail.Body != task.Body {
		t.Fatalf("Task body changed during projection: %q", detail.Body)
	}
	if preview := detail.Summary.GetBodyPreview(); !utf8.ValidString(preview) {
		t.Fatalf("Task body preview is invalid UTF-8: %q", preview)
	}
	return detail
}
