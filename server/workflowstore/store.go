package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"builder/server/metadata"
	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
	"builder/server/workflowjson"
	"github.com/google/uuid"
)

type Store struct {
	metadata     *metadata.Store
	db           *sql.DB
	queries      *sqlitegen.Queries
	roleResolver workflow.RoleResolver
	now          func() time.Time
	eventMu      sync.RWMutex
	eventSink    WorkflowEventPublisher
}

type Option func(*Store)

func WithRoleResolver(resolver workflow.RoleResolver) Option {
	return func(s *Store) {
		s.roleResolver = resolver
	}
}

func WithNow(now func() time.Time) Option {
	return func(s *Store) {
		if now != nil {
			s.now = now
		}
	}
}

func New(metadataStore *metadata.Store, opts ...Option) (*Store, error) {
	if metadataStore == nil || metadataStore.DB() == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	store := &Store{
		metadata:  metadataStore,
		db:        metadataStore.DB(),
		queries:   metadataStore.Queries(),
		now:       func() time.Time { return time.Now().UTC() },
		eventSink: noopWorkflowEventPublisher{},
	}
	for _, opt := range opts {
		opt(store)
	}
	return store, nil
}

type WorkflowRecord struct {
	ID          workflow.WorkflowID
	Name        string
	Description string
	Version     int64
}

type NodeRecord struct {
	ID             workflow.NodeID
	WorkflowID     workflow.WorkflowID
	Key            workflow.ModelKey
	Kind           workflow.NodeKind
	DisplayName    string
	GroupID        string
	GroupKey       string
	SubagentRole   string
	PromptTemplate string
	OutputFields   []workflow.OutputField
}

type NodeGroupRecord struct {
	ID          string
	WorkflowID  workflow.WorkflowID
	Key         workflow.ModelKey
	DisplayName string
	SortOrder   int64
}

type WorkflowEventRecord struct {
	ProjectID        string
	WorkflowID       string
	Resource         string
	Action           string
	ChangedIDs       []string
	OccurredAtUnixMs int64
}

type WorkflowEventPublisher interface {
	PublishWorkflowEvent(context.Context, WorkflowEventRecord) error
}

type noopWorkflowEventPublisher struct{}

func (noopWorkflowEventPublisher) PublishWorkflowEvent(context.Context, WorkflowEventRecord) error {
	return nil
}

type TransitionGroupRecord struct {
	ID           workflow.TransitionGroupID
	WorkflowID   workflow.WorkflowID
	SourceNodeID workflow.NodeID
	TransitionID workflow.TransitionID
	DisplayName  string
}

type EdgeRecord struct {
	ID                 workflow.EdgeID
	WorkflowID         workflow.WorkflowID
	TransitionGroupID  workflow.TransitionGroupID
	Key                workflow.ModelKey
	TargetNodeID       workflow.NodeID
	RequiresApproval   bool
	ContextMode        workflow.ContextMode
	ContextSource      workflow.ContextSource
	InputBindings      []workflow.InputBinding
	OutputRequirements []workflow.OutputRequirement
}

type ProjectWorkflowLinkRecord struct {
	ID         string
	ProjectID  string
	WorkflowID workflow.WorkflowID
	IsDefault  bool
}

type ProjectWorkflowUnlinkResult struct {
	LinkID     string
	ProjectID  string
	WorkflowID workflow.WorkflowID
	Unlinked   bool
	Blockers   []ProjectWorkflowUnlinkBlocker
}

type ProjectWorkflowUnlinkBlocker struct {
	Code    string
	Message string
	Count   int
	Tasks   []ProjectWorkflowUnlinkTaskReference
}

type ProjectWorkflowUnlinkTaskReference struct {
	TaskID  workflow.TaskID
	ShortID string
	Title   string
}

type TaskRecord struct {
	ID                workflow.TaskID
	ProjectID         string
	WorkflowID        workflow.WorkflowID
	LinkID            string
	ShortID           string
	Title             string
	Body              string
	SourceURL         string
	SourceWorkspaceID string
	ManagedWorktreeID string
	CanceledAt        int64
	CancelReason      string
	Version           int64
}

type PlacementRecord struct {
	ID     workflow.PlacementID
	TaskID workflow.TaskID
	NodeID workflow.NodeID
	State  string
}

type RunRecord struct {
	ID                    workflow.RunID
	TaskID                workflow.TaskID
	PlacementID           workflow.PlacementID
	NodeID                workflow.NodeID
	SessionID             string
	Generation            int64
	AutomationRequestedAt int64
	StartedAt             int64
	CompletedAt           int64
	InterruptedAt         int64
	InterruptionReason    string
	WaitingAskID          string
	FinalAnswerViolations int64
	InvalidCompletions    int64
}

type RunnableRunRecord struct {
	RunRecord
	WorkflowRevisionSeen int64
}

type RunStartContext struct {
	Run               RunRecord
	Task              TaskRecord
	Workflow          WorkflowRecord
	Node              NodeRecord
	ContextMode       workflow.ContextMode
	SourceRunID       workflow.RunID
	SourceSessionID   string
	SourceNode        NodeRecord
	TransitionIDs     []string
	TransitionOptions []TransitionOption
	InputValues       map[string]string
	WorkspaceID       string
	WorkspaceRoot     string
	WorktreeID        string
	WorktreeRoot      string
}

type TransitionOption struct {
	ID          string
	DisplayName string
	Description string
}

type TransitionRecord struct {
	ID           workflow.TransitionID
	TaskID       workflow.TaskID
	TransitionID string
	State        string
	Commentary   string
	OutputValues map[string]string
	CreatedAt    int64
}

type TransitionEdgeRecord struct {
	ID                   string
	TaskTransitionID     workflow.TransitionID
	WorkflowEdgeID       workflow.EdgeID
	EdgeKey              string
	TargetNodeID         workflow.NodeID
	TargetPlacementID    workflow.PlacementID
	State                string
	WorkflowRevisionSeen int64
}

type CommentRecord struct {
	ID        string
	TaskID    workflow.TaskID
	Body      string
	Author    string
	AuthorID  string
	CreatedAt int64
	UpdatedAt int64
}

type CreateWorkflowRequest struct {
	Name        string
	Description string
}

type CreateAndLinkWorkflowRequest struct {
	Name          string
	Description   string
	ProjectID     string
	DefaultPolicy WorkflowLinkDefaultPolicy
}

type WorkflowLinkDefaultPolicy string

const (
	WorkflowLinkDefaultNever            WorkflowLinkDefaultPolicy = "never"
	WorkflowLinkDefaultAlways           WorkflowLinkDefaultPolicy = "always"
	WorkflowLinkDefaultIfProjectHasNone WorkflowLinkDefaultPolicy = "if_project_has_none"
)

type ListWorkflowsRequest struct {
	PageSize  int
	PageToken string
	Query     string
}

type ListWorkflowsResult struct {
	Workflows     []WorkflowRecord
	NextPageToken string
}

const (
	defaultWorkflowListPageSize = 50
	maxWorkflowListPageSize     = 100
)

func (s *Store) CreateWorkflow(ctx context.Context, req CreateWorkflowRequest) (WorkflowRecord, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return WorkflowRecord{}, errors.New("workflow name is required")
	}
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkflowRecord{}, fmt.Errorf("begin workflow create tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	record, err := insertWorkflow(ctx, q, now, CreateWorkflowRequest{Name: name, Description: req.Description})
	if err != nil {
		return WorkflowRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkflowRecord{}, fmt.Errorf("commit workflow create tx: %w", err)
	}
	return record, nil
}

func (s *Store) CreateAndLinkWorkflow(ctx context.Context, req CreateAndLinkWorkflowRequest) (WorkflowRecord, ProjectWorkflowLinkRecord, error) {
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkflowRecord{}, ProjectWorkflowLinkRecord{}, fmt.Errorf("begin workflow create-and-link tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	record, err := insertWorkflow(ctx, q, now, CreateWorkflowRequest{Name: req.Name, Description: req.Description})
	if err != nil {
		return WorkflowRecord{}, ProjectWorkflowLinkRecord{}, err
	}
	link, err := s.linkWorkflowInTx(ctx, tx, q, now, strings.TrimSpace(req.ProjectID), record.ID, req.DefaultPolicy)
	if err != nil {
		return WorkflowRecord{}, ProjectWorkflowLinkRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkflowRecord{}, ProjectWorkflowLinkRecord{}, fmt.Errorf("commit workflow create-and-link tx: %w", err)
	}
	return record, link, nil
}

func insertWorkflow(ctx context.Context, q *sqlitegen.Queries, now int64, req CreateWorkflowRequest) (WorkflowRecord, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return WorkflowRecord{}, errors.New("workflow name is required")
	}
	description := strings.TrimSpace(req.Description)
	workflowID := prefixedID("workflow")
	startID := prefixedID("node")
	doneID := prefixedID("node")
	if err := q.InsertWorkflow(ctx, sqlitegen.InsertWorkflowParams{ID: workflowID, Name: name, Description: description, Version: 1, CreatedAtUnixMs: now, UpdatedAtUnixMs: now}); err != nil {
		return WorkflowRecord{}, fmt.Errorf("insert workflow: %w", err)
	}
	if err := q.InsertWorkflowNode(ctx, sqlitegen.InsertWorkflowNodeParams{ID: startID, WorkflowID: workflowID, NodeKey: "backlog", Kind: string(workflow.NodeKindStart), DisplayName: "Backlog", OutputFieldsJson: "[]", SortOrder: 0}); err != nil {
		return WorkflowRecord{}, fmt.Errorf("insert backlog node: %w", err)
	}
	if err := q.InsertWorkflowNode(ctx, sqlitegen.InsertWorkflowNodeParams{ID: doneID, WorkflowID: workflowID, NodeKey: "done", Kind: string(workflow.NodeKindTerminal), DisplayName: "Done", OutputFieldsJson: "[]", SortOrder: 1000}); err != nil {
		return WorkflowRecord{}, fmt.Errorf("insert done node: %w", err)
	}
	return WorkflowRecord{ID: workflow.WorkflowID(workflowID), Name: name, Description: description, Version: 1}, nil
}

func (s *Store) UpdateWorkflowInfo(ctx context.Context, workflowID workflow.WorkflowID, name string, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("workflow name is required")
	}
	updated, err := s.queries.UpdateWorkflowInfo(ctx, sqlitegen.UpdateWorkflowInfoParams{ID: string(workflowID), Name: name, Description: strings.TrimSpace(description), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return fmt.Errorf("update workflow info: %w", err)
	}
	if updated == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListWorkflows(ctx context.Context, req ListWorkflowsRequest) (ListWorkflowsResult, error) {
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = defaultWorkflowListPageSize
	}
	if pageSize > maxWorkflowListPageSize {
		pageSize = maxWorkflowListPageSize
	}
	offset := 0
	if strings.TrimSpace(req.PageToken) != "" {
		parsed, err := strconv.Atoi(req.PageToken)
		if err != nil || parsed < 0 {
			return ListWorkflowsResult{}, fmt.Errorf("invalid workflow list page token")
		}
		offset = parsed
	}
	query := strings.TrimSpace(req.Query)
	args := []any{}
	where := ""
	if query != "" {
		where = "WHERE lower(name) LIKE ? OR lower(description) LIKE ?"
		like := "%" + strings.ToLower(query) + "%"
		args = append(args, like, like)
	}
	args = append(args, pageSize+1, offset)
	rows, err := s.db.QueryContext(ctx, `
SELECT
    id,
    name,
    description,
    version,
    created_at_unix_ms,
    updated_at_unix_ms
FROM workflows
`+where+`
ORDER BY lower(name) ASC, id ASC
LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return ListWorkflowsResult{}, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]WorkflowRecord, 0, pageSize)
	for rows.Next() {
		var row workflowRecordRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Description, &row.Version, &row.CreatedAtUnixMs, &row.UpdatedAtUnixMs); err != nil {
			return ListWorkflowsResult{}, err
		}
		out = append(out, workflowRecordFromRow(row))
	}
	if err := rows.Err(); err != nil {
		return ListWorkflowsResult{}, err
	}
	nextPageToken := ""
	if len(out) > pageSize {
		out = out[:pageSize]
		nextPageToken = strconv.Itoa(offset + pageSize)
	}
	return ListWorkflowsResult{Workflows: out, NextPageToken: nextPageToken}, nil
}

func (s *Store) AddNode(ctx context.Context, node NodeRecord) (int64, error) {
	if strings.TrimSpace(string(node.WorkflowID)) == "" {
		return 0, errors.New("workflow id is required")
	}
	outputFields, err := marshalJSON(node.OutputFields)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if node.ID == "" {
		node.ID = workflow.NodeID(prefixedID("node"))
	}
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, node.WorkflowID)
	if err != nil {
		return 0, err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, node.WorkflowID, withWorkflowGraphNode(currentGraph, node)); err != nil {
		return 0, err
	}
	groupID, err := resolveWorkflowNodeGroupID(ctx, q, string(node.WorkflowID), node.GroupID, node.GroupKey)
	if err != nil {
		return 0, err
	}
	if err := q.InsertWorkflowNode(ctx, sqlitegen.InsertWorkflowNodeParams{
		ID:               string(node.ID),
		WorkflowID:       string(node.WorkflowID),
		NodeKey:          string(node.Key),
		Kind:             string(node.Kind),
		DisplayName:      strings.TrimSpace(node.DisplayName),
		SubagentRole:     strings.TrimSpace(node.SubagentRole),
		PromptTemplate:   strings.TrimSpace(node.PromptTemplate),
		OutputFieldsJson: outputFields,
		GroupID:          nullableString(groupID),
		SortOrder:        100,
	}); err != nil {
		return 0, fmt.Errorf("insert workflow node: %w", err)
	}
	revision, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: string(node.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment workflow version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) UpdateNode(ctx context.Context, node NodeRecord) (int64, error) {
	if strings.TrimSpace(string(node.ID)) == "" {
		return 0, errors.New("node id is required")
	}
	if strings.TrimSpace(string(node.WorkflowID)) == "" {
		return 0, errors.New("workflow id is required")
	}
	outputFields, err := marshalJSON(node.OutputFields)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, node.WorkflowID)
	if err != nil {
		return 0, err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, node.WorkflowID, withWorkflowGraphNode(currentGraph, node)); err != nil {
		return 0, err
	}
	groupID, err := resolveWorkflowNodeGroupID(ctx, q, string(node.WorkflowID), node.GroupID, node.GroupKey)
	if err != nil {
		return 0, err
	}
	updated, err := tx.ExecContext(ctx, `
UPDATE workflow_nodes
SET
    node_key = ?,
    kind = ?,
    display_name = ?,
    subagent_role = ?,
    prompt_template = ?,
    output_fields_json = ?,
    group_id = ?
WHERE id = ?
  AND workflow_id = ?`,
		string(node.Key),
		string(node.Kind),
		strings.TrimSpace(node.DisplayName),
		strings.TrimSpace(node.SubagentRole),
		strings.TrimSpace(node.PromptTemplate),
		outputFields,
		nullableString(groupID),
		string(node.ID),
		string(node.WorkflowID),
	)
	if err != nil {
		return 0, fmt.Errorf("update workflow node: %w", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, sql.ErrNoRows
	}
	revision, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: string(node.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment workflow version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) AddNodeGroup(ctx context.Context, group NodeGroupRecord) (NodeGroupRecord, int64, error) {
	if strings.TrimSpace(string(group.WorkflowID)) == "" {
		return NodeGroupRecord{}, 0, errors.New("workflow id is required")
	}
	if strings.TrimSpace(string(group.Key)) == "" {
		return NodeGroupRecord{}, 0, errors.New("group key is required")
	}
	if strings.TrimSpace(group.DisplayName) == "" {
		return NodeGroupRecord{}, 0, errors.New("group display name is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return NodeGroupRecord{}, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if strings.TrimSpace(group.ID) == "" {
		group.ID = prefixedID("workflow-node-group")
	}
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, group.WorkflowID)
	if err != nil {
		return NodeGroupRecord{}, 0, err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, group.WorkflowID, withWorkflowGraphNodeGroup(currentGraph, group)); err != nil {
		return NodeGroupRecord{}, 0, err
	}
	if err := q.InsertWorkflowNodeGroup(ctx, sqlitegen.InsertWorkflowNodeGroupParams{ID: group.ID, WorkflowID: string(group.WorkflowID), GroupKey: string(group.Key), DisplayName: strings.TrimSpace(group.DisplayName), SortOrder: group.SortOrder}); err != nil {
		return NodeGroupRecord{}, 0, fmt.Errorf("insert workflow node group: %w", err)
	}
	revision, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: string(group.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return NodeGroupRecord{}, 0, fmt.Errorf("increment workflow version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return NodeGroupRecord{}, 0, err
	}
	return group, revision, nil
}

func (s *Store) UpdateNodeGroup(ctx context.Context, group NodeGroupRecord) (NodeGroupRecord, int64, error) {
	if strings.TrimSpace(group.ID) == "" {
		return NodeGroupRecord{}, 0, errors.New("group id is required")
	}
	if strings.TrimSpace(string(group.WorkflowID)) == "" {
		return NodeGroupRecord{}, 0, errors.New("workflow id is required")
	}
	if strings.TrimSpace(string(group.Key)) == "" {
		return NodeGroupRecord{}, 0, errors.New("group key is required")
	}
	if strings.TrimSpace(group.DisplayName) == "" {
		return NodeGroupRecord{}, 0, errors.New("group display name is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return NodeGroupRecord{}, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, group.WorkflowID)
	if err != nil {
		return NodeGroupRecord{}, 0, err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, group.WorkflowID, withWorkflowGraphNodeGroup(currentGraph, group)); err != nil {
		return NodeGroupRecord{}, 0, err
	}
	updated, err := q.UpdateWorkflowNodeGroup(ctx, sqlitegen.UpdateWorkflowNodeGroupParams{ID: group.ID, WorkflowID: string(group.WorkflowID), GroupKey: string(group.Key), DisplayName: strings.TrimSpace(group.DisplayName), SortOrder: group.SortOrder})
	if err != nil {
		return NodeGroupRecord{}, 0, fmt.Errorf("update workflow node group: %w", err)
	}
	if updated != 1 {
		return NodeGroupRecord{}, 0, sql.ErrNoRows
	}
	revision, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: string(group.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return NodeGroupRecord{}, 0, fmt.Errorf("increment workflow version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return NodeGroupRecord{}, 0, err
	}
	return group, revision, nil
}

func (s *Store) DeleteNodeGroup(ctx context.Context, workflowID workflow.WorkflowID, groupID string) (int64, error) {
	if strings.TrimSpace(string(workflowID)) == "" {
		return 0, errors.New("workflow id is required")
	}
	if strings.TrimSpace(groupID) == "" {
		return 0, errors.New("group id is required")
	}
	nodeCount, err := s.queries.CountWorkflowNodesByGroup(ctx, nullableString(groupID))
	if err != nil {
		return 0, err
	}
	if nodeCount > 0 {
		return 0, errors.New("workflow node group is in use")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, workflowID)
	if err != nil {
		return 0, err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, workflowID, withoutWorkflowGraphNodeGroup(currentGraph, strings.TrimSpace(groupID))); err != nil {
		return 0, err
	}
	deleted, err := q.DeleteWorkflowNodeGroup(ctx, sqlitegen.DeleteWorkflowNodeGroupParams{ID: strings.TrimSpace(groupID), WorkflowID: string(workflowID)})
	if err != nil {
		return 0, fmt.Errorf("delete workflow node group: %w", err)
	}
	if deleted != 1 {
		return 0, sql.ErrNoRows
	}
	revision, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: string(workflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment workflow version: %w", err)
	}
	return revision, tx.Commit()
}

func (s *Store) SetWorkflowEventPublisher(publisher WorkflowEventPublisher) {
	if s == nil {
		return
	}
	s.eventMu.Lock()
	if publisher == nil {
		publisher = noopWorkflowEventPublisher{}
	}
	s.eventSink = publisher
	s.eventMu.Unlock()
}

func (s *Store) PublishWorkflowEvent(ctx context.Context, event WorkflowEventRecord) error {
	if strings.TrimSpace(event.Resource) == "" {
		return errors.New("event resource is required")
	}
	if strings.TrimSpace(event.Action) == "" {
		return errors.New("event action is required")
	}
	occurredAt := event.OccurredAtUnixMs
	if occurredAt == 0 {
		occurredAt = s.now().UnixMilli()
	}
	normalized := WorkflowEventRecord{
		ProjectID:        strings.TrimSpace(event.ProjectID),
		WorkflowID:       strings.TrimSpace(event.WorkflowID),
		Resource:         strings.TrimSpace(event.Resource),
		Action:           strings.TrimSpace(event.Action),
		ChangedIDs:       append([]string(nil), event.ChangedIDs...),
		OccurredAtUnixMs: occurredAt,
	}
	s.eventMu.RLock()
	sink := s.eventSink
	s.eventMu.RUnlock()
	return sink.PublishWorkflowEvent(ctx, normalized)
}

func (s *Store) AddTransitionGroup(ctx context.Context, group TransitionGroupRecord) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if group.ID == "" {
		group.ID = workflow.TransitionGroupID(prefixedID("group"))
	}
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, group.WorkflowID)
	if err != nil {
		return 0, err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, group.WorkflowID, withWorkflowGraphTransitionGroup(currentGraph, group)); err != nil {
		return 0, err
	}
	if err := ensureWorkflowNodeID(ctx, q, string(group.WorkflowID), group.SourceNodeID); err != nil {
		return 0, err
	}
	if err := q.InsertWorkflowTransitionGroup(ctx, sqlitegen.InsertWorkflowTransitionGroupParams{ID: string(group.ID), SourceNodeID: string(group.SourceNodeID), TransitionID: strings.TrimSpace(string(group.TransitionID)), DisplayName: strings.TrimSpace(group.DisplayName), SortOrder: 100}); err != nil {
		return 0, fmt.Errorf("insert transition group: %w", err)
	}
	revision, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: string(group.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment workflow version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) UpdateTransitionGroup(ctx context.Context, group TransitionGroupRecord) (int64, error) {
	if strings.TrimSpace(string(group.ID)) == "" {
		return 0, errors.New("transition group id is required")
	}
	if strings.TrimSpace(string(group.WorkflowID)) == "" {
		return 0, errors.New("workflow id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, group.WorkflowID)
	if err != nil {
		return 0, err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, group.WorkflowID, withWorkflowGraphTransitionGroup(currentGraph, group)); err != nil {
		return 0, err
	}
	if err := ensureWorkflowNodeID(ctx, q, string(group.WorkflowID), group.SourceNodeID); err != nil {
		return 0, err
	}
	updated, err := tx.ExecContext(ctx, `
UPDATE workflow_transition_groups
SET
    source_node_id = ?,
    transition_id = ?,
    display_name = ?
WHERE id = ?
  AND EXISTS (
      SELECT 1
      FROM workflow_nodes source
      WHERE source.id = workflow_transition_groups.source_node_id
        AND source.workflow_id = ?
  )`,
		string(group.SourceNodeID),
		strings.TrimSpace(string(group.TransitionID)),
		strings.TrimSpace(group.DisplayName),
		string(group.ID),
		string(group.WorkflowID),
	)
	if err != nil {
		return 0, fmt.Errorf("update transition group: %w", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, sql.ErrNoRows
	}
	revision, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: string(group.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment workflow version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) AddEdge(ctx context.Context, edge EdgeRecord) (int64, error) {
	contextSource := workflow.CanonicalContextSource(edge.ContextSource)
	inputs, err := marshalJSON(edge.InputBindings)
	if err != nil {
		return 0, err
	}
	requirements, err := marshalJSON(edge.OutputRequirements)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if edge.ID == "" {
		edge.ID = workflow.EdgeID(prefixedID("edge"))
	}
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, edge.WorkflowID)
	if err != nil {
		return 0, err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, edge.WorkflowID, withWorkflowGraphEdge(currentGraph, edge)); err != nil {
		return 0, err
	}
	if err := ensureWorkflowTransitionGroupID(ctx, tx, string(edge.WorkflowID), edge.TransitionGroupID); err != nil {
		return 0, err
	}
	if err := ensureWorkflowNodeID(ctx, q, string(edge.WorkflowID), edge.TargetNodeID); err != nil {
		return 0, err
	}
	if err := q.InsertWorkflowEdge(ctx, sqlitegen.InsertWorkflowEdgeParams{ID: string(edge.ID), TransitionGroupID: string(edge.TransitionGroupID), EdgeKey: string(edge.Key), TargetNodeID: string(edge.TargetNodeID), RequiresApproval: boolToInt64(edge.RequiresApproval), ContextMode: string(edge.ContextMode), ContextSourceKind: string(contextSource.Kind), ContextSourceNodeKey: string(contextSource.NodeKey), InputBindingsJson: inputs, OutputRequirementsJson: requirements, SortOrder: 100}); err != nil {
		return 0, fmt.Errorf("insert workflow edge: %w", err)
	}
	revision, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: string(edge.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment workflow version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) UpdateEdge(ctx context.Context, edge EdgeRecord) (int64, error) {
	if strings.TrimSpace(string(edge.ID)) == "" {
		return 0, errors.New("edge id is required")
	}
	if strings.TrimSpace(string(edge.WorkflowID)) == "" {
		return 0, errors.New("workflow id is required")
	}
	contextSource := workflow.CanonicalContextSource(edge.ContextSource)
	inputs, err := marshalJSON(edge.InputBindings)
	if err != nil {
		return 0, err
	}
	requirements, err := marshalJSON(edge.OutputRequirements)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, edge.WorkflowID)
	if err != nil {
		return 0, err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, edge.WorkflowID, withWorkflowGraphEdge(currentGraph, edge)); err != nil {
		return 0, err
	}
	if err := ensureWorkflowTransitionGroupID(ctx, tx, string(edge.WorkflowID), edge.TransitionGroupID); err != nil {
		return 0, err
	}
	if err := ensureWorkflowNodeID(ctx, q, string(edge.WorkflowID), edge.TargetNodeID); err != nil {
		return 0, err
	}
	updated, err := tx.ExecContext(ctx, `
UPDATE workflow_edges
SET
    transition_group_id = ?,
    edge_key = ?,
    target_node_id = ?,
    requires_approval = ?,
    context_mode = ?,
    context_source_kind = ?,
    context_source_node_key = ?,
    input_bindings_json = ?,
    output_requirements_json = ?
WHERE id = ?
  AND EXISTS (
      SELECT 1
      FROM workflow_transition_groups tg
      JOIN workflow_nodes source ON source.id = tg.source_node_id
      WHERE tg.id = workflow_edges.transition_group_id
        AND source.workflow_id = ?
  )`,
		string(edge.TransitionGroupID),
		string(edge.Key),
		string(edge.TargetNodeID),
		boolToInt64(edge.RequiresApproval),
		string(edge.ContextMode),
		string(contextSource.Kind),
		string(contextSource.NodeKey),
		inputs,
		requirements,
		string(edge.ID),
		string(edge.WorkflowID),
	)
	if err != nil {
		return 0, fmt.Errorf("update workflow edge: %w", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, sql.ErrNoRows
	}
	revision, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: string(edge.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment workflow version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) DeleteNode(ctx context.Context, nodeID workflow.NodeID) error {
	if strings.TrimSpace(string(nodeID)) == "" {
		return errors.New("node id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	node, err := q.GetWorkflowNode(ctx, string(nodeID))
	if err != nil {
		return err
	}
	workflowID := workflow.WorkflowID(node.WorkflowID)
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, workflowID)
	if err != nil {
		return err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, workflowID, withoutWorkflowGraphNode(currentGraph, nodeID)); err != nil {
		return err
	}
	refs, err := q.CountTaskNodeReferences(ctx, string(nodeID))
	if err != nil {
		return err
	}
	if refs > 0 {
		return fmt.Errorf("workflow node has task history references")
	}
	if deleted, err := q.DeleteWorkflowNode(ctx, string(nodeID)); err != nil {
		return fmt.Errorf("delete workflow node: %w", err)
	} else if deleted != 1 {
		return sql.ErrNoRows
	}
	if _, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: node.WorkflowID, UpdatedAtUnixMs: s.now().UnixMilli()}); err != nil {
		return fmt.Errorf("increment workflow version: %w", err)
	}
	return tx.Commit()
}

func (s *Store) DeleteEdge(ctx context.Context, edgeID workflow.EdgeID) error {
	if strings.TrimSpace(string(edgeID)) == "" {
		return errors.New("edge id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	edge, err := q.GetWorkflowEdge(ctx, string(edgeID))
	if err != nil {
		return err
	}
	workflowID := workflow.WorkflowID(edge.WorkflowID)
	currentGraph, err := currentWorkflowGraphSavePrepared(ctx, q, workflowID)
	if err != nil {
		return err
	}
	if err := enforceWorkflowGraphEditPolicy(ctx, tx, q, workflowID, withoutWorkflowGraphEdge(currentGraph, edgeID)); err != nil {
		return err
	}
	refs, err := q.CountTaskEdgeReferences(ctx, sql.NullString{String: string(edgeID), Valid: true})
	if err != nil {
		return err
	}
	if refs > 0 {
		return fmt.Errorf("workflow edge has task history references")
	}
	if deleted, err := q.DeleteWorkflowEdge(ctx, string(edgeID)); err != nil {
		return fmt.Errorf("delete workflow edge: %w", err)
	} else if deleted != 1 {
		return sql.ErrNoRows
	}
	if _, err := q.IncrementWorkflowVersion(ctx, sqlitegen.IncrementWorkflowVersionParams{ID: edge.WorkflowID, UpdatedAtUnixMs: s.now().UnixMilli()}); err != nil {
		return fmt.Errorf("increment workflow version: %w", err)
	}
	return tx.Commit()
}

func (s *Store) GetDefinition(ctx context.Context, workflowID workflow.WorkflowID) (workflow.Definition, WorkflowRecord, error) {
	row, err := s.queries.GetWorkflow(ctx, string(workflowID))
	if err != nil {
		return workflow.Definition{}, WorkflowRecord{}, err
	}
	nodes, err := s.queries.ListWorkflowNodes(ctx, string(workflowID))
	if err != nil {
		return workflow.Definition{}, WorkflowRecord{}, err
	}
	groups, err := s.queries.ListWorkflowTransitionGroups(ctx, string(workflowID))
	if err != nil {
		return workflow.Definition{}, WorkflowRecord{}, err
	}
	edges, err := s.queries.ListWorkflowEdges(ctx, string(workflowID))
	if err != nil {
		return workflow.Definition{}, WorkflowRecord{}, err
	}
	def := workflow.Definition{ID: workflow.WorkflowID(row.ID), DisplayName: row.Name}
	for _, node := range nodes {
		outputFields := []workflow.OutputField{}
		if err := unmarshalJSON(node.OutputFieldsJson, &outputFields); err != nil {
			return workflow.Definition{}, WorkflowRecord{}, err
		}
		def.Nodes = append(def.Nodes, workflow.Node{WorkflowID: workflow.WorkflowID(node.WorkflowID), ID: workflow.NodeID(node.ID), Key: workflow.ModelKey(node.NodeKey), DisplayName: node.DisplayName, Kind: workflow.NodeKind(node.Kind), SubagentRole: node.SubagentRole, PromptTemplate: node.PromptTemplate, OutputFields: outputFields})
	}
	for _, group := range groups {
		def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{WorkflowID: workflow.WorkflowID(group.WorkflowID), ID: workflow.TransitionGroupID(group.ID), SourceNodeID: workflow.NodeID(group.SourceNodeID), TransitionID: workflow.TransitionID(group.TransitionID), DisplayName: group.DisplayName})
	}
	for _, edge := range edges {
		inputs := []workflow.InputBinding{}
		requirements := []workflow.OutputRequirement{}
		if err := unmarshalJSON(edge.InputBindingsJson, &inputs); err != nil {
			return workflow.Definition{}, WorkflowRecord{}, err
		}
		if err := unmarshalJSON(edge.OutputRequirementsJson, &requirements); err != nil {
			return workflow.Definition{}, WorkflowRecord{}, err
		}
		def.Edges = append(def.Edges, workflow.Edge{WorkflowID: workflow.WorkflowID(edge.WorkflowID), ID: workflow.EdgeID(edge.ID), Key: workflow.ModelKey(edge.EdgeKey), TransitionGroupID: workflow.TransitionGroupID(edge.TransitionGroupID), TargetNodeID: workflow.NodeID(edge.TargetNodeID), RequiresApproval: edge.RequiresApproval != 0, ContextMode: workflow.ContextMode(edge.ContextMode), ContextSource: workflow.CanonicalContextSource(workflow.ContextSource{Kind: workflow.ContextSourceKind(edge.ContextSourceKind), NodeKey: workflow.ModelKey(edge.ContextSourceNodeKey)}), InputBindings: inputs, OutputRequirements: requirements})
	}
	return def, workflowRecordFromGetWorkflowRow(row), nil
}

type workflowRecordRow struct {
	ID              string
	Name            string
	Description     string
	Version         int64
	CreatedAtUnixMs int64
	UpdatedAtUnixMs int64
}

func workflowRecordFromRow(row workflowRecordRow) WorkflowRecord {
	return WorkflowRecord{ID: workflow.WorkflowID(row.ID), Name: row.Name, Description: row.Description, Version: row.Version}
}

func workflowRecordFromGetWorkflowRow(row sqlitegen.Workflow) WorkflowRecord {
	return workflowRecordFromRow(workflowRecordRow{ID: row.ID, Name: row.Name, Description: row.Description, Version: row.Version, CreatedAtUnixMs: row.CreatedAtUnixMs, UpdatedAtUnixMs: row.UpdatedAtUnixMs})
}

func nodeGroupRecordFromRow(row sqlitegen.WorkflowNodeGroup) NodeGroupRecord {
	return NodeGroupRecord{ID: row.ID, WorkflowID: workflow.WorkflowID(row.WorkflowID), Key: workflow.ModelKey(row.GroupKey), DisplayName: row.DisplayName, SortOrder: row.SortOrder}
}

func resolveWorkflowNodeGroupID(ctx context.Context, q *sqlitegen.Queries, workflowID string, groupID string, groupKey string) (string, error) {
	trimmedGroupID := strings.TrimSpace(groupID)
	if trimmedGroupID != "" {
		row, err := q.GetWorkflowNodeGroupByID(ctx, trimmedGroupID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", fmt.Errorf("workflow node group %q not found", trimmedGroupID)
			}
			return "", err
		}
		if row.WorkflowID != strings.TrimSpace(workflowID) {
			return "", fmt.Errorf("workflow node group %q belongs to workflow %q", trimmedGroupID, row.WorkflowID)
		}
		return trimmedGroupID, nil
	}
	trimmedGroupKey := strings.TrimSpace(groupKey)
	if trimmedGroupKey == "" {
		return "", nil
	}
	row, err := q.GetWorkflowNodeGroupByKey(ctx, sqlitegen.GetWorkflowNodeGroupByKeyParams{WorkflowID: strings.TrimSpace(workflowID), GroupKey: trimmedGroupKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("workflow node group %q not found", trimmedGroupKey)
		}
		return "", err
	}
	return row.ID, nil
}

func ensureWorkflowNodeID(ctx context.Context, q *sqlitegen.Queries, workflowID string, nodeID workflow.NodeID) error {
	trimmedNodeID := strings.TrimSpace(string(nodeID))
	if trimmedNodeID == "" {
		return errors.New("workflow node id is required")
	}
	row, err := q.GetWorkflowNode(ctx, trimmedNodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("workflow node %q not found: %w", trimmedNodeID, sql.ErrNoRows)
		}
		return fmt.Errorf("resolve workflow node %q: %w", trimmedNodeID, err)
	}
	if row.WorkflowID != strings.TrimSpace(workflowID) {
		return fmt.Errorf("workflow node %q belongs to workflow %q, not %q", trimmedNodeID, row.WorkflowID, strings.TrimSpace(workflowID))
	}
	return nil
}

func ensureWorkflowTransitionGroupID(ctx context.Context, tx *sql.Tx, workflowID string, groupID workflow.TransitionGroupID) error {
	trimmedGroupID := strings.TrimSpace(string(groupID))
	if trimmedGroupID == "" {
		return errors.New("workflow transition group id is required")
	}
	var rowWorkflowID string
	err := tx.QueryRowContext(ctx, `
SELECT source.workflow_id
FROM workflow_transition_groups tg
JOIN workflow_nodes source ON source.id = tg.source_node_id
WHERE tg.id = ?
LIMIT 1`, trimmedGroupID).Scan(&rowWorkflowID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("workflow transition group %q not found: %w", trimmedGroupID, sql.ErrNoRows)
		}
		return fmt.Errorf("resolve workflow transition group %q: %w", trimmedGroupID, err)
	}
	if rowWorkflowID != strings.TrimSpace(workflowID) {
		return fmt.Errorf("workflow transition group %q belongs to workflow %q, not %q", trimmedGroupID, rowWorkflowID, strings.TrimSpace(workflowID))
	}
	return nil
}

func prefixedID(prefix string) string {
	return prefix + "-" + uuid.NewString()
}

func nullableString(value string) sql.NullString {
	trimmed := strings.TrimSpace(value)
	return sql.NullString{String: trimmed, Valid: trimmed != ""}
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func marshalJSON(value any) (string, error) {
	return workflowjson.MarshalString(value)
}

func unmarshalJSON(raw string, target any) error {
	return workflowjson.UnmarshalString(raw, target)
}
