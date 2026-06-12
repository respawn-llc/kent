---
name: builder-workflows
description: How to use Builder CLI to manage workflows, tasks, nodes, and task comments. Use when the user asks to define/edit workflows, inspect Builder tasks, or add/list/replace Builder task comments.
---

## Workflow Concepts
- A workflow is a graph of node states connected by transition branches.
- A task is the durable user-facing unit of work moving through one workflow.
- A node is a visible workflow state. `start` is where tasks are created, `agent` runs Builder automation, `join` waits for parallel branches, and `terminal` is a sink where automation stops.
- An edge connects a transition group to a target node. A transition group is selected by a `transition_id`; if the group has multiple edges, it fans out to parallel target nodes.
- A graph revision increments when graph-affecting edits are made.
- Model-facing keys such as node keys, edge keys, transition IDs, and parameter keys must be stable lower-case keys matching Builder's model-key rules. Prefer `implement`, `review`, `done`, `needs_changes` over display labels with spaces.

Authoritative command details are the live CLI:

```bash
builder workflow --help
builder task --help
```

## Build A Workflow
The CLI authoring path is: create a workflow, add or update nodes, add or update edges, link it to a project, validate it, then create task records for human-operated workflow execution.

```bash
builder workflow create --description "Implement and review changes" "Implementation Review"
builder workflow inspect "Implementation Review"

builder workflow node add "Implementation Review" --key implement --kind agent --agent <implementer-role>
builder workflow node add "Implementation Review" --key review --kind agent --agent <reviewer-role>

builder workflow edge add "Implementation Review" --from backlog --transition start --edge-key start --to implement --context new_session --prompt "Implement the task."
builder workflow edge add "Implementation Review" --from implement --transition review --edge-key review --to review --context new_session --prompt "Review the implementation."
builder workflow edge add "Implementation Review" --from review --transition done --edge-key done --to done --context new_session

builder workflow link . "Implementation Review" --default
builder workflow validate "Implementation Review" --mode execution
```

Important CLI behavior:

- `workflow create` auto-creates the initial backlog/start and done/terminal shape; inspect after create before adding duplicate start or terminal nodes.
- Workflow references can be exact workflow IDs or exact workflow names. Prefer IDs in scripts and exact names in interactive work.
- Agent roles are stored exactly as provided. Replace role placeholders with configured subagent role names instead of assuming `default` is normalized.
- `workflow node add` returns a generated `node_id`. Edges are authored with node keys via `--from` and `--to`.
- `workflow edge add` creates or reuses the transition group for the given source node and transition ID, then adds an edge to that group.
- Agent-targeting edges require `--prompt <text>`. Omit `--prompt` for edges targeting `start`, `join`, or `terminal` nodes.
- `--transition-description <text>` sets the model-facing transition description on the edge's transition group. Use it to tell the agent when to pick this transition over its siblings.
- `--param <key>=<description>` (repeatable on `edge add` and `edge update`) declares a transition parameter the source agent must produce when taking the transition. On `edge update`, passing `--param` replaces the full parameter set; `--clear-params` removes all parameters. Omitting both preserves existing parameters.
- The workflow CLI does not define `--output`, `--input`, or `--require-output`; those legacy node-owned contract fields are inert. Author transition contracts with `--param` instead.
- `workflow validate` returns exit code 1 for invalid workflows and still prints `valid false` plus validation rows. Treat that as actionable validation output, not a shell failure to ignore.
- Draft workflows can be saved and linked while semantic validation fails. Validate before task creation as a best practice; workflow automation requires execution validation before it starts.

## Edit Existing Workflows
Start every edit by inspecting the current graph:

```bash
builder workflow inspect <workflow>
```

The first section lists node IDs, keys, kinds, display names, and subagent roles. The `transition_groups` section maps source node IDs to transition IDs. The `edges` section maps transition groups to target node IDs and records context mode and approval requirements.

Update existing graph pieces by stable keys or emitted IDs:

- Add agent, join, or terminal nodes with `builder workflow node add`; update existing nodes with `builder workflow node update <workflow> <node-key>`.
- Add routes with `builder workflow edge add`; update existing edges with `builder workflow edge update <workflow> <edge-id>`.
- Link, unlink, and set project defaults with `builder workflow link`, `builder workflow unlink`, and `builder workflow default`.

```bash
builder workflow node update "Implementation Review" implement --agent <implementer-role>
builder workflow edge update "Implementation Review" edge-abc123 --transition needs_review --transition-display-name "Needs Review" --edge-key review --to review --context compact_and_continue_session --prompt "Review the implementation."
```

Node update flags are partial: omitted scalar fields keep current values. Edge update flags are partial; provided `--prompt` replaces the branch prompt.

Validate after each meaningful graph change:

```bash
builder workflow validate <workflow> --mode draft
builder workflow validate <workflow> --mode task_creation
builder workflow validate <workflow> --mode execution
```

Use `draft` while authoring, `task_creation` before creating tasks, and `execution` before starting automation.

## Context And Approval
Each edge requires a context mode:

- `new_session`: start a fresh Builder session and inject task metadata plus previous output.
- `continue_session`: continue the previous Builder session, applying the target node's subagent role context. The reused session stays authoritative only for immutable contract fields already snapshotted by prior model dispatch, so cross-role continuation is allowed.
- `compact_and_continue_session`: ask the previous agent for a handoff, then continue with the next node prompt, handoff, and task metadata.

Use `new_session` as the default unless the workflow intentionally needs conversational continuity across nodes. Use `--requires-approval` when a transition must pause before the target node starts:

```bash
builder workflow edge add <workflow> --from implement --transition done --edge-key done --to done --context new_session --requires-approval
```

## Transition Prompts And Parameters
The transition prompt is the task the target agent runs; parameters are the typed outputs the source agent must produce to carry context across the edge.

```bash
builder workflow edge update "Implementation Review" edge-abc123 \
  --transition-description "Implementation is complete; send for review." \
  --param "plan_file_path=Path to the plan document the implementer followed." \
  --prompt "Review the changes against the plan at {{.Params.plan_file_path}}."
```

Prompt placeholders are Go template fields:

- Built-in task and node fields: `{{.TaskId}}`, `{{.TaskShortId}}`, `{{.TaskTitle}}`, `{{.TaskBody}}`, `{{.NodeId}}`, `{{.NodeKey}}`, `{{.NodeDisplayName}}`. Unsupported top-level fields are validation errors.
- This transition's own parameters: `{{.Params.<parameter_key>}}`. Join-to-agent prompts read aggregated branch parameters the same way.
- A guaranteed-prior transition's parameter: `{{.Params.<transition_id>.<parameter_key>}}`. A transition is guaranteed-prior only when every path from `start` to the prompt-owning edge's source passes through it. Use this to reference an earlier output instead of re-declaring the same parameter; the reference fails execution validation if the transition is not guaranteed-prior.

Parameter rules enforced by validation:

- Parameters are string-only and required once declared. Keys use workflow model-key format and cannot be `transition` or `commentary`.
- Every parameter the source agent provides forms its provision contract: the same parameter key declared on more than one transition out of one source node, or on more than one branch of one fan-out transition, must use an identical description, otherwise the provision fields conflict.
- Declare a parameter on the transition whose source agent can produce it. Prefer referencing a guaranteed-prior transition over re-threading the same value, but re-declare locally where the graph converges or loops and no single upstream transition dominates.

## Operate Tasks
Create task records against a linked/default workflow and project, then inspect them:

```bash
builder task create --project . --workflow <workflow> --title "Fix flaky workflow test" --body "Investigate and fix the failure."
builder task list --project .
builder task show <short-id-or-task-id>
```

Task IDs beginning with `task-` are global. Short IDs are project-scoped, so pass `--project <project-id-or-path>` when the current directory is not the target project.

Use `task show` as the main state probe. It prints task metadata followed by placements, runs, transitions, and comments. Use it to find:

- active placement node IDs and states;
- run IDs and interrupted/completed timestamps;
- comment IDs for replacement or deletion.

## Comments
Task comments are durable task-local notes. They are useful for user instructions, review notes, and work logs that should not be committed into a worktree.

```bash
builder task comment add --project . <short-id-or-task-id> --body "Please prioritize the failing scheduler test."
builder task comment list --project . <short-id-or-task-id>
builder task comment replace <comment-id> --body "Updated note."
```

`comment replace` replaces the full body. Use `comment list --include-deleted` when deleted comments matter.

## Human-Only Task Actions
Do not run task commands that start automation, cancel work, move tasks, resume runs, approve transitions, or delete comments. These operations are reserved for humans. If the user asks for one of them, provide the exact command for the user to run themselves. If workflow work is blocked and you need one of these actions, use `ask_question` to call for help.

## Practical Workflow
For workflow authoring requests:

1. Inspect existing workflows and project links with `builder workflow list`, `builder workflow inspect`, and the current project context.
2. Create or add graph pieces using stable keys. Keep display names human-readable and keys machine-stable.
3. Validate in the strictest mode that matches the user's intent.
4. Link the workflow to the project and set it as default when the user wants new tasks to use it.
5. Create a small smoke-test task when useful, inspect it, and report emitted IDs.

Starting task automation can launch model work and consume provider credits. For exploratory validation prefer `workflow validate`, `task create`, `task show`, and comments.
