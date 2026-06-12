package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"builder/prompts"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/sessionenv"
)

const (
	taskListDefaultPageSize        = 100
	taskCommentListDefaultPageSize = 100
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
		fs := newCommandFlagSet("builder task", stderr, taskUsage)
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
		fs := newCommandFlagSet("builder task", stderr, taskUsage)
		taskUsage.write(fs)
		return 2
	}
}

func taskCreateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task create", stderr, taskCommandUsage)
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
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
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
	fs := newCommandFlagSet("builder task start", stderr, taskCommandUsage)
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
	fmt.Fprintf(stdout, "run_id\t%s\nplacement_id\t%s\ntransition_id\t%s\n", resp.RunID, resp.PlacementID, resp.TransitionID)
	return 0
}

func taskListSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task list", stderr, taskCommandUsage)
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
	tasks, projectID, nextPageToken, err := workflowTaskPageForProject(context.Background(), cfg, remote, *projectRef, *pageSize, *pageToken)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	items := taskListItems(tasks)
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

func taskListItems(tasks []serverapi.WorkflowTaskSummary) []taskListItem {
	items := make([]taskListItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, taskListItem{
			ShortID:    task.ShortID,
			TaskID:     task.ID,
			WorkflowID: task.WorkflowID,
			Status:     taskListStatus(task),
			Title:      task.Title,
		})
	}
	return items
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
	fs := newCommandFlagSet("builder task show", stderr, taskCommandUsage)
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
	fs := newCommandFlagSet("builder task cancel", stderr, taskCommandUsage)
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
	fmt.Fprintf(stdout, "canceled_task_id\t%s\n", taskID)
	return 0
}

func taskResumeSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task resume", stderr, taskCommandUsage)
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
	fmt.Fprintf(stdout, "run_id\t%s\nplacement_id\t%s\nnode_id\t%s\ngeneration\t%d\n", resp.RunID, resp.PlacementID, resp.NodeID, resp.Generation)
	if strings.TrimSpace(resp.SessionID) != "" {
		fmt.Fprintf(stdout, "session_id\t%s\n", resp.SessionID)
	}
	return 0
}

func taskApproveSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task approve", stderr, taskCommandUsage)
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
	fs := newCommandFlagSet("builder task move", stderr, taskCommandUsage)
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
		fs := newCommandFlagSet("builder task comment", stderr, taskUsage)
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
		fs := newCommandFlagSet("builder task comment", stderr, taskUsage)
		taskUsage.write(fs)
		return 2
	}
}

func taskCommentAddSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder task comment add", stderr, taskCommandUsage)
	body := fs.String("body", "", "comment body")
	author := fs.String("author", "", "comment author")
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
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.AddWorkflowTaskComment(ctx, serverapi.WorkflowTaskCommentAddRequest{TaskID: taskID, Body: *body, Author: commentAuthor.Kind, AuthorID: commentAuthor.ID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "comment_id\t%s\ntask_id\t%s\n", resp.Comment.ID, resp.Comment.TaskID)
	return 0
}

type taskCommentAuthor struct {
	Kind string
	ID   string
}

func taskCommentAuthorForAdd(ctx context.Context, remote workflowCommandRemote, taskID string, explicitAuthor string, explicit bool) taskCommentAuthor {
	if explicit {
		return taskCommentAuthor{Kind: strings.TrimSpace(explicitAuthor)}
	}
	sessionID, ok := sessionenv.LookupBuilderSessionID(os.LookupEnv)
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
	fs := newCommandFlagSet("builder task comment list", stderr, taskCommandUsage)
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
	fs := newCommandFlagSet("builder task comment replace", stderr, taskCommandUsage)
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
	fs := newCommandFlagSet("builder task comment delete", stderr, taskUsage)
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
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowBoard(rpcCtx, serverapi.WorkflowBoardRequest{ProjectID: projectID})
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	return resp.Board, nil
}

func workflowTaskPageForProject(ctx context.Context, cfg config.App, remote workflowCommandRemote, projectRef string, pageSize int, pageToken string) ([]serverapi.WorkflowTaskSummary, string, string, error) {
	projectID, err := resolveWorkflowProjectID(ctx, cfg, remote, projectRef)
	if err != nil {
		return nil, "", "", err
	}
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.GetWorkflowBoard(rpcCtx, serverapi.WorkflowBoardRequest{
		ProjectID: projectID,
		PageSize:  pageSize,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, "", "", err
	}
	board := resp.Board
	tasks := workflowTasksForListPage(board, pageSize)
	return tasks, board.ProjectID, board.NextPageToken, nil
}

func workflowTasksForListPage(board serverapi.WorkflowBoard, pageSize int) []serverapi.WorkflowTaskSummary {
	cards := append([]serverapi.WorkflowBoardTaskCard(nil), board.Cards...)
	if strings.TrimSpace(board.NextPageToken) == "" && len(cards) < pageSize {
		remaining := pageSize - len(cards)
		donePreview := board.DonePreview
		if len(donePreview) > remaining {
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

func writeTaskDetail(stdout io.Writer, task serverapi.WorkflowTaskDetail) {
	fmt.Fprintf(stdout, "%s: %s\n", task.Summary.ShortID, task.Summary.Title)
	fmt.Fprintln(stdout, "Body:")
	fmt.Fprintln(stdout, "```md")
	fmt.Fprintln(stdout, task.Body)
	fmt.Fprintln(stdout, "```")
	fmt.Fprintf(stdout, "Status: %s\n", taskDetailStatus(task))
	fmt.Fprintf(stdout, "Project: %q (%s)\n", task.Project.DisplayName, task.Project.ProjectID)
	fmt.Fprintf(stdout, "Workflow: %q (%s)\n", task.Workflow.DisplayName, task.Workflow.WorkflowID)
	fmt.Fprintf(stdout, "Created at %s UTC\n", utcISO(task.Summary.CreatedAtUnixMs))
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
		fmt.Fprintf(stdout, "Comments under this task: %d. `builder task comment list %s` to show them.\n", len(comments), shortID)
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
		fmt.Fprintf(stdout, "%s at %s UTC:\n%s\n", readableTaskCommentAuthor(comment), utcISO(comment.CreatedAtUnixMs), comment.Body)
	}
}

func readableTaskCommentAuthor(comment serverapi.WorkflowTaskComment) string {
	author := strings.TrimSpace(comment.Author)
	authorID := strings.TrimSpace(comment.AuthorID)
	if strings.EqualFold(author, "user") {
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

func utcISO(unixMs int64) string {
	return time.UnixMilli(unixMs).UTC().Format(time.RFC3339)
}
