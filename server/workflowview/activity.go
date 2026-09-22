package workflowview

import (
	"context"
	"errors"
	"strings"
	"time"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflowstore"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type Activity struct {
	queries *sqlitegen.Queries
}

type activityPage struct {
	task       sqlitegen.TaskRecord
	rows       []taskActivityRow
	comments   map[string]sqlitegen.TaskComment
	nextOffset *int32
}

type taskActivityRow struct {
	activityID       string
	kind             string
	sourceID         string
	occurredAtUnixMs int64
	updatedAtUnixMs  int64
	sessionName      *string
}

func NewActivity(metadataStore *metadata.Store) (*Activity, error) {
	if metadataStore == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	return &Activity{queries: metadataStore.Queries()}, nil
}

func (a *Activity) List(ctx context.Context, req *taskpb.TaskOffsetPageRequest) (*taskpb.ActivityListSuccess, error) {
	page, err := a.loadPage(ctx, req)
	if err != nil {
		return nil, err
	}
	items, err := a.itemsFromPage(page)
	if err != nil {
		return nil, err
	}
	return &taskpb.ActivityListSuccess{Items: items, NextOffset: page.nextOffset}, nil
}

func (a *Activity) loadPage(ctx context.Context, req *taskpb.TaskOffsetPageRequest) (activityPage, error) {
	if a == nil {
		return activityPage{}, errors.New("activity is required")
	}
	if err := protoapi.Validate(req); err != nil {
		return activityPage{}, err
	}
	task, err := a.queries.GetTask(ctx, strings.TrimSpace(req.TaskId))
	if err != nil {
		return activityPage{}, err
	}
	window := TaskPageWindow(req)
	rows, err := a.activityRows(ctx, task.ID, window.Offset, window.Limit+1)
	if err != nil {
		return activityPage{}, err
	}
	offsetPage := serverapi.FinalizeWorkflowOffsetPage(window, rows)
	next, err := TaskNextOffset(offsetPage.NextOffset)
	if err != nil {
		return activityPage{}, err
	}
	comments, err := a.commentsByID(ctx, sourceIDsByType(offsetPage.Items, "comment"))
	if err != nil {
		return activityPage{}, err
	}
	return activityPage{
		task:       task,
		rows:       offsetPage.Items,
		comments:   comments,
		nextOffset: next,
	}, nil
}

func (a *Activity) activityRows(ctx context.Context, taskID string, offset int, limit int) ([]taskActivityRow, error) {
	if limit <= 0 {
		return []taskActivityRow{}, nil
	}
	rows, err := a.queries.ListWorkflowTaskActivityRows(ctx, sqlitegen.ListWorkflowTaskActivityRowsParams{
		PageLimit:  int64(limit),
		PageOffset: int64(offset),
		TaskID:     taskID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]taskActivityRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, taskActivityRow{
			activityID:       row.ActivityID,
			kind:             row.Kind,
			sourceID:         row.SourceID,
			occurredAtUnixMs: row.OccurredAtUnixMs,
			updatedAtUnixMs:  row.UpdatedAtUnixMs,
			sessionName:      metadata.OptionalString(row.SessionName),
		})
	}
	return out, nil
}

func (a *Activity) commentsByID(ctx context.Context, ids []string) (map[string]sqlitegen.TaskComment, error) {
	out := map[string]sqlitegen.TaskComment{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := a.queries.ListTaskCommentsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

func (a *Activity) itemsFromPage(page activityPage) ([]*taskpb.ActivityItem, error) {
	items := make([]*taskpb.ActivityItem, 0, len(page.rows))
	for _, row := range page.rows {
		item := &taskpb.ActivityItem{
			ActivityId: row.activityID, TaskId: page.task.ID,
			OccurredAt: timestamppb.New(time.UnixMilli(row.occurredAtUnixMs)),
			UpdatedAt:  timestamppb.New(time.UnixMilli(row.updatedAtUnixMs)),
		}
		switch row.kind {
		case "comment":
			comment, ok := page.comments[row.sourceID]
			if !ok {
				return nil, errors.New("activity comment source is missing")
			}
			dto, err := Comment(workflowstore.CommentRecordFromRow(comment))
			if err != nil {
				return nil, err
			}
			item.Activity = &taskpb.ActivityItem_Comment{Comment: dto}
		case "session_started":
			if row.sessionName == nil || strings.TrimSpace(*row.sessionName) == "" {
				return nil, errors.New("activity session source has no name")
			}
			item.Activity = &taskpb.ActivityItem_SessionStarted{SessionStarted: &taskpb.SessionStarted{SessionId: row.sourceID, Name: *row.sessionName}}
		default:
			return nil, errors.New("activity kind is unsupported")
		}
		items = append(items, item)
	}
	return items, nil
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
