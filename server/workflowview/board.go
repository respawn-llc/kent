package workflowview

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type Board struct {
	metadata     *metadata.Store
	queries      *sqlitegen.Queries
	definitions  *DefinitionProjection
	roleResolver workflow.RoleResolver
	projection   *TaskStatusProjection
}

func NewBoard(metadataStore *metadata.Store, definitions *DefinitionProjection, roleResolver workflow.RoleResolver, projection *TaskStatusProjection) (*Board, error) {
	if metadataStore == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	if definitions == nil {
		return nil, errors.New("definition projection is required")
	}
	if roleResolver == nil {
		return nil, errors.New("role resolver is required")
	}
	if projection == nil {
		return nil, errors.New("task status projection is required")
	}
	return &Board{
		metadata:     metadataStore,
		queries:      metadataStore.Queries(),
		definitions:  definitions,
		roleResolver: roleResolver,
		projection:   projection,
	}, nil
}

func (b *Board) Get(ctx context.Context, req serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoard, error) {
	if b == nil {
		return serverapi.WorkflowBoard{}, errors.New("board is required")
	}
	if err := req.ValidateRPC(); err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return serverapi.WorkflowBoard{}, errors.New("project_id is required")
	}
	definitions, picker, err := b.selectionInputs(ctx, projectID)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	project, err := projectWorkspaceFacts(ctx, b.queries, projectID)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	labelFilter, err := resolveWorkflowTaskLabelFilter(ctx, b.queries, projectID, req.LabelFilter)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	selected := workflowBoardSelectorFromRequest(req).selectFrom(picker)
	if selected == nil {
		return serverapi.WorkflowBoard{
			ProjectID:         projectID,
			Project:           project,
			WorkflowPicker:    picker,
			GeneratedAtUnixMs: time.Now().UTC().UnixMilli(),
		}, nil
	}
	snapshot := definitions[selected.WorkflowID]
	selectedWorkflowID := selected.WorkflowID
	groups := boardGroups(snapshot.api)
	columns, err := boardColumns(snapshot)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	if err := b.applyColumnTaskCounts(ctx, columns, projectID, selectedWorkflowID, labelFilter, req.DependencyFilter); err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	return serverapi.WorkflowBoard{
		ProjectID:         projectID,
		Project:           project,
		SelectedWorkflow:  selected,
		WorkflowPicker:    picker,
		Groups:            groups,
		Columns:           columns,
		GeneratedAtUnixMs: time.Now().UTC().UnixMilli(),
	}, nil
}

func (b *Board) ListNodeCards(ctx context.Context, req serverapi.WorkflowBoardNodeCardsListRequest) (serverapi.WorkflowBoardNodeCardsListResponse, error) {
	if b == nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, errors.New("board is required")
	}
	if err := req.ValidateRPC(); err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	projectID := strings.TrimSpace(req.ProjectID)
	workflowID := req.WorkflowID
	nodeID := strings.TrimSpace(req.NodeID)
	project, err := projectWorkspaceFacts(ctx, b.queries, projectID)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	labelFilter, err := resolveWorkflowTaskLabelFilter(ctx, b.queries, projectID, req.LabelFilter)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	labelFilterArgs, err := labelFilter.queryArgs()
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = serverapi.WorkflowBoardNodeCardsMaxPageSize
	}
	sortSelection := req.Sort
	if sortSelection == nil {
		sortSelection = &serverapi.WorkflowTaskListSort{
			Field:     serverapi.WorkflowTaskListSortFieldUpdated,
			Direction: serverapi.WorkflowTaskListSortDirectionDesc,
		}
	}
	offset := 0
	if req.Offset != nil {
		offset = *req.Offset
	}
	definition, err := b.projection.definition(ctx, b.queries, workflowID)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	if _, ok := workflowNodesByID(definition.api)[nodeID]; !ok {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, errors.New("node_id is invalid for workflow")
	}
	rows, err := b.queries.ListBoardNodeTasks(ctx, sqlitegen.ListBoardNodeTasksParams{
		ProjectID:            projectID,
		WorkflowID:           workflowID,
		NodeID:               nodeID,
		LabelFilterKind:      labelFilterArgs.kind,
		LabelFilterMode:      labelFilterArgs.mode,
		LabelIdsJson:         labelFilterArgs.labelIDsJSON,
		ExcludedLabelIdsJson: labelFilterArgs.excludedLabelIDsJSON,
		DependencyFilter:     workflowTaskDependencyFilterQueryArg(req.DependencyFilter),
		SortField:            string(sortSelection.Field),
		SortDirection:        string(sortSelection.Direction),
		OffsetRows:           int64(offset),
		LimitRows:            int64(pageSize),
	})
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	dependencyProgressByTaskID, err := boardDependencyProgressByTaskID(rows)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	tasks := boardNodeTaskRecords(rows)
	hasExtra := len(tasks) > pageSize
	if hasExtra {
		tasks = tasks[:pageSize]
	}
	projectTaskIDs := workflowTaskIDs(taskIDs(tasks))
	observation, err := b.projection.Observe(projectTaskIDs)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	projectedByTaskID, err := b.projection.Project(ctx, observation, b.queries, projectTaskIDs)
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	labelsByTask, err := loadTaskLabelsByTask(ctx, b.queries, taskIDs(tasks))
	if err != nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, err
	}
	labelIDsByTask := taskLabelIDsByTask(labelsByTask)
	cards := make([]serverapi.WorkflowBoardTaskCard, 0, len(tasks))
	for _, task := range tasks {
		projected, exists := projectedByTaskID[workflow.TaskID(task.ID)]
		if !exists {
			return serverapi.WorkflowBoardNodeCardsListResponse{}, fmt.Errorf("task status projection omitted Task %q", task.ID)
		}
		source, err := taskSourceWorkspace(ctx, b.queries, task, project.DefaultWorkspaceID)
		if err != nil {
			return serverapi.WorkflowBoardNodeCardsListResponse{}, err
		}
		card, _ := b.card(
			projected,
			labelIDsByTask[task.ID],
			source,
			dependencyProgressByTaskID[task.ID],
		)
		cards = append(cards, card)
	}
	var nextOffset *int
	if hasExtra {
		next := offset + pageSize
		nextOffset = &next
	}
	return serverapi.WorkflowBoardNodeCardsListResponse{
		ProjectID:         projectID,
		WorkflowID:        workflowID,
		NodeID:            nodeID,
		Cards:             cards,
		NextOffset:        nextOffset,
		GeneratedAtUnixMs: time.Now().UTC().UnixMilli(),
	}, nil
}

func (b *Board) card(projected TaskStatusProjectionResult, labelIDs []string, sourceWorkspace serverapi.ProjectWorkspaceSummary, dependencyProgress *serverapi.WorkflowTaskDependencyProgress) (serverapi.WorkflowBoardTaskCard, bool) {
	task := projected.Task
	return serverapi.WorkflowBoardTaskCard{
		TaskID:             task.ID,
		ShortID:            task.ShortID,
		Title:              task.Title,
		Preview:            markdownPreview(task.Body),
		WorkflowID:         task.WorkflowID,
		ActiveNodeIDs:      append([]string(nil), projected.Status.NodeIDs...),
		SourceWorkspace:    sourceWorkspace,
		Status:             projected.Status,
		Actions:            projected.Actions,
		LabelIDs:           labelIDs,
		DependencyProgress: dependencyProgress,
		UpdatedAtUnixMs:    task.UpdatedAtUnixMs,
	}, projected.Done
}

type taskDependencyProgressRow struct {
	taskID         string
	satisfiedCount int64
	totalCount     int64
}

func projectTaskDependencyProgress(rows []taskDependencyProgressRow) (map[string]*serverapi.WorkflowTaskDependencyProgress, error) {
	progress := make(map[string]*serverapi.WorkflowTaskDependencyProgress)
	for _, row := range rows {
		if row.totalCount < 1 || row.satisfiedCount < 0 || row.satisfiedCount > row.totalCount {
			return nil, fmt.Errorf("task %q dependency aggregate is invalid: satisfied=%d total=%d", row.taskID, row.satisfiedCount, row.totalCount)
		}
		progress[row.taskID] = &serverapi.WorkflowTaskDependencyProgress{
			SatisfiedCount: int(row.satisfiedCount),
			TotalCount:     int(row.totalCount),
		}
	}
	return progress, nil
}

func boardDependencyProgressByTaskID(rows []sqlitegen.ListBoardNodeTasksRow) (map[string]*serverapi.WorkflowTaskDependencyProgress, error) {
	progressRows := make([]taskDependencyProgressRow, 0, len(rows))
	for _, row := range rows {
		if row.DependencySatisfiedCount.Valid != row.DependencyTotalCount.Valid {
			return nil, fmt.Errorf("board task %q dependency aggregate has inconsistent absence", row.ID)
		}
		if !row.DependencyTotalCount.Valid {
			continue
		}
		progressRows = append(progressRows, taskDependencyProgressRow{
			taskID:         row.ID,
			satisfiedCount: row.DependencySatisfiedCount.Int64,
			totalCount:     row.DependencyTotalCount.Int64,
		})
	}
	return projectTaskDependencyProgress(progressRows)
}

func taskIDs(tasks []sqlitegen.TaskRecord) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func workflowTaskIDs(taskIDs []string) []workflow.TaskID {
	ids := make([]workflow.TaskID, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		ids = append(ids, workflow.TaskID(taskID))
	}
	return ids
}

func boardNodeTaskRecords(rows []sqlitegen.ListBoardNodeTasksRow) []sqlitegen.TaskRecord {
	tasks := make([]sqlitegen.TaskRecord, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, sqlitegen.TaskRecord{
			ID:                          row.ID,
			ProjectID:                   row.ProjectID,
			ProjectWorkflowLinkID:       row.ProjectWorkflowLinkID,
			WorkflowID:                  row.WorkflowID,
			WorkflowRevisionSeen:        row.WorkflowRevisionSeen,
			TaskSeq:                     row.TaskSeq,
			ShortID:                     row.ShortID,
			Title:                       row.Title,
			Body:                        row.Body,
			SourceUrl:                   row.SourceUrl,
			SourceWorkspaceID:           row.SourceWorkspaceID,
			ManagedWorktreeID:           row.ManagedWorktreeID,
			ExecutionTargetMode:         row.ExecutionTargetMode,
			ExecutionTargetRequestedRef: row.ExecutionTargetRequestedRef,
			ExecutionTargetResolvedRef:  row.ExecutionTargetResolvedRef,
			ExecutionTargetCommitOid:    row.ExecutionTargetCommitOid,
			ExecutionTargetProvenance:   row.ExecutionTargetProvenance,
			CreatedAtUnixMs:             row.CreatedAtUnixMs,
			UpdatedAtUnixMs:             row.UpdatedAtUnixMs,
			MetadataJson:                row.MetadataJson,
		})
	}
	return tasks
}

func (b *Board) selectionInputs(ctx context.Context, projectID string) (map[runtimeids.WorkflowID]definitionSnapshot, []serverapi.WorkflowPickerItem, error) {
	links, err := b.queries.ListProjectWorkflowLinks(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	taskActivityRows, err := b.queries.ListProjectWorkflowTaskActivity(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	workflowIDs := make([]runtimeids.WorkflowID, 0, len(links))
	linkByWorkflowID := map[runtimeids.WorkflowID]sqlitegen.ProjectWorkflowLinkRecord{}
	for _, link := range links {
		if _, exists := linkByWorkflowID[link.WorkflowID]; exists {
			continue
		}
		linkByWorkflowID[link.WorkflowID] = link
		workflowIDs = append(workflowIDs, link.WorkflowID)
	}
	activityByWorkflowID := map[runtimeids.WorkflowID]int64{}
	for _, activity := range taskActivityRows {
		if _, linked := linkByWorkflowID[activity.WorkflowID]; linked {
			activityByWorkflowID[activity.WorkflowID] = activity.LatestUpdatedAtUnixMs
		}
	}
	definitions := make(map[runtimeids.WorkflowID]definitionSnapshot, len(workflowIDs))
	picker := make([]serverapi.WorkflowPickerItem, 0, len(workflowIDs))
	for _, workflowID := range workflowIDs {
		snapshot, err := b.definitions.snapshot(ctx, workflowID)
		if err != nil {
			return nil, nil, err
		}
		definitions[workflowID] = snapshot
		link, linked := linkByWorkflowID[workflowID]
		if !linked {
			return nil, nil, fmt.Errorf("workflow selection invariant violated: active link missing for project_id=%q workflow_id=%q", projectID, workflowID)
		}
		validation := definitionExecutionValidation(snapshot.domain, b.roleResolver)
		diagnostics, err := ValidationErrors(&workflowID, validation.Errors)
		if err != nil {
			return nil, nil, err
		}
		taskDiagnostics, err := protoapi.WorkflowValidationErrorsForJSON(diagnostics)
		if err != nil {
			return nil, nil, err
		}
		picker = append(picker, serverapi.WorkflowPickerItem{
			WorkflowID:           workflowID,
			DisplayName:          snapshot.api.Workflow.Name,
			Description:          snapshot.api.Workflow.Description,
			Version:              snapshot.api.Workflow.Version,
			IsProjectDefault:     link.IsDefault != 0,
			ValidForTaskCreation: !validation.HasBlockingErrors(),
			ValidationErrors:     taskDiagnostics,
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
	return definitions, picker, nil
}

func (b *Board) applyColumnTaskCounts(ctx context.Context, columns []serverapi.WorkflowBoardColumn, projectID string, workflowID runtimeids.WorkflowID, labelFilter workflowTaskLabelFilterFacts, dependencyFilter *bool) error {
	labelFilterArgs, err := labelFilter.queryArgs()
	if err != nil {
		return err
	}
	rows, err := b.queries.ListBoardColumnTaskCounts(ctx, sqlitegen.ListBoardColumnTaskCountsParams{
		ProjectID:            projectID,
		WorkflowID:           workflowID,
		LabelFilterKind:      labelFilterArgs.kind,
		LabelFilterMode:      labelFilterArgs.mode,
		LabelIdsJson:         labelFilterArgs.labelIDsJSON,
		ExcludedLabelIdsJson: labelFilterArgs.excludedLabelIDsJSON,
		DependencyFilter:     workflowTaskDependencyFilterQueryArg(dependencyFilter),
	})
	if err != nil {
		return err
	}
	indexByNodeID := map[string]int{}
	for index, column := range columns {
		indexByNodeID[column.Node.NodeID] = index
	}
	for _, row := range rows {
		nodeID := strings.TrimSpace(row.NodeID)
		if nodeID == "" {
			continue
		}
		if index, ok := indexByNodeID[nodeID]; ok {
			columns[index].TaskCount = int(row.TaskCount)
		}
	}
	return nil
}

type workflowBoardSelector interface {
	selectFrom(picker []serverapi.WorkflowPickerItem) *serverapi.WorkflowPickerItem
}

type workflowBoardDefaultSelector struct{}

func (workflowBoardDefaultSelector) selectFrom(picker []serverapi.WorkflowPickerItem) *serverapi.WorkflowPickerItem {
	return defaultWorkflowBoardSelection(picker)
}

type workflowBoardExplicitSelector struct {
	workflowID runtimeids.WorkflowID
}

func (s workflowBoardExplicitSelector) selectFrom(picker []serverapi.WorkflowPickerItem) *serverapi.WorkflowPickerItem {
	if selected := exactWorkflowBoardSelection(picker, s.workflowID); selected != nil {
		return selected
	}
	return defaultWorkflowBoardSelection(picker)
}

func workflowBoardSelectorFromRequest(req serverapi.WorkflowBoardRequest) workflowBoardSelector {
	if req.WorkflowID != nil {
		return workflowBoardExplicitSelector{workflowID: *req.WorkflowID}
	}
	return workflowBoardDefaultSelector{}
}

func exactWorkflowBoardSelection(picker []serverapi.WorkflowPickerItem, workflowID runtimeids.WorkflowID) *serverapi.WorkflowPickerItem {
	for index := range picker {
		if picker[index].WorkflowID == workflowID {
			return &picker[index]
		}
	}
	return nil
}

func defaultWorkflowBoardSelection(picker []serverapi.WorkflowPickerItem) *serverapi.WorkflowPickerItem {
	for index := range picker {
		if picker[index].IsProjectDefault {
			return &picker[index]
		}
	}
	if len(picker) == 0 {
		return nil
	}
	return &picker[0]
}

func boardGroups(def *pb.WorkflowDefinition) []serverapi.WorkflowBoardGroup {
	columnNodes := boardColumnNodes(def)
	groups := make([]serverapi.WorkflowBoardGroup, 0, len(def.NodeGroups))
	for _, group := range def.NodeGroups {
		dto := serverapi.WorkflowBoardGroup{
			GroupID:     group.GroupId,
			Key:         group.GroupKey,
			DisplayName: group.DisplayName,
			SortOrder:   int(group.SortOrder),
		}
		for _, node := range columnNodes {
			if node.GroupId != nil && *node.GroupId == group.GroupId {
				dto.NodeIDs = append(dto.NodeIDs, node.Id)
			}
		}
		if len(dto.NodeIDs) > 0 {
			groups = append(groups, dto)
		}
	}
	return groups
}

func boardColumns(snapshot definitionSnapshot) ([]serverapi.WorkflowBoardColumn, error) {
	columns := make([]serverapi.WorkflowBoardColumn, 0, len(snapshot.api.Nodes))
	derived := workflow.DeriveWiring(snapshot.domain)
	for index, node := range boardColumnNodes(snapshot.api) {
		kind, err := protoapi.WorkflowNodeKind.Decode(node.Kind)
		if err != nil {
			return nil, err
		}
		columns = append(columns, serverapi.WorkflowBoardColumn{
			Node: serverapi.WorkflowBoardNodeSummary{
				NodeID:       node.Id,
				Key:          node.Key,
				Kind:         kind,
				DisplayName:  node.DisplayName,
				AssigneeRole: node.GetSubagentRole(),
				SortOrder:    index,
				OutputFields: protoapi.WorkflowOutputFieldsForJSON(OutputFields(derived.PossibleProvisionFieldsForNode(workflow.NodeID(node.Id)))),
			},
			GroupID:   node.GroupId,
			SortOrder: index,
			IsBacklog: node.Kind == pb.NodeKind_WORKFLOW_NODE_KIND_START,
			IsDone:    node.Kind == pb.NodeKind_WORKFLOW_NODE_KIND_TERMINAL,
		})
	}
	return columns, nil
}

func boardVisibleNodeKind(kind pb.NodeKind) bool {
	return kind != pb.NodeKind_WORKFLOW_NODE_KIND_JOIN
}
