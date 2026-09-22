package workflowview

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TaskProjector struct{}

type TaskStatusInput struct {
	TaskID             string
	Kind               string
	NodeIDsJSON        string
	AttentionTypesJSON string
	Done               bool
}

type TaskFactsInput struct {
	Task              sqlitegen.TaskRecord
	Status            workflowTaskStatusFact
	CurrentNodes      []workflow.CurrentNode
	LiveExecutions    []sessionruntime.TaskExecution
	ConcurrencyQueued []workflow.CurrentNodeReference
	Definition        definitionSnapshot
	CanDelete         bool
}

type TaskFacts struct {
	Summary *taskpb.TaskSummary
	Status  *taskpb.TaskStatus
	Actions *taskpb.TaskActions
	Done    bool
}

func NewTaskProjector() *TaskProjector {
	return &TaskProjector{}
}

func (*TaskProjector) DecodeStatus(input TaskStatusInput) (workflowTaskStatusFact, error) {
	kind, err := protoapi.TaskStatusKind.Encode(input.Kind)
	if err != nil {
		return workflowTaskStatusFact{}, fmt.Errorf("workflow task status record for task %q has invalid kind %q", input.TaskID, input.Kind)
	}
	nativeState, err := protoapi.TaskNativeState(kind)
	if err != nil {
		return workflowTaskStatusFact{}, err
	}
	nodeIDs, err := workflowTaskStatusIDs(input.TaskID, "node_ids_json", input.NodeIDsJSON)
	if err != nil {
		return workflowTaskStatusFact{}, err
	}
	attentionTypes, err := workflowTaskStatusAttentionTypes(input.TaskID, input.AttentionTypesJSON)
	if err != nil {
		return workflowTaskStatusFact{}, err
	}
	return workflowTaskStatusFact{
		Status: &taskpb.TaskStatus{
			Kind:           kind,
			NativeState:    nativeState,
			NodeIds:        nodeIDs,
			AttentionTypes: attentionTypes,
		},
		Done: input.Done,
	}, nil
}

func (*TaskProjector) ProjectTaskFacts(input TaskFactsInput) TaskFacts {
	done := input.Status.Done || currentNodesContainTerminal(input.CurrentNodes, input.Definition.nodeKinds)
	return TaskFacts{
		Summary: taskSummary(input.Task, input.Status.Status, done),
		Status:  input.Status.Status,
		Actions: taskActions(
			done,
			input.Status.Status,
			input.CurrentNodes,
			input.LiveExecutions,
			input.ConcurrencyQueued,
			input.CanDelete,
		),
		Done: done,
	}
}

func Comment(comment workflowstore.CommentRecord) (*taskpb.Comment, error) {
	author, err := protoapi.TaskCommentAuthor.Encode(comment.Author)
	if err != nil {
		return nil, err
	}
	var authorID *string
	if comment.AuthorID != "" {
		authorID = &comment.AuthorID
	}
	return &taskpb.Comment{
		Id: comment.ID, TaskId: string(comment.TaskID), Body: comment.Body, Author: author, AuthorId: authorID,
		CreatedAt: timestamppb.New(time.UnixMilli(comment.CreatedAt)),
		UpdatedAt: timestamppb.New(time.UnixMilli(comment.UpdatedAt)),
	}, nil
}

func ProjectCurrentNodes(nodes []workflow.CurrentNode) []*taskpb.AttentionCurrentNode {
	projected := make([]*taskpb.AttentionCurrentNode, 0, len(nodes))
	for _, currentNode := range nodes {
		projected = append(projected, workflowCurrentNode(currentNode))
	}
	return projected
}

func workflowCurrentNode(currentNode workflow.CurrentNode) *taskpb.AttentionCurrentNode {
	projected := workflowCurrentNodeReference(currentNode.Reference)
	if currentNode.SessionID != nil {
		value := currentNode.SessionID.String()
		projected.SessionId = &value
	}
	if currentNode.AgentExecutionSelection != nil {
		assignee := currentNode.AgentExecutionSelection.Assignee
		projected.EffectiveAssignee = &assignee
		if currentNode.AgentExecutionSelection.Thinking != nil {
			thinking := string(*currentNode.AgentExecutionSelection.Thinking)
			projected.EffectiveThinking = &thinking
		}
	}
	return projected
}

func workflowCurrentNodeReference(reference workflow.CurrentNodeReference) *taskpb.AttentionCurrentNode {
	projected := &taskpb.AttentionCurrentNode{NodeId: string(reference.NodeID)}
	if value, present := reference.TransitionBranchKey(); present {
		branch := string(value)
		projected.TransitionBranchKey = &branch
	}
	return projected
}

func workflowTaskStatusIDs(taskID string, field string, encoded string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return nil, fmt.Errorf("workflow task status record for task %q has malformed %s: %w", taskID, field, err)
	}
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("workflow task status record for task %q has blank %s[%d]", taskID, field, index)
		}
		if index > 0 && values[index-1] >= value {
			return nil, fmt.Errorf("workflow task status record for task %q has non-deterministic %s", taskID, field)
		}
	}
	return values, nil
}

func workflowTaskStatusAttentionTypes(taskID string, encoded string) ([]taskpb.TaskAttentionKind, error) {
	var values []string
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return nil, fmt.Errorf("workflow task status record for task %q has malformed attention_types_json: %w", taskID, err)
	}
	out := make([]taskpb.TaskAttentionKind, 0, len(values))
	for index, value := range values {
		kind, err := protoapi.TaskAttentionKind.Encode(value)
		if err != nil {
			return nil, fmt.Errorf("workflow task status record for task %q has unknown attention_types_json[%d] %q", taskID, index, value)
		}
		if index > 0 && values[index-1] >= value {
			return nil, fmt.Errorf("workflow task status record for task %q has non-deterministic attention_types_json", taskID)
		}
		out = append(out, kind)
	}
	return out, nil
}

func taskSummary(task sqlitegen.TaskRecord, status *taskpb.TaskStatus, done bool) *taskpb.TaskSummary {
	return &taskpb.TaskSummary{
		Id:                task.ID,
		ProjectId:         task.ProjectID,
		WorkflowId:        task.WorkflowID.String(),
		ShortId:           task.ShortID,
		Title:             task.Title,
		BodyPreview:       proto.String(bodyPreview(task.Body)),
		SourceWorkspaceId: metadata.OptionalString(task.SourceWorkspaceID),
		CreatedAt:         timestamppb.New(time.UnixMilli(task.CreatedAtUnixMs)),
		UpdatedAt:         timestamppb.New(time.UnixMilli(task.UpdatedAtUnixMs)),
		Done:              done,
		ActiveNodeIds:     append([]string(nil), status.NodeIds...),
	}
}

func currentNodesContainTerminal(nodes []workflow.CurrentNode, nodeKinds map[string]workflow.NodeKind) bool {
	for _, currentNode := range nodes {
		if nodeKinds[string(currentNode.Reference.NodeID)] == workflow.NodeKindTerminal {
			return true
		}
	}
	return false
}

func taskActions(
	done bool,
	status *taskpb.TaskStatus,
	currentNodes []workflow.CurrentNode,
	live []sessionruntime.TaskExecution,
	concurrencyQueued []workflow.CurrentNodeReference,
	canDelete bool,
) *taskpb.TaskActions {
	hasLiveExecution := len(live) != 0
	hasInterruptibleExecution := false
	for _, execution := range live {
		hasInterruptibleExecution = hasInterruptibleExecution ||
			(!execution.Queued && !execution.HasPendingPrompts())
	}
	actions := &taskpb.TaskActions{
		CanStart:     !done && !hasLiveExecution && status.Kind == taskpb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG,
		CanInterrupt: !done && hasInterruptibleExecution,
		CanResume: !done &&
			(len(concurrencyQueued) != 0 ||
				(!hasLiveExecution &&
					(status.Kind == taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED ||
						status.Kind == taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE))),
		CanDelete: canDelete,
	}
	return actions
}
