package workflowview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/runtimeactivity"
	"core/server/session"
	"core/server/workflow"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

type TaskSessions struct {
	queries    *sqlitegen.Queries
	activities ActiveTaskSessionActivitySource
}

type ActiveTaskSessionActivitySource interface {
	ActiveRuntimeActivitySnapshots(context.Context) ([]runtimeactivity.ActiveSessionSnapshot, error)
}

type taskSessionProjection struct {
	item            *taskpb.SessionItem
	createdAtUnixMs int64
}

func NewTaskSessions(metadataStore *metadata.Store, activities ActiveTaskSessionActivitySource) (*TaskSessions, error) {
	if metadataStore == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	if activities == nil {
		return nil, errors.New("active Task Session activity source is required")
	}
	return &TaskSessions{queries: metadataStore.Queries(), activities: activities}, nil
}

func (s *TaskSessions) List(ctx context.Context, req *taskpb.TaskOffsetPageRequest) (*taskpb.SessionListSuccess, error) {
	if s == nil || s.queries == nil {
		return nil, errors.New("Task Sessions read model is required")
	}
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	taskID := strings.TrimSpace(req.TaskId)
	if _, err := s.queries.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	window := TaskPageWindow(req)
	active, activeSessionIDs, err := s.activeTaskSessions(ctx, taskID)
	if err != nil {
		return nil, err
	}
	capacity := window.Limit + 1
	items := make([]*taskpb.SessionItem, 0, capacity)
	if window.Offset < len(active) {
		activeEnd := min(len(active), window.Offset+capacity)
		for _, projection := range active[window.Offset:activeEnd] {
			items = append(items, projection.item)
		}
	}
	remaining := capacity - len(items)
	if remaining > 0 {
		excludedSessionIDsJSON, err := json.Marshal(activeSessionIDs)
		if err != nil {
			return nil, err
		}
		idleOffset := max(window.Offset-len(active), 0)
		rows, err := s.queries.ListIdleWorkflowTaskSessions(ctx, sqlitegen.ListIdleWorkflowTaskSessionsParams{
			TaskID:                 sql.NullString{String: taskID, Valid: true},
			ExcludedSessionIdsJson: string(excludedSessionIDsJSON),
			PageOffset:             int64(idleOffset),
			PageLimit:              int64(remaining),
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			projection, err := taskSessionProjectionFromFields(
				row.SessionID,
				row.SessionName,
				row.NodeName,
				row.ContinuationJson,
				row.CreatedAtUnixMs,
				taskpb.SessionStatus_SESSION_STATUS_IDLE,
			)
			if err != nil {
				return nil, err
			}
			items = append(items, projection.item)
		}
	}
	page := serverapi.FinalizeWorkflowOffsetPage(window, items)
	next, err := TaskNextOffset(page.NextOffset)
	if err != nil {
		return nil, err
	}
	return &taskpb.SessionListSuccess{TaskId: taskID, Items: page.Items, NextOffset: next}, nil
}

func (s *TaskSessions) activeTaskSessions(ctx context.Context, taskID string) ([]taskSessionProjection, []string, error) {
	snapshots, err := s.activities.ActiveRuntimeActivitySnapshots(ctx)
	if err != nil {
		return nil, nil, err
	}
	statuses := make(map[string]taskpb.SessionStatus, len(snapshots))
	candidateIDs := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		status, err := taskSessionStatus(snapshot.Activity)
		if err != nil {
			return nil, nil, fmt.Errorf("Session %q runtime activity: %w", snapshot.SessionID, err)
		}
		if status == taskpb.SessionStatus_SESSION_STATUS_IDLE {
			continue
		}
		statuses[snapshot.SessionID] = status
		candidateIDs = append(candidateIDs, snapshot.SessionID)
	}
	if len(candidateIDs) == 0 {
		return []taskSessionProjection{}, []string{}, nil
	}
	sessionIDsJSON, err := json.Marshal(candidateIDs)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.queries.ListActiveWorkflowTaskSessions(ctx, sqlitegen.ListActiveWorkflowTaskSessionsParams{
		TaskID:         sql.NullString{String: taskID, Valid: true},
		SessionIdsJson: string(sessionIDsJSON),
	})
	if err != nil {
		return nil, nil, err
	}
	active := make([]taskSessionProjection, 0, len(rows))
	activeSessionIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		status, exists := statuses[row.SessionID]
		if !exists {
			return nil, nil, fmt.Errorf("active Task Session query returned uncaptured Session %q", row.SessionID)
		}
		projection, err := taskSessionProjectionFromFields(
			row.SessionID,
			row.SessionName,
			row.NodeName,
			row.ContinuationJson,
			row.CreatedAtUnixMs,
			status,
		)
		if err != nil {
			return nil, nil, err
		}
		active = append(active, projection)
		activeSessionIDs = append(activeSessionIDs, row.SessionID)
	}
	sort.Slice(active, func(left int, right int) bool {
		leftRank := taskSessionStatusRank(active[left].item.Status)
		rightRank := taskSessionStatusRank(active[right].item.Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if active[left].createdAtUnixMs != active[right].createdAtUnixMs {
			return active[left].createdAtUnixMs > active[right].createdAtUnixMs
		}
		return active[left].item.SessionId > active[right].item.SessionId
	})
	return active, activeSessionIDs, nil
}

func taskSessionStatus(activity *runtimepb.Activity) (taskpb.SessionStatus, error) {
	if err := protoapi.Validate(activity); err != nil {
		return taskpb.SessionStatus_SESSION_STATUS_UNSPECIFIED, err
	}
	switch activity.State {
	case runtimepb.ActivityState_RUNTIME_ACTIVITY_AWAITING_PROMPT:
		return taskpb.SessionStatus_SESSION_STATUS_QUESTION, nil
	case runtimepb.ActivityState_RUNTIME_ACTIVITY_STARTING,
		runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING,
		runtimepb.ActivityState_RUNTIME_ACTIVITY_DRAINING,
		runtimepb.ActivityState_RUNTIME_ACTIVITY_CLOSING:
		return taskpb.SessionStatus_SESSION_STATUS_RUNNING, nil
	case runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE, runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE:
		return taskpb.SessionStatus_SESSION_STATUS_IDLE, nil
	default:
		return taskpb.SessionStatus_SESSION_STATUS_UNSPECIFIED, fmt.Errorf("unsupported runtime activity state %q", activity.State)
	}
}

func taskSessionStatusRank(status taskpb.SessionStatus) int {
	switch status {
	case taskpb.SessionStatus_SESSION_STATUS_RUNNING:
		return 0
	case taskpb.SessionStatus_SESSION_STATUS_QUESTION:
		return 1
	case taskpb.SessionStatus_SESSION_STATUS_IDLE:
		return 2
	default:
		panic(fmt.Sprintf("unknown Task Session status %q", status))
	}
}

func taskSessionProjectionFromFields(
	sessionID string,
	sessionName string,
	nodeName sql.NullString,
	continuationJSON string,
	createdAtUnixMs int64,
	status taskpb.SessionStatus,
) (taskSessionProjection, error) {
	agentRole, err := taskSessionAgentRole(continuationJSON)
	if err != nil {
		return taskSessionProjection{}, fmt.Errorf("Session %q Agent role: %w", sessionID, err)
	}
	return taskSessionProjection{
		item: &taskpb.SessionItem{
			SessionId:   sessionID,
			SessionName: optionalTaskSessionString(sessionName),
			NodeName:    metadata.OptionalString(nodeName),
			AgentRole:   agentRole,
			Status:      status,
		},
		createdAtUnixMs: createdAtUnixMs,
	}, nil
}

func taskSessionAgentRole(continuationJSON string) (string, error) {
	var continuation session.ContinuationContext
	if err := json.Unmarshal([]byte(continuationJSON), &continuation); err != nil {
		return "", fmt.Errorf("decode continuation: %w", err)
	}
	normalized, err := session.NormalizeContinuationContext(continuation)
	if err != nil {
		return "", err
	}
	if normalized == nil || normalized.AgentRole == nil {
		return workflow.DefaultAgentRole, nil
	}
	return *normalized.AgentRole, nil
}

func optionalTaskSessionString(value string) *string {
	if value == "" {
		return nil
	}
	copied := value
	return &copied
}
