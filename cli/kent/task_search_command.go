package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

func taskSearchSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task search", stderr, taskSearchUsage)
	fts5 := fs.Bool("fts5", false, "interpret the query as a raw FTS5 expression")
	caseSensitive := fs.Bool("case-sensitive", false, "require exact code points in literal title, body, and Comment search")
	contextSize := fs.Int("context", serverapi.TaskSearchDefaultContext, "context size for each matching source")
	includeComments := fs.Bool("include-comments", false, "include Task Comments in the search")
	pageSize := fs.Int("page-size", serverapi.TaskSearchDefaultPageSize, "maximum hits to print")
	offset := fs.Int("offset", 0, "zero-based hit offset")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	var projectFlags repeatedStringFlag
	var statusFlags repeatedStringFlag
	fs.Var(&projectFlags, "project", "project ID or registered workspace path; repeat for multiple projects")
	fs.Var(&statusFlags, "status", "task status filter; comma-separated or repeatable")

	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task search requires exactly one <query>")
		return 2
	}
	statusKinds, err := parseTaskSearchStatusKinds([]string(statusFlags))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	mode := taskpb.SearchMode_SEARCH_MODE_LITERAL
	if *fts5 {
		mode = taskpb.SearchMode_SEARCH_MODE_FTS5
	}
	contextCount, err := protoapi.Int32(*contextSize, "context")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	pageCount, err := protoapi.Int32(*pageSize, "page_size")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	request := &taskpb.SearchRequest{
		Mode:            mode,
		Query:           strings.TrimSpace(positionals[0]),
		Context:         contextCount,
		CaseSensitive:   *caseSensitive,
		IncludeComments: *includeComments,
		StatusKinds:     statusKinds,
		PageSize:        pageCount,
	}
	if flagExplicit(fs, "offset") {
		value, err := protoapi.Int32(*offset, "offset")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		request.Offset = &value
	}
	if err := validateTaskSearchCommandRequest(request); err != nil {
		writeTaskSearchError(stderr, err)
		return 2
	}
	return runWorkflowCommandSession(stderr, func(cfg config.Connection, remote *client.Remote) int {
		return runTaskSearch(
			context.Background(),
			cfg,
			remote,
			remote,
			[]string(projectFlags),
			request,
			*jsonOut,
			stdout,
			stderr,
		)
	})
}

func runTaskSearch(
	ctx context.Context,
	cfg config.Connection,
	projects apicontract.ProjectViewService,
	workflows apicontract.WorkflowService,
	projectRefs []string,
	request *taskpb.SearchRequest,
	jsonOut bool,
	stdout io.Writer,
	stderr io.Writer,
) int {
	projectIDs, err := resolveTaskSearchProjectIDs(ctx, cfg, projects, projectRefs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	request.ProjectIds = projectIDs
	response, err := searchWorkflowTasks(ctx, workflows, request)
	if err != nil {
		return writeTaskSearchError(stderr, err)
	}
	return writeTaskSearchResponse(stdout, stderr, response, jsonOut)
}

func validateTaskSearchCommandRequest(request *taskpb.SearchRequest) error {
	if err := protoapi.Validate(request); err != nil {
		return err
	}
	if request.Mode == taskpb.SearchMode_SEARCH_MODE_LITERAL {
		return serverapi.ValidateTaskSearchLiteralQuery(request.Query)
	}
	return nil
}

func resolveTaskSearchProjectIDs(ctx context.Context, cfg config.Connection, remote apicontract.ProjectViewService, refs []string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		projectID, err := resolveWorkflowProjectID(ctx, cfg, remote, ref)
		if err != nil {
			return nil, err
		}
		ids = append(ids, projectID)
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

func parseTaskSearchStatusKinds(raw []string) ([]taskpb.TaskStatusKind, error) {
	statuses, err := parseTaskListStatusKinds(raw)
	if err != nil {
		return nil, err
	}
	slices.Sort(statuses)
	return statuses, nil
}

func searchWorkflowTasks(ctx context.Context, remote apicontract.WorkflowService, request *taskpb.SearchRequest) (*taskpb.SearchSuccess, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	return remote.SearchWorkflowTasks(rpcCtx, request)
}

func writeTaskSearchResponse(stdout io.Writer, stderr io.Writer, response *taskpb.SearchSuccess, jsonOut bool) int {
	if err := protoapi.Validate(response); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if jsonOut {
		output, err := taskSearchOutput(response)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if exitCode := writeCommandJSON(stdout, stderr, output); exitCode != 0 {
			return exitCode
		}
	} else {
		projection, err := taskSearchPlainProjectionFromResponse(response)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := writeTaskSearchPlainProjection(stdout, projection); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if response.NextOffset != nil {
		if err := writeNextOffset(stderr, int(*response.NextOffset)); err != nil {
			return 1
		}
	}
	return 0
}

func writeTaskSearchError(stderr io.Writer, err error) int {
	var searchErr *serverapi.TaskSearchError
	if errors.As(err, &searchErr) {
		switch searchErr.Reason {
		case serverapi.TaskSearchErrorReasonNormalizedTooShort:
			fmt.Fprintln(stderr, "task search query is too short after normalization")
			return 2
		}
	}
	fmt.Fprintln(stderr, err)
	return 1
}
