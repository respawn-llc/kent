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
	"github.com/google/uuid"
)

const workflowCommandTimeout = 5 * time.Second

type workflowCommandRemote interface {
	client.WorkflowClient
	ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error)
	Close() error
}

var workflowCommandRemoteOpener = openWorkflowCommandRemote

func workflowSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := flag.NewFlagSet("builder workflow", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.Usage = func() { writeWorkflowUsage(fs) }
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
		fs := flag.NewFlagSet("builder workflow", flag.ContinueOnError)
		fs.SetOutput(stderr)
		writeWorkflowUsage(fs)
		return 2
	}
}

func workflowCreateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder workflow create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowUsage(fs) }
	description := fs.String("description", "", "workflow description")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	name := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if name == "" {
		fmt.Fprintln(stderr, "workflow create requires <name>")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_ = cfg
	defer func() { _ = remote.Close() }()
	ctx, cancel := workflowRPCContext(context.Background())
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
	fs := flag.NewFlagSet("builder workflow list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowUsage(fs) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "workflow list does not accept positional arguments")
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
	resp, err := remote.ListWorkflows(ctx, serverapi.WorkflowListRequest{})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, workflow := range resp.Workflows {
		fmt.Fprintf(stdout, "%s\t%s\t%d\n", workflow.ID, workflow.Name, workflow.GraphRevision)
	}
	return 0
}

func workflowNodeSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "add" {
		return workflowNodeAddSubcommand(args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("builder workflow node", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowUsage(fs) }
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
	fs := flag.NewFlagSet("builder workflow node add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowNodeAddUsage(fs) }
	key := fs.String("key", "", "node model key")
	kind := fs.String("kind", "", "node kind: start|agent|join|terminal")
	displayName := fs.String("display-name", "", "node display name")
	prompt := fs.String("prompt", "", "agent prompt template")
	agent := fs.String("agent", "", "subagent role for agent nodes")
	workflowRef, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
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
	_, remote, err := workflowOpen(context.Background(), ".")
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
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	resp, err := remote.AddWorkflowNode(ctx, serverapi.WorkflowNodeAddRequest{WorkflowID: workflowID, NodeID: nodeID, Key: *key, Kind: *kind, DisplayName: *displayName, SubagentRole: *agent, PromptTemplate: *prompt})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "node_id\t%s\nkey\t%s\ngraph_revision\t%d\n", nodeID, *key, resp.GraphRevision)
	return 0
}

func workflowEdgeSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "add" {
		return workflowEdgeAddSubcommand(args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("builder workflow edge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowUsage(fs) }
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
	fs := flag.NewFlagSet("builder workflow edge add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowEdgeAddUsage(fs) }
	fromKey := fs.String("from", "", "source node key")
	transitionID := fs.String("transition", "", "transition id")
	edgeKey := fs.String("edge-key", "", "edge key")
	toKey := fs.String("to", "", "target node key")
	contextMode := fs.String("context", "", "context mode: new_session|continue_session|compact_and_continue_session")
	requiresApproval := fs.Bool("requires-approval", false, "require approval before target runs")
	workflowRef, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
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
	_, remote, err := workflowOpen(context.Background(), ".")
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
	for _, group := range def.TransitionGroups {
		if group.SourceNodeID == source.ID && group.TransitionID == strings.TrimSpace(*transitionID) {
			groupID = group.ID
			break
		}
	}
	if groupID == "" {
		groupID = "group-" + uuid.NewString()
		ctx, cancel := workflowRPCContext(context.Background())
		resp, addErr := remote.AddWorkflowTransitionGroup(ctx, serverapi.WorkflowTransitionGroupAddRequest{WorkflowID: def.Workflow.ID, GroupID: groupID, SourceNodeID: source.ID, TransitionID: *transitionID, DisplayName: workflowDisplayNameFromKey(*transitionID)})
		cancel()
		if addErr != nil {
			fmt.Fprintln(stderr, addErr)
			return 1
		}
		_ = resp
	}
	edgeID := "edge-" + uuid.NewString()
	ctx, cancel := workflowRPCContext(context.Background())
	resp, err := remote.AddWorkflowEdge(ctx, serverapi.WorkflowEdgeAddRequest{WorkflowID: def.Workflow.ID, EdgeID: edgeID, TransitionGroupID: groupID, Key: *edgeKey, TargetNodeID: target.ID, ContextMode: *contextMode, RequiresApproval: *requiresApproval})
	cancel()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "edge_id\t%s\ngroup_id\t%s\ngraph_revision\t%d\n", edgeID, groupID, resp.GraphRevision)
	return 0
}

func workflowLinkSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder workflow link", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowLinkUsage(fs) }
	defaultLink := fs.Bool("default", false, "make workflow project default")
	positionals, flagArgs := takeLeadingPositionals(args, 2)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 2 {
		fmt.Fprintln(stderr, "workflow link requires <project> and <workflow>")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
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
	ctx, cancel := workflowRPCContext(context.Background())
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
	fs := flag.NewFlagSet("builder workflow unlink", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowUsage(fs) }
	positionals, flagArgs := takeLeadingPositionals(args, 2)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 2 {
		fmt.Fprintln(stderr, "workflow unlink requires <project> and <workflow>")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
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
	ctx, cancel := workflowRPCContext(context.Background())
	defer cancel()
	if err := remote.UnlinkWorkflowFromProject(ctx, serverapi.WorkflowUnlinkProjectRequest{LinkID: link.ID}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "unlinked_link_id\t%s\n", link.ID)
	return 0
}

func workflowDefaultSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder workflow default", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowUsage(fs) }
	positionals, flagArgs := takeLeadingPositionals(args, 2)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 2 {
		fmt.Fprintln(stderr, "workflow default requires <project> and <workflow>")
		return 2
	}
	cfg, remote, err := workflowOpen(context.Background(), ".")
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
	ctx, cancel := workflowRPCContext(context.Background())
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
	fs := flag.NewFlagSet("builder workflow validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowValidateUsage(fs) }
	mode := fs.String("mode", string(serverapi.WorkflowValidationModeExecution), "validation mode: draft|task_creation|execution")
	_ = fs.String("project", "", "reserved project id/path")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "workflow validate requires <workflow>")
		return 2
	}
	_, remote, err := workflowOpen(context.Background(), ".")
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
	ctx, cancel := workflowRPCContext(context.Background())
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
	fs := flag.NewFlagSet("builder workflow inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeWorkflowUsage(fs) }
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "workflow inspect requires <workflow>")
		return 2
	}
	_, remote, err := workflowOpen(context.Background(), ".")
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
	fmt.Fprintf(stdout, "workflow_id\t%s\nname\t%s\ngraph_revision\t%d\n", def.Workflow.ID, def.Workflow.Name, def.Workflow.GraphRevision)
	fmt.Fprintln(stdout, "nodes")
	for _, node := range def.Nodes {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", node.ID, node.Key, node.Kind, node.DisplayName, node.SubagentRole)
	}
	fmt.Fprintln(stdout, "transition_groups")
	for _, group := range def.TransitionGroups {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", group.ID, group.SourceNodeID, group.TransitionID, group.DisplayName)
	}
	fmt.Fprintln(stdout, "edges")
	for _, edge := range def.Edges {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%t\n", edge.ID, edge.TransitionGroupID, edge.Key, edge.TargetNodeID, edge.ContextMode, edge.RequiresApproval)
	}
	return 0
}

func openWorkflowCommandRemote(ctx context.Context, path string) (config.App, workflowCommandRemote, error) {
	return bindingCommandRemoteOpener(ctx, path)
}

func workflowOpen(ctx context.Context, path string) (config.App, workflowCommandRemote, error) {
	return workflowCommandRemoteOpener(ctx, path)
}

func workflowRPCContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, workflowCommandTimeout)
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
	ctx, cancel := workflowRPCContext(ctx)
	defer cancel()
	list, err := remote.ListWorkflows(ctx, serverapi.WorkflowListRequest{})
	if err != nil {
		return serverapi.WorkflowDefinition{}, err
	}
	matches := make([]serverapi.WorkflowRecord, 0, 1)
	for _, workflow := range list.Workflows {
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
	getCtx, getCancel := workflowRPCContext(context.Background())
	defer getCancel()
	resp, err := remote.GetWorkflow(getCtx, serverapi.WorkflowGetRequest{WorkflowID: matches[0].ID})
	if err != nil {
		return serverapi.WorkflowDefinition{}, err
	}
	return resp.Definition, nil
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
		rpcCtx, cancel := workflowRPCContext(ctx)
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
	rpcCtx, cancel := workflowRPCContext(ctx)
	defer cancel()
	resp, err := remote.ListProjectWorkflowLinks(rpcCtx, serverapi.WorkflowListProjectLinksRequest{ProjectID: projectID})
	if err != nil {
		return serverapi.ProjectWorkflowLink{}, err
	}
	for _, link := range resp.Links {
		if link.WorkflowID == workflowID && link.UnlinkedAtUnixMs == 0 {
			return link, nil
		}
	}
	return serverapi.ProjectWorkflowLink{}, fmt.Errorf("project %s has no active link to workflow %s", projectID, workflowID)
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

func sortedWorkflowTasks(board serverapi.WorkflowBoard) []serverapi.WorkflowTaskSummary {
	seen := map[string]serverapi.WorkflowTaskSummary{}
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
