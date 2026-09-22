package main

import (
	"context"
	"core/shared/apicontract"
	"core/shared/client"
	protoapi "core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"fmt"
	"io"
	"strconv"
	"strings"

	"core/shared/config"
	"core/shared/labelcontract"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type workflowProjectLabelCatalogSnapshot struct {
	ProjectID          string
	LabelsByID         map[string]*pb.ProjectLabel
	LabelsByFoldedName map[string]*pb.ProjectLabel
}

func writeWorkflowLabelJSON(stdout, stderr io.Writer, label *pb.ProjectLabel) int {
	return writeCommandJSON(stdout, stderr, struct {
		Label workflowLabelJSON `json:"label"`
	}{Label: workflowLabelJSON{ID: label.Id, Name: label.Name}})
}

type taskLabelAssignmentOperation uint8

const (
	taskLabelAssignmentOperationAdd taskLabelAssignmentOperation = iota
	taskLabelAssignmentOperationRemove
)

func taskLabelSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return dispatchCommandGroup(args, stdout, stderr, commandGroup{
		path:  "task label",
		usage: taskLabelUsage,
		routes: map[string]commandHandler{
			"add":    taskLabelAddSubcommand,
			"create": taskLabelCreateSubcommand,
			"delete": taskLabelDeleteSubcommand,
			"list":   taskLabelListSubcommand,
			"remove": taskLabelRemoveSubcommand,
			"rename": taskLabelRenameSubcommand,
		},
	})
}

func taskLabelAddSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return taskLabelAssignmentSubcommand(args, stdout, stderr, taskLabelAssignmentOperationAdd)
}

func taskLabelRemoveSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return taskLabelAssignmentSubcommand(args, stdout, stderr, taskLabelAssignmentOperationRemove)
}

func taskLabelAssignmentSubcommand(args []string, stdout io.Writer, stderr io.Writer, operation taskLabelAssignmentOperation) int {
	usage := taskLabelAddUsage
	commandName := "add"
	if operation == taskLabelAssignmentOperationRemove {
		usage = taskLabelRemoveUsage
		commandName = "remove"
	}
	fs := newCommandFlagSet(config.Command+" task label "+commandName, stderr, usage)
	projectRef := fs.String("project", ".", "project ID or attached workspace path used to resolve a short ID")
	var selectors repeatedStringFlag
	fs.Var(&selectors, "label", "label name or canonical UUIDv4; repeat for multiple labels")
	jsonOut := fs.Bool("json", false, "write the authoritative task label assignment as JSON")
	positionals, ok, exitCode := parseWorkflowPositionals(fs, args, 1, stderr, "task label "+commandName+" requires <short-id-or-task-id>")
	if !ok {
		return exitCode
	}
	if len(selectors) == 0 {
		fmt.Fprintln(stderr, "task label "+commandName+" requires at least one --label <name-or-uuid>")
		return 2
	}
	return runWorkflowCommandSession(stderr, func(cfg config.App, remote *client.Remote) int {
		task, err := resolveWorkflowTask(context.Background(), cfg, remote, remote, *projectRef, positionals[0])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if strings.TrimSpace(task.Summary.ProjectID) == "" {
			fmt.Fprintln(stderr, "resolved task is missing project_id")
			return 1
		}
		_, snapshot, err := loadWorkflowProjectLabelCatalog(context.Background(), remote, task.Summary.ProjectID)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		labelIDs, err := resolveWorkflowProjectLabelSelectors(snapshot, selectors)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		request := &taskpb.LabelsUpdateRequest{TaskId: task.Summary.ID}
		switch operation {
		case taskLabelAssignmentOperationAdd:
			request.AddLabelIds = labelIDs
		case taskLabelAssignmentOperationRemove:
			request.RemoveLabelIds = labelIDs
		default:
			fmt.Fprintln(stderr, "invalid task label assignment operation")
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
		defer cancel()
		response, err := remote.UpdateWorkflowTaskLabels(ctx, request)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := protoapi.Validate(response); err != nil {
			fmt.Fprintf(stderr, "invalid task label assignment response: %v\n", err)
			return 1
		}
		if response.Assignment.TaskId != task.Summary.ID {
			fmt.Fprintf(stderr, "task label assignment response task %q does not match resolved task %q\n", response.Assignment.TaskId, task.Summary.ID)
			return 1
		}
		if *jsonOut {
			return writeCommandJSON(stdout, stderr, struct {
				Assignment workflowLabelAssignmentJSON `json:"assignment"`
			}{Assignment: workflowLabelAssignmentJSON{TaskID: response.Assignment.TaskId, LabelIDs: append([]string{}, response.Assignment.LabelIds...)}})
		}
		fmt.Fprintf(stdout, "Updated labels for task %s.\n", taskDisplayID(task))
		return 0
	})
}

func taskLabelCreateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task label create", stderr, taskLabelCreateUsage)
	projectRef := fs.String("project", ".", "project ID or attached workspace path")
	jsonOut := fs.Bool("json", false, "write the created Project label as JSON")
	positionals, ok, exitCode := parseWorkflowPositionals(fs, args, 1, stderr, "task label create requires <name>")
	if !ok {
		return exitCode
	}
	return runWorkflowCommandSession(stderr, func(cfg config.App, remote *client.Remote) int {
		projectID, err := resolveWorkflowProjectID(context.Background(), cfg, remote, *projectRef)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
		defer cancel()
		response, err := remote.CreateWorkflowProjectLabel(ctx, &pb.ProjectLabelCreateRequest{
			ProjectId: projectID,
			Name:      positionals[0],
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if *jsonOut {
			return writeWorkflowLabelJSON(stdout, stderr, response.Label)
		}
		fmt.Fprintf(stdout, "Created label %q (%s).\n", response.Label.Name, response.Label.Id)
		return 0
	})
}

func taskLabelListSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task label list", stderr, taskLabelListUsage)
	projectRef := fs.String("project", ".", "project ID or attached workspace path")
	name := fs.String("name", "", "label name to match")
	jsonOut := fs.Bool("json", false, "write the Project label catalog as JSON")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "task label list does not accept positional arguments")
		return 2
	}
	nameProvided := flagExplicit(fs, "name")
	if nameProvided && strings.TrimSpace(*name) == "" {
		fmt.Fprintln(stderr, "task label list --name requires a non-blank value")
		return 2
	}
	return runWorkflowCommandSession(stderr, func(cfg config.App, remote *client.Remote) int {
		projectID, err := resolveWorkflowProjectID(context.Background(), cfg, remote, *projectRef)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		catalog, snapshot, err := loadWorkflowProjectLabelCatalog(context.Background(), remote, projectID)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if nameProvided {
			catalog.Catalog.Labels = []*pb.ProjectLabel{}
			if record, found := resolveWorkflowProjectLabelName(snapshot, *name); found {
				catalog.Catalog.Labels = append(catalog.Catalog.Labels, record)
			}
		}
		if *jsonOut {
			labels := make([]workflowLabelJSON, 0, len(catalog.Catalog.Labels))
			for _, label := range catalog.Catalog.Labels {
				labels = append(labels, workflowLabelJSON{ID: label.Id, Name: label.Name})
			}
			return writeCommandJSON(stdout, stderr, struct {
				Catalog workflowLabelCatalogJSON `json:"catalog"`
			}{Catalog: workflowLabelCatalogJSON{ProjectID: catalog.Catalog.ProjectId, Labels: labels}})
		}
		for _, record := range catalog.Catalog.Labels {
			fmt.Fprintf(stdout, "%q (%s)\n", record.Name, record.Id)
		}
		return 0
	})
}

func taskLabelRenameSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task label rename", stderr, taskLabelRenameUsage)
	projectRef := fs.String("project", ".", "project ID or attached workspace path")
	selector := fs.String("label", "", "label name or canonical UUIDv4")
	jsonOut := fs.Bool("json", false, "write the renamed Project label as JSON")
	positionals, ok, exitCode := parseWorkflowPositionals(fs, args, 1, stderr, "task label rename requires <new-name>")
	if !ok {
		return exitCode
	}
	if !flagExplicit(fs, "label") {
		fmt.Fprintln(stderr, "task label rename requires --label <name-or-uuid>")
		return 2
	}
	return runWorkflowCommandSession(stderr, func(cfg config.App, remote *client.Remote) int {
		projectID, err := resolveWorkflowProjectID(context.Background(), cfg, remote, *projectRef)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		_, snapshot, err := loadWorkflowProjectLabelCatalog(context.Background(), remote, projectID)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		resolvedIDs, err := resolveWorkflowProjectLabelSelectors(snapshot, []string{*selector})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		labelID := resolvedIDs[0]
		ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
		defer cancel()
		response, err := remote.RenameWorkflowProjectLabel(ctx, &pb.ProjectLabelRenameRequest{
			ProjectId: projectID,
			LabelId:   labelID,
			Name:      positionals[0],
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := protoapi.Validate(response.Label); err != nil {
			fmt.Fprintf(stderr, "invalid Project label rename response: %v\n", err)
			return 1
		}
		if response.Label.Id != labelID {
			fmt.Fprintf(stderr, "Project label rename response ID %q does not match selected label %q\n", response.Label.Id, labelID)
			return 1
		}
		if *jsonOut {
			return writeWorkflowLabelJSON(stdout, stderr, response.Label)
		}
		fmt.Fprintf(stdout, "Renamed label %q (%s).\n", response.Label.Name, response.Label.Id)
		return 0
	})
}

func taskLabelDeleteSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task label delete", stderr, taskLabelDeleteUsage)
	projectRef := fs.String("project", ".", "project ID or attached workspace path")
	selector := fs.String("label", "", "label name or canonical UUIDv4")
	jsonOut := fs.Bool("json", false, "write the deleted Project label ID as JSON")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "task label delete does not accept positional arguments")
		return 2
	}
	if !flagExplicit(fs, "label") {
		fmt.Fprintln(stderr, "task label delete requires --label <name-or-uuid>")
		return 2
	}
	return runWorkflowCommandSession(stderr, func(cfg config.App, remote *client.Remote) int {
		projectID, err := resolveWorkflowProjectID(context.Background(), cfg, remote, *projectRef)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		_, snapshot, err := loadWorkflowProjectLabelCatalog(context.Background(), remote, projectID)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		resolvedIDs, err := resolveWorkflowProjectLabelSelectors(snapshot, []string{*selector})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		labelID := resolvedIDs[0]
		selected := snapshot.LabelsByID[labelID]
		ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
		defer cancel()
		response, err := remote.DeleteWorkflowProjectLabel(ctx, &pb.ProjectLabelDeleteRequest{
			ProjectId: projectID,
			LabelId:   labelID,
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := protoapi.Validate(response); err != nil {
			fmt.Fprintf(stderr, "invalid Project label delete response: %v\n", err)
			return 1
		}
		if response.LabelId != labelID {
			fmt.Fprintf(stderr, "Project label delete response ID %q does not match selected label %q\n", response.LabelId, labelID)
			return 1
		}
		if *jsonOut {
			return writeCommandJSON(stdout, stderr, struct {
				LabelID string `json:"label_id"`
			}{LabelID: response.LabelId})
		}
		fmt.Fprintf(stdout, "Deleted label %q (%s).\n", selected.Name, response.LabelId)
		return 0
	})
}

func loadWorkflowProjectLabelCatalog(ctx context.Context, remote apicontract.WorkflowService, projectID string) (*pb.ProjectLabelCatalogSuccess, workflowProjectLabelCatalogSnapshot, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	response, err := remote.ListWorkflowProjectLabels(rpcCtx, &pb.ProjectLabelCatalogRequest{ProjectId: projectID})
	if err != nil {
		return &pb.ProjectLabelCatalogSuccess{}, workflowProjectLabelCatalogSnapshot{}, err
	}
	snapshot, err := workflowProjectLabelCatalogSnapshotFromResponse(response.Catalog, projectID)
	if err != nil {
		return &pb.ProjectLabelCatalogSuccess{}, workflowProjectLabelCatalogSnapshot{}, err
	}
	return response, snapshot, nil
}

func workflowProjectLabelCatalogSnapshotFromResponse(catalog *pb.ProjectLabelCatalog, projectID string) (workflowProjectLabelCatalogSnapshot, error) {
	if err := protoapi.Validate(catalog); err != nil {
		return workflowProjectLabelCatalogSnapshot{}, fmt.Errorf("invalid Project label catalog response: %w", err)
	}
	if catalog.ProjectId != projectID {
		return workflowProjectLabelCatalogSnapshot{}, fmt.Errorf("Project label catalog response project %q does not match requested project %q", catalog.ProjectId, projectID)
	}
	snapshot := workflowProjectLabelCatalogSnapshot{
		ProjectID:          projectID,
		LabelsByID:         make(map[string]*pb.ProjectLabel, len(catalog.Labels)),
		LabelsByFoldedName: make(map[string]*pb.ProjectLabel, len(catalog.Labels)),
	}
	for _, record := range catalog.Labels {
		if _, exists := snapshot.LabelsByID[record.Id]; exists {
			return workflowProjectLabelCatalogSnapshot{}, fmt.Errorf("Project label catalog response contains duplicate label ID %q", record.Id)
		}
		foldedName := labelcontract.Fold(record.Name)
		if existing, exists := snapshot.LabelsByFoldedName[foldedName]; exists {
			return workflowProjectLabelCatalogSnapshot{}, fmt.Errorf("Project label catalog response contains duplicate folded label name %q for IDs %q and %q", foldedName, existing.Id, record.Id)
		}
		snapshot.LabelsByID[record.Id] = record
		snapshot.LabelsByFoldedName[foldedName] = record
	}
	return snapshot, nil
}

func resolveWorkflowProjectLabelSelectors(snapshot workflowProjectLabelCatalogSnapshot, rawSelectors []string) ([]string, error) {
	resolvedGroups, err := resolveWorkflowProjectLabelSelectorGroups(snapshot, [][]string{rawSelectors})
	if err != nil {
		return nil, err
	}
	return resolvedGroups[0].IDs, nil
}

func resolveWorkflowProjectLabelFilter(
	snapshot workflowProjectLabelCatalogSnapshot,
	mode serverapi.WorkflowTaskNamedLabelFilterMode,
	includedSelectors []string,
	excludedSelectors []string,
) (serverapi.WorkflowTaskLabelFilter, error) {
	resolvedGroups, err := resolveWorkflowProjectLabelSelectorGroups(snapshot, [][]string{includedSelectors, excludedSelectors})
	if err != nil {
		return serverapi.WorkflowTaskLabelFilter{}, err
	}
	if conflictID := sharedWorkflowProjectLabelSelectorGroups(resolvedGroups[0], resolvedGroups[1]); conflictID != nil {
		return serverapi.WorkflowTaskLabelFilter{}, conflictingWorkflowProjectLabelSelectorsError{
			Included: resolvedGroups[0].SelectorsByID[*conflictID],
			Excluded: resolvedGroups[1].SelectorsByID[*conflictID],
		}
	}
	filter := serverapi.WorkflowTaskLabelFilter{
		Kind: serverapi.WorkflowTaskLabelFilterKindNamed,
		Named: &serverapi.WorkflowTaskNamedLabelFilter{
			Mode:             mode,
			LabelIDs:         resolvedGroups[0].IDs,
			ExcludedLabelIDs: resolvedGroups[1].IDs,
		},
	}
	if err := filter.Validate(); err != nil {
		return serverapi.WorkflowTaskLabelFilter{}, err
	}
	return filter, nil
}

type resolvedWorkflowProjectLabelSelectorGroup struct {
	IDs           []string
	SelectorsByID map[string]string
}

func resolveWorkflowProjectLabelSelectorGroups(
	snapshot workflowProjectLabelCatalogSnapshot,
	rawSelectorGroups [][]string,
) ([]resolvedWorkflowProjectLabelSelectorGroup, error) {
	resolvedGroups := make([]resolvedWorkflowProjectLabelSelectorGroup, len(rawSelectorGroups))
	unresolved := make([]string, 0)
	for groupIndex, rawSelectors := range rawSelectorGroups {
		resolvedIDs := make([]string, 0, len(rawSelectors))
		seenIDs := make(map[string]struct{}, len(rawSelectors))
		selectorsByID := make(map[string]string, len(rawSelectors))
		for _, raw := range rawSelectors {
			record, found := resolveWorkflowProjectLabelSelector(snapshot, raw)
			if !found {
				unresolved = append(unresolved, raw)
				continue
			}
			if _, exists := seenIDs[record.Id]; exists {
				continue
			}
			seenIDs[record.Id] = struct{}{}
			resolvedIDs = append(resolvedIDs, record.Id)
			selectorsByID[record.Id] = raw
		}
		resolvedGroups[groupIndex] = resolvedWorkflowProjectLabelSelectorGroup{
			IDs:           resolvedIDs,
			SelectorsByID: selectorsByID,
		}
	}
	if len(unresolved) > 0 {
		return nil, unresolvedWorkflowProjectLabelSelectorsError{Selectors: unresolved}
	}
	return resolvedGroups, nil
}

func sharedWorkflowProjectLabelSelectorGroups(
	included resolvedWorkflowProjectLabelSelectorGroup,
	excluded resolvedWorkflowProjectLabelSelectorGroup,
) *string {
	for _, labelID := range included.IDs {
		if _, exists := excluded.SelectorsByID[labelID]; exists {
			return &labelID
		}
	}
	return nil
}

type conflictingWorkflowProjectLabelSelectorsError struct {
	Included string
	Excluded string
}

func (err conflictingWorkflowProjectLabelSelectorsError) Error() string {
	return fmt.Sprintf(
		"--label %s conflicts with --not-label %s because both select the same Label",
		strconv.Quote(err.Included),
		strconv.Quote(err.Excluded),
	)
}

func workflowProjectLabelNames(snapshot workflowProjectLabelCatalogSnapshot, ids []string) ([]string, error) {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		record, found := snapshot.LabelsByID[id]
		if !found {
			return nil, fmt.Errorf("Project label catalog changed while rendering task labels; retry the command")
		}
		names = append(names, record.Name)
	}
	return names, nil
}

func resolveWorkflowProjectLabelSelector(snapshot workflowProjectLabelCatalogSnapshot, raw string) (*pb.ProjectLabel, bool) {
	if _, err := runtimeids.ParseCanonicalUUIDv4(raw, "label selector"); err == nil {
		record, found := snapshot.LabelsByID[raw]
		return record, found
	}
	return resolveWorkflowProjectLabelName(snapshot, raw)
}

func resolveWorkflowProjectLabelName(snapshot workflowProjectLabelCatalogSnapshot, raw string) (*pb.ProjectLabel, bool) {
	record, found := snapshot.LabelsByFoldedName[labelcontract.Fold(strings.TrimSpace(raw))]
	return record, found
}

type unresolvedWorkflowProjectLabelSelectorsError struct {
	Selectors []string
}

func (err unresolvedWorkflowProjectLabelSelectorsError) Error() string {
	quoted := make([]string, len(err.Selectors))
	for index, selector := range err.Selectors {
		quoted[index] = strconv.Quote(selector)
	}
	return "unresolved label selectors: " + strings.Join(quoted, ", ")
}
