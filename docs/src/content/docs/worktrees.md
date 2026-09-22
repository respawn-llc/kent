---
title: Worktrees
description: Create, enter, and delete Git worktrees from Kent.
---

Kent will create and manage worktrees for you. Agents will enter new worktrees if they need to, and workflows can automatically create worktrees for their tasks (see [workflows](../workflows)). If you want to manually manage worktrees, run `/wt` in the TUI, or ask the agent to use the CLI:

```bash
kent worktree status
kent worktree list
kent worktree create <branch-or-ref> [path]
kent worktree enter <selector>
kent worktree leave
kent worktree delete <selector>
```

## Configuration

Use a setup script to prepare new worktrees with local data such as `.env` files, encryption credentials, Gradle wrappers, installed dependencies, local skills, docs, or config.

Setup uses the [configuration layers](../config/#precedence), including the Main Workspace's `.kent/config.local.toml`. The private file is read from the Main Workspace. Relative `setup_script` paths remain relative to the source workspace.

```toml
[worktrees]
base_dir = "~/.kent/worktrees"
# setup_script = "scripts/setup-worktree.sh"
# setup_timeout_seconds = 60
```

- `base_dir` sets the namespace for Kent-managed worktrees.
- `setup_script` runs after Kent creates a worktree. Relative paths resolve from the source workspace root.
- `setup_timeout_seconds` sets the setup script timeout. The default is `60`; `0` or a negative value disables the timeout.

Kent invokes the script with the new worktree as its cwd and three positional arguments:

1. source workspace root
2. branch name
3. worktree root

Kent supplies these environment variables to the script:

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
