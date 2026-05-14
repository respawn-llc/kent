# Async Workflow Orchestration

## Purpose

Design Builder's backend foundation for asynchronous, configurable agent pipelines before frontend implementation. The feature turns Builder from a manually driven terminal coding-agent harness into an orchestrator for project-scoped workflows where tasks move through graph nodes, Kanban statuses, agent workers, review loops, and merge/cleanup stages.

Frontend design is intentionally out of scope for this document except where backend contracts must support later workflow/Kanban UI, question inbox, task views, and status visualization.

## Current Idea

- Users define workflows made of nodes and transitions.
- Nodes can map to Kanban statuses; several nodes may share one status.
- A task entering an auto-runnable node can start an agent or other executor automatically.
- Agent nodes use configured subagent roles, custom prompts, dynamic completion tools, and goal-like autonomous looping.
- Completion tools return structured decisions such as advance, send back, request user input, fail, or move to another workflow branch.
- Review nodes can emit findings and move tasks back to implementation; architecture/design nodes can send underspecified work back to design.
- Work should run asynchronously through a queue with global/project/workflow concurrency limits to avoid rate limits.
- Work should use Builder's existing project, workspace, worktree, session, goal, ask_question, background process, and server architecture.

## External Reference

Anthropic's "Building Effective AI Agents" article is useful vocabulary for workflow families Builder should support: prompt chaining, routing, parallelization/sectioning/voting, orchestrator-workers, evaluator-optimizer, and autonomous agents. Builder's design should model these as composable workflow graph/execution primitives rather than separate products. The article also reinforces two constraints that match Builder's direction: keep orchestration simple/transparent and invest heavily in model-facing tool interfaces.

Source: https://www.anthropic.com/engineering/building-effective-agents

## Known Constraints

- Durable domain identity is already `project > workspace > worktree`; async tasks must fit this model instead of reintroducing workspace-scoped identity.
- SQLite is authoritative for structured metadata; large append-only artifacts remain file-backed.
- Full transcript history can be gigabytes; workflow code must not load `events.jsonl` fully into memory.
- Existing goal mode is user/session-oriented and requires `ask_question`; workflow agents likely need a server-owned autonomous task loop with similar nudge semantics but different completion authority.
- Current subagent roles configure model/provider/tool/settings overlays, not workflow-specific node behavior.
- Existing `RunPromptService` is one-shot headless prompt execution and returns a final assistant string; async workflow agents need durable, resumable runs with structured terminal events.
- Workflow definitions must validate referenced subagent roles before execution. If config changes remove or invalidate a role, affected workflow runs should fail fast with actionable validation errors rather than silently substituting another agent.
- Each workflow edge may specify context-preservation mode for the next node: start a new blank session with previous output/task metadata, continue the prior session with a new prompt/goal, or compact then continue the prior session with metadata.
- Domain language is defined in `docs/dev/TERMINOLOGY.md`; use it consistently before naming database tables and services.

## Open Questions

- What CLI surface is enough to test v1 without building frontend?
- How should user questions pause/resume tasks across many concurrent agents?
- What is the migration path from current sessions/goals/subagents into workflow runs?

## Product Decisions

Decisions will be recorded here during the planning interview.

- V1's smallest testable vertical slice is backend/API/CLI first: create a task, auto-run at least one agent node in a worktree, capture structured completion, and move task status. The CLI can be clunky and removable; it exists to test backend behavior before GUI investment.
- `Task` is the primary durable work item. Existing Builder sessions are execution artifacts under tasks, not the task itself. One task may accumulate many sessions through loops, branches, retries, and complex chains.
- Moving a task from backlog to to-do should auto-run through auto nodes until the task reaches a terminal state or blocks on a user question, manual gate, error, capacity limit, or other explicit stop condition.
- Workflow definitions may rely on TOML-configured subagent roles. This creates config drift risk; v1 accepts fail-fast validation rather than inventing a full stable workflow file/schema solution immediately.
- Builder should support the major agentic workflow patterns from the Anthropic article in some form: prompt chaining, routing, parallelization with aggregation, orchestrator-workers, evaluator-optimizer loops, and open-ended autonomous agents.
- Per-edge context preservation must be configurable in v1 with at least three modes: `new_session`, `continue_session`, and `compact_and_continue_session`.
- V1 workflow definitions are SQLite-authoritative and created/edited through backend API plus a minimal CLI. No stable graph file format is required in v1.
- Workflow definitions should be globally reusable. Projects link to workflow definitions rather than copying graph definitions. Workflow validation is project-contextual because subagent roles and workspace config may differ by project.
- A project can link multiple workflows and has one default workflow for task creation.
- V1 does not snapshot/version workflow definitions for existing tasks. Tasks use the current linked workflow definition; workflow-version edge cases are deferred.
- Destructive graph edits are guarded. Deleting workflow graph elements requires no non-terminal tasks to reference them; deleting the initial/backlog node also requires selecting a replacement initial node.
- Node config and edge config are distinct. Nodes configure agent runs: subagent role, prompt, output schema, limits, and run stop conditions. Edges configure transitions: next node, human approval/manual interaction, context preservation, input bindings, routing, and join/aggregation behavior.
- Subagent role is the executable node's assignee. There is no separate assignee field. UI can display subagent roles as assignees for convenience.
- Workflow nodes select existing subagent roles only; no per-node model/provider/tool/auth overrides. Subagent roles define agent identity fully.
- V1 should keep node identity equal to visible Kanban column/status identity. Multiple executable nodes sharing one column creates ambiguous manual moves and unclear debugging. Later UI can add display grouping if needed.
- Workflows can contain executable agent nodes, terminal nodes, and join nodes. Approval remains an edge property, not a separate manual-node requirement.
- Workflow creation should auto-create default `backlog` and `done` nodes as ordinary editable nodes. This avoids hardcoded unmapped statuses while keeping setup ergonomic.
- V1 workflows have exactly one start node. The start node is non-executable and has no inputs. Multiple start nodes are expected later and should not be made architecturally difficult.
- Terminal nodes are strict sinks. Manual reopen/rework is a user override execution, not a durable graph transition or graph mutation.
- Workflow validation should reject detached graph islands. Every node must be reachable from the start node, every non-terminal node must be able to reach a terminal node, self-loops/cycles are allowed, and terminal nodes cannot auto-run.
- Parallel joins always wait for all required inputs in v1. Racing/first-success semantics are out of scope.
- Join nodes are non-agent fan-in points. They aggregate inbound transition payloads into a deterministic results collection and then follow their outgoing edge. If synthesis is needed, put an agent node after the join.
- Parallel branches are ordinary workflow nodes that happen to run concurrently. They are not subtasks and do not require a separate child-task model. A task may have multiple active node placements/runs while explicit fan-out is active.
- Fan-out uses transition groups. `transition_id` selects a transition group; a transition group can contain one edge or multiple edges. Multiple edges in the group create parallel node placements/runs that later converge at a join.
- Orchestrator-workers should not dynamically create workflow nodes or Kanban columns in v1. An orchestrator is an ordinary agent node that may use existing subagent/session infrastructure inside its run or feed statically defined graph branches.
- Agent nodes complete by calling a node-specific completion tool, not by returning natural language. The completion payload chooses a user-defined outgoing transition when the node has more than one outgoing edge and supplies node output fields. Runtime failure, cancellation, and unanswered questions are orchestration states, not model-selected terminal statuses.
- Workflow runs should treat a normal assistant final answer as invalid output. Runtime should append a nudge and continue until the model calls the completion tool, calls `ask_question`, is canceled, or hits a runtime error.
- The model-facing completion control should be a static workflow-only tool, not a CLI command and not a dynamically generated per-node tool schema. Recommended shape: `complete_node` with `transition_id`, optional `commentary`, and `payload` as a flat `map[string]string`. Runtime validates the payload against active task/run/node state. If called outside an active workflow run, it returns an explicit not-in-workflow error.
- `complete_node` is workflow control infrastructure and is available in every workflow run regardless of subagent role tool config.
- User questions use existing `ask_question` tool-call/session infrastructure. A model does not report `needs_user_input` as a completion status; it calls `ask_question`, and the run pauses until answered. V1 should not introduce a separate durable task-question source of truth unless existing session transcript/resume semantics prove insufficient.
- Node output schemas are user-authored but intentionally flat. Fields are strings; arrays, nested objects, and mixed scalar types are out of scope for v1. String-only fields keep UI/query/schema generation tractable while allowing users to stringify richer content when needed.
- Completion tools expose only `transition_id`, never `next_node`. The selected transition group derives target nodes and transition behavior.
- Every completion tool includes optional `commentary` as a visible, pass-along string escape hatch for content not captured by configured fields.
- Custom completion fields are optional in the generated tool schema. The selected edge may impose payload requirements after `transition_id` is known; if required fields are missing, runtime returns a structured tool error/developer nudge and keeps the same run going.
- Edge approval is a boolean edge property. When approval is required, the source run finishes and the node transition waits before scheduling the target run.
- A task owns one managed worktree by default. Implementation and review nodes reuse it unless later node/edge configuration adds an explicit override.
- Builder creates the task worktree when scheduling the first executable run that needs workspace access. In the default pipeline this coincides with moving the task from backlog into executable work.
- Task worktree branch creation should reuse existing worktree logic. The branch name is the task short ID.
- `continue_session` and `compact_and_continue_session` may transition across nodes with different subagent roles. Existing session locking means model/tool differences cannot be fully applied in the same session; Builder should apply the target node prompt/role guidance and accept that the prior session contract remains. Use `new_session` when the target node must get its fresh subagent contract.
- Autonomous node stop limits are not part of v1. Operator cancellation and runtime errors still stop work.
- The execution queue is durable in SQLite. Startup reconciliation should inspect runnable/running/interrupted state and requeue safe work.
- The queue does not own runtime leases. Runtime leases remain execution-control state, similar to current client/frontend leases, not durable queue authority.
- If execution stops mid-node, mark the run `interrupted`, preserve session transcript and dirty worktree state, and require human resume. Resume continues the interrupted session/run instead of rerunning the whole node from scratch.
- V1 has no automatic retry. Failures, cancellations, crashes, and model/runtime interruptions converge on an interrupted state with explicit human resume.
- Concurrency limit is global only and configured in `config.toml`.
- A task may have multiple active node placements/runs only when the workflow graph explicitly fans out into parallel branches; otherwise task execution is single-active-run.
- Task required fields are title, short ID, and body. Task metadata should be designed for future import/export and may include a source URL for imported external work.
- Task short IDs are project-scoped sequential keys with a project key prefix, e.g. `BLD-123`. Project creation should choose the key explicitly; default suggestion can use the first three letters of the project name.
- Existing projects without a project key should get one from the default project-name logic when task support is initialized, with collision handling.
- If a workflow references a subagent role that no longer exists in effective config, the node transition blocks with a validation error before scheduling the run. Same-name subagent setting changes are accepted.
- Agents may add, replace, and soft-delete task comments through CLI/API task management, not model-callable comment tools. A skill or reminder should teach workflow agents the CLI. Comments should record author/source agent when available and stay in Builder persistence, not files in the worktree. Task comments are not injected automatically into agent context; agents read them through CLI when needed.
- `RunPromptService` should not back workflow nodes. It is a one-shot final-string API, while workflow nodes need durable runs, structured completion, interruption, and resume.
- Existing user goal state should not be reused as workflow autonomy state. Workflow needs a goal-like loop shape, but task/node/run identity must own completion, interruption, and resume semantics.
- Task lifecycle state should derive from node placement/run state rather than a separate task status enum. The task's node placement is the workflow/Kanban state; blocked/running/interrupted/done conditions come from runs and terminal nodes.

## Completion Control Schema

The static `complete_node` tool should have a stable schema:

- `transition_id`: optional string. Runtime requires it when a node has more than one outgoing transition group and validates it against valid transition IDs.
- `commentary`: optional catch-all string field; visible to the user and passed along to the next node by default.
- `payload`: optional flat string map for user-defined fields such as `review_findings`, `verification`, `architecture_notes`, or `merge_notes`.

Prefer `transition_id` over `next_node` because transition groups and edges own approval, context preservation, input bindings, fan-out, and routing semantics. Target nodes are derived from the selected transition group.

Node-specific field guidance belongs in developer prompts, not in the tool schema. Selected-edge validation checks payload requirements. Example: a review node can define `review_findings` as an available output field, while only the `changes_requested` edge requires it.

## Input Binding Direction

Edges should use explicit input bindings from source fields to target input names. Target node prompts can reference bound input names with simple template placeholders such as `{{review_comments}}`.

## Manual Move Requirements

Manual moves are node transitions initiated by the user instead of the source node's completion tool:

- A manual move must use an edge or synthesize equivalent edge input metadata explicitly.
- Moving backward can reuse the latest stored transition payloads and task metadata from prior completed runs.
- Moving to an executable node should pause before queueing and require explicit user approval to start automation from the manually selected node.
- If the selected transition context-preservation mode requires continuing a session and no valid source session exists, the move is rejected. The user must choose a transition that can use `new_session` or provide a valid continuation source.
- Manual move implementation needs regression tests for forward moves with provided payload, backward moves reusing existing payloads, rejection when required payload is absent, rejection when continuation is required but unavailable, and approval-before-queue behavior.

## Audit Direction

Keep normalized rows for tasks/runs/transitions/comments and durable transition logs for debugging and UI history. Do not design a full event-sourced task system for v1.

## Domain Vocabulary

See `docs/dev/TERMINOLOGY.md`.

## Backend Architecture Draft

### Composition

Add a server-owned workflow orchestration layer above sessions/runtimes:

- `server/workflow`: domain service for workflow definitions, graph validation, project links, tasks, comments, node placements, transitions, runs, queue reconciliation, and scheduler decisions.
- `server/workflowruntime`: execution adapter that prepares sessions, starts/resumes node runs, injects node prompts, handles `complete_node`, and applies context-preservation modes.
- `server/workflowview`: read-model service for project boards, task detail, run/transition history, and CLI-friendly views.
- `shared/serverapi/workflow.go`: serializable request/response DTOs and validation for workflow/task APIs.
- `shared/servicecontract`: workflow service interfaces following existing route-shaped contract pattern.
- `shared/client`: loopback/remote workflow clients.
- `server/transport`: RPC routes for workflow/task operations.
- `cli/builder`: minimal `workflow` and `task` commands for backend testing and agent CLI usage.

`server/core` should compose workflow services from the metadata store, runtime registry, session runtime service, worktree service, auth manager, and config. The workflow scheduler starts with the server process and stops with core shutdown.

### Runtime Model

Workflow runtime is not `RunPromptService`. It needs durable run identity, structured completion, interruption, resume, and queue scheduling. Reuse lower-level runtime/session pieces:

- Session planning from `server/launch` and subagent role resolution.
- Runtime activation/control from `server/sessionruntime` and `server/runtimecontrol`.
- Step execution, compaction, queued user-message flushing, and transcript persistence from `server/runtime`.
- Worktree creation/switching from `server/worktree`.
- Existing `ask_question` tool/session flow.

Do not reuse user goal state as workflow state. Workflow autonomy uses a goal-like loop shape:

1. Prepare node prompt from task, node config, edge input bindings, comments accessible through CLI, and any transition payload.
2. Start or resume a session according to context-preservation mode.
3. Run model turns until one of: `complete_node`, `ask_question`, interruption/cancel/error.
4. Treat normal assistant final answers as invalid output and append a developer nudge.
5. On accepted `complete_node`, persist transition payload and stop the node run without sending another model turn.

### Completion Tool

`complete_node` is a static workflow-control tool exposed only when a session is executing a workflow node. It is available regardless of subagent role tool config.

Stable schema:

```json
{
  "transition_id": "string",
  "commentary": "string",
  "payload": {
    "field_name": "string"
  }
}
```

Runtime validation:

- If not in a workflow run, return a tool error.
- If multiple transition groups are available, require `transition_id`.
- Validate `transition_id` against source node transition groups.
- Validate payload field names against node output schema.
- Validate selected-edge payload requirements after transition group selection.
- On validation failure, return a structured tool error and append a developer nudge; keep the run active.
- On success, persist transition log, mark source run completed, apply approval/queue/fan-out/join rules.

The runtime step loop needs a workflow completion signal so tool execution can terminate the node run immediately after persisting the tool result.

### Node Transitions

Automatic node transitions come from accepted `complete_node` payloads. Manual moves are user override executions with stricter validation:

- They must choose a real edge/transition group or provide equivalent edge input metadata.
- They can reuse stored payloads from prior completed runs when moving backward.
- They pause before automation and require explicit user approval to queue the target run.
- They reject continuation modes when no valid source session exists.

Edge approvals persist as pending transition logs. Approval means: approve a selected transition payload from a specific completed run/edge. After approval, Builder schedules target placements/runs from the stored payload.

### Parallelism And Joins

Transition groups model fan-out. A selected transition group can contain multiple edges. Each edge creates a target node placement and, for executable nodes, a queued run. These are still one task, not subtasks.

Join nodes are non-agent fan-in points:

- They wait for all active branch placements created by the selected transition group that target the join's inbound set.
- They aggregate inbound payloads into deterministic results collection.
- They then follow their outgoing transition group.
- Agent synthesis belongs in a normal agent node after the join.

### Queue And Recovery

The execution queue is durable SQLite state, separate from runtime leases.

Run states:

- `queued`: eligible for scheduler claim.
- `running`: scheduler started runtime work.
- `waiting_for_question`: run is blocked in `ask_question` flow.
- `interrupted`: run stopped before valid transition payload; session/worktree preserved for resume.
- `completed`: accepted transition payload or non-agent node finished.
- `failed`: unrecoverable orchestration failure that cannot continue without user action.
- `canceled`: user canceled the run.

Startup reconciliation:

- Leave `queued`, `completed`, `failed`, and `canceled` as-is.
- Keep `waiting_for_question` only if the session/runtime can rehydrate the pending ask; otherwise mark interrupted and resume through the existing session transcript.
- Leave pending approval transitions as-is.
- Mark stale `running` runs as `interrupted`.
- Do not auto-retry interrupted runs.
- Explicit resume continues the interrupted session/run from its current transcript/worktree state.

Concurrency is one global config value, defaulting to five automated runs.

### Worktrees

A task owns one managed worktree by default. Builder creates it when the first workspace-requiring executable run is scheduled. Branch name is the task short ID and should reuse existing worktree branch/root collision handling.

Worktree deletion/retargeting must treat non-terminal tasks referencing a managed worktree as blockers.

### CLI Surface

Minimal testing-oriented commands:

- `builder workflow create <name>`
- `builder workflow node add <workflow> --id <id> --kind agent|join|terminal|start --prompt <text> --agent <role>`
- `builder workflow edge add <workflow> --from <node> --transition <id> --to <node> --context new_session|continue_session|compact_and_continue_session`
- `builder workflow link <project> <workflow> [--default]`
- `builder workflow validate <workflow> [--project <project>]`
- `builder task create --title <title> --body <body> [--workflow <workflow>]`
- `builder task list [--project <project>]`
- `builder task show <short-id>`
- `builder task move <short-id> <node> [--payload field=value ...]`
- `builder task approve <transition-id>`
- `builder task resume <short-id>`
- `builder task comment add|replace|delete <short-id> ...`

The exact CLI can be clunky; it exists to exercise backend behavior and teach agents task CLI usage.

## Data Model Draft

Use SQLite for structured workflow/task state. Keep transcripts and large session artifacts file-backed through existing session persistence.

### Existing Tables To Extend

- `projects`
  - Add `project_key TEXT` with global uniqueness in a persistence root.
  - Add `next_task_seq INTEGER NOT NULL DEFAULT 1`, or use a separate counter table if cleaner for migration.
- `sessions`
  - Add index on `(worktree_id, updated_at_unix_ms DESC)` for task/worktree blockers and views.
- `worktrees`
  - Keep physical worktree metadata here. Task ownership can live on `tasks.managed_worktree_id`; add worktree provenance later only if views need it.

### New Tables

`workflows`

- `id TEXT PRIMARY KEY`
- `name TEXT NOT NULL`
- `description TEXT NOT NULL DEFAULT ''`
- `start_node_id TEXT`
- `created_at_unix_ms INTEGER NOT NULL`
- `updated_at_unix_ms INTEGER NOT NULL`
- `metadata_json TEXT NOT NULL DEFAULT '{}'`

`workflow_nodes`

- `id TEXT PRIMARY KEY`
- `workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE`
- `node_key TEXT NOT NULL`
- `kind TEXT NOT NULL` (`start|agent|join|terminal`)
- `display_name TEXT NOT NULL`
- `subagent_role TEXT NOT NULL DEFAULT ''`
- `prompt_template TEXT NOT NULL DEFAULT ''`
- `output_schema_json TEXT NOT NULL DEFAULT '{}'`
- `sort_order INTEGER NOT NULL DEFAULT 0`
- `metadata_json TEXT NOT NULL DEFAULT '{}'`
- unique `(workflow_id, node_key)`

`workflow_transition_groups`

- `id TEXT PRIMARY KEY`
- `workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE`
- `source_node_id TEXT NOT NULL REFERENCES workflow_nodes(id) ON DELETE CASCADE`
- `transition_id TEXT NOT NULL`
- `display_name TEXT NOT NULL DEFAULT ''`
- `sort_order INTEGER NOT NULL DEFAULT 0`
- `metadata_json TEXT NOT NULL DEFAULT '{}'`
- unique `(source_node_id, transition_id)`

`workflow_edges`

- `id TEXT PRIMARY KEY`
- `workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE`
- `transition_group_id TEXT NOT NULL REFERENCES workflow_transition_groups(id) ON DELETE CASCADE`
- `source_node_id TEXT NOT NULL REFERENCES workflow_nodes(id) ON DELETE CASCADE`
- `target_node_id TEXT NOT NULL REFERENCES workflow_nodes(id) ON DELETE CASCADE`
- `requires_approval INTEGER NOT NULL DEFAULT 0`
- `context_mode TEXT NOT NULL`
- `input_bindings_json TEXT NOT NULL DEFAULT '{}'`
- `payload_requirements_json TEXT NOT NULL DEFAULT '{}'`
- `sort_order INTEGER NOT NULL DEFAULT 0`

`project_workflow_links`

- `id TEXT PRIMARY KEY`
- `project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE`
- `workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE RESTRICT`
- `is_default INTEGER NOT NULL DEFAULT 0`
- `created_at_unix_ms INTEGER NOT NULL`
- `updated_at_unix_ms INTEGER NOT NULL`
- unique `(project_id, workflow_id)`
- partial unique default per project where `is_default = 1`

`tasks`

- `id TEXT PRIMARY KEY`
- `project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE`
- `workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE RESTRICT`
- `task_seq INTEGER NOT NULL`
- `short_id TEXT NOT NULL`
- `title TEXT NOT NULL`
- `body TEXT NOT NULL`
- `source_url TEXT NOT NULL DEFAULT ''`
- `managed_worktree_id TEXT REFERENCES worktrees(id) ON DELETE SET NULL`
- `created_at_unix_ms INTEGER NOT NULL`
- `updated_at_unix_ms INTEGER NOT NULL`
- `metadata_json TEXT NOT NULL DEFAULT '{}'`
- unique `(project_id, task_seq)`
- unique `(project_id, short_id)`

`task_node_placements`

- `id TEXT PRIMARY KEY`
- `task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE`
- `node_id TEXT NOT NULL REFERENCES workflow_nodes(id) ON DELETE RESTRICT`
- `state TEXT NOT NULL` (`active|waiting_approval|completed|superseded`)
- `created_by_transition_id TEXT REFERENCES task_transitions(id) ON DELETE SET NULL`
- `created_at_unix_ms INTEGER NOT NULL`
- `updated_at_unix_ms INTEGER NOT NULL`

`task_runs`

- `id TEXT PRIMARY KEY`
- `task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE`
- `placement_id TEXT NOT NULL REFERENCES task_node_placements(id) ON DELETE CASCADE`
- `node_id TEXT NOT NULL REFERENCES workflow_nodes(id) ON DELETE RESTRICT`
- `session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL`
- `state TEXT NOT NULL`
- `queued_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `started_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `finished_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `interrupted_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `error_json TEXT NOT NULL DEFAULT '{}'`
- `metadata_json TEXT NOT NULL DEFAULT '{}'`

`task_transitions`

- `id TEXT PRIMARY KEY`
- `task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE`
- `source_run_id TEXT REFERENCES task_runs(id) ON DELETE SET NULL`
- `source_placement_id TEXT REFERENCES task_node_placements(id) ON DELETE SET NULL`
- `source_node_id TEXT REFERENCES workflow_nodes(id) ON DELETE SET NULL`
- `transition_group_id TEXT REFERENCES workflow_transition_groups(id) ON DELETE SET NULL`
- `transition_id TEXT NOT NULL`
- `actor TEXT NOT NULL` (`agent|user|system`)
- `state TEXT NOT NULL` (`pending_approval|approved|applied|rejected|invalid`)
- `commentary TEXT NOT NULL DEFAULT ''`
- `payload_json TEXT NOT NULL DEFAULT '{}'`
- `created_at_unix_ms INTEGER NOT NULL`
- `applied_at_unix_ms INTEGER NOT NULL DEFAULT 0`

`task_transition_edges`

- `id TEXT PRIMARY KEY`
- `task_transition_id TEXT NOT NULL REFERENCES task_transitions(id) ON DELETE CASCADE`
- `workflow_edge_id TEXT REFERENCES workflow_edges(id) ON DELETE SET NULL`
- `target_node_id TEXT REFERENCES workflow_nodes(id) ON DELETE SET NULL`
- `target_placement_id TEXT REFERENCES task_node_placements(id) ON DELETE SET NULL`
- `state TEXT NOT NULL` (`pending|queued|completed|blocked`)
- `metadata_json TEXT NOT NULL DEFAULT '{}'`

`task_comments`

- `id TEXT PRIMARY KEY`
- `task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE`
- `body TEXT NOT NULL`
- `author_kind TEXT NOT NULL`
- `author_id TEXT NOT NULL DEFAULT ''`
- `source_run_id TEXT REFERENCES task_runs(id) ON DELETE SET NULL`
- `created_at_unix_ms INTEGER NOT NULL`
- `updated_at_unix_ms INTEGER NOT NULL`
- `deleted_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `metadata_json TEXT NOT NULL DEFAULT '{}'`

### Indexes

- `workflow_nodes(workflow_id, sort_order)`
- `workflow_transition_groups(source_node_id, transition_id)`
- `workflow_edges(transition_group_id, sort_order)`
- `workflow_edges(source_node_id)`
- `workflow_edges(target_node_id)`
- `tasks(project_id, updated_at_unix_ms DESC)`
- `tasks(project_id, short_id)`
- `task_node_placements(task_id, state)`
- `task_node_placements(node_id, state)`
- `task_runs(state, queued_at_unix_ms)`
- `task_runs(task_id, created_at_unix_ms DESC)`
- `task_transitions(task_id, created_at_unix_ms DESC)`
- `task_comments(task_id, updated_at_unix_ms DESC)`

### Core Query Shapes

- Load project board: project workflow links, workflow nodes, active task placements, active/waiting/interrupted run summaries.
- Create task: allocate project sequence atomically, create task, create start-node placement.
- Move task manually: validate graph/input/continuation, create transition log, create pending approval state before queue.
- Claim next run: count running runs globally, select queued run ordered by queued time, atomically mark running.
- Complete run: persist transition payload, validate transition group/edges, create target placements/runs or pending approvals, mark source placement completed.
- Join check: query active placements/runs created by same transition group/inbound set; aggregate once all complete.
- Resume interrupted run: validate session/worktree still available, mark queued/running through scheduler, continue existing session.

## Implementation Plan

To be drafted after architecture review.
