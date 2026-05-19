---
name: builder-workflows
description: Use Builder workflow/task CLI commands to define or edit workflow graphs, including node/edge updates, project links, task lifecycle actions, comments, approvals, and manual transitions.
---

Builder workflows are durable directed graphs for tasks. Use `builder workflow` to create and inspect graph definitions, and use `builder task` to operate task instances that move through those graphs.

Authoritative command details are the live CLI:

```bash
builder workflow --help
builder task --help
builder workflow <subcommand> --help
builder task <subcommand> --help
```

## Workflow Concepts
- A workflow is a graph of node (status) transitions constrained by Edges (input-output and condition pairs).
- A task is the durable user-facing unit of work moving through one workflow.
- A node is a visible workflow state. `start` is where tasks are created, `agent` runs Builder automation, `join` waits for parallel branches, and `terminal` is a sink where automation stops.
- An edge connects a transition group to a target node. A transition group is selected by a `transition_id`; if the group has multiple edges, it fans out to parallel target nodes.
- A graph revision increments when graph-affecting edits are made. CLI output includes revisions for traceability.
- Model-facing keys such as node keys, edge keys, transition IDs, output field names, and binding names must be stable lower-case keys matching Builder's model-key rules. Prefer `implement`, `review`, `done`, `needs_changes` over display labels with spaces.

## Build A Workflow
The CLI authoring path is: create a workflow, add or update nodes, add or update edges, link it to a project, validate it, then create/start tasks.

```bash
builder workflow create --description "Implement and review changes" "Implementation Review"
builder workflow inspect "Implementation Review"

builder workflow node add "Implementation Review" --key implement --kind agent --agent <implementer-role> --prompt "Implement the task." --output summary="Implementation summary"
builder workflow node add "Implementation Review" --key review --kind agent --agent <reviewer-role> --prompt "Review the implementation."

builder workflow edge add "Implementation Review" --from backlog --transition start --edge-key start --to implement --context new_session
builder workflow edge add "Implementation Review" --from implement --transition review --edge-key review --to review --context new_session --require-output summary --input implementation_summary=transition_output:summary
builder workflow edge add "Implementation Review" --from review --transition done --edge-key done --to done --context new_session

builder workflow link . "Implementation Review" --default
builder workflow validate "Implementation Review" --mode execution
```

Important CLI behavior:

- `workflow create` auto-creates the initial backlog/start and done/terminal shape; inspect after create before adding duplicate start or terminal nodes.
- Workflow references can be exact workflow IDs or exact workflow names. Prefer IDs in scripts and exact names in interactive work.
- Agent roles are stored exactly as provided. Replace role placeholders with configured subagent role names instead of assuming `default` is normalized.
- `workflow node add` returns a generated `node_id`. Edges are authored with node keys via `--from` and `--to`, while `task move` needs a target node ID.
- `workflow edge add` creates or reuses the transition group for the given source node and transition ID, then adds an edge to that group.
- Node output fields use `--output name=description`; repeat it for multiple fields. Updating a node with any `--output` flag replaces the node's output field list.
- Edge input bindings use `--input name=source:field`, where common sources are `task`, `transition_output`, and `join`. Edge output requirements use `--require-output <field>` and must reference an output field declared on the source node. Updating an edge with any `--input` or `--require-output` flag replaces that list.
- `workflow validate` returns exit code 1 for invalid workflows and still prints `valid false` plus validation rows. Treat that as actionable validation output, not a shell failure to ignore.
- Draft workflows can be saved and linked while semantic validation fails. Validate before task creation as a best practice; `task start` enforces execution validation before automation starts.

## Edit Existing Workflows
Start every edit by inspecting the current graph:

```bash
builder workflow inspect <workflow>
```

The first section lists node IDs, keys, kinds, display names, subagent roles, and any `output_field` rows. The `transition_groups` section maps source node IDs to transition IDs. The `edges` section maps transition groups to target node IDs and records context mode, approval requirements, `input_binding` rows, and `output_requirement` rows.

Update existing graph pieces by stable keys or emitted IDs:

- Add agent, join, or terminal nodes with `builder workflow node add`; update existing nodes with `builder workflow node update <workflow> <node-key>`.
- Add routes with `builder workflow edge add`; update existing edges with `builder workflow edge update <workflow> <edge-id>`.
- Link, unlink, and set project defaults with `builder workflow link`, `builder workflow unlink`, and `builder workflow default`.

```bash
builder workflow node update "Implementation Review" implement --prompt "Implement the task and include risk notes." --output summary="Implementation summary" --output risks="Known risks"
builder workflow edge update "Implementation Review" edge-abc123 --transition needs_review --transition-display-name "Needs Review" --edge-key review --to review --context compact_and_continue_session --require-output summary --input implementation_summary=transition_output:summary
```

Node update flags are partial except for repeated list flags: omitted scalar fields keep current values, while provided `--prompt` or `--agent` can intentionally set an empty value, and provided `--output` replaces the output fields list. Edge update flags are also partial; provided `--input` and `--require-output` replace their lists.

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
- `continue_session`: continue the previous Builder session. This requires compatible source/target session context and the same subagent role across direct agent continuation.
- `compact_and_continue_session`: ask the previous agent for a handoff, then continue with the next node prompt, handoff, and task metadata.

Use `new_session` as the default unless the workflow intentionally needs conversational continuity across nodes. Use `--requires-approval` when a transition must pause before the target node starts:

```bash
builder workflow edge add <workflow> --from implement --transition done --edge-key done --to done --context new_session --requires-approval
```

When an edge requiring approval is selected, approve the pending transition with:

```bash
builder task approve <transition-id>
```

The `<transition-id>` for approval is the task transition record ID emitted by `task show`, `task move`, or workflow runtime output, not the graph's transition key such as `done`.

## Operate Tasks
Create tasks against a linked/default workflow and project:

```bash
builder task create --project . --workflow <workflow> --title "Fix flaky workflow test" --body "Investigate and fix the failure."
builder task list --project .
builder task show <short-id-or-task-id>
builder task start <short-id-or-task-id>
```

Task IDs beginning with `task-` are global. Short IDs are project-scoped, so pass `--project <project-id-or-path>` when the current directory is not the target project.

Use `task show` as the main state probe. It prints task metadata followed by placements, runs, transitions, and comments. Use it to find:

- active placement node IDs and states;
- run IDs and interrupted/completed timestamps;
- task transition record IDs for pending approval;
- comment IDs for replacement or deletion.

Resume an interrupted task run:

```bash
builder task resume --project . <short-id-or-task-id>
```

Cancel a task when no further automation should run:

```bash
builder task cancel --project . --reason "superseded by TASK-123" <short-id-or-task-id>
```

## Manual Moves
Manual moves place a task into a target node ID and can provide transition commentary plus output values:

```bash
builder task move --project . <short-id-or-task-id> <target-node-id> --commentary "Manual review complete" --output summary="Reviewed and accepted"
```

Use `workflow inspect <workflow>` to get target node IDs. Use manual moves deliberately: validation can reject moves when required output, approval, session-continuation, or graph constraints are not satisfied.

## Comments
Task comments are durable task-local notes. They are useful for user instructions, review notes, and work logs that should not be committed into a worktree.

```bash
builder task comment add --project . <short-id-or-task-id> --body "Please prioritize the failing scheduler test."
builder task comment list --project . <short-id-or-task-id>
builder task comment replace <comment-id> --body "Updated note."
builder task comment delete <comment-id>
```

`comment replace` replaces the full body. `comment delete` soft-deletes the comment; use `comment list --include-deleted` when deleted comments matter.

## Practical Workflow
For workflow authoring requests:

1. Inspect existing workflows and project links with `builder workflow list`, `builder workflow inspect`, and the current project context.
2. Create or add graph pieces using stable keys. Keep display names human-readable and keys machine-stable.
3. Validate in the strictest mode that matches the user's intent.
4. Link the workflow to the project and set it as default when the user wants new tasks to use it.
5. Create a small smoke-test task, start it only when the user intends real automation, and report emitted IDs.

Real task starts can launch model work and consume provider credits. For user-requested workflow operation it is fine to run them; for exploratory validation prefer `workflow validate`, `task create`, `task show`, and comments unless the user asked to start automation.
