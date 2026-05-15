package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"builder/shared/config"
	"builder/shared/serverapi"
)

func taskSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := flag.NewFlagSet("builder task", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.Usage = func() { writeTaskUsage(fs) }
		fs.Usage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "create":
		return taskCreateSubcommand(args[1:], stdout, stderr)
	case "start":
		return taskStartSubcommand(args[1:], stdout, stderr)
	case "list":
		return taskListSubcommand(args[1:], stdout, stderr)
	case "show":
		return taskShowSubcommand(args[1:], stdout, stderr)
	case "cancel":
		return taskCancelSubcommand(args[1:], stdout, stderr)
	case "move", "approve", "resume":
		return taskUnsupportedSubcommand(args[0], stdout, stderr)
	case "comment":
		return taskCommentSubcommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown task command: %s\n\n", args[0])
		fs := flag.NewFlagSet("builder task", flag.ContinueOnError)
		fs.SetOutput(stderr)
		writeTaskUsage(fs)
		return 2
	}
}

func taskCreateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder task create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeTaskCreateUsage(fs) }
	title := fs.String("title", "", "task title")
	body := fs.String("body", "", "task body")
	workflowRef := fs.String("workflow", "", "workflow id or exact workflow name")
	projectRef := fs.String("project", ".", "project id or path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "task create does not accept positional arguments")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	projectID, err := resolveWorkflowProjectID(context.Background(), cfg, remote, *projectRef)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	workflowID := ""
	if strings.TrimSpace(*workflowRef) != "" {
		workflowID, err = resolveWorkflowID(context.Background(), remote, *workflowRef)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	resp, err := remote.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: projectID, WorkflowID: workflowID, Title: *title, Body: *body})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "task_id\t%s\nshort_id\t%s\nworkflow_id\t%s\n", resp.Task.ID, resp.Task.ShortID, resp.Task.WorkflowID)
	return 0
}

func taskStartSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder task start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeTaskStartUsage(fs) }
	projectRef := fs.String("project", ".", "project id or path for short ids")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task start requires <short-id-or-task-id>")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	taskID, err := resolveWorkflowTaskID(context.Background(), cfg, remote, *projectRef, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	resp, err := remote.StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{TaskID: taskID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "run_id\t%s\nplacement_id\t%s\ntransition_id\t%s\n", resp.RunID, resp.PlacementID, resp.TransitionID)
	return 0
}

func taskListSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder task list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeTaskListUsage(fs) }
	projectRef := fs.String("project", ".", "project id or path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "task list does not accept positional arguments")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	board, err := workflowBoardForProject(context.Background(), cfg, remote, *projectRef)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, task := range sortedWorkflowTasks(board) {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%t\t%t\t%s\n", task.ShortID, task.ID, task.WorkflowID, task.Done, task.CanceledAt != 0, task.Title)
	}
	return 0
}

func taskShowSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder task show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeTaskShowUsage(fs) }
	projectRef := fs.String("project", ".", "project id or path for short ids")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task show requires <short-id-or-task-id>")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	taskID, err := resolveWorkflowTaskID(context.Background(), cfg, remote, *projectRef, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	resp, err := remote.GetWorkflowTask(ctx, serverapi.WorkflowTaskGetRequest{TaskID: taskID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	writeTaskDetail(stdout, resp.Task)
	return 0
}

func taskCancelSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder task cancel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeTaskCancelUsage(fs) }
	projectRef := fs.String("project", ".", "project id or path for short ids")
	reason := fs.String("reason", "", "cancel reason")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task cancel requires <short-id-or-task-id>")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	taskID, err := resolveWorkflowTaskID(context.Background(), cfg, remote, *projectRef, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	if err := remote.CancelWorkflowTask(ctx, serverapi.WorkflowTaskCancelRequest{TaskID: taskID, Reason: *reason}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "canceled_task_id\t%s\n", taskID)
	return 0
}

func taskUnsupportedSubcommand(action string, stdout io.Writer, stderr io.Writer) int {
	_ = stdout
	fmt.Fprintf(stderr, "task %s is not implemented yet; reserved for a later workflow runtime slice\n", action)
	return 1
}

func taskCommentSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := flag.NewFlagSet("builder task comment", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.Usage = func() { writeTaskCommentUsage(fs) }
		fs.Usage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "add":
		return taskCommentAddSubcommand(args[1:], stdout, stderr)
	case "list":
		return taskCommentListSubcommand(args[1:], stdout, stderr)
	case "replace":
		return taskCommentReplaceSubcommand(args[1:], stdout, stderr)
	case "delete":
		return taskCommentDeleteSubcommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown task comment command: %s\n\n", args[0])
		fs := flag.NewFlagSet("builder task comment", flag.ContinueOnError)
		fs.SetOutput(stderr)
		writeTaskCommentUsage(fs)
		return 2
	}
}

func taskCommentAddSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder task comment add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeTaskCommentAddUsage(fs) }
	body := fs.String("body", "", "comment body")
	author := fs.String("author", "user", "comment author")
	projectRef := fs.String("project", ".", "project id or path for short ids")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task comment add requires <short-id-or-task-id>")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	taskID, err := resolveWorkflowTaskID(context.Background(), cfg, remote, *projectRef, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	resp, err := remote.AddWorkflowTaskComment(ctx, serverapi.WorkflowTaskCommentAddRequest{TaskID: taskID, Body: *body, Author: *author})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "comment_id\t%s\ntask_id\t%s\n", resp.Comment.ID, resp.Comment.TaskID)
	return 0
}

func taskCommentListSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder task comment list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeTaskCommentListUsage(fs) }
	projectRef := fs.String("project", ".", "project id or path for short ids")
	includeDeleted := fs.Bool("include-deleted", false, "include deleted comments")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task comment list requires <short-id-or-task-id>")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	taskID, err := resolveWorkflowTaskID(context.Background(), cfg, remote, *projectRef, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	resp, err := remote.ListWorkflowTaskComments(ctx, serverapi.WorkflowTaskCommentListRequest{TaskID: taskID, IncludeDeleted: *includeDeleted})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, comment := range resp.Comments {
		fmt.Fprintf(stdout, "%s\t%s\t%t\t%s\n", comment.ID, comment.Author, comment.DeletedAt != 0, comment.Body)
	}
	return 0
}

func taskCommentReplaceSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder task comment replace", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeTaskCommentReplaceUsage(fs) }
	body := fs.String("body", "", "replacement comment body")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task comment replace requires <comment-id>")
		return 2
	}
	_, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	if err := remote.ReplaceWorkflowTaskComment(ctx, serverapi.WorkflowTaskCommentReplaceRequest{CommentID: positionals[0], Body: *body}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "replaced_comment_id\t%s\n", positionals[0])
	return 0
}

func taskCommentDeleteSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder task comment delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeTaskCommentDeleteUsage(fs) }
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task comment delete requires <comment-id>")
		return 2
	}
	_, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	if err := remote.DeleteWorkflowTaskComment(ctx, serverapi.WorkflowTaskCommentDeleteRequest{CommentID: positionals[0]}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "deleted_comment_id\t%s\n", positionals[0])
	return 0
}

func workflowBoardForProject(ctx context.Context, cfg config.App, remote workflowCommandRemote, projectRef string) (serverapi.WorkflowBoard, error) {
	projectID, err := resolveWorkflowProjectID(ctx, cfg, remote, projectRef)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	rpcCtx, cancel := workflowRPCContext(ctx)
	defer cancel()
	resp, err := remote.GetWorkflowBoard(rpcCtx, serverapi.WorkflowBoardRequest{ProjectID: projectID})
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	return resp.Board, nil
}

func resolveWorkflowTaskID(ctx context.Context, cfg config.App, remote workflowCommandRemote, projectRef string, ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", errors.New("task id is required")
	}
	if strings.HasPrefix(trimmed, "task-") {
		rpcCtx, cancel := workflowRPCContext(ctx)
		defer cancel()
		resp, err := remote.GetWorkflowTask(rpcCtx, serverapi.WorkflowTaskGetRequest{TaskID: trimmed})
		if err != nil {
			return "", err
		}
		return resp.Task.Summary.ID, nil
	}
	board, err := workflowBoardForProject(ctx, cfg, remote, projectRef)
	if err == nil {
		matches := make([]serverapi.WorkflowTaskSummary, 0, 1)
		for _, task := range sortedWorkflowTasks(board) {
			if task.ID == trimmed || task.ShortID == trimmed {
				matches = append(matches, task)
			}
		}
		if len(matches) == 1 {
			return matches[0].ID, nil
		}
		if len(matches) > 1 {
			return "", fmt.Errorf("task %q is ambiguous; use task id", trimmed)
		}
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("task %q not found in project %s", trimmed, board.ProjectID)
}

func writeTaskDetail(stdout io.Writer, task serverapi.WorkflowTaskDetail) {
	fmt.Fprintf(stdout, "task_id\t%s\nshort_id\t%s\nworkflow_id\t%s\ntitle\t%s\ndone\t%t\ncanceled\t%t\n", task.Summary.ID, task.Summary.ShortID, task.Summary.WorkflowID, task.Summary.Title, task.Summary.Done, task.Summary.CanceledAt != 0)
	fmt.Fprintln(stdout, "placements")
	for _, placement := range task.Placements {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", placement.ID, placement.NodeID, placement.State)
	}
	fmt.Fprintln(stdout, "runs")
	for _, run := range task.Runs {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%d\t%d\n", run.ID, run.PlacementID, run.NodeID, run.CompletedAtUnixMs, run.InterruptedAtUnixMs)
	}
	fmt.Fprintln(stdout, "transitions")
	for _, transition := range task.Transitions {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", transition.ID, transition.TransitionID, transition.State, transition.Commentary)
	}
	fmt.Fprintln(stdout, "comments")
	for _, comment := range task.Comments {
		fmt.Fprintf(stdout, "%s\t%s\t%t\t%s\n", comment.ID, comment.Author, comment.DeletedAt != 0, comment.Body)
	}
}
