package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
)

type CreateTaskRequest struct {
	ProjectID  string
	WorkflowID workflow.WorkflowID
	Title      string
	Body       string
	SourceURL  string
}

type StartTaskResult struct {
	TransitionID string
	PlacementID  workflow.PlacementID
	RunID        workflow.RunID
}

type CompleteRunRequest struct {
	RunID              workflow.RunID
	TransitionID       string
	OutputValues       map[string]string
	Commentary         string
	Actor              string
	ExpectedGeneration int64
	RequireGeneration  bool
}

type CompletionValidationIssue struct {
	Code    string
	Field   string
	Message string
}

type CompletionValidationError struct {
	Issues []CompletionValidationIssue
}

func (e CompletionValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "workflow completion is invalid"
	}
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		if strings.TrimSpace(issue.Field) != "" {
			parts = append(parts, issue.Field+": "+issue.Message)
			continue
		}
		parts = append(parts, issue.Message)
	}
	return "workflow completion is invalid: " + strings.Join(parts, "; ")
}

type ProtocolViolationKind string

const (
	ProtocolViolationFinalAnswer       ProtocolViolationKind = "final_answer"
	ProtocolViolationInvalidCompletion ProtocolViolationKind = "invalid_completion"
)

type RecordProtocolViolationRequest struct {
	RunID              workflow.RunID
	Kind               ProtocolViolationKind
	MaxCount           int
	Detail             string
	ExpectedGeneration int64
	RequireGeneration  bool
}

type RecordProtocolViolationResult struct {
	Count       int64
	Interrupted bool
}

type CompleteRunResult struct {
	TransitionID workflow.TransitionID
	State        string
	PlacementIDs []workflow.PlacementID
	RunIDs       []workflow.RunID
}

type runStartSnapshot struct {
	WorkflowID           workflow.WorkflowID          `json:"workflow_id"`
	WorkflowRevisionSeen int64                        `json:"workflow_revision_seen"`
	Node                 nodeContractSnapshot         `json:"node"`
	TransitionGroups     []transitionContractSnapshot `json:"transition_groups"`
}

type nodeContractSnapshot struct {
	ID             workflow.NodeID        `json:"id"`
	Key            workflow.ModelKey      `json:"key"`
	DisplayName    string                 `json:"display_name"`
	Kind           workflow.NodeKind      `json:"kind"`
	SubagentRole   string                 `json:"subagent_role,omitempty"`
	PromptTemplate string                 `json:"prompt_template,omitempty"`
	OutputFields   []workflow.OutputField `json:"output_fields,omitempty"`
}

type transitionContractSnapshot struct {
	ID           workflow.TransitionGroupID `json:"id"`
	TransitionID string                     `json:"transition_id"`
	DisplayName  string                     `json:"display_name"`
	Edges        []edgeContractSnapshot     `json:"edges"`
}

type edgeContractSnapshot struct {
	ID                 workflow.EdgeID              `json:"id"`
	Key                workflow.ModelKey            `json:"key"`
	TargetNode         nodeContractSnapshot         `json:"target_node"`
	ContextMode        workflow.ContextMode         `json:"context_mode"`
	RequiresApproval   bool                         `json:"requires_approval"`
	InputBindings      []workflow.InputBinding      `json:"input_bindings,omitempty"`
	OutputRequirements []workflow.OutputRequirement `json:"output_requirements,omitempty"`
}

func (s *Store) LinkWorkflow(ctx context.Context, projectID string, workflowID workflow.WorkflowID, isDefault bool) (ProjectWorkflowLinkRecord, error) {
	now := s.now().UnixMilli()
	linkID := prefixedID("workflow-link")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if isDefault {
		if err := q.ClearProjectDefaultWorkflowLinks(ctx, sqlitegen.ClearProjectDefaultWorkflowLinksParams{ProjectID: projectID, UpdatedAtUnixMs: now}); err != nil {
			return ProjectWorkflowLinkRecord{}, err
		}
	}
	if err := q.InsertProjectWorkflowLink(ctx, sqlitegen.InsertProjectWorkflowLinkParams{ID: linkID, ProjectID: projectID, WorkflowID: string(workflowID), IsDefault: boolToInt64(isDefault), CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return ProjectWorkflowLinkRecord{}, fmt.Errorf("insert project workflow link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	return ProjectWorkflowLinkRecord{ID: linkID, ProjectID: projectID, WorkflowID: workflowID, IsDefault: isDefault}, nil
}

func (s *Store) ListProjectWorkflowLinks(ctx context.Context, projectID string) ([]ProjectWorkflowLinkRecord, error) {
	rows, err := s.queries.ListProjectWorkflowLinks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectWorkflowLinkRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, linkRecordFromRow(row))
	}
	return out, nil
}

func (s *Store) SetDefaultProjectWorkflowLink(ctx context.Context, projectID string, workflowID workflow.WorkflowID) (ProjectWorkflowLinkRecord, error) {
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	link, err := q.GetActiveProjectWorkflowLinkByWorkflow(ctx, sqlitegen.GetActiveProjectWorkflowLinkByWorkflowParams{ProjectID: projectID, WorkflowID: string(workflowID)})
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	if err := q.ClearProjectDefaultWorkflowLinks(ctx, sqlitegen.ClearProjectDefaultWorkflowLinksParams{ProjectID: projectID, UpdatedAtUnixMs: now}); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE project_workflow_links SET is_default = 1, updated_at_unix_ms = ? WHERE id = ? AND project_id = ? AND unlinked_at_unix_ms = 0`, now, link.ID, projectID)
	if err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	if count, err := updated.RowsAffected(); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	} else if count != 1 {
		return ProjectWorkflowLinkRecord{}, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return ProjectWorkflowLinkRecord{}, err
	}
	link.IsDefault = 1
	link.UpdatedAtUnixMs = now
	return linkRecordFromRow(link), nil
}

func (s *Store) UnlinkProjectWorkflow(ctx context.Context, linkID string, replacementDefaultLinkID string) error {
	link, err := s.queries.GetProjectWorkflowLink(ctx, linkID)
	if err != nil {
		return err
	}
	nonTerminal, err := s.queries.CountNonTerminalTasksByProjectWorkflowLink(ctx, linkID)
	if err != nil {
		return err
	}
	if nonTerminal > 0 {
		return fmt.Errorf("project workflow link has non-terminal task references")
	}
	activeLinks, err := s.queries.CountActiveProjectWorkflowLinks(ctx, link.ProjectID)
	if err != nil {
		return err
	}
	if link.IsDefault != 0 && activeLinks > 1 && strings.TrimSpace(replacementDefaultLinkID) == "" {
		return fmt.Errorf("replacement default workflow link is required")
	}
	taskRefs, err := s.queries.CountTasksByProjectWorkflowLink(ctx, linkID)
	if err != nil {
		return err
	}
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if taskRefs == 0 {
		if _, err := q.DeleteProjectWorkflowLink(ctx, linkID); err != nil {
			return err
		}
	} else {
		if _, err := q.SoftUnlinkProjectWorkflowLink(ctx, sqlitegen.SoftUnlinkProjectWorkflowLinkParams{ID: linkID, UnlinkedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
			return err
		}
	}
	replacementDefaultLinkID = strings.TrimSpace(replacementDefaultLinkID)
	if replacementDefaultLinkID != "" {
		if err := q.ClearProjectDefaultWorkflowLinks(ctx, sqlitegen.ClearProjectDefaultWorkflowLinksParams{ProjectID: link.ProjectID, UpdatedAtUnixMs: now}); err != nil {
			return err
		}
		updated, err := tx.ExecContext(ctx, `UPDATE project_workflow_links SET is_default = 1, updated_at_unix_ms = ? WHERE id = ? AND project_id = ? AND unlinked_at_unix_ms = 0`, now, replacementDefaultLinkID, link.ProjectID)
		if err != nil {
			return err
		}
		if count, err := updated.RowsAffected(); err != nil {
			return err
		} else if count != 1 {
			return fmt.Errorf("replacement default workflow link is invalid")
		}
	}
	return tx.Commit()
}

func (s *Store) CreateTask(ctx context.Context, req CreateTaskRequest) (TaskRecord, error) {
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	if title == "" {
		return TaskRecord{}, errors.New("task title is required")
	}
	if body == "" {
		return TaskRecord{}, errors.New("task body is required")
	}
	link, err := s.resolveTaskWorkflowLink(ctx, req.ProjectID, req.WorkflowID)
	if err != nil {
		return TaskRecord{}, err
	}
	def, wf, err := s.GetDefinition(ctx, workflow.WorkflowID(link.WorkflowID))
	if err != nil {
		return TaskRecord{}, err
	}
	validation := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextTaskCreation, RoleResolver: s.roleResolver})
	if validation.HasBlockingErrors() {
		return TaskRecord{}, fmt.Errorf("workflow validation failed: %v", validation.Codes())
	}
	startNode, err := startNode(def)
	if err != nil {
		return TaskRecord{}, err
	}
	now := s.now().UnixMilli()
	taskID := prefixedID("task")
	placementID := prefixedID("placement")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	allocated, err := q.AllocateProjectTaskSequence(ctx, sqlitegen.AllocateProjectTaskSequenceParams{ProjectID: req.ProjectID, UpdatedAtUnixMs: now})
	if err != nil {
		return TaskRecord{}, fmt.Errorf("allocate task sequence: %w", err)
	}
	seq := allocated.NextTaskSeq - 1
	shortID := fmt.Sprintf("%s-%d", strings.TrimSpace(allocated.ProjectKey), seq)
	if err := q.InsertTask(ctx, sqlitegen.InsertTaskParams{ID: taskID, ProjectID: req.ProjectID, ProjectWorkflowLinkID: link.ID, WorkflowID: link.WorkflowID, WorkflowRevisionSeen: wf.GraphRevision, TaskSeq: seq, ShortID: shortID, Title: title, Body: body, SourceUrl: strings.TrimSpace(req.SourceURL), ManagedWorktreeID: sql.NullString{}, CreatedAtUnixMs: now, UpdatedAtUnixMs: now, MetadataJson: "{}"}); err != nil {
		return TaskRecord{}, fmt.Errorf("insert task: %w", err)
	}
	if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: placementID, TaskID: taskID, NodeID: string(startNode.ID), State: "active", CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return TaskRecord{}, fmt.Errorf("insert start placement: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TaskRecord{}, err
	}
	return TaskRecord{ID: workflow.TaskID(taskID), ProjectID: req.ProjectID, WorkflowID: workflow.WorkflowID(link.WorkflowID), LinkID: link.ID, ShortID: shortID, Title: title, Body: body, SourceURL: strings.TrimSpace(req.SourceURL), GraphRevision: wf.GraphRevision}, nil
}

func (s *Store) StartTask(ctx context.Context, taskID workflow.TaskID) (StartTaskResult, error) {
	prepared, err := s.prepareTaskStart(ctx, taskID)
	if err != nil {
		return StartTaskResult{}, err
	}
	now := s.now().UnixMilli()
	transitionID := prefixedID("transition")
	targetPlacementID := prefixedID("placement")
	runID := prefixedID("run")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StartTaskResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if _, err := q.UpdateTaskNodePlacementState(ctx, sqlitegen.UpdateTaskNodePlacementStateParams{ID: prepared.startPlacement.ID, State: "completed", UpdatedAtUnixMs: now}); err != nil {
		return StartTaskResult{}, err
	}
	if err := q.InsertTaskTransition(ctx, sqlitegen.InsertTaskTransitionParams{ID: transitionID, TaskID: string(taskID), SourcePlacementID: sql.NullString{String: prepared.startPlacement.ID, Valid: true}, SourceNodeID: sql.NullString{String: string(prepared.start.ID), Valid: true}, SourceNodeKey: string(prepared.start.Key), SourceNodeDisplayName: prepared.start.DisplayName, TransitionGroupID: sql.NullString{String: string(prepared.group.ID), Valid: true}, TransitionID: prepared.group.TransitionID, TransitionDisplayName: prepared.group.DisplayName, WorkflowRevisionSeen: prepared.workflow.GraphRevision, Actor: "system", State: "applied", OutputValuesJson: "{}", CreatedAtUnixMs: now, AppliedAtUnixMs: now}); err != nil {
		return StartTaskResult{}, err
	}
	if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: targetPlacementID, TaskID: string(taskID), NodeID: string(prepared.target.ID), State: "active", CreatedByTransitionID: sql.NullString{String: transitionID, Valid: true}, CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return StartTaskResult{}, err
	}
	if err := q.InsertTaskTransitionEdge(ctx, sqlitegen.InsertTaskTransitionEdgeParams{ID: prefixedID("transition-edge"), TaskTransitionID: transitionID, WorkflowEdgeID: sql.NullString{String: string(prepared.edge.ID), Valid: true}, EdgeKey: string(prepared.edge.Key), WorkflowRevisionSeen: prepared.workflow.GraphRevision, TargetNodeID: sql.NullString{String: string(prepared.target.ID), Valid: true}, TargetNodeKey: string(prepared.target.Key), TargetNodeDisplayName: prepared.target.DisplayName, TargetNodeKind: string(prepared.target.Kind), TargetPlacementID: sql.NullString{String: targetPlacementID, Valid: true}, State: "applied", ContextMode: string(prepared.edge.ContextMode), RequiresApproval: boolToInt64(prepared.edge.RequiresApproval), InputBindingsJson: mustJSON(prepared.edge.InputBindings), OutputRequirementsJson: mustJSON(prepared.edge.OutputRequirements), MetadataJson: "{}"}); err != nil {
		return StartTaskResult{}, err
	}
	runSnapshot, err := newRunStartSnapshot(prepared.definition, prepared.workflow, prepared.target.ID)
	if err != nil {
		return StartTaskResult{}, err
	}
	runSnapshotJSON, err := marshalJSON(runSnapshot)
	if err != nil {
		return StartTaskResult{}, err
	}
	if err := q.InsertTaskRun(ctx, sqlitegen.InsertTaskRunParams{ID: runID, TaskID: string(taskID), PlacementID: targetPlacementID, NodeID: string(prepared.target.ID), WorkflowRevisionSeen: prepared.workflow.GraphRevision, AutomationRequestedAtUnixMs: now, CreatedAtUnixMs: now, UpdatedAtUnixMs: now, InterruptionDetailJson: "{}", RunStartSnapshotJson: runSnapshotJSON, MetadataJson: "{}"}); err != nil {
		return StartTaskResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return StartTaskResult{}, err
	}
	return StartTaskResult{TransitionID: transitionID, PlacementID: workflow.PlacementID(targetPlacementID), RunID: workflow.RunID(runID)}, nil
}

func (s *Store) ValidateTaskStart(ctx context.Context, taskID workflow.TaskID) error {
	_, err := s.prepareTaskStart(ctx, taskID)
	return err
}

type preparedTaskStart struct {
	task           sqlitegen.Task
	definition     workflow.Definition
	workflow       WorkflowRecord
	start          workflow.Node
	group          workflow.TransitionGroup
	edge           workflow.Edge
	target         workflow.Node
	startPlacement sqlitegen.TaskNodePlacement
}

func (s *Store) prepareTaskStart(ctx context.Context, taskID workflow.TaskID) (preparedTaskStart, error) {
	task, err := s.queries.GetTask(ctx, string(taskID))
	if err != nil {
		return preparedTaskStart{}, err
	}
	def, wf, err := s.GetDefinition(ctx, workflow.WorkflowID(task.WorkflowID))
	if err != nil {
		return preparedTaskStart{}, err
	}
	validation := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: workflow.ValidationContextExecution, RoleResolver: s.roleResolver})
	if validation.HasBlockingErrors() {
		return preparedTaskStart{}, fmt.Errorf("workflow validation failed: %v", validation.Codes())
	}
	start, err := startNode(def)
	if err != nil {
		return preparedTaskStart{}, err
	}
	group, edge, target, err := startTransition(def, start.ID)
	if err != nil {
		return preparedTaskStart{}, err
	}
	startPlacement, err := s.queries.GetActiveStartPlacementForTask(ctx, string(taskID))
	if err != nil {
		return preparedTaskStart{}, err
	}
	return preparedTaskStart{task: task, definition: def, workflow: wf, start: start, group: group, edge: edge, target: target, startPlacement: startPlacement}, nil
}

func (s *Store) CompleteRun(ctx context.Context, req CompleteRunRequest) (CompleteRunResult, error) {
	if strings.TrimSpace(string(req.RunID)) == "" {
		return CompleteRunResult{}, errors.New("run id is required")
	}
	if len(req.Commentary) > workflow.MaxCommentaryBytes {
		return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "commentary_too_large", Field: "commentary", Message: "commentary is too large"}}}
	}
	issues := []CompletionValidationIssue{}
	for _, name := range sortedStringKeys(req.OutputValues) {
		value := req.OutputValues[name]
		if strings.TrimSpace(name) == "" {
			issues = append(issues, CompletionValidationIssue{Code: "output_field_required", Message: "output field name is required"})
		}
		if len(value) > workflow.MaxOutputValueBytes {
			issues = append(issues, CompletionValidationIssue{Code: "output_too_large", Field: strings.TrimSpace(name), Message: "output field is too large"})
		}
	}
	if len(issues) > 0 {
		return CompleteRunResult{}, CompletionValidationError{Issues: issues}
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = "agent"
	}
	if actor != "agent" && actor != "user" && actor != "system" {
		return CompleteRunResult{}, fmt.Errorf("unsupported transition actor %q", actor)
	}
	if req.OutputValues == nil {
		req.OutputValues = map[string]string{}
	}
	run, err := s.queries.GetTaskRun(ctx, string(req.RunID))
	if err != nil {
		return CompleteRunResult{}, err
	}
	if run.CompletedAtUnixMs != 0 {
		return CompleteRunResult{}, errors.New("run already completed")
	}
	if run.InterruptedAtUnixMs != 0 {
		return CompleteRunResult{}, errors.New("run already interrupted")
	}
	if req.RequireGeneration && run.RunGeneration != req.ExpectedGeneration {
		return CompleteRunResult{}, fmt.Errorf("stale workflow run generation: got %d want %d", req.ExpectedGeneration, run.RunGeneration)
	}
	snapshot := runStartSnapshot{}
	if err := unmarshalJSON(run.RunStartSnapshotJson, &snapshot); err != nil {
		return CompleteRunResult{}, err
	}
	selectedTransitionID := strings.TrimSpace(req.TransitionID)
	if selectedTransitionID == "" {
		if len(snapshot.TransitionGroups) == 0 {
			return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "no_outgoing_transition", Field: "transition_id", Message: "no outgoing transition is available in run-start snapshot"}}}
		}
		if len(snapshot.TransitionGroups) != 1 {
			return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "transition_id_required", Field: "transition_id", Message: "transition id is required when multiple transitions are available"}}}
		}
		selectedTransitionID = snapshot.TransitionGroups[0].TransitionID
	}
	group, ok := snapshot.transitionByID(selectedTransitionID)
	if !ok {
		return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "invalid_transition_id", Field: "transition_id", Message: fmt.Sprintf("transition %q is not available in run-start snapshot", selectedTransitionID)}}}
	}
	issues = append(issues, knownOutputIssues(snapshot.Node, req.OutputValues)...)
	issues = append(issues, requiredOutputIssues(group, req.OutputValues)...)
	if len(issues) > 0 {
		return CompleteRunResult{}, CompletionValidationError{Issues: issues}
	}
	if group.requiresApproval() {
		return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "approval_execution_unsupported", Field: "transition_id", Message: "approval-gated transitions cannot execute until approval resume is implemented"}}}
	}
	if group.targetsJoin() {
		return CompleteRunResult{}, CompletionValidationError{Issues: []CompletionValidationIssue{{Code: "join_execution_unsupported", Field: "transition_id", Message: "join targets cannot execute until join progression is implemented"}}}
	}
	outputValuesJSON, err := marshalJSON(req.OutputValues)
	if err != nil {
		return CompleteRunResult{}, err
	}
	now := s.now().UnixMilli()
	transitionState := "applied"
	appliedAt := now
	var targetDef workflow.Definition
	var targetWorkflow WorkflowRecord
	targetDef, targetWorkflow, err = s.GetDefinition(ctx, snapshot.WorkflowID)
	if err != nil {
		return CompleteRunResult{}, err
	}
	transitionID := prefixedID("transition")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CompleteRunResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	updatedRun, err := tx.ExecContext(ctx, `
UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    completed_at_unix_ms = ?,
    waiting_ask_id = ''
WHERE id = ?
  AND run_generation = ?
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0`,
		now,
		now,
		run.ID,
		run.RunGeneration,
	)
	if err != nil {
		return CompleteRunResult{}, fmt.Errorf("complete run: %w", err)
	}
	updatedCount, err := updatedRun.RowsAffected()
	if err != nil {
		return CompleteRunResult{}, err
	}
	if updatedCount != 1 {
		return CompleteRunResult{}, sql.ErrNoRows
	}
	if updated, err := q.UpdateTaskNodePlacementState(ctx, sqlitegen.UpdateTaskNodePlacementStateParams{ID: run.PlacementID, State: "completed", UpdatedAtUnixMs: now}); err != nil {
		return CompleteRunResult{}, fmt.Errorf("complete source placement: %w", err)
	} else if updated != 1 {
		return CompleteRunResult{}, sql.ErrNoRows
	}
	if err := q.InsertTaskTransition(ctx, sqlitegen.InsertTaskTransitionParams{ID: transitionID, TaskID: run.TaskID, SourceRunID: sql.NullString{String: run.ID, Valid: true}, SourcePlacementID: sql.NullString{String: run.PlacementID, Valid: true}, SourceNodeID: sql.NullString{String: string(snapshot.Node.ID), Valid: true}, SourceNodeKey: string(snapshot.Node.Key), SourceNodeDisplayName: snapshot.Node.DisplayName, TransitionGroupID: sql.NullString{String: string(group.ID), Valid: true}, TransitionID: group.TransitionID, TransitionDisplayName: group.DisplayName, WorkflowRevisionSeen: snapshot.WorkflowRevisionSeen, Actor: actor, State: transitionState, Commentary: strings.TrimSpace(req.Commentary), OutputValuesJson: outputValuesJSON, CreatedAtUnixMs: now, AppliedAtUnixMs: appliedAt}); err != nil {
		return CompleteRunResult{}, fmt.Errorf("insert completion transition: %w", err)
	}
	result := CompleteRunResult{TransitionID: workflow.TransitionID(transitionID), State: transitionState}
	for _, edge := range group.Edges {
		targetPlacementID := prefixedID("placement")
		if err := q.InsertTaskNodePlacement(ctx, sqlitegen.InsertTaskNodePlacementParams{ID: targetPlacementID, TaskID: run.TaskID, NodeID: string(edge.TargetNode.ID), State: "active", CreatedByTransitionID: sql.NullString{String: transitionID, Valid: true}, ParallelBatchTransitionID: sql.NullString{String: transitionID, Valid: len(group.Edges) > 1}, ParallelBranchEdgeID: sql.NullString{String: string(edge.ID), Valid: true}, CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
			return CompleteRunResult{}, fmt.Errorf("insert target placement: %w", err)
		}
		result.PlacementIDs = append(result.PlacementIDs, workflow.PlacementID(targetPlacementID))
		if err := insertTransitionEdgeSnapshot(ctx, q, transitionID, snapshot.WorkflowRevisionSeen, edge, targetPlacementID, "applied"); err != nil {
			return CompleteRunResult{}, err
		}
		if edge.TargetNode.Kind != workflow.NodeKindAgent {
			continue
		}
		targetRunID := prefixedID("run")
		targetSnapshot, err := newRunStartSnapshot(targetDef, targetWorkflow, edge.TargetNode.ID)
		if err != nil {
			return CompleteRunResult{}, err
		}
		targetSnapshotJSON, err := marshalJSON(targetSnapshot)
		if err != nil {
			return CompleteRunResult{}, err
		}
		if err := q.InsertTaskRun(ctx, sqlitegen.InsertTaskRunParams{ID: targetRunID, TaskID: run.TaskID, PlacementID: targetPlacementID, NodeID: string(edge.TargetNode.ID), WorkflowRevisionSeen: targetWorkflow.GraphRevision, AutomationRequestedAtUnixMs: now, CreatedAtUnixMs: now, UpdatedAtUnixMs: now, InterruptionDetailJson: "{}", RunStartSnapshotJson: targetSnapshotJSON, MetadataJson: "{}"}); err != nil {
			return CompleteRunResult{}, fmt.Errorf("insert target run: %w", err)
		}
		result.RunIDs = append(result.RunIDs, workflow.RunID(targetRunID))
	}
	if err := tx.Commit(); err != nil {
		return CompleteRunResult{}, err
	}
	return result, nil
}

func (s *Store) CancelTask(ctx context.Context, taskID workflow.TaskID, reason string) error {
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if _, err := q.CancelTask(ctx, sqlitegen.CancelTaskParams{ID: string(taskID), CanceledAtUnixMs: now, CancellationReason: strings.TrimSpace(reason), UpdatedAtUnixMs: now}); err != nil {
		return err
	}
	if _, err := q.InterruptActiveTaskRuns(ctx, sqlitegen.InterruptActiveTaskRunsParams{TaskID: string(taskID), UpdatedAtUnixMs: now, InterruptedAtUnixMs: now, InterruptionReason: "task_canceled", InterruptionDetailJson: "{}"}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListPlacements(ctx context.Context, taskID workflow.TaskID) ([]PlacementRecord, error) {
	rows, err := s.queries.ListTaskNodePlacements(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	out := make([]PlacementRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, PlacementRecord{ID: workflow.PlacementID(row.ID), TaskID: workflow.TaskID(row.TaskID), NodeID: workflow.NodeID(row.NodeID), State: row.State})
	}
	return out, nil
}

func (s *Store) ListRuns(ctx context.Context, taskID workflow.TaskID) ([]RunRecord, error) {
	rows, err := s.queries.ListTaskRuns(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	out := make([]RunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, runRecordFromTaskRun(row))
	}
	return out, nil
}

func (s *Store) ListRunnableRuns(ctx context.Context, limit int64) ([]RunnableRunRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.queries.ListRunnableWorkflowRuns(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RunnableRunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, RunnableRunRecord{RunRecord: runRecordFromTaskRun(row), WorkflowRevisionSeen: row.WorkflowRevisionSeen})
	}
	return out, nil
}

func (s *Store) ClaimRun(ctx context.Context, runID workflow.RunID, expectedGeneration int64) (RunnableRunRecord, error) {
	now := s.now().UnixMilli()
	row, err := s.queries.ClaimWorkflowRun(ctx, sqlitegen.ClaimWorkflowRunParams{ID: string(runID), ExpectedGeneration: expectedGeneration, UpdatedAtUnixMs: now, StartedAtUnixMs: now})
	if err != nil {
		return RunnableRunRecord{}, err
	}
	return RunnableRunRecord{RunRecord: runRecordFromTaskRun(row), WorkflowRevisionSeen: row.WorkflowRevisionSeen}, nil
}

func (s *Store) InterruptRun(ctx context.Context, runID workflow.RunID, reason string, detailJSON string) error {
	if strings.TrimSpace(detailJSON) == "" {
		detailJSON = "{}"
	}
	now := s.now().UnixMilli()
	updated, err := s.queries.InterruptWorkflowRun(ctx, sqlitegen.InterruptWorkflowRunParams{ID: string(runID), UpdatedAtUnixMs: now, InterruptedAtUnixMs: now, InterruptionReason: strings.TrimSpace(reason), InterruptionDetailJson: detailJSON})
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) InterruptRunGeneration(ctx context.Context, runID workflow.RunID, generation int64, reason string, detailJSON string) error {
	if strings.TrimSpace(detailJSON) == "" {
		detailJSON = "{}"
	}
	now := s.now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `
UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    interrupted_at_unix_ms = ?,
    interruption_reason = ?,
    interruption_detail_json = ?
WHERE id = ?
  AND run_generation = ?
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0`,
		now,
		now,
		strings.TrimSpace(reason),
		detailJSON,
		string(runID),
		generation,
	)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ReconcileStartedRuns(ctx context.Context, reason string) (int64, error) {
	now := s.now().UnixMilli()
	return s.queries.InterruptStartedWorkflowRunsForRecovery(ctx, sqlitegen.InterruptStartedWorkflowRunsForRecoveryParams{UpdatedAtUnixMs: now, InterruptedAtUnixMs: now, InterruptionReason: strings.TrimSpace(reason), InterruptionDetailJson: "{}"})
}

func (s *Store) ListWaitingAskRuns(ctx context.Context) ([]RunRecord, error) {
	rows, err := s.queries.ListWaitingAskWorkflowRuns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, runRecordFromTaskRun(row))
	}
	return out, nil
}

func (s *Store) GetRunStartContext(ctx context.Context, runID workflow.RunID) (RunStartContext, error) {
	run, err := s.queries.GetTaskRun(ctx, string(runID))
	if err != nil {
		return RunStartContext{}, err
	}
	task, err := s.queries.GetTask(ctx, run.TaskID)
	if err != nil {
		return RunStartContext{}, err
	}
	snapshot := runStartSnapshot{}
	if err := unmarshalJSON(run.RunStartSnapshotJson, &snapshot); err != nil {
		return RunStartContext{}, err
	}
	inputValues, err := s.resolveRunInputValues(ctx, run.PlacementID, taskRecordFromTask(task))
	if err != nil {
		return RunStartContext{}, err
	}
	worktreeID := strings.TrimSpace(task.ManagedWorktreeID.String)
	if worktreeID == "" {
		return RunStartContext{
			Run:           runRecordFromTaskRun(run),
			Task:          taskRecordFromTask(task),
			Node:          nodeRecordFromSnapshot(snapshot.Node, snapshot.WorkflowID),
			TransitionIDs: transitionIDsFromSnapshot(snapshot),
			InputValues:   inputValues,
		}, nil
	}
	worktree, err := s.metadata.GetWorktreeRecordByID(ctx, worktreeID)
	if err != nil {
		return RunStartContext{}, err
	}
	workspace, err := s.metadata.GetWorkspaceByID(ctx, worktree.WorkspaceID)
	if err != nil {
		return RunStartContext{}, err
	}
	return RunStartContext{
		Run:           runRecordFromTaskRun(run),
		Task:          taskRecordFromTask(task),
		Node:          nodeRecordFromSnapshot(snapshot.Node, snapshot.WorkflowID),
		TransitionIDs: transitionIDsFromSnapshot(snapshot),
		InputValues:   inputValues,
		WorkspaceID:   workspace.ID,
		WorkspaceRoot: workspace.CanonicalRootPath,
		WorktreeID:    worktree.ID,
		WorktreeRoot:  worktree.CanonicalRoot,
	}, nil
}

func (s *Store) resolveRunInputValues(ctx context.Context, placementID string, task TaskRecord) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    tr.commentary,
    tr.output_values_json,
    te.input_bindings_json
FROM task_node_placements p
JOIN task_transitions tr ON tr.id = p.created_by_transition_id
JOIN task_transition_edges te
    ON te.task_transition_id = tr.id
    AND te.target_placement_id = p.id
WHERE p.id = ?
ORDER BY te.rowid ASC
LIMIT 1`, placementID)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow run input values: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return map[string]string{}, nil
	}
	var commentary, outputValuesJSON, inputBindingsJSON string
	if err := rows.Scan(&commentary, &outputValuesJSON, &inputBindingsJSON); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	outputValues := map[string]string{}
	if err := unmarshalJSON(outputValuesJSON, &outputValues); err != nil {
		return nil, err
	}
	bindings := []workflow.InputBinding{}
	if err := unmarshalJSON(inputBindingsJSON, &bindings); err != nil {
		return nil, err
	}
	return resolveInputBindingValues(task, commentary, outputValues, bindings)
}

func resolveInputBindingValues(task TaskRecord, commentary string, outputValues map[string]string, bindings []workflow.InputBinding) (map[string]string, error) {
	if len(bindings) == 0 {
		return map[string]string{}, nil
	}
	values := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		name := strings.TrimSpace(binding.Name)
		if name == "" {
			continue
		}
		switch binding.Source {
		case workflow.BindingSourceTask:
			values[name] = taskInputBindingValue(task, binding.Field)
		case workflow.BindingSourceTransitionOutput:
			field := strings.TrimSpace(binding.Field)
			if field == "commentary" {
				values[name] = commentary
			} else {
				values[name] = outputValues[field]
			}
		case workflow.BindingSourceJoin:
			return nil, fmt.Errorf("join-sourced input bindings cannot execute until join aggregation is implemented")
		default:
			return nil, fmt.Errorf("unsupported input binding source %q", binding.Source)
		}
	}
	return values, nil
}

func taskInputBindingValue(task TaskRecord, field string) string {
	switch strings.TrimSpace(field) {
	case "short_id":
		return task.ShortID
	case "title":
		return task.Title
	case "body":
		return task.Body
	case "source_url":
		return task.SourceURL
	default:
		return ""
	}
}

func (s *Store) AttachRunSession(ctx context.Context, runID workflow.RunID, expectedGeneration int64, sessionID string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    session_id = ?
WHERE id = ?
  AND run_generation = ?
  AND started_at_unix_ms > 0
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
  AND session_id IS NULL`,
		s.now().UnixMilli(),
		strings.TrimSpace(sessionID),
		string(runID),
		expectedGeneration,
	)
	if err != nil {
		return fmt.Errorf("attach workflow run session: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListTransitions(ctx context.Context, taskID workflow.TaskID) ([]TransitionRecord, error) {
	rows, err := s.queries.ListTaskTransitions(ctx, string(taskID))
	if err != nil {
		return nil, err
	}
	out := make([]TransitionRecord, 0, len(rows))
	for _, row := range rows {
		outputs := map[string]string{}
		if err := unmarshalJSON(row.OutputValuesJson, &outputs); err != nil {
			return nil, err
		}
		out = append(out, TransitionRecord{ID: workflow.TransitionID(row.ID), TaskID: workflow.TaskID(row.TaskID), TransitionID: row.TransitionID, State: row.State, Commentary: row.Commentary, OutputValues: outputs, CreatedAt: row.CreatedAtUnixMs})
	}
	return out, nil
}

func (s *Store) ListTransitionEdges(ctx context.Context, transitionID workflow.TransitionID) ([]TransitionEdgeRecord, error) {
	rows, err := s.queries.ListTaskTransitionEdges(ctx, string(transitionID))
	if err != nil {
		return nil, err
	}
	out := make([]TransitionEdgeRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, TransitionEdgeRecord{
			ID:                   row.ID,
			TaskTransitionID:     workflow.TransitionID(row.TaskTransitionID),
			WorkflowEdgeID:       workflow.EdgeID(row.WorkflowEdgeID.String),
			EdgeKey:              row.EdgeKey,
			TargetNodeID:         workflow.NodeID(row.TargetNodeID.String),
			TargetPlacementID:    workflow.PlacementID(row.TargetPlacementID.String),
			State:                row.State,
			WorkflowRevisionSeen: row.WorkflowRevisionSeen,
		})
	}
	return out, nil
}

func runRecordFromTaskRun(row sqlitegen.TaskRun) RunRecord {
	return RunRecord{
		ID:                    workflow.RunID(row.ID),
		TaskID:                workflow.TaskID(row.TaskID),
		PlacementID:           workflow.PlacementID(row.PlacementID),
		NodeID:                workflow.NodeID(row.NodeID),
		SessionID:             row.SessionID.String,
		Generation:            row.RunGeneration,
		AutomationRequestedAt: row.AutomationRequestedAtUnixMs,
		StartedAt:             row.StartedAtUnixMs,
		CompletedAt:           row.CompletedAtUnixMs,
		InterruptedAt:         row.InterruptedAtUnixMs,
		InterruptionReason:    row.InterruptionReason,
		WaitingAskID:          row.WaitingAskID,
		FinalAnswerViolations: row.FinalAnswerViolationCount,
		InvalidCompletions:    row.InvalidCompletionCount,
	}
}

func taskRecordFromTask(row sqlitegen.Task) TaskRecord {
	return TaskRecord{
		ID:                workflow.TaskID(row.ID),
		ProjectID:         row.ProjectID,
		WorkflowID:        workflow.WorkflowID(row.WorkflowID),
		LinkID:            row.ProjectWorkflowLinkID,
		ShortID:           row.ShortID,
		Title:             row.Title,
		Body:              row.Body,
		SourceURL:         row.SourceUrl,
		ManagedWorktreeID: strings.TrimSpace(row.ManagedWorktreeID.String),
		CanceledAt:        row.CanceledAtUnixMs,
		CancelReason:      row.CancellationReason,
		GraphRevision:     row.WorkflowRevisionSeen,
	}
}

func nodeRecordFromSnapshot(node nodeContractSnapshot, workflowID workflow.WorkflowID) NodeRecord {
	return NodeRecord{
		ID:             node.ID,
		WorkflowID:     workflowID,
		Key:            node.Key,
		Kind:           node.Kind,
		DisplayName:    node.DisplayName,
		SubagentRole:   node.SubagentRole,
		PromptTemplate: node.PromptTemplate,
		OutputFields:   append([]workflow.OutputField(nil), node.OutputFields...),
	}
}

func transitionIDsFromSnapshot(snapshot runStartSnapshot) []string {
	out := make([]string, 0, len(snapshot.TransitionGroups))
	for _, group := range snapshot.TransitionGroups {
		id := strings.TrimSpace(group.TransitionID)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (s *Store) resolveTaskWorkflowLink(ctx context.Context, projectID string, workflowID workflow.WorkflowID) (sqlitegen.ProjectWorkflowLink, error) {
	if strings.TrimSpace(string(workflowID)) == "" {
		return s.queries.GetDefaultProjectWorkflowLink(ctx, projectID)
	}
	return s.queries.GetActiveProjectWorkflowLinkByWorkflow(ctx, sqlitegen.GetActiveProjectWorkflowLinkByWorkflowParams{ProjectID: projectID, WorkflowID: string(workflowID)})
}

func linkRecordFromRow(row sqlitegen.ProjectWorkflowLink) ProjectWorkflowLinkRecord {
	return ProjectWorkflowLinkRecord{ID: row.ID, ProjectID: row.ProjectID, WorkflowID: workflow.WorkflowID(row.WorkflowID), IsDefault: row.IsDefault != 0, UnlinkedAtUnixMs: row.UnlinkedAtUnixMs}
}

func startNode(def workflow.Definition) (workflow.Node, error) {
	for _, node := range def.Nodes {
		if node.Kind == workflow.NodeKindStart {
			return node, nil
		}
	}
	return workflow.Node{}, errors.New("workflow has no start node")
}

func startTransition(def workflow.Definition, startNodeID workflow.NodeID) (workflow.TransitionGroup, workflow.Edge, workflow.Node, error) {
	var groups []workflow.TransitionGroup
	for _, group := range def.TransitionGroups {
		if group.SourceNodeID == startNodeID {
			groups = append(groups, group)
		}
	}
	if len(groups) != 1 {
		return workflow.TransitionGroup{}, workflow.Edge{}, workflow.Node{}, errors.New("start node must have exactly one transition group")
	}
	var edges []workflow.Edge
	for _, edge := range def.Edges {
		if edge.TransitionGroupID == groups[0].ID {
			edges = append(edges, edge)
		}
	}
	if len(edges) != 1 {
		return workflow.TransitionGroup{}, workflow.Edge{}, workflow.Node{}, errors.New("start transition group must have exactly one edge")
	}
	for _, node := range def.Nodes {
		if node.ID == edges[0].TargetNodeID {
			return groups[0], edges[0], node, nil
		}
	}
	return workflow.TransitionGroup{}, workflow.Edge{}, workflow.Node{}, errors.New("start transition target missing")
}

func mustJSON(value any) string {
	raw, err := marshalJSON(value)
	if err != nil {
		return "null"
	}
	return raw
}

func newRunStartSnapshot(def workflow.Definition, record WorkflowRecord, nodeID workflow.NodeID) (runStartSnapshot, error) {
	nodes := make(map[workflow.NodeID]workflow.Node, len(def.Nodes))
	for _, node := range def.Nodes {
		nodes[node.ID] = node
	}
	node, ok := nodes[nodeID]
	if !ok {
		return runStartSnapshot{}, fmt.Errorf("snapshot node %q missing", nodeID)
	}
	groupsBySource := make(map[workflow.NodeID][]workflow.TransitionGroup, len(def.TransitionGroups))
	for _, group := range def.TransitionGroups {
		groupsBySource[group.SourceNodeID] = append(groupsBySource[group.SourceNodeID], group)
	}
	edgesByGroup := make(map[workflow.TransitionGroupID][]workflow.Edge, len(def.Edges))
	for _, edge := range def.Edges {
		edgesByGroup[edge.TransitionGroupID] = append(edgesByGroup[edge.TransitionGroupID], edge)
	}
	snapshot := runStartSnapshot{
		WorkflowID:           record.ID,
		WorkflowRevisionSeen: record.GraphRevision,
		Node:                 nodeSnapshot(node),
	}
	for _, group := range groupsBySource[nodeID] {
		groupSnapshot := transitionContractSnapshot{ID: group.ID, TransitionID: group.TransitionID, DisplayName: group.DisplayName}
		for _, edge := range edgesByGroup[group.ID] {
			target, ok := nodes[edge.TargetNodeID]
			if !ok {
				return runStartSnapshot{}, fmt.Errorf("snapshot edge target %q missing", edge.TargetNodeID)
			}
			groupSnapshot.Edges = append(groupSnapshot.Edges, edgeContractSnapshot{
				ID:                 edge.ID,
				Key:                edge.Key,
				TargetNode:         nodeSnapshot(target),
				ContextMode:        edge.ContextMode,
				RequiresApproval:   edge.RequiresApproval,
				InputBindings:      edge.InputBindings,
				OutputRequirements: edge.OutputRequirements,
			})
		}
		snapshot.TransitionGroups = append(snapshot.TransitionGroups, groupSnapshot)
	}
	return snapshot, nil
}

func nodeSnapshot(node workflow.Node) nodeContractSnapshot {
	return nodeContractSnapshot{
		ID:             node.ID,
		Key:            node.Key,
		DisplayName:    node.DisplayName,
		Kind:           node.Kind,
		SubagentRole:   node.SubagentRole,
		PromptTemplate: node.PromptTemplate,
		OutputFields:   node.OutputFields,
	}
}

func (s runStartSnapshot) transitionByID(transitionID string) (transitionContractSnapshot, bool) {
	for _, group := range s.TransitionGroups {
		if group.TransitionID == transitionID {
			return group, true
		}
	}
	return transitionContractSnapshot{}, false
}

func (g transitionContractSnapshot) requiresApproval() bool {
	for _, edge := range g.Edges {
		if edge.RequiresApproval {
			return true
		}
	}
	return false
}

func (g transitionContractSnapshot) targetsJoin() bool {
	for _, edge := range g.Edges {
		if edge.TargetNode.Kind == workflow.NodeKindJoin {
			return true
		}
	}
	return false
}

func requiredOutputIssues(group transitionContractSnapshot, values map[string]string) []CompletionValidationIssue {
	issues := []CompletionValidationIssue{}
	for _, edge := range group.Edges {
		for _, requirement := range edge.OutputRequirements {
			if strings.TrimSpace(values[requirement.FieldName]) == "" {
				issues = append(issues, CompletionValidationIssue{Code: "required_output_missing", Field: requirement.FieldName, Message: "required output is missing"})
			}
		}
	}
	return issues
}

func knownOutputIssues(node nodeContractSnapshot, values map[string]string) []CompletionValidationIssue {
	known := make(map[string]bool, len(node.OutputFields))
	for _, field := range node.OutputFields {
		name := strings.TrimSpace(field.Name)
		if name != "" {
			known[name] = true
		}
	}
	issues := []CompletionValidationIssue{}
	for _, name := range sortedStringKeys(values) {
		field := strings.TrimSpace(name)
		if field == "" {
			continue
		}
		if !known[field] {
			issues = append(issues, CompletionValidationIssue{Code: "unknown_output_field", Field: field, Message: "output field is not declared by source node"})
		}
	}
	return issues
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *Store) RecordProtocolViolation(ctx context.Context, req RecordProtocolViolationRequest) (RecordProtocolViolationResult, error) {
	if strings.TrimSpace(string(req.RunID)) == "" {
		return RecordProtocolViolationResult{}, errors.New("run id is required")
	}
	if req.MaxCount <= 0 {
		return RecordProtocolViolationResult{}, errors.New("protocol violation max count must be > 0")
	}
	detail := strings.TrimSpace(req.Detail)
	if detail == "" {
		detail = "{}"
	}
	now := s.now().UnixMilli()
	var count int64
	var interruptedAt int64
	var err error
	switch req.Kind {
	case ProtocolViolationFinalAnswer:
		err = s.db.QueryRowContext(ctx, `
UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    final_answer_violation_count = final_answer_violation_count + 1,
    interrupted_at_unix_ms = CASE WHEN final_answer_violation_count + 1 >= ? THEN ? ELSE interrupted_at_unix_ms END,
    interruption_reason = CASE WHEN final_answer_violation_count + 1 >= ? THEN 'workflow_protocol_violation_limit' ELSE interruption_reason END,
    interruption_detail_json = CASE WHEN final_answer_violation_count + 1 >= ? THEN ? ELSE interruption_detail_json END
WHERE id = ?
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
  AND (? = 0 OR run_generation = ?)
RETURNING final_answer_violation_count, interrupted_at_unix_ms`,
			now, req.MaxCount, now, req.MaxCount, req.MaxCount, detail, string(req.RunID), boolToInt64(req.RequireGeneration), req.ExpectedGeneration,
		).Scan(&count, &interruptedAt)
	case ProtocolViolationInvalidCompletion:
		err = s.db.QueryRowContext(ctx, `
UPDATE task_runs
SET
    updated_at_unix_ms = ?,
    invalid_completion_count = invalid_completion_count + 1,
    interrupted_at_unix_ms = CASE WHEN invalid_completion_count + 1 >= ? THEN ? ELSE interrupted_at_unix_ms END,
    interruption_reason = CASE WHEN invalid_completion_count + 1 >= ? THEN 'workflow_protocol_violation_limit' ELSE interruption_reason END,
    interruption_detail_json = CASE WHEN invalid_completion_count + 1 >= ? THEN ? ELSE interruption_detail_json END
WHERE id = ?
  AND completed_at_unix_ms = 0
  AND interrupted_at_unix_ms = 0
  AND (? = 0 OR run_generation = ?)
RETURNING invalid_completion_count, interrupted_at_unix_ms`,
			now, req.MaxCount, now, req.MaxCount, req.MaxCount, detail, string(req.RunID), boolToInt64(req.RequireGeneration), req.ExpectedGeneration,
		).Scan(&count, &interruptedAt)
	default:
		return RecordProtocolViolationResult{}, fmt.Errorf("unsupported protocol violation kind %q", req.Kind)
	}
	if errors.Is(err, sql.ErrNoRows) {
		run, getErr := s.queries.GetTaskRun(ctx, string(req.RunID))
		if getErr != nil {
			return RecordProtocolViolationResult{}, getErr
		}
		if run.CompletedAtUnixMs != 0 {
			return RecordProtocolViolationResult{Count: protocolViolationCount(run, req.Kind), Interrupted: true}, nil
		}
		if run.InterruptedAtUnixMs != 0 {
			return RecordProtocolViolationResult{Count: protocolViolationCount(run, req.Kind), Interrupted: true}, nil
		}
		if req.RequireGeneration && run.RunGeneration != req.ExpectedGeneration {
			return RecordProtocolViolationResult{}, fmt.Errorf("stale workflow run generation: got %d want %d", req.ExpectedGeneration, run.RunGeneration)
		}
		return RecordProtocolViolationResult{}, sql.ErrNoRows
	}
	if err != nil {
		return RecordProtocolViolationResult{}, err
	}
	return RecordProtocolViolationResult{Count: count, Interrupted: interruptedAt != 0}, nil
}

func protocolViolationCount(run sqlitegen.TaskRun, kind ProtocolViolationKind) int64 {
	switch kind {
	case ProtocolViolationFinalAnswer:
		return run.FinalAnswerViolationCount
	case ProtocolViolationInvalidCompletion:
		return run.InvalidCompletionCount
	default:
		return 0
	}
}

func insertTransitionEdgeSnapshot(ctx context.Context, q *sqlitegen.Queries, transitionID string, revision int64, edge edgeContractSnapshot, targetPlacementID string, state string) error {
	if err := q.InsertTaskTransitionEdge(ctx, sqlitegen.InsertTaskTransitionEdgeParams{
		ID:                     prefixedID("transition-edge"),
		TaskTransitionID:       transitionID,
		WorkflowEdgeID:         sql.NullString{String: string(edge.ID), Valid: edge.ID != ""},
		EdgeKey:                string(edge.Key),
		WorkflowRevisionSeen:   revision,
		TargetNodeID:           sql.NullString{String: string(edge.TargetNode.ID), Valid: edge.TargetNode.ID != ""},
		TargetNodeKey:          string(edge.TargetNode.Key),
		TargetNodeDisplayName:  edge.TargetNode.DisplayName,
		TargetNodeKind:         string(edge.TargetNode.Kind),
		TargetPlacementID:      sql.NullString{String: targetPlacementID, Valid: targetPlacementID != ""},
		State:                  state,
		ContextMode:            string(edge.ContextMode),
		RequiresApproval:       boolToInt64(edge.RequiresApproval),
		InputBindingsJson:      mustJSON(edge.InputBindings),
		OutputRequirementsJson: mustJSON(edge.OutputRequirements),
		MetadataJson:           "{}",
	}); err != nil {
		return fmt.Errorf("insert transition edge snapshot: %w", err)
	}
	return nil
}
