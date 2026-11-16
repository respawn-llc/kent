package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"core/shared/config"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
)

type taskListCommandContext struct {
	ProjectRef             string
	ResolvedProjectID      string
	SelectedWorkflowID     *runtimeids.WorkflowID
	ColumnKeys             []string
	StatusKinds            []string
	AttentionKinds         []string
	Sort                   []string
	LabelSelectors         []string
	ExcludedLabelSelectors []string
	LabelMatch             *string
	Unlabeled              bool
	DependencyFilter       *bool
	Offset                 int
	Limit                  int
	JSON                   bool
}

type taskCreateCommandContext struct {
	ProjectRef         string
	ResolvedProjectID  string
	SelectedWorkflowID *runtimeids.WorkflowID
	Title              string
	Body               string
	BodyFile           string
	SourceURL          string
	SourceWorkspace    string
	LabelSelectors     []string
	JSON               bool
}

type taskImportCommandContext struct {
	Request   *taskimportpb.Request
	SourceURL string
}

type taskWorkflowRecoveryKind uint8

const (
	taskWorkflowRecoveryNoLinkedWorkflows taskWorkflowRecoveryKind = iota + 1
	taskWorkflowRecoveryWorkflowNotLinked
	taskWorkflowRecoveryWorkflowRequiredColumns
	taskWorkflowRecoveryAmbiguousWithoutDefault
)

type taskWorkflowRecoveryCommandKind string

const (
	taskWorkflowRecoveryCommandCreateWorkflow       taskWorkflowRecoveryCommandKind = "create_workflow"
	taskWorkflowRecoveryCommandLinkCreatedWorkflow  taskWorkflowRecoveryCommandKind = "link_created_workflow"
	taskWorkflowRecoveryCommandListWorkflows        taskWorkflowRecoveryCommandKind = "list_workflows"
	taskWorkflowRecoveryCommandLinkExistingWorkflow taskWorkflowRecoveryCommandKind = "link_existing_workflow"
	taskWorkflowRecoveryCommandListProjectWorkflows taskWorkflowRecoveryCommandKind = "list_project_workflows"
	taskWorkflowRecoveryCommandRetryTaskList        taskWorkflowRecoveryCommandKind = "retry_task_list"
	taskWorkflowRecoveryCommandRetryTaskCreate      taskWorkflowRecoveryCommandKind = "retry_task_create"
	taskWorkflowRecoveryCommandRetryTaskImport      taskWorkflowRecoveryCommandKind = "retry_task_import"
	taskWorkflowRecoveryCommandLinkSelectedWorkflow taskWorkflowRecoveryCommandKind = "link_selected_workflow"
	taskWorkflowRecoveryCommandSetDefaultWorkflow   taskWorkflowRecoveryCommandKind = "set_default_workflow"
)

type taskWorkflowRecoveryCommand struct {
	Kind taskWorkflowRecoveryCommandKind
	Args []string
}

type taskWorkflowRecovery struct {
	Kind               taskWorkflowRecoveryKind
	ProjectRef         string
	SelectedWorkflowID *runtimeids.WorkflowID
	Commands           []taskWorkflowRecoveryCommand
}

type taskWorkflowRecoveryFailure struct {
	kind       taskWorkflowRecoveryKind
	projectID  string
	workflowID *runtimeids.WorkflowID
}

type taskWorkflowRecoveryContext struct {
	projectRef         string
	resolvedProjectID  string
	selectedWorkflowID *runtimeids.WorkflowID
	list               *taskListCommandContext
	create             *taskCreateCommandContext
	importTask         *taskImportCommandContext
}

func taskListRecoveryForScopeError(scopeErr *taskpb.ListScopeErrorDetails, commandContext taskListCommandContext) (taskWorkflowRecovery, error) {
	kind, err := taskListRecoveryKind(scopeErr.Reason)
	if err != nil {
		return taskWorkflowRecovery{}, err
	}
	var workflowID *runtimeids.WorkflowID
	if scopeErr.WorkflowId != nil {
		id, err := runtimeids.WorkflowIDFromProto(*scopeErr.WorkflowId)
		if err != nil {
			return taskWorkflowRecovery{}, err
		}
		workflowID = &id
	}
	failure := taskWorkflowRecoveryFailure{
		kind:       kind,
		projectID:  scopeErr.ProjectId,
		workflowID: workflowID,
	}
	return taskWorkflowRecoveryForFailure(failure, taskWorkflowRecoveryContext{
		projectRef:         commandContext.ProjectRef,
		resolvedProjectID:  commandContext.ResolvedProjectID,
		selectedWorkflowID: commandContext.SelectedWorkflowID,
		list:               &commandContext,
	})
}

func taskCreateRecoveryForSelectionError(createErr *taskpb.CreateSelectionDetails, commandContext taskCreateCommandContext) (taskWorkflowRecovery, error) {
	failure, err := taskWorkflowSelectionFailure(createErr)
	if err != nil {
		return taskWorkflowRecovery{}, err
	}
	return taskWorkflowRecoveryForFailure(failure, taskWorkflowRecoveryContext{
		projectRef:         commandContext.ProjectRef,
		resolvedProjectID:  commandContext.ResolvedProjectID,
		selectedWorkflowID: commandContext.SelectedWorkflowID,
		create:             &commandContext,
	})
}

func taskImportRecoveryForSelectionError(selection *taskpb.CreateSelectionDetails, commandContext taskImportCommandContext) (taskWorkflowRecovery, error) {
	if commandContext.Request == nil || commandContext.SourceURL == "" {
		return taskWorkflowRecovery{}, errors.New("import recovery requires its request and resolved source URL")
	}
	failure, err := taskWorkflowSelectionFailure(selection)
	if err != nil {
		return taskWorkflowRecovery{}, err
	}
	return taskWorkflowRecoveryForFailure(failure, taskWorkflowRecoveryContext{
		projectRef: failure.projectID, resolvedProjectID: failure.projectID,
		selectedWorkflowID: failure.workflowID, importTask: &commandContext,
	})
}

func taskWorkflowSelectionFailure(createErr *taskpb.CreateSelectionDetails) (taskWorkflowRecoveryFailure, error) {
	kind, err := taskCreateRecoveryKind(createErr.Reason)
	if err != nil {
		return taskWorkflowRecoveryFailure{}, err
	}
	var workflowID *runtimeids.WorkflowID
	if createErr.WorkflowId != nil {
		value, err := runtimeids.WorkflowIDFromProto(*createErr.WorkflowId)
		if err != nil {
			return taskWorkflowRecoveryFailure{}, err
		}
		workflowID = &value
	}
	return taskWorkflowRecoveryFailure{kind: kind, projectID: createErr.ProjectId, workflowID: workflowID}, nil
}

func taskListRecoveryKind(reason taskpb.ListScopeErrorReason) (taskWorkflowRecoveryKind, error) {
	switch reason {
	case taskpb.ListScopeErrorReason_LIST_SCOPE_ERROR_REASON_NO_LINKED_WORKFLOWS:
		return taskWorkflowRecoveryNoLinkedWorkflows, nil
	case taskpb.ListScopeErrorReason_LIST_SCOPE_ERROR_REASON_WORKFLOW_NOT_LINKED:
		return taskWorkflowRecoveryWorkflowNotLinked, nil
	case taskpb.ListScopeErrorReason_LIST_SCOPE_ERROR_REASON_WORKFLOW_REQUIRED_FOR_COLUMNS:
		return taskWorkflowRecoveryWorkflowRequiredColumns, nil
	default:
		return 0, fmt.Errorf("unsupported task-list workflow recovery reason %q", reason)
	}
}

func taskCreateRecoveryKind(reason taskpb.CreateSelectionReason) (taskWorkflowRecoveryKind, error) {
	switch reason {
	case taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_NO_LINKED_WORKFLOWS:
		return taskWorkflowRecoveryNoLinkedWorkflows, nil
	case taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_WORKFLOW_NOT_LINKED:
		return taskWorkflowRecoveryWorkflowNotLinked, nil
	case taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_AMBIGUOUS_WITHOUT_DEFAULT:
		return taskWorkflowRecoveryAmbiguousWithoutDefault, nil
	default:
		return 0, fmt.Errorf("unsupported task-create workflow recovery reason %q", reason)
	}
}

func taskWorkflowRecoveryForFailure(failure taskWorkflowRecoveryFailure, commandContext taskWorkflowRecoveryContext) (taskWorkflowRecovery, error) {
	if strings.TrimSpace(commandContext.projectRef) == "" || strings.TrimSpace(commandContext.resolvedProjectID) == "" {
		return taskWorkflowRecovery{}, errors.New("task workflow recovery requires project context")
	}
	contextCount := 0
	for _, present := range []bool{commandContext.list != nil, commandContext.create != nil, commandContext.importTask != nil} {
		if present {
			contextCount++
		}
	}
	if contextCount != 1 {
		return taskWorkflowRecovery{}, errors.New("task workflow recovery requires exactly one command context")
	}
	if failure.projectID != commandContext.resolvedProjectID {
		return taskWorkflowRecovery{}, fmt.Errorf("task workflow recovery project %q does not match resolved project %q", failure.projectID, commandContext.resolvedProjectID)
	}
	var selectedWorkflowID *runtimeids.WorkflowID
	if commandContext.selectedWorkflowID != nil {
		value := *commandContext.selectedWorkflowID
		selectedWorkflowID = &value
	}
	recovery := taskWorkflowRecovery{
		Kind:               failure.kind,
		ProjectRef:         commandContext.projectRef,
		SelectedWorkflowID: selectedWorkflowID,
	}
	listProjectWorkflows := taskWorkflowListProjectCommand(commandContext.projectRef)
	switch failure.kind {
	case taskWorkflowRecoveryNoLinkedWorkflows:
		if selectedWorkflowID != nil {
			return taskWorkflowRecovery{}, errors.New("no-linked-workflows recovery cannot select a workflow")
		}
		recovery.Commands = append(
			taskWorkflowNoLinksPreparationCommands(commandContext.projectRef),
			commandContext.retryCommand(nil),
		)
	case taskWorkflowRecoveryWorkflowNotLinked:
		if selectedWorkflowID == nil {
			return taskWorkflowRecovery{}, errors.New("workflow-not-linked recovery requires a selected workflow")
		}
		if *failure.workflowID != *selectedWorkflowID {
			return taskWorkflowRecovery{}, fmt.Errorf("task workflow recovery workflow %q does not match selected workflow %q", failure.workflowID, selectedWorkflowID)
		}
		recovery.Commands = []taskWorkflowRecoveryCommand{
			listProjectWorkflows,
			commandContext.retryCommandForWorkflowPlaceholder(),
			{Kind: taskWorkflowRecoveryCommandLinkSelectedWorkflow, Args: []string{config.Command, "workflow", "link", commandContext.projectRef, failure.workflowID.String()}},
		}
	case taskWorkflowRecoveryWorkflowRequiredColumns:
		if commandContext.list == nil || selectedWorkflowID != nil {
			return taskWorkflowRecovery{}, errors.New("workflow-required-for-columns recovery requires project-wide task list context")
		}
		recovery.Commands = []taskWorkflowRecoveryCommand{
			listProjectWorkflows,
			commandContext.retryCommandForWorkflowPlaceholder(),
		}
	case taskWorkflowRecoveryAmbiguousWithoutDefault:
		if (commandContext.create == nil && commandContext.importTask == nil) || selectedWorkflowID != nil {
			return taskWorkflowRecovery{}, errors.New("ambiguous recovery requires task create context without a selected workflow")
		}
		recovery.Commands = []taskWorkflowRecoveryCommand{
			listProjectWorkflows,
			commandContext.retryCommandForWorkflowPlaceholder(),
			{Kind: taskWorkflowRecoveryCommandSetDefaultWorkflow, Args: []string{config.Command, "workflow", "default", commandContext.projectRef, "<uuid>"}},
		}
	default:
		return taskWorkflowRecovery{}, fmt.Errorf("unsupported task workflow recovery kind %d", failure.kind)
	}
	return recovery, nil
}

type taskWorkflowRetrySelector interface {
	appendCLIArgs([]string) []string
}

type taskWorkflowRetryWorkflowID struct {
	value runtimeids.WorkflowID
}

func (s taskWorkflowRetryWorkflowID) appendCLIArgs(args []string) []string {
	return append(args, "--workflow", s.value.String())
}

type taskWorkflowRetryWorkflowPlaceholder struct{}

func (taskWorkflowRetryWorkflowPlaceholder) appendCLIArgs(args []string) []string {
	return append(args, "--workflow", "<uuid>")
}

func taskWorkflowRetrySelectorForWorkflowID(workflowID *runtimeids.WorkflowID) taskWorkflowRetrySelector {
	if workflowID == nil {
		return nil
	}
	return taskWorkflowRetryWorkflowID{value: *workflowID}
}

func (c taskWorkflowRecoveryContext) retryCommandForWorkflowPlaceholder() taskWorkflowRecoveryCommand {
	return c.retryCommandForWorkflowSelector(taskWorkflowRetryWorkflowPlaceholder{})
}

func (c taskWorkflowRecoveryContext) retryCommand(workflowID *runtimeids.WorkflowID) taskWorkflowRecoveryCommand {
	return c.retryCommandForWorkflowSelector(taskWorkflowRetrySelectorForWorkflowID(workflowID))
}

func (c taskWorkflowRecoveryContext) retryCommandForWorkflowSelector(selector taskWorkflowRetrySelector) taskWorkflowRecoveryCommand {
	if c.list != nil {
		return taskWorkflowRecoveryCommand{
			Kind: taskWorkflowRecoveryCommandRetryTaskList,
			Args: taskListRetryCommandArgsForSelector(*c.list, selector),
		}
	}
	if c.importTask != nil {
		args := []string{config.Command, "task", "import", c.importTask.SourceURL, "--project", c.projectRef}
		if selector != nil {
			args = selector.appendCLIArgs(args)
		}
		request := c.importTask.Request
		if request.SourceWorkspace != nil {
			args = append(args, "--source-workspace", *request.SourceWorkspace)
		}
		if request.CreateLabels {
			args = append(args, "--labels")
		}
		if request.Mode == taskimportpb.ImportMode_IMPORT_MODE_FORCE_CREATE {
			args = append(args, "--force")
		}
		return taskWorkflowRecoveryCommand{Kind: taskWorkflowRecoveryCommandRetryTaskImport, Args: args}
	}
	return taskWorkflowRecoveryCommand{
		Kind: taskWorkflowRecoveryCommandRetryTaskCreate,
		Args: taskCreateRetryCommandArgsForSelector(*c.create, selector),
	}
}

func writeTaskWorkflowSelectionRecovery(stderr io.Writer, recovery taskWorkflowRecovery) {
	switch recovery.Kind {
	case taskWorkflowRecoveryNoLinkedWorkflows:
		fmt.Fprintln(stderr, "This project doesn't have any linked workflows yet. First, create a workflow or link an existing one, then retry.")
	case taskWorkflowRecoveryWorkflowNotLinked:
		fmt.Fprintln(stderr, "The selected workflow isn't linked to this project.")
	case taskWorkflowRecoveryAmbiguousWithoutDefault:
		fmt.Fprintf(
			stderr,
			"Tasks need both a project and a workflow binding, but your input leaves workflow choice ambiguous. Run `%s`, then retry with `--workflow <uuid>` or set a default with `%s`.\n",
			commandString(recovery.Commands[0].Args),
			commandString(recovery.Commands[2].Args),
		)
	}
	for _, command := range recovery.Commands {
		fmt.Fprintf(stderr, "  %s\n", commandString(command.Args))
	}
}

func taskWorkflowNoLinksPreparationCommands(projectRef string) []taskWorkflowRecoveryCommand {
	return []taskWorkflowRecoveryCommand{
		{Kind: taskWorkflowRecoveryCommandCreateWorkflow, Args: []string{config.Command, "workflow", "create", "<name>"}},
		{Kind: taskWorkflowRecoveryCommandLinkCreatedWorkflow, Args: []string{config.Command, "workflow", "link", projectRef, "<created-uuid>"}},
		{Kind: taskWorkflowRecoveryCommandListWorkflows, Args: []string{config.Command, "workflow", "list"}},
		{Kind: taskWorkflowRecoveryCommandLinkExistingWorkflow, Args: []string{config.Command, "workflow", "link", projectRef, "<uuid>"}},
	}
}

func taskWorkflowListProjectCommand(projectRef string) taskWorkflowRecoveryCommand {
	return taskWorkflowRecoveryCommand{
		Kind: taskWorkflowRecoveryCommandListProjectWorkflows,
		Args: []string{config.Command, "workflow", "list", "--project", projectRef},
	}
}

func taskCreateRetryCommandArgs(commandContext taskCreateCommandContext, workflowID *runtimeids.WorkflowID) []string {
	return taskCreateRetryCommandArgsForSelector(commandContext, taskWorkflowRetrySelectorForWorkflowID(workflowID))
}

func taskCreateRetryCommandArgsForSelector(commandContext taskCreateCommandContext, selector taskWorkflowRetrySelector) []string {
	args := []string{config.Command, "task", "create", "--project", commandContext.ProjectRef}
	if selector != nil {
		args = selector.appendCLIArgs(args)
	}
	args = append(args, "--title", commandContext.Title)
	if strings.TrimSpace(commandContext.BodyFile) != "" {
		args = append(args, "--body-file", commandContext.BodyFile)
	} else {
		args = append(args, "--body", commandContext.Body)
	}
	if strings.TrimSpace(commandContext.SourceURL) != "" {
		args = append(args, "--source-url", commandContext.SourceURL)
	}
	if strings.TrimSpace(commandContext.SourceWorkspace) != "" {
		args = append(args, "--source-workspace", commandContext.SourceWorkspace)
	}
	for _, selector := range commandContext.LabelSelectors {
		args = append(args, "--label", selector)
	}
	if commandContext.JSON {
		args = append(args, "--json")
	}
	return args
}

func taskListRetryCommandArgs(commandContext taskListCommandContext, workflowID *runtimeids.WorkflowID) []string {
	return taskListRetryCommandArgsForSelector(commandContext, taskWorkflowRetrySelectorForWorkflowID(workflowID))
}

func taskListRetryCommandArgsForSelector(commandContext taskListCommandContext, selector taskWorkflowRetrySelector) []string {
	args := []string{config.Command, "task", "list", "--project", commandContext.ProjectRef}
	if selector != nil {
		args = selector.appendCLIArgs(args)
	}
	for _, status := range commandContext.StatusKinds {
		args = append(args, "--status", string(status))
	}
	for _, attention := range commandContext.AttentionKinds {
		args = append(args, "--attention", string(attention))
	}
	for _, columnKey := range commandContext.ColumnKeys {
		args = append(args, "--column", columnKey)
	}
	for _, sortSelector := range commandContext.Sort {
		args = append(args, "--sort", sortSelector)
	}
	for _, selector := range commandContext.LabelSelectors {
		args = append(args, "--label", selector)
	}
	for _, selector := range commandContext.ExcludedLabelSelectors {
		args = append(args, "--not-label", selector)
	}
	if commandContext.LabelMatch != nil {
		args = append(args, "--label-match", string(*commandContext.LabelMatch))
	}
	if commandContext.Unlabeled {
		args = append(args, "--unlabeled")
	}
	if commandContext.DependencyFilter != nil {
		if *commandContext.DependencyFilter {
			args = append(args, "--unblocked")
		} else {
			args = append(args, "--blocked")
		}
	}
	args = append(args, "--limit", fmt.Sprintf("%d", commandContext.Limit))
	if commandContext.JSON {
		args = append(args, "--json")
	}
	return args
}
