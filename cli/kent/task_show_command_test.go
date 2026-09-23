package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func taskShowFixture(t *testing.T) *taskpb.TaskDetail {
	t.Helper()
	workflowID := testsetup.WorkflowID(t, "task-show").String()
	return &taskpb.TaskDetail{
		Summary: &taskpb.TaskSummary{
			Id: "task-1", ShortId: "KNT-1", Title: "Task", ProjectId: "project-1", WorkflowId: workflowID,
			CreatedAt: timestamppb.New(time.UnixMilli(1)), UpdatedAt: timestamppb.New(time.UnixMilli(1)),
		},
		Project:  &taskpb.BoardProject{ProjectKey: "KNT", DisplayName: "Project", DefaultWorkspaceId: "workspace-1", AttachedWorkspaceCount: 1},
		Workflow: &taskpb.TaskWorkflowSummary{WorkflowId: workflowID, DisplayName: "Workflow", Version: 1},
		SourceWorkspace: &taskpb.TaskSourceWorkspace{
			WorkspaceId: "workspace-1", DisplayName: "Main", RootPath: "/workspace",
			Availability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		},
		Status:  taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE),
		Actions: &taskpb.TaskActions{}, Dependencies: &taskpb.TaskDependencies{},
	}
}

func taskShowJSONForTest(t *testing.T, task *taskpb.TaskDetail) []byte {
	t.Helper()
	output, err := taskShowOutputFromDetail(task)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTaskShowJSONIncludesCurrentNodesAndRetainedSessionCount(t *testing.T) {
	task := taskShowFixture(t)
	task.CurrentNodes = []*taskpb.AttentionCurrentNode{{NodeId: "node-1", SessionId: proto.String("session-1")}}
	task.RetainedSessionCount = 3
	var output struct {
		CurrentNodes []struct {
			NodeID string `json:"node_id"`
		} `json:"current_nodes"`
		RetainedSessionCount int `json:"retained_session_count"`
	}
	if err := json.Unmarshal(taskShowJSONForTest(t, task), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.CurrentNodes) != 1 || output.CurrentNodes[0].NodeID != "node-1" || output.RetainedSessionCount != 3 {
		t.Fatalf("Task detail facts lost: %+v", output)
	}
}

func TestTaskShowJSONIncludesEffectiveCurrentNodeSelection(t *testing.T) {
	task := taskShowFixture(t)
	task.CurrentNodes = []*taskpb.AttentionCurrentNode{{
		NodeId: "node-1", EffectiveAssignee: proto.String("reviewer"), EffectiveThinking: proto.String("high"),
	}}
	var output struct {
		CurrentNodes []struct {
			Assignee *string `json:"effective_assignee"`
			Thinking *string `json:"effective_thinking"`
		} `json:"current_nodes"`
	}
	if err := json.Unmarshal(taskShowJSONForTest(t, task), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.CurrentNodes) != 1 || output.CurrentNodes[0].Assignee == nil || *output.CurrentNodes[0].Assignee != "reviewer" ||
		output.CurrentNodes[0].Thinking == nil || *output.CurrentNodes[0].Thinking != "high" {
		t.Fatalf("effective selections lost: %+v", output)
	}
}

func TestTaskShowHumanOutputReportsEffectiveCurrentNodeSelection(t *testing.T) {
	task := taskShowFixture(t)
	task.CurrentNodes = []*taskpb.AttentionCurrentNode{{
		NodeId: "node-1", EffectiveAssignee: proto.String("reviewer"), EffectiveThinking: proto.String("high"),
	}}
	var output bytes.Buffer
	if err := writeTaskDetail(&output, task); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("reviewer")) || !bytes.Contains(output.Bytes(), []byte("high")) {
		t.Fatalf("effective selections missing: %q", output.String())
	}
}

func TestTaskShowHumanOutputReportsRetainedSessionsWithoutDuplicatingCurrentNodeIdentity(t *testing.T) {
	task := taskShowFixture(t)
	task.CurrentNodes = []*taskpb.AttentionCurrentNode{{NodeId: "node-1", SessionId: proto.String("session-1")}}
	task.LiveSessions = []*taskpb.LiveSession{{SessionId: "session-1", NodeDisplayName: "Agent"}}
	task.RetainedSessionCount = 3
	var output bytes.Buffer
	if err := writeTaskDetail(&output, task); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("Current session: session-1\n")) ||
		!bytes.Contains(output.Bytes(), []byte("Retained sessions: 3\n")) || bytes.Contains(output.Bytes(), []byte("node-1")) {
		t.Fatalf("Session presentation = %q", output.String())
	}
}

func TestTaskShowHumanOutputUsesSharedDependencySections(t *testing.T) {
	task := taskShowFixture(t)
	task.Dependencies = &taskpb.TaskDependencies{
		BlockerCount: 1, UnsatisfiedBlockerCount: 1,
		Directions: []*taskpb.DependencyDirectionProjection{{
			Direction: taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKED_BY, TotalCount: 1, UnsatisfiedCount: proto.Int32(1),
			Items: []*taskpb.DependencyItem{{
				TaskId: "task-2", ShortId: "KNT-2", Title: "Foundation", WorkflowId: task.Summary.WorkflowId,
				Status:       taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE),
				Satisfaction: taskpb.DependencySatisfaction_DEPENDENCY_SATISFACTION_UNSATISFIED.Enum(),
			}},
		}},
	}
	var output bytes.Buffer
	if err := writeTaskDetail(&output, task); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(output.Bytes(), []byte("Blocked by:\nKNT-2: Foundation (active)\n")) {
		t.Fatalf("dependency sections = %q", output.String())
	}
}

func TestTaskShowJSONIncludesAggregateDependenciesOnlyWhenNonzero(t *testing.T) {
	task := taskShowFixture(t)
	task.Dependencies = &taskpb.TaskDependencies{
		BlockerCount: 2, UnsatisfiedBlockerCount: 1, DirectlyBlockedTaskCount: 3,
		Directions: []*taskpb.DependencyDirectionProjection{{
			Direction: taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS,
			Items:     []*taskpb.DependencyItem{{TaskId: "must-not-leak"}},
		}},
	}
	data := taskShowJSONForTest(t, task)
	var output struct {
		Dependencies map[string]int `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Dependencies) != 3 || output.Dependencies["blocker_count"] != 2 ||
		output.Dependencies["unsatisfied_blocker_count"] != 1 || output.Dependencies["blocked_task_count"] != 3 {
		t.Fatalf("dependency counts = %s", data)
	}
	if bytes.Contains(data, []byte("directions")) || bytes.Contains(data, []byte("must-not-leak")) {
		t.Fatalf("Task Show leaked dependency entries: %s", data)
	}
	if bytes.Contains(taskShowJSONForTest(t, taskShowFixture(t)), []byte(`"dependencies"`)) {
		t.Fatal("empty dependency counts were emitted")
	}
}
