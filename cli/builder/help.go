package main

import (
	"flag"
	"fmt"
	"io"
)

func writeHelpSection(w io.Writer, title string, lines ...string) {
	if w == nil || title == "" {
		return
	}
	_, _ = fmt.Fprintln(w, title)
	for _, line := range lines {
		_, _ = fmt.Fprintln(w, line)
	}
	if len(lines) > 0 {
		_, _ = fmt.Fprintln(w)
	}
}

func writeRootUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder:",
		"  builder [flags]",
		"  builder run [flags] <prompt>",
		"  builder serve [flags]",
		"  builder service <status|install|uninstall|start|stop|restart>",
		"  builder session-id",
		"  builder goal <show|set|pause|resume|clear|complete>",
		"  builder workflow <create|list|node|edge|link|unlink|default|validate|inspect>",
		"  builder task <create|start|resume|approve|move|list|show|cancel|comment>",
		"  builder project [path]",
		"  builder project list",
		"  builder project create --path <server-path> --name <project-name>",
		"  builder attach [--project <project-id>] [path]",
		"  builder rebind <session-id> <new-path>",
	)
	writeHelpSection(out, "What This Does:",
		"  `builder` without a subcommand starts the interactive TUI.",
		"  `builder run` executes one headless prompt and exits.",
		"  `builder serve` starts the app server in daemon mode.",
		"  `builder service` manages the Builder server background service.",
		"  `builder session-id` prints the caller session id when invoked from a Builder shell command.",
		"  `builder goal` manages the active session goal through the live runtime.",
		"  `builder workflow` manages workflow definitions and project links.",
		"  `builder task` manages workflow tasks and comments.",
		"  `builder project` / `attach` / `rebind` inspect or repair workspace bindings.",
	)
	writeHelpSection(out, "Commands:",
		"  run      Execute a headless prompt against a workspace and print the final result.",
		"  serve    Start the Builder app server and keep serving until interrupted.",
		"  service  Install, inspect, or restart the Builder server background service.",
		"  session-id  Print the Builder caller session id from BUILDER_SESSION_ID.",
		"  goal     Show or update the live session goal.",
		"  workflow Manage workflow definitions and project workflow links.",
		"  task     Manage workflow tasks and task comments.",
		"  project  Inspect project bindings, list projects, or create a project.",
		"  attach   Attach another workspace path to an existing project.",
		"  rebind   Retarget one session to a different workspace root.",
	)
	writeHelpSection(out, "Examples:",
		"  builder",
		"  builder run --fast \"summarize the repo\"",
		"  builder service status",
		"  builder service install",
		"  builder session-id",
		"  builder goal show",
		"  builder workflow list",
		"  builder task list",
		"  builder project",
		"  builder attach ../other-checkout",
		"  builder rebind <session-id> ../moved-workspace",
		"  builder <command> --help",
	)
	writeHelpSection(out, "Flags:")
	fs.PrintDefaults()
}

func writeRunUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder run:",
		"  builder run [flags] <prompt>",
	)
	writeHelpSection(out, "What This Does:",
		"  Execute one headless prompt against a workspace without starting the TUI.",
		"  Builder creates or resumes a session, runs the prompt, prints the final answer, and exits.",
	)
	writeHelpSection(out, "Session Selection:",
		"  `--session <id>` resumes an existing session.",
		"  `--continue <id>` is the same concept, optimized for chaining follow-up runs.",
		"  `--session` and `--continue` may both be provided only when they match.",
	)
	writeHelpSection(out, "Subagents:",
		"  `--agent <role>` selects a named role from `[subagents.<role>]` in `~/.builder/config.toml`.",
		"  `--fast` is shorthand for the built-in `fast` role.",
	)
	writeHelpSection(out, "Examples:",
		"  builder run \"summarize the unstaged changes\"",
		"  builder run --continue <session-id> \"follow-up\"",
		"  builder run --fast --output-mode=json \"scan the repo and return JSON\"",
		"  builder run --model gpt-5.4-mini \"review this module\"",
	)
	writeHelpSection(out, "Flags:")
	fs.PrintDefaults()
}

func writeSessionIDUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder session-id:",
		"  builder session-id",
	)
	writeHelpSection(out, "What This Does:",
		"  Print BUILDER_SESSION_ID when invoked from a shell command started by Builder.",
		"  The command fails outside Builder-managed shell commands.",
	)
}

func writeGoalUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder goal:",
		"  builder goal show [--json] [--session <id>]",
		"  builder goal set [--session <id>] <objective>",
		"  builder goal pause [--session <id>]",
		"  builder goal resume [--session <id>]",
		"  builder goal clear [--session <id>]",
		"  builder goal complete [--session <id>] [--confirm]",
	)
	writeHelpSection(out, "What This Does:",
		"  Manage the live runtime goal for a session.",
		"  Inside Builder shell commands, BUILDER_SESSION_ID targets the caller session automatically.",
		"  Agent shell commands may set the first goal, but cannot overwrite or otherwise mutate an existing goal.",
		"  Outside Builder shell commands, pass --session <id>.",
	)
}

func writeGoalCommandUsage(fs *flag.FlagSet) {
	writeGoalUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeWorkflowUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder workflow:",
		"  builder workflow create [--description <text>] <name>",
		"  builder workflow list",
		"  builder workflow node add <workflow> --key <node-key> --kind start|agent|join|terminal [--display-name <name>] [--prompt <text>] [--agent <role>]",
		"  builder workflow edge add <workflow> --from <source-node-key> --transition <transition-id> --edge-key <edge-key> --to <target-node-key> --context <mode>",
		"  builder workflow link <project> <workflow> [--default]",
		"  builder workflow unlink <project> <workflow>",
		"  builder workflow default <project> <workflow>",
		"  builder workflow validate <workflow> [--mode draft|task_creation|execution]",
		"  builder workflow inspect <workflow>",
	)
	writeHelpSection(out, "What This Does:",
		"  Manage workflow definitions, graph nodes/edges, and project workflow links through the Builder server API.",
		"  Workflows describe durable agent pipelines: tasks start at a start node, agent nodes execute Builder runs, and terminal nodes mark completion.",
		"  Use `workflow create`, add nodes/edges, link a workflow to a project, then create/start tasks with `builder task`.",
		"  Workflow references may be exact workflow ids or exact workflow names.",
	)
}

func writeWorkflowCreateUsage(fs *flag.FlagSet) {
	writeWorkflowUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeWorkflowNodeAddUsage(fs *flag.FlagSet) {
	writeWorkflowUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeWorkflowEdgeAddUsage(fs *flag.FlagSet) {
	writeWorkflowUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeWorkflowLinkUsage(fs *flag.FlagSet) {
	writeWorkflowUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeWorkflowValidateUsage(fs *flag.FlagSet) {
	writeWorkflowUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder task:",
		"  builder task create --title <title> --body <body> [--workflow <workflow>] [--project <project>]",
		"  builder task start <short-id-or-task-id>",
		"  builder task resume <short-id-or-task-id>",
		"  builder task approve <transition-id>",
		"  builder task move <short-id-or-task-id> <target-node-id> [--output name=value]",
		"  builder task list [--project <project>]",
		"  builder task show <short-id-or-task-id>",
		"  builder task cancel <short-id-or-task-id> [--reason <text>]",
		"  builder task comment add <short-id-or-task-id> --body <text>",
		"  builder task comment list <short-id-or-task-id>",
		"  builder task comment replace <comment-id> --body <text>",
		"  builder task comment delete <comment-id>",
	)
	writeHelpSection(out, "What This Does:",
		"  Manage workflow tasks and comments through the Builder server API.",
		"  Short ids are resolved within the current project by default.",
	)
}

func writeTaskCreateUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskStartUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskResumeUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskApproveUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskMoveUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskListUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskShowUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskCancelUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskCommentUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
}

func writeTaskCommentAddUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskCommentListUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskCommentReplaceUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
	writeHelpSection(fs.Output(), "Flags:")
	fs.PrintDefaults()
}

func writeTaskCommentDeleteUsage(fs *flag.FlagSet) {
	writeTaskUsage(fs)
}

func writeProjectUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder project:",
		"  builder project [path]",
		"  builder project list",
		"  builder project create --path <server-path> --name <project-name>",
	)
	writeHelpSection(out, "What This Does:",
		"  Inspect or manage Builder project bindings.",
		"  `builder project [path]` prints the project id bound to `path` or the current directory.",
		"  `builder project list` lists projects known to the current server.",
		"  `builder project create` registers a new project for a server-visible workspace path.",
	)
	writeHelpSection(out, "Path Semantics:",
		"  For local loopback mode, paths are local filesystem paths.",
		"  For remote daemons, paths passed to `project create` must be visible on the server machine.",
	)
	writeHelpSection(out, "Examples:",
		"  builder project",
		"  builder project ../other-checkout",
		"  builder project list",
		"  builder project create --path /srv/repos/app --name app",
	)
}

func writeProjectListUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder project list:",
		"  builder project list",
	)
	writeHelpSection(out, "What This Does:",
		"  List projects known to the current Builder server.",
		"  Output columns are: project id, display name, and root path.",
	)
	writeHelpSection(out, "Examples:",
		"  builder project list",
	)
}

func writeProjectCreateUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder project create:",
		"  builder project create --path <server-path> --name <project-name>",
	)
	writeHelpSection(out, "What This Does:",
		"  Create a new Builder project and bind its first workspace root.",
		"  The path must exist and must be visible to the Builder server.",
	)
	writeHelpSection(out, "Examples:",
		"  builder project create --path /srv/repos/app --name app",
	)
	writeHelpSection(out, "Flags:")
	fs.PrintDefaults()
}

func writeAttachUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder attach:",
		"  builder attach [path]",
		"  builder attach --project <project-id> [path]",
	)
	writeHelpSection(out, "What This Does:",
		"  Attach another workspace path to an existing Builder project.",
		"  The command prints the project id after the attach succeeds.",
	)
	writeHelpSection(out, "How Project Selection Works:",
		"  Without `--project`, Builder reads the project bound to the current working directory and reuses it.",
		"  With `--project`, Builder skips current-directory lookup and uses the explicit project id instead.",
		"  If `[path]` is omitted, Builder attaches the current directory.",
	)
	writeHelpSection(out, "Path Semantics:",
		"  In loopback mode, `[path]` is a local filesystem path.",
		"  Against a remote daemon, `[path]` must be visible on the server machine.",
	)
	writeHelpSection(out, "Examples:",
		"  builder attach ../other-checkout",
		"  builder attach --project <project-id> /srv/repos/other-checkout",
	)
	writeHelpSection(out, "Flags:")
	fs.PrintDefaults()
}

func writeRebindUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder rebind:",
		"  builder rebind <session-id> <new-path>",
	)
	writeHelpSection(out, "What This Does:",
		"  Retarget one session to a different workspace root.",
		"  Use this when the original workspace moved or when the session should continue from another bound copy.",
	)
	writeHelpSection(out, "Requirements:",
		"  `<new-path>` must exist.",
		"  `<new-path>` must not already be bound to a different project.",
	)
	writeHelpSection(out, "Examples:",
		"  builder rebind <session-id> ../moved-workspace",
	)
}

func writeServeUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder serve:",
		"  builder serve [flags]",
	)
	writeHelpSection(out, "What This Does:",
		"  Start the Builder app server and keep serving until interrupted.",
		"  This is for daemon/server mode, not for running one prompt interactively.",
	)
	writeHelpSection(out, "Notes:",
		"  `builder serve` is workspace-agnostic at startup.",
		"  Session config is resolved from each session workspace: defaults, ~/.builder, env, <workspace>/.builder.",
	)
	writeHelpSection(out, "Examples:",
		"  builder serve",
		"  builder project create --path /srv/repos/app --name app",
	)
	writeHelpSection(out, "Flags:")
	fs.PrintDefaults()
}

func writeServiceUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder service:",
		"  builder service status [--json]",
		"  builder service install [--force] [--no-start]",
		"  builder service uninstall [--keep-running]",
		"  builder service start",
		"  builder service stop",
		"  builder service restart [--if-installed]",
	)
	writeHelpSection(out, "What This Does:",
		"  Manage the Builder server background service for one shared local `builder serve`.",
		"  The service starts at login and is supervised by the OS.",
	)
	writeHelpSection(out, "Backends:",
		"  macOS: launchd LaunchAgent.",
		"  Linux/WSL2: systemd --user unit.",
		"  Windows: Scheduled Task, with Startup folder fallback when needed.",
	)
	writeHelpSection(out, "macOS Notes:",
		"  `restart --if-installed` rewrites the LaunchAgent, unloads the loaded job, and bootstraps it again.",
		"  `start` bootstraps an unloaded LaunchAgent; launchd starts it through RunAtLoad.",
	)
	writeHelpSection(out, "Examples:",
		"  builder service status",
		"  builder service install",
		"  builder service restart",
	)
}

func writeServiceStatusUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder service status:",
		"  builder service status [--json]",
	)
	writeHelpSection(out, "Flags:")
	fs.PrintDefaults()
}

func writeServiceInstallUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder service install:",
		"  builder service install [--force] [--no-start]",
	)
	writeHelpSection(out, "Flags:")
	fs.PrintDefaults()
}

func writeServiceUninstallUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder service uninstall:",
		"  builder service uninstall [--keep-running]",
	)
	writeHelpSection(out, "Flags:")
	fs.PrintDefaults()
}

func writeServiceLifecycleUsage(fs *flag.FlagSet, action serviceAction) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder service "+string(action)+":",
		"  builder service "+string(action),
	)
}

func writeServiceRestartUsage(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	out := fs.Output()
	writeHelpSection(out, "Usage of builder service restart:",
		"  builder service restart [--if-installed]",
	)
	writeHelpSection(out, "Notes:",
		"  Refuses to run inside Builder shell commands to avoid halting active agent work.",
	)
	writeHelpSection(out, "Flags:")
	fs.PrintDefaults()
}
