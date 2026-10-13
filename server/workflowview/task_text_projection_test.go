package workflowview

import (
	"strings"
	"testing"
	"unicode/utf8"

	"core/server/metadata/sqlitegen"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"

	"github.com/google/uuid"
)

func TestTaskProjectorProjectsFullUnicodeTaskBodyPreview(t *testing.T) {
	body := "a" + strings.Repeat("я", 60)
	summary := projectTaskBodyPreview(t, body)
	if got := summary.GetBodyPreview(); got != body {
		t.Fatalf("Task body preview = %q, want the full valid UTF-8 description %q", got, body)
	}
}

func TestTaskProjectorProjectsTaskBodyPreviewBoundaries(t *testing.T) {
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
			summary := projectTaskBodyPreview(t, test.body)
			if got := summary.GetBodyPreview(); got != test.want {
				t.Fatalf("Task body preview = %q, want %q", got, test.want)
			}
		})
	}
}

func projectTaskBodyPreview(t *testing.T, body string) *taskpb.TaskSummary {
	t.Helper()
	task := sqlitegen.TaskRecord{
		ID:              uuid.NewString(),
		ProjectID:       uuid.NewString(),
		WorkflowID:      runtimeids.NewWorkflowID(),
		TaskSeq:         1,
		ShortID:         "1",
		Title:           "Unicode preview",
		Body:            body,
		CreatedAtUnixMs: 1,
		UpdatedAtUnixMs: 1,
	}
	projector := NewTaskProjector()
	status, err := projector.DecodeStatus(TaskStatusInput{
		TaskID:             task.ID,
		Kind:               "backlog",
		NodeIDsJSON:        "[]",
		AttentionTypesJSON: "[]",
	})
	if err != nil {
		t.Fatalf("DecodeStatus: %v", err)
	}

	projected := projector.ProjectTaskFacts(TaskFactsInput{Task: task, Status: status})
	if task.Body != body {
		t.Fatalf("Task body changed during projection: %q", task.Body)
	}
	if preview := projected.Summary.GetBodyPreview(); !utf8.ValidString(preview) {
		t.Fatalf("Task body preview is invalid UTF-8: %q", preview)
	}
	return projected.Summary
}
