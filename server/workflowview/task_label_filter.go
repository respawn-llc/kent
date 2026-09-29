package workflowview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"core/server/metadata/sqlitegen"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

type workflowProjectLabelByIDReader interface {
	ListProjectLabelsByIDs(context.Context, []string) ([]sqlitegen.ListProjectLabelsByIDsRow, error)
}

type workflowTaskLabelFilterQueryArgs struct {
	kind                 string
	mode                 sql.NullString
	labelIDsJSON         string
	excludedLabelIDsJSON string
}

func resolveWorkflowTaskLabelFilter(
	ctx context.Context,
	queries workflowProjectLabelByIDReader,
	projectID string,
	filter *taskpb.LabelFilter,
) (workflowTaskLabelFilterQueryArgs, error) {
	args := workflowTaskLabelFilterQueryArgs{}
	labelIDs, excludedLabelIDs := []string{}, []string{}
	switch selected := filter.GetFilter().(type) {
	case *taskpb.LabelFilter_None:
		args.kind = "none"
	case *taskpb.LabelFilter_Unlabeled:
		args.kind = "unlabeled"
	case *taskpb.LabelFilter_Named:
		args.kind = "named"
		switch selected.Named.GetMode() {
		case taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ANY:
			args.mode = sql.NullString{String: "any", Valid: true}
		case taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ALL:
			args.mode = sql.NullString{String: "all", Valid: true}
		default:
			return workflowTaskLabelFilterQueryArgs{}, errors.New("named task label filter mode is invalid")
		}
		labelIDs = append(labelIDs, selected.Named.LabelIds...)
		excludedLabelIDs = append(excludedLabelIDs, selected.Named.ExcludedLabelIds...)
		slices.Sort(labelIDs)
		slices.Sort(excludedLabelIDs)
		allLabelIDs := append(append([]string{}, labelIDs...), excludedLabelIDs...)
		slices.Sort(allLabelIDs)
		rows, err := queries.ListProjectLabelsByIDs(ctx, allLabelIDs)
		if err != nil {
			return workflowTaskLabelFilterQueryArgs{}, err
		}
		projectByLabelID := make(map[string]string, len(rows))
		for _, row := range rows {
			projectByLabelID[row.ID] = row.ProjectID
		}
		for _, labelID := range allLabelIDs {
			labelProjectID, exists := projectByLabelID[labelID]
			if !exists {
				return workflowTaskLabelFilterQueryArgs{}, &serverapi.WorkflowLabelError{
					Reason: serverapi.WorkflowLabelErrorReasonLabelNotFound, ProjectID: &projectID, LabelID: &labelID,
				}
			}
			if labelProjectID != projectID {
				return workflowTaskLabelFilterQueryArgs{}, &serverapi.WorkflowLabelError{
					Reason: serverapi.WorkflowLabelErrorReasonWrongProject, ProjectID: &projectID, LabelID: &labelID,
				}
			}
		}
	default:
		return workflowTaskLabelFilterQueryArgs{}, errors.New("task label filter is required")
	}
	included, err := json.Marshal(labelIDs)
	if err != nil {
		return workflowTaskLabelFilterQueryArgs{}, err
	}
	excluded, err := json.Marshal(excludedLabelIDs)
	if err != nil {
		return workflowTaskLabelFilterQueryArgs{}, err
	}
	args.labelIDsJSON = string(included)
	args.excludedLabelIDsJSON = string(excluded)
	return args, nil
}
