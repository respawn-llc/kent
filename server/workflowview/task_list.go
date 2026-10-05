package workflowview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type TaskList struct {
	metadata    *metadata.Store
	queries     *sqlitegen.Queries
	definitions *DefinitionProjection
	projection  *TaskStatusProjection
}

func NewTaskList(metadataStore *metadata.Store, definitions *DefinitionProjection, projection *TaskStatusProjection) (*TaskList, error) {
	if metadataStore == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	if definitions == nil {
		return nil, errors.New("definition projection is required")
	}
	if projection == nil {
		return nil, errors.New("task status projection is required")
	}
	return &TaskList{
		metadata:    metadataStore,
		queries:     metadataStore.Queries(),
		definitions: definitions,
		projection:  projection,
	}, nil
}

func (l *TaskList) List(ctx context.Context, req *taskpb.ListRequest) (*taskpb.ListSuccess, error) {
	if l == nil {
		return nil, errors.New("task list is required")
	}
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	window := serverapi.OffsetWindow{Offset: int(req.GetOffset()), Limit: serverapi.OffsetPaginationMaxLimit}
	if req.Limit != nil {
		window.Limit = int(*req.Limit)
	}
	var workflowSelector *runtimeids.WorkflowID
	if req.WorkflowId != nil {
		value, err := runtimeids.ParseWorkflowID(*req.WorkflowId)
		if err != nil {
			return nil, err
		}
		workflowSelector = &value
	}
	projectID, workflowID, err := l.resolveScope(ctx, req.ProjectId, workflowSelector)
	if err != nil {
		return nil, err
	}
	if _, err := l.metadata.GetProjectEditMetadata(ctx, projectID); err != nil {
		return nil, err
	}
	labelFilter, err := resolveWorkflowTaskLabelFilter(ctx, l.queries, projectID, req.LabelFilter)
	if err != nil {
		return nil, err
	}
	var columns []*taskpb.BoardColumn
	if workflowID == nil {
		if len(req.ColumnKeys) > 0 || workflowTaskListSortUsesColumn(req.Sort) {
			errorProjectID := projectID
			return nil, &serverapi.WorkflowTaskListScopeError{
				Reason:    serverapi.WorkflowTaskListScopeReasonWorkflowRequiredColumns,
				ProjectID: &errorProjectID,
			}
		}
	} else {
		snapshot, snapshotErr := l.definitions.snapshot(ctx, *workflowID)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		columns, err = boardColumns(snapshot)
		if err != nil {
			return nil, err
		}
		if err := validateWorkflowTaskListColumnKeys(req.ColumnKeys, columns); err != nil {
			return nil, err
		}
	}
	sortSelectors := normalizeWorkflowTaskListSort(req.Sort)
	var narrowedQuery *workflowTaskListNarrowedQueryFacts
	if workflowID != nil {
		narrowedQuery = &workflowTaskListNarrowedQueryFacts{
			workflowID: *workflowID,
			columns:    columns,
			columnKeys: req.ColumnKeys,
		}
	}
	observation, err := l.projection.Observe(nil)
	if err != nil {
		return nil, err
	}
	statusKinds := req.StatusKinds
	if req.Group != nil {
		for _, definition := range protoapi.TaskGroupDefinitions() {
			if definition.Group == *req.Group {
				statusKinds = definition.StatusKinds
				break
			}
		}
	}
	page, err := l.queryRows(ctx, workflowTaskListQueryRequest{
		projectID:          projectID,
		narrowed:           narrowedQuery,
		statusKinds:        statusKinds,
		attentionKinds:     req.AttentionKinds,
		labelFilter:        labelFilter,
		dependencyFilter:   req.DependencyFilter,
		sortSelectors:      sortSelectors,
		offset:             window.Offset,
		limit:              window.Limit + 1,
		liveTaskStatesJSON: observation.LiveTaskStatesJSON,
	})
	if err != nil {
		return nil, err
	}
	matchingWorkflowCardinality, err := workflowTaskListMatchingWorkflowCardinality(page.matchingWorkflowCount)
	if err != nil {
		return nil, err
	}
	pageItems := page.rows
	hasNext := len(pageItems) > window.Limit
	if hasNext {
		pageItems = pageItems[:window.Limit]
	}
	pageTaskIDs := make([]string, 0, len(pageItems))
	for _, row := range pageItems {
		pageTaskIDs = append(pageTaskIDs, row.item.TaskId)
	}
	labelsByTask, err := loadTaskLabelsByTask(ctx, l.queries, pageTaskIDs)
	if err != nil {
		return nil, err
	}
	dependencyRows, err := l.queries.ListTaskDependencyProgressByTasks(ctx, pageTaskIDs)
	if err != nil {
		return nil, err
	}
	progressRows := make([]taskDependencyProgressRow, 0, len(dependencyRows))
	for _, row := range dependencyRows {
		progressRows = append(progressRows, taskDependencyProgressRow{
			taskID:         row.TaskID,
			satisfiedCount: row.DependencySatisfiedCount,
			totalCount:     row.DependencyTotalCount,
		})
	}
	dependencyProgressByTask, err := projectTaskDependencyProgress(progressRows)
	if err != nil {
		return nil, err
	}
	responseItems := make([]*taskpb.ListItem, 0, len(pageItems))
	for _, row := range pageItems {
		item := row.item
		item.Labels = labelsByTask[item.TaskId]
		item.DependencyProgress = dependencyProgressByTask[item.TaskId]
		responseItems = append(responseItems, item)
	}
	var nextOffset *int32
	if hasNext {
		value, err := protoapi.Int32(window.Offset+len(pageItems), "next_offset")
		if err != nil {
			return nil, err
		}
		nextOffset = &value
	}
	return &taskpb.ListSuccess{
		Scope: &taskpb.ListScope{
			ProjectId:  projectID,
			WorkflowId: req.WorkflowId,
		},
		MatchingWorkflowCardinality: matchingWorkflowCardinality,
		NextOffset:                  nextOffset,
		GeneratedAt:                 timestamppb.Now(),
		Tasks:                       responseItems,
	}, nil
}

func (l *TaskList) CountGroups(ctx context.Context, req *taskpb.ProjectTaskGroupCountsRequest) (*taskpb.ProjectTaskGroupCountsSuccess, error) {
	if l == nil {
		return nil, errors.New("task list is required")
	}
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if _, err := l.metadata.GetProjectEditMetadata(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	observation, err := l.projection.Observe(nil)
	if err != nil {
		return nil, err
	}
	counts, err := l.queries.CountProjectTaskGroups(ctx, sqlitegen.CountProjectTaskGroupsParams{
		ProjectID:          req.ProjectId,
		LiveTaskStatesJson: observation.LiveTaskStatesJSON,
	})
	if err != nil {
		return nil, err
	}
	active, err := protoapi.Int32(int(counts.ActiveCount), "active")
	if err != nil {
		return nil, err
	}
	backlog, err := protoapi.Int32(int(counts.BacklogCount), "backlog")
	if err != nil {
		return nil, err
	}
	done, err := protoapi.Int32(int(counts.DoneCount), "done")
	if err != nil {
		return nil, err
	}
	return &taskpb.ProjectTaskGroupCountsSuccess{
		ProjectId:   req.ProjectId,
		Definitions: protoapi.TaskGroupDefinitions(),
		Counts: &taskpb.ProjectTaskGroupCounts{
			Active:  active,
			Backlog: backlog,
			Done:    done,
		},
		GeneratedAt: timestamppb.Now(),
	}, nil
}

func (l *TaskList) resolveScope(ctx context.Context, projectIDValue *string, workflowIDValue *runtimeids.WorkflowID) (string, *runtimeids.WorkflowID, error) {
	if projectIDValue != nil && workflowIDValue != nil {

		if _, err := l.queries.GetActiveProjectWorkflowLinkByWorkflow(ctx, sqlitegen.GetActiveProjectWorkflowLinkByWorkflowParams{
			ProjectID:  *projectIDValue,
			WorkflowID: *workflowIDValue,
		}); err == nil {
			workflowID := *workflowIDValue
			return *projectIDValue, &workflowID, nil
		} else if errors.Is(err, sql.ErrNoRows) {
			errorProjectID := *projectIDValue
			errorWorkflowID := *workflowIDValue
			return "", nil, &serverapi.WorkflowTaskListScopeError{
				Reason:     serverapi.WorkflowTaskListScopeReasonWorkflowNotLinked,
				ProjectID:  &errorProjectID,
				WorkflowID: &errorWorkflowID,
			}
		} else {
			return "", nil, err
		}
	}
	if projectIDValue != nil {
		linkCount, err := l.queries.CountActiveProjectWorkflowLinks(ctx, *projectIDValue)
		if err != nil {
			return "", nil, err
		}
		if linkCount == 0 {
			errorProjectID := *projectIDValue
			return "", nil, &serverapi.WorkflowTaskListScopeError{
				Reason:    serverapi.WorkflowTaskListScopeReasonNoLinkedWorkflows,
				ProjectID: &errorProjectID,
			}
		}
		return *projectIDValue, nil, nil
	}
	return "", nil, &serverapi.WorkflowTaskListScopeError{
		Reason: serverapi.WorkflowTaskListScopeReasonNoLinkedWorkflows,
	}
}

func validateWorkflowTaskListColumnKeys(columnKeys []string, columns []*taskpb.BoardColumn) error {
	visible := map[string]bool{}
	for _, column := range columns {
		visible[column.Node.Key] = true
	}
	for index, columnKey := range columnKeys {
		if !visible[columnKey] {
			return serverapi.WorkflowRequestValidationError{
				Code:    serverapi.WorkflowRequestErrorInvalidValue,
				Field:   fmt.Sprintf("column_keys[%d]", index),
				Message: fmt.Sprintf("unknown workflow column key %q", columnKey),
			}
		}
	}
	return nil
}
