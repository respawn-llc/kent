package workflowview

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	args := []any{}
	columnRows := make([]string, 0, len(req.columns))
	for _, column := range req.columns {
		columnRows = append(columnRows, "(?, ?, ?, ?, ?, ?)")
		args = append(args, column.Node.NodeID, column.Node.Key, column.Node.Kind, column.SortOrder, boolInt(column.IsBacklog), boolInt(column.IsDone))
	}
	sentinelStatusOrder := len(req.columns)
	args = append(args,
		req.projectID, req.workflowID,
		req.canceledTerminalID,
		req.canceledTerminalID, req.projectID, req.workflowID, req.canceledTerminalID,
		req.projectID, req.workflowID, req.canceledTerminalID,
		sentinelStatusOrder,
	)
	var b strings.Builder
	b.WriteString(`
WITH visible_columns(node_id, node_key, node_kind, status_order, is_backlog, is_done) AS (
    VALUES `)
	b.WriteString(strings.Join(columnRows, ","))
	b.WriteString(`
),
selected_tasks AS (
    SELECT *
    FROM task_records
    WHERE project_id = ?
      AND workflow_id = ?
),
effective_placements AS (
    SELECT t.id AS task_id, p.id AS placement_id, p.node_id AS node_id, p.state AS state, vc.status_order AS status_order, vc.node_key AS node_key, vc.node_kind AS node_kind
    FROM selected_tasks t
    JOIN task_node_placements p ON p.task_id = t.id
    JOIN visible_columns vc ON vc.node_id = p.node_id
    WHERE p.state IN ('active', 'waiting_approval')
      AND (t.canceled_at_unix_ms = 0 OR vc.node_kind = 'terminal' OR trim(?) = '')
    UNION
    SELECT t.id AS task_id, '' AS placement_id, vc.node_id AS node_id, 'active' AS state, vc.status_order AS status_order, vc.node_key AS node_key, vc.node_kind AS node_kind
    FROM selected_tasks t
    JOIN visible_columns vc ON vc.node_id = ?
    WHERE t.project_id = ?
      AND t.workflow_id = ?
      AND t.canceled_at_unix_ms != 0
      AND trim(?) != ''
      AND NOT EXISTS (
          SELECT 1
          FROM task_node_placements p
          JOIN workflow_nodes n ON n.id = p.node_id
          WHERE p.task_id = t.id
            AND p.state IN ('active', 'waiting_approval')
            AND n.kind = 'terminal'
      )
    UNION
    SELECT t.id AS task_id, 'pending-approval:' || tt.id AS placement_id, tt.source_node_id AS node_id, 'waiting_approval' AS state, vc.status_order AS status_order, vc.node_key AS node_key, vc.node_kind AS node_kind
    FROM task_transition_records tt
    JOIN selected_tasks t ON t.id = tt.task_id
    JOIN visible_columns vc ON vc.node_id = tt.source_node_id
    WHERE tt.state = 'pending_approval'
      AND t.project_id = ?
      AND t.workflow_id = ?
      AND (t.canceled_at_unix_ms = 0 OR trim(?) = '')
),
per_task_status AS (
    SELECT task_id, MIN(status_order) AS status_order
    FROM effective_placements
    GROUP BY task_id
),
run_counts AS (
    SELECT task_id, COUNT(*) AS run_count
    FROM task_run_records
    GROUP BY task_id
),
task_rows AS (
    SELECT
        t.id, t.project_id, t.project_workflow_link_id, t.workflow_id, t.workflow_revision_seen, t.task_seq, t.short_id, t.title, t.body, t.source_url, t.source_workspace_id, t.managed_worktree_id, t.canceled_at_unix_ms, t.cancellation_reason, t.created_at_unix_ms, t.updated_at_unix_ms, t.metadata_json,
        CAST(COALESCE(pts.status_order, ?) AS INTEGER) AS status_order,
        CAST(COALESCE(rc.run_count, 0) AS INTEGER) AS run_count,
        LOWER(t.title) AS title_sort,
        CASE
            WHEN t.canceled_at_unix_ms != 0 THEN 'canceled'
            WHEN EXISTS (SELECT 1 FROM effective_placements ep_done WHERE ep_done.task_id = t.id AND ep_done.node_kind = 'terminal') THEN 'done'
            WHEN EXISTS (SELECT 1 FROM effective_placements ep_waiting WHERE ep_waiting.task_id = t.id AND ep_waiting.state = 'waiting_approval')
              OR EXISTS (
                  SELECT 1
                  FROM task_run_records r
                  JOIN effective_placements ep_run ON ep_run.placement_id = r.placement_id
                  WHERE ep_run.task_id = t.id
                    AND r.completed_at_unix_ms = 0
                    AND (r.started_at_unix_ms != 0 OR r.interrupted_at_unix_ms != 0 OR trim(r.waiting_ask_id) != '')
              ) THEN 'running'
            ELSE 'open'
        END AS run_status
    FROM selected_tasks t
    LEFT JOIN per_task_status pts ON pts.task_id = t.id
    LEFT JOIN run_counts rc ON rc.task_id = t.id
)
SELECT
    id, project_id, project_workflow_link_id, workflow_id, workflow_revision_seen, task_seq, short_id, title, body, source_url, source_workspace_id, managed_worktree_id, canceled_at_unix_ms, cancellation_reason, created_at_unix_ms, updated_at_unix_ms, metadata_json,
    status_order, run_count, run_status, title_sort
FROM task_rows
WHERE 1 = 1`)
	if len(req.statusKeys) > 0 {
		b.WriteString(`
  AND EXISTS (SELECT 1 FROM effective_placements ep_filter WHERE ep_filter.task_id = task_rows.id AND ep_filter.node_key IN (` + sqlPlaceholders(len(req.statusKeys)) + `))`)
		for _, key := range req.statusKeys {
			args = append(args, key)
		}
	}
	if len(req.runStatuses) > 0 {
		b.WriteString(`
  AND run_status IN (` + sqlPlaceholders(len(req.runStatuses)) + `)`)
		for _, status := range req.runStatuses {
			args = append(args, string(status))
		}
	}
	if req.cursorSet {
		predicate, predicateArgs := workflowTaskListCursorPredicate(req.sortSelectors, req.cursor)
		if predicate == "" {
			return nil, ErrInvalidPageToken
		}
		b.WriteString(`
  AND (` + predicate + `)`)
		args = append(args, predicateArgs...)
	}
	orderBy, err := workflowTaskListOrderBy(req.sortSelectors)
	if err != nil {
		return nil, err
	}
	b.WriteString(`
ORDER BY `)
	b.WriteString(orderBy)
	b.WriteString(`
LIMIT ?`)
	args = append(args, req.limit)
	rows, err := s.metadata.DB().QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []workflowTaskListRow{}
	for rows.Next() {
		var row workflowTaskListRow
		var runStatus string
		if err := rows.Scan(
			&row.task.ID, &row.task.ProjectID, &row.task.ProjectWorkflowLinkID, &row.task.WorkflowID, &row.task.WorkflowRevisionSeen, &row.task.TaskSeq, &row.task.ShortID, &row.task.Title, &row.task.Body, &row.task.SourceUrl, &row.task.SourceWorkspaceID, &row.task.ManagedWorktreeID, &row.task.CanceledAtUnixMs, &row.task.CancellationReason, &row.task.CreatedAtUnixMs, &row.task.UpdatedAtUnixMs, &row.task.MetadataJson,
			&row.statusOrder, &row.runCount, &runStatus, &row.titleSort,
		); err != nil {
			return nil, err
		}
		row.runStatus = serverapi.WorkflowTaskRunStatus(runStatus)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func sqlPlaceholders(count int) string {
	parts := make([]string, count)
	for index := range parts {
		parts[index] = "?"
	}
	return strings.Join(parts, ",")
}

func workflowTaskListOrderBy(sortSelectors []serverapi.WorkflowTaskListSort) (string, error) {
	parts := make([]string, 0, len(sortSelectors)+1)
	for _, selector := range sortSelectors {
		column, err := workflowTaskListSortColumn(selector.Field)
		if err != nil {
			return "", err
		}
		direction := "ASC"
		if selector.Direction == serverapi.WorkflowTaskListSortDirectionDesc {
			direction = "DESC"
		}
		parts = append(parts, column+" "+direction)
	}
	parts = append(parts, "id ASC")
	return strings.Join(parts, ", "), nil
}

func workflowTaskListSortColumn(field serverapi.WorkflowTaskListSortField) (string, error) {
	switch field {
	case serverapi.WorkflowTaskListSortFieldCreated:
		return "created_at_unix_ms", nil
	case serverapi.WorkflowTaskListSortFieldUpdated:
		return "updated_at_unix_ms", nil
	case serverapi.WorkflowTaskListSortFieldStatus:
		return "status_order", nil
	case serverapi.WorkflowTaskListSortFieldRunCount:
		return "run_count", nil
	case serverapi.WorkflowTaskListSortFieldTitle:
		return "title_sort", nil
	default:
		return "", fmt.Errorf("unsupported workflow task list sort field %q", field)
	}
}

func workflowTaskListCursorPredicate(sortSelectors []serverapi.WorkflowTaskListSort, cursor workflowTaskListCursor) (string, []any) {
	terms := append([]serverapi.WorkflowTaskListSort(nil), sortSelectors...)
	terms = append(terms, serverapi.WorkflowTaskListSort{Field: serverapi.WorkflowTaskListSortField("task_id"), Direction: serverapi.WorkflowTaskListSortDirectionAsc})
	clauses := make([]string, 0, len(terms))
	args := []any{}
	for index, term := range terms {
		parts := make([]string, 0, index+1)
		for priorIndex := 0; priorIndex < index; priorIndex++ {
			column, err := workflowTaskListCursorColumn(terms[priorIndex].Field)
			if err != nil {
				return "", nil
			}
			parts = append(parts, column+" = ?")
			args = append(args, workflowTaskListCursorValue(cursor, terms[priorIndex].Field))
		}
		column, err := workflowTaskListCursorColumn(term.Field)
		if err != nil {
			return "", nil
		}
		operator := ">"
		if term.Direction == serverapi.WorkflowTaskListSortDirectionDesc {
			operator = "<"
		}
		parts = append(parts, column+" "+operator+" ?")
		args = append(args, workflowTaskListCursorValue(cursor, term.Field))
		clauses = append(clauses, "("+strings.Join(parts, " AND ")+")")
	}
	return strings.Join(clauses, " OR "), args
}

func workflowTaskListCursorColumn(field serverapi.WorkflowTaskListSortField) (string, error) {
	if field == serverapi.WorkflowTaskListSortField("task_id") {
		return "id", nil
	}
	return workflowTaskListSortColumn(field)
}

func workflowTaskListCursorValue(cursor workflowTaskListCursor, field serverapi.WorkflowTaskListSortField) any {
	switch field {
	case serverapi.WorkflowTaskListSortFieldCreated:
		return cursor.CreatedAtUnixMs
	case serverapi.WorkflowTaskListSortFieldUpdated:
		return cursor.UpdatedAtUnixMs
	case serverapi.WorkflowTaskListSortFieldStatus:
		return cursor.StatusOrder
	case serverapi.WorkflowTaskListSortFieldRunCount:
		return cursor.RunCount
	case serverapi.WorkflowTaskListSortFieldTitle:
		return cursor.TitleSort
	case serverapi.WorkflowTaskListSortField("task_id"):
		return cursor.TaskID
	default:
		return nil
	}
}
