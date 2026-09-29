package main

import (
	"context"
	"core/shared/apicontract"
	"core/shared/client"
	"fmt"
	"io"
	"strings"
	"time"

	"core/shared/config"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type taskShowOutput struct {
	Summary              taskSummaryJSON                `json:"summary"`
	Body                 string                         `json:"body"`
	SourceURL            *string                        `json:"source_url,omitempty"`
	Project              *taskpb.BoardProject           `json:"project"`
	Workflow             taskWorkflowJSON               `json:"workflow"`
	SourceWorkspace      taskSourceWorkspaceJSON        `json:"source_workspace"`
	ExecutionTarget      *taskExecutionTargetJSON       `json:"execution_target,omitempty"`
	WorktreePath         *string                        `json:"worktree_path"`
	CurrentNodes         []*taskpb.AttentionCurrentNode `json:"current_nodes"`
	LiveSessions         []*taskpb.LiveSession          `json:"live_sessions"`
	CurrentScripts       []*taskpb.CurrentScript        `json:"current_scripts"`
	RetainedSessionCount int32                          `json:"retained_session_count"`
	Status               taskStatusJSON                 `json:"status"`
	Actions              taskActionsJSON                `json:"actions"`
	LabelIDs             []string                       `json:"label_ids"`
	AttentionCount       int32                          `json:"attention_count"`
	Dependencies         *taskShowDependencySummary     `json:"dependencies,omitempty"`
}

type taskShowDependencySummary struct {
	BlockerCount            int32 `json:"blocker_count"`
	UnsatisfiedBlockerCount int32 `json:"unsatisfied_blocker_count"`
	BlockedTaskCount        int32 `json:"blocked_task_count"`
}

func taskShowSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task show", stderr, taskShowUsage)
	projectRef := fs.String("project", ".", "project ID or attached workspace path used to resolve a short ID")
	jsonOut := fs.Bool("json", false, "write the complete task detail as JSON")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task show requires <short-id-or-task-id>")
		return 2
	}
	return runWorkflowCommandSession(stderr, func(cfg config.App, remote *client.Remote) int {
		requestedProjectID, task, err := getWorkflowTaskForShow(context.Background(), cfg, remote, remote, *projectRef, positionals[0])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if requestedProjectID != nil && task.Summary.ProjectId != *requestedProjectID {
			fmt.Fprintf(stderr, "Note: This task belongs to another project %s\n", task.Project.ProjectKey)
		}
		if *jsonOut {
			output, err := taskShowOutputFromDetail(task)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return writeCommandJSON(stdout, stderr, output)
		}
		labelNames, err := taskLabelNamesForHumanOutput(context.Background(), remote, task.Summary.ProjectId, task.LabelIds)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := writeTaskDetailWithLabelNames(stdout, task, labelNames); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	})
}

func taskShowOutputFromDetail(task *taskpb.TaskDetail) (taskShowOutput, error) {
	status, err := taskStatusOutput(task.Status)
	if err != nil {
		return taskShowOutput{}, err
	}
	source, err := taskSourceWorkspaceOutput(task.SourceWorkspace)
	if err != nil {
		return taskShowOutput{}, err
	}
	target, err := taskExecutionTargetOutput(task.ExecutionTarget)
	if err != nil {
		return taskShowOutput{}, err
	}
	output := taskShowOutput{
		Summary:              taskSummaryOutput(task.Summary),
		Body:                 task.Body,
		SourceURL:            task.SourceUrl,
		Project:              task.Project,
		Workflow:             taskWorkflowJSON{WorkflowID: task.Workflow.WorkflowId, DisplayName: task.Workflow.DisplayName, Version: task.Workflow.Version},
		SourceWorkspace:      source,
		ExecutionTarget:      target,
		WorktreePath:         task.WorktreePath,
		CurrentNodes:         append([]*taskpb.AttentionCurrentNode{}, task.CurrentNodes...),
		LiveSessions:         append([]*taskpb.LiveSession{}, task.LiveSessions...),
		CurrentScripts:       append([]*taskpb.CurrentScript{}, task.CurrentScripts...),
		RetainedSessionCount: task.RetainedSessionCount,
		Status:               status,
		Actions:              taskActionsJSON{CanStart: task.Actions.CanStart, CanInterrupt: task.Actions.CanInterrupt, CanResume: task.Actions.CanResume, CanDelete: task.Actions.CanDelete},
		LabelIDs:             normalizedLabelIDs(task.LabelIds),
		AttentionCount:       task.AttentionCount,
	}
	if task.Dependencies.BlockerCount != 0 ||
		task.Dependencies.UnsatisfiedBlockerCount != 0 ||
		task.Dependencies.DirectlyBlockedTaskCount != 0 {
		output.Dependencies = &taskShowDependencySummary{
			BlockerCount:            task.Dependencies.BlockerCount,
			UnsatisfiedBlockerCount: task.Dependencies.UnsatisfiedBlockerCount,
			BlockedTaskCount:        task.Dependencies.DirectlyBlockedTaskCount,
		}
	}
	return output, nil
}

func normalizedLabelIDs(ids []string) []string {
	normalized := make([]string, 0, len(ids))
	return append(normalized, ids...)
}

func getWorkflowTaskForShow(
	ctx context.Context,
	cfg config.App,
	projects apicontract.ProjectViewService,
	workflows apicontract.WorkflowService,
	projectRef string,
	ref string,
) (*string, *taskpb.TaskDetail, error) {
	selector, err := classifyWorkflowTaskSelector(ref)
	if err != nil {
		return nil, nil, err
	}
	var requestedProjectID *string
	if resolved, err := resolveWorkflowProjectID(ctx, cfg, projects, projectRef); err == nil {
		requestedProjectID = &resolved
	} else if selector.kind != workflowTaskSelectorTaskID {
		return nil, nil, err
	}
	if selector.kind == workflowTaskSelectorTaskID {
		detail, err := getWorkflowTaskByID(ctx, workflows, selector.value)
		return requestedProjectID, detail, err
	}
	if requestedProjectID != nil {
		detail, err := getWorkflowTaskByProjectShortID(ctx, workflows, *requestedProjectID, selector.value)
		if err == nil {
			return requestedProjectID, detail, nil
		}
		if !isWorkflowTaskNotFound(err) {
			return requestedProjectID, nil, err
		}
	}
	detail, err := getWorkflowTaskByShortID(ctx, workflows, selector.value)
	if err == nil {
		return requestedProjectID, detail, nil
	}
	if !isWorkflowTaskNotFound(err) {
		return requestedProjectID, nil, err
	}
	if requestedProjectID != nil {
		return requestedProjectID, nil, fmt.Errorf("task %q not found in project %s", selector.value, *requestedProjectID)
	}
	return requestedProjectID, nil, fmt.Errorf("task %q not found", selector.value)
}
func writeTaskDetail(stdout io.Writer, task *taskpb.TaskDetail) error {
	return writeTaskDetailWithLabelNames(stdout, task, nil)
}

func writeTaskDetailWithLabelNames(stdout io.Writer, task *taskpb.TaskDetail, labelNames []string) error {
	statusText, err := taskStatusText(task.Status)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: %s\n", task.Summary.ShortId, task.Summary.Title)
	fmt.Fprintln(stdout, "Body:")
	fmt.Fprintln(stdout, "```md")
	fmt.Fprintln(stdout, task.Body)
	fmt.Fprintln(stdout, "```")
	fmt.Fprintf(stdout, "Status: %s\n", statusText)
	fmt.Fprintf(stdout, "Project: %q (%s)\n", task.Project.DisplayName, task.Summary.ProjectId)
	fmt.Fprintf(stdout, "Workflow: %q (%s)\n", task.Workflow.DisplayName, task.Workflow.WorkflowId)
	fmt.Fprintf(stdout, "Created at %s UTC\n", task.Summary.CreatedAt.AsTime().UTC().Format(time.RFC3339))
	if strings.TrimSpace(task.SourceWorkspace.RootPath) != "" {
		fmt.Fprintf(stdout, "Main workspace: %s\n", task.SourceWorkspace.RootPath)
	}
	if task.ExecutionTarget != nil {
		if err := writeTaskExecutionTarget(stdout, task.ExecutionTarget); err != nil {
			return err
		}
	}
	if task.WorktreePath != nil {
		fmt.Fprintf(stdout, "Worktree: %s\n", *task.WorktreePath)
	}
	for _, session := range task.LiveSessions {
		fmt.Fprintf(stdout, "Current session: %s\n", session.SessionId)
	}
	fmt.Fprintf(stdout, "Retained sessions: %d\n", task.RetainedSessionCount)
	for _, script := range task.CurrentScripts {
		fmt.Fprintf(stdout, "Current script: %s (%s)\n", script.Path, script.CurrentNode.NodeId)
	}
	for _, node := range task.CurrentNodes {
		if node.EffectiveAssignee != nil {
			fmt.Fprintf(stdout, "Current node %s effective assignee: %s\n", node.NodeId, *node.EffectiveAssignee)
		}
		if node.EffectiveThinking != nil {
			fmt.Fprintf(stdout, "Current node %s effective thinking: %s\n", node.NodeId, *node.EffectiveThinking)
		}
	}
	if task.SourceUrl != nil {
		fmt.Fprintf(stdout, "Imported from: %s\n", *task.SourceUrl)
	}
	if len(labelNames) > 0 {
		fmt.Fprintf(stdout, "Labels:")
		for _, name := range labelNames {
			fmt.Fprintf(stdout, " %q", name)
		}
		fmt.Fprintln(stdout)
	}
	return writeTaskDependencyDirections(stdout, task.Dependencies.Directions)
}

func taskLabelNamesForHumanOutput(ctx context.Context, remote apicontract.WorkflowService, projectID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	_, snapshot, err := loadWorkflowProjectLabelCatalog(ctx, remote, projectID)
	if err != nil {
		return nil, err
	}
	return workflowProjectLabelNames(snapshot, ids)
}

func writeTaskExecutionTarget(stdout io.Writer, target *taskpb.ExecutionTarget) error {
	mode, err := protoapi.WorkflowExecutionTargetMode.Decode(target.Mode)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Execution target: %s\n", mode)
	if target.RequestedRef != nil {
		fmt.Fprintf(stdout, "Requested revision: %s\n", *target.RequestedRef)
	}
	if target.ResolvedRef != nil {
		fmt.Fprintf(stdout, "Resolved revision: %s\n", *target.ResolvedRef)
	}
	if target.CommitOid != nil {
		label := "Resolved commit"
		if target.Provenance == taskpb.ExecutionTargetProvenance_EXECUTION_TARGET_PROVENANCE_LEGACY_OBSERVED {
			label = "Observed commit (legacy)"
		}
		fmt.Fprintf(stdout, "%s: %s\n", label, shortCommitOID(*target.CommitOid))
	}
	return nil
}

func shortCommitOID(commitOID string) string {
	const displayLength = 12
	trimmed := strings.TrimSpace(commitOID)
	if len(trimmed) <= displayLength {
		return trimmed
	}
	return trimmed[:displayLength]
}

func taskStatusText(status *taskpb.TaskStatus) (string, error) {
	return protoapi.TaskStatusKind.Decode(status.Kind)
}
