package workflowview

import (
	"context"
	"database/sql"
	"errors"
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
)

type Service struct {
	metadata *metadata.Store
	queries  *sqlitegen.Queries
}

func New(metadataStore *metadata.Store) (*Service, error) {
	if metadataStore == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	return &Service{metadata: metadataStore, queries: metadataStore.Queries()}, nil
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
	primaryWorkspace := serverapi.ProjectWorkspaceSummary{}
	workspacesByID := map[string]serverapi.ProjectWorkspaceSummary{}
	for _, workspace := range project.Workspaces {
		dto := projectWorkspaceSummary(workspace)
		workspacesByID[dto.WorkspaceID] = dto
		if workspace.IsPrimary {
			primaryWorkspace = dto
		}
	}
	placementsByTaskID, err := s.boardPlacementsByTask(ctx, tasks)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	workflowIDs := make([]string, 0, len(links)+len(tasks))
	seen := map[string]bool{}
	linkByWorkflowID := map[string]sqlitegen.ProjectWorkflowLink{}
	for _, link := range links {
		if link.UnlinkedAtUnixMs == 0 {
			linkByWorkflowID[link.WorkflowID] = link
		}
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
		validation := workflow.ValidateDefinition(definitionForValidation(def), workflow.ValidationOptions{Context: workflow.ValidationContextTaskCreation, RoleResolver: roleResolver})
		picker = append(picker, serverapi.WorkflowPickerItem{
			WorkflowID:           workflowID,
			DisplayName:          def.Workflow.Name,
			Description:          def.Workflow.Description,
			GraphRevision:        def.Workflow.GraphRevision,
			IsProjectDefault:     link.ID != "" && link.IsDefault != 0,
			ValidForTaskCreation: validation.Valid(),
			ValidationErrors:     validationErrors(def.Workflow.ID, validation.Errors),
			UnlinkedAtUnixMs:     link.UnlinkedAtUnixMs,
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
		latestSequence, err := s.latestEventSequence(ctx, projectID)
		if err != nil {
			return serverapi.WorkflowBoard{}, err
		}
		return serverapi.WorkflowBoard{ProjectID: projectID, Project: projectBoardProject(project), WorkflowPicker: picker, GeneratedAtUnixMs: time.Now().UTC().UnixMilli(), LatestEventSequence: latestSequence}, nil
	}
	def := definitions[selected.WorkflowID]
	nodeKinds := nodeKindsByWorkflowID[selected.WorkflowID]
	groups := boardGroups(def)
	columns := boardColumns(def)
	cards := make([]serverapi.WorkflowBoardTaskCard, 0)
	donePreview := make([]serverapi.WorkflowBoardTaskCard, 0)
	for _, task := range tasks {
		if task.WorkflowID != selected.WorkflowID {
			continue
		}
		card, done, err := s.taskCard(ctx, task, placementsByTaskID[task.ID], nodeKinds, sourceWorkspaceForTask(task, workspacesByID, primaryWorkspace))
		if err != nil {
			return serverapi.WorkflowBoard{}, err
		}
		if done {
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
	applyColumnTaskCounts(columns, cards, donePreview)
	latestSequence, err := s.latestEventSequence(ctx, projectID)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	board := serverapi.WorkflowBoard{
		ProjectID:           projectID,
		Project:             projectBoardProject(project),
		SelectedWorkflow:    selected,
		WorkflowPicker:      picker,
		Groups:              groups,
		Columns:             columns,
		Cards:               cards,
		DonePreview:         donePreview,
		NextPageToken:       nextPageToken,
		GeneratedAtUnixMs:   time.Now().UTC().UnixMilli(),
		LatestEventSequence: latestSequence,
	}
	return board, nil
}

func (s *Service) boardPlacementsByTask(ctx context.Context, tasks []sqlitegen.Task) (map[string][]sqlitegen.TaskNodePlacement, error) {
	if len(tasks) == 0 {
		return map[string][]sqlitegen.TaskNodePlacement{}, nil
	}
	taskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.ID)
	}
	placements, err := s.queries.ListTaskNodePlacementsByTasks(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	byTaskID := make(map[string][]sqlitegen.TaskNodePlacement, len(tasks))
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
	_, nodeKinds, err := s.definition(ctx, task.WorkflowID)
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
	comments, err := s.queries.ListTaskComments(ctx, sqlitegen.ListTaskCommentsParams{TaskID: task.ID, IncludeDeleted: int64(0)})
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
	detail := serverapi.WorkflowTaskDetail{Summary: taskSummary(task, placements, nodeKinds), Body: task.Body, SourceURL: task.SourceUrl, SourceWorkspace: sourceWorkspaceForTask(task, workspacesByID, primaryWorkspace)}
	for _, placement := range placements {
		detail.Placements = append(detail.Placements, placementDTO(placement))
	}
	for _, run := range runs {
		detail.Runs = append(detail.Runs, serverapi.WorkflowRun{ID: run.ID, TaskID: run.TaskID, PlacementID: run.PlacementID, NodeID: run.NodeID, SessionID: run.SessionID.String, Generation: run.RunGeneration, StartedAtUnixMs: run.StartedAtUnixMs, CompletedAtUnixMs: run.CompletedAtUnixMs, InterruptedAtUnixMs: run.InterruptedAtUnixMs, InterruptionReason: run.InterruptionReason, WaitingAskID: run.WaitingAskID})
	}
	for _, transition := range transitions {
		outputs := map[string]string{}
		if err := workflowjson.UnmarshalString(transition.OutputValuesJson, &outputs); err != nil {
			return serverapi.WorkflowTaskDetail{}, err
		}
		dto := serverapi.WorkflowTaskTransition{ID: transition.ID, TaskID: transition.TaskID, TransitionID: transition.TransitionID, State: transition.State, Commentary: transition.Commentary, OutputValues: outputs, CreatedAt: transition.CreatedAtUnixMs}
		edges, err := s.queries.ListTaskTransitionEdges(ctx, transition.ID)
		if err != nil {
			return serverapi.WorkflowTaskDetail{}, err
		}
		for _, edge := range edges {
			dto.Edges = append(dto.Edges, serverapi.WorkflowTransitionEdge{ID: edge.ID, TaskTransitionID: edge.TaskTransitionID, WorkflowEdgeID: edge.WorkflowEdgeID.String, EdgeKey: edge.EdgeKey, TargetNodeID: edge.TargetNodeID.String, TargetPlacementID: edge.TargetPlacementID.String, State: edge.State, WorkflowRevisionSeen: edge.WorkflowRevisionSeen})
		}
		detail.Transitions = append(detail.Transitions, dto)
	}
	for _, comment := range comments {
		detail.Comments = append(detail.Comments, commentDTO(comment))
	}
	return detail, nil
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
	def := serverapi.WorkflowDefinition{Workflow: serverapi.WorkflowRecord{ID: row.ID, Name: row.Name, Description: row.Description, GraphRevision: row.GraphRevision}}
	nodeGroups, err := s.queries.ListWorkflowNodeGroups(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	groupByID := map[string]serverapi.WorkflowNodeGroup{}
	for _, group := range nodeGroups {
		dto := serverapi.WorkflowNodeGroup{GroupID: group.ID, WorkflowID: group.WorkflowID, GroupKey: group.GroupKey, DisplayName: group.DisplayName, SortOrder: int(group.SortOrder), MetadataJSON: group.MetadataJson}
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
		def.Edges = append(def.Edges, serverapi.WorkflowEdge{ID: edge.ID, WorkflowID: edge.WorkflowID, TransitionGroupID: edge.TransitionGroupID, Key: edge.EdgeKey, TargetNodeID: edge.TargetNodeID, RequiresApproval: edge.RequiresApproval != 0, ContextMode: edge.ContextMode, InputBindings: inputs, OutputRequirements: requirements})
	}
	return def, nodeKinds, nil
}

func taskSummary(task sqlitegen.Task, placements []sqlitegen.TaskNodePlacement, nodeKinds map[string]workflow.NodeKind) serverapi.WorkflowTaskSummary {
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

func placementDTO(placement sqlitegen.TaskNodePlacement) serverapi.WorkflowPlacement {
	return serverapi.WorkflowPlacement{ID: placement.ID, TaskID: placement.TaskID, NodeID: placement.NodeID, State: placement.State, ParallelBatchTransitionID: strings.TrimSpace(placement.ParallelBatchTransitionID.String), ParallelBranchEdgeID: strings.TrimSpace(placement.ParallelBranchEdgeID.String)}
}

func commentDTO(comment sqlitegen.TaskComment) serverapi.WorkflowTaskComment {
	return serverapi.WorkflowTaskComment{ID: comment.ID, TaskID: comment.TaskID, Body: comment.Body, Author: comment.AuthorKind, AuthorID: comment.AuthorID, DeletedAt: comment.DeletedAtUnixMs, UpdatedAt: comment.UpdatedAtUnixMs}
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

func sourceWorkspaceForTask(task sqlitegen.Task, workspacesByID map[string]serverapi.ProjectWorkspaceSummary, fallback serverapi.ProjectWorkspaceSummary) serverapi.ProjectWorkspaceSummary {
	if workspace, ok := workspacesByID[strings.TrimSpace(task.SourceWorkspaceID.String)]; ok {
		return workspace
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
		out.Edges = append(out.Edges, workflow.Edge{WorkflowID: workflow.WorkflowID(edge.WorkflowID), ID: workflow.EdgeID(edge.ID), Key: workflow.ModelKey(edge.Key), TransitionGroupID: workflow.TransitionGroupID(edge.TransitionGroupID), TargetNodeID: workflow.NodeID(edge.TargetNodeID), RequiresApproval: edge.RequiresApproval, ContextMode: workflow.ContextMode(edge.ContextMode), InputBindings: inputs, OutputRequirements: requirements})
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
			if node.GroupID == group.GroupID {
				dto.NodeIDs = append(dto.NodeIDs, node.ID)
			}
		}
		groups = append(groups, dto)
	}
	return groups
}

func boardColumns(def serverapi.WorkflowDefinition) []serverapi.WorkflowBoardColumn {
	columns := make([]serverapi.WorkflowBoardColumn, 0, len(def.Nodes))
	for index, node := range def.Nodes {
		columns = append(columns, serverapi.WorkflowBoardColumn{
			Node:      serverapi.WorkflowBoardNodeSummary{NodeID: node.ID, Key: node.Key, Kind: node.Kind, DisplayName: node.DisplayName, AssigneeRole: node.SubagentRole, SortOrder: index},
			GroupID:   node.GroupID,
			SortOrder: index,
			IsBacklog: node.Kind == string(workflow.NodeKindStart),
			IsDone:    node.Kind == string(workflow.NodeKindTerminal),
		})
	}
	return columns
}

func (s *Service) taskCard(ctx context.Context, task sqlitegen.Task, placements []sqlitegen.TaskNodePlacement, nodeKinds map[string]workflow.NodeKind, sourceWorkspace serverapi.ProjectWorkspaceSummary) (serverapi.WorkflowBoardTaskCard, bool, error) {
	summary := taskSummary(task, placements, nodeKinds)
	runs, err := s.queries.ListTaskRuns(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowBoardTaskCard{}, false, err
	}
	status, actions := taskStatusAndActions(task, summary, placements, runs, nodeKinds)
	return serverapi.WorkflowBoardTaskCard{TaskID: task.ID, ShortID: task.ShortID, Title: task.Title, BodyPreview: summary.BodyPreview, WorkflowID: task.WorkflowID, ActiveNodeIDs: summary.ActiveNodeIDs, SourceWorkspace: sourceWorkspace, Status: status, Actions: actions, UpdatedAtUnixMs: task.UpdatedAtUnixMs}, summary.Done, nil
}

func taskStatusAndActions(task sqlitegen.Task, summary serverapi.WorkflowTaskSummary, placements []sqlitegen.TaskNodePlacement, runs []sqlitegen.TaskRun, nodeKinds map[string]workflow.NodeKind) (serverapi.WorkflowTaskStatus, serverapi.WorkflowTaskActions) {
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
	actions.CanStart = task.CanceledAtUnixMs == 0 && backlog && len(runs) == 0
	actions.CanInterrupt = len(runningRunIDs) == 1
	actions.NeedsDetailForInterrupt = len(runningRunIDs) > 1
	if actions.CanInterrupt {
		actions.InterruptRunID = runningRunIDs[0]
	}
	actions.CanResume = len(interruptedRunIDs) == 1
	actions.NeedsDetailForResume = len(interruptedRunIDs) > 1
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
		status.AttentionTypes = []string{"interrupted"}
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

func applyColumnTaskCounts(columns []serverapi.WorkflowBoardColumn, cards []serverapi.WorkflowBoardTaskCard, donePreview []serverapi.WorkflowBoardTaskCard) {
	indexByNodeID := map[string]int{}
	for index, column := range columns {
		indexByNodeID[column.Node.NodeID] = index
	}
	for _, card := range append(cards, donePreview...) {
		for _, nodeID := range card.ActiveNodeIDs {
			if index, ok := indexByNodeID[nodeID]; ok {
				columns[index].TaskCount++
			}
		}
	}
}

func (s *Service) latestEventSequence(ctx context.Context, projectID string) (int64, error) {
	value, err := s.queries.GetLatestWorkflowEventSequence(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return 0, err
	}
	return int64FromDBValue(value), nil
}

func int64FromDBValue(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case []byte:
		parsed, _ := strconv.ParseInt(string(typed), 10, 64)
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	default:
		return 0
	}
}
