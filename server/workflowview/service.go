package workflowview

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"builder/server/metadata"
	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
	"builder/server/workflowjson"
	"builder/shared/clientui"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
)

type Service struct {
	metadata    *metadata.Store
	queries     *sqlitegen.Queries
	transcripts SessionTranscriptPageProvider
}

const attentionKindInterruptedRun = "interrupted_run"

type Option func(*Service)

type SessionTranscriptPageProvider interface {
	GetSessionTranscriptPage(ctx context.Context, req serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error)
}

func WithSessionTranscriptProvider(provider SessionTranscriptPageProvider) Option {
	return func(s *Service) {
		s.transcripts = provider
	}
}

func New(metadataStore *metadata.Store, opts ...Option) (*Service, error) {
	if metadataStore == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	svc := &Service{metadata: metadataStore, queries: metadataStore.Queries()}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc, nil
}

func (s *Service) GetDefinition(ctx context.Context, workflowID string) (serverapi.WorkflowDefinition, map[string]workflow.NodeKind, error) {
	if s == nil {
		return serverapi.WorkflowDefinition{}, nil, errors.New("workflow view service is required")
	}
	if strings.TrimSpace(workflowID) == "" {
		return serverapi.WorkflowDefinition{}, nil, errors.New("workflow_id is required")
	}
	return s.definition(ctx, workflowID)
}

func (s *Service) GetBoard(ctx context.Context, req serverapi.WorkflowBoardRequest, roleResolver workflow.RoleResolver) (serverapi.WorkflowBoard, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	if s == nil {
		return serverapi.WorkflowBoard{}, errors.New("workflow view service is required")
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if strings.TrimSpace(projectID) == "" {
		return serverapi.WorkflowBoard{}, errors.New("project_id is required")
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	offset, err := parseOffsetPageToken(req.PageToken)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	donePreviewLimit := req.DonePreviewLimit
	if donePreviewLimit == 0 {
		donePreviewLimit = 20
	}
	links, err := s.queries.ListProjectWorkflowLinks(ctx, projectID)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	tasks, err := s.queries.ListTasksByProject(ctx, projectID)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	project, err := s.metadata.GetProjectOverview(ctx, projectID)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	primaryWorkspace, workspacesByID := boardProjectWorkspaceSummaries(project)
	placementsByTaskID, err := s.boardPlacementsByTask(ctx, tasks)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	workflowIDs := make([]string, 0, len(links)+len(tasks))
	seen := map[string]bool{}
	linkByWorkflowID := map[string]sqlitegen.ProjectWorkflowLinkRecord{}
	for _, link := range links {
		linkByWorkflowID[link.WorkflowID] = preferredProjectWorkflowLink(linkByWorkflowID[link.WorkflowID], link)
		if !seen[link.WorkflowID] {
			workflowIDs = append(workflowIDs, link.WorkflowID)
			seen[link.WorkflowID] = true
		}
	}
	for _, task := range tasks {
		if !seen[task.WorkflowID] {
			workflowIDs = append(workflowIDs, task.WorkflowID)
			seen[task.WorkflowID] = true
		}
	}
	definitions := make(map[string]serverapi.WorkflowDefinition, len(workflowIDs))
	nodeKindsByWorkflowID := make(map[string]map[string]workflow.NodeKind, len(workflowIDs))
	activityByWorkflowID := map[string]int64{}
	for _, task := range tasks {
		if task.UpdatedAtUnixMs > activityByWorkflowID[task.WorkflowID] {
			activityByWorkflowID[task.WorkflowID] = task.UpdatedAtUnixMs
		}
	}
	picker := make([]serverapi.WorkflowPickerItem, 0, len(workflowIDs))
	for _, workflowID := range workflowIDs {
		def, nodeKinds, err := s.definition(ctx, workflowID)
		if err != nil {
			return serverapi.WorkflowBoard{}, err
		}
		definitions[workflowID] = def
		nodeKindsByWorkflowID[workflowID] = nodeKinds
		link := linkByWorkflowID[workflowID]
		validation := workflow.ValidateDefinition(definitionForValidation(def), workflow.ValidationOptions{Context: workflow.ValidationContextExecution, RoleResolver: roleResolver})
		picker = append(picker, serverapi.WorkflowPickerItem{
			WorkflowID:           workflowID,
			DisplayName:          def.Workflow.Name,
			Description:          def.Workflow.Description,
			GraphRevision:        def.Workflow.GraphRevision,
			IsProjectDefault:     link.ID != "" && link.IsDefault != 0,
			ValidForTaskCreation: validation.Valid() && link.ID != "",
			ValidationErrors:     validationErrors(def.Workflow.ID, validation.Errors),
		})
	}
	sort.SliceStable(picker, func(i, j int) bool {
		if picker[i].IsProjectDefault != picker[j].IsProjectDefault {
			return picker[i].IsProjectDefault
		}
		if activityByWorkflowID[picker[i].WorkflowID] != activityByWorkflowID[picker[j].WorkflowID] {
			return activityByWorkflowID[picker[i].WorkflowID] > activityByWorkflowID[picker[j].WorkflowID]
		}
		return strings.ToLower(picker[i].DisplayName) < strings.ToLower(picker[j].DisplayName)
	})
	selected := selectWorkflow(picker, req.WorkflowID)
	if selected.WorkflowID == "" {
		return serverapi.WorkflowBoard{ProjectID: projectID, Project: projectBoardProject(project), WorkflowPicker: picker, GeneratedAtUnixMs: time.Now().UTC().UnixMilli()}, nil
	}
	def := definitions[selected.WorkflowID]
	nodeKinds := nodeKindsByWorkflowID[selected.WorkflowID]
	groups := boardGroups(def)
	columns := boardColumns(def)
	applyColumnTaskCountsFromPlacements(columns, tasks, placementsByTaskID, selected.WorkflowID, def, nodeKinds)
	cards := make([]serverapi.WorkflowBoardTaskCard, 0)
	doneCards := make([]serverapi.WorkflowBoardTaskCard, 0)
	donePreview := make([]serverapi.WorkflowBoardTaskCard, 0)
	for _, task := range tasks {
		if task.WorkflowID != selected.WorkflowID {
			continue
		}
		card, done, err := s.taskCard(ctx, task, effectiveBoardPlacementsForTask(task, placementsByTaskID[task.ID], def, nodeKinds), def, nodeKinds, sourceWorkspaceForTask(task, workspacesByID, primaryWorkspace))
		if err != nil {
			return serverapi.WorkflowBoard{}, err
		}
		if done {
			doneCards = append(doneCards, card)
			if len(donePreview) < donePreviewLimit {
				donePreview = append(donePreview, card)
			}
			continue
		}
		cards = append(cards, card)
	}
	nextPageToken := ""
	if len(cards) > offset+pageSize {
		nextPageToken = strconv.Itoa(offset + pageSize)
	}
	cards = pageCards(cards, offset, pageSize)
	board := serverapi.WorkflowBoard{
		ProjectID:          projectID,
		Project:            projectBoardProject(project),
		SelectedWorkflow:   selected,
		WorkflowPicker:     picker,
		Groups:             groups,
		Columns:            columns,
		Cards:              cards,
		DonePreview:        donePreview,
		HasHiddenDoneCards: false,
		NextPageToken:      nextPageToken,
		GeneratedAtUnixMs:  time.Now().UTC().UnixMilli(),
	}
	return board, nil
}

func (s *Service) ListBoardNodeCards(ctx context.Context, req serverapi.WorkflowBoardNodeCardsListRequest, _ workflow.RoleResolver) (serverapi.WorkflowBoardNodeCardsListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	if s == nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, errors.New("workflow view service is required")
	}
	projectID := strings.TrimSpace(req.ProjectID)
	workflowID := strings.TrimSpace(req.WorkflowID)
	nodeID := strings.TrimSpace(req.NodeID)
	def, nodeKinds, err := s.definition(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	if _, ok := workflowNodeByID(def)[nodeID]; !ok {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, errors.New("node_id is invalid for workflow")
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	if pageSize > 200 {
		pageSize = 200
	}
	cursor, err := parseBoardNodeCardsPageToken(req.PageToken, projectID, workflowID, nodeID)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	cursorSet := int64(0)
	if cursor.hasValue {
		cursorSet = 1
	}
	tasks, err := s.queries.ListBoardNodeTasks(ctx, sqlitegen.ListBoardNodeTasksParams{
		ProjectID:              projectID,
		WorkflowID:             workflowID,
		CursorSet:              cursorSet,
		CursorUpdatedAtUnixMs:  cursor.updatedAtUnixMs,
		CursorTaskID:           cursor.taskID,
		NodeID:                 nodeID,
		CanceledTerminalNodeID: canceledBoardTerminalNodeID(def),
		LimitRows:              int64(pageSize + 1),
	})
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	project, err := s.metadata.GetProjectOverview(ctx, projectID)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	primaryWorkspace, workspacesByID := boardProjectWorkspaceSummaries(project)
	placementsByTaskID, err := s.boardPlacementsByTask(ctx, tasks)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	candidates := tasks
	hasNext := len(candidates) > pageSize
	if hasNext {
		candidates = candidates[:pageSize]
	}
	cards := make([]serverapi.WorkflowBoardTaskCard, 0, len(candidates))
	for _, task := range candidates {
		card, _, err := s.taskCard(ctx, task, effectiveBoardPlacementsForTask(task, placementsByTaskID[task.ID], def, nodeKinds), def, nodeKinds, sourceWorkspaceForTask(task, workspacesByID, primaryWorkspace))
		if err != nil {
			return serverapi.WorkflowBoardNodeCardsListResponse{}, err
		}
		cards = append(cards, card)
	}
	nextPageToken := ""
	if hasNext && len(candidates) > 0 {
		last := candidates[len(candidates)-1]
		nextPageToken = boardNodeCardsPageToken(projectID, workflowID, nodeID, last)
	}
	return serverapi.WorkflowBoardNodeCardsListResponse{ProjectID: projectID, WorkflowID: workflowID, NodeID: nodeID, Cards: cards, NextPageToken: nextPageToken, GeneratedAtUnixMs: time.Now().UTC().UnixMilli()}, nil
}

func (s *Service) boardPlacementsByTask(ctx context.Context, tasks []sqlitegen.TaskRecord) (map[string][]sqlitegen.TaskNodePlacementRecord, error) {
	if len(tasks) == 0 {
		return map[string][]sqlitegen.TaskNodePlacementRecord{}, nil
	}
	taskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.ID)
	}
	placements, err := s.queries.ListTaskNodePlacementsByTasks(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	byTaskID := make(map[string][]sqlitegen.TaskNodePlacementRecord, len(tasks))
	for _, placement := range placements {
		byTaskID[placement.TaskID] = append(byTaskID[placement.TaskID], placement)
	}
	return byTaskID, nil
}

func (s *Service) GetTask(ctx context.Context, taskID string) (serverapi.WorkflowTaskDetail, error) {
	if s == nil {
		return serverapi.WorkflowTaskDetail{}, errors.New("workflow view service is required")
	}
	if strings.TrimSpace(taskID) == "" {
		return serverapi.WorkflowTaskDetail{}, errors.New("task_id is required")
	}
	task, err := s.queries.GetTask(ctx, taskID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	def, nodeKinds, err := s.definition(ctx, task.WorkflowID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return serverapi.WorkflowTaskDetail{}, err
	}
	placements, err := s.queries.ListTaskNodePlacements(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	runs, err := s.queries.ListTaskRuns(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	transitions, err := s.queries.ListTaskTransitions(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	comments, err := s.queries.ListTaskComments(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	project, err := s.metadata.GetProjectOverview(ctx, task.ProjectID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	primaryWorkspace := serverapi.ProjectWorkspaceSummary{}
	workspacesByID := map[string]serverapi.ProjectWorkspaceSummary{}
	for _, workspace := range project.Workspaces {
		dto := projectWorkspaceSummary(workspace)
		workspacesByID[dto.WorkspaceID] = dto
		if workspace.IsPrimary {
			primaryWorkspace = dto
		}
	}
	linkByWorkflowID := map[string]sqlitegen.ProjectWorkflowLinkRecord{}
	links, err := s.queries.ListProjectWorkflowLinks(ctx, task.ProjectID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	for _, link := range links {
		linkByWorkflowID[link.WorkflowID] = preferredProjectWorkflowLink(linkByWorkflowID[link.WorkflowID], link)
	}
	nodeByID := workflowNodeByID(def)
	summary := taskSummary(task, placements, nodeKinds)
	status, actions := taskStatusAndActions(task, summary, placements, runs, def, nodeKinds)
	detail := serverapi.WorkflowTaskDetail{Summary: summary, Project: projectBoardProject(project), Workflow: workflowPickerItem(def, linkByWorkflowID[task.WorkflowID], nil), Body: task.Body, SourceURL: task.SourceUrl, SourceWorkspace: sourceWorkspaceForTask(task, workspacesByID, primaryWorkspace), Status: status, Actions: actions}
	if strings.TrimSpace(task.ManagedWorktreeID.String) != "" {
		if worktree, err := s.queries.GetWorktreeByID(ctx, strings.TrimSpace(task.ManagedWorktreeID.String)); err == nil {
			view := worktreeView(worktree)
			detail.ManagedWorktree = &view
		} else if !errors.Is(err, sql.ErrNoRows) {
			return serverapi.WorkflowTaskDetail{}, err
		}
	}
	attention, err := s.attentionItems(ctx, task.ProjectID, task.ID, nil)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	sortAttentionItems(attention)
	detail.Attention = attention
	for _, placement := range placements {
		detail.Placements = append(detail.Placements, placementDTO(placement, nodeByID))
	}
	sessionNames, err := s.sessionNamesByRun(ctx, runs)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	for _, run := range runs {
		detail.Runs = append(detail.Runs, runDTO(run, nodeByID, sessionNames))
	}
	edgesByTransitionID, err := s.transitionEdgesByTransitionID(ctx, transitions)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	for _, transition := range transitions {
		dto, err := transitionDTO(transition, edgesByTransitionID[transition.ID])
		if err != nil {
			return serverapi.WorkflowTaskDetail{}, err
		}
		detail.Transitions = append(detail.Transitions, dto)
	}
	for _, comment := range comments {
		detail.Comments = append(detail.Comments, commentDTO(comment))
	}
	return detail, nil
}

func (s *Service) ListTaskActivity(ctx context.Context, req serverapi.WorkflowTaskActivityListRequest) (serverapi.WorkflowTaskActivityListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	task, err := s.queries.GetTask(ctx, strings.TrimSpace(req.TaskID))
	if err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	def, _, err := s.definition(ctx, task.WorkflowID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	nodeByID := workflowNodeByID(def)
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	cursor, err := parseActivityPageToken(req.PageToken)
	if err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	rows, err := s.taskActivityRows(ctx, task.ID, cursor, pageSize+1)
	if err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	pageRows := rows
	hasNext := len(rows) > pageSize
	if hasNext {
		pageRows = rows[:pageSize]
	}
	comments, err := s.commentsByID(ctx, sourceIDsByType(pageRows, "comment"))
	if err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	transitions, err := s.transitionsByID(ctx, sourceIDsByType(pageRows, "transition"))
	if err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	edgesByTransitionID, err := s.transitionEdgesByTransitionID(ctx, transitions)
	if err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	transitionByID := taskTransitionByID(transitions)
	runs, err := s.runsByID(ctx, sourceIDsByTypes(pageRows, "run_started", "run_completed", "run_interrupted"))
	if err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	sessionNames, err := s.sessionNamesByRun(ctx, runs)
	if err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	runByID := taskRunByID(runs)
	items, err := s.activityItemsFromRows(task, pageRows, comments, transitionByID, edgesByTransitionID, runByID, nodeByID, sessionNames)
	if err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	nextPageToken := ""
	if hasNext && len(items) > 0 {
		last := items[len(items)-1]
		nextPageToken = activityPageToken(last)
	}
	return serverapi.WorkflowTaskActivityListResponse{Items: items, NextPageToken: nextPageToken, GeneratedAtUnixMs: time.Now().UTC().UnixMilli()}, nil
}

func (s *Service) GetTaskTeleportTarget(ctx context.Context, req serverapi.WorkflowTaskTeleportTargetRequest) (serverapi.WorkflowTaskTeleportTargetResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskTeleportTargetResponse{}, err
	}
	task, err := s.queries.GetTask(ctx, strings.TrimSpace(req.TaskID))
	if err != nil {
		return serverapi.WorkflowTaskTeleportTargetResponse{}, err
	}
	runs, err := s.queries.ListTaskRuns(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowTaskTeleportTargetResponse{}, err
	}
	var selected sqlitegen.TaskRunRecord
	found := false
	requestedRunID := strings.TrimSpace(req.RunID)
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if requestedRunID != "" && run.ID != requestedRunID {
			continue
		}
		if requestedRunID == "" && strings.TrimSpace(run.SessionID.String) == "" {
			continue
		}
		selected = run
		found = true
		break
	}
	if !found {
		if requestedRunID != "" {
			return serverapi.WorkflowTaskTeleportTargetResponse{Available: false, TaskID: task.ID, RunID: requestedRunID, ProjectID: task.ProjectID, FailureReason: "run not found for task"}, nil
		}
		return serverapi.WorkflowTaskTeleportTargetResponse{Available: false, TaskID: task.ID, RunID: requestedRunID, ProjectID: task.ProjectID, FailureReason: "no task run session yet"}, nil
	}
	if strings.TrimSpace(selected.SessionID.String) == "" {
		return serverapi.WorkflowTaskTeleportTargetResponse{Available: false, TaskID: task.ID, RunID: selected.ID, ProjectID: task.ProjectID, FailureReason: "no task run session yet"}, nil
	}
	target, err := s.queries.GetSessionExecutionTargetByID(ctx, selected.SessionID.String)
	if err != nil {
		return serverapi.WorkflowTaskTeleportTargetResponse{}, err
	}
	worktreeID := strings.TrimSpace(target.WorktreeID.String)
	if worktreeID == "" {
		worktreeID = strings.TrimSpace(task.ManagedWorktreeID.String)
	}
	return serverapi.WorkflowTaskTeleportTargetResponse{Available: true, TaskID: task.ID, RunID: selected.ID, SessionID: selected.SessionID.String, ProjectID: task.ProjectID, WorkspaceID: target.WorkspaceID, WorktreeID: worktreeID, CwdRelpath: target.CwdRelpath}, nil
}

func (s *Service) ListAttention(ctx context.Context, req serverapi.WorkflowAttentionListRequest, roleResolver workflow.RoleResolver) (serverapi.WorkflowAttentionListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowAttentionListResponse{}, err
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	offset, err := parseOffsetPageToken(req.PageToken)
	if err != nil {
		return serverapi.WorkflowAttentionListResponse{}, err
	}
	items, err := s.attentionItems(ctx, strings.TrimSpace(req.ProjectID), "", roleResolver)
	if err != nil {
		return serverapi.WorkflowAttentionListResponse{}, err
	}
	sortAttentionItems(items)
	nextPageToken := ""
	if len(items) > offset+pageSize {
		nextPageToken = strconv.Itoa(offset + pageSize)
	}
	items = pageAttentionItems(items, offset, pageSize)
	return serverapi.WorkflowAttentionListResponse{Items: items, NextPageToken: nextPageToken, GeneratedAtUnixMs: time.Now().UTC().UnixMilli()}, nil
}

func (s *Service) ListTaskAttention(ctx context.Context, req serverapi.WorkflowTaskAttentionListRequest, roleResolver workflow.RoleResolver) (serverapi.WorkflowTaskAttentionListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskAttentionListResponse{}, err
	}
	task, err := s.queries.GetTask(ctx, strings.TrimSpace(req.TaskID))
	if err != nil {
		return serverapi.WorkflowTaskAttentionListResponse{}, err
	}
	items, err := s.attentionItems(ctx, task.ProjectID, task.ID, roleResolver)
	if err != nil {
		return serverapi.WorkflowTaskAttentionListResponse{}, err
	}
	sortAttentionItems(items)
	return serverapi.WorkflowTaskAttentionListResponse{Items: items, GeneratedAtUnixMs: time.Now().UTC().UnixMilli()}, nil
}

func (s *Service) attentionItems(ctx context.Context, projectID string, taskID string, roleResolver workflow.RoleResolver) ([]serverapi.WorkflowAttentionItem, error) {
	items := []serverapi.WorkflowAttentionItem{}
	approvals, err := s.approvalAttentionItems(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	items = append(items, approvals...)
	questions, err := s.questionAttentionItems(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	items = append(items, questions...)
	interrupted, err := s.interruptedRunAttentionItems(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	items = append(items, interrupted...)
	if strings.TrimSpace(taskID) == "" {
		blockers, err := s.validationAttentionItems(ctx, projectID, roleResolver)
		if err != nil {
			return nil, err
		}
		items = append(items, blockers...)
	}
	return items, nil
}

func (s *Service) definition(ctx context.Context, workflowID string) (serverapi.WorkflowDefinition, map[string]workflow.NodeKind, error) {
	row, err := s.queries.GetWorkflow(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	nodes, err := s.queries.ListWorkflowNodes(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	def := serverapi.WorkflowDefinition{Workflow: serverapi.WorkflowRecord{ID: row.ID, Name: row.Name, Description: row.Description, GraphRevision: row.GraphRevision, DefinitionRevision: row.DefinitionRevision}}
	nodeGroups, err := s.queries.ListWorkflowNodeGroups(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	groupByID := map[string]serverapi.WorkflowNodeGroup{}
	for _, group := range nodeGroups {
		dto := serverapi.WorkflowNodeGroup{GroupID: group.ID, WorkflowID: group.WorkflowID, GroupKey: group.GroupKey, DisplayName: group.DisplayName, SortOrder: int(group.SortOrder)}
		groupByID[group.ID] = dto
		def.NodeGroups = append(def.NodeGroups, dto)
	}
	groups, err := s.queries.ListWorkflowTransitionGroups(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	edges, err := s.queries.ListWorkflowEdges(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	nodeKinds := map[string]workflow.NodeKind{}
	for _, node := range nodes {
		fields := []serverapi.WorkflowOutputField{}
		if err := workflowjson.UnmarshalString(node.OutputFieldsJson, &fields); err != nil {
			return serverapi.WorkflowDefinition{}, nil, err
		}
		groupID := strings.TrimSpace(node.GroupID.String)
		groupKey := ""
		if group, ok := groupByID[groupID]; ok {
			groupKey = group.GroupKey
		}
		def.Nodes = append(def.Nodes, serverapi.WorkflowNode{ID: node.ID, WorkflowID: node.WorkflowID, Key: node.NodeKey, Kind: node.Kind, DisplayName: node.DisplayName, GroupID: groupID, GroupKey: groupKey, SubagentRole: node.SubagentRole, PromptTemplate: node.PromptTemplate, OutputFields: fields})
		nodeKinds[node.ID] = workflow.NodeKind(node.Kind)
	}
	for _, group := range groups {
		def.TransitionGroups = append(def.TransitionGroups, serverapi.WorkflowTransitionGroup{ID: group.ID, WorkflowID: group.WorkflowID, SourceNodeID: group.SourceNodeID, TransitionID: string(group.TransitionID), DisplayName: group.DisplayName})
	}
	for _, edge := range edges {
		inputs := []serverapi.WorkflowInputBinding{}
		requirements := []serverapi.WorkflowOutputRequirement{}
		if err := workflowjson.UnmarshalString(edge.InputBindingsJson, &inputs); err != nil {
			return serverapi.WorkflowDefinition{}, nil, err
		}
		if err := workflowjson.UnmarshalString(edge.OutputRequirementsJson, &requirements); err != nil {
			return serverapi.WorkflowDefinition{}, nil, err
		}
		def.Edges = append(def.Edges, serverapi.WorkflowEdge{ID: edge.ID, WorkflowID: edge.WorkflowID, TransitionGroupID: edge.TransitionGroupID, Key: edge.EdgeKey, TargetNodeID: edge.TargetNodeID, RequiresApproval: edge.RequiresApproval != 0, ContextMode: edge.ContextMode, ContextSource: apiContextSource(workflow.ContextSource{Kind: workflow.ContextSourceKind(edge.ContextSourceKind), NodeKey: workflow.ModelKey(edge.ContextSourceNodeKey)}), InputBindings: inputs, OutputRequirements: requirements})
	}
	return def, nodeKinds, nil
}

func taskSummary(task sqlitegen.TaskRecord, placements []sqlitegen.TaskNodePlacementRecord, nodeKinds map[string]workflow.NodeKind) serverapi.WorkflowTaskSummary {
	summary := serverapi.WorkflowTaskSummary{ID: task.ID, ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, ShortID: task.ShortID, Title: task.Title, BodyPreview: bodyPreview(task.Body), SourceWorkspaceID: strings.TrimSpace(task.SourceWorkspaceID.String), CanceledAt: task.CanceledAtUnixMs, CancelReason: task.CancellationReason, CreatedAtUnixMs: task.CreatedAtUnixMs, UpdatedAtUnixMs: task.UpdatedAtUnixMs}
	seenActive := map[string]bool{}
	for _, placement := range placements {
		if placement.State != "active" && placement.State != "waiting_approval" {
			continue
		}
		if nodeKinds[placement.NodeID] == workflow.NodeKindTerminal {
			summary.Done = true
		}
		if !seenActive[placement.NodeID] {
			summary.ActiveNodeIDs = append(summary.ActiveNodeIDs, placement.NodeID)
			seenActive[placement.NodeID] = true
		}
	}
	return summary
}

func placementDTO(placement sqlitegen.TaskNodePlacementRecord, nodes map[string]serverapi.WorkflowNode) serverapi.WorkflowPlacement {
	dto := serverapi.WorkflowPlacement{ID: placement.ID, TaskID: placement.TaskID, NodeID: placement.NodeID, State: placement.State, ParallelBatchTransitionID: strings.TrimSpace(placement.ParallelBatchTransitionID.String), ParallelBranchEdgeID: strings.TrimSpace(placement.ParallelBranchEdgeID.String)}
	if node, ok := nodes[placement.NodeID]; ok {
		dto.NodeKey = node.Key
		dto.NodeDisplayName = node.DisplayName
		dto.NodeKind = node.Kind
	}
	return dto
}

func commentDTO(comment sqlitegen.TaskComment) serverapi.WorkflowTaskComment {
	return serverapi.WorkflowTaskComment{ID: comment.ID, TaskID: comment.TaskID, Body: comment.Body, Author: comment.AuthorKind, AuthorID: comment.AuthorID, CreatedAtUnixMs: comment.CreatedAtUnixMs, UpdatedAt: comment.UpdatedAtUnixMs}
}

func workflowNodeByID(def serverapi.WorkflowDefinition) map[string]serverapi.WorkflowNode {
	out := make(map[string]serverapi.WorkflowNode, len(def.Nodes))
	for _, node := range def.Nodes {
		out[node.ID] = node
	}
	return out
}

func workflowTransitionGroupByID(def serverapi.WorkflowDefinition) map[string]serverapi.WorkflowTransitionGroup {
	out := make(map[string]serverapi.WorkflowTransitionGroup, len(def.TransitionGroups))
	for _, group := range def.TransitionGroups {
		out[group.ID] = group
	}
	return out
}

func workflowPickerItem(def serverapi.WorkflowDefinition, link sqlitegen.ProjectWorkflowLinkRecord, validation *workflow.ValidationResult) serverapi.WorkflowPickerItem {
	item := serverapi.WorkflowPickerItem{WorkflowID: def.Workflow.ID, DisplayName: def.Workflow.Name, Description: def.Workflow.Description, GraphRevision: def.Workflow.GraphRevision, IsProjectDefault: link.ID != "" && link.IsDefault != 0, ValidForTaskCreation: link.ID != ""}
	if validation != nil {
		item.ValidForTaskCreation = link.ID != "" && validation.Valid()
		item.ValidationErrors = validationErrors(def.Workflow.ID, validation.Errors)
	}
	return item
}

func preferredProjectWorkflowLink(current sqlitegen.ProjectWorkflowLinkRecord, next sqlitegen.ProjectWorkflowLinkRecord) sqlitegen.ProjectWorkflowLinkRecord {
	if current.ID == "" {
		return next
	}
	return current
}

func worktreeView(row sqlitegen.GetWorktreeByIDRow) serverapi.WorktreeView {
	return serverapi.WorktreeView{WorktreeID: row.ID, DisplayName: displayNameForPath(row.CanonicalRootPath), CanonicalRoot: row.CanonicalRootPath, Availability: availabilityForPath(row.CanonicalRootPath), IsMain: row.IsMain != 0, BuilderManaged: row.BuilderManaged != 0, CreatedBranch: row.CreatedBranch != 0, OriginSessionID: row.OriginSessionID}
}

func displayNameForPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(trimmed))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func availabilityForPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if _, err := os.Stat(trimmed); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "missing"
		}
		return "inaccessible"
	}
	return "available"
}

type taskActivityRow struct {
	activityID       string
	kind             string
	sourceID         string
	occurredAtUnixMs int64
	updatedAtUnixMs  int64
	actor            string
}

func (s *Service) taskActivityRows(ctx context.Context, taskID string, cursor activityPageCursor, limit int) ([]taskActivityRow, error) {
	if limit <= 0 {
		return []taskActivityRow{}, nil
	}
	cursorActive := int64(0)
	if cursor.hasValue {
		cursorActive = 1
	}
	rows, err := s.metadata.DB().QueryContext(ctx, `
SELECT activity_id, kind, source_id, occurred_at_unix_ms, updated_at_unix_ms, actor
FROM (
    SELECT
        'comment:' || c.id AS activity_id,
        'comment' AS kind,
        c.id AS source_id,
        c.updated_at_unix_ms AS occurred_at_unix_ms,
        c.updated_at_unix_ms AS updated_at_unix_ms,
        c.author_kind AS actor
    FROM task_comments c
    WHERE c.task_id = ?

    UNION ALL

    SELECT
        'transition:' || tt.id AS activity_id,
        'transition' AS kind,
        tt.id AS source_id,
        tt.created_at_unix_ms AS occurred_at_unix_ms,
        tt.applied_at_unix_ms AS updated_at_unix_ms,
        tt.actor AS actor
    FROM task_transitions tt
    WHERE tt.task_id = ?

    UNION ALL

    SELECT
        'run_started:' || r.id AS activity_id,
        'run_started' AS kind,
        r.id AS source_id,
        r.started_at_unix_ms AS occurred_at_unix_ms,
        r.updated_at_unix_ms AS updated_at_unix_ms,
        '' AS actor
    FROM task_run_records r
    WHERE r.task_id = ?
      AND r.started_at_unix_ms > 0

    UNION ALL

    SELECT
        'run_completed:' || r.id AS activity_id,
        'run_completed' AS kind,
        r.id AS source_id,
        r.completed_at_unix_ms AS occurred_at_unix_ms,
        r.updated_at_unix_ms AS updated_at_unix_ms,
        '' AS actor
    FROM task_run_records r
    WHERE r.task_id = ?
      AND r.completed_at_unix_ms > 0

    UNION ALL

    SELECT
        'run_interrupted:' || r.id AS activity_id,
        'run_interrupted' AS kind,
        r.id AS source_id,
        r.interrupted_at_unix_ms AS occurred_at_unix_ms,
        r.updated_at_unix_ms AS updated_at_unix_ms,
        '' AS actor
    FROM task_run_records r
    WHERE r.task_id = ?
      AND r.interrupted_at_unix_ms > 0

    UNION ALL

    SELECT
        'task_canceled:' || t.id AS activity_id,
        'task_canceled' AS kind,
        t.id AS source_id,
        t.canceled_at_unix_ms AS occurred_at_unix_ms,
        t.updated_at_unix_ms AS updated_at_unix_ms,
        '' AS actor
    FROM task_records t
    WHERE t.id = ?
      AND t.canceled_at_unix_ms > 0
) activity
WHERE (? = 0 OR occurred_at_unix_ms < ? OR (occurred_at_unix_ms = ? AND activity_id < ?))
ORDER BY occurred_at_unix_ms DESC, activity_id DESC
LIMIT ?`, taskID, taskID, taskID, taskID, taskID, taskID, cursorActive, cursor.occurredAtUnixMs, cursor.occurredAtUnixMs, cursor.activityID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []taskActivityRow{}
	for rows.Next() {
		var row taskActivityRow
		if err := rows.Scan(&row.activityID, &row.kind, &row.sourceID, &row.occurredAtUnixMs, &row.updatedAtUnixMs, &row.actor); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Service) activityItemsFromRows(task sqlitegen.TaskRecord, rows []taskActivityRow, comments map[string]sqlitegen.TaskComment, transitions map[string]sqlitegen.TaskTransitionRecord, edges map[string][]sqlitegen.TaskTransitionEdgeRecord, runs map[string]sqlitegen.TaskRunRecord, nodes map[string]serverapi.WorkflowNode, sessionNames map[string]string) ([]serverapi.WorkflowTaskActivityItem, error) {
	items := make([]serverapi.WorkflowTaskActivityItem, 0, len(rows))
	for _, row := range rows {
		item := serverapi.WorkflowTaskActivityItem{ActivityID: row.activityID, Type: row.kind, TaskID: task.ID, OccurredAtUnixMs: row.occurredAtUnixMs, UpdatedAtUnixMs: row.updatedAtUnixMs, Actor: row.actor}
		switch row.kind {
		case "comment":
			comment, ok := comments[row.sourceID]
			if !ok {
				return nil, errors.New("activity comment source is missing")
			}
			item.Summary = "Comment"
			dto := commentDTO(comment)
			item.Comment = &dto
		case "transition":
			transition, ok := transitions[row.sourceID]
			if !ok {
				return nil, errors.New("activity transition source is missing")
			}
			dto, err := transitionDTO(transition, edges[transition.ID])
			if err != nil {
				return nil, err
			}
			summary := strings.TrimSpace(dto.TransitionDisplayName)
			if summary == "" {
				summary = dto.TransitionID
			}
			item.Actor = transition.Actor
			item.Summary = "Transition: " + summary
			item.Transition = &dto
		case "run_started", "run_completed", "run_interrupted":
			run, ok := runs[row.sourceID]
			if !ok {
				return nil, errors.New("activity run source is missing")
			}
			runView := runDTO(run, nodes, sessionNames)
			item.Run = &runView
			switch row.kind {
			case "run_started":
				item.Summary = "Run started"
			case "run_completed":
				item.Summary = "Run completed"
			case "run_interrupted":
				item.Summary = "Run interrupted"
				attention := serverapi.WorkflowAttentionItem{ID: attentionKindInterruptedRun + ":" + run.ID, Kind: attentionKindInterruptedRun, ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, TaskID: task.ID, TaskShortID: task.ShortID, TaskTitle: task.Title, RunID: run.ID, SessionID: run.SessionID.String, Message: "Run interrupted", OccurredAtUnixMs: run.InterruptedAtUnixMs}
				item.Attention = &attention
			}
		case "task_canceled":
			item.Summary = "Task canceled"
		default:
			return nil, errors.New("activity kind is unsupported")
		}
		items = append(items, item)
	}
	return items, nil
}

func runDTO(run sqlitegen.TaskRunRecord, nodes map[string]serverapi.WorkflowNode, sessionNames map[string]string) serverapi.WorkflowRun {
	dto := serverapi.WorkflowRun{ID: run.ID, TaskID: run.TaskID, PlacementID: run.PlacementID, NodeID: run.NodeID, SessionID: run.SessionID.String, Generation: run.RunGeneration, StartedAtUnixMs: run.StartedAtUnixMs, CompletedAtUnixMs: run.CompletedAtUnixMs, InterruptedAtUnixMs: run.InterruptedAtUnixMs, InterruptionReason: run.InterruptionReason, WaitingAskID: run.WaitingAskID, Status: runStatus(run)}
	if node, ok := nodes[run.NodeID]; ok {
		dto.Role = node.SubagentRole
	}
	if name, ok := sessionNames[strings.TrimSpace(run.SessionID.String)]; ok {
		dto.SessionName = name
	}
	return dto
}

func runStatus(run sqlitegen.TaskRunRecord) string {
	switch {
	case run.CompletedAtUnixMs != 0:
		return "completed"
	case run.InterruptedAtUnixMs != 0:
		return "interrupted"
	case strings.TrimSpace(run.WaitingAskID) != "":
		return "waiting_question"
	case run.StartedAtUnixMs != 0:
		return "running"
	default:
		return "pending"
	}
}

func transitionDTO(transition sqlitegen.TaskTransitionRecord, edges []sqlitegen.TaskTransitionEdgeRecord) (serverapi.WorkflowTaskTransition, error) {
	outputs := map[string]string{}
	if err := workflowjson.UnmarshalString(transition.OutputValuesJson, &outputs); err != nil {
		return serverapi.WorkflowTaskTransition{}, err
	}
	dto := serverapi.WorkflowTaskTransition{
		ID:                    transition.ID,
		TaskID:                transition.TaskID,
		SourceRunID:           strings.TrimSpace(transition.SourceRunID.String),
		SourcePlacementID:     strings.TrimSpace(transition.SourcePlacementID.String),
		SourceNodeID:          strings.TrimSpace(transition.SourceNodeID.String),
		SourceNodeKey:         transition.SourceNodeKey,
		SourceNodeDisplayName: transition.SourceNodeDisplayName,
		TransitionGroupID:     strings.TrimSpace(transition.TransitionGroupID.String),
		TransitionID:          transition.TransitionID,
		TransitionDisplayName: transition.TransitionDisplayName,
		WorkflowRevisionSeen:  transition.WorkflowRevisionSeen,
		Actor:                 transition.Actor,
		State:                 transition.State,
		Commentary:            transition.Commentary,
		OutputValues:          outputs,
		CreatedAt:             transition.CreatedAtUnixMs,
		AppliedAtUnixMs:       transition.AppliedAtUnixMs,
	}
	for _, edge := range edges {
		inputs := []serverapi.WorkflowInputBinding{}
		if err := workflowjson.UnmarshalString(edge.InputBindingsJson, &inputs); err != nil {
			return serverapi.WorkflowTaskTransition{}, err
		}
		requirements := []serverapi.WorkflowOutputRequirement{}
		if err := workflowjson.UnmarshalString(edge.OutputRequirementsJson, &requirements); err != nil {
			return serverapi.WorkflowTaskTransition{}, err
		}
		dto.Edges = append(dto.Edges, serverapi.WorkflowTransitionEdge{
			ID:                    edge.ID,
			TaskTransitionID:      edge.TaskTransitionID,
			WorkflowEdgeID:        strings.TrimSpace(edge.WorkflowEdgeID.String),
			EdgeKey:               edge.EdgeKey,
			TargetNodeID:          strings.TrimSpace(edge.TargetNodeID.String),
			TargetNodeKey:         edge.TargetNodeKey,
			TargetNodeDisplayName: edge.TargetNodeDisplayName,
			TargetNodeKind:        edge.TargetNodeKind,
			TargetPlacementID:     strings.TrimSpace(edge.TargetPlacementID.String),
			State:                 edge.State,
			ContextMode:           edge.ContextMode,
			RequiresApproval:      edge.RequiresApproval != 0,
			InputBindings:         inputs,
			OutputRequirements:    requirements,
			WorkflowRevisionSeen:  edge.WorkflowRevisionSeen,
		})
	}
	return dto, nil
}

func (s *Service) sessionNamesByRun(ctx context.Context, runs []sqlitegen.TaskRunRecord) (map[string]string, error) {
	sessionIDs := []string{}
	seen := map[string]bool{}
	for _, run := range runs {
		sessionID := strings.TrimSpace(run.SessionID.String)
		if sessionID == "" || seen[sessionID] {
			continue
		}
		sessionIDs = append(sessionIDs, sessionID)
		seen[sessionID] = true
	}
	if len(sessionIDs) == 0 {
		return map[string]string{}, nil
	}
	placeholders := make([]string, 0, len(sessionIDs))
	args := make([]any, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		placeholders = append(placeholders, "?")
		args = append(args, sessionID)
	}
	rows, err := s.metadata.DB().QueryContext(ctx, `SELECT id, name FROM sessions WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var sessionID, name string
		if err := rows.Scan(&sessionID, &name); err != nil {
			return nil, err
		}
		out[sessionID] = name
	}
	return out, rows.Err()
}

func (s *Service) transitionEdgesByTransitionID(ctx context.Context, transitions []sqlitegen.TaskTransitionRecord) (map[string][]sqlitegen.TaskTransitionEdgeRecord, error) {
	transitionIDs := make([]string, 0, len(transitions))
	args := make([]any, 0, len(transitions))
	for _, transition := range transitions {
		transitionIDs = append(transitionIDs, "?")
		args = append(args, transition.ID)
	}
	out := map[string][]sqlitegen.TaskTransitionEdgeRecord{}
	if len(args) == 0 {
		return out, nil
	}
	rows, err := s.metadata.DB().QueryContext(ctx, `
SELECT
    id,
    task_transition_id,
    workflow_edge_id,
    edge_key,
    workflow_revision_seen,
    target_node_id,
    target_node_key,
    target_node_display_name,
    target_node_kind,
    target_placement_id,
    state,
    context_mode,
    requires_approval,
    input_bindings_json,
    output_requirements_json,
    metadata_json
FROM task_transition_edge_records
WHERE task_transition_id IN (`+strings.Join(transitionIDs, ",")+`)
ORDER BY task_transition_id ASC, (
    SELECT storage.rowid
    FROM task_transition_edges storage
    WHERE storage.id = task_transition_edge_records.id
) ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var edge sqlitegen.TaskTransitionEdgeRecord
		if err := rows.Scan(
			&edge.ID,
			&edge.TaskTransitionID,
			&edge.WorkflowEdgeID,
			&edge.EdgeKey,
			&edge.WorkflowRevisionSeen,
			&edge.TargetNodeID,
			&edge.TargetNodeKey,
			&edge.TargetNodeDisplayName,
			&edge.TargetNodeKind,
			&edge.TargetPlacementID,
			&edge.State,
			&edge.ContextMode,
			&edge.RequiresApproval,
			&edge.InputBindingsJson,
			&edge.OutputRequirementsJson,
			&edge.MetadataJson,
		); err != nil {
			return nil, err
		}
		out[edge.TaskTransitionID] = append(out[edge.TaskTransitionID], edge)
	}
	return out, rows.Err()
}

func (s *Service) commentsByID(ctx context.Context, ids []string) (map[string]sqlitegen.TaskComment, error) {
	out := map[string]sqlitegen.TaskComment{}
	if len(ids) == 0 {
		return out, nil
	}
	placeholders, args := placeholdersAndArgs(ids)
	rows, err := s.metadata.DB().QueryContext(ctx, `
SELECT
    id,
    task_id,
    body,
    author_kind,
    author_id,
    created_at_unix_ms,
    updated_at_unix_ms
FROM task_comments
WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var row sqlitegen.TaskComment
		if err := rows.Scan(&row.ID, &row.TaskID, &row.Body, &row.AuthorKind, &row.AuthorID, &row.CreatedAtUnixMs, &row.UpdatedAtUnixMs); err != nil {
			return nil, err
		}
		out[row.ID] = row
	}
	return out, rows.Err()
}

func (s *Service) transitionsByID(ctx context.Context, ids []string) ([]sqlitegen.TaskTransitionRecord, error) {
	if len(ids) == 0 {
		return []sqlitegen.TaskTransitionRecord{}, nil
	}
	placeholders, args := placeholdersAndArgs(ids)
	rows, err := s.metadata.DB().QueryContext(ctx, `
SELECT
    id,
    task_id,
    source_run_id,
    source_placement_id,
    source_node_id,
    source_node_key,
    source_node_display_name,
    transition_group_id,
    transition_id,
    transition_display_name,
    workflow_revision_seen,
    actor,
    state,
    commentary,
    output_values_json,
    created_at_unix_ms,
    applied_at_unix_ms
FROM task_transition_records
WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []sqlitegen.TaskTransitionRecord{}
	for rows.Next() {
		var row sqlitegen.TaskTransitionRecord
		if err := rows.Scan(
			&row.ID,
			&row.TaskID,
			&row.SourceRunID,
			&row.SourcePlacementID,
			&row.SourceNodeID,
			&row.SourceNodeKey,
			&row.SourceNodeDisplayName,
			&row.TransitionGroupID,
			&row.TransitionID,
			&row.TransitionDisplayName,
			&row.WorkflowRevisionSeen,
			&row.Actor,
			&row.State,
			&row.Commentary,
			&row.OutputValuesJson,
			&row.CreatedAtUnixMs,
			&row.AppliedAtUnixMs,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Service) runsByID(ctx context.Context, ids []string) ([]sqlitegen.TaskRunRecord, error) {
	if len(ids) == 0 {
		return []sqlitegen.TaskRunRecord{}, nil
	}
	placeholders, args := placeholdersAndArgs(ids)
	rows, err := s.metadata.DB().QueryContext(ctx, `
SELECT
    id,
    task_id,
    placement_id,
    node_id,
    session_id,
    run_generation,
    workflow_revision_seen,
    automation_requested_at_unix_ms,
    created_at_unix_ms,
    updated_at_unix_ms,
    started_at_unix_ms,
    completed_at_unix_ms,
    interrupted_at_unix_ms,
    interruption_reason,
    interruption_detail_json,
    waiting_ask_id,
    final_answer_violation_count,
    invalid_completion_count,
    run_start_snapshot_json,
    metadata_json
FROM task_run_records
WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []sqlitegen.TaskRunRecord{}
	for rows.Next() {
		var row sqlitegen.TaskRunRecord
		if err := rows.Scan(
			&row.ID,
			&row.TaskID,
			&row.PlacementID,
			&row.NodeID,
			&row.SessionID,
			&row.RunGeneration,
			&row.WorkflowRevisionSeen,
			&row.AutomationRequestedAtUnixMs,
			&row.CreatedAtUnixMs,
			&row.UpdatedAtUnixMs,
			&row.StartedAtUnixMs,
			&row.CompletedAtUnixMs,
			&row.InterruptedAtUnixMs,
			&row.InterruptionReason,
			&row.InterruptionDetailJson,
			&row.WaitingAskID,
			&row.FinalAnswerViolationCount,
			&row.InvalidCompletionCount,
			&row.RunStartSnapshotJson,
			&row.MetadataJson,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func placeholdersAndArgs(ids []string) (string, []any) {
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	return strings.Join(placeholders, ","), args
}

func sourceIDsByType(rows []taskActivityRow, kind string) []string {
	ids := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		if row.kind != kind || seen[row.sourceID] {
			continue
		}
		ids = append(ids, row.sourceID)
		seen[row.sourceID] = true
	}
	return ids
}

func sourceIDsByTypes(rows []taskActivityRow, kinds ...string) []string {
	allowed := map[string]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		if !allowed[row.kind] || seen[row.sourceID] {
			continue
		}
		ids = append(ids, row.sourceID)
		seen[row.sourceID] = true
	}
	return ids
}

func taskTransitionByID(transitions []sqlitegen.TaskTransitionRecord) map[string]sqlitegen.TaskTransitionRecord {
	out := make(map[string]sqlitegen.TaskTransitionRecord, len(transitions))
	for _, transition := range transitions {
		out[transition.ID] = transition
	}
	return out
}

func taskRunByID(runs []sqlitegen.TaskRunRecord) map[string]sqlitegen.TaskRunRecord {
	out := make(map[string]sqlitegen.TaskRunRecord, len(runs))
	for _, run := range runs {
		out[run.ID] = run
	}
	return out
}

type activityPageCursor struct {
	occurredAtUnixMs int64
	activityID       string
	hasValue         bool
}

func parseActivityPageToken(token string) (activityPageCursor, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return activityPageCursor{}, nil
	}
	timestampPart, encodedID, ok := strings.Cut(trimmed, "|")
	if !ok {
		return activityPageCursor{}, errors.New("page_token is invalid")
	}
	occurredAt, err := strconv.ParseInt(timestampPart, 10, 64)
	if err != nil || occurredAt < 0 {
		return activityPageCursor{}, errors.New("page_token is invalid")
	}
	decodedID, err := base64.RawURLEncoding.DecodeString(encodedID)
	if err != nil || strings.TrimSpace(string(decodedID)) == "" {
		return activityPageCursor{}, errors.New("page_token is invalid")
	}
	return activityPageCursor{occurredAtUnixMs: occurredAt, activityID: string(decodedID), hasValue: true}, nil
}

func activityPageToken(item serverapi.WorkflowTaskActivityItem) string {
	return strconv.FormatInt(item.OccurredAtUnixMs, 10) + "|" + base64.RawURLEncoding.EncodeToString([]byte(item.ActivityID))
}

func (s *Service) approvalAttentionItems(ctx context.Context, projectID string, taskID string) ([]serverapi.WorkflowAttentionItem, error) {
	rows, err := s.metadata.DB().QueryContext(ctx, `
SELECT tt.id, t.project_id, t.workflow_id, t.id, t.short_id, t.title, tt.created_at_unix_ms
FROM task_transitions tt
JOIN task_records t ON t.id = tt.task_id
WHERE tt.state = 'pending_approval'
  AND t.canceled_at_unix_ms = 0
  AND (? = '' OR t.project_id = ?)
  AND (? = '' OR t.id = ?)
ORDER BY tt.created_at_unix_ms DESC, tt.rowid DESC`, strings.TrimSpace(projectID), strings.TrimSpace(projectID), strings.TrimSpace(taskID), strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []serverapi.WorkflowAttentionItem{}
	for rows.Next() {
		var transitionID, rowProjectID, workflowID, rowTaskID, shortID, title string
		var occurred int64
		if err := rows.Scan(&transitionID, &rowProjectID, &workflowID, &rowTaskID, &shortID, &title, &occurred); err != nil {
			return nil, err
		}
		items = append(items, serverapi.WorkflowAttentionItem{ID: "approval:" + transitionID, Kind: "approval", ProjectID: rowProjectID, WorkflowID: workflowID, TaskID: rowTaskID, TaskShortID: shortID, TaskTitle: title, TaskTransitionID: transitionID, Message: "Approval required", OccurredAtUnixMs: occurred})
	}
	return items, rows.Err()
}

func (s *Service) questionAttentionItems(ctx context.Context, projectID string, taskID string) ([]serverapi.WorkflowAttentionItem, error) {
	rows, err := s.metadata.DB().QueryContext(ctx, `
SELECT r.id, COALESCE(r.session_id, ''), r.waiting_ask_id, t.project_id, t.workflow_id, t.id, t.short_id, t.title, r.updated_at_unix_ms
FROM task_run_records r
JOIN task_records t ON t.id = r.task_id
WHERE trim(r.waiting_ask_id) != ''
  AND r.completed_at_unix_ms = 0
  AND r.interrupted_at_unix_ms = 0
  AND t.canceled_at_unix_ms = 0
  AND (? = '' OR t.project_id = ?)
  AND (? = '' OR t.id = ?)
ORDER BY r.updated_at_unix_ms DESC, (
    SELECT storage.rowid
    FROM task_runs storage
    WHERE storage.id = r.id
) DESC`, strings.TrimSpace(projectID), strings.TrimSpace(projectID), strings.TrimSpace(taskID), strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	type questionAttentionRow struct {
		runID, sessionID, askID, projectID, workflowID, taskID, shortID, title string
		occurred                                                               int64
	}
	rawRows := []questionAttentionRow{}
	for rows.Next() {
		var row questionAttentionRow
		if err := rows.Scan(&row.runID, &row.sessionID, &row.askID, &row.projectID, &row.workflowID, &row.taskID, &row.shortID, &row.title, &row.occurred); err != nil {
			_ = rows.Close()
			return nil, err
		}
		rawRows = append(rawRows, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	items := []serverapi.WorkflowAttentionItem{}
	questions := newPendingQuestionResolver(s.transcripts)
	for _, row := range rawRows {
		question, err := questions.Question(ctx, row.sessionID, row.askID)
		if err != nil {
			question = pendingQuestionFallbackMessage
		}
		items = append(items, serverapi.WorkflowAttentionItem{ID: "question:" + row.runID + ":" + row.askID, Kind: "question", ProjectID: row.projectID, WorkflowID: row.workflowID, TaskID: row.taskID, TaskShortID: row.shortID, TaskTitle: row.title, RunID: row.runID, SessionID: row.sessionID, AskID: row.askID, Message: question, OccurredAtUnixMs: row.occurred})
	}
	return items, nil
}

const pendingQuestionTranscriptPageSize = clientui.MaxCommittedTranscriptSuffixLimit
const pendingQuestionFallbackMessage = "Question pending; open the task to answer."

type pendingQuestionResolver struct {
	transcripts SessionTranscriptPageProvider
	bySession   map[string]map[string]string
}

func newPendingQuestionResolver(transcripts SessionTranscriptPageProvider) *pendingQuestionResolver {
	return &pendingQuestionResolver{transcripts: transcripts, bySession: map[string]map[string]string{}}
}

func (r *pendingQuestionResolver) Question(ctx context.Context, sessionID string, askID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	askID = strings.TrimSpace(askID)
	if r == nil || r.transcripts == nil {
		return "", errors.New("session transcript provider is required to resolve pending question")
	}
	if sessionID == "" || askID == "" {
		return "", errors.New("session_id and ask_id are required to resolve pending question")
	}
	questions, ok := r.bySession[sessionID]
	if ok {
		if question := strings.TrimSpace(questions[askID]); question != "" {
			return question, nil
		}
	} else {
		r.bySession[sessionID] = map[string]string{}
	}
	question, err := r.findQuestion(ctx, sessionID, askID)
	if err != nil {
		return "", err
	}
	r.bySession[sessionID][askID] = question
	return question, nil
}

func (r *pendingQuestionResolver) findQuestion(ctx context.Context, sessionID string, askID string) (string, error) {
	resp, err := r.transcripts.GetSessionTranscriptPage(ctx, serverapi.SessionTranscriptPageRequest{SessionID: sessionID, Window: clientui.TranscriptWindowOngoingTail})
	if err != nil {
		return "", fmt.Errorf("load session %q transcript tail for pending question %q: %w", sessionID, askID, err)
	}
	if question := askQuestionFromTranscriptEntries(resp.Transcript.Entries, askID); question != "" {
		return question, nil
	}
	for nextEnd := resp.Transcript.Offset; nextEnd > 0; {
		start := nextEnd - pendingQuestionTranscriptPageSize
		if start < 0 {
			start = 0
		}
		page, err := r.transcripts.GetSessionTranscriptPage(ctx, serverapi.SessionTranscriptPageRequest{SessionID: sessionID, Offset: start, Limit: nextEnd - start})
		if err != nil {
			return "", fmt.Errorf("load session %q transcript page for pending question %q: %w", sessionID, askID, err)
		}
		if question := askQuestionFromTranscriptEntries(page.Transcript.Entries, askID); question != "" {
			return question, nil
		}
		nextEnd = start
	}
	return "", fmt.Errorf("pending question %q was not found in session %q transcript", askID, sessionID)
}

func askQuestionFromTranscriptEntries(entries []clientui.ChatEntry, askID string) string {
	for _, entry := range entries {
		entryAskID := strings.TrimSpace(entry.ToolCallID)
		if strings.TrimSpace(entry.Role) != "tool_call" || entryAskID != askID || entry.ToolCall == nil {
			continue
		}
		if strings.TrimSpace(entry.ToolCall.ToolName) != string(toolspec.ToolAskQuestion) {
			continue
		}
		if question := strings.TrimSpace(entry.ToolCall.Question); question != "" {
			return question
		}
	}
	return ""
}

func (s *Service) interruptedRunAttentionItems(ctx context.Context, projectID string, taskID string) ([]serverapi.WorkflowAttentionItem, error) {
	rows, err := s.metadata.DB().QueryContext(ctx, `
SELECT r.id, COALESCE(r.session_id, ''), r.interruption_reason, t.project_id, t.workflow_id, t.id, t.short_id, t.title, r.interrupted_at_unix_ms
FROM task_run_records r
JOIN task_records t ON t.id = r.task_id
WHERE r.interrupted_at_unix_ms > 0
  AND r.completed_at_unix_ms = 0
  AND t.canceled_at_unix_ms = 0
  AND (? = '' OR t.project_id = ?)
  AND (? = '' OR t.id = ?)
ORDER BY r.interrupted_at_unix_ms DESC, (
    SELECT storage.rowid
    FROM task_runs storage
    WHERE storage.id = r.id
) DESC`, strings.TrimSpace(projectID), strings.TrimSpace(projectID), strings.TrimSpace(taskID), strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []serverapi.WorkflowAttentionItem{}
	for rows.Next() {
		var runID, sessionID, reason, rowProjectID, workflowID, rowTaskID, shortID, title string
		var occurred int64
		if err := rows.Scan(&runID, &sessionID, &reason, &rowProjectID, &workflowID, &rowTaskID, &shortID, &title, &occurred); err != nil {
			return nil, err
		}
		message := "Run interrupted"
		if strings.TrimSpace(reason) != "" {
			message = "Run interrupted: " + strings.TrimSpace(reason)
		}
		items = append(items, serverapi.WorkflowAttentionItem{ID: attentionKindInterruptedRun + ":" + runID, Kind: attentionKindInterruptedRun, ProjectID: rowProjectID, WorkflowID: workflowID, TaskID: rowTaskID, TaskShortID: shortID, TaskTitle: title, RunID: runID, SessionID: sessionID, Message: message, OccurredAtUnixMs: occurred})
	}
	return items, rows.Err()
}

func (s *Service) validationAttentionItems(ctx context.Context, projectID string, roleResolver workflow.RoleResolver) ([]serverapi.WorkflowAttentionItem, error) {
	rows, err := s.metadata.DB().QueryContext(ctx, `
SELECT project_id, workflow_id, updated_at_unix_ms
FROM project_workflow_links
WHERE (? = '' OR project_id = ?)
ORDER BY updated_at_unix_ms DESC, rowid DESC`, strings.TrimSpace(projectID), strings.TrimSpace(projectID))
	if err != nil {
		return nil, err
	}
	type workflowLink struct {
		projectID  string
		workflowID string
		occurredAt int64
	}
	links := []workflowLink{}
	for rows.Next() {
		var rowProjectID, workflowID string
		var occurred int64
		if err := rows.Scan(&rowProjectID, &workflowID, &occurred); err != nil {
			_ = rows.Close()
			return nil, err
		}
		links = append(links, workflowLink{projectID: rowProjectID, workflowID: workflowID, occurredAt: occurred})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	items := []serverapi.WorkflowAttentionItem{}
	for _, link := range links {
		def, _, err := s.definition(ctx, link.workflowID)
		if err != nil {
			return nil, err
		}
		validation := workflow.ValidateDefinition(definitionForValidation(def), workflow.ValidationOptions{Context: workflow.ValidationContextExecution, RoleResolver: roleResolver})
		if validation.Valid() {
			continue
		}
		items = append(items, serverapi.WorkflowAttentionItem{ID: "validation_blocker:" + link.projectID + ":" + link.workflowID, Kind: "validation_blocker", ProjectID: link.projectID, WorkflowID: link.workflowID, Message: fmt.Sprintf("Workflow %q is invalid for task start", def.Workflow.Name), OccurredAtUnixMs: link.occurredAt})
	}
	return items, nil
}

func sortAttentionItems(items []serverapi.WorkflowAttentionItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].OccurredAtUnixMs != items[j].OccurredAtUnixMs {
			return items[i].OccurredAtUnixMs > items[j].OccurredAtUnixMs
		}
		return items[i].ID > items[j].ID
	})
}

func pageAttentionItems(items []serverapi.WorkflowAttentionItem, offset int, pageSize int) []serverapi.WorkflowAttentionItem {
	if offset >= len(items) {
		return []serverapi.WorkflowAttentionItem{}
	}
	end := offset + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func parseOffsetPageToken(token string) (int, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, errors.New("page_token is invalid")
	}
	return offset, nil
}

func projectBoardProject(project clientui.ProjectOverview) serverapi.ProjectBoardProject {
	return serverapi.ProjectBoardProject{ProjectID: project.Project.ProjectID, ProjectKey: project.Project.ProjectKey, DisplayName: project.Project.DisplayName}
}

func projectWorkspaceSummary(workspace clientui.ProjectWorkspaceSummary) serverapi.ProjectWorkspaceSummary {
	return serverapi.ProjectWorkspaceSummary{WorkspaceID: workspace.WorkspaceID, DisplayName: workspace.DisplayName, RootPath: workspace.RootPath, Availability: string(workspace.Availability), IsPrimary: workspace.IsPrimary, UpdatedAtUnixMs: workspace.UpdatedAt.UnixMilli()}
}

func boardProjectWorkspaceSummaries(project clientui.ProjectOverview) (serverapi.ProjectWorkspaceSummary, map[string]serverapi.ProjectWorkspaceSummary) {
	primaryWorkspace := serverapi.ProjectWorkspaceSummary{}
	workspacesByID := map[string]serverapi.ProjectWorkspaceSummary{}
	for _, workspace := range project.Workspaces {
		dto := projectWorkspaceSummary(workspace)
		workspacesByID[dto.WorkspaceID] = dto
		if workspace.IsPrimary {
			primaryWorkspace = dto
		}
	}
	return primaryWorkspace, workspacesByID
}

func sourceWorkspaceForTask(task sqlitegen.TaskRecord, workspacesByID map[string]serverapi.ProjectWorkspaceSummary, fallback serverapi.ProjectWorkspaceSummary) serverapi.ProjectWorkspaceSummary {
	if workspace, ok := workspacesByID[strings.TrimSpace(task.SourceWorkspaceID.String)]; ok {
		return workspace
	}
	snapshot := struct {
		SourceWorkspaceSnapshot struct {
			WorkspaceID string `json:"workspace_id"`
			DisplayName string `json:"display_name"`
			RootPath    string `json:"root_path"`
		} `json:"source_workspace_snapshot"`
	}{}
	if err := workflowjson.UnmarshalString(task.MetadataJson, &snapshot); err == nil {
		if strings.TrimSpace(snapshot.SourceWorkspaceSnapshot.RootPath) != "" {
			return serverapi.ProjectWorkspaceSummary{
				WorkspaceID:  strings.TrimSpace(snapshot.SourceWorkspaceSnapshot.WorkspaceID),
				DisplayName:  strings.TrimSpace(snapshot.SourceWorkspaceSnapshot.DisplayName),
				RootPath:     strings.TrimSpace(snapshot.SourceWorkspaceSnapshot.RootPath),
				Availability: "unlinked",
			}
		}
	}
	return fallback
}

func bodyPreview(body string) string {
	trimmed := strings.TrimSpace(body)
	const limit = 96
	if len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit]
}

func definitionForValidation(def serverapi.WorkflowDefinition) workflow.Definition {
	out := workflow.Definition{ID: workflow.WorkflowID(def.Workflow.ID), DisplayName: def.Workflow.Name}
	for _, node := range def.Nodes {
		fields := make([]workflow.OutputField, 0, len(node.OutputFields))
		for _, field := range node.OutputFields {
			fields = append(fields, workflow.OutputField{Name: field.Name, Description: field.Description})
		}
		out.Nodes = append(out.Nodes, workflow.Node{WorkflowID: workflow.WorkflowID(node.WorkflowID), ID: workflow.NodeID(node.ID), Key: workflow.ModelKey(node.Key), Kind: workflow.NodeKind(node.Kind), DisplayName: node.DisplayName, SubagentRole: node.SubagentRole, PromptTemplate: node.PromptTemplate, OutputFields: fields})
	}
	for _, group := range def.TransitionGroups {
		out.TransitionGroups = append(out.TransitionGroups, workflow.TransitionGroup{WorkflowID: workflow.WorkflowID(group.WorkflowID), ID: workflow.TransitionGroupID(group.ID), SourceNodeID: workflow.NodeID(group.SourceNodeID), TransitionID: workflow.TransitionID(group.TransitionID), DisplayName: group.DisplayName})
	}
	for _, edge := range def.Edges {
		inputs := make([]workflow.InputBinding, 0, len(edge.InputBindings))
		for _, input := range edge.InputBindings {
			inputs = append(inputs, workflow.InputBinding{Name: input.Name, Source: workflow.BindingSource(input.Source), Field: input.Field})
		}
		requirements := make([]workflow.OutputRequirement, 0, len(edge.OutputRequirements))
		for _, requirement := range edge.OutputRequirements {
			requirements = append(requirements, workflow.OutputRequirement{FieldName: requirement.FieldName})
		}
		out.Edges = append(out.Edges, workflow.Edge{WorkflowID: workflow.WorkflowID(edge.WorkflowID), ID: workflow.EdgeID(edge.ID), Key: workflow.ModelKey(edge.Key), TransitionGroupID: workflow.TransitionGroupID(edge.TransitionGroupID), TargetNodeID: workflow.NodeID(edge.TargetNodeID), RequiresApproval: edge.RequiresApproval, ContextMode: workflow.ContextMode(edge.ContextMode), ContextSource: workflow.CanonicalContextSource(workflow.ContextSource{Kind: workflow.ContextSourceKind(edge.ContextSource.Kind), NodeKey: workflow.ModelKey(edge.ContextSource.NodeKey)}), InputBindings: inputs, OutputRequirements: requirements})
	}
	return out
}

func validationErrors(workflowID string, errs []workflow.ValidationError) []serverapi.WorkflowValidationError {
	out := make([]serverapi.WorkflowValidationError, 0, len(errs))
	for _, err := range errs {
		out = append(out, serverapi.WorkflowValidationError{Code: string(err.Code), Message: err.Message, WorkflowID: workflowID, NodeID: string(err.NodeID), TransitionGroupID: string(err.TransitionGroupID), EdgeID: string(err.EdgeID), RelatedIDs: err.RelatedIDs, BlocksContext: err.BlocksContext})
	}
	return out
}

func selectWorkflow(picker []serverapi.WorkflowPickerItem, requested string) serverapi.WorkflowPickerItem {
	trimmed := strings.TrimSpace(requested)
	if trimmed != "" {
		for _, item := range picker {
			if item.WorkflowID == trimmed {
				return item
			}
		}
	}
	for _, item := range picker {
		if item.IsProjectDefault {
			return item
		}
	}
	if len(picker) == 0 {
		return serverapi.WorkflowPickerItem{}
	}
	return picker[0]
}

func boardGroups(def serverapi.WorkflowDefinition) []serverapi.WorkflowBoardGroup {
	groups := make([]serverapi.WorkflowBoardGroup, 0, len(def.NodeGroups))
	for _, group := range def.NodeGroups {
		dto := serverapi.WorkflowBoardGroup{GroupID: group.GroupID, Key: group.GroupKey, DisplayName: group.DisplayName, SortOrder: group.SortOrder}
		for _, node := range def.Nodes {
			if node.GroupID == group.GroupID && boardVisibleNodeKind(node.Kind) {
				dto.NodeIDs = append(dto.NodeIDs, node.ID)
			}
		}
		if len(dto.NodeIDs) == 0 {
			continue
		}
		groups = append(groups, dto)
	}
	return groups
}

func boardColumns(def serverapi.WorkflowDefinition) []serverapi.WorkflowBoardColumn {
	columns := make([]serverapi.WorkflowBoardColumn, 0, len(def.Nodes))
	for index, node := range def.Nodes {
		if !boardVisibleNodeKind(node.Kind) {
			continue
		}
		columns = append(columns, serverapi.WorkflowBoardColumn{
			Node: serverapi.WorkflowBoardNodeSummary{
				NodeID:                 node.ID,
				Key:                    node.Key,
				Kind:                   node.Kind,
				DisplayName:            node.DisplayName,
				AssigneeRole:           node.SubagentRole,
				SortOrder:              index,
				OutputFields:           node.OutputFields,
				TransitionOutputFields: boardTransitionOutputFields(def, node.ID),
			},
			GroupID:   node.GroupID,
			SortOrder: index,
			IsBacklog: node.Kind == string(workflow.NodeKindStart),
			IsDone:    node.Kind == string(workflow.NodeKindTerminal),
		})
	}
	return columns
}

func boardVisibleNodeKind(kind string) bool {
	return workflow.NodeKind(kind) != workflow.NodeKindJoin
}

func boardTransitionOutputFields(def serverapi.WorkflowDefinition, targetNodeID string) []serverapi.WorkflowOutputField {
	nodesByID := workflowNodeByID(def)
	groupsByID := workflowTransitionGroupByID(def)
	fields := make([]serverapi.WorkflowOutputField, 0)
	seen := map[string]bool{}
	for _, edge := range def.Edges {
		if edge.TargetNodeID != targetNodeID {
			continue
		}
		sourceOutputFields := map[string]serverapi.WorkflowOutputField{}
		if group, ok := groupsByID[edge.TransitionGroupID]; ok {
			if sourceNode, ok := nodesByID[group.SourceNodeID]; ok {
				for _, field := range sourceNode.OutputFields {
					name := strings.TrimSpace(field.Name)
					if name != "" {
						sourceOutputFields[name] = field
					}
				}
			}
		}
		for _, binding := range edge.InputBindings {
			if binding.Source != string(workflow.BindingSourceTransitionOutput) {
				continue
			}
			name := strings.TrimSpace(binding.Field)
			if name == "" || seen[name] {
				continue
			}
			field := sourceOutputFields[name]
			if strings.TrimSpace(field.Name) == "" {
				field = serverapi.WorkflowOutputField{Name: name}
			}
			fields = append(fields, field)
			seen[name] = true
		}
	}
	return fields
}

func (s *Service) taskCard(ctx context.Context, task sqlitegen.TaskRecord, placements []sqlitegen.TaskNodePlacementRecord, def serverapi.WorkflowDefinition, nodeKinds map[string]workflow.NodeKind, sourceWorkspace serverapi.ProjectWorkspaceSummary) (serverapi.WorkflowBoardTaskCard, bool, error) {
	summary := taskSummary(task, placements, nodeKinds)
	runs, err := s.queries.ListTaskRuns(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowBoardTaskCard{}, false, err
	}
	status, actions := taskStatusAndActions(task, summary, placements, runs, def, nodeKinds)
	return serverapi.WorkflowBoardTaskCard{TaskID: task.ID, ShortID: task.ShortID, Title: task.Title, BodyPreview: summary.BodyPreview, WorkflowID: task.WorkflowID, ActiveNodeIDs: summary.ActiveNodeIDs, SourceWorkspace: sourceWorkspace, Status: status, Actions: actions, UpdatedAtUnixMs: task.UpdatedAtUnixMs}, summary.Done, nil
}

func taskStatusAndActions(task sqlitegen.TaskRecord, summary serverapi.WorkflowTaskSummary, placements []sqlitegen.TaskNodePlacementRecord, runs []sqlitegen.TaskRunRecord, def serverapi.WorkflowDefinition, nodeKinds map[string]workflow.NodeKind) (serverapi.WorkflowTaskStatus, serverapi.WorkflowTaskActions) {
	status := serverapi.WorkflowTaskStatus{NodeIDs: summary.ActiveNodeIDs}
	actions := serverapi.WorkflowTaskActions{CanCancel: task.CanceledAtUnixMs == 0 && !summary.Done}
	runningRunIDs := []string{}
	interruptedRunIDs := []string{}
	waitingAskRunIDs := []string{}
	waitingApproval := false
	backlog := false
	for _, placement := range placements {
		if placement.State == "waiting_approval" {
			waitingApproval = true
		}
		if placement.State == "active" && nodeKinds[placement.NodeID] == workflow.NodeKindStart {
			backlog = true
		}
	}
	for _, run := range runs {
		if run.CompletedAtUnixMs != 0 {
			continue
		}
		if strings.TrimSpace(run.WaitingAskID) != "" {
			waitingAskRunIDs = append(waitingAskRunIDs, run.ID)
		}
		if run.InterruptedAtUnixMs != 0 {
			interruptedRunIDs = append(interruptedRunIDs, run.ID)
			continue
		}
		if run.StartedAtUnixMs != 0 {
			runningRunIDs = append(runningRunIDs, run.ID)
		}
	}
	actions.CanStart = task.CanceledAtUnixMs == 0 && backlog && !waitingApproval && len(runningRunIDs) == 0 && len(waitingAskRunIDs) == 0
	taskActive := task.CanceledAtUnixMs == 0
	if taskActive {
		actions.ManualMoveTargetNodeIDs = manualMoveTargetNodeIDs(def, placements, nodeKinds)
	}
	actions.CanInterrupt = taskActive && len(runningRunIDs) == 1
	actions.NeedsDetailForInterrupt = taskActive && len(runningRunIDs) > 1
	if actions.CanInterrupt {
		actions.InterruptRunID = runningRunIDs[0]
	}
	actions.CanResume = taskActive && len(interruptedRunIDs) == 1
	actions.NeedsDetailForResume = taskActive && len(interruptedRunIDs) > 1
	if actions.CanResume {
		actions.ResumeRunID = interruptedRunIDs[0]
	}
	switch {
	case task.CanceledAtUnixMs != 0:
		status.Kind = "canceled"
		status.Label = "Canceled"
		status.NativeState = "canceled"
	case summary.Done:
		status.Kind = "done"
		status.Label = "Done"
		status.NativeState = "terminal"
	case len(waitingAskRunIDs) > 0:
		status.Kind = "waiting_question"
		status.Label = "Question"
		status.NativeState = "waiting_ask"
		status.RunIDs = waitingAskRunIDs
		status.AttentionTypes = []string{"question"}
	case waitingApproval:
		status.Kind = "waiting_approval"
		status.Label = "Approval"
		status.NativeState = "waiting_approval"
		status.AttentionTypes = []string{"approval"}
	case len(interruptedRunIDs) > 0:
		status.Kind = "interrupted"
		status.Label = "Interrupted"
		status.NativeState = "interrupted"
		status.RunIDs = interruptedRunIDs
		status.AttentionTypes = []string{attentionKindInterruptedRun}
	case len(runningRunIDs) > 0:
		status.Kind = "running"
		status.Label = "Running"
		status.NativeState = "running"
		status.RunIDs = runningRunIDs
	case backlog:
		status.Kind = "backlog"
		status.Label = "Backlog"
		status.NativeState = "active"
	default:
		status.Kind = "active"
		status.Label = "Active"
		status.NativeState = "active"
	}
	return status, actions
}

func manualMoveTargetNodeIDs(def serverapi.WorkflowDefinition, placements []sqlitegen.TaskNodePlacementRecord, nodeKinds map[string]workflow.NodeKind) []string {
	activeNodeID := ""
	for _, placement := range placements {
		if placement.State != "active" {
			continue
		}
		if activeNodeID != "" {
			return []string{}
		}
		if nodeKinds[placement.NodeID] == workflow.NodeKindTerminal {
			return []string{}
		}
		activeNodeID = placement.NodeID
	}
	if activeNodeID == "" {
		return []string{}
	}
	groupIDs := map[string]bool{}
	for _, group := range def.TransitionGroups {
		if group.SourceNodeID == activeNodeID {
			groupIDs[group.ID] = true
		}
	}
	targets := []string{}
	seen := map[string]bool{}
	for _, node := range def.Nodes {
		if workflow.NodeKind(node.Kind) == workflow.NodeKindTerminal {
			seen[node.ID] = true
			targets = append(targets, node.ID)
		}
	}
	for _, edge := range def.Edges {
		if !groupIDs[edge.TransitionGroupID] || edge.RequiresApproval || len(edge.OutputRequirements) > 0 {
			continue
		}
		if !seen[edge.TargetNodeID] {
			seen[edge.TargetNodeID] = true
			targets = append(targets, edge.TargetNodeID)
		}
	}
	return targets
}

func pageCards(cards []serverapi.WorkflowBoardTaskCard, offset int, pageSize int) []serverapi.WorkflowBoardTaskCard {
	if offset >= len(cards) {
		return []serverapi.WorkflowBoardTaskCard{}
	}
	end := offset + pageSize
	if end > len(cards) {
		end = len(cards)
	}
	return cards[offset:end]
}

func applyColumnTaskCountsFromPlacements(columns []serverapi.WorkflowBoardColumn, tasks []sqlitegen.TaskRecord, placementsByTaskID map[string][]sqlitegen.TaskNodePlacementRecord, workflowID string, def serverapi.WorkflowDefinition, nodeKinds map[string]workflow.NodeKind) {
	indexByNodeID := map[string]int{}
	for index, column := range columns {
		indexByNodeID[column.Node.NodeID] = index
	}
	for _, task := range tasks {
		if task.WorkflowID != workflowID {
			continue
		}
		countedNodeIDs := map[string]bool{}
		for _, placement := range effectiveBoardPlacementsForTask(task, placementsByTaskID[task.ID], def, nodeKinds) {
			if countedNodeIDs[placement.NodeID] {
				continue
			}
			if index, ok := indexByNodeID[placement.NodeID]; ok {
				columns[index].TaskCount++
				countedNodeIDs[placement.NodeID] = true
			}
		}
	}
}

func activeBoardPlacements(placements []sqlitegen.TaskNodePlacementRecord) []sqlitegen.TaskNodePlacementRecord {
	active := make([]sqlitegen.TaskNodePlacementRecord, 0, len(placements))
	for _, placement := range placements {
		if placement.State != "active" && placement.State != "waiting_approval" {
			continue
		}
		active = append(active, placement)
	}
	return active
}

func effectiveBoardPlacementsForTask(task sqlitegen.TaskRecord, placements []sqlitegen.TaskNodePlacementRecord, def serverapi.WorkflowDefinition, nodeKinds map[string]workflow.NodeKind) []sqlitegen.TaskNodePlacementRecord {
	active := activeBoardPlacements(placements)
	if task.CanceledAtUnixMs == 0 {
		return active
	}
	terminalNodeID := canceledBoardTerminalNodeID(def)
	if terminalNodeID == "" {
		return active
	}
	terminalPlacements := make([]sqlitegen.TaskNodePlacementRecord, 0, len(active))
	for _, placement := range active {
		if nodeKinds[placement.NodeID] == workflow.NodeKindTerminal {
			terminalPlacements = append(terminalPlacements, placement)
		}
	}
	if len(terminalPlacements) > 0 {
		return terminalPlacements
	}
	return []sqlitegen.TaskNodePlacementRecord{{
		ID:              "",
		TaskID:          task.ID,
		NodeID:          terminalNodeID,
		State:           "active",
		CreatedAtUnixMs: task.UpdatedAtUnixMs,
		UpdatedAtUnixMs: task.UpdatedAtUnixMs,
	}}
}

func canceledBoardTerminalNodeID(def serverapi.WorkflowDefinition) string {
	fallback := ""
	for _, node := range def.Nodes {
		if workflow.NodeKind(node.Kind) != workflow.NodeKindTerminal {
			continue
		}
		if fallback == "" {
			fallback = node.ID
		}
		if node.Key == "done" {
			return node.ID
		}
	}
	return fallback
}

func taskBelongsToBoardNode(task sqlitegen.TaskRecord, placements []sqlitegen.TaskNodePlacementRecord, def serverapi.WorkflowDefinition, nodeKinds map[string]workflow.NodeKind, nodeID string) bool {
	for _, placement := range effectiveBoardPlacementsForTask(task, placements, def, nodeKinds) {
		if placement.NodeID == nodeID {
			return true
		}
	}
	return false
}

type boardNodeCardsPageCursor struct {
	projectID       string
	workflowID      string
	nodeID          string
	updatedAtUnixMs int64
	taskID          string
	hasValue        bool
}

type boardNodeCardsPageTokenPayload struct {
	Version         int    `json:"version"`
	ProjectID       string `json:"project_id"`
	WorkflowID      string `json:"workflow_id"`
	NodeID          string `json:"node_id"`
	UpdatedAtUnixMs int64  `json:"updated_at_unix_ms"`
	TaskID          string `json:"task_id"`
}

func parseBoardNodeCardsPageToken(token string, projectID string, workflowID string, nodeID string) (boardNodeCardsPageCursor, error) {
	if strings.TrimSpace(token) == "" {
		return boardNodeCardsPageCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return boardNodeCardsPageCursor{}, errors.New("page_token is invalid")
	}
	var payload boardNodeCardsPageTokenPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return boardNodeCardsPageCursor{}, errors.New("page_token is invalid")
	}
	if payload.Version != 1 || payload.ProjectID != projectID || payload.WorkflowID != workflowID || payload.NodeID != nodeID || strings.TrimSpace(payload.TaskID) == "" || payload.UpdatedAtUnixMs < 0 {
		return boardNodeCardsPageCursor{}, errors.New("page_token is invalid")
	}
	return boardNodeCardsPageCursor{projectID: payload.ProjectID, workflowID: payload.WorkflowID, nodeID: payload.NodeID, updatedAtUnixMs: payload.UpdatedAtUnixMs, taskID: payload.TaskID, hasValue: true}, nil
}

func boardNodeCardsPageToken(projectID string, workflowID string, nodeID string, task sqlitegen.TaskRecord) string {
	payload := boardNodeCardsPageTokenPayload{Version: 1, ProjectID: projectID, WorkflowID: workflowID, NodeID: nodeID, UpdatedAtUnixMs: task.UpdatedAtUnixMs, TaskID: task.ID}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func apiContextSource(in workflow.ContextSource) serverapi.WorkflowContextSource {
	source := workflow.CanonicalContextSource(in)
	return serverapi.WorkflowContextSource{Kind: string(source.Kind), NodeKey: string(source.NodeKey)}
}

func boardNodeCardIsAfterCursor(task sqlitegen.TaskRecord, cursor boardNodeCardsPageCursor) bool {
	if task.UpdatedAtUnixMs < cursor.updatedAtUnixMs {
		return true
	}
	if task.UpdatedAtUnixMs > cursor.updatedAtUnixMs {
		return false
	}
	return task.ID < cursor.taskID
}

func sortBoardNodeTasks(tasks []sqlitegen.TaskRecord) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].UpdatedAtUnixMs != tasks[j].UpdatedAtUnixMs {
			return tasks[i].UpdatedAtUnixMs > tasks[j].UpdatedAtUnixMs
		}
		return tasks[i].ID > tasks[j].ID
	})
}
