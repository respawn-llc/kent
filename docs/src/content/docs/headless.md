---
title: Headless runs
description: Headless Kent runs, scriptable output modes, and how interactive Kent uses the same mechanism for subagents.
---

Kent supports a headless, non-interactive run mode via `kent run`, which is also how Kent runs subagents.
This keeps the subagent path contextual and scriptable: subagent invocations are not new tools and consume no extra tokens in model context.

Headless runs are non-interactive. They do not stop to ask the human operator questions mid-run, issue tool preambles, or support the Supervisor. That makes them more suitable for background execution, automation, and saves tokens. You can talk to a headless agent if you select it in the `/resume` (session picker).

:::tip
`kent run` needs a server connection to keep long-running shells and agents properly orchestrated. If you want to script kent runs, make sure the [Server](../server/) is running.
:::

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

When a human invokes `kent run steer`, the running Session receives a user message. When agents communicate, they also receive guidance on how to respond.

### Questions

Subagent `kent run` sessions cannot ask questions, but agents can answer each other's questions for workflow tasks and interactive sessions. In general, humans don't need to use the CLI, agents know how to ask and answer each other's questions.

To inspect or answer a pending Question from an interactive or Workflow Session:

```bash
kent question --session <session-id>
kent question answer --session <session-id> --option 1 --commentary "Additional context"
kent questions --task KENT-335
kent questions answer --task KENT-335 --commentary "Freeform answer"
kent questions list --session <session-id>
kent questions list --session <session-id> --max-handoffs 1 --json
```

More info in the CLI help.

## Subagent Roles

Roles select the model settings and context used by a headless Session.

- Resuming a session selects its last-used role.
- You can start a new interactive session with a specified role by running `kent --agent <role>`.
- Once the agent starts, kent snapshots and locks some settings from its role - model, provider, tools, thinking, and others, to prevent cache invalidation because you cannot change the agent role without invalidating the caches, so **any further adjustments to roles or parameters will be ignored at least until the next compaction.**
- To apply a role while reopening a specific Session, combine it with `--session` or `--continue`.
- To open an interactive session with a role, run: `kent --agent <role_key>`.

See [Configuration](../config/#subagents) for role overrides and delegation metadata.

## Delegation Depth

Kent limits model-originated creation of new children to prevent infinite subagent recursion. A root session is depth `0`. With the default maximum of `2`, a root can create a subagent at depth `1`, that subagent can create one at depth `2`, and creation of a child at depth `3` is rejected.

Configure the root-level TOML key, supported range, and disable-with-zero behavior in the [configuration reference](../config/#core-settings). There is no environment-variable or `kent run` flag override.

## Workspace Binding

Headless runs fail if the selected workspace is not already attached to a Kent project.
Attach the workspace to a project before launching a headless run:

- `kent project` prints the project id for the bound workspace at `path` or `cwd`. Use to learn project IDs.
- `kent attach <path>` attaches another workspace at [path] to the project already bound to `cwd`.
- `kent attach --project <project-id> [path]` attaches using the ID.
- `kent detach --project <project-id> [path]` removes one workspace binding from that project. The path defaults to the current directory. Use `--workspace <workspace-id>` when the saved path is inaccessible or missing.
- `kent project default --project <project-id> [path]` changes the project's default workspace.
- `kent rebind <session-id> <new-path>` retargets a session to a target path's only attached project.
- `kent rebind --project <project-id> <session-id> <new-path>` selects a non-workflow session's project explicitly and attaches an unbound target workspace.

- Workflow sessions cannot move across projects.
- Existing background commands continue in their original directories.

More info in the CLI help.

### Project deletion

Delete a Project by its canonical Project ID:

```bash
kent project delete <project-id>
```

You can only do that if there are no active sessions or tasks targeting it.

:::warning

Deleting a project **permanently deletes without a trace all tasks associated with it** and orphans the sessions.

:::

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

| Flag              | Description                                                                                                                 |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `--timeout`       | Optional run timeout such as `30s`, `5m`, or `1h`. Default is no timeout.                                                   |
| `--output-mode`   | `final-text` or `json`. Default is `final-text`.                                                                            |
| `--progress-mode` | `stderr` for live responses and notices, or `quiet` for final-result-only output. Default is `stderr`.                      |
| `-q`, `--quiet`   | Shortcut for `--progress-mode=quiet`.                                                                                       |
| `--continue`      | Continue a previous session by id.                                                                                          |
| `--agent`         | Select a role. `default` uses the headless default. Omission selects a resumed session's role or the default for a new run. |
| `--fast`          | Shortcut for the built-in `fast` subagent role.                                                                             |
