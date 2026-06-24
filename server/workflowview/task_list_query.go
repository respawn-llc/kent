package workflowview

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"core/server/metadata/sqlitegen"
	"core/shared/serverapi"
)

func normalizeWorkflowTaskListSort(sortSelectors []serverapi.WorkflowTaskListSort) []serverapi.WorkflowTaskListSort {
	if len(sortSelectors) == 0 {
		return []serverapi.WorkflowTaskListSort{
			{Field: serverapi.WorkflowTaskListSortFieldStatus, Direction: serverapi.WorkflowTaskListSortDirectionAsc},
			{Field: serverapi.WorkflowTaskListSortFieldUpdated, Direction: serverapi.WorkflowTaskListSortDirectionDesc},
		}
	}
	return append([]serverapi.WorkflowTaskListSort(nil), sortSelectors...)
}

type workflowTaskListPageTokenPayload struct {
	Version             int                    `json:"version"`
	ProjectID           string                 `json:"project_id"`
	WorkflowID          string                 `json:"workflow_id"`
	WorkflowVersion     int64                  `json:"workflow_version"`
	StatusStructureHash string                 `json:"status_structure_hash"`
	Fingerprint         string                 `json:"fingerprint"`
	Cursor              workflowTaskListCursor `json:"cursor"`
}

type workflowTaskListCursor struct {
	TaskID          string `json:"task_id"`
	CreatedAtUnixMs int64  `json:"created_at_unix_ms"`
	UpdatedAtUnixMs int64  `json:"updated_at_unix_ms"`
	StatusOrder     int    `json:"status_order"`
	RunCount        int    `json:"run_count"`
	TitleSort       string `json:"title_sort"`
}

func parseWorkflowTaskListPageToken(raw string) (workflowTaskListPageTokenPayload, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return workflowTaskListPageTokenPayload{}, false, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return workflowTaskListPageTokenPayload{}, false, ErrInvalidPageToken
	}
	var payload workflowTaskListPageTokenPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return workflowTaskListPageTokenPayload{}, false, ErrInvalidPageToken
	}
	if payload.Version != 1 || strings.TrimSpace(payload.ProjectID) == "" || strings.TrimSpace(payload.WorkflowID) == "" || strings.TrimSpace(payload.Cursor.TaskID) == "" || strings.TrimSpace(payload.Fingerprint) == "" {
		return workflowTaskListPageTokenPayload{}, false, ErrInvalidPageToken
	}
	return payload, true, nil
}

func workflowTaskListPageToken(payload workflowTaskListPageTokenPayload) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func workflowTaskListRequestFingerprint(req serverapi.WorkflowTaskListRequest, sortSelectors []serverapi.WorkflowTaskListSort, statusStructureHash string) string {
	statusKeys := dedupeSortedStrings(req.StatusKeys)
	runStatusStrings := make([]string, 0, len(req.RunStatuses))
	for _, status := range req.RunStatuses {
		runStatusStrings = append(runStatusStrings, string(status))
	}
	runStatuses := dedupeSortedStrings(runStatusStrings)
	payload := struct {
		StatusKeys          []string                         `json:"status_keys"`
		RunStatuses         []string                         `json:"run_statuses"`
		Sort                []serverapi.WorkflowTaskListSort `json:"sort"`
		StatusStructureHash string                           `json:"status_structure_hash"`
	}{StatusKeys: statusKeys, RunStatuses: runStatuses, Sort: sortSelectors, StatusStructureHash: statusStructureHash}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func workflowTaskListStatusStructureHash(def serverapi.WorkflowDefinition, columns []serverapi.WorkflowBoardColumn) string {
	parts := make([]string, 0, len(columns)+1)
	for _, column := range columns {
		parts = append(parts, strings.Join([]string{column.Node.NodeID, column.Node.Key, column.Node.Kind, strconv.Itoa(column.SortOrder), strconv.FormatBool(column.IsBacklog), strconv.FormatBool(column.IsDone)}, "\x00"))
	}
	parts = append(parts, "canceled:"+canceledBoardTerminalNodeID(def))
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type workflowTaskListItemWithSort struct {
	item        serverapi.WorkflowTaskListItem
	statusOrder int
	titleSort   string
}

func workflowTaskListCursorFromItem(item workflowTaskListItemWithSort) workflowTaskListCursor {
	return workflowTaskListCursor{
		TaskID:          item.item.TaskID,
		CreatedAtUnixMs: item.item.CreatedAtUnixMs,
		UpdatedAtUnixMs: item.item.UpdatedAtUnixMs,
		StatusOrder:     item.statusOrder,
		RunCount:        item.item.RunCount,
		TitleSort:       item.titleSort,
	}
}

func dedupeSortedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	out := make([]string, 0, len(sorted))
	for _, value := range sorted {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

type workflowTaskListQueryRequest struct {
	projectID          string
	workflowID         string
	columns            []serverapi.WorkflowBoardColumn
	canceledTerminalID string
	statusKeys         []string
	runStatuses        []serverapi.WorkflowTaskRunStatus
	sortSelectors      []serverapi.WorkflowTaskListSort
	cursor             workflowTaskListCursor
	cursorSet          bool
	limit              int
}

type workflowTaskListRow struct {
	task        sqlitegen.TaskRecord
	statusOrder int
	runCount    int
	runStatus   serverapi.WorkflowTaskRunStatus
	titleSort   string
}

func (s *Service) listWorkflowTaskListRows(ctx context.Context, req workflowTaskListQueryRequest) ([]workflowTaskListRow, error) {
	if len(req.columns) == 0 {
		return []workflowTaskListRow{}, nil
	}
	visibleColumnsJSON, err := workflowTaskListVisibleColumnsJSON(req.columns)
	if err != nil {
		return nil, err
	}
	statusKeysJSON, err := json.Marshal(req.statusKeys)
	if err != nil {
		return nil, err
	}
	runStatuses := make([]string, 0, len(req.runStatuses))
	for _, status := range req.runStatuses {
		runStatuses = append(runStatuses, string(status))
	}
	runStatusesJSON, err := json.Marshal(runStatuses)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListWorkflowTaskListFilteredRows(ctx, sqlitegen.ListWorkflowTaskListFilteredRowsParams{
		StatusFilterSet:        boolInt64(len(req.statusKeys) > 0),
		StatusKeysJson:         string(statusKeysJSON),
		RunStatusFilterSet:     boolInt64(len(req.runStatuses) > 0),
		RunStatusesJson:        string(runStatusesJSON),
		VisibleColumnsJson:     visibleColumnsJSON,
		ProjectID:              req.projectID,
		WorkflowID:             req.workflowID,
		CanceledTerminalNodeID: req.canceledTerminalID,
		SentinelStatusOrder:    int64(len(req.columns)),
	})
	if err != nil {
		return nil, err
	}
	out := make([]workflowTaskListRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, workflowTaskListRow{
			task: sqlitegen.TaskRecord{
				ID:                    row.ID,
				ProjectID:             row.ProjectID,
				ProjectWorkflowLinkID: row.ProjectWorkflowLinkID,
				WorkflowID:            row.WorkflowID,
				WorkflowRevisionSeen:  row.WorkflowRevisionSeen,
				TaskSeq:               row.TaskSeq,
				ShortID:               row.ShortID,
				Title:                 row.Title,
				Body:                  row.Body,
				SourceUrl:             row.SourceUrl,
				SourceWorkspaceID:     row.SourceWorkspaceID,
				ManagedWorktreeID:     row.ManagedWorktreeID,
				CanceledAtUnixMs:      row.CanceledAtUnixMs,
				CancellationReason:    row.CancellationReason,
				CreatedAtUnixMs:       row.CreatedAtUnixMs,
				UpdatedAtUnixMs:       row.UpdatedAtUnixMs,
				MetadataJson:          row.MetadataJson,
			},
			statusOrder: int(row.StatusOrder),
			runCount:    int(row.RunCount),
			runStatus:   serverapi.WorkflowTaskRunStatus(row.RunStatus),
			titleSort:   row.TitleSort,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return workflowTaskListRowLess(out[i], out[j], req.sortSelectors)
	})
	if req.cursorSet {
		filtered := out[:0]
		for _, row := range out {
			if workflowTaskListRowAfterCursor(row, req.sortSelectors, req.cursor) {
				filtered = append(filtered, row)
			}
		}
		out = filtered
	}
	if req.limit >= 0 && len(out) > req.limit {
		out = out[:req.limit]
	}
	return out, nil
}

type workflowTaskListVisibleColumn struct {
	NodeID      string `json:"node_id"`
	NodeKey     string `json:"node_key"`
	NodeKind    string `json:"node_kind"`
	StatusOrder int    `json:"status_order"`
}

func workflowTaskListVisibleColumnsJSON(columns []serverapi.WorkflowBoardColumn) (string, error) {
	rows := make([]workflowTaskListVisibleColumn, 0, len(columns))
	for _, column := range columns {
		rows = append(rows, workflowTaskListVisibleColumn{
			NodeID:      column.Node.NodeID,
			NodeKey:     column.Node.Key,
			NodeKind:    column.Node.Kind,
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

func workflowTaskListRowLess(left workflowTaskListRow, right workflowTaskListRow, sortSelectors []serverapi.WorkflowTaskListSort) bool {
	for _, selector := range sortSelectors {
		cmp := workflowTaskListRowCompare(left, right, selector.Field)
		if cmp == 0 {
			continue
		}
		if selector.Direction == serverapi.WorkflowTaskListSortDirectionDesc {
			return cmp > 0
		}
		return cmp < 0
	}
	return left.task.ID < right.task.ID
}

func workflowTaskListRowAfterCursor(row workflowTaskListRow, sortSelectors []serverapi.WorkflowTaskListSort, cursor workflowTaskListCursor) bool {
	for _, selector := range sortSelectors {
		cmp := workflowTaskListRowCompareCursor(row, cursor, selector.Field)
		if cmp == 0 {
			continue
		}
		if selector.Direction == serverapi.WorkflowTaskListSortDirectionDesc {
			return cmp < 0
		}
		return cmp > 0
	}
	return row.task.ID > cursor.TaskID
}

func workflowTaskListRowCompare(left workflowTaskListRow, right workflowTaskListRow, field serverapi.WorkflowTaskListSortField) int {
	switch field {
	case serverapi.WorkflowTaskListSortFieldCreated:
		return compareInt64(left.task.CreatedAtUnixMs, right.task.CreatedAtUnixMs)
	case serverapi.WorkflowTaskListSortFieldUpdated:
		return compareInt64(left.task.UpdatedAtUnixMs, right.task.UpdatedAtUnixMs)
	case serverapi.WorkflowTaskListSortFieldStatus:
		return compareInt(left.statusOrder, right.statusOrder)
	case serverapi.WorkflowTaskListSortFieldRunCount:
		return compareInt(left.runCount, right.runCount)
	case serverapi.WorkflowTaskListSortFieldTitle:
		return compareString(left.titleSort, right.titleSort)
	default:
		return 0
	}
}

func workflowTaskListRowCompareCursor(row workflowTaskListRow, cursor workflowTaskListCursor, field serverapi.WorkflowTaskListSortField) int {
	switch field {
	case serverapi.WorkflowTaskListSortFieldCreated:
		return compareInt64(row.task.CreatedAtUnixMs, cursor.CreatedAtUnixMs)
	case serverapi.WorkflowTaskListSortFieldUpdated:
		return compareInt64(row.task.UpdatedAtUnixMs, cursor.UpdatedAtUnixMs)
	case serverapi.WorkflowTaskListSortFieldStatus:
		return compareInt(row.statusOrder, cursor.StatusOrder)
	case serverapi.WorkflowTaskListSortFieldRunCount:
		return compareInt(row.runCount, cursor.RunCount)
	case serverapi.WorkflowTaskListSortFieldTitle:
		return compareString(row.titleSort, cursor.TitleSort)
	default:
		return 0
	}
}

func compareInt(left int, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareInt64(left int64, right int64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareString(left string, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
