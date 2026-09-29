package main

import (
	"bytes"
	"core/shared/protoapi"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTaskListJSONPreservesEnrichedResponse(t *testing.T) {
	workflowID := runtimeids.NewWorkflowID()
	otherWorkflowID := runtimeids.NewWorkflowID()
	workflowName := "Delivery"
	nextOffset := int32(8)
	projectWide := &taskpb.ListSuccess{
		Scope:                       &taskpb.ListScope{ProjectId: "project-1"},
		MatchingWorkflowCardinality: taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_MULTIPLE,
		NextOffset:                  &nextOffset,
		GeneratedAt:                 timestamppb.New(time.UnixMilli(1720000000000)),
		Tasks: []*taskpb.ListItem{{
			TaskId:       "task-1",
			ShortId:      "KENT-1",
			WorkflowId:   workflowID.String(),
			WorkflowName: &workflowName,
			Title:        "Project-wide task",
			CreatedAt:    timestamppb.New(time.UnixMilli(1710000000000)),
			UpdatedAt:    timestamppb.New(time.UnixMilli(1720000000000)),
			Status:       taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE),
			Labels: []*workflowpb.ProjectLabel{
				{Id: "label-2", Name: "shared name"},
				{Id: "label-1", Name: "Alpha"},
			},
			DependencyProgress: &taskpb.DependencyProgress{
				SatisfiedCount: 1,
				TotalCount:     2,
			},
		}},
	}
	narrowed := &taskpb.ListSuccess{
		Scope: &taskpb.ListScope{
			ProjectId:  "project-1",
			WorkflowId: proto.String(otherWorkflowID.String()),
		},
		MatchingWorkflowCardinality: taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_ONE,
		GeneratedAt:                 timestamppb.New(time.UnixMilli(1720000001000)),
		Tasks: []*taskpb.ListItem{{
			TaskId:     "task-2",
			ShortId:    "KENT-2",
			WorkflowId: otherWorkflowID.String(),
			Title:      "Workflow task",
			CreatedAt:  timestamppb.New(time.UnixMilli(1710000001000)),
			UpdatedAt:  timestamppb.New(time.UnixMilli(1720000001000)),
			ColumnKeys: &taskpb.ColumnKeys{},
			Status:     taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE),
			Labels:     []*workflowpb.ProjectLabel{},
		}},
	}

	for _, test := range []struct {
		name        string
		response    *taskpb.ListSuccess
		cardinality string
		status      string
		nativeState string
	}{
		{
			name:        "project-wide",
			response:    projectWide,
			cardinality: "multiple", status: "active", nativeState: "active",
		},
		{
			name:        "workflow-narrowed",
			response:    narrowed,
			cardinality: "one", status: "done", nativeState: "terminal",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := writeTaskListResponse(&stdout, &stderr, test.response, true); code != 0 || stderr.Len() != 0 {
				t.Fatalf("JSON exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			var got struct {
				Scope struct {
					ProjectID  string  `json:"project_id"`
					WorkflowID *string `json:"workflow_id"`
				} `json:"scope"`
				Cardinality string `json:"matching_workflow_cardinality"`
				NextOffset  *int32 `json:"next_offset"`
				GeneratedAt int64  `json:"generated_at_unix_ms"`
				Tasks       []struct {
					ID           string    `json:"task_id"`
					ShortID      string    `json:"short_id"`
					WorkflowID   string    `json:"workflow_id"`
					WorkflowName *string   `json:"workflow_name"`
					Title        string    `json:"title"`
					CreatedAt    int64     `json:"created_at_unix_ms"`
					UpdatedAt    int64     `json:"updated_at_unix_ms"`
					ColumnKeys   *[]string `json:"column_keys"`
					Status       struct {
						Kind        string `json:"kind"`
						NativeState string `json:"native_state"`
					} `json:"status"`
					Labels             []*workflowpb.ProjectLabel  `json:"labels"`
					DependencyProgress *taskDependencyProgressJSON `json:"dependency_progress"`
				} `json:"tasks"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("decode JSON: %v", err)
			}
			want := test.response
			if got.Scope.ProjectID != want.Scope.ProjectId || !reflect.DeepEqual(got.Scope.WorkflowID, want.Scope.WorkflowId) ||
				got.Cardinality != test.cardinality || !reflect.DeepEqual(got.NextOffset, want.NextOffset) ||
				got.GeneratedAt != want.GeneratedAt.AsTime().UnixMilli() || len(got.Tasks) != len(want.Tasks) {
				t.Fatalf("response = %+v, want %+v", got, test.response)
			}
			for index, row := range got.Tasks {
				expected := want.Tasks[index]
				if row.ID != expected.TaskId || row.ShortID != expected.ShortId || row.Title != expected.Title ||
					row.WorkflowID != expected.WorkflowId || !reflect.DeepEqual(row.WorkflowName, expected.WorkflowName) ||
					row.CreatedAt != expected.CreatedAt.AsTime().UnixMilli() || row.UpdatedAt != expected.UpdatedAt.AsTime().UnixMilli() ||
					row.Status.Kind != test.status || row.Status.NativeState != test.nativeState ||
					!slices.EqualFunc(row.Labels, expected.Labels, func(left, right *workflowpb.ProjectLabel) bool { return proto.Equal(left, right) }) {
					t.Fatalf("row = %+v, want %+v", row, expected)
				}
				if (row.ColumnKeys == nil) != (expected.ColumnKeys == nil) ||
					(row.ColumnKeys != nil && !slices.Equal(*row.ColumnKeys, expected.ColumnKeys.Values)) {
					t.Fatalf("Current Node visibility changed: %+v", row)
				}
				if (row.DependencyProgress == nil) != (expected.DependencyProgress == nil) ||
					(row.DependencyProgress != nil && (row.DependencyProgress.SatisfiedCount != expected.DependencyProgress.SatisfiedCount ||
						row.DependencyProgress.TotalCount != expected.DependencyProgress.TotalCount)) {
					t.Fatalf("dependency progress changed: %+v", row)
				}
			}
		})
	}
}

func TestTaskListHumanProjectWideRendering(t *testing.T) {
	workflowID := runtimeids.NewWorkflowID()
	otherWorkflowID := runtimeids.NewWorkflowID()
	firstWorkflowName := "Delivery"
	secondWorkflowName := "Manual Move Router"
	nextOffset := int32(8)
	response := &taskpb.ListSuccess{
		Scope:                       &taskpb.ListScope{ProjectId: "project-1"},
		MatchingWorkflowCardinality: taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_MULTIPLE,
		NextOffset:                  &nextOffset,
		Tasks: []*taskpb.ListItem{
			{
				TaskId:       "task-1",
				ShortId:      "KENT-1",
				WorkflowId:   workflowID.String(),
				WorkflowName: &firstWorkflowName,
				Title:        "First task",
				Status:       taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE),
				Labels: []*workflowpb.ProjectLabel{
					{Id: "label-2", Name: "shared name"},
					{Id: "label-1", Name: "Alpha"},
				},
				DependencyProgress: &taskpb.DependencyProgress{
					SatisfiedCount: 1,
					TotalCount:     2,
				},
			},
			{
				TaskId:       "task-2",
				ShortId:      "KENT-2",
				WorkflowId:   otherWorkflowID.String(),
				WorkflowName: &secondWorkflowName,
				Title:        "Second task",
				Status:       taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE),
				Labels:       []*workflowpb.ProjectLabel{},
			},
		},
	}
	var stdout, stderr bytes.Buffer
	if code := writeTaskListResponse(&stdout, &stderr, response, false); code != 0 {
		t.Fatalf("human exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if got := stderr.String(); got != nextOffsetLine(int(nextOffset))+"\n" {
		t.Fatalf("pagination continuation = %q, want generated continuation", got)
	}
	lines := assertTaskListCommonHumanLines(t, stdout.Bytes(), 8, []int{0, 5}, response.Tasks)
	labelLine := lines[2]
	remaining, ok := taskListHumanPayloadAfterField(labelLine)
	if !ok {
		t.Fatalf("labels line has an invalid field prefix: %q", labelLine)
	}
	for index, label := range response.Tasks[0].Labels {
		if index > 0 {
			if len(remaining) == 0 || remaining[0] != ' ' {
				t.Fatalf("label %d is missing its separator: %q", index, labelLine)
			}
			remaining = remaining[1:]
		}
		quoted, err := strconv.QuotedPrefix(string(remaining))
		if err != nil {
			t.Fatalf("label %d is not quoted: %q", index, labelLine)
		}
		got, err := strconv.Unquote(quoted)
		if err != nil || got != label.Name {
			t.Fatalf("label %d = %q, want response value %q", index, got, label.Name)
		}
		remaining = remaining[len(quoted):]
	}
	if len(remaining) != 0 {
		t.Fatalf("labels line has trailing data: %q", labelLine)
	}
	for index, lineIndex := range []int{3, 7} {
		workflowName := response.Tasks[index].WorkflowName
		if workflowName == nil {
			t.Fatalf("workflow line has no response workflow name: %q", lines[lineIndex])
		}
		payload, ok := taskListHumanPayloadAfterField(lines[lineIndex])
		if !ok || !bytes.Equal(payload, []byte(*workflowName)) {
			t.Fatalf("workflow line does not preserve the response value: %q", lines[lineIndex])
		}
	}
	wantDependency := []byte(strconv.Itoa(int(response.Tasks[0].DependencyProgress.SatisfiedCount)) + "/" +
		strconv.Itoa(int(response.Tasks[0].DependencyProgress.TotalCount)))
	dependencyFields := bytes.Fields(lines[4])
	if len(dependencyFields) != 2 || dependencyFields[0][len(dependencyFields[0])-1] != ':' ||
		!bytes.Equal(dependencyFields[1], wantDependency) {
		t.Fatalf("dependency line does not preserve the response value: %q", lines[4])
	}

	soleWorkflowName := "Delivery"
	oneWorkflowResponse := &taskpb.ListSuccess{
		Scope:                       &taskpb.ListScope{ProjectId: "project-1"},
		MatchingWorkflowCardinality: taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_ONE,
		Tasks: []*taskpb.ListItem{{
			TaskId:       "task-3",
			ShortId:      "KENT-3",
			WorkflowId:   workflowID.String(),
			WorkflowName: &soleWorkflowName,
			Title:        "One workflow task",
			Status:       taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE),
			Labels:       []*workflowpb.ProjectLabel{},
		}},
	}
	stdout.Reset()
	stderr.Reset()
	if code := writeTaskListResponse(&stdout, &stderr, oneWorkflowResponse, false); code != 0 || stderr.Len() != 0 {
		t.Fatalf("one-workflow exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	lines = assertTaskListCommonHumanLines(t, stdout.Bytes(), 2, []int{0}, oneWorkflowResponse.Tasks)
}

func TestTaskListHumanWorkflowNarrowedRendering(t *testing.T) {
	workflowID := runtimeids.NewWorkflowID()
	workflowName := "Delivery"
	emptyColumnKeys := []string{}
	response := &taskpb.ListSuccess{
		Scope: &taskpb.ListScope{
			ProjectId:  "project-1",
			WorkflowId: proto.String(workflowID.String()),
		},
		MatchingWorkflowCardinality: taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_ONE,
		Tasks: []*taskpb.ListItem{
			{
				TaskId:       "task-1",
				ShortId:      "KENT-1",
				WorkflowId:   workflowID.String(),
				WorkflowName: &workflowName,
				Title:        "Narrowed task",
				ColumnKeys:   &taskpb.ColumnKeys{Values: []string{"build", "deploy"}},
				Status:       taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE),
			},
			{
				TaskId:       "task-2",
				ShortId:      "KENT-2",
				WorkflowId:   workflowID.String(),
				WorkflowName: &workflowName,
				Title:        "Empty nodes task",
				ColumnKeys:   &taskpb.ColumnKeys{Values: emptyColumnKeys},
				Status:       taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE),
				DependencyProgress: &taskpb.DependencyProgress{
					SatisfiedCount: 2,
					TotalCount:     2,
				},
			},
		},
	}
	var stdout, stderr bytes.Buffer
	if code := writeTaskListResponse(&stdout, &stderr, response, false); code != 0 || stderr.Len() != 0 {
		t.Fatalf("human exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	lines := assertTaskListCommonHumanLines(t, stdout.Bytes(), 6, []int{0, 3}, response.Tasks)
	currentFields := bytes.Fields(lines[2])
	wantCurrent := bytes.Fields([]byte(strings.Join(response.Tasks[0].ColumnKeys.Values, ", ")))
	if len(currentFields) != len(wantCurrent)+2 ||
		!bytes.Equal(bytes.Join(currentFields[2:], []byte{' '}), bytes.Join(wantCurrent, []byte{' '})) {
		t.Fatalf("current-nodes line does not preserve response values: %q", lines[2])
	}
	emptyFields := bytes.Fields(lines[5])
	if len(emptyFields) != 3 || len(emptyFields[2]) < 2 ||
		emptyFields[2][0] != '(' || emptyFields[2][len(emptyFields[2])-1] != ')' {
		t.Fatalf("empty current-nodes line has an invalid field shape: %q", lines[5])
	}
}

func assertTaskListCommonHumanLines(
	t *testing.T,
	output []byte,
	expectedLines int,
	headerLineIndexes []int,
	tasks []*taskpb.ListItem,
) [][]byte {
	t.Helper()
	lines := bytes.Split(bytes.TrimSuffix(output, []byte{'\n'}), []byte{'\n'})
	if len(lines) != expectedLines {
		t.Fatalf("human output lines=%d, want %d", len(lines), expectedLines)
	}
	if len(headerLineIndexes) != len(tasks) {
		t.Fatalf("header indexes=%d, tasks=%d", len(headerLineIndexes), len(tasks))
	}
	for _, line := range lines {
		if len(line) == 0 {
			t.Fatal("human output contains an empty line")
		}
	}
	for index, lineIndex := range headerLineIndexes {
		task := tasks[index]
		headerFields := bytes.Fields(lines[lineIndex])
		titleFields := bytes.Fields([]byte(task.Title + "."))
		if len(headerFields) == 0 || len(headerFields) != len(titleFields)+1 ||
			!bytes.Equal(headerFields[0], []byte(task.ShortId+":")) ||
			!bytes.Equal(bytes.Join(headerFields[1:], []byte{' '}), bytes.Join(titleFields, []byte{' '})) {
			t.Fatalf("header line does not preserve response values: %q", lines[lineIndex])
		}
		statusFields := bytes.Fields(lines[lineIndex+1])
		status, err := protoapi.TaskStatusKind.Decode(task.Status.Kind)
		if err != nil {
			t.Fatal(err)
		}
		if len(statusFields) != 2 || len(statusFields[0]) == 0 ||
			statusFields[0][len(statusFields[0])-1] != ':' ||
			!bytes.Equal(statusFields[1], []byte(status)) {
			t.Fatalf("status line does not preserve the response value: %q", lines[lineIndex+1])
		}
	}
	return lines
}

func taskListHumanPayloadAfterField(line []byte) ([]byte, bool) {
	fields := bytes.Fields(line)
	if len(fields) < 2 || len(fields[0]) == 0 || fields[0][len(fields[0])-1] != ':' ||
		len(line) <= len(fields[0]) || line[len(fields[0])] != ' ' {
		return nil, false
	}
	return line[len(fields[0])+1:], true
}
