---
title: Headless runs
description: Headless Kent runs, scriptable output modes, and how interactive Kent uses the same mechanism for subagents.
---

Kent supports a headless, non-interactive run mode via `kent run`.
When the interactive Kent session uses subagents, it does so by launching separate headless Kent runs.
This keeps the subagent path contextual and scriptable: subagent invocations are not new tools and consume no extra tokens in model context.

Run a single prompt:

```bash
kent run --agent fast "summarize the unstaged changes in this repo"
```

Continue an existing headless session:

```bash
kent run --continue <session-id> "<follow-up>"
```

Control an active shared run from another shell or agent:

```bash
kent run steer <session-id> "adjust the next step" # steer a running agent
kent run stop <session-id> # gracefully interrupt the run
kent run wait <session-id> # wait for the model's turn to end
kent run watch <session-id> # report the next question or terminal outcome
```

When a human invokes `kent run steer`, the running Session receives a user message. When another Kent Session invokes it, the running Session receives a developer-role agent steer that identifies the source Session and includes the command form for replying.

Headless `kent run` Sessions cannot create Questions. To inspect or answer a pending Question from an interactive or Workflow Session:

```bash
kent question --session <session-id>
kent question answer --session <session-id> --option 1 --commentary "Additional context"
kent questions --task KENT-335
kent questions answer --task KENT-335 --commentary "Freeform answer"
```

Workflow Tasks can be observed without polling:

```bash
kent task wait KENT-335
kent task watch KENT-335
```

List answered Questions from one Session, newest first:

```bash
kent questions list --session <session-id>
kent questions list --session <session-id> --max-handoffs 1 --json
```

`--max-handoffs` defaults to 25 and counts the current unfinished history window. Question history reads the Session log and can be slow, especially in JSON mode. A Task may retain hundreds of Sessions; use `kent task sessions <task>` to choose a Session before listing its Question history.

These commands are one-shot server-side waits. `wait` ignores Questions but reports the next interruption, error, or terminal completion; `watch` reports the next Question, interruption, error, or terminal completion. A reported Question includes a runnable `kent question answer` command.

Use exactly one of `--session` or `--task`. Task short IDs resolve in the current workspace's Project unless `--project` selects another Project. A Task with pending Questions in several Sessions is ambiguous; Kent answers nothing and lists each candidate Session name and ID.

:::tip
`kent run` needs a server connection to keep long-running shells and agents properly orchestrated. If you want to script kent runs, make sure the [Server](../server/) is running.
:::

## Subagent Roles

Roles select the model settings and context used by a headless Session.

For a new `kent run`, omitting `--agent` when no other role is selected, or explicitly passing `--agent default`, selects the always-available `default` role. The `[subagents.default]` table can override the base model, Thinking, tools, Skills, and role description; without overrides, the role inherits the base settings.

When resuming a Session, omitting `--agent` preserves the Session's recorded role. Opening that Session in TUI or Desktop preserves its selected headless role. New interactive TUI and Desktop Sessions use the base settings unless another role is selected, and interactive `--agent default` selects those base settings.

If you remove a role before the Session's first model request locks its settings, resuming the Session switches it to the interactive base settings and clears the role selection. An agent making that headless continuation must pass the `default` role's delegation checks. Once the Session's settings are locked, resuming retains its recorded role even if you remove the role from configuration.

`--agent <role>` selects another role from `[subagents.<role>]` in the local or global config file; `none` and `self` are not run-agent selectors. Humans can launch roles with `kent run` even when delegation metadata blocks model-originated calls. Built-in roles (`default` and `fast`) follow the same child-delegation rules as custom roles. Direct Workflow Node assignment uses the selected role's settings regardless of delegation flags.

To open an interactive session with a role, run:

```bash
kent --agent research
```

To apply a role while reopening a specific Session, combine it with `--session` or `--continue`. An unlocked Session may select another role, including the headless default with `--agent default`. A locked Session keeps its persisted model request shape and role.

See [Configuration](../config/#subagents) for role overrides and delegation metadata.

## Delegation Depth

Kent limits model-originated creation of new child agents. A root session is depth `0`; with the default maximum of `2`, a root can create a subagent at depth `1`, that subagent can create one at depth `2`, and creation of a child at depth `3` is rejected.

The same limit applies across role changes and to delegation initiated by workflow agents. Scheduler-created workflow sessions start at depth `0`. Derived sessions such as `/new`, `/review`, rollback forks, and workflow fan-out clones preserve their source's agent ancestry.

The policy applies only when creating a new model-originated child. Opening or continuing an existing session is unchanged. Kent reads the active effective configuration for each attempt, rejects an over-limit launch before creating its session, and tells the current agent to stop trying subagents and complete the task itself.

Configure the root-level TOML key, supported range, and disable-with-zero behavior in the [configuration reference](../config/#core-settings). There is no environment-variable or `kent run` flag override.

## Session Behavior

Headless runs are non-interactive. They do not stop to ask the human operator questions mid-run, issue tool preambles, or support the Supervisor. That makes them more suitable for background execution, automation, and saves tokens. You can talk to a headless agent if you select it in the `/resume` (session picker).

### Provider usage history

Successful provider operations retain one usage observation in Session history. Each observation includes the operation identity and originating Session identity, operation purpose and time, requested and served provider/model details, provider response identity when available, provider-reported usage and metadata, and hosted-tool evidence.

Usage observations retain provider data for external price lookup; Kent does not calculate monetary costs, fetch prices, or expose Session totals. Missing provider fields and usage from earlier history remain absent rather than representing zero consumption, and copied history preserves the original operation identity.

Only provider operations that return successfully through the model transport are observed. Failed, interrupted, or incomplete operations, provider omissions, and process termination can leave the retained history incomplete.

## Workspace Binding

Headless runs fail if the selected workspace is not already attached to a Kent project.
This is needed to enable functionality related to project management and allows remote execution, but sometimes comes as a limitation where you want to run subagents in different repos. To fix the error, you simply need to approve workspace (git repo, folder etc.) binding:

- `kent project` prints the project id for the bound workspace at `path` or `cwd`. Use to learn project IDs.
- `kent attach <path>` attaches another workspace at [path] to the project already bound to `cwd`.
- `kent attach --project <project-id> [path]` attaches using the ID.
- `kent detach --project <project-id> [path]` removes one workspace binding from that project. The path defaults to the current directory; use `--workspace <workspace-id>` when the saved path is inaccessible or missing.
- `kent project default --project <project-id> [path]` changes the project's default workspace. It accepts the same path or workspace-ID selector and applies immediately.
- `kent rebind <session-id> <new-path>` retargets a session to a target path's only attached project, prefers the source project when the path is shared with it, and requires `--project` when the path is shared by multiple other projects.
- `kent rebind --project <project-id> <session-id> <new-path>` selects a non-workflow session's project explicitly and attaches an unbound target workspace.

For a live session, Kent acknowledges the scheduled move and applies it between agent steps, preserving the running agent and queued input even across projects. Existing background commands continue in their original directories; new commands use the destination. Dormant session moves complete synchronously and reject running session-owned background commands.

Detach and default-workspace selection require an explicit project ID. Path selectors are converted to absolute server paths before the request. A shared path can be detached from one project without changing its binding in another project.

Use `--json` for automation. Successful detach returns `status: "ok"` with `project_id` and `workspace_id`; successful default selection returns the updated project at `result.project`. Operational failures return one `status: "error"` object with a stable error code. Detach blockers include bounded guidance; a default-workspace blocker directs you to choose another attached workspace with `kent project default`.

Detach error codes are `project_not_found`, `workspace_not_attached`, `workspace_detach_blocked`, `workspace_detach_conflict`, and `request_failed`. Default-workspace selection uses `project_not_found`, `workspace_not_attached`, and `request_failed`. JSON omits absent results, error identities, blocker counts, and retryability fields.

Detach blockers use these recovery actions:

- `default_workspace`: choose another attached workspace with `kent project default`, then retry.
- `active_sessions`: stop active runs or rebind their Sessions, then retry.
- `non_terminal_tasks`: move editable Backlog Tasks to another source workspace, or complete, manually move, or delete dependent Tasks, then retry.
- `executable_current_nodes`: stop execution and move, complete, or delete affected Tasks until no executable Current Node uses the workspace, then retry.
- `managed_owned_worktrees`: delete dependent worktrees or their quiescent owning Tasks, then retry.
- `missing_history_snapshot`: re-save the editable Task's source workspace; keep the binding if its history cannot be edited.

### Project deletion

Delete a Project by its canonical Project ID:

```bash
kent project delete <project-id> --confirm
```

Project deletion is non-interactive and accepts only the canonical Project ID. The command checks for unfinished Tasks before checking `--confirm`. An agent-shell invocation is human-only when the Project contains any non-terminal Task, including a Backlog Task; it reports the Project ID and does not request deletion. Task state may change before deletion is processed, and the server's deletion blockers remain authoritative.

Without `--confirm`, the command makes no deletion request. Use `--json` for automation; it emits one `status` envelope on `stdout`, with the canonical Project ID in `result.project_id` on success or `error.project_id` on operational failure. Blocked deletion preserves server blocker codes, messages, order, and positive counts. Blockers and other operational failures exit nonzero.

Stable deletion error codes are `confirmation_required`, `human_only_unfinished_work`, `project_not_found`, `project_delete_blocked`, and `request_failed`.

Project deletion never deletes or moves workspace files.

## Output Modes

By default, `kent run` writes each finalized assistant commentary or final response to `stdout` as it is committed. Use `--quiet` to suppress live output and print only the terminal result. For scripting, use JSON mode:

```bash
kent run --output-mode=json "summarize the repo" | jq
```

JSON mode emits exactly one final object on `stdout`.

```json
{
  "status": "ok",
  "result": "...",
  "session_id": "...",
  "session_name": "...",
  "continue_id": "...",
  "continue_command": "kent run --continue ... \"follow-up\"",
  "warnings": ["..."],
  "duration_ms": 1234
}
```

On failure, JSON mode emits `status: "error"` and an `error` object instead of `result`.

An over-limit child launch uses the stable `subagent_max_depth_exceeded` code and includes the attempted depth and active maximum:

```json
{
  "status": "error",
  "duration_ms": 0,
  "error": {
    "code": "subagent_max_depth_exceeded",
    "message": "subagent launch rejected at depth 3 (maximum 2): ...",
    "attempted_depth": 3,
    "max_depth": 2
  }
}
```

Because the child was never created, this response has no session ID or continuation command. Final-text mode prints the actionable policy message instead.

---

Supported run-specific flags:

| Flag              | Description                                                                                                  |
| ----------------- | ------------------------------------------------------------------------------------------------------------ |
| `--timeout`       | Optional run timeout such as `30s`, `5m`, or `1h`. Default is no timeout.                                    |
| `--output-mode`   | `final-text` or `json`. Default is `final-text`.                                                             |
| `--progress-mode` | `stderr` for live responses and notices, or `quiet` for final-result-only output. Default is `stderr`.       |
| `-q`, `--quiet`   | Shortcut for `--progress-mode=quiet`.                                                                        |
| `--continue`      | Continue a previous session by id.                                                                           |
| `--agent`         | Select a role; `default` uses the headless default. Omission preserves a resumed role and defaults new runs. |
| `--fast`          | Shortcut for the built-in `fast` subagent role.                                                              |
