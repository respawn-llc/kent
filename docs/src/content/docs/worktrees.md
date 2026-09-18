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

`list`, `create`, and `delete` work without a Session. Use `--project <project-id>` for that Project's default Workspace, or add `--workspace <workspace-id>` to choose another Workspace within it.

```bash
kent worktree create --project <project-id> --workspace <workspace-id> feature/search
```

Without `--project`, Kent uses the agent's Session, otherwise `--session`, otherwise the current directory. `--workspace` alone selects within that Project. These management commands do not move the Session.

## Select

Select a worktree by its exact ID, branch, display name, or path. IDs take precedence, followed by branch, display name, and path. Ambiguous selectors fail.

`list` labels worktrees by availability:

- **registered**: available to Git and managed by Kent
- **external**: available to Git but not managed by Kent; entering it registers it
- **missing**: managed by Kent, but absent from Git

`list` marks the Session's current worktree with `*` unless `--project` or `--workspace` is supplied. Lists without a Session are markerless.

`status` reports a missing checkout or branch without changing the session's worktree.

## Create and enter

`create` prepares the checkout and runs its setup script. With a Session, the CLI prints a separate `kent worktree enter` command; the TUI enters the worktree after creation succeeds.

`enter`, `leave`, and deletion of the active worktree may finish after the command returns. For an active Session, `enter` and `leave` join Pending Work until the next eligible Agent Step boundary; the Session keeps its current worktree until the change starts. Kent presents these queued actions as `/wt switch <selector>` and `/wt leave`, regardless of whether they came from the TUI or CLI. `--json` returns the operation acknowledgement. Kent reports completion or failure in session activity. A server restart cancels a pending change.

## Delete

The Main Workspace and Git main worktree cannot be deleted. Deletion blocks while a Session has active work in the worktree or a background process uses it. Active-Session failures list up to 50 Session names and IDs and indicate when more exist. Ask those Sessions to leave or finish, then retry. Idle Sessions using the worktree move to the main workspace before removal.

Dirty worktrees, or worktrees whose state cannot be determined, require `--force`. This flag applies only to the worktree folder. Agent and human CLI callers retain branches by default. `--delete-branch` deletes a branch without confirmation only when Git considers it safe. Supplying both `--delete-branch` and `--force-delete-branch` authorizes deletion even when the branch is unmerged.

If Git retains the branch, deletion succeeds and the CLI prints `Kept branch <name>: <diagnostic>`.

Deleting an ongoing Task's Worktree preserves the Task, its Sessions, and its managed binding. See [Task target recovery](../workflows/#recover-an-unavailable-task-target) before continuing its executable work.

## Configuration

Use a setup script to prepare new worktrees with local data such as `.env` files, encryption credentials, Gradle wrappers, installed dependencies, local skills, docs, or config.

Setup uses the [configuration layers](../config/#precedence), including the Main Workspace's `.kent/config.local.toml`, and validates the full configuration before running the script. The private file is read from the Main Workspace, not copied into the worktree. Relative `setup_script` paths remain relative to the source workspace.

```toml
[worktrees]
base_dir = "~/.kent/worktrees"
# setup_script = "scripts/setup-worktree.sh"
# setup_timeout_seconds = 60
```

- `base_dir` sets the namespace for Kent-managed worktrees. Automatic and explicit worktree paths must remain inside this directory and must not overlap the source workspace in either direction.
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

`session_id` is nullable: Sessionless CLI creation and workflow task setup supply `null`, while session-originated creation supplies the requesting session ID.
