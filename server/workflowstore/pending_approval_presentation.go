package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/shared/runtimeids"
)

func (s *Store) ListPendingApprovals(ctx context.Context, taskID workflow.TaskID) ([]workflow.PendingApproval, error) {
	if strings.TrimSpace(string(taskID)) == "" {
		return nil, errors.New("task id is required")
	}
	return s.pendingApprovalPresentations(ctx, sql.NullString{String: string(taskID), Valid: true}, nil)
}

func (s *Store) PendingApproval(ctx context.Context, approvalID workflow.ApprovalID) (workflow.PendingApproval, error) {
	approvals, err := s.PendingApprovalsByID(ctx, []workflow.ApprovalID{approvalID})
	if err != nil {
		return workflow.PendingApproval{}, err
	}
	approval, ok := approvals[approvalID]
	if !ok {
		return workflow.PendingApproval{}, sql.ErrNoRows
	}
	return approval, nil
}

// PendingApprovalsByID projects only the requested approvals that are still pending.
func (s *Store) PendingApprovalsByID(ctx context.Context, ids []workflow.ApprovalID) (map[workflow.ApprovalID]workflow.PendingApproval, error) {
	out := make(map[workflow.ApprovalID]workflow.PendingApproval, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pendingApprovalPresentations(ctx, sql.NullString{}, ids)
	if err != nil {
		return nil, err
	}
	for _, approval := range rows {
		out[approval.ID] = approval
	}
	return out, nil
}

func (s *Store) pendingApprovalPresentations(ctx context.Context, taskID sql.NullString, ids []workflow.ApprovalID) ([]workflow.PendingApproval, error) {
	encodedIDs, err := workflow.MarshalString(ids)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	headers, err := q.ListPendingApprovalPresentationHeaders(ctx, sqlitegen.ListPendingApprovalPresentationHeadersParams{
		TaskID: taskID, ApprovalIdsJson: encodedIDs,
	})
	if err != nil {
		return nil, err
	}
	approvals := make([]workflow.PendingApproval, 0, len(headers))
	if len(headers) == 0 {
		return approvals, nil
	}
	headerIDs := make([]string, 0, len(headers))
	for _, header := range headers {
		headerIDs = append(headerIDs, header.TaskPendingApproval.ID)
	}
	branchIDs, err := workflow.MarshalString(headerIDs)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListPendingApprovalPresentationBranches(ctx, branchIDs)
	if err != nil {
		return nil, err
	}
	branches := make(map[string][]sqlitegen.ListPendingApprovalPresentationBranchesRow, len(headers))
	for _, row := range rows {
		branches[row.ApprovalID] = append(branches[row.ApprovalID], row)
	}
	for _, header := range headers {
		approval, err := pendingApprovalPresentationFromRows(header, branches[header.TaskPendingApproval.ID])
		if err != nil {
			return nil, err
		}
		approvals = append(approvals, approval)
	}
	return approvals, nil
}

func pendingApprovalPresentationFromRows(
	row sqlitegen.ListPendingApprovalPresentationHeadersRow,
	branches []sqlitegen.ListPendingApprovalPresentationBranchesRow,
) (workflow.PendingApproval, error) {
	facts, err := pendingApprovalHeaderFromRow(row.TaskPendingApproval)
	if err != nil {
		return workflow.PendingApproval{}, err
	}
	if !row.SourceKey.Valid {
		return workflow.PendingApproval{}, errors.New("pending approval source node is missing")
	}
	if len(branches) == 0 {
		return workflow.PendingApproval{}, errors.New("pending approval has no branch choices")
	}
	var group *workflow.TransitionGroup
	if row.CurrentTransitionKey.Valid {
		group = &workflow.TransitionGroup{
			ID: facts.TransitionID, WorkflowID: row.WorkflowID, SourceNodeID: facts.Source.NodeID,
			TransitionID: workflow.TransitionID(row.CurrentTransitionKey.String),
			DisplayName:  row.CurrentTransitionName.String, Description: row.CurrentTransitionDescription.String,
		}
	}
	approval := facts.presentation(workflow.DisplayLabel(row.SourceName.String, row.SourceKey.String), group)
	approval.Branches = make([]workflow.PendingApprovalBranch, 0, len(branches))
	for _, row := range branches {
		branch, err := pendingApprovalBranchFromRow(row.TransitionBranchKey, row.EdgeID)
		if err != nil {
			return workflow.PendingApproval{}, err
		}
		choice := branch.choice()
		if group != nil && row.TargetNodeID != nil {
			if !row.TargetKey.Valid {
				return workflow.PendingApproval{}, errors.New("pending approval target node is missing")
			}
			targetNodeID, err := runtimeids.GraphEntityIDText(row.TargetNodeID)
			if err != nil {
				return workflow.PendingApproval{}, err
			}
			choice.Target, err = branch.target(
				facts.Source, len(branches), workflow.NodeID(targetNodeID), workflow.NodeKind(row.TargetKind.String),
				workflow.DisplayLabel(row.TargetName.String, row.TargetKey.String),
			)
			if err != nil {
				return workflow.PendingApproval{}, err
			}
		}
		approval.Branches = append(approval.Branches, choice)
	}
	return approval, nil
}
