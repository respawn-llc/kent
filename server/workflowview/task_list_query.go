package workflowview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"core/server/metadata/sqlitegen"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func normalizeWorkflowTaskListSort(sortSelectors []*taskpb.ListSort) []*taskpb.ListSort {
	if len(sortSelectors) == 0 {
		return []*taskpb.ListSort{
			{Field: taskpb.ListSortField_LIST_SORT_FIELD_STATUS, Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC},
			{Field: taskpb.ListSortField_LIST_SORT_FIELD_UPDATED, Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_DESC},
		}
	}
	return append([]*taskpb.ListSort(nil), sortSelectors...)
}

func workflowTaskListSortUsesColumn(sortSelectors []*taskpb.ListSort) bool {
	for _, selector := range sortSelectors {
		if selector.Field == taskpb.ListSortField_LIST_SORT_FIELD_COLUMN {
			return true
		}
	}
	return false
}

type workflowTaskListQueryRequest struct {
	projectID          string
	narrowed           *workflowTaskListNarrowedQueryFacts
	statusKinds        []taskpb.TaskStatusKind
	attentionKinds     []taskpb.TaskAttentionKind
	labelFilter        workflowTaskLabelFilterQueryArgs
	dependencyFilter   *bool
	sortSelectors      []*taskpb.ListSort
	liveTaskStatesJSON string
	offset             int
	limit              int
}

type workflowTaskListNarrowedQueryFacts struct {
	workflowID runtimeids.WorkflowID
	columns    []*taskpb.BoardColumn
	columnKeys []string
}

type workflowTaskListRow struct {
	item                  *taskpb.ListItem
	titleSort             string
	primaryStatusRank     int
	columnRank            *int
	matchingWorkflowCount int
}

type workflowTaskListPageResult struct {
	rows                  []workflowTaskListRow
	matchingWorkflowCount int
}

func (l *TaskList) queryRows(ctx context.Context, req workflowTaskListQueryRequest) (workflowTaskListPageResult, error) {
	if l == nil {
		return workflowTaskListPageResult{}, errors.New("task list is required")
	}
	var workflowFilter *runtimeids.WorkflowID
	visibleColumnsJSON := sql.NullString{}
	columnKeysJSON := sql.NullString{}
	columnFilterSet := false
	if req.narrowed != nil {
		workflowFilter = &req.narrowed.workflowID
		encodedColumns, err := workflowTaskListVisibleColumnsJSON(req.narrowed.columns)
		if err != nil {
			return workflowTaskListPageResult{}, err
		}
		visibleColumnsJSON = sql.NullString{String: encodedColumns, Valid: true}
		encodedColumnKeys, err := json.Marshal(req.narrowed.columnKeys)
		if err != nil {
			return workflowTaskListPageResult{}, err
		}
		columnKeysJSON = sql.NullString{String: string(encodedColumnKeys), Valid: true}
		columnFilterSet = len(req.narrowed.columnKeys) > 0
	}
	statusKinds := make([]string, 0, len(req.statusKinds))
	for _, kind := range req.statusKinds {
		name, err := protoapi.TaskStatusKind.Decode(kind)
		if err != nil {
			return workflowTaskListPageResult{}, err
		}
		statusKinds = append(statusKinds, name)
	}
	statusKindsJSON, err := workflowTaskKindsJSON(statusKinds)
	if err != nil {
		return workflowTaskListPageResult{}, err
	}
	attentionKinds := make([]string, 0, len(req.attentionKinds))
	for _, kind := range req.attentionKinds {
		name, err := protoapi.TaskAttentionKind.Decode(kind)
		if err != nil {
			return workflowTaskListPageResult{}, err
		}
		attentionKinds = append(attentionKinds, name)
	}
	attentionKindsJSON, err := workflowTaskKindsJSON(attentionKinds)
	if err != nil {
		return workflowTaskListPageResult{}, err
	}
	var sortFields [7]string
	for index, selector := range req.sortSelectors {
		name, err := protoapi.TaskListSortField.Decode(selector.Field)
		if err != nil {
			return workflowTaskListPageResult{}, err
		}
		sortFields[index] = name
	}
	labelFilterArgs := req.labelFilter
	rows, err := l.queries.ListWorkflowTaskListRows(ctx, sqlitegen.ListWorkflowTaskListRowsParams{
		ProjectID:            req.projectID,
		WorkflowID:           workflowFilter,
		VisibleColumnsJson:   visibleColumnsJSON,
		ColumnFilterSet:      boolInt64(columnFilterSet),
		ColumnKeysJson:       columnKeysJSON,
		StatusFilterSet:      boolInt64(len(req.statusKinds) > 0),
		StatusKindsJson:      statusKindsJSON,
		AttentionFilterSet:   boolInt64(len(req.attentionKinds) > 0),
		AttentionKindsJson:   attentionKindsJSON,
		LabelFilterKind:      labelFilterArgs.kind,
		LabelFilterMode:      labelFilterArgs.mode,
		LabelIdsJson:         labelFilterArgs.labelIDsJSON,
		ExcludedLabelIdsJson: labelFilterArgs.excludedLabelIDsJSON,
		DependencyFilter:     workflowTaskDependencyFilterQueryArg(req.dependencyFilter),
		OffsetRows:           int64(req.offset),
		SortSelectorCount:    int64(len(req.sortSelectors)),
		Sort1Field:           sortFields[0],
		Sort1Desc:            workflowTaskListSortDescending(req.sortSelectors, 0),
		Sort2Field:           sortFields[1],
		Sort2Desc:            workflowTaskListSortDescending(req.sortSelectors, 1),
		Sort3Field:           sortFields[2],
		Sort3Desc:            workflowTaskListSortDescending(req.sortSelectors, 2),
		Sort4Field:           sortFields[3],
		Sort4Desc:            workflowTaskListSortDescending(req.sortSelectors, 3),
		Sort5Field:           sortFields[4],
		Sort5Desc:            workflowTaskListSortDescending(req.sortSelectors, 4),
		Sort6Field:           sortFields[5],
		Sort6Desc:            workflowTaskListSortDescending(req.sortSelectors, 5),
		Sort7Field:           sortFields[6],
		Sort7Desc:            workflowTaskListSortDescending(req.sortSelectors, 6),
		LiveTaskStatesJson:   req.liveTaskStatesJSON,
		LimitRows:            int64(req.limit),
	})
	if err != nil {
		return workflowTaskListPageResult{}, err
	}
	if len(rows) == 0 {
		return workflowTaskListPageResult{}, errors.New("workflow task list query omitted its summary row")
	}
	result := workflowTaskListPageResult{
		rows:                  make([]workflowTaskListRow, 0, len(rows)-1),
		matchingWorkflowCount: int(rows[0].MatchingWorkflowCount),
	}
	for _, row := range rows {
		if int(row.MatchingWorkflowCount) != result.matchingWorkflowCount {
			return workflowTaskListPageResult{}, fmt.Errorf("workflow task list query returned inconsistent matching workflow counts: first=%d count=%d", result.matchingWorkflowCount, row.MatchingWorkflowCount)
		}
		if !row.ID.Valid {
			continue
		}
		statusFact, err := l.projection.DecodeStatus(TaskStatusInput{
			TaskID:             row.ID.String,
			Kind:               row.Kind.String,
			NodeIDsJSON:        row.NodeIdsJson.String,
			AttentionTypesJSON: row.AttentionTypesJson.String,
		})
		if err != nil {
			return workflowTaskListPageResult{}, err
		}
		var columnKeys *taskpb.ColumnKeys
		if req.narrowed != nil {
			values := []string{}
			if row.ColumnKeysJson.Valid {
				var err error
				values, err = workflowTaskListColumnKeys(row.ID.String, row.ColumnKeysJson.String)
				if err != nil {
					return workflowTaskListPageResult{}, err
				}
			}
			columnKeys = &taskpb.ColumnKeys{Values: values}
		}
		columnRank := nullableInt(row.ColumnRank)
		if workflowTaskListSortUsesColumn(req.sortSelectors) && columnRank == nil {
			return workflowTaskListPageResult{}, fmt.Errorf("workflow task list record for task %q is missing a column rank required by column sorting", row.ID.String)
		}
		var workflowName *string
		if req.narrowed == nil {
			if !row.WorkflowName.Valid || strings.TrimSpace(row.WorkflowName.String) == "" {
				return workflowTaskListPageResult{}, fmt.Errorf("project-wide workflow task list record for task %q is missing workflow name", row.ID.String)
			}
			value := row.WorkflowName.String
			workflowName = &value
		}
		result.rows = append(result.rows, workflowTaskListRow{
			item: &taskpb.ListItem{
				TaskId:       row.ID.String,
				ShortId:      row.ShortID.String,
				WorkflowId:   row.WorkflowID.String(),
				WorkflowName: workflowName,
				Title:        row.Title.String,
				CreatedAt:    timestamppb.New(time.UnixMilli(row.CreatedAtUnixMs.Int64)),
				UpdatedAt:    timestamppb.New(time.UnixMilli(row.UpdatedAtUnixMs.Int64)),
				ColumnKeys:   columnKeys,
				Status:       statusFact.Status,
			},
			titleSort:             row.TitleSort.String,
			primaryStatusRank:     int(row.PrimaryStatusRank.Int64),
			columnRank:            columnRank,
			matchingWorkflowCount: int(row.MatchingWorkflowCount),
		})
	}
	return result, nil
}

func workflowTaskKindsJSON[T ~string](kinds []T) (string, error) {
	encoded, err := json.Marshal(kinds)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func workflowTaskListMatchingWorkflowCardinality(count int) (taskpb.MatchingWorkflowCardinality, error) {
	if count < 0 {
		return taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_UNSPECIFIED, fmt.Errorf("workflow task list query returned invalid matching workflow count %d", count)
	}
	switch count {
	case 0:
		return taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_NONE, nil
	case 1:
		return taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_ONE, nil
	default:
		return taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_MULTIPLE, nil
	}
}

func workflowTaskListColumnKeys(taskID string, encoded string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return nil, fmt.Errorf("workflow task list record for task %q has malformed column_keys_json: %w", taskID, err)
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("workflow task list record for task %q has blank column_keys_json[%d]", taskID, index)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("workflow task list record for task %q has duplicate column_keys_json value %q", taskID, value)
		}
		seen[value] = struct{}{}
	}
	return values, nil
}

func nullableInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

func workflowTaskListSortSelector(sortSelectors []*taskpb.ListSort, index int) *taskpb.ListSort {
	if index >= len(sortSelectors) {
		return nil
	}
	return sortSelectors[index]
}

func workflowTaskListSortDescending(sortSelectors []*taskpb.ListSort, index int) int64 {
	if workflowTaskListSortSelector(sortSelectors, index).GetDirection() == taskpb.ListSortDirection_LIST_SORT_DIRECTION_DESC {
		return 1
	}
	return 0
}

type workflowTaskListVisibleColumn struct {
	NodeID      string `json:"node_id"`
	NodeKey     string `json:"node_key"`
	NodeKind    string `json:"node_kind"`
	StatusOrder int32  `json:"status_order"`
}

func workflowTaskListVisibleColumnsJSON(columns []*taskpb.BoardColumn) (string, error) {
	rows := make([]workflowTaskListVisibleColumn, 0, len(columns))
	for _, column := range columns {
		kind, err := protoapi.WorkflowNodeKind.Decode(column.Node.Kind)
		if err != nil {
			return "", err
		}
		rows = append(rows, workflowTaskListVisibleColumn{
			NodeID:      column.Node.NodeId,
			NodeKey:     column.Node.Key,
			NodeKind:    kind,
			StatusOrder: column.SortOrder,
		})
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
