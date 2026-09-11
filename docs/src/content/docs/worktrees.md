---
title: Worktrees
description: Create, enter, and delete Git worktrees from Kent.
---

Kent can create and manage worktrees for you. Agents will enter new worktrees if they need to, and workflows can automatically create worktrees for their tasks (see [workflows](../workflows)). If you want to manually manage worktrees, run `/wt` in the TUI, or ask the agent to use the CLI:

```bash
kent worktree status
kent worktree list
kent worktree create <branch-or-ref> [path]
kent worktree enter <selector>
kent worktree leave
kent worktree delete <selector>
```

Every command supports `--json`. Session-scoped commands automatically use the current Session inside a Kent shell or accept `--session <id>` explicitly.

## Select a Project or Workspace

`list`, `create`, and `delete` work without a Session. `--project <project-id>` selects that Project's default Workspace, independently of your shell directory or agent's Project. Add `--workspace <workspace-id>` to select another Workspace in that Project.

```bash
kent worktree list --project <project-id>
kent worktree create --project <project-id> --workspace <workspace-id> feature/search
kent worktree delete --project <project-id> <selector>
```

Without `--project`, Kent uses the issuing agent Session, otherwise `--session`, otherwise the current directory. `--workspace` alone selects within that inferred Project. Inside agent shells, the issuing Session remains the caller even if `--session` names another Session.

The selected Workspace governs reference resolution, setup, and deletion checks as well as the operation itself. Invalid or foreign selections fail without falling back to the caller's Project. Managing another Project does not move the caller Session or change its working directory.

## Select

Select a worktree by its exact ID, branch, display name, or path. IDs take precedence, followed by branch, display name, and path. Ambiguous selectors fail.

`list` labels worktrees by availability:

- **registered**: available to Git and managed by Kent
- **external**: available to Git but not managed by Kent; entering it registers it
- **missing**: managed by Kent, but absent from Git

With Session context and no explicit Project or Workspace selection, `list` marks the Session's current worktree with `*`. Explicit selections and Sessionless lists are markerless; Kent does not infer a Session from workspace history.

`status` reports a missing checkout or branch without changing the session's worktree.

## Create and enter

`create` prepares the checkout and runs its setup script without moving the caller. With Session context, the CLI prints a separate `kent worktree enter` command using the created absolute path and, for human callers, `--session`. Without a Session, it prints only the created root. The TUI enters the worktree after creation succeeds.

`create --json` returns the created Worktree and, when a caller Session exists, its location in `target`. For cross-Project creation, `target` describes the caller's location, not the selected management Workspace. Sessionless creation omits `target`.

Outside agent shells, `leave` requires `--session <id>` to move the specified agent back to its main workspace. It follows the same navigation safety and timing as agent-issued leave.

`enter`, `leave`, and deletion of the active worktree may finish after the command returns. For an active Session, `enter` and `leave` join Pending Work until the next eligible Agent Step boundary; the Session keeps its current worktree until the change starts. Kent presents these queued actions as `/wt switch <selector>` and `/wt leave`, regardless of whether they came from the TUI or CLI. `--json` returns the operation acknowledgement. Kent reports completion or failure in session activity. A server restart cancels a pending change.

## Delete

The Main Workspace and Git main worktree cannot be deleted. Deletion blocks while another session has active work in the worktree or a background process uses it. Idle sessions using the worktree move to the main workspace before removal.

Dirty worktrees, or worktrees whose state cannot be determined, require `--force`. This flag applies only to the worktree folder. Agent-shell deletion always retains branches; other CLI callers can pass `--delete-branch` to delete a branch only when Git considers it safe. `--force-delete-branch` requires `--delete-branch` and deletes the branch without Git's merged-branch check.

If Git retains the branch, deletion succeeds and the CLI prints `Kept branch <name>: <diagnostic>`.

## Configuration

Use a setup script to prepare new worktrees with local data such as `.env` files, encryption credentials, Gradle wrappers, installed dependencies, local skills, docs, or config.

```toml
[worktrees]
base_dir = "~/.kent/worktrees"
# setup_script = "scripts/setup-worktree.sh"
# setup_timeout_seconds = 60
```

- `base_dir` sets the namespace for Kent-managed worktrees. Relative creation paths resolve within this directory. Automatic and explicit worktree paths must remain inside it and must not overlap the source workspace in either direction.
- A persisted managed worktree outside this namespace cannot be activated or restored automatically; move it into the namespace before retrying.
- `setup_script` runs after Kent creates a worktree and before the create command or a workflow run uses it. Relative paths resolve from the source workspace root.
- `setup_timeout_seconds` sets the setup script timeout. The default is `60`; `0` or a negative value disables the timeout.

Kent waits for setup to finish. If setup fails, times out, or is canceled, creation fails and the worktree remains available for inspection, repair, or deletion.

Kent invokes the script with the new worktree as its cwd and three positional arguments:

1. source workspace root
2. branch name
3. worktree root

Kent supplies these reserved environment variables, replacing conflicting inherited values:

- `KENT_WORKTREE_SOURCE_WORKSPACE_ROOT` - Original/main workspace root that created the worktree, e.g. `/home/user/dev/app` or `C:\Users\user\dev\app`.
- `KENT_WORKTREE_BRANCH_NAME` - Branch/ref name selected for the new worktree, e.g. `feature/search-fix`.
- `KENT_WORKTREE_ROOT` - Opaque filesystem path to the newly created worktree; setup script runs with this as cwd, for example `/home/user/.kent/worktrees/app/417`. Use this value instead of deriving a path from the branch name.
- `KENT_WORKTREE_SESSION_ID` - Kent session id that requested the worktree, e.g. `b31234ab-78ce-43d1-8f4c-2d6c6d4adbc1`. Present only when a session initiates creation; Sessionless CLI creation and workflow task setup omit it.
- `KENT_WORKTREE_PROJECT_ID` - Kent project id for the workspace/project, e.g. `project-94b18685-19ed-4513-96bb-bcffa10410ff`.
- `KENT_WORKTREE_WORKSPACE_ID` - Kent workspace binding id for the source workspace, e.g. `workspace-2f7b6d4a`.
- `KENT_WORKTREE_WORKTREE_ID` - UUID for the created worktree, e.g. `c4aaf0cf-4c50-4560-b6a2-6c294d0b1495`.
- `KENT_WORKTREE_CREATED_BRANCH` - Whether Kent created a new branch for this worktree, e.g. `true` or `false`.
- `KENT_WORKTREE_PAYLOAD_JSON` - Full setup payload as one JSON string containing all fields above, e.g. `{"source_workspace_root":"/repo","branch_name":"feature/x","worktree_root":"/repo-wt","session_id":null,"project_id":"...","workspace_id":"...","worktree_id":"...","created_branch":true}`.

It also receives the same payload as JSON on stdin:

```json
{
  "source_workspace_root": "/path/to/main/workspace",
  "branch_name": "feature/name",
  "worktree_root": "/path/to/new/worktree",
  "session_id": null,
  "project_id": "...",
  "workspace_id": "...",
  "worktree_id": "...",
  "created_branch": true
}
```

`session_id` is nullable: Sessionless CLI creation and workflow task setup supply `null`, while session-originated creation supplies the requesting session ID, including when managing another Project.
