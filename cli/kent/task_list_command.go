package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const taskListDefaultLimit = 100

func taskListSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task list", stderr, taskListUsage)
	projectRef := fs.String("project", ".", "project id or path")
	workflowID := fs.String("workflow", "", "workflow selector `<uuid>`")
	offset := fs.Int("offset", 0, "zero-based task offset")
	limit := fs.Int("limit", taskListDefaultLimit, "maximum tasks to print")
	var statusFlags repeatedStringFlag
	var columnFlags repeatedStringFlag
	var attentionFlags repeatedStringFlag
	var sortFlags repeatedStringFlag
	var labelFlags repeatedStringFlag
	var notLabelFlags repeatedStringFlag
	fs.Var(&statusFlags, "status", "task status filter; comma-separated or repeatable")
	fs.Var(&columnFlags, "column", "workflow column key filter; comma-separated or repeatable")
	fs.Var(&attentionFlags, "attention", "task attention filter; comma-separated or repeatable")
	fs.Var(&sortFlags, "sort", "sort selectors such as status:asc,updated:desc")
	fs.Var(&labelFlags, "label", "label name or canonical UUIDv4; repeat for multiple labels")
	fs.Var(&notLabelFlags, "not-label", "excluded label name or canonical UUIDv4; repeat for multiple labels")
	unblocked := fs.Bool("unblocked", false, "only include tasks with no unsatisfied direct dependencies")
	blocked := fs.Bool("blocked", false, "only include tasks with unsatisfied direct dependencies")
	labelMatchRaw := fs.String("label-match", "any", "label match mode: any or all")
	unlabeled := fs.Bool("unlabeled", false, "only include tasks without labels")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	unblockedProvided := flagExplicit(fs, "unblocked")
	blockedProvided := flagExplicit(fs, "blocked")
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "task list does not accept positional arguments")
		return 2
	}
	dependencyFilter, err := parseTaskListDependencyFilter(
		*unblocked,
		unblockedProvided,
		*blocked,
		blockedProvided,
	)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := validateWorkflowPagination(*offset, *limit); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	columnKeys, err := parseTaskListFilterValues([]string(columnFlags), "column")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	statusKinds, err := parseTaskListStatusKinds([]string(statusFlags))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	attentionKinds, err := parseTaskListAttentionKinds([]string(attentionFlags))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	sortSelectors, err := parseTaskListSortSelectors([]string(sortFlags))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	labelMatchExplicit := flagExplicit(fs, "label-match")
	labelMatch, err := parseTaskListLabelMatch(*labelMatchRaw, labelMatchExplicit, len(labelFlags)+len(notLabelFlags), *unlabeled)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	workflowProvided := flagExplicit(fs, "workflow")
	var selectedWorkflowID *runtimeids.WorkflowID
	if workflowProvided {
		selector, parseErr := parseWorkflowSelector(*workflowID)
		err = parseErr
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		selectedWorkflowID = &selector
	}
	var recoveryLabelMatch *string
	if labelMatchExplicit {
		value := *labelMatchRaw
		recoveryLabelMatch = &value
	}
	return runWorkflowCommandSession(stderr, func(cfg config.App, remote *client.Remote) int {
		projectID, err := resolveWorkflowProjectID(context.Background(), cfg, remote, *projectRef)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		labelFilter := &taskpb.LabelFilter{Filter: &taskpb.LabelFilter_None{None: &emptypb.Empty{}}}
		if *unlabeled {
			labelFilter = &taskpb.LabelFilter{Filter: &taskpb.LabelFilter_Unlabeled{Unlabeled: &emptypb.Empty{}}}
		} else if len(labelFlags)+len(notLabelFlags) > 0 {
			_, snapshot, err := loadWorkflowProjectLabelCatalog(context.Background(), remote, projectID)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			labelFilter, err = resolveWorkflowProjectLabelFilter(snapshot, labelMatch, labelFlags, notLabelFlags)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
		}
		pageOffset, err := protoapi.Int32(*offset, "offset")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		var workflowSelector *string
		if selectedWorkflowID != nil {
			workflowSelector = proto.String(selectedWorkflowID.String())
		}
		request := &taskpb.ListRequest{
			ProjectId:        &projectID,
			WorkflowId:       workflowSelector,
			LabelFilter:      labelFilter,
			DependencyFilter: dependencyFilter,
			ColumnKeys:       columnKeys,
			StatusKinds:      statusKinds,
			AttentionKinds:   attentionKinds,
			Sort:             sortSelectors,
			Offset:           &pageOffset,
			Limit:            proto.Int32(int32(*limit)),
		}
		resp, err := workflowTaskList(context.Background(), remote, request)
		if err != nil {
			writeTaskListError(stderr, err, taskListCommandContext{
				ProjectRef:             *projectRef,
				ResolvedProjectID:      projectID,
				SelectedWorkflowID:     selectedWorkflowID,
				ColumnKeys:             columnKeys,
				StatusKinds:            statusFlags,
				AttentionKinds:         attentionFlags,
				Sort:                   sortFlags,
				LabelSelectors:         append([]string(nil), labelFlags...),
				ExcludedLabelSelectors: append([]string(nil), notLabelFlags...),
				LabelMatch:             recoveryLabelMatch,
				Unlabeled:              *unlabeled,
				DependencyFilter:       dependencyFilter,
				Offset:                 *offset,
				Limit:                  *limit,
				JSON:                   *jsonOut,
			})
			return 1
		}
		return writeTaskListResponse(stdout, stderr, resp, *jsonOut)
	})
}

func parseTaskListDependencyFilter(
	unblocked bool,
	unblockedProvided bool,
	blocked bool,
	blockedProvided bool,
) (*bool, error) {
	if (unblockedProvided && !unblocked) || (blockedProvided && !blocked) {
		return nil, errors.New("--unblocked and --blocked must be enabled when supplied")
	}
	if unblockedProvided && blockedProvided {
		return nil, errors.New("--unblocked and --blocked are mutually exclusive")
	}
	if unblockedProvided {
		value := true
		return &value, nil
	}
	if blockedProvided {
		value := false
		return &value, nil
	}
	return nil, nil
}

func parseTaskListLabelMatch(raw string, explicit bool, selectorCount int, unlabeled bool) (taskpb.NamedLabelFilterMode, error) {
	mode, err := protoapi.TaskLabelFilterMode.Encode(raw)
	if err != nil {
		return taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_UNSPECIFIED, errors.New("--label-match is invalid")
	}
	if unlabeled && (selectorCount > 0 || explicit) {
		return taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_UNSPECIFIED, errors.New("--unlabeled cannot be combined with --label, --not-label, or --label-match")
	}
	if explicit && selectorCount == 0 {
		return taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_UNSPECIFIED, errors.New("--label-match requires at least one --label or --not-label")
	}
	return mode, nil
}

func writeTaskListResponse(stdout io.Writer, stderr io.Writer, response *taskpb.ListSuccess, jsonOut bool) int {
	resp, err := taskListOutput(response)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if jsonOut {
		return writeCommandJSON(stdout, stderr, resp)
	}
	for _, task := range resp.Tasks {
		fmt.Fprintf(stdout, "%s: %s.\n", task.ShortID, task.Title)
		fmt.Fprintf(stdout, "Status: %s\n", task.Status.Kind)
		if len(task.Labels) > 0 {
			fmt.Fprint(stdout, "Labels:")
			for _, label := range task.Labels {
				fmt.Fprintf(stdout, " %q", label.Name)
			}
			fmt.Fprintln(stdout)
		}
		if response.MatchingWorkflowCardinality == taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_MULTIPLE &&
			task.WorkflowName != nil {
			fmt.Fprintf(stdout, "Workflow: %s\n", *task.WorkflowName)
		}
		if task.ColumnKeys != nil {
			fmt.Fprintf(stdout, "Current nodes: %s\n", taskListColumnKeysText(*task.ColumnKeys))
		}
		if task.DependencyProgress != nil &&
			task.DependencyProgress.TotalCount > 0 &&
			task.DependencyProgress.SatisfiedCount < task.DependencyProgress.TotalCount {
			fmt.Fprintf(
				stdout,
				"Deps: %d/%d\n",
				task.DependencyProgress.SatisfiedCount,
				task.DependencyProgress.TotalCount,
			)
		}
	}
	if resp.NextOffset != nil {
		if err := writeNextOffset(stderr, int(*resp.NextOffset)); err != nil {
			return 1
		}
	}
	return 0
}

func writeTaskListError(stderr io.Writer, err error, commandContext taskListCommandContext) {
	var listError *client.TaskListError
	if !errors.As(err, &listError) || listError.Failure.GetScopeError() == nil {
		fmt.Fprintln(stderr, err)
		return
	}
	recovery, projectionErr := taskListRecoveryForScopeError(listError.Failure.GetScopeError(), commandContext)
	if projectionErr != nil {
		fmt.Fprintln(stderr, projectionErr)
		return
	}
	switch recovery.Kind {
	case taskWorkflowRecoveryNoLinkedWorkflows:
		fmt.Fprintln(stderr, "This project doesn't have any linked workflows yet. First, create a workflow or link an existing one, then retry.")
	case taskWorkflowRecoveryWorkflowNotLinked:
		fmt.Fprintln(stderr, "The selected workflow isn't linked to this project.")
	case taskWorkflowRecoveryWorkflowRequiredColumns:
		fmt.Fprintln(stderr, "Column filters and column sorting require an explicit workflow.")
	}
	for _, command := range recovery.Commands {
		fmt.Fprintf(stderr, "  %s\n", commandString(command.Args))
	}
}

func taskListColumnKeysText(columnKeys []string) string {
	if len(columnKeys) == 0 {
		return "(none)"
	}
	return strings.Join(columnKeys, ", ")
}

func parseTaskListFilterValues(raw []string, name string) ([]string, error) {
	values, err := tokenizeTaskListValues(raw, name)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	deduplicated := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		deduplicated = append(deduplicated, value)
	}
	return deduplicated, nil
}

func tokenizeTaskListValues(raw []string, name string) ([]string, error) {
	values := []string{}
	for _, entry := range raw {
		for _, part := range strings.Split(entry, ",") {
			value := strings.TrimSpace(part)
			if value == "" {
				return nil, fmt.Errorf("--%s contains a blank value", name)
			}
			values = append(values, value)
		}
	}
	return values, nil
}

func parseTaskListStatusKinds(raw []string) ([]taskpb.TaskStatusKind, error) {
	values, err := parseTaskListFilterValues(raw, "status")
	if err != nil {
		return nil, err
	}
	statuses := make([]taskpb.TaskStatusKind, 0, len(values))
	for _, value := range values {
		status, err := protoapi.TaskStatusKind.Encode(value)
		if err != nil {
			return nil, fmt.Errorf("--status is invalid")
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func parseTaskListAttentionKinds(raw []string) ([]taskpb.TaskAttentionKind, error) {
	values, err := parseTaskListFilterValues(raw, "attention")
	if err != nil {
		return nil, err
	}
	out := make([]taskpb.TaskAttentionKind, 0, len(values))
	for _, value := range values {
		kind, err := protoapi.TaskAttentionKind.Encode(value)
		if err != nil {
			return nil, fmt.Errorf("--attention is invalid")
		}
		out = append(out, kind)
	}
	return out, nil
}

func parseTaskListSortSelectors(raw []string) ([]*taskpb.ListSort, error) {
	values, err := tokenizeTaskListValues(raw, "sort")
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, nil
	}
	selectors := make([]*taskpb.ListSort, 0, len(values))
	seen := map[taskpb.ListSortField]bool{}
	for _, value := range values {
		fieldValue, directionValue, ok := strings.Cut(value, ":")
		if !ok {
			return nil, fmt.Errorf("--sort selector %q must be field:direction", value)
		}
		field, err := parseTaskListSortField(strings.TrimSpace(fieldValue))
		if err != nil {
			return nil, err
		}
		if seen[field] {
			return nil, fmt.Errorf("--sort field %q must not be repeated", field)
		}
		seen[field] = true
		direction, err := protoapi.TaskListSortDirection.Encode(strings.TrimSpace(directionValue))
		if err != nil {
			return nil, fmt.Errorf("--sort direction must be asc or desc")
		}
		selectors = append(selectors, &taskpb.ListSort{Field: field, Direction: direction})
	}
	return selectors, nil
}

func parseTaskListSortField(value string) (taskpb.ListSortField, error) {
	switch value {
	case "created", "created_at":
		return taskpb.ListSortField_LIST_SORT_FIELD_CREATED, nil
	case "updated", "updated_at":
		return taskpb.ListSortField_LIST_SORT_FIELD_UPDATED, nil
	case "status":
		return taskpb.ListSortField_LIST_SORT_FIELD_STATUS, nil
	case "column":
		return taskpb.ListSortField_LIST_SORT_FIELD_COLUMN, nil
	case "title":
		return taskpb.ListSortField_LIST_SORT_FIELD_TITLE, nil
	case "labels":
		return taskpb.ListSortField_LIST_SORT_FIELD_LABELS, nil
	case "short-id", "short_id":
		return taskpb.ListSortField_LIST_SORT_FIELD_SHORT_ID, nil
	default:
		return taskpb.ListSortField_LIST_SORT_FIELD_UNSPECIFIED, fmt.Errorf("--sort field must be created, updated, status, column, title, labels, or short_id")
	}
}
