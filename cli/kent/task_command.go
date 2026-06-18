package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"core/prompts"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/sessionenv"
)

const (
	taskListDefaultPageSize        = 100
	taskCommentListDefaultPageSize = 100
)

var (
	taskStartSessionPollTimeout  = 7 * time.Second
	taskStartSessionPollInterval = 200 * time.Millisecond
)

type taskListOutput struct {
	ProjectID     string         `json:"project_id"`
	NextPageToken string         `json:"next_page_token,omitempty"`
	Tasks         []taskListItem `json:"tasks"`
}

type taskListItem struct {
	ShortID    string `json:"short_id"`
	TaskID     string `json:"task_id"`
	WorkflowID string `json:"workflow_id"`
	Status     string `json:"status"`
	Title      string `json:"title"`
}

type taskShowOutput struct {
	Summary         serverapi.WorkflowTaskSummary     `json:"summary"`
	Body            string                            `json:"body"`
	SourceURL       string                            `json:"source_url,omitempty"`
	Project         serverapi.ProjectBoardProject     `json:"project"`
	Workflow        serverapi.WorkflowPickerItem      `json:"workflow"`
	SourceWorkspace serverapi.ProjectWorkspaceSummary `json:"source_workspace"`
	ManagedWorktree *serverapi.WorktreeView           `json:"managed_worktree,omitempty"`
	Status          serverapi.WorkflowTaskStatus      `json:"status"`
	Actions         serverapi.WorkflowTaskActions     `json:"actions"`
	AttentionCount  int                               `json:"attention_count"`
	PlacementCount  int                               `json:"placement_count"`
	RunCount        int                               `json:"run_count"`
	TransitionCount int                               `json:"transition_count"`
	CommentCount    int                               `json:"comment_count"`
}

func taskSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := newCommandFlagSet(config.Command+" task", stderr, taskUsage)
		fs.Usage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "create":
		return taskCreateSubcommand(args[1:], stdout, stderr)
	case "edit":
		return taskEditSubcommand(args[1:], stdout, stderr)
	case "start":
		return taskStartSubcommand(args[1:], stdout, stderr)
	case "list":
		return taskListSubcommand(args[1:], stdout, stderr)
	case "show":
		return taskShowSubcommand(args[1:], stdout, stderr)
	case "cancel":
		return taskCancelSubcommand(args[1:], stdout, stderr)
	case "delete":
		return taskDeleteSubcommand(args[1:], stdout, stderr)
	case "approve":
		return taskApproveSubcommand(args[1:], stdout, stderr)
	case "move":
		return taskMoveSubcommand(args[1:], stdout, stderr)
	case "complete":
		return taskCompleteSubcommand(args[1:], stdout, stderr)
	case "resume":
		return taskResumeSubcommand(args[1:], stdout, stderr)
	case "comment", "comments":
		return taskCommentSubcommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown task command: %s\n\n", args[0])
		fs := newCommandFlagSet(config.Command+" task", stderr, taskUsage)
		taskUsage.write(fs)
		return 2
	}
}

func taskCreateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task create", stderr, taskCommandUsage)
	title := fs.String("title", "", "task title")
	body := fs.String("body", "", "task body")
	bodyFile := fs.String("body-file", "", "path to task body file")
	workflowRef := fs.String("workflow", "", "workflow id or exact workflow name")
	projectRef := fs.String("project", ".", "project id or path")
	sourceURL := fs.String("source-url", "", "external source URL")
	sourceWorkspace := fs.String("source-workspace", "", "source workspace id or path")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "task create does not accept positional arguments")
		return 2
	}
	taskBody, err := readTaskBodyFlag(*body, *bodyFile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
	sourceWorkspaceID := ""
	if strings.TrimSpace(*sourceWorkspace) != "" {
		sourceWorkspaceID, err = resolveWorkflowSourceWorkspaceID(context.Background(), cfg, remote, *sourceWorkspace)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: projectID, WorkflowID: workflowID, Title: *title, Body: taskBody, SourceURL: *sourceURL, SourceWorkspaceID: sourceWorkspaceID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	task, err := getWorkflowTaskByID(context.Background(), remote, resp.Task.ID)
	if err != nil {
		fmt.Fprintf(stderr, "created task %s but failed to load task detail for output: %v\n", resp.Task.ID, err)
		return 1
	}
	if *jsonOut {
		if err := json.NewEncoder(stdout).Encode(taskShowOutputFromDetail(task)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	writeTaskDetail(stdout, task)
	return 0
}

func taskEditSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task edit", stderr, taskCommandUsage)
	title := fs.String("title", "", "new task title")
	body := fs.String("body", "", "new task body")
	bodyFile := fs.String("body-file", "", "path to new task body file")
	sourceWorkspace := fs.String("source-workspace", "", "source workspace id or path")
	projectRef := fs.String("project", ".", "project id or path for short ids")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task edit requires <short-id-or-task-id>")
		return 2
	}
	titleProvided := flagWasProvided(fs, "title")
	bodyProvided := flagWasProvided(fs, "body")
	bodyFileProvided := flagWasProvided(fs, "body-file")
	workspaceProvided := flagWasProvided(fs, "source-workspace")
	if !titleProvided && !bodyProvided && !bodyFileProvided && !workspaceProvided {
		fmt.Fprintln(stderr, "task edit requires at least one of --title, --body, --body-file, or --source-workspace")
		return 2
	}
	if bodyProvided && bodyFileProvided {
		fmt.Fprintln(stderr, "--body cannot be combined with --body-file")
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
	// UpdateWorkflowTask requires a title, so reuse the current one unless the
	// caller is changing it.
	current, err := getWorkflowTaskByID(context.Background(), remote, taskID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	req := serverapi.WorkflowTaskUpdateRequest{TaskID: taskID, Title: current.Summary.Title}
	if titleProvided {
		req.Title = *title
	}
	if bodyProvided || bodyFileProvided {
		newBody, err := readTaskEditBody(*body, *bodyFile, bodyFileProvided)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		req.Body = &newBody
	}
	if workspaceProvided {
		workspaceID, err := resolveWorkflowSourceWorkspaceID(context.Background(), cfg, remote, *sourceWorkspace)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		req.SourceWorkspaceID = workspaceID
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.UpdateWorkflowTask(ctx, req)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		if err := json.NewEncoder(stdout).Encode(resp); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "Edited task %s.\n", taskSummaryDisplayID(resp.Task))
	return 0
}

// readTaskEditBody reads the replacement body for task edit. Unlike task create,
// an empty value is allowed (it clears the body) since the caller opted into a
// body change by passing the flag.
func readTaskEditBody(body string, bodyFile string, bodyFileProvided bool) (string, error) {
	if bodyFileProvided {
		content, err := os.ReadFile(bodyFile)
		if err != nil {
			return "", fmt.Errorf("read --body-file: %w", err)
		}
		return string(content), nil
	}
	return body, nil
}

func taskStartSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task start", stderr, taskCommandUsage)
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
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{TaskID: taskID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	detail, err := waitForWorkflowTaskRunSession(context.Background(), remote, taskID, resp.RunID, taskStartSessionPollTimeout, taskStartSessionPollInterval)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	writeTaskStartResult(stdout, detail, resp)
	return 0
}

func taskListSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task list", stderr, taskCommandUsage)
	projectRef := fs.String("project", ".", "project id or path")
	pageSize := fs.Int("page-size", taskListDefaultPageSize, "maximum tasks to print")
	pageToken := fs.String("page-token", "", "page token from a previous task list response")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "task list does not accept positional arguments")
		return 2
	}
	if *pageSize < 1 {
		fmt.Fprintln(stderr, "task list requires --page-size to be positive")
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	tasks, statusByTaskID, projectID, nextPageToken, err := workflowTaskPageForProject(context.Background(), cfg, remote, *projectRef, *pageSize, *pageToken)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	items := taskListItems(tasks, statusByTaskID)
	if *jsonOut {
		if err := json.NewEncoder(stdout).Encode(taskListOutput{ProjectID: projectID, NextPageToken: nextPageToken, Tasks: items}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	for _, item := range items {
		fmt.Fprintf(stdout, "%s: %s.\nStatus: %s\n", item.ShortID, item.Title, item.Status)
	}
	if strings.TrimSpace(nextPageToken) != "" {
		fmt.Fprintf(stderr, "Next page token: `%s`\n", nextPageToken)
	}
	return 0
}

func taskListItems(tasks []serverapi.WorkflowTaskSummary, statusByTaskID map[string]string) []taskListItem {
	items := make([]taskListItem, 0, len(tasks))
	for _, task := range tasks {
		status, ok := statusByTaskID[task.ID]
		if !ok {
			status = taskListStatus(task)
		}
		items = append(items, taskListItem{
			ShortID:    task.ShortID,
			TaskID:     task.ID,
			WorkflowID: task.WorkflowID,
			Status:     status,
			Title:      task.Title,
		})
	}
	return items
}

// taskListStatusByID maps each board task to a CLI list status using the
// board card's authoritative status kind, which distinguishes an actually
// running task from a backlog task that merely has an active start-node
// placement. The summary alone can't make that distinction.
func taskListStatusByID(board serverapi.WorkflowBoard) map[string]string {
	statuses := make(map[string]string)
	for _, card := range board.Cards {
		statuses[card.TaskID] = taskListStatusFromCardStatus(card.Status)
	}
	for _, card := range board.DonePreview {
		statuses[card.TaskID] = taskListStatusFromCardStatus(card.Status)
	}
	return statuses
}

func taskListStatusFromCardStatus(status serverapi.WorkflowTaskStatus) string {
	switch status.Kind {
	case "done":
		return "done"
	case "canceled":
		return "canceled"
	case "running", "interrupted", "waiting_question", "waiting_approval":
		return "running"
	default:
		// backlog, active, and any future kind: no active run, so it is open.
		return "open"
	}
}

func taskListStatus(task serverapi.WorkflowTaskSummary) string {
	if task.CanceledAt != 0 {
		return "canceled"
	}
	if task.Done {
		return "done"
	}
	if len(task.ActiveNodeIDs) > 0 {
		return "running"
	}
	return "open"
}

func taskShowSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task show", stderr, taskCommandUsage)
	projectRef := fs.String("project", ".", "project id or path for short ids")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task show requires <short-id-or-task-id>")
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
		fmt.Fprintf(stderr, "Note: This task belongs to another project %s\n", task.Project.ProjectKey)
	}
	if *jsonOut {
		if err := json.NewEncoder(stdout).Encode(taskShowOutputFromDetail(task)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	writeTaskDetail(stdout, task)
	return 0
}

func taskShowOutputFromDetail(task serverapi.WorkflowTaskDetail) taskShowOutput {
	return taskShowOutput{
		Summary:         task.Summary,
		Body:            task.Body,
		SourceURL:       task.SourceURL,
		Project:         task.Project,
		Workflow:        task.Workflow,
		SourceWorkspace: task.SourceWorkspace,
		ManagedWorktree: task.ManagedWorktree,
		Status:          task.Status,
		Actions:         task.Actions,
		AttentionCount:  len(task.Attention),
		PlacementCount:  len(task.Placements),
		RunCount:        len(task.Runs),
		TransitionCount: len(task.Transitions),
		CommentCount:    len(task.Comments),
	}
}

func taskCancelSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task cancel", stderr, taskCommandUsage)
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
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	if err := remote.CancelWorkflowTask(ctx, serverapi.WorkflowTaskCancelRequest{TaskID: taskID, Reason: *reason}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	detail, err := getWorkflowTaskByID(context.Background(), remote, taskID)
	if err != nil {
		fmt.Fprintf(stderr, "canceled task %s but failed to load task detail for output: %v\n", taskID, err)
		return 1
	}
	fmt.Fprintf(stdout, "Canceled task %s.\n", taskDisplayID(detail))
	return 0
}

func taskDeleteSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task delete", stderr, taskCommandUsage)
	projectRef := fs.String("project", ".", "project id or path for short ids")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task delete requires <short-id-or-task-id>")
		return 2
	}
	if denyAgentHumanOnlyTaskAction(stderr) {
		return 1
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
	// Load the task detail before deletion so we can report a stable display id;
	// the task no longer exists once the delete RPC succeeds.
	displayID := taskID
	if detail, err := getWorkflowTaskByID(context.Background(), remote, taskID); err == nil {
		displayID = taskDisplayID(detail)
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	if err := remote.DeleteWorkflowTask(ctx, serverapi.WorkflowTaskDeleteRequest{TaskID: taskID}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "Deleted task %s.\n", displayID)
	return 0
}

func taskResumeSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task resume", stderr, taskCommandUsage)
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
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{TaskID: taskID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	detail, err := getWorkflowTaskByID(context.Background(), remote, taskID)
	if err != nil {
		fmt.Fprintf(stderr, "resumed task %s but failed to load task detail for output: %v\n", taskID, err)
		return 1
	}
	writeTaskResumeResult(stdout, detail, resp)
	return 0
}

func taskApproveSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task approve", stderr, taskCommandUsage)
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
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.ApproveWorkflowTask(ctx, serverapi.WorkflowTaskApproveRequest{TransitionID: positionals[0]})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if strings.TrimSpace(resp.TaskID) == "" {
		fmt.Fprintf(stderr, "approved transition %s but response did not include task id for output\n", resp.TransitionID)
		return 1
	}
	detail, err := getWorkflowTaskByID(context.Background(), remote, resp.TaskID)
	if err != nil {
		fmt.Fprintf(stderr, "approved transition %s but failed to load task detail for output: %v\n", resp.TransitionID, err)
		return 1
	}
	writeTaskTransitionResult(stdout, "Approved transition of", detail, resp.TransitionID, resp.RunIDs)
	return 0
}

func taskMoveSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task move", stderr, taskCommandUsage)
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
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.MoveWorkflowTask(ctx, serverapi.WorkflowTaskMoveRequest{TaskID: taskID, TargetNodeID: positionals[1], OutputValues: outputs.values, Commentary: *commentary})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	detail, err := getWorkflowTaskByID(context.Background(), remote, taskID)
	if err != nil {
		fmt.Fprintf(stderr, "moved task %s but failed to load task detail for output: %v\n", taskID, err)
		return 1
	}
	writeTaskTransitionResult(stdout, "Moved task", detail, resp.TransitionID, resp.RunIDs)
	return 0
}

type taskCompleteArgs struct {
	RunID          string
	SessionID      string
	TaskRef        string
	ProjectRef     string
	TransitionID   string
	Commentary     string
	Force          bool
	JSONPayload    string
	JSONFile       string
	JSONPayloadSet bool
	JSONFileSet    bool
	OutputValues   map[string]string
	FieldFlagsUsed bool
}

func taskCompleteSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	parsed, ok, exitCode := parseTaskCompleteArgs(args, stderr)
	if !ok {
		return exitCode
	}
	agentSessionID, agentContext := sessionenv.LookupSessionID(os.LookupEnv)
	if agentContext && parsed.Force {
		fmt.Fprintln(stderr, prompts.WorkflowHumanOnlyTaskActionDeniedPrompt)
		return 1
	}
	if !agentContext && !parsed.Force {
		fmt.Fprintln(stderr, prompts.WorkflowTaskCompleteHumanSafetyWarningPrompt)
		return 1
	}
	if count := parsed.selectorCount(); count > 1 {
		fmt.Fprintln(stderr, "at most one completion target selector is allowed")
		return 2
	} else if !agentContext && count != 1 {
		fmt.Fprintln(stderr, "task complete --force requires exactly one explicit selector: --run, --session, or --task")
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	req, err := parsed.request(context.Background(), cfg, remote, agentSessionID, agentContext)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := req.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.CompleteWorkflowTask(ctx, req)
	if err != nil {
		fmt.Fprintln(stderr, taskCompleteErrorMessage(err))
		return 1
	}
	if parsed.JSONPayloadSet || parsed.JSONFileSet {
		if err := json.NewEncoder(stdout).Encode(resp); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	writeTaskCompleteResult(stdout, resp)
	return 0
}

func (a taskCompleteArgs) selectorCount() int {
	count := 0
	for _, value := range []string{a.RunID, a.SessionID, a.TaskRef} {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func (a taskCompleteArgs) request(ctx context.Context, cfg config.App, remote workflowCommandRemote, agentSessionID string, agentContext bool) (serverapi.WorkflowTaskCompleteRequest, error) {
	req := serverapi.WorkflowTaskCompleteRequest{
		RunID:        strings.TrimSpace(a.RunID),
		SessionID:    strings.TrimSpace(a.SessionID),
		TransitionID: strings.TrimSpace(a.TransitionID),
		OutputValues: cloneStringMap(a.OutputValues),
		Commentary:   a.Commentary,
	}
	if len(req.OutputValues) == 0 {
		req.OutputValues = nil
	}
	if agentContext {
		req.ActorKind = serverapi.WorkflowTaskCompleteActorAgent
		req.AgentSessionID = strings.TrimSpace(agentSessionID)
	} else {
		req.ActorKind = serverapi.WorkflowTaskCompleteActorUser
		req.Force = a.Force
	}
	taskRef := strings.TrimSpace(a.TaskRef)
	if taskRef == "" {
		return req, nil
	}
	if strings.HasPrefix(taskRef, "task-") {
		req.TaskID = taskRef
		return req, nil
	}
	projectID, err := resolveWorkflowProjectID(ctx, cfg, remote, a.ProjectRef)
	if err != nil {
		return serverapi.WorkflowTaskCompleteRequest{}, err
	}
	req.ProjectID = projectID
	req.ShortID = taskRef
	return req, nil
}

func parseTaskCompleteArgs(args []string, stderr io.Writer) (taskCompleteArgs, bool, int) {
	parsed := taskCompleteArgs{ProjectRef: ".", OutputValues: map[string]string{}}
	for index := 0; index < len(args); index++ {
		raw := args[index]
		name, inlineValue, hasInlineValue, ok := taskCompleteFlag(raw)
		if !ok {
			fmt.Fprintf(stderr, "task complete does not accept positional arguments: %s\n", raw)
			return taskCompleteArgs{}, false, 2
		}
		switch name {
		case "help", "h":
			writeTaskCompleteUsage(stderr)
			return taskCompleteArgs{}, false, 0
		case "force":
			value, err := taskCompleteBoolFlagValue(inlineValue, hasInlineValue)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			parsed.Force = value
		case "run":
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			parsed.RunID = strings.TrimSpace(value)
		case "session":
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			parsed.SessionID = strings.TrimSpace(value)
		case "task":
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			parsed.TaskRef = strings.TrimSpace(value)
		case "project":
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			parsed.ProjectRef = strings.TrimSpace(value)
		case "transition":
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			parsed.TransitionID = value
			parsed.FieldFlagsUsed = true
		case "commentary":
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			parsed.Commentary = value
			parsed.FieldFlagsUsed = true
		case "param":
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			if err := setTaskCompleteOutputValue(parsed.OutputValues, value); err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			parsed.FieldFlagsUsed = true
		case "json":
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			parsed.JSONPayload = value
			parsed.JSONPayloadSet = true
		case "json-file":
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			parsed.JSONFile = strings.TrimSpace(value)
			parsed.JSONFileSet = true
		default:
			value, next, err := taskCompleteStringFlagValue(args, index, inlineValue, hasInlineValue, name)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return taskCompleteArgs{}, false, 2
			}
			index = next
			parsed.OutputValues[name] = value
			parsed.FieldFlagsUsed = true
		}
	}
	if parsed.JSONPayloadSet && parsed.JSONFileSet {
		fmt.Fprintln(stderr, "--json cannot be combined with --json-file")
		return taskCompleteArgs{}, false, 2
	}
	if (parsed.JSONPayloadSet || parsed.JSONFileSet) && parsed.FieldFlagsUsed {
		fmt.Fprintln(stderr, "--json cannot be combined with completion field flags")
		return taskCompleteArgs{}, false, 2
	}
	if parsed.JSONPayloadSet || parsed.JSONFileSet {
		if err := parsed.applyJSONPayload(); err != nil {
			fmt.Fprintln(stderr, err)
			return taskCompleteArgs{}, false, 2
		}
	}
	return parsed, true, 0
}

func taskCompleteFlag(raw string) (string, string, bool, bool) {
	if !strings.HasPrefix(raw, "-") || raw == "-" {
		return "", "", false, false
	}
	trimmed := strings.TrimLeft(raw, "-")
	if trimmed == "" {
		return "", "", false, false
	}
	name, value, hasValue := strings.Cut(trimmed, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", false, false
	}
	return name, value, hasValue, true
}

func taskCompleteStringFlagValue(args []string, index int, inlineValue string, hasInlineValue bool, name string) (string, int, error) {
	if hasInlineValue {
		return inlineValue, index, nil
	}
	next := index + 1
	if next >= len(args) {
		return "", index, fmt.Errorf("--%s requires a value", name)
	}
	if strings.HasPrefix(args[next], "-") && args[next] != "-" {
		return "", index, fmt.Errorf("--%s requires a value", name)
	}
	return args[next], next, nil
}

func taskCompleteBoolFlagValue(inlineValue string, hasInlineValue bool) (bool, error) {
	if !hasInlineValue {
		return true, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(inlineValue))
	if err != nil {
		return false, fmt.Errorf("--force requires a boolean value when assigned with '='")
	}
	return value, nil
}

func setTaskCompleteOutputValue(values map[string]string, raw string) error {
	name, value, ok := strings.Cut(raw, "=")
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return fmt.Errorf("param must be name=value")
	}
	values[name] = value
	return nil
}

func (a *taskCompleteArgs) applyJSONPayload() error {
	raw := a.JSONPayload
	if a.JSONFileSet {
		content, err := os.ReadFile(a.JSONFile)
		if err != nil {
			return fmt.Errorf("read --json-file: %w", err)
		}
		raw = string(content)
	}
	fields, err := parseTaskCompleteJSONPayload(raw)
	if err != nil {
		return err
	}
	a.TransitionID = fields.TransitionID
	a.Commentary = fields.Commentary
	a.OutputValues = fields.OutputValues
	return nil
}

type taskCompleteJSONFields struct {
	TransitionID string
	Commentary   string
	OutputValues map[string]string
}

func parseTaskCompleteJSONPayload(raw string) (taskCompleteJSONFields, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	var payload map[string]json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		return taskCompleteJSONFields{}, fmt.Errorf("parse --json payload: %w", err)
	}
	if payload == nil {
		return taskCompleteJSONFields{}, errors.New("parse --json payload: expected one JSON object")
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return taskCompleteJSONFields{}, errors.New("parse --json payload: expected one JSON object")
	}
	out := taskCompleteJSONFields{OutputValues: map[string]string{}}
	if rawOutputValues, ok := payload["output_values"]; ok {
		values, err := taskCompleteJSONOutputValues(rawOutputValues)
		if err != nil {
			return taskCompleteJSONFields{}, err
		}
		for key, value := range values {
			out.OutputValues[key] = value
		}
	}
	for _, key := range sortedRawJSONKeys(payload) {
		switch key {
		case "output_values":
			continue
		case "transition", "transition_id":
			value, ok, err := taskCompleteJSONStringValue(payload[key], key)
			if err != nil {
				return taskCompleteJSONFields{}, err
			}
			if !ok {
				continue
			}
			trimmed := strings.TrimSpace(value)
			if out.TransitionID != "" && trimmed != "" && out.TransitionID != trimmed {
				return taskCompleteJSONFields{}, errors.New("parse --json payload: transition and transition_id cannot disagree")
			}
			out.TransitionID = trimmed
		case "commentary":
			value, ok, err := taskCompleteJSONStringValue(payload[key], key)
			if err != nil {
				return taskCompleteJSONFields{}, err
			}
			if ok {
				out.Commentary = value
			}
		case "run_id", "session_id", "task_id", "project_id", "short_id", "actor_kind", "agent_session_id", "force":
			return taskCompleteJSONFields{}, fmt.Errorf("parse --json payload: %s must be passed as a flag, not in the JSON payload", key)
		default:
			value, ok, err := taskCompleteJSONParameterValue(payload[key], key)
			if err != nil {
				return taskCompleteJSONFields{}, err
			}
			if ok {
				out.OutputValues[key] = value
			}
		}
	}
	return out, nil
}

func taskCompleteJSONOutputValues(raw json.RawMessage) (map[string]string, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return map[string]string{}, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse --json payload: output_values must be an object")
	}
	values := map[string]string{}
	for _, key := range sortedRawJSONKeys(payload) {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return nil, errors.New("parse --json payload: output_values field name is required")
		}
		value, ok, err := taskCompleteJSONParameterValue(payload[key], "output_values."+trimmed)
		if err != nil {
			return nil, err
		}
		if ok {
			values[trimmed] = value
		}
	}
	return values, nil
}

func taskCompleteJSONStringValue(raw json.RawMessage, field string) (string, bool, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false, fmt.Errorf("parse --json payload: %s must be a string", field)
	}
	return value, true, nil
}

func taskCompleteJSONParameterValue(raw json.RawMessage, field string) (string, bool, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return "null", true, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, true, nil
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, bytes.TrimSpace(raw)); err != nil {
		return "", false, fmt.Errorf("parse --json payload: %s must be valid JSON", field)
	}
	return compacted.String(), true, nil
}

func sortedRawJSONKeys(payload map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func taskCompleteErrorMessage(err error) string {
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, serverapi.ErrWorkflowTaskCompleteTargetNotFound):
		return "no active unfinished agent run matched the completion selector. Retry with --run <run-id>, --session <session-id>, or --task <task-id-or-short-id>."
	case errors.Is(err, serverapi.ErrWorkflowTaskCompleteSelectorAmbiguous):
		return "the completion selector matched multiple active workflow runs. Retry with --run <run-id> or the current Kent session."
	default:
		return err.Error()
	}
}

func writeTaskCompleteUsage(stderr io.Writer) {
	fs := newCommandFlagSet(config.Command+" task complete", stderr, taskCommandUsage)
	fs.String("run", "", "active workflow run id to complete")
	fs.String("session", "", "Kent session id whose active workflow run should be completed")
	fs.String("task", "", "task id or short id whose active workflow run should be completed")
	fs.String("project", ".", "project id or path for task short ids")
	fs.String("transition", "", "workflow transition id")
	fs.String("commentary", "", "transition commentary")
	fs.String("param", "", "completion parameter as name=value; repeatable")
	fs.String("json", "", "completion payload JSON; implies JSON response output")
	fs.String("json-file", "", "path to completion payload JSON; implies JSON response output")
	fs.Bool("force", false, "allow non-agent completion with an explicit selector")
	fs.Usage()
}

func writeTaskCompleteResult(stdout io.Writer, resp serverapi.WorkflowTaskCompleteResponse) {
	fmt.Fprintf(stdout, "Completed task %s from run %s via transition %s.\n", strings.TrimSpace(resp.TaskID), strings.TrimSpace(resp.RunID), strings.TrimSpace(resp.TransitionID))
	if state := strings.TrimSpace(resp.State); state != "" && state != "applied" {
		fmt.Fprintf(stdout, "State: %s\n", state)
	}
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
		fs := newCommandFlagSet(config.Command+" task comment", stderr, taskUsage)
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
		fs := newCommandFlagSet(config.Command+" task comment", stderr, taskUsage)
		taskUsage.write(fs)
		return 2
	}
}

func taskCommentAddSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task comment add", stderr, taskCommandUsage)
	body := fs.String("body", "", "comment body")
	bodyFile := fs.String("body-file", "", "path to comment body file")
	author := fs.String("author", "", "comment author")
	authorID := fs.String("author-id", "", "comment author id")
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
	commentBody, err := readTaskBodyFlag(*body, *bodyFile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
	commentAuthor := taskCommentAuthorForAdd(context.Background(), remote, taskID, *author, flagWasProvided(fs, "author"))
	if trimmedAuthorID := strings.TrimSpace(*authorID); trimmedAuthorID != "" {
		commentAuthor.ID = trimmedAuthorID
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.AddWorkflowTaskComment(ctx, serverapi.WorkflowTaskCommentAddRequest{TaskID: taskID, Body: commentBody, Author: commentAuthor.Kind, AuthorID: commentAuthor.ID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "comment_id\t%s\ntask_id\t%s\n", resp.Comment.ID, resp.Comment.TaskID)
	return 0
}

func readTaskBodyFlag(body string, bodyFile string) (string, error) {
	trimmedBody := strings.TrimSpace(body)
	if strings.TrimSpace(bodyFile) == "" {
		if trimmedBody == "" {
			return "", errors.New("either --body or --body-file is required")
		}
		return body, nil
	}
	if body != "" {
		return "", errors.New("--body cannot be combined with --body-file")
	}
	content, err := os.ReadFile(bodyFile)
	if err != nil {
		return "", fmt.Errorf("read --body-file: %w", err)
	}
	return string(content), nil
}

type taskCommentAuthor struct {
	Kind string
	ID   string
}

func taskCommentAuthorForAdd(ctx context.Context, remote workflowCommandRemote, taskID string, explicitAuthor string, explicit bool) taskCommentAuthor {
	if explicit {
		return taskCommentAuthor{Kind: strings.TrimSpace(explicitAuthor)}
	}
	sessionID, ok := sessionenv.LookupSessionID(os.LookupEnv)
	if !ok {
		return taskCommentAuthor{Kind: "user"}
	}
	detail, err := getWorkflowTaskByID(ctx, remote, taskID)
	if err == nil {
		if authorID := workflowTaskAgentAuthorID(detail, sessionID); authorID != "" {
			return taskCommentAuthor{Kind: "agent", ID: authorID}
		}
	}
	return taskCommentAuthor{Kind: "agent", ID: sessionAgentAuthorID(ctx, remote, sessionID)}
}

func workflowTaskAgentAuthorID(task serverapi.WorkflowTaskDetail, sessionID string) string {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return ""
	}
	nodeKeyByID := map[string]string{}
	for _, placement := range task.Placements {
		if strings.TrimSpace(placement.NodeID) != "" && strings.TrimSpace(placement.NodeKey) != "" {
			nodeKeyByID[placement.NodeID] = strings.TrimSpace(placement.NodeKey)
		}
	}
	run, ok := workflowTaskAgentRun(task, trimmedSessionID)
	if !ok {
		return ""
	}
	if role := strings.TrimSpace(run.Role); role != "" {
		return role
	}
	if nodeKey := nodeKeyByID[strings.TrimSpace(run.NodeID)]; nodeKey != "" {
		return fmt.Sprintf("Node %s agent", nodeKey)
	}
	if nodeID := strings.TrimSpace(run.NodeID); nodeID != "" {
		return fmt.Sprintf("Node %s agent", nodeID)
	}
	if sessionName := strings.TrimSpace(run.SessionName); sessionName != "" {
		return fmt.Sprintf("Session %s agent", sessionName)
	}
	return fmt.Sprintf("Session %s agent", trimmedSessionID)
}

func workflowTaskAgentRun(task serverapi.WorkflowTaskDetail, sessionID string) (serverapi.WorkflowRun, bool) {
	currentRunIDs := map[string]bool{}
	for _, runID := range task.Status.RunIDs {
		if strings.TrimSpace(runID) != "" {
			currentRunIDs[strings.TrimSpace(runID)] = true
		}
	}
	var selected serverapi.WorkflowRun
	selectedScore := workflowTaskAgentRunScore{}
	found := false
	for _, run := range task.Runs {
		if strings.TrimSpace(run.SessionID) != sessionID {
			continue
		}
		score := workflowTaskAgentRunScore{
			Current:       currentRunIDs[strings.TrimSpace(run.ID)],
			Unfinished:    run.CompletedAtUnixMs == 0 && run.InterruptedAtUnixMs == 0,
			StartedAt:     run.StartedAtUnixMs,
			CompletedAt:   run.CompletedAtUnixMs,
			InterruptedAt: run.InterruptedAtUnixMs,
			Generation:    run.Generation,
			ID:            strings.TrimSpace(run.ID),
		}
		if !found || score.betterThan(selectedScore) {
			selected = run
			selectedScore = score
			found = true
		}
	}
	return selected, found
}

type workflowTaskAgentRunScore struct {
	Current       bool
	Unfinished    bool
	StartedAt     int64
	CompletedAt   int64
	InterruptedAt int64
	Generation    int64
	ID            string
}

func (s workflowTaskAgentRunScore) betterThan(other workflowTaskAgentRunScore) bool {
	switch {
	case s.Current != other.Current:
		return s.Current
	case s.Unfinished != other.Unfinished:
		return s.Unfinished
	case s.StartedAt != other.StartedAt:
		return s.StartedAt > other.StartedAt
	case s.CompletedAt != other.CompletedAt:
		return s.CompletedAt > other.CompletedAt
	case s.InterruptedAt != other.InterruptedAt:
		return s.InterruptedAt > other.InterruptedAt
	case s.Generation != other.Generation:
		return s.Generation > other.Generation
	default:
		return s.ID > other.ID
	}
}

type sessionMainViewGetter interface {
	GetSessionMainView(ctx context.Context, req serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error)
}

func sessionAgentAuthorID(ctx context.Context, remote workflowCommandRemote, sessionID string) string {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if getter, ok := remote.(sessionMainViewGetter); ok {
		rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
		defer cancel()
		resp, err := getter.GetSessionMainView(rpcCtx, serverapi.SessionMainViewRequest{SessionID: trimmedSessionID})
		if err == nil {
			if sessionName := strings.TrimSpace(resp.MainView.Session.SessionName); sessionName != "" {
				return fmt.Sprintf("Session %s agent", sessionName)
			}
		}
	}
	return fmt.Sprintf("Session %s agent", trimmedSessionID)
}

func taskCommentListSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task comment list", stderr, taskCommandUsage)
	projectRef := fs.String("project", ".", "project id or path for short ids")
	pageSize := fs.Int("page-size", taskCommentListDefaultPageSize, "maximum comments to print")
	pageToken := fs.String("page-token", "", "page token from a previous task comment list response")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task comment list requires <short-id-or-task-id>")
		return 2
	}
	if *pageSize < 1 {
		fmt.Fprintln(stderr, "task comment list requires --page-size to be positive")
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
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
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.ListWorkflowTaskComments(ctx, serverapi.WorkflowTaskCommentListRequest{TaskID: taskID, PageSize: *pageSize, PageToken: *pageToken})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	writeTaskCommentList(stdout, resp.Comments)
	if strings.TrimSpace(resp.NextPageToken) != "" {
		fmt.Fprintf(stderr, "Next page token: `%s`\n", resp.NextPageToken)
	}
	return 0
}

func writeTaskCommentList(stdout io.Writer, comments []serverapi.WorkflowTaskComment) {
	if len(comments) == 0 {
		return
	}
	fmt.Fprintf(stdout, "Comments (%d):\n", len(comments))
	writeTaskCommentBlocks(stdout, comments)
}

func taskCommentReplaceSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task comment replace", stderr, taskCommandUsage)
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
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	if err := remote.ReplaceWorkflowTaskComment(ctx, serverapi.WorkflowTaskCommentReplaceRequest{CommentID: positionals[0], Body: *body}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "replaced_comment_id\t%s\n", positionals[0])
	return 0
}

func taskCommentDeleteSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task comment delete", stderr, taskUsage)
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
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	if err := remote.DeleteWorkflowTaskComment(ctx, serverapi.WorkflowTaskCommentDeleteRequest{CommentID: positionals[0]}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "deleted_comment_id\t%s\n", positionals[0])
	return 0
}

func denyAgentHumanOnlyTaskAction(stderr io.Writer) bool {
	if _, ok := sessionenv.LookupSessionID(os.LookupEnv); !ok {
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
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowBoard(rpcCtx, serverapi.WorkflowBoardRequest{ProjectID: projectID})
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	return resp.Board, nil
}

func workflowTaskPageForProject(ctx context.Context, cfg config.App, remote workflowCommandRemote, projectRef string, pageSize int, pageToken string) ([]serverapi.WorkflowTaskSummary, map[string]string, string, string, error) {
	projectID, err := resolveWorkflowProjectID(ctx, cfg, remote, projectRef)
	if err != nil {
		return nil, nil, "", "", err
	}
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowBoard(rpcCtx, serverapi.WorkflowBoardRequest{
		ProjectID: projectID,
		PageSize:  pageSize,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, nil, "", "", err
	}
	board := resp.Board
	tasks := workflowTasksForListPage(board, pageSize)
	return tasks, taskListStatusByID(board), board.ProjectID, board.NextPageToken, nil
}

func workflowTasksForListPage(board serverapi.WorkflowBoard, pageSize int) []serverapi.WorkflowTaskSummary {
	cards := append([]serverapi.WorkflowBoardTaskCard(nil), board.Cards...)
	// Board pagination only covers open tasks. When the open stream is
	// exhausted (no next token), surface the bounded done preview so done tasks
	// stay reachable from `task list` — including the boundary case where open
	// cards already fill the page, which previously hid them entirely.
	if strings.TrimSpace(board.NextPageToken) == "" {
		donePreview := board.DonePreview
		if remaining := pageSize - len(cards); remaining > 0 && len(donePreview) > remaining {
			donePreview = donePreview[:remaining]
		}
		cards = append(cards, donePreview...)
	}
	return sortedWorkflowTasksFromCards(serverapi.WorkflowBoard{ProjectID: board.ProjectID}, cards)
}

func resolveWorkflowTaskID(ctx context.Context, cfg config.App, remote workflowCommandRemote, projectRef string, ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", errors.New("task id is required")
	}
	if strings.HasPrefix(trimmed, "task-") {
		rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
		defer cancel()
		resp, err := remote.GetWorkflowTask(rpcCtx, serverapi.WorkflowTaskGetRequest{TaskID: trimmed})
		if err != nil {
			return "", err
		}
		return resp.Task.Summary.ID, nil
	}
	projectID, err := resolveWorkflowProjectID(ctx, cfg, remote, projectRef)
	if err != nil {
		return "", err
	}
	detail, err := getWorkflowTaskByProjectShortID(ctx, remote, projectID, trimmed)
	if err != nil {
		if errors.Is(err, serverapi.ErrWorkflowTaskNotFound) || errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("task %q not found in project %s", trimmed, projectID)
		}
		return "", err
	}
	return detail.Summary.ID, nil
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
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowTask(rpcCtx, serverapi.WorkflowTaskGetRequest{TaskID: taskID})
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	return resp.Task, nil
}

func getWorkflowTaskByProjectShortID(ctx context.Context, remote workflowCommandRemote, projectID string, shortID string) (serverapi.WorkflowTaskDetail, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowTask(rpcCtx, serverapi.WorkflowTaskGetRequest{ProjectID: projectID, ShortID: shortID})
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	return resp.Task, nil
}

func getWorkflowTaskByShortID(ctx context.Context, remote workflowCommandRemote, shortID string) (serverapi.WorkflowTaskDetail, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowTask(rpcCtx, serverapi.WorkflowTaskGetRequest{ShortID: shortID})
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	return resp.Task, nil
}

func waitForWorkflowTaskRunSession(ctx context.Context, remote workflowCommandRemote, taskID string, runID string, timeout time.Duration, interval time.Duration) (serverapi.WorkflowTaskDetail, error) {
	if strings.TrimSpace(taskID) == "" {
		return serverapi.WorkflowTaskDetail{}, errors.New("task id is required")
	}
	trimmedRunID := strings.TrimSpace(runID)
	if trimmedRunID == "" {
		return serverapi.WorkflowTaskDetail{}, errors.New("run id is required")
	}
	if interval <= 0 {
		interval = taskStartSessionPollInterval
	}
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		detail, err := getWorkflowTaskByID(pollCtx, remote, taskID)
		if err != nil {
			if pollCtx.Err() != nil {
				return serverapi.WorkflowTaskDetail{}, fmt.Errorf("started task %s with run %s but session id was not assigned within %s", taskID, trimmedRunID, timeout)
			}
			return serverapi.WorkflowTaskDetail{}, fmt.Errorf("started task %s with run %s but failed to load task detail while waiting for session id: %w", taskID, trimmedRunID, err)
		}
		if run, ok := workflowTaskRunByID(detail, trimmedRunID); ok && strings.TrimSpace(run.SessionID) != "" {
			return detail, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			return serverapi.WorkflowTaskDetail{}, fmt.Errorf("started task %s with run %s but session id was not assigned within %s", taskDisplayID(detail), trimmedRunID, timeout)
		case <-timer.C:
		}
	}
}

func writeTaskStartResult(stdout io.Writer, task serverapi.WorkflowTaskDetail, resp serverapi.WorkflowTaskStartResponse) {
	run, _ := workflowTaskRunByID(task, resp.RunID)
	sessionID := strings.TrimSpace(run.SessionID)
	placement, _ := workflowTaskPlacementByID(task, resp.PlacementID)
	nodeKey := placementDisplayKey(placement, run.NodeID)
	fmt.Fprintf(stdout, "Started task %s in session %s using workflow %q (%s).\n", taskDisplayID(task), sessionID, task.Workflow.DisplayName, task.Workflow.WorkflowID)
	fmt.Fprintf(stdout, "First node: %s\n", nodeKey)
}

func writeTaskResumeResult(stdout io.Writer, task serverapi.WorkflowTaskDetail, resp serverapi.WorkflowTaskResumeResponse) {
	sessionID := strings.TrimSpace(resp.SessionID)
	run, ok := workflowTaskRunByID(task, resp.RunID)
	if sessionID == "" && ok {
		sessionID = strings.TrimSpace(run.SessionID)
	}
	placement, _ := workflowTaskPlacementByID(task, resp.PlacementID)
	nodeKey := placementDisplayKey(placement, resp.NodeID)
	fmt.Fprintf(stdout, "Resumed task %s in session %s.\n", taskDisplayID(task), sessionID)
	fmt.Fprintf(stdout, "Current node: %s\n", nodeKey)
}

func writeTaskTransitionResult(stdout io.Writer, action string, task serverapi.WorkflowTaskDetail, transitionID string, runIDs []string) {
	transition, _ := workflowTaskTransitionByID(task, transitionID)
	fmt.Fprintf(stdout, "%s %s from `%s` to `%s`.\n", action, taskDisplayID(task), transitionStartKey(transition, transitionID), transitionEndKey(transition, transitionID))
	if nodeKey, sessionID, ok := transitionStartedRun(task, transition, runIDs); ok {
		fmt.Fprintf(stdout, "Because of this, started node %s in session %s.\n", nodeKey, sessionID)
	}
}

func workflowTaskTransitionByID(task serverapi.WorkflowTaskDetail, transitionID string) (serverapi.WorkflowTaskTransition, bool) {
	trimmedTransitionID := strings.TrimSpace(transitionID)
	for _, transition := range task.Transitions {
		if strings.TrimSpace(transition.ID) == trimmedTransitionID {
			return transition, true
		}
	}
	return serverapi.WorkflowTaskTransition{}, false
}

func workflowTaskRunByID(task serverapi.WorkflowTaskDetail, runID string) (serverapi.WorkflowRun, bool) {
	trimmedRunID := strings.TrimSpace(runID)
	for _, run := range task.Runs {
		if strings.TrimSpace(run.ID) == trimmedRunID {
			return run, true
		}
	}
	return serverapi.WorkflowRun{}, false
}

func workflowTaskPlacementByID(task serverapi.WorkflowTaskDetail, placementID string) (serverapi.WorkflowPlacement, bool) {
	trimmedPlacementID := strings.TrimSpace(placementID)
	for _, placement := range task.Placements {
		if strings.TrimSpace(placement.ID) == trimmedPlacementID {
			return placement, true
		}
	}
	return serverapi.WorkflowPlacement{}, false
}

func taskDisplayID(task serverapi.WorkflowTaskDetail) string {
	return taskSummaryDisplayID(task.Summary)
}

func taskSummaryDisplayID(summary serverapi.WorkflowTaskSummary) string {
	if shortID := strings.TrimSpace(summary.ShortID); shortID != "" {
		return shortID
	}
	return strings.TrimSpace(summary.ID)
}

func placementDisplayKey(placement serverapi.WorkflowPlacement, fallbackNodeID string) string {
	if nodeKey := strings.TrimSpace(placement.NodeKey); nodeKey != "" {
		return nodeKey
	}
	if nodeID := strings.TrimSpace(placement.NodeID); nodeID != "" {
		return nodeID
	}
	return strings.TrimSpace(fallbackNodeID)
}

func transitionStartKey(transition serverapi.WorkflowTaskTransition, fallback string) string {
	if sourceKey := strings.TrimSpace(transition.SourceNodeKey); sourceKey != "" {
		return sourceKey
	}
	if sourceID := strings.TrimSpace(transition.SourceNodeID); sourceID != "" {
		return sourceID
	}
	if transitionID := strings.TrimSpace(transition.TransitionID); transitionID != "" {
		return transitionID
	}
	return strings.TrimSpace(fallback)
}

func transitionEndKey(transition serverapi.WorkflowTaskTransition, fallback string) string {
	if edge, ok := transitionSelectedEdge(transition); ok {
		if edgeKey := strings.TrimSpace(edge.EdgeKey); edgeKey != "" {
			return edgeKey
		}
		if targetKey := strings.TrimSpace(edge.TargetNodeKey); targetKey != "" {
			return targetKey
		}
		if targetID := strings.TrimSpace(edge.TargetNodeID); targetID != "" {
			return targetID
		}
	}
	if transitionID := strings.TrimSpace(transition.TransitionID); transitionID != "" {
		return transitionID
	}
	return strings.TrimSpace(fallback)
}

func transitionStartedRun(task serverapi.WorkflowTaskDetail, transition serverapi.WorkflowTaskTransition, runIDs []string) (string, string, bool) {
	for _, runID := range runIDs {
		run, ok := workflowTaskRunByID(task, runID)
		if !ok || strings.TrimSpace(run.SessionID) == "" {
			continue
		}
		placement, _ := workflowTaskPlacementByID(task, run.PlacementID)
		nodeKey := placementDisplayKey(placement, run.NodeID)
		if strings.TrimSpace(nodeKey) == "" {
			nodeKey = transitionEndNodeKey(transition)
		}
		return nodeKey, strings.TrimSpace(run.SessionID), true
	}
	return "", "", false
}

func transitionEndNodeKey(transition serverapi.WorkflowTaskTransition) string {
	if edge, ok := transitionSelectedEdge(transition); ok {
		if targetKey := strings.TrimSpace(edge.TargetNodeKey); targetKey != "" {
			return targetKey
		}
		return strings.TrimSpace(edge.TargetNodeID)
	}
	return ""
}

func transitionSelectedEdge(transition serverapi.WorkflowTaskTransition) (serverapi.WorkflowTransitionEdge, bool) {
	for _, edge := range transition.Edges {
		if strings.TrimSpace(edge.State) == "applied" {
			return edge, true
		}
	}
	if len(transition.Edges) > 0 {
		return transition.Edges[0], true
	}
	return serverapi.WorkflowTransitionEdge{}, false
}

func writeTaskDetail(stdout io.Writer, task serverapi.WorkflowTaskDetail) {
	fmt.Fprintf(stdout, "%s: %s\n", task.Summary.ShortID, task.Summary.Title)
	fmt.Fprintln(stdout, "Body:")
	fmt.Fprintln(stdout, "```md")
	fmt.Fprintln(stdout, task.Body)
	fmt.Fprintln(stdout, "```")
	fmt.Fprintf(stdout, "Status: %s\n", taskDetailStatus(task))
	fmt.Fprintf(stdout, "Project: %q (%s)\n", task.Project.DisplayName, task.Project.ProjectID)
	fmt.Fprintf(stdout, "Workflow: %q (%s)\n", task.Workflow.DisplayName, task.Workflow.WorkflowID)
	fmt.Fprintf(stdout, "Created at %s UTC\n", time.UnixMilli(task.Summary.CreatedAtUnixMs).UTC().Format(time.RFC3339))
	if len(task.Runs) > 0 {
		fmt.Fprintf(stdout, "Total agent runs: %d\n", len(task.Runs))
	}
	if strings.TrimSpace(task.SourceWorkspace.RootPath) != "" {
		fmt.Fprintf(stdout, "Main workspace: %s\n", task.SourceWorkspace.RootPath)
	}
	if task.ManagedWorktree != nil && strings.TrimSpace(task.ManagedWorktree.CanonicalRoot) != "" {
		fmt.Fprintf(stdout, "Worktree: %s\n", task.ManagedWorktree.CanonicalRoot)
	}
	if strings.TrimSpace(task.SourceURL) != "" {
		fmt.Fprintf(stdout, "Imported from: %s\n", task.SourceURL)
	}
	writeTaskDetailComments(stdout, task.Summary.ShortID, task.Comments)
}

func taskDetailStatus(task serverapi.WorkflowTaskDetail) string {
	if task.Summary.CanceledAt != 0 || task.Status.Kind == "canceled" {
		return "canceled"
	}
	if task.Summary.Done || task.Status.Kind == "done" {
		return "done"
	}
	switch task.Status.Kind {
	case "", "backlog", "open":
		return "open"
	default:
		return "running"
	}
}

func writeTaskDetailComments(stdout io.Writer, shortID string, comments []serverapi.WorkflowTaskComment) {
	if len(comments) == 0 {
		return
	}
	if len(comments) >= 10 {
		fmt.Fprintf(stdout, "Comments under this task: %d. `"+config.Command+" task comment list %s` to show them.\n", len(comments), shortID)
		return
	}
	sortedComments := sortedTaskCommentsByCreatedAt(comments)
	fmt.Fprintf(stdout, "Comments (%d):\n", len(sortedComments))
	writeTaskCommentBlocks(stdout, sortedComments)
}

func sortedTaskCommentsByCreatedAt(comments []serverapi.WorkflowTaskComment) []serverapi.WorkflowTaskComment {
	sortedComments := append([]serverapi.WorkflowTaskComment(nil), comments...)
	sort.SliceStable(sortedComments, func(i, j int) bool {
		if sortedComments[i].CreatedAtUnixMs == sortedComments[j].CreatedAtUnixMs {
			return sortedComments[i].ID > sortedComments[j].ID
		}
		return sortedComments[i].CreatedAtUnixMs > sortedComments[j].CreatedAtUnixMs
	})
	return sortedComments
}

func writeTaskCommentBlocks(stdout io.Writer, sortedComments []serverapi.WorkflowTaskComment) {
	for i, comment := range sortedComments {
		if i > 0 {
			fmt.Fprintln(stdout, "---")
		}
		fmt.Fprintf(stdout, "%s at %s UTC:\n%s\n", readableTaskCommentAuthor(comment), time.UnixMilli(comment.CreatedAtUnixMs).UTC().Format(time.RFC3339), comment.Body)
	}
}

func readableTaskCommentAuthor(comment serverapi.WorkflowTaskComment) string {
	author := strings.TrimSpace(comment.Author)
	authorID := strings.TrimSpace(comment.AuthorID)
	if strings.EqualFold(author, "user") {
		if authorID != "" {
			return authorID
		}
		return "User"
	}
	if authorID != "" {
		return authorID
	}
	if author == "" {
		return "Unknown"
	}
	return strings.ToUpper(author[:1]) + author[1:]
}
