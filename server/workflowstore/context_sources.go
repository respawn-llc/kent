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
	ContextMode            string                       `json:"context_mode"`
	ContextSource          workflow.ContextSource       `json:"context_source,omitempty"`
	SourceRunID            string                       `json:"source_run_id,omitempty"`
	SourceSessionID        string                       `json:"source_session_id,omitempty"`
	NodeOutputValues       map[string]map[string]string `json:"node_output_values,omitempty"`
	TargetRunStartSnapshot *runStartSnapshot            `json:"target_run_start_snapshot,omitempty"`
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

func (s *Store) resolvePromptNodeOutputValues(ctx context.Context, tx *sql.Tx, taskID string, beforeUnixMs int64, snapshot runStartSnapshot) (map[string]map[string]string, error) {
	refs, err := workflow.ExtractPromptTemplateReferences(snapshot.Node.PromptTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse prompt node references: %w", err)
	}
	if len(refs.Invalid) > 0 {
		return nil, fmt.Errorf("prompt node references are invalid: %s", refs.Invalid[0].Message)
	}
	if len(refs.NodeOutputs) == 0 {
		return nil, nil
	}
	out := map[string]map[string]string{}
	for _, ref := range refs.NodeOutputs {
		source, ok := snapshot.nodeByKey(ref.NodeKey)
		if !ok {
			return nil, fmt.Errorf("prompt node reference %s missing from run snapshot", workflow.PromptReferenceDescription(ref))
		}
		value, err := latestNodeOutputValue(ctx, tx, taskID, string(source.ID), strings.TrimSpace(ref.FieldName), beforeUnixMs)
		if err != nil {
			return nil, err
		}
		nodeKey := strings.TrimSpace(string(ref.NodeKey))
		if out[nodeKey] == nil {
			out[nodeKey] = map[string]string{}
		}
		out[nodeKey][strings.TrimSpace(ref.FieldName)] = value
	}
	return out, nil
}

func latestNodeOutputValue(ctx context.Context, tx *sql.Tx, taskID string, nodeID string, fieldName string, beforeUnixMs int64) (string, error) {
	var outputValuesJSON string
	err := tx.QueryRowContext(ctx, `
SELECT tr.output_values_json
FROM task_runs r
JOIN task_node_placements p ON p.id = r.placement_id
JOIN task_transitions tr ON tr.source_run_id = r.id
WHERE p.task_id = ?
  AND p.node_id = ?
  AND r.completed_at_unix_ms > 0
  AND r.completed_at_unix_ms <= ?
  AND tr.state != 'rejected'
ORDER BY r.completed_at_unix_ms DESC, tr.created_at_unix_ms DESC, r.rowid DESC
LIMIT 1`, taskID, nodeID, beforeUnixMs).Scan(&outputValuesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("prompt node reference source node %q has no completed output for task", nodeID)
	}
	if err != nil {
		return "", err
	}
	outputValues := map[string]string{}
	if err := unmarshalJSON(outputValuesJSON, &outputValues); err != nil {
		return "", err
	}
	value := outputValues[fieldName]
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("prompt node reference source node %q missing output %q", nodeID, fieldName)
	}
	return value, nil
}

func targetRunMetadata(edge edgeContractSnapshot, source resolvedContextSourceRun, nodeOutputValues map[string]map[string]string) (string, error) {
	return marshalJSON(workflowRunMetadata{
		ContextMode:      string(edge.ContextMode),
		ContextSource:    workflow.CanonicalContextSource(edge.ContextSource),
		SourceRunID:      source.runID,
		SourceSessionID:  source.sessionID,
		NodeOutputValues: nodeOutputValues,
	})
}
