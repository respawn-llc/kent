package main

import (
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type taskSearchJSON struct {
	Mode       string                `json:"mode"`
	Groups     []taskSearchGroupJSON `json:"groups"`
	NextOffset *int32                `json:"next_offset,omitempty"`
}

type taskSearchGroupJSON struct {
	ProjectID     string              `json:"project_id"`
	ProjectKey    string              `json:"project_key"`
	TaskID        string              `json:"task_id"`
	ShortID       string              `json:"short_id"`
	WorkflowID    string              `json:"workflow_id"`
	Title         string              `json:"title"`
	Status        taskStatusJSON      `json:"status"`
	TotalHitCount int32               `json:"total_hit_count"`
	Hits          []taskSearchHitJSON `json:"hits"`
}

type taskSearchHitJSON struct {
	Ordinal int32                  `json:"ordinal"`
	Source  taskSearchSourceJSON   `json:"source"`
	Literal *taskSearchLiteralJSON `json:"literal,omitempty"`
	FTS5    *taskpb.SearchFts5Hit  `json:"fts5,omitempty"`
}

type taskSearchSourceJSON struct {
	Kind      string  `json:"kind"`
	CommentID *string `json:"comment_id,omitempty"`
}

type taskSearchLiteralJSON struct {
	Before         string `json:"before"`
	Match          string `json:"match"`
	After          string `json:"after"`
	LeftTruncated  bool   `json:"left_truncated"`
	RightTruncated bool   `json:"right_truncated"`
}

func taskSearchOutput(response *taskpb.SearchSuccess) (taskSearchJSON, error) {
	mode, err := protoapi.TaskSearchMode.Decode(response.Mode)
	if err != nil {
		return taskSearchJSON{}, err
	}
	out := taskSearchJSON{Mode: mode, Groups: make([]taskSearchGroupJSON, 0, len(response.Groups)), NextOffset: response.NextOffset}
	for _, group := range response.Groups {
		status, err := taskStatusOutput(group.Status)
		if err != nil {
			return taskSearchJSON{}, err
		}
		projected := taskSearchGroupJSON{
			ProjectID: group.ProjectId, ProjectKey: group.ProjectKey, TaskID: group.TaskId, ShortID: group.ShortId,
			WorkflowID: group.WorkflowId, Title: group.Title, Status: status, TotalHitCount: group.TotalHitCount,
			Hits: make([]taskSearchHitJSON, 0, len(group.Hits)),
		}
		for _, hit := range group.Hits {
			kind, err := protoapi.TaskSearchSourceKind.Decode(hit.Source.Kind)
			if err != nil {
				return taskSearchJSON{}, err
			}
			item := taskSearchHitJSON{
				Ordinal: hit.Ordinal, Source: taskSearchSourceJSON{Kind: kind, CommentID: hit.Source.CommentId},
				FTS5: hit.GetFts5(),
			}
			if literal := hit.GetLiteral(); literal != nil {
				item.Literal = &taskSearchLiteralJSON{
					Before: literal.Before, Match: literal.Match, After: literal.After,
					LeftTruncated: literal.LeftTruncated, RightTruncated: literal.RightTruncated,
				}
			}
			projected.Hits = append(projected.Hits, item)
		}
		out.Groups = append(out.Groups, projected)
	}
	return out, nil
}
