package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/shared/runtimeids"
)

type ApprovalAttentionProjection struct {
	ApprovalID       workflow.ApprovalID
	Source           workflow.CurrentNodeReference
	ProjectID        string
	WorkflowID       runtimeids.WorkflowID
	TaskShortID      string
	TaskTitle        string
	SessionID        string
	OccurredAtUnixMs int64
}

type InterruptedCurrentNodeAttentionProjection struct {
	CurrentNode            workflow.CurrentNodeReference
	ProjectID              string
	WorkflowID             runtimeids.WorkflowID
	TaskShortID            string
	TaskTitle              string
	SessionID              string
	InterruptionReason     string
	InterruptionDetailJSON string
	OccurredAtUnixMs       int64
}

type TaskAttentionResolution struct {
	Approvals               []ApprovalAttentionProjection
	InterruptedCurrentNodes []InterruptedCurrentNodeAttentionProjection
}

func (s *Store) PendingApprovalAttentionProjection(ctx context.Context, approvalID workflow.ApprovalID) (ApprovalAttentionProjection, bool, error) {
	if err := approvalID.Validate(); err != nil {
		return ApprovalAttentionProjection{}, false, err
	}
	return pendingApprovalAttentionProjection(ctx, s.queries, approvalID)
}

func pendingApprovalAttentionProjection(ctx context.Context, q *sqlitegen.Queries, approvalID workflow.ApprovalID) (ApprovalAttentionProjection, bool, error) {
	rows, err := q.ListPendingApprovalAttentionFacts(ctx, sqlitegen.ListPendingApprovalAttentionFactsParams{
		ApprovalID: sql.NullString{String: approvalID.String(), Valid: true}, TaskIdsJson: "[]",
	})
	if err != nil {
		return ApprovalAttentionProjection{}, false, err
	}
	if len(rows) == 0 {
		return ApprovalAttentionProjection{}, false, nil
	}
	projection, err := approvalAttentionProjectionFromRow(rows[0])
	return projection, err == nil, err
}

func approvalAttentionProjectionFromRow(row sqlitegen.ListPendingApprovalAttentionFactsRow) (ApprovalAttentionProjection, error) {
	approvalID, err := workflow.ParseApprovalID(row.ID)
	if err != nil {
		return ApprovalAttentionProjection{}, err
	}
	source, sourceSession, err := pendingApprovalSourceFromRow(row.SourceTaskID, row.SourceNodeID, row.SourceTransitionBranchKey, row.SourceSessionID)
	if err != nil {
		return ApprovalAttentionProjection{}, err
	}
	sessionID := ""
	if sourceSession != nil {
		sessionID = sourceSession.String()
	}
	return ApprovalAttentionProjection{
		ApprovalID:       approvalID,
		Source:           source,
		ProjectID:        row.ProjectID,
		WorkflowID:       row.WorkflowID,
		TaskShortID:      row.ShortID,
		TaskTitle:        row.Title,
		SessionID:        sessionID,
		OccurredAtUnixMs: row.CreatedAtUnixMs,
	}, nil
}

func (s *Store) PendingInterruptedCurrentNodeAttentionProjection(ctx context.Context, reference workflow.CurrentNodeReference) (InterruptedCurrentNodeAttentionProjection, bool, error) {
	if err := reference.Validate(); err != nil {
		return InterruptedCurrentNodeAttentionProjection{}, false, err
	}
	return s.pendingInterruptedCurrentNodeAttentionProjection(ctx, s.queries, reference)
}

func (s *Store) pendingInterruptedCurrentNodeAttentionProjection(ctx context.Context, q *sqlitegen.Queries, reference workflow.CurrentNodeReference) (InterruptedCurrentNodeAttentionProjection, bool, error) {
	currentNode, err := s.currentNodeForReference(ctx, q, reference)
	if errors.Is(err, sql.ErrNoRows) {
		return InterruptedCurrentNodeAttentionProjection{}, false, nil
	}
	if err != nil {
		return InterruptedCurrentNodeAttentionProjection{}, false, err
	}
	if !isAttentionInterruption(currentNode) {
		return InterruptedCurrentNodeAttentionProjection{}, false, nil
	}
	task, err := q.GetTask(ctx, string(reference.TaskID))
	if err != nil {
		return InterruptedCurrentNodeAttentionProjection{}, false, err
	}
	projection, err := interruptionAttentionProjection(task, currentNode)
	return projection, err == nil, err
}

func isAttentionInterruption(currentNode workflow.CurrentNode) bool {
	return currentNode.Scheduling != nil &&
		currentNode.Scheduling.State == workflow.CurrentNodeSchedulingInterrupted &&
		currentNode.Scheduling.Interruption != nil &&
		attentionInterruptionRequiresNotification(string(currentNode.Scheduling.Interruption.Reason))
}

func interruptionAttentionProjection(task sqlitegen.TaskRecord, currentNode workflow.CurrentNode) (InterruptedCurrentNodeAttentionProjection, error) {
	interruption := currentNode.Scheduling.Interruption
	detail, err := workflow.MarshalString(interruption.Detail)
	if err != nil {
		return InterruptedCurrentNodeAttentionProjection{}, fmt.Errorf("encode interrupted current node attention detail: %w", err)
	}
	sessionID := ""
	if currentNode.SessionID != nil {
		sessionID = currentNode.SessionID.String()
	}
	return InterruptedCurrentNodeAttentionProjection{
		CurrentNode:            currentNode.Reference,
		ProjectID:              task.ProjectID,
		WorkflowID:             task.WorkflowID,
		TaskShortID:            task.ShortID,
		TaskTitle:              task.Title,
		SessionID:              sessionID,
		InterruptionReason:     string(interruption.Reason),
		InterruptionDetailJSON: detail,
		OccurredAtUnixMs:       interruption.OccurredAt.UnixMilli(),
	}, nil
}

func (s *Store) taskAttentionResolution(ctx context.Context, q *sqlitegen.Queries, taskID workflow.TaskID) (TaskAttentionResolution, error) {
	approvals, err := taskApprovalAttentionResolutions(ctx, q, []string{string(taskID)})
	if err != nil {
		return TaskAttentionResolution{}, err
	}
	interruptions, err := s.taskInterruptionAttentionResolutions(ctx, q, taskID)
	return TaskAttentionResolution{Approvals: approvals, InterruptedCurrentNodes: interruptions}, err
}

func (s *Store) taskInterruptionAttentionResolutions(ctx context.Context, q *sqlitegen.Queries, taskID workflow.TaskID) ([]InterruptedCurrentNodeAttentionProjection, error) {
	currentNodes, err := s.listTaskCurrentNodes(ctx, q, taskID)
	if err != nil {
		return nil, err
	}
	var task *sqlitegen.TaskRecord
	var projections []InterruptedCurrentNodeAttentionProjection
	for _, currentNode := range currentNodes {
		if !isAttentionInterruption(currentNode) {
			continue
		}
		if task == nil {
			row, err := q.GetTask(ctx, string(taskID))
			if err != nil {
				return nil, err
			}
			task = &row
		}
		projection, err := interruptionAttentionProjection(*task, currentNode)
		if err != nil {
			return nil, err
		}
		projections = append(projections, projection)
	}
	return projections, nil
}

func taskApprovalAttentionResolutions(ctx context.Context, q *sqlitegen.Queries, taskIDs []string) ([]ApprovalAttentionProjection, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}
	encodedIDs, err := workflow.MarshalString(taskIDs)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListPendingApprovalAttentionFacts(ctx, sqlitegen.ListPendingApprovalAttentionFactsParams{TaskIdsJson: encodedIDs})
	if err != nil {
		return nil, err
	}
	var projections []ApprovalAttentionProjection
	for _, row := range rows {
		projection, err := approvalAttentionProjectionFromRow(row)
		if err != nil {
			return nil, err
		}
		projections = append(projections, projection)
	}
	return projections, nil
}

func (s *Store) workflowAttentionResolution(ctx context.Context, q *sqlitegen.Queries, workflowID runtimeids.WorkflowID) (TaskAttentionResolution, error) {
	taskIDs, err := q.ListWorkflowTaskIDs(ctx, workflowID)
	if err != nil {
		return TaskAttentionResolution{}, err
	}
	approvals, err := taskApprovalAttentionResolutions(ctx, q, taskIDs)
	if err != nil {
		return TaskAttentionResolution{}, err
	}
	resolution := TaskAttentionResolution{Approvals: approvals}
	for _, taskID := range taskIDs {
		interruptions, err := s.taskInterruptionAttentionResolutions(ctx, q, workflow.TaskID(taskID))
		if err != nil {
			return TaskAttentionResolution{}, err
		}
		resolution.InterruptedCurrentNodes = append(resolution.InterruptedCurrentNodes, interruptions...)
	}
	return resolution, nil
}

func attentionInterruptionRequiresNotification(reason string) bool {
	return workflow.IsActionableCurrentNodeInterruptionReason(
		workflow.CurrentNodeInterruptionReason(strings.TrimSpace(reason)),
	)
}
