package workflowstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"builder/server/metadata"
	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
	"github.com/google/uuid"
)

type Store struct {
	metadata     *metadata.Store
	db           *sql.DB
	queries      *sqlitegen.Queries
	roleResolver workflow.RoleResolver
	now          func() time.Time
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
		metadata: metadataStore,
		db:       metadataStore.DB(),
		queries:  metadataStore.Queries(),
		now:      func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(store)
	}
	return store, nil
}

type WorkflowRecord struct {
	ID            workflow.WorkflowID
	Name          string
	Description   string
	GraphRevision int64
}

type NodeRecord struct {
	ID             workflow.NodeID
	WorkflowID     workflow.WorkflowID
	Key            workflow.ModelKey
	Kind           workflow.NodeKind
	DisplayName    string
	SubagentRole   string
	PromptTemplate string
	OutputFields   []workflow.OutputField
}

type TransitionGroupRecord struct {
	ID           workflow.TransitionGroupID
	WorkflowID   workflow.WorkflowID
	SourceNodeID workflow.NodeID
	TransitionID string
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
	InputBindings      []workflow.InputBinding
	OutputRequirements []workflow.OutputRequirement
}

type ProjectWorkflowLinkRecord struct {
	ID               string
	ProjectID        string
	WorkflowID       workflow.WorkflowID
	IsDefault        bool
	UnlinkedAtUnixMs int64
}

type TaskRecord struct {
	ID            workflow.TaskID
	ProjectID     string
	WorkflowID    workflow.WorkflowID
	LinkID        string
	ShortID       string
	Title         string
	Body          string
	SourceURL     string
	CanceledAt    int64
	CancelReason  string
	GraphRevision int64
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
}

type RunnableRunRecord struct {
	RunRecord
	WorkflowRevisionSeen int64
}

type TransitionRecord struct {
	ID           workflow.TransitionID
	TaskID       workflow.TaskID
	TransitionID string
	State        string
	Commentary   string
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
	DeletedAt int64
	UpdatedAt int64
}

type CreateWorkflowRequest struct {
	Name        string
	Description string
}

func (s *Store) CreateWorkflow(ctx context.Context, req CreateWorkflowRequest) (WorkflowRecord, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return WorkflowRecord{}, errors.New("workflow name is required")
	}
	now := s.now().UnixMilli()
	workflowID := prefixedID("workflow")
	startID := prefixedID("node")
	doneID := prefixedID("node")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkflowRecord{}, fmt.Errorf("begin workflow create tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if err := q.InsertWorkflow(ctx, sqlitegen.InsertWorkflowParams{ID: workflowID, Name: name, Description: strings.TrimSpace(req.Description), GraphRevision: 1, CreatedAtUnixMs: now, UpdatedAtUnixMs: now, MetadataJson: "{}"}); err != nil {
		return WorkflowRecord{}, fmt.Errorf("insert workflow: %w", err)
	}
	if err := q.InsertWorkflowNode(ctx, sqlitegen.InsertWorkflowNodeParams{ID: startID, WorkflowID: workflowID, NodeKey: "backlog", Kind: string(workflow.NodeKindStart), DisplayName: "Backlog", OutputFieldsJson: "[]", SortOrder: 0, MetadataJson: "{}"}); err != nil {
		return WorkflowRecord{}, fmt.Errorf("insert backlog node: %w", err)
	}
	if err := q.InsertWorkflowNode(ctx, sqlitegen.InsertWorkflowNodeParams{ID: doneID, WorkflowID: workflowID, NodeKey: "done", Kind: string(workflow.NodeKindTerminal), DisplayName: "Done", OutputFieldsJson: "[]", SortOrder: 1000, MetadataJson: "{}"}); err != nil {
		return WorkflowRecord{}, fmt.Errorf("insert done node: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return WorkflowRecord{}, fmt.Errorf("commit workflow create tx: %w", err)
	}
	return WorkflowRecord{ID: workflow.WorkflowID(workflowID), Name: name, Description: strings.TrimSpace(req.Description), GraphRevision: 1}, nil
}

func (s *Store) UpdateWorkflowInfo(ctx context.Context, workflowID workflow.WorkflowID, name string, description string) error {
	updated, err := s.queries.UpdateWorkflowInfo(ctx, sqlitegen.UpdateWorkflowInfoParams{ID: string(workflowID), Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return fmt.Errorf("update workflow info: %w", err)
	}
	if updated == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListWorkflows(ctx context.Context) ([]WorkflowRecord, error) {
	rows, err := s.queries.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, workflowRecordFromRow(row))
	}
	return out, nil
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
	if err := q.InsertWorkflowNode(ctx, sqlitegen.InsertWorkflowNodeParams{
		ID:               string(node.ID),
		WorkflowID:       string(node.WorkflowID),
		NodeKey:          string(node.Key),
		Kind:             string(node.Kind),
		DisplayName:      strings.TrimSpace(node.DisplayName),
		SubagentRole:     strings.TrimSpace(node.SubagentRole),
		PromptTemplate:   strings.TrimSpace(node.PromptTemplate),
		OutputFieldsJson: outputFields,
		SortOrder:        100,
		MetadataJson:     "{}",
	}); err != nil {
		return 0, fmt.Errorf("insert workflow node: %w", err)
	}
	revision, err := q.IncrementWorkflowGraphRevision(ctx, sqlitegen.IncrementWorkflowGraphRevisionParams{ID: string(node.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment graph revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
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
	if err := q.InsertWorkflowTransitionGroup(ctx, sqlitegen.InsertWorkflowTransitionGroupParams{ID: string(group.ID), WorkflowID: string(group.WorkflowID), SourceNodeID: string(group.SourceNodeID), TransitionID: strings.TrimSpace(group.TransitionID), DisplayName: strings.TrimSpace(group.DisplayName), SortOrder: 100, MetadataJson: "{}"}); err != nil {
		return 0, fmt.Errorf("insert transition group: %w", err)
	}
	revision, err := q.IncrementWorkflowGraphRevision(ctx, sqlitegen.IncrementWorkflowGraphRevisionParams{ID: string(group.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment graph revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) AddEdge(ctx context.Context, edge EdgeRecord) (int64, error) {
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
	if err := q.InsertWorkflowEdge(ctx, sqlitegen.InsertWorkflowEdgeParams{ID: string(edge.ID), WorkflowID: string(edge.WorkflowID), TransitionGroupID: string(edge.TransitionGroupID), EdgeKey: string(edge.Key), TargetNodeID: string(edge.TargetNodeID), RequiresApproval: boolToInt64(edge.RequiresApproval), ContextMode: string(edge.ContextMode), InputBindingsJson: inputs, OutputRequirementsJson: requirements, SortOrder: 100, MetadataJson: "{}"}); err != nil {
		return 0, fmt.Errorf("insert workflow edge: %w", err)
	}
	revision, err := q.IncrementWorkflowGraphRevision(ctx, sqlitegen.IncrementWorkflowGraphRevisionParams{ID: string(edge.WorkflowID), UpdatedAtUnixMs: s.now().UnixMilli()})
	if err != nil {
		return 0, fmt.Errorf("increment graph revision: %w", err)
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
	node, err := s.queries.GetWorkflowNode(ctx, string(nodeID))
	if err != nil {
		return err
	}
	nonTerminalRefs, err := s.queries.CountNonTerminalTasksByWorkflow(ctx, node.WorkflowID)
	if err != nil {
		return err
	}
	if nonTerminalRefs > 0 {
		return fmt.Errorf("workflow has non-terminal task references")
	}
	refs, err := s.queries.CountTaskNodeReferences(ctx, string(nodeID))
	if err != nil {
		return err
	}
	if refs > 0 {
		return fmt.Errorf("workflow node has task history references")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if deleted, err := q.DeleteWorkflowNode(ctx, string(nodeID)); err != nil {
		return fmt.Errorf("delete workflow node: %w", err)
	} else if deleted != 1 {
		return sql.ErrNoRows
	}
	if _, err := q.IncrementWorkflowGraphRevision(ctx, sqlitegen.IncrementWorkflowGraphRevisionParams{ID: node.WorkflowID, UpdatedAtUnixMs: s.now().UnixMilli()}); err != nil {
		return fmt.Errorf("increment graph revision: %w", err)
	}
	return tx.Commit()
}

func (s *Store) ArchiveNode(ctx context.Context, nodeID workflow.NodeID) error {
	if strings.TrimSpace(string(nodeID)) == "" {
		return errors.New("node id is required")
	}
	node, err := s.queries.GetWorkflowNode(ctx, string(nodeID))
	if err != nil {
		return err
	}
	nonTerminalRefs, err := s.queries.CountNonTerminalTasksByWorkflow(ctx, node.WorkflowID)
	if err != nil {
		return err
	}
	if nonTerminalRefs > 0 {
		return fmt.Errorf("workflow has non-terminal task references")
	}
	now := s.now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if archived, err := q.ArchiveWorkflowNode(ctx, sqlitegen.ArchiveWorkflowNodeParams{ID: string(nodeID), ArchivedAtUnixMs: now}); err != nil {
		return fmt.Errorf("archive workflow node: %w", err)
	} else if archived != 1 {
		return sql.ErrNoRows
	}
	if _, err := q.IncrementWorkflowGraphRevision(ctx, sqlitegen.IncrementWorkflowGraphRevisionParams{ID: node.WorkflowID, UpdatedAtUnixMs: now}); err != nil {
		return fmt.Errorf("increment graph revision: %w", err)
	}
	return tx.Commit()
}

func (s *Store) DeleteEdge(ctx context.Context, edgeID workflow.EdgeID) error {
	if strings.TrimSpace(string(edgeID)) == "" {
		return errors.New("edge id is required")
	}
	edge, err := s.queries.GetWorkflowEdge(ctx, string(edgeID))
	if err != nil {
		return err
	}
	nonTerminalRefs, err := s.queries.CountNonTerminalTasksByWorkflow(ctx, edge.WorkflowID)
	if err != nil {
		return err
	}
	if nonTerminalRefs > 0 {
		return fmt.Errorf("workflow has non-terminal task references")
	}
	refs, err := s.queries.CountTaskEdgeReferences(ctx, sql.NullString{String: string(edgeID), Valid: true})
	if err != nil {
		return err
	}
	if refs > 0 {
		return fmt.Errorf("workflow edge has task history references")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if deleted, err := q.DeleteWorkflowEdge(ctx, string(edgeID)); err != nil {
		return fmt.Errorf("delete workflow edge: %w", err)
	} else if deleted != 1 {
		return sql.ErrNoRows
	}
	if _, err := q.IncrementWorkflowGraphRevision(ctx, sqlitegen.IncrementWorkflowGraphRevisionParams{ID: edge.WorkflowID, UpdatedAtUnixMs: s.now().UnixMilli()}); err != nil {
		return fmt.Errorf("increment graph revision: %w", err)
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
		def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{WorkflowID: workflow.WorkflowID(group.WorkflowID), ID: workflow.TransitionGroupID(group.ID), SourceNodeID: workflow.NodeID(group.SourceNodeID), TransitionID: group.TransitionID, DisplayName: group.DisplayName})
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
		def.Edges = append(def.Edges, workflow.Edge{WorkflowID: workflow.WorkflowID(edge.WorkflowID), ID: workflow.EdgeID(edge.ID), Key: workflow.ModelKey(edge.EdgeKey), TransitionGroupID: workflow.TransitionGroupID(edge.TransitionGroupID), TargetNodeID: workflow.NodeID(edge.TargetNodeID), RequiresApproval: edge.RequiresApproval != 0, ContextMode: workflow.ContextMode(edge.ContextMode), InputBindings: inputs, OutputRequirements: requirements})
	}
	return def, workflowRecordFromRow(row), nil
}

func workflowRecordFromRow(row sqlitegen.Workflow) WorkflowRecord {
	return WorkflowRecord{ID: workflow.WorkflowID(row.ID), Name: row.Name, Description: row.Description, GraphRevision: row.GraphRevision}
}

func prefixedID(prefix string) string {
	return prefix + "-" + uuid.NewString()
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func marshalJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalJSON(raw string, target any) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode workflow JSON: %w", err)
	}
	return nil
}
