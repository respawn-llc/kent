package workflowview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
	"core/shared/tasksearchtext"

	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type TaskSearch struct {
	queries    *sqlitegen.Queries
	projection *TaskStatusProjection
}

func NewTaskSearch(
	metadataStore *metadata.Store,
	projection *TaskStatusProjection,
) (*TaskSearch, error) {
	switch {
	case metadataStore == nil || metadataStore.Queries() == nil:
		return nil, errors.New("metadata store is required")
	case projection == nil:
		return nil, errors.New("task status projection is required")
	}
	return &TaskSearch{
		queries:    metadataStore.Queries(),
		projection: projection,
	}, nil
}

func (s *TaskSearch) Search(ctx context.Context, req *taskpb.SearchRequest) (response *taskpb.SearchSuccess, err error) {
	if s == nil || s.queries == nil || s.projection == nil {
		return nil, errors.New("task search is required")
	}
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if req.Mode == taskpb.SearchMode_SEARCH_MODE_LITERAL {
		if err := serverapi.ValidateTaskSearchLiteralQuery(req.Query); err != nil {
			return nil, err
		}
	}
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	offset := int(req.GetOffset())
	observation, err := s.projection.Observe(nil)
	if err != nil {
		return nil, err
	}
	if err := s.validateSchemaAndScope(ctx, s.queries, req); err != nil {
		if req.Mode == taskpb.SearchMode_SEARCH_MODE_FTS5 {
			return nil, taskSearchFTS5OperationalError(err)
		}
		return nil, err
	}
	if req.Mode == taskpb.SearchMode_SEARCH_MODE_FTS5 {
		if _, validationErr := s.queries.ValidateTaskSearchFTS5Expression(ctx, sql.NullString{String: req.Query, Valid: true}); validationErr != nil {
			return nil, taskSearchFTS5OperationalError(validationErr)
		}
	}
	rows, err := s.queryPage(ctx, s.queries, observation.LiveTaskStatesJSON, req, offset)
	if err != nil {
		if req.Mode == taskpb.SearchMode_SEARCH_MODE_FTS5 {
			return nil, taskSearchFTS5OperationalError(err)
		}
		return nil, err
	}
	hasNext := len(rows) > int(req.PageSize)
	if hasNext {
		rows = rows[:req.PageSize]
	}
	groups, err := s.materializeGroups(ctx, s.queries, req, rows)
	if err != nil {
		return nil, err
	}
	var next *int32
	if hasNext && len(rows) > 0 {
		value, err := protoapi.Int32(offset+len(rows), "next_offset")
		if err != nil {
			return nil, err
		}
		next = &value
	}
	response = &taskpb.SearchSuccess{Mode: req.Mode, Groups: groups, NextOffset: next}
	if err := protoapi.Validate(response); err != nil {
		return nil, fmt.Errorf("validate task search response: %w", err)
	}
	return response, nil
}

func (s *TaskSearch) validateSchemaAndScope(ctx context.Context, queries *sqlitegen.Queries, req *taskpb.SearchRequest) error {
	schemaFailures, err := queries.ListTaskSearchSchemaContractFailures(ctx)
	if err != nil {
		return err
	}
	if len(schemaFailures) != 0 {
		return errors.New("task search schema is incomplete")
	}
	projectIDsJSON, err := json.Marshal(taskSearchProjectIDs(req))
	if err != nil {
		return fmt.Errorf("encode task search project ids: %w", err)
	}
	unknown, err := queries.ListUnknownTaskSearchProjectIDs(ctx, string(projectIDsJSON))
	if err != nil {
		return err
	}
	if len(unknown) != 0 {
		return fmt.Errorf("task search project scope contains unknown project ids: %s", strings.Join(unknown, ","))
	}
	return nil
}

func (s *TaskSearch) queryPage(ctx context.Context, queries *sqlitegen.Queries, liveTaskStatesJSON string, req *taskpb.SearchRequest, offset int) ([]sqlitegen.ListTaskSearchPageDescriptorsRow, error) {
	projectIDsJSON, err := taskSearchOptionalJSON(taskSearchProjectIDs(req))
	if err != nil {
		return nil, fmt.Errorf("encode task search project ids: %w", err)
	}
	statuses, err := taskSearchStatusKinds(req)
	if err != nil {
		return nil, err
	}
	statusKindsJSON, err := taskSearchOptionalJSON(statuses)
	if err != nil {
		return nil, fmt.Errorf("encode task search status kinds: %w", err)
	}
	candidateExpression := req.Query
	caseMode := int64(tasksearchtext.LiteralCaseInsensitive)
	if req.Mode == taskpb.SearchMode_SEARCH_MODE_LITERAL {
		mode := tasksearchtext.LiteralCaseInsensitive
		if req.CaseSensitive {
			mode = tasksearchtext.LiteralCaseSensitive
		}
		matcher, matcherErr := tasksearchtext.NewLiteralMatcher(req.Query, mode)
		if matcherErr != nil {
			return nil, matcherErr
		}
		candidateExpression = matcher.CandidateExpression()
		caseMode = int64(mode)
	}
	mode, err := protoapi.TaskSearchMode.Decode(req.Mode)
	if err != nil {
		return nil, err
	}
	return queries.ListTaskSearchPageDescriptors(ctx, sqlitegen.ListTaskSearchPageDescriptorsParams{
		Mode:                mode,
		CandidateExpression: candidateExpression,
		LiteralQuery:        req.Query,
		CaseMode:            caseMode,
		IncludeComments:     boolInt64(req.IncludeComments),
		ProjectIdsJson:      projectIDsJSON,
		StatusKindsJson:     statusKindsJSON,
		ContextClusters:     int64(req.Context),
		OffsetRows:          int64(offset),
		LimitRows:           int64(req.PageSize + 1),
		LiveTaskStatesJson:  liveTaskStatesJSON,
		ShortIDCaseMode:     int64(tasksearchtext.LiteralCaseInsensitive),
	})
}

func taskSearchOptionalJSON[T any](values []T) (sql.NullString, error) {
	if len(values) == 0 {
		return sql.NullString{}, nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(encoded), Valid: true}, nil
}

func (s *TaskSearch) materializeGroups(ctx context.Context, queries *sqlitegen.Queries, req *taskpb.SearchRequest, rows []sqlitegen.ListTaskSearchPageDescriptorsRow) ([]*taskpb.SearchGroup, error) {
	var textMatcher tasksearchtext.LiteralMatcher
	var shortIDMatcher tasksearchtext.LiteralMatcher
	if req.Mode == taskpb.SearchMode_SEARCH_MODE_LITERAL {
		mode := tasksearchtext.LiteralCaseInsensitive
		if req.CaseSensitive {
			mode = tasksearchtext.LiteralCaseSensitive
		}
		var err error
		textMatcher, err = tasksearchtext.NewLiteralMatcher(req.Query, mode)
		if err != nil {
			return nil, err
		}
		shortIDMatcher, err = tasksearchtext.NewLiteralMatcher(req.Query, tasksearchtext.LiteralCaseInsensitive)
		if err != nil {
			return nil, err
		}
	}
	groups := make([]*taskpb.SearchGroup, 0, len(rows))
	groupIndexes := make(map[string]int, len(rows))
	for _, row := range rows {
		statusKind, err := taskSearchSQLiteString(row.StatusKind, "status kind")
		if err != nil {
			return nil, err
		}
		nodeIDsJSON, err := taskSearchSQLiteString(row.NodeIdsJson, "node ids")
		if err != nil {
			return nil, err
		}
		attentionTypesJSON, err := taskSearchSQLiteString(row.AttentionTypesJson, "attention types")
		if err != nil {
			return nil, err
		}
		status, err := s.projection.DecodeStatus(TaskStatusInput{
			TaskID:             row.TaskID,
			Kind:               statusKind,
			NodeIDsJSON:        nodeIDsJSON,
			AttentionTypesJSON: attentionTypesJSON,
			Done:               row.IsDone != 0,
		})
		if err != nil {
			return nil, err
		}
		index, exists := groupIndexes[row.TaskID]
		if !exists {
			index = len(groups)
			groupIndexes[row.TaskID] = index
			total, err := protoapi.Int32(int(row.TotalHitCount), "total_hit_count")
			if err != nil {
				return nil, err
			}
			groups = append(groups, &taskpb.SearchGroup{
				ProjectId:     row.ProjectID,
				ProjectKey:    row.ProjectKey,
				TaskId:        row.TaskID,
				ShortId:       row.ShortID,
				WorkflowId:    row.WorkflowID.String(),
				Title:         row.TaskTitle,
				Status:        status.Status,
				TotalHitCount: total,
				Hits:          []*taskpb.SearchHit{},
			})
		}
		hit, err := taskSearchMaterializeHit(ctx, queries, req, textMatcher, shortIDMatcher, row)
		if err != nil {
			return nil, err
		}
		groups[index].Hits = append(groups[index].Hits, hit)
	}
	return groups, nil
}

func taskSearchMaterializeHit(
	ctx context.Context,
	queries *sqlitegen.Queries,
	req *taskpb.SearchRequest,
	textMatcher tasksearchtext.LiteralMatcher,
	shortIDMatcher tasksearchtext.LiteralMatcher,
	row sqlitegen.ListTaskSearchPageDescriptorsRow,
) (*taskpb.SearchHit, error) {
	kind, err := protoapi.TaskSearchSourceKind.Encode(row.SourceKind)
	if err != nil {
		return nil, err
	}
	apiSource := &taskpb.SearchSource{Kind: kind}
	if row.CommentID.Valid {
		value := row.CommentID.String
		apiSource.CommentId = &value
	}
	ordinal, err := protoapi.Int32(int(row.Ordinal), "ordinal")
	if err != nil {
		return nil, err
	}
	hit := &taskpb.SearchHit{Ordinal: ordinal, Source: apiSource}
	switch req.Mode {
	case taskpb.SearchMode_SEARCH_MODE_LITERAL:
		source, err := queries.GetTaskSearchSourceByDocumentID(ctx, row.DocumentID)
		if err != nil {
			return nil, err
		}
		if source.SourceKind != row.SourceKind {
			return nil, errors.New("task search source identity changed during read")
		}
		sourceText, err := taskSearchSourceText(source)
		if err != nil {
			return nil, err
		}
		matcher := textMatcher
		sourceOrdinal := int(row.SourceOrdinal)
		if kind == taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_SHORT_ID {
			if sourceOrdinal != 1 || sourceText != row.ShortID {
				return nil, errors.New("task search Short ID source is inconsistent")
			}
			matcher = shortIDMatcher
			sourceText = row.ShortID
		}
		literal, found := matcher.NthHit(sourceText, sourceOrdinal, int(req.Context))
		if !found {
			return nil, errors.New("task search selected literal occurrence is absent")
		}
		hit.Match = &taskpb.SearchHit_Literal{Literal: &taskpb.SearchLiteralHit{
			Before:         literal.Before,
			Match:          literal.Match,
			After:          literal.After,
			LeftTruncated:  literal.LeftTruncated,
			RightTruncated: literal.RightTruncated,
		}}
	case taskpb.SearchMode_SEARCH_MODE_FTS5:
		if kind == taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_SHORT_ID {
			return nil, errors.New("task search Short ID source requires literal mode")
		}
		snippet, err := taskSearchSQLiteString(row.RawSnippet, "FTS5 snippet")
		if err != nil {
			return nil, err
		}
		hit.Match = &taskpb.SearchHit_Fts5{Fts5: &taskpb.SearchFts5Hit{Snippet: snippet}}
	default:
		return nil, errors.New("task search mode is invalid")
	}
	return hit, nil
}

func taskSearchSourceText(source sqlitegen.GetTaskSearchSourceByDocumentIDRow) (string, error) {
	kind, err := protoapi.TaskSearchSourceKind.Encode(source.SourceKind)
	if err != nil {
		return "", err
	}
	switch kind {
	case taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_SHORT_ID:
		return taskSearchSQLiteString(source.ShortID, "Short ID")
	case taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_TITLE:
		return taskSearchSQLiteString(source.Title, "title")
	case taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_BODY:
		return taskSearchSQLiteString(source.Body, "body")
	case taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_COMMENT:
		return taskSearchSQLiteString(source.Comment, "comment")
	default:
		return "", errors.New("task search source kind is invalid")
	}
}

func taskSearchSQLiteString(value interface{}, field string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("task search %s has type %T, want text", field, value)
	}
	return text, nil
}

func taskSearchFTS5OperationalError(err error) error {
	var sqliteErr *sqlitedriver.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == sqlite3.SQLITE_ERROR {
		return errors.New("task search FTS5 query could not be evaluated")
	}
	return err
}

func taskSearchProjectIDs(req *taskpb.SearchRequest) []string {
	return append([]string{}, req.ProjectIds...)
}

func taskSearchStatusKinds(req *taskpb.SearchRequest) ([]string, error) {
	statuses := make([]string, 0, len(req.StatusKinds))
	for _, status := range req.StatusKinds {
		name, err := protoapi.TaskStatusKind.Decode(status)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, name)
	}
	return statuses, nil
}
