package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"builder/prompts"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/sessionenv"
)

func taskSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := newCommandFlagSet("builder task", stderr, writeTaskUsage)
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
	case "approve":
		return taskApproveSubcommand(args[1:], stdout, stderr)
	case "move":
		return taskMoveSubcommand(args[1:], stdout, stderr)
	case "resume":
		return taskResumeSubcommand(args[1:], stdout, stderr)
	case "comment":
		return taskCommentSubcommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown task command: %s\n\n", args[0])
		fs := newCommandFlagSet("builder task", stderr, writeTaskUsage)
		writeTaskUsage(fs)
		return 2
	}
}

func taskCreateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task create", stderr, writeTaskCreateUsage)
	title := fs.String("title", "", "task title")
	body := fs.String("body", "", "task body")
	workflowRef := fs.String("workflow", "", "workflow id or exact workflow name")
	projectRef := fs.String("project", ".", "project id or path")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
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
	fs := newCommandFlagSet("builder task start", stderr, writeTaskStartUsage)
	projectRef := fs.String("project", ".", "project id or path for short ids")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task start requires <short-id-or-task-id>")
		return 2
	}
	if denyAgentHumanOnlyTaskAction(stderr) {
		return 1
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
	fs := newCommandFlagSet("builder task list", stderr, writeTaskListUsage)
	projectRef := fs.String("project", ".", "project id or path")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
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
	tasks, _, err := workflowTasksForProject(context.Background(), cfg, remote, *projectRef)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, task := range tasks {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%t\t%t\t%s\n", task.ShortID, task.ID, task.WorkflowID, task.Done, task.CanceledAt != 0, task.Title)
	}
	return 0
}

func taskShowSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task show", stderr, writeTaskShowUsage)
	projectRef := fs.String("project", ".", "project id or path for short ids")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
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
	requestedProjectID, task, err := getWorkflowTaskForShow(context.Background(), cfg, remote, *projectRef, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if requestedProjectID != "" && task.Summary.ProjectID != "" && task.Summary.ProjectID != requestedProjectID && task.Project.ProjectKey != "" {
		fmt.Fprintf(stdout, "[Note: This task belongs to another project %s]\n", task.Project.ProjectKey)
	}
	writeTaskDetail(stdout, task)
	return 0
}

func taskCancelSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task cancel", stderr, writeTaskCancelUsage)
	projectRef := fs.String("project", ".", "project id or path for short ids")
	reason := fs.String("reason", "", "cancel reason")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task cancel requires <short-id-or-task-id>")
		return 2
	}
	if denyAgentHumanOnlyTaskAction(stderr) {
		return 1
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

func taskResumeSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task resume", stderr, writeTaskResumeUsage)
	projectRef := fs.String("project", ".", "project id or path for short ids")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task resume requires <short-id-or-task-id>")
		return 2
	}
	if denyAgentHumanOnlyTaskAction(stderr) {
		return 1
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
	resp, err := remote.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{TaskID: taskID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "run_id\t%s\nplacement_id\t%s\nnode_id\t%s\ngeneration\t%d\n", resp.RunID, resp.PlacementID, resp.NodeID, resp.Generation)
	if strings.TrimSpace(resp.SessionID) != "" {
		fmt.Fprintf(stdout, "session_id\t%s\n", resp.SessionID)
	}
	return 0
}

func taskApproveSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task approve", stderr, writeTaskApproveUsage)
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task approve requires <transition-id>")
		return 2
	}
	if denyAgentHumanOnlyTaskAction(stderr) {
		return 1
	}
	_, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	resp, err := remote.ApproveWorkflowTask(ctx, serverapi.WorkflowTaskApproveRequest{TransitionID: positionals[0]})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "transition_id\t%s\nstate\t%s\n", resp.TransitionID, resp.State)
	for _, placementID := range resp.PlacementIDs {
		fmt.Fprintf(stdout, "placement_id\t%s\n", placementID)
	}
	for _, runID := range resp.RunIDs {
		fmt.Fprintf(stdout, "run_id\t%s\n", runID)
	}
	return 0
}

func taskMoveSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task move", stderr, writeTaskMoveUsage)
	projectRef := fs.String("project", ".", "project id or path for short ids")
	commentary := fs.String("commentary", "", "transition commentary")
	outputs := stringMapFlag{}
	fs.Var(&outputs, "output", "output value as name=value; repeatable")
	positionals, flagArgs := takeLeadingPositionals(args, 2)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 2 {
		fmt.Fprintln(stderr, "task move requires <short-id-or-task-id> <target-node-id>")
		return 2
	}
	if denyAgentHumanOnlyTaskAction(stderr) {
		return 1
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
	resp, err := remote.MoveWorkflowTask(ctx, serverapi.WorkflowTaskMoveRequest{TaskID: taskID, TargetNodeID: positionals[1], OutputValues: outputs.values, Commentary: *commentary})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "transition_id\t%s\nstate\t%s\n", resp.TransitionID, resp.State)
	for _, placementID := range resp.PlacementIDs {
		fmt.Fprintf(stdout, "placement_id\t%s\n", placementID)
	}
	for _, runID := range resp.RunIDs {
		fmt.Fprintf(stdout, "run_id\t%s\n", runID)
	}
	return 0
}

type stringMapFlag struct {
	values map[string]string
}

func (f *stringMapFlag) String() string {
	if f == nil || len(f.values) == 0 {
		return ""
	}
	return fmt.Sprintf("%v", f.values)
}

func (f *stringMapFlag) Set(raw string) error {
	name, value, ok := strings.Cut(raw, "=")
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return fmt.Errorf("output must be name=value")
	}
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[name] = value
	return nil
}

func taskCommentSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := newCommandFlagSet("builder task comment", stderr, writeTaskCommentUsage)
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
		fs := newCommandFlagSet("builder task comment", stderr, writeTaskCommentUsage)
		writeTaskCommentUsage(fs)
		return 2
	}
}

func taskCommentAddSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task comment add", stderr, writeTaskCommentAddUsage)
	body := fs.String("body", "", "comment body")
	author := fs.String("author", "user", "comment author")
	projectRef := fs.String("project", ".", "project id or path for short ids")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
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
	fs := newCommandFlagSet("builder task comment list", stderr, writeTaskCommentListUsage)
	projectRef := fs.String("project", ".", "project id or path for short ids")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
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
	resp, err := remote.ListWorkflowTaskComments(ctx, serverapi.WorkflowTaskCommentListRequest{TaskID: taskID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, comment := range resp.Comments {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", comment.ID, comment.Author, comment.Body)
	}
	return 0
}

func taskCommentReplaceSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task comment replace", stderr, writeTaskCommentReplaceUsage)
	body := fs.String("body", "", "replacement comment body")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
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
	fs := newCommandFlagSet("builder task comment delete", stderr, writeTaskCommentDeleteUsage)
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task comment delete requires <comment-id>")
		return 2
	}
	if denyAgentHumanOnlyTaskAction(stderr) {
		return 1
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

func denyAgentHumanOnlyTaskAction(stderr io.Writer) bool {
	if _, ok := sessionenv.LookupBuilderSessionID(os.LookupEnv); !ok {
		return false
	}
	fmt.Fprintln(stderr, prompts.WorkflowHumanOnlyTaskActionDeniedPrompt)
	return true
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
	tasks, projectID, err := workflowTasksForProject(ctx, cfg, remote, projectRef)
	if err == nil {
		matches := make([]serverapi.WorkflowTaskSummary, 0, 1)
		for _, task := range tasks {
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
	return "", fmt.Errorf("task %q not found in project %s", trimmed, projectID)
}

func getWorkflowTaskForShow(ctx context.Context, cfg config.App, remote workflowCommandRemote, projectRef string, ref string) (string, serverapi.WorkflowTaskDetail, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", serverapi.WorkflowTaskDetail{}, errors.New("task id is required")
	}
	requestedProjectID := ""
	if resolved, err := resolveWorkflowProjectID(ctx, cfg, remote, projectRef); err == nil {
		requestedProjectID = resolved
	} else if !strings.HasPrefix(trimmed, "task-") {
		return "", serverapi.WorkflowTaskDetail{}, err
	}
	if strings.HasPrefix(trimmed, "task-") {
		detail, err := getWorkflowTaskByID(ctx, remote, trimmed)
		return requestedProjectID, detail, err
	}
	if requestedProjectID != "" {
		detail, err := getWorkflowTaskByProjectShortID(ctx, remote, requestedProjectID, trimmed)
		if err == nil {
			return requestedProjectID, detail, nil
		}
		if !isWorkflowTaskNotFound(err) {
			return requestedProjectID, serverapi.WorkflowTaskDetail{}, err
		}
	}
	detail, err := getWorkflowTaskByShortID(ctx, remote, trimmed)
	if err == nil {
		return requestedProjectID, detail, nil
	}
	if !isWorkflowTaskNotFound(err) {
		return requestedProjectID, serverapi.WorkflowTaskDetail{}, err
	}
	if requestedProjectID != "" {
		return requestedProjectID, serverapi.WorkflowTaskDetail{}, fmt.Errorf("task %q not found in project %s", trimmed, requestedProjectID)
	}
	return requestedProjectID, serverapi.WorkflowTaskDetail{}, fmt.Errorf("task %q not found", trimmed)
}

func isWorkflowTaskNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, serverapi.ErrWorkflowTaskNotFound)
}

func getWorkflowTaskByID(ctx context.Context, remote workflowCommandRemote, taskID string) (serverapi.WorkflowTaskDetail, error) {
	rpcCtx, cancel := workflowRPCContext(ctx)
	defer cancel()
	resp, err := remote.GetWorkflowTask(rpcCtx, serverapi.WorkflowTaskGetRequest{TaskID: taskID})
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	return resp.Task, nil
}

func getWorkflowTaskByProjectShortID(ctx context.Context, remote workflowCommandRemote, projectID string, shortID string) (serverapi.WorkflowTaskDetail, error) {
	rpcCtx, cancel := workflowRPCContext(ctx)
	defer cancel()
	resp, err := remote.GetWorkflowTask(rpcCtx, serverapi.WorkflowTaskGetRequest{ProjectID: projectID, ShortID: shortID})
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	return resp.Task, nil
}

func getWorkflowTaskByShortID(ctx context.Context, remote workflowCommandRemote, shortID string) (serverapi.WorkflowTaskDetail, error) {
	rpcCtx, cancel := workflowRPCContext(ctx)
	defer cancel()
	resp, err := remote.GetWorkflowTask(rpcCtx, serverapi.WorkflowTaskGetRequest{ShortID: shortID})
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	return resp.Task, nil
}

func writeTaskDetail(stdout io.Writer, task serverapi.WorkflowTaskDetail) {
	fmt.Fprintf(stdout, "task_id\t%s\nshort_id\t%s\nworkflow_id\t%s\ntitle\t%s\ndone\t%t\ncanceled\t%t\n", task.Summary.ID, task.Summary.ShortID, task.Summary.WorkflowID, task.Summary.Title, task.Summary.Done, task.Summary.CanceledAt != 0)
	fmt.Fprintln(stdout, "placements")
	for _, placement := range task.Placements {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", placement.ID, placement.NodeID, placement.State, placement.ParallelBatchTransitionID, placement.ParallelBranchEdgeID)
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
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", comment.ID, comment.Author, comment.Body)
	}
}
