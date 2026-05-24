package workflowstore

import (
	"context"
	"database/sql"
	"strings"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
)

type WorkflowGraphEditPolicyImpact struct {
	ActiveNodePlacementCount             int64
	PendingApprovalCount                 int64
	ActiveRunCount                       int64
	RunnableRunCount                     int64
	StartNodeChangeCount                 int64
	LastTerminalChangeCount              int64
	TaskReferencedNodeKindChangeCount    int64
	TaskReferencedNodeKindChangeRefCount int64
}

type WorkflowGraphEditPolicyBlocker struct {
	Code    string
	Message string
	Count   int64
}

type WorkflowGraphEditPolicyResult struct {
	Impact   WorkflowGraphEditPolicyImpact
	Blockers []WorkflowGraphEditPolicyBlocker
}

type WorkflowGraphEditPolicyError struct {
	Blockers []WorkflowGraphEditPolicyBlocker
}

func (e WorkflowGraphEditPolicyError) Error() string {
	if len(e.Blockers) == 0 {
		return "workflow graph edit blocked"
	}
	messages := make([]string, 0, len(e.Blockers))
	for _, blocker := range e.Blockers {
		messages = append(messages, blocker.Message)
	}
	return strings.Join(messages, "; ")
}

type workflowGraphEditPolicyQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func enforceWorkflowGraphEditPolicy(ctx context.Context, db workflowGraphEditPolicyQuerier, q *sqlitegen.Queries, workflowID workflow.WorkflowID, prepared preparedWorkflowGraphSave) error {
	result, err := workflowGraphEditPolicy(ctx, db, q, workflowID, prepared)
	if err != nil {
		return err
	}
	if len(result.Blockers) > 0 {
		return WorkflowGraphEditPolicyError{Blockers: result.Blockers}
	}
	return nil
}

func workflowGraphEditPolicy(ctx context.Context, db workflowGraphEditPolicyQuerier, q *sqlitegen.Queries, workflowID workflow.WorkflowID, prepared preparedWorkflowGraphSave) (WorkflowGraphEditPolicyResult, error) {
	activeImpact, err := workflowGraphActiveWorkPolicyImpact(ctx, db, workflowID)
	if err != nil {
		return WorkflowGraphEditPolicyResult{}, err
	}
	structuralImpact, err := workflowGraphStructuralPolicyImpact(ctx, q, workflowID, prepared)
	if err != nil {
		return WorkflowGraphEditPolicyResult{}, err
	}
	impact := WorkflowGraphEditPolicyImpact{
		ActiveNodePlacementCount:             activeImpact.ActiveNodePlacementCount,
		PendingApprovalCount:                 activeImpact.PendingApprovalCount,
		ActiveRunCount:                       activeImpact.ActiveRunCount,
		RunnableRunCount:                     activeImpact.RunnableRunCount,
		StartNodeChangeCount:                 structuralImpact.StartNodeChangeCount,
		LastTerminalChangeCount:              structuralImpact.LastTerminalChangeCount,
		TaskReferencedNodeKindChangeCount:    structuralImpact.TaskReferencedNodeKindChangeCount,
		TaskReferencedNodeKindChangeRefCount: structuralImpact.TaskReferencedNodeKindChangeRefCount,
	}
	return WorkflowGraphEditPolicyResult{Impact: impact, Blockers: workflowGraphEditPolicyBlockers(impact)}, nil
}

func workflowGraphActiveWorkPolicyImpact(ctx context.Context, db workflowGraphEditPolicyQuerier, workflowID workflow.WorkflowID) (WorkflowGraphEditPolicyImpact, error) {
	activePlacements, err := workflowGraphEditPolicyCount(ctx, db, `
SELECT CAST(COUNT(DISTINCT p.id) AS INTEGER)
FROM task_records t
JOIN task_node_placements p ON p.task_id = t.id AND p.state IN ('active', 'waiting_approval')
JOIN workflow_nodes n ON n.id = p.node_id
WHERE t.workflow_id = ?
  AND t.canceled_at_unix_ms = 0
  AND n.kind NOT IN ('start', 'terminal')`, string(workflowID))
	if err != nil {
		return WorkflowGraphEditPolicyImpact{}, err
	}
	pendingApprovals, err := workflowGraphEditPolicyCount(ctx, db, `
SELECT CAST(COUNT(DISTINCT tt.id) AS INTEGER)
FROM task_transition_records tt
JOIN task_records t ON t.id = tt.task_id
WHERE t.workflow_id = ?
  AND t.canceled_at_unix_ms = 0
  AND tt.state = 'pending_approval'`, string(workflowID))
	if err != nil {
		return WorkflowGraphEditPolicyImpact{}, err
	}
	activeRuns, err := workflowGraphEditPolicyCount(ctx, db, `
SELECT CAST(COUNT(DISTINCT r.id) AS INTEGER)
FROM task_run_records r
JOIN task_records t ON t.id = r.task_id
JOIN task_node_placements p ON p.id = r.placement_id
JOIN workflow_nodes n ON n.id = r.node_id
WHERE t.workflow_id = ?
  AND t.canceled_at_unix_ms = 0
  AND r.started_at_unix_ms > 0
  AND r.completed_at_unix_ms = 0
  AND r.interrupted_at_unix_ms = 0
  AND p.state = 'active'
  AND n.kind = 'agent'`, string(workflowID))
	if err != nil {
		return WorkflowGraphEditPolicyImpact{}, err
	}
	runnableRuns, err := workflowGraphEditPolicyCount(ctx, db, `
SELECT CAST(COUNT(DISTINCT r.id) AS INTEGER)
FROM task_run_records r
JOIN task_records t ON t.id = r.task_id
JOIN task_node_placements p ON p.id = r.placement_id
JOIN workflow_nodes n ON n.id = r.node_id
WHERE t.workflow_id = ?
  AND t.canceled_at_unix_ms = 0
  AND r.automation_requested_at_unix_ms > 0
  AND r.started_at_unix_ms = 0
  AND r.completed_at_unix_ms = 0
  AND r.interrupted_at_unix_ms = 0
  AND r.waiting_ask_id = ''
  AND p.state = 'active'
  AND n.kind = 'agent'`, string(workflowID))
	if err != nil {
		return WorkflowGraphEditPolicyImpact{}, err
	}
	return WorkflowGraphEditPolicyImpact{
		ActiveNodePlacementCount: activePlacements,
		PendingApprovalCount:     pendingApprovals,
		ActiveRunCount:           activeRuns,
		RunnableRunCount:         runnableRuns,
	}, nil
}

func workflowGraphStructuralPolicyImpact(ctx context.Context, q *sqlitegen.Queries, workflowID workflow.WorkflowID, prepared preparedWorkflowGraphSave) (WorkflowGraphEditPolicyImpact, error) {
	currentNodes, err := q.ListWorkflowNodes(ctx, string(workflowID))
	if err != nil {
		return WorkflowGraphEditPolicyImpact{}, err
	}
	nextNodes := map[workflow.NodeID]NodeRecord{}
	nextTerminalCount := int64(0)
	for _, node := range prepared.nodes {
		nextNodes[node.ID] = node
		if node.Kind == workflow.NodeKindTerminal {
			nextTerminalCount++
		}
	}
	impact := WorkflowGraphEditPolicyImpact{}
	currentTerminalCount := int64(0)
	for _, current := range currentNodes {
		nodeID := workflow.NodeID(current.ID)
		currentKind := workflow.NodeKind(current.Kind)
		next, exists := nextNodes[nodeID]
		if currentKind == workflow.NodeKindStart && (!exists || next.Kind != workflow.NodeKindStart) {
			impact.StartNodeChangeCount++
		}
		if currentKind == workflow.NodeKindTerminal {
			currentTerminalCount++
		}
		if exists && currentKind != next.Kind {
			refCount, err := q.CountTaskNodeReferences(ctx, current.ID)
			if err != nil {
				return WorkflowGraphEditPolicyImpact{}, err
			}
			if refCount > 0 {
				impact.TaskReferencedNodeKindChangeCount++
				impact.TaskReferencedNodeKindChangeRefCount += refCount
			}
		}
	}
	if currentTerminalCount > 0 && nextTerminalCount == 0 {
		impact.LastTerminalChangeCount = 1
	}
	return impact, nil
}

func workflowGraphEditPolicyCount(ctx context.Context, db workflowGraphEditPolicyQuerier, query string, args ...any) (int64, error) {
	var count int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func workflowGraphEditPolicyBlockers(impact WorkflowGraphEditPolicyImpact) []WorkflowGraphEditPolicyBlocker {
	blockers := []WorkflowGraphEditPolicyBlocker{}
	if impact.ActiveNodePlacementCount > 0 {
		blockers = append(blockers, WorkflowGraphEditPolicyBlocker{Code: "active_node_placements", Message: "Workflow graph changes are blocked while tasks are active outside backlog or terminal nodes.", Count: impact.ActiveNodePlacementCount})
	}
	if impact.PendingApprovalCount > 0 {
		blockers = append(blockers, WorkflowGraphEditPolicyBlocker{Code: "pending_approvals", Message: "Workflow graph changes are blocked while workflow transitions are pending approval.", Count: impact.PendingApprovalCount})
	}
	if impact.ActiveRunCount > 0 {
		blockers = append(blockers, WorkflowGraphEditPolicyBlocker{Code: "active_runs", Message: "Workflow graph changes are blocked while workflow runs are active.", Count: impact.ActiveRunCount})
	}
	if impact.RunnableRunCount > 0 {
		blockers = append(blockers, WorkflowGraphEditPolicyBlocker{Code: "runnable_runs", Message: "Workflow graph changes are blocked while workflow runs are runnable.", Count: impact.RunnableRunCount})
	}
	if impact.StartNodeChangeCount > 0 {
		blockers = append(blockers, WorkflowGraphEditPolicyBlocker{Code: "start_node_changed", Message: "The workflow start node cannot be removed, replaced, or changed to another kind.", Count: impact.StartNodeChangeCount})
	}
	if impact.LastTerminalChangeCount > 0 {
		blockers = append(blockers, WorkflowGraphEditPolicyBlocker{Code: "last_terminal_changed", Message: "Workflow graph changes must leave at least one terminal node.", Count: impact.LastTerminalChangeCount})
	}
	if impact.TaskReferencedNodeKindChangeCount > 0 {
		blockers = append(blockers, WorkflowGraphEditPolicyBlocker{Code: "task_referenced_node_kind_changed", Message: "Workflow node kind changes are blocked for nodes referenced by existing tasks.", Count: impact.TaskReferencedNodeKindChangeRefCount})
	}
	return blockers
}
