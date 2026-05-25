package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
)

type workflowRunMetadata struct {
	ContextMode            string                 `json:"context_mode"`
	ContextSource          workflow.ContextSource `json:"context_source,omitempty"`
	SourceRunID            string                 `json:"source_run_id,omitempty"`
	SourceSessionID        string                 `json:"source_session_id,omitempty"`
	TargetRunStartSnapshot *runStartSnapshot      `json:"target_run_start_snapshot,omitempty"`
}

type resolvedContextSourceRun struct {
	runID     string
	sessionID string
}

func resolvedContextSourceRunFromMetadata(ctx context.Context, tx *sql.Tx, metadata workflowRunMetadata) (resolvedContextSourceRun, bool, error) {
	runID := strings.TrimSpace(metadata.SourceRunID)
	if runID == "" {
		return resolvedContextSourceRun{}, false, nil
	}
	sessionID := strings.TrimSpace(metadata.SourceSessionID)
	if sessionID == "" {
		run, err := sqlitegen.New(tx).GetTaskRun(ctx, runID)
		if err != nil {
			return resolvedContextSourceRun{}, true, err
		}
		sessionID = strings.TrimSpace(run.SessionID.String)
	}
	return resolvedContextSourceRun{runID: runID, sessionID: sessionID}, true, nil
}

func (s *Store) resolveContextSourceRun(ctx context.Context, tx *sql.Tx, taskID string, beforeUnixMs int64, immediate *sqlitegen.TaskRunRecord, snapshot runStartSnapshot, edge edgeContractSnapshot) (resolvedContextSourceRun, error) {
	source := workflow.CanonicalContextSource(edge.ContextSource)
	switch source.Kind {
	case workflow.ContextSourceImmediateSource:
		if immediate == nil {
			return resolvedContextSourceRun{}, nil
		}
		return resolvedContextSourceRun{runID: immediate.ID, sessionID: strings.TrimSpace(immediate.SessionID.String)}, nil
	case workflow.ContextSourceSelectedNode:
		node, ok := snapshot.nodeByKey(source.NodeKey)
		if !ok {
			return resolvedContextSourceRun{}, fmt.Errorf("selected context source node %q missing from run snapshot", source.NodeKey)
		}
		var runID string
		err := tx.QueryRowContext(ctx, `
SELECT r.id
FROM task_runs r
JOIN task_node_placements p ON p.id = r.placement_id
WHERE p.task_id = ?
  AND p.node_id = ?
  AND r.completed_at_unix_ms > 0
  AND r.completed_at_unix_ms <= ?
ORDER BY r.completed_at_unix_ms DESC, r.rowid DESC
LIMIT 1`, taskID, string(node.ID), beforeUnixMs).Scan(&runID)
		if errors.Is(err, sql.ErrNoRows) {
			return resolvedContextSourceRun{}, fmt.Errorf("selected context source node %q has no completed run for task", source.NodeKey)
		}
		if err != nil {
			return resolvedContextSourceRun{}, err
		}
		run, err := sqlitegen.New(tx).GetTaskRun(ctx, runID)
		if err != nil {
			return resolvedContextSourceRun{}, err
		}
		return resolvedContextSourceRun{runID: run.ID, sessionID: strings.TrimSpace(run.SessionID.String)}, nil
	default:
		return resolvedContextSourceRun{}, fmt.Errorf("context source kind %q is invalid", source.Kind)
	}
}

func targetRunMetadata(edge edgeContractSnapshot, source resolvedContextSourceRun) (string, error) {
	return marshalJSON(workflowRunMetadata{
		ContextMode:     string(edge.ContextMode),
		ContextSource:   workflow.CanonicalContextSource(edge.ContextSource),
		SourceRunID:     source.runID,
		SourceSessionID: source.sessionID,
	})
}
