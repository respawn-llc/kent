package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/workflowkey"

	"github.com/google/uuid"
)

const (
	workflowCommandTimeout              = 5 * time.Second
	workflowCommandWorkflowListPageSize = serverapi.WorkflowListMaxPageSize
)

type workflowCommandRemote interface {
	client.WorkflowClient
	ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error)
	Close() error
}

var workflowCommandRemoteOpener func(context.Context, string) (config.App, workflowCommandRemote, error) = func(ctx context.Context, path string) (config.App, workflowCommandRemote, error) {
	cfg, remote, err := bindingCommandRemoteOpener(ctx, path)
	return cfg, remote, err
}

func workflowSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := newCommandFlagSet("builder workflow", stderr, workflowUsage)
		fs.Usage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "create":
		return workflowCreateSubcommand(args[1:], stdout, stderr)
	case "list":
		return workflowListSubcommand(args[1:], stdout, stderr)
	case "node":
		return workflowNodeSubcommand(args[1:], stdout, stderr)
	case "edge":
		return workflowEdgeSubcommand(args[1:], stdout, stderr)
	case "link":
		return workflowLinkSubcommand(args[1:], stdout, stderr)
	case "unlink":
		return workflowUnlinkSubcommand(args[1:], stdout, stderr)
	case "default":
		return workflowDefaultSubcommand(args[1:], stdout, stderr)
	case "validate":
		return workflowValidateSubcommand(args[1:], stdout, stderr)
	case "inspect":
		return workflowInspectSubcommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown workflow command: %s\n\n", args[0])
		fs := newCommandFlagSet("builder workflow", stderr, workflowUsage)
		workflowUsage.write(fs)
		return 2
	}
}

func workflowCreateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow create", stderr, workflowCommandUsage)
	description := fs.String("description", "", "workflow description")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	name := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if name == "" {
		fmt.Fprintln(stderr, "workflow create requires <name>")
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_ = cfg
	defer func() { _ = remote.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.CreateWorkflow(ctx, serverapi.WorkflowCreateRequest{Name: name, Description: *description})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "workflow_id\t%s\nname\t%s\n", resp.Workflow.ID, resp.Workflow.Name)
	return 0
}

func workflowListSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow list", stderr, workflowUsage)
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "workflow list does not accept positional arguments")
		return 2
	}
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	workflows, err := listAllWorkflows(context.Background(), remote)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, workflow := range workflows {
		fmt.Fprintf(stdout, "%s\t%s\t%d\n", workflow.ID, workflow.Name, workflow.Version)
	}
	return 0
}

func workflowNodeSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "add" {
		return workflowNodeAddSubcommand(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "update" {
		return workflowNodeUpdateSubcommand(args[1:], stdout, stderr)
	}
	fs := newCommandFlagSet("builder workflow node", stderr, workflowUsage)
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs.Usage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	fmt.Fprintf(stderr, "unknown workflow node command: %s\n\n", args[0])
	fs.Usage()
	return 2
}

func workflowNodeAddSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow node add", stderr, workflowCommandUsage)
	key := fs.String("key", "", "node model key")
	kind := fs.String("kind", "", "node kind: start|agent|join|terminal")
	displayName := fs.String("display-name", "", "node display name")
	prompt := fs.String("prompt", "", "agent prompt template")
	agent := fs.String("agent", "", "subagent role for agent nodes")
	workflowRef, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	workflowRef = append(workflowRef, fs.Args()...)
	if len(workflowRef) != 1 {
		fmt.Fprintln(stderr, "workflow node add requires <workflow>")
		return 2
	}
	if strings.TrimSpace(*key) == "" || strings.TrimSpace(*kind) == "" {
		fmt.Fprintln(stderr, "workflow node add requires --key and --kind")
		return 2
	}
	if *displayName == "" {
		*displayName = workflowDisplayNameFromKey(*key)
	}
	nodeID := "node-" + uuid.NewString()
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	workflowID, err := resolveWorkflowID(context.Background(), remote, workflowRef[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.AddWorkflowNode(ctx, serverapi.WorkflowNodeAddRequest{WorkflowID: workflowID, NodeID: nodeID, Key: *key, Kind: *kind, DisplayName: *displayName, SubagentRole: *agent, PromptTemplate: *prompt})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "node_id\t%s\nkey\t%s\nversion\t%d\n", nodeID, *key, resp.Version)
	return 0
}

func workflowNodeUpdateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow node update", stderr, workflowCommandUsage)
	key := fs.String("key", "", "node model key")
	kind := fs.String("kind", "", "node kind: start|agent|join|terminal")
	displayName := fs.String("display-name", "", "node display name")
	prompt := fs.String("prompt", "", "agent prompt template")
	agent := fs.String("agent", "", "subagent role for agent nodes")
	positionals, flagArgs := takeLeadingPositionals(args, 2)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 2 {
		fmt.Fprintln(stderr, "workflow node update requires <workflow> <node-key>")
		return 2
	}
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	def, err := resolveWorkflowDefinition(context.Background(), remote, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	node, err := workflowNodeByKey(def, positionals[1])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	updated := node
	if strings.TrimSpace(*key) != "" {
		updated.Key = strings.TrimSpace(*key)
	}
	if strings.TrimSpace(*kind) != "" {
		updated.Kind = strings.TrimSpace(*kind)
	}
	if strings.TrimSpace(*displayName) != "" {
		updated.DisplayName = strings.TrimSpace(*displayName)
	}
	if fs.Lookup("prompt") != nil && flagWasProvided(fs, "prompt") {
		updated.PromptTemplate = *prompt
	}
	if fs.Lookup("agent") != nil && flagWasProvided(fs, "agent") {
		updated.SubagentRole = *agent
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.UpdateWorkflowNode(ctx, serverapi.WorkflowNodeUpdateRequest{
		WorkflowID:         def.Workflow.ID,
		NodeID:             updated.ID,
		Key:                updated.Key,
		Kind:               updated.Kind,
		DisplayName:        updated.DisplayName,
		GroupKey:           updated.GroupKey,
		SubagentRole:       updated.SubagentRole,
		PromptTemplate:     updated.PromptTemplate,
		InputFields:        updated.InputFields,
		JoinInputProviders: updated.JoinInputProviders,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "node_id\t%s\nkey\t%s\nversion\t%d\n", updated.ID, updated.Key, resp.Version)
	return 0
}

func workflowEdgeSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "add" {
		return workflowEdgeAddSubcommand(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "update" {
		return workflowEdgeUpdateSubcommand(args[1:], stdout, stderr)
	}
	fs := newCommandFlagSet("builder workflow edge", stderr, workflowUsage)
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs.Usage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	fmt.Fprintf(stderr, "unknown workflow edge command: %s\n\n", args[0])
	fs.Usage()
	return 2
}

func workflowEdgeAddSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow edge add", stderr, workflowCommandUsage)
	fromKey := fs.String("from", "", "source node key")
	transitionID := fs.String("transition", "", "transition id")
	edgeKey := fs.String("edge-key", "", "edge key")
	toKey := fs.String("to", "", "target node key")
	contextMode := fs.String("context", "", "context mode: new_session|continue_session|compact_and_continue_session")
	contextSource := fs.String("context-source", "", "context source: immediate_source|node:<node-key>")
	requiresApproval := fs.Bool("requires-approval", false, "require approval before target runs")
	prompt := fs.String("prompt", "", "branch prompt template for agent targets")
	transitionDescription := fs.String("transition-description", "", "model-facing transition description explaining when to pick it")
	var params repeatedStringFlag
	fs.Var(&params, "param", "transition parameter as key=description (repeatable); declares a value the source agent must produce")
	workflowRef, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	workflowRef = append(workflowRef, fs.Args()...)
	if len(workflowRef) != 1 {
		fmt.Fprintln(stderr, "workflow edge add requires <workflow>")
		return 2
	}
	if strings.TrimSpace(*fromKey) == "" || strings.TrimSpace(*transitionID) == "" || strings.TrimSpace(*edgeKey) == "" || strings.TrimSpace(*toKey) == "" || strings.TrimSpace(*contextMode) == "" {
		fmt.Fprintln(stderr, "workflow edge add requires --from, --transition, --edge-key, --to, and --context")
		return 2
	}
	parsedContextSource, err := parseWorkflowContextSourceSelector(*contextSource)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	parsedParameters, err := parseWorkflowParameters(params)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	def, err := resolveWorkflowDefinition(context.Background(), remote, workflowRef[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	source, err := workflowNodeByKey(def, *fromKey)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	target, err := workflowNodeByKey(def, *toKey)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	groupID := ""
	var existingGroup *serverapi.WorkflowTransitionGroup
	for i := range def.TransitionGroups {
		group := def.TransitionGroups[i]
		if group.SourceNodeID == source.ID && group.TransitionID == strings.TrimSpace(*transitionID) {
			groupID = group.ID
			existingGroup = &group
			break
		}
	}
	trimmedDescription := strings.TrimSpace(*transitionDescription)
	if groupID == "" {
		groupID = "group-" + uuid.NewString()
		ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
		resp, addErr := remote.AddWorkflowTransitionGroup(ctx, serverapi.WorkflowTransitionGroupAddRequest{WorkflowID: def.Workflow.ID, GroupID: groupID, SourceNodeID: source.ID, TransitionID: *transitionID, DisplayName: workflowDisplayNameFromKey(*transitionID), Description: trimmedDescription})
		cancel()
		if addErr != nil {
			fmt.Fprintln(stderr, addErr)
			return 1
		}
		_ = resp
	}
	descriptionRollback := func() {}
	if groupID != "" && flagWasProvided(fs, "transition-description") && existingGroup != nil {
		previousGroup := *existingGroup
		ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
		resp, updateErr := remote.UpdateWorkflowTransitionGroup(ctx, serverapi.WorkflowTransitionGroupUpdateRequest{WorkflowID: def.Workflow.ID, GroupID: existingGroup.ID, SourceNodeID: existingGroup.SourceNodeID, TransitionID: existingGroup.TransitionID, DisplayName: existingGroup.DisplayName, Description: trimmedDescription})
		cancel()
		if updateErr != nil {
			fmt.Fprintln(stderr, updateErr)
			return 1
		}
		_ = resp
		// Restore the prior description if a later step fails so a non-zero exit
		// never leaves the transition group description partially mutated.
		descriptionRollback = func() {
			rbCtx, rbCancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
			_, rbErr := remote.UpdateWorkflowTransitionGroup(rbCtx, serverapi.WorkflowTransitionGroupUpdateRequest{WorkflowID: def.Workflow.ID, GroupID: previousGroup.ID, SourceNodeID: previousGroup.SourceNodeID, TransitionID: previousGroup.TransitionID, DisplayName: previousGroup.DisplayName, Description: previousGroup.Description})
			rbCancel()
			if rbErr != nil {
				fmt.Fprintf(stderr, "rollback transition group %s description failed: %v\n", previousGroup.ID, rbErr)
			}
		}
	}
	edgeID := "edge-" + uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	resp, err := remote.AddWorkflowEdge(ctx, serverapi.WorkflowEdgeAddRequest{WorkflowID: def.Workflow.ID, EdgeID: edgeID, TransitionGroupID: groupID, Key: *edgeKey, TargetNodeID: target.ID, ContextMode: *contextMode, ContextSource: parsedContextSource, RequiresApproval: *requiresApproval, PromptTemplate: *prompt, Parameters: parsedParameters})
	cancel()
	if err != nil {
		descriptionRollback()
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "edge_id\t%s\ngroup_id\t%s\nversion\t%d\n", edgeID, groupID, resp.Version)
	return 0
}

func workflowEdgeUpdateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow edge update", stderr, workflowCommandUsage)
	transitionID := fs.String("transition", "", "transition id for the edge's transition group")
	transitionDisplayName := fs.String("transition-display-name", "", "transition display name")
	transitionDescription := fs.String("transition-description", "", "model-facing transition description explaining when to pick it")
	edgeKey := fs.String("edge-key", "", "edge key")
	toKey := fs.String("to", "", "target node key")
	contextMode := fs.String("context", "", "context mode: new_session|continue_session|compact_and_continue_session")
	contextSource := fs.String("context-source", "", "context source: immediate_source|node:<node-key>")
	prompt := fs.String("prompt", "", "branch prompt template for agent targets")
	var params repeatedStringFlag
	fs.Var(&params, "param", "transition parameter as key=description (repeatable); replaces all parameters when provided")
	clearParams := fs.Bool("clear-params", false, "remove all transition parameters")
	positionals, flagArgs := takeLeadingPositionals(args, 2)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 2 {
		fmt.Fprintln(stderr, "workflow edge update requires <workflow> <edge-id>")
		return 2
	}
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	def, err := resolveWorkflowDefinition(context.Background(), remote, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	edge, err := workflowEdgeByID(def, positionals[1])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	group, err := workflowTransitionGroupByID(def, edge.TransitionGroupID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	updatedGroup := group
	if strings.TrimSpace(*transitionID) != "" {
		updatedGroup.TransitionID = strings.TrimSpace(*transitionID)
	}
	if strings.TrimSpace(*transitionDisplayName) != "" {
		updatedGroup.DisplayName = strings.TrimSpace(*transitionDisplayName)
	} else if strings.TrimSpace(*transitionID) != "" {
		updatedGroup.DisplayName = workflowDisplayNameFromKey(*transitionID)
	}
	if flagWasProvided(fs, "transition-description") {
		updatedGroup.Description = strings.TrimSpace(*transitionDescription)
	}
	updatedEdge := edge
	if strings.TrimSpace(*edgeKey) != "" {
		updatedEdge.Key = strings.TrimSpace(*edgeKey)
	}
	if strings.TrimSpace(*toKey) != "" {
		target, targetErr := workflowNodeByKey(def, *toKey)
		if targetErr != nil {
			fmt.Fprintln(stderr, targetErr)
			return 1
		}
		updatedEdge.TargetNodeID = target.ID
	}
	if strings.TrimSpace(*contextMode) != "" {
		updatedEdge.ContextMode = strings.TrimSpace(*contextMode)
	}
	if strings.TrimSpace(*contextSource) != "" {
		parsedContextSource, parseErr := parseWorkflowContextSourceSelector(*contextSource)
		if parseErr != nil {
			fmt.Fprintln(stderr, parseErr)
			return 2
		}
		updatedEdge.ContextSource = parsedContextSource
	}
	if flagWasProvided(fs, "prompt") {
		updatedEdge.PromptTemplate = *prompt
	}
	if *clearParams && flagWasProvided(fs, "param") {
		fmt.Fprintln(stderr, "use either --param or --clear-params, not both")
		return 2
	}
	if *clearParams {
		updatedEdge.Parameters = nil
	} else if flagWasProvided(fs, "param") {
		parsedParameters, parseErr := parseWorkflowParameters(params)
		if parseErr != nil {
			fmt.Fprintln(stderr, parseErr)
			return 2
		}
		updatedEdge.Parameters = parsedParameters
	}
	// Commit the transition group change only after every edge flag has parsed, so
	// a malformed --param or --context-source can never leave the group mutated
	// while the command exits non-zero before touching the edge.
	if updatedGroup != group {
		groupCtx, groupCancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
		groupResp, updateErr := remote.UpdateWorkflowTransitionGroup(groupCtx, serverapi.WorkflowTransitionGroupUpdateRequest{WorkflowID: def.Workflow.ID, GroupID: updatedGroup.ID, SourceNodeID: updatedGroup.SourceNodeID, TransitionID: updatedGroup.TransitionID, DisplayName: updatedGroup.DisplayName, Description: updatedGroup.Description})
		groupCancel()
		if updateErr != nil {
			fmt.Fprintln(stderr, updateErr)
			return 1
		}
		_ = groupResp
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.UpdateWorkflowEdge(ctx, serverapi.WorkflowEdgeUpdateRequest{WorkflowID: def.Workflow.ID, EdgeID: updatedEdge.ID, TransitionGroupID: updatedEdge.TransitionGroupID, Key: updatedEdge.Key, TargetNodeID: updatedEdge.TargetNodeID, ContextMode: updatedEdge.ContextMode, ContextSource: updatedEdge.ContextSource, RequiresApproval: updatedEdge.RequiresApproval, PromptTemplate: updatedEdge.PromptTemplate, Parameters: updatedEdge.Parameters})
	if err != nil {
		if updatedGroup != group {
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
			_, rollbackErr := remote.UpdateWorkflowTransitionGroup(rollbackCtx, serverapi.WorkflowTransitionGroupUpdateRequest{WorkflowID: def.Workflow.ID, GroupID: group.ID, SourceNodeID: group.SourceNodeID, TransitionID: group.TransitionID, DisplayName: group.DisplayName, Description: group.Description})
			rollbackCancel()
			if rollbackErr != nil {
				fmt.Fprintf(stderr, "%v; rollback transition group %s failed: %v\n", err, group.ID, rollbackErr)
				return 1
			}
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "edge_id\t%s\ngroup_id\t%s\nversion\t%d\n", updatedEdge.ID, updatedEdge.TransitionGroupID, resp.Version)
	return 0
}

func workflowLinkSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow link", stderr, workflowCommandUsage)
	defaultLink := fs.Bool("default", false, "make workflow project default")
	positionals, flagArgs := takeLeadingPositionals(args, 2)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 2 {
		fmt.Fprintln(stderr, "workflow link requires <project> and <workflow>")
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	projectID, err := resolveWorkflowProjectID(context.Background(), cfg, remote, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	workflowID, err := resolveWorkflowID(context.Background(), remote, positionals[1])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: projectID, WorkflowID: workflowID, Default: *defaultLink})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "link_id\t%s\nproject_id\t%s\nworkflow_id\t%s\ndefault\t%t\n", resp.Link.ID, resp.Link.ProjectID, resp.Link.WorkflowID, resp.Link.Default)
	return 0
}

func workflowUnlinkSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow unlink", stderr, workflowUsage)
	positionals, flagArgs := takeLeadingPositionals(args, 2)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 2 {
		fmt.Fprintln(stderr, "workflow unlink requires <project> and <workflow>")
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	link, err := resolveWorkflowProjectLink(context.Background(), cfg, remote, positionals[0], positionals[1])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.UnlinkWorkflowFromProject(ctx, serverapi.WorkflowUnlinkProjectRequest{LinkID: link.ID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !resp.Unlinked {
		writeWorkflowUnlinkBlockers(stderr, resp.Blockers)
		return 1
	}
	fmt.Fprintf(stdout, "unlinked_link_id\t%s\n", link.ID)
	return 0
}

func workflowDefaultSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow default", stderr, workflowUsage)
	positionals, flagArgs := takeLeadingPositionals(args, 2)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 2 {
		fmt.Fprintln(stderr, "workflow default requires <project> and <workflow>")
		return 2
	}
	cfg, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	projectID, err := resolveWorkflowProjectID(context.Background(), cfg, remote, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	workflowID, err := resolveWorkflowID(context.Background(), remote, positionals[1])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.SetDefaultProjectWorkflowLink(ctx, serverapi.WorkflowSetDefaultProjectLinkRequest{ProjectID: projectID, WorkflowID: workflowID})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "default_link_id\t%s\nproject_id\t%s\nworkflow_id\t%s\n", resp.Link.ID, resp.Link.ProjectID, resp.Link.WorkflowID)
	return 0
}

func workflowValidateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow validate", stderr, workflowCommandUsage)
	mode := fs.String("mode", string(serverapi.WorkflowValidationModeExecution), "validation mode: draft|task_creation|execution")
	_ = fs.String("project", "", "reserved project id/path")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "workflow validate requires <workflow>")
		return 2
	}
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	workflowID, err := resolveWorkflowID(context.Background(), remote, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
	defer cancel()
	resp, err := remote.ValidateWorkflow(ctx, serverapi.WorkflowValidateRequest{WorkflowID: workflowID, Mode: serverapi.WorkflowValidationMode(*mode)})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "valid\t%t\n", resp.Valid)
	for _, validationErr := range resp.Errors {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", validationErr.Code, validationErr.Message, validationErr.NodeID, validationErr.TransitionGroupID, validationErr.EdgeID)
	}
	if !resp.Valid {
		return 1
	}
	return 0
}

func workflowInspectSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet("builder workflow inspect", stderr, workflowUsage)
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "workflow inspect requires <workflow>")
		return 2
	}
	_, remote, err := workflowCommandRemoteOpener(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = remote.Close() }()
	def, err := resolveWorkflowDefinition(context.Background(), remote, positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "workflow_id\t%s\nname\t%s\nversion\t%d\n", def.Workflow.ID, def.Workflow.Name, def.Workflow.Version)
	fmt.Fprintln(stdout, "nodes")
	for _, node := range def.Nodes {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", node.ID, node.Key, node.Kind, node.DisplayName, node.SubagentRole)
		for _, field := range node.OutputFields {
			fmt.Fprintf(stdout, "output_field\t%s\t%s\t%s\n", node.Key, field.Name, field.Description)
		}
	}
	fmt.Fprintln(stdout, "transition_groups")
	for _, group := range def.TransitionGroups {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", group.ID, group.SourceNodeID, group.TransitionID, group.DisplayName)
	}
	fmt.Fprintln(stdout, "edges")
	for _, edge := range def.Edges {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%t\n", edge.ID, edge.TransitionGroupID, edge.Key, edge.TargetNodeID, edge.ContextMode, edge.RequiresApproval)
		source := canonicalAPIContextSource(edge.ContextSource)
		fmt.Fprintf(stdout, "context_source\t%s\t%s\t%s\n", edge.Key, source.Kind, source.NodeKey)
		for _, binding := range edge.InputBindings {
			fmt.Fprintf(stdout, "input_binding\t%s\t%s\t%s\t%s\n", edge.Key, binding.Name, binding.Source, binding.Field)
		}
		for _, requirement := range edge.OutputRequirements {
			fmt.Fprintf(stdout, "output_requirement\t%s\t%s\n", edge.Key, requirement.FieldName)
		}
	}
	return 0
}

func parseWorkflowContextSourceSelector(raw string) (serverapi.WorkflowContextSource, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "immediate_source" {
		return serverapi.WorkflowContextSource{Kind: "immediate_source"}, nil
	}
	prefix := "node:"
	if strings.HasPrefix(trimmed, prefix) {
		nodeKey := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if nodeKey == "" {
			return serverapi.WorkflowContextSource{}, errors.New("context source selector node key is required")
		}
		return serverapi.WorkflowContextSource{Kind: "selected_node", NodeKey: nodeKey}, nil
	}
	return serverapi.WorkflowContextSource{}, fmt.Errorf("context source selector must be immediate_source or node:<node-key>")
}

// repeatedStringFlag collects a flag that may be supplied multiple times.
type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string { return strings.Join(*f, ",") }

func (f *repeatedStringFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// parseWorkflowParameters converts repeated key=description entries into transition
// parameters. The source agent of the transition must produce a value for each declared
// key; transition prompts reference them as {{.Params.<key>}}, and downstream prompts
// reference guaranteed-prior transitions as {{.Params.<transition_id>.<key>}}.
func parseWorkflowParameters(raw []string) ([]serverapi.WorkflowParameter, error) {
	parameters := make([]serverapi.WorkflowParameter, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, entry := range raw {
		key, description, found := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		description = strings.TrimSpace(description)
		if !found || key == "" || description == "" {
			return nil, fmt.Errorf("parameter %q must be key=description with a non-empty key and description", entry)
		}
		if !workflowkey.Valid(key) {
			return nil, fmt.Errorf("parameter key %q is invalid; it must %s", key, workflowkey.Description)
		}
		if workflowkey.ReservedParameter(key) {
			return nil, fmt.Errorf("parameter key %q is reserved and cannot be declared", key)
		}
		if seen[key] {
			return nil, fmt.Errorf("parameter key %q is declared more than once", key)
		}
		seen[key] = true
		parameters = append(parameters, serverapi.WorkflowParameter{Key: key, Description: description})
	}
	return parameters, nil
}

func canonicalAPIContextSource(source serverapi.WorkflowContextSource) serverapi.WorkflowContextSource {
	if strings.TrimSpace(source.Kind) == "" {
		return serverapi.WorkflowContextSource{Kind: "immediate_source"}
	}
	return source
}

func resolveWorkflowID(ctx context.Context, remote workflowCommandRemote, ref string) (string, error) {
	def, err := resolveWorkflowDefinition(ctx, remote, ref)
	if err != nil {
		return "", err
	}
	return def.Workflow.ID, nil
}

func resolveWorkflowDefinition(ctx context.Context, remote workflowCommandRemote, ref string) (serverapi.WorkflowDefinition, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return serverapi.WorkflowDefinition{}, errors.New("workflow is required")
	}
	workflows, err := listAllWorkflows(ctx, remote)
	if err != nil {
		return serverapi.WorkflowDefinition{}, err
	}
	matches := make([]serverapi.WorkflowRecord, 0, 1)
	for _, workflow := range workflows {
		if workflow.ID == trimmed || workflow.Name == trimmed {
			matches = append(matches, workflow)
		}
	}
	if len(matches) == 0 {
		return serverapi.WorkflowDefinition{}, fmt.Errorf("workflow %q not found", trimmed)
	}
	if len(matches) > 1 {
		return serverapi.WorkflowDefinition{}, fmt.Errorf("workflow %q is ambiguous; use workflow id", trimmed)
	}
	getCtx, getCancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer getCancel()
	resp, err := remote.GetWorkflow(getCtx, serverapi.WorkflowGetRequest{WorkflowID: matches[0].ID})
	if err != nil {
		return serverapi.WorkflowDefinition{}, err
	}
	return resp.Definition, nil
}

func listAllWorkflows(ctx context.Context, remote workflowCommandRemote) ([]serverapi.WorkflowRecord, error) {
	workflows := make([]serverapi.WorkflowRecord, 0)
	pageToken := ""
	for {
		rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
		req := serverapi.WorkflowListRequest{PageSize: workflowCommandWorkflowListPageSize, PageToken: pageToken}
		resp, err := remote.ListWorkflows(rpcCtx, req)
		cancel()
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, resp.Workflows...)
		pageToken = strings.TrimSpace(resp.NextPageToken)
		if pageToken == "" {
			return workflows, nil
		}
	}
}

func workflowNodeByKey(def serverapi.WorkflowDefinition, key string) (serverapi.WorkflowNode, error) {
	trimmed := strings.TrimSpace(key)
	for _, node := range def.Nodes {
		if node.Key == trimmed {
			return node, nil
		}
	}
	return serverapi.WorkflowNode{}, fmt.Errorf("workflow node key %q not found", trimmed)
}

func workflowEdgeByID(def serverapi.WorkflowDefinition, edgeID string) (serverapi.WorkflowEdge, error) {
	trimmed := strings.TrimSpace(edgeID)
	for _, edge := range def.Edges {
		if edge.ID == trimmed {
			return edge, nil
		}
	}
	return serverapi.WorkflowEdge{}, fmt.Errorf("workflow edge id %q not found", trimmed)
}

func workflowTransitionGroupByID(def serverapi.WorkflowDefinition, groupID string) (serverapi.WorkflowTransitionGroup, error) {
	trimmed := strings.TrimSpace(groupID)
	for _, group := range def.TransitionGroups {
		if group.ID == trimmed {
			return group, nil
		}
	}
	return serverapi.WorkflowTransitionGroup{}, fmt.Errorf("workflow transition group id %q not found", trimmed)
}

func flagWasProvided(fs *flag.FlagSet, name string) bool {
	provided := false
	fs.Visit(func(flag *flag.Flag) {
		if flag.Name == name {
			provided = true
		}
	})
	return provided
}

func resolveWorkflowProjectID(ctx context.Context, cfg config.App, remote workflowCommandRemote, ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", errors.New("project is required")
	}
	if trimmed == "." || strings.Contains(trimmed, string(os.PathSeparator)) || pathExists(trimmed) {
		path := trimmed
		if trimmed == "." {
			path = cfg.WorkspaceRoot
		}
		abs, err := normalizeBindingCommandPath(path)
		if err != nil {
			return "", err
		}
		rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
		defer cancel()
		resp, err := remote.ResolveProjectPath(rpcCtx, serverapi.ProjectResolvePathRequest{Path: abs})
		if err != nil {
			return "", err
		}
		if resp.Binding == nil {
			return "", errWorkspaceNotRegistered
		}
		return strings.TrimSpace(resp.Binding.ProjectID), nil
	}
	return trimmed, nil
}

func resolveWorkflowProjectLink(ctx context.Context, cfg config.App, remote workflowCommandRemote, projectRef string, workflowRef string) (serverapi.ProjectWorkflowLink, error) {
	projectID, err := resolveWorkflowProjectID(ctx, cfg, remote, projectRef)
	if err != nil {
		return serverapi.ProjectWorkflowLink{}, err
	}
	workflowID, err := resolveWorkflowID(ctx, remote, workflowRef)
	if err != nil {
		return serverapi.ProjectWorkflowLink{}, err
	}
	rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
	defer cancel()
	resp, err := remote.ListProjectWorkflowLinks(rpcCtx, serverapi.WorkflowListProjectLinksRequest{ProjectID: projectID})
	if err != nil {
		return serverapi.ProjectWorkflowLink{}, err
	}
	for _, link := range resp.Links {
		if link.WorkflowID == workflowID {
			return link, nil
		}
	}
	return serverapi.ProjectWorkflowLink{}, fmt.Errorf("project %s has no active link to workflow %s", projectID, workflowID)
}

func writeWorkflowUnlinkBlockers(stderr io.Writer, blockers []serverapi.WorkflowUnlinkProjectBlocker) {
	if len(blockers) == 0 {
		fmt.Fprintln(stderr, "workflow link was not unlinked")
		return
	}
	for _, blocker := range blockers {
		if blocker.Count > 0 {
			fmt.Fprintf(stderr, "%s\t%s\t%d\n", blocker.Code, blocker.Message, blocker.Count)
		} else {
			fmt.Fprintf(stderr, "%s\t%s\n", blocker.Code, blocker.Message)
		}
		for _, task := range blocker.Tasks {
			fmt.Fprintf(stderr, "task\t%s\t%s\t%s\n", task.TaskID, task.ShortID, task.Title)
		}
	}
}

func workflowDisplayNameFromKey(key string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(key), func(r rune) bool { return r == '_' || r == '-' })
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	display := strings.TrimSpace(strings.Join(parts, " "))
	if display == "" {
		return strings.TrimSpace(key)
	}
	return display
}

func workflowTasksForProject(ctx context.Context, cfg config.App, remote workflowCommandRemote, projectRef string) ([]serverapi.WorkflowTaskSummary, string, error) {
	board, err := workflowBoardForProject(ctx, cfg, remote, projectRef)
	if err != nil {
		return nil, "", err
	}
	for pageToken := strings.TrimSpace(board.NextPageToken); pageToken != ""; {
		rpcCtx, cancel := context.WithTimeout(ctx, workflowCommandTimeout)
		resp, err := remote.GetWorkflowBoard(rpcCtx, serverapi.WorkflowBoardRequest{
			ProjectID:  board.ProjectID,
			WorkflowID: board.SelectedWorkflow.WorkflowID,
			PageSize:   200,
			PageToken:  pageToken,
		})
		cancel()
		if err != nil {
			return nil, "", err
		}
		board.Cards = append(board.Cards, resp.Board.Cards...)
		pageToken = strings.TrimSpace(resp.Board.NextPageToken)
	}
	return sortedWorkflowTasksFromCards(board, nil), board.ProjectID, nil
}

func sortedWorkflowTasksFromCards(board serverapi.WorkflowBoard, cards []serverapi.WorkflowBoardTaskCard) []serverapi.WorkflowTaskSummary {
	seen := map[string]serverapi.WorkflowTaskSummary{}
	for _, card := range cards {
		seen[card.TaskID] = workflowTaskSummaryFromCard(board.ProjectID, card)
	}
	for _, card := range board.Cards {
		seen[card.TaskID] = workflowTaskSummaryFromCard(board.ProjectID, card)
	}
	for _, card := range board.DonePreview {
		seen[card.TaskID] = workflowTaskSummaryFromCard(board.ProjectID, card)
	}
	for _, workflow := range board.Workflows {
		for _, task := range workflow.Tasks {
			seen[task.ID] = task
		}
	}
	tasks := make([]serverapi.WorkflowTaskSummary, 0, len(seen))
	for _, task := range seen {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].ShortID == tasks[j].ShortID {
			return tasks[i].ID < tasks[j].ID
		}
		return tasks[i].ShortID < tasks[j].ShortID
	})
	return tasks
}

func workflowTaskSummaryFromCard(projectID string, card serverapi.WorkflowBoardTaskCard) serverapi.WorkflowTaskSummary {
	return serverapi.WorkflowTaskSummary{
		ID:                card.TaskID,
		ProjectID:         projectID,
		WorkflowID:        card.WorkflowID,
		ShortID:           card.ShortID,
		Title:             card.Title,
		BodyPreview:       card.BodyPreview,
		SourceWorkspaceID: card.SourceWorkspace.WorkspaceID,
		UpdatedAtUnixMs:   card.UpdatedAtUnixMs,
		Done:              card.Status.Kind == "done",
		ActiveNodeIDs:     append([]string(nil), card.ActiveNodeIDs...),
	}
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(filepath.Clean(path))
	return err == nil
}

func takeLeadingPositionals(args []string, count int) ([]string, []string) {
	if count <= 0 {
		return nil, args
	}
	positionals := make([]string, 0, count)
	index := 0
	for index < len(args) && len(positionals) < count {
		arg := strings.TrimSpace(args[index])
		if arg == "" || strings.HasPrefix(arg, "-") {
			break
		}
		positionals = append(positionals, args[index])
		index++
	}
	return positionals, args[index:]
}
