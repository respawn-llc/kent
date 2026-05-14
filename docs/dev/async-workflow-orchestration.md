# Async Workflow Orchestration

## Purpose

Design Builder's backend foundation for asynchronous, configurable agent pipelines before frontend implementation. The feature turns Builder from a manually driven terminal coding-agent harness into an orchestrator for project-scoped workflows where tasks move through graph nodes, Kanban statuses, agent workers, review loops, and merge/cleanup stages.

Frontend design is intentionally out of scope for this document except where backend contracts must support later workflow/Kanban UI, question inbox, task views, and status visualization.

## Current Idea

- Users define workflows made of nodes, transition groups, and edges.
- Nodes are visible Kanban/status identity and execution identity.
- A task entering an executable node can start an agent run automatically.
- Agent nodes use configured subagent roles, custom prompts, static workflow completion control, and goal-like autonomous looping.
- The static `complete_node` tool returns a selected transition group plus structured flat-string payload fields. User questions use `ask_question`; runtime failures and cancellations are orchestration states.
- Review nodes can emit findings and move tasks back to implementation; architecture/design nodes can send underspecified work back to design.
- Work should run asynchronously through a durable queue with a global concurrency limit to avoid rate limits.
- Work should reuse Builder's existing project, workspace, worktree, session, runtime, `ask_question`, background process, and server architecture while keeping workflow state separate from user goal state.

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

## Remaining Implementation Risks

- Existing `ask_question` resume must be proven against workflow interruption/restart; if it fails, ask persistence must become durable source of truth.
- `complete_node` needs runtime hook work so terminal tool calls stop workflow node execution cleanly.
- Task-owned worktree creation needs lower-level worktree primitives that do not require an interactive session controller lease.

## Product Decisions

Decisions will be recorded here during the planning interview.

- V1's smallest testable vertical slice is backend/API/CLI first: create a task, auto-run at least one agent node in a worktree, capture structured completion, and move task status. The CLI can be clunky and removable; it exists to test backend behavior before GUI investment.
- `Task` is the primary durable work item. Existing Builder sessions are execution artifacts under tasks, not the task itself. One task may accumulate many sessions through loops, branches, retries, and complex chains.
- Moving a task from backlog to to-do should auto-run through auto nodes until the task reaches a terminal node or blocks on a user question, manual gate, error, capacity limit, or other explicit stop condition.
- Workflow definitions may rely on TOML-configured subagent roles. This creates config drift risk; v1 accepts fail-fast validation rather than inventing a full stable workflow file/schema solution immediately.
- Builder should support the major agentic workflow patterns from the Anthropic article in some form: prompt chaining, routing, parallelization with aggregation, orchestrator-workers, evaluator-optimizer loops, and open-ended autonomous agents.
- Per-edge context preservation must be configurable in v1 with at least three modes: `new_session`, `continue_session`, and `compact_and_continue_session`.
- V1 workflow definitions are SQLite-authoritative and created/edited through backend API plus a minimal CLI. No stable graph file format is required in v1.
- Workflow definitions should be globally reusable. Projects link to workflow definitions rather than copying graph definitions. Workflow validation is project-contextual because subagent roles and workspace config may differ by project.
- A project can link multiple workflows and has one default workflow for task creation.
- V1 does not snapshot/version workflow definitions for existing tasks. Tasks use the current linked workflow definition. Behavior-affecting workflow edits are allowed while tasks exist; UI/API should warn that active tasks may change behavior. Destructive graph deletes are still guarded: deleting workflow graph elements requires no non-terminal tasks to reference them, and deleting the initial/backlog node also requires selecting a replacement initial node.
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
- User questions use existing `ask_question` tool-call/session infrastructure. A model does not report `needs_user_input` as a completion status; it calls `ask_question`, and the run pauses until answered. V1 should not introduce a separate task-question projection. If existing ask infrastructure cannot reliably resume workflow asks, upgrade ask persistence as the source of truth instead of adding a shadow task-question table.
- Node output schemas are user-authored but intentionally flat. Fields are strings; arrays, nested objects, and mixed scalar types are out of scope for v1. String-only fields keep UI/query/schema generation tractable while allowing users to stringify richer content when needed.
- Completion tools expose only `transition_id`, never `next_node`. The selected transition group derives target nodes and transition behavior.
- Every completion tool includes optional `commentary` as a visible, pass-along string escape hatch for content not captured by configured fields.
- Custom completion fields are optional in the generated tool schema. The selected edge may impose payload requirements after `transition_id` is known; if required fields are missing, runtime returns a structured tool error/developer nudge and keeps the same run going.
- Edge approval is a boolean edge property. When approval is required, the source run finishes and the node transition waits before scheduling the target run.
- A task owns one managed worktree by default. Implementation and review nodes reuse it unless later node/edge configuration adds an explicit override.
- Builder creates the task worktree when scheduling the first executable run that needs workspace access. In the default pipeline this coincides with moving the task from backlog into executable work.
- Task worktree branch creation should reuse existing worktree logic. The branch name is the task short ID.
- `continue_session` requires the same subagent role/session contract across source and target nodes. Direct continuation across roles is invalid because it cannot apply the target role contract and invalidates cache assumptions. `new_session` and `compact_and_continue_session` may cross roles because they use a fresh context boundary; compact mode carries a handoff document rather than preserving direct prompt-cache continuity.
- Autonomous node stop limits are not part of v1. Operator cancellation and runtime errors still stop work. Repeated invalid workflow-protocol failures, such as repeated final answers or invalid `complete_node` payloads, may be capped if the guard is cheap to implement.
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
- CLI is an internal backend-testing and agent-control surface, not the primary manual QA surface for users. User manual QA should wait until there is a usable GUI/POC backed by the workflow APIs.
- Workflow API/read-model shapes do not need public stability before Builder 2.0. A parallel POC GUI can consume them, but it should expect breaking changes while workflow orchestration is under active development.
- The POC GUI should sit behind a thin workflow API adapter layer so backend DTO/read-model churn does not spread through UI code.

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

Implementation may split these responsibilities across narrower Go packages if that preserves a pure domain/validation boundary. In particular, if `server/workflow` is used as the pure domain package, persistence, scheduler, and runtime adapters should live in sibling packages instead of adding DB/runtime imports to the domain package.

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

Workflow workers need server-owned runtime activation/resume. They must not fake frontend controller ownership or reuse frontend leases as queue/liveness authority.

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
- Require `complete_node` to be the only tool call in the assistant response. If it is mixed with other tool calls, reject completion and nudge the model to retry cleanly.
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

Pending approvals must store resolved transition group, edge set, and effective edge config snapshots so later graph edits do not change what the user approves.

### Parallelism And Joins

Transition groups model fan-out. A selected transition group can contain multiple edges. Each edge creates a target node placement and, for executable nodes, a queued run. These are still one task, not subtasks.

Fan-out creates a parallel batch. The accepted source transition log is the batch identity; each branch placement records the fan-out edge that created that branch and carries that branch identity until the branch reaches a join or terminal node.

Join nodes are non-agent fan-in points:

- They wait for all branch identities in the parallel batch that reach the join.
- They aggregate inbound payloads into a deterministic results collection keyed by branch identity and source node.
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

Queue claims need fencing/idempotency. Claiming a queued run should use a transactional compare-and-swap from `queued` to `running`, store claim metadata, and make run completion/transition application one SQLite transaction.

### Worktrees

A task owns one managed worktree by default. Builder creates it when the first workspace-requiring executable run is scheduled. Branch name is the task short ID and should reuse existing worktree branch/root collision handling.

Worktree deletion/retargeting must treat non-terminal tasks referencing a managed worktree as blockers.

Workflow worktree creation needs lower-level worktree primitives that create/register task worktrees without requiring a session controller lease or switching an interactive session.

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
- `builder task move <short-id> <node> --placement <placement-id> [--edge <edge-id>] [--payload field=value ...]`
- `builder task approve <task-transition-id>`
- `builder task resume <short-id>`
- `builder task comment add|replace|delete <short-id> ...`

The exact CLI can be clunky; it exists to exercise backend behavior and teach agents task CLI usage.
Commands whose backend semantics land later may initially fail loudly as unsupported placeholders rather than implementing partial behavior. In particular, full manual moves and approvals belong with the approval/manual-move slice, not the first CLI CRUD slice.

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
- `edge_key TEXT NOT NULL`
- `target_node_id TEXT NOT NULL REFERENCES workflow_nodes(id) ON DELETE CASCADE`
- `requires_approval INTEGER NOT NULL DEFAULT 0`
- `context_mode TEXT NOT NULL`
- `input_bindings_json TEXT NOT NULL DEFAULT '{}'`
- `payload_requirements_json TEXT NOT NULL DEFAULT '{}'`
- `sort_order INTEGER NOT NULL DEFAULT 0`
- unique `(transition_group_id, edge_key)`

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
- `project_workflow_link_id TEXT NOT NULL REFERENCES project_workflow_links(id) ON DELETE RESTRICT`
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
- `parallel_batch_transition_id TEXT REFERENCES task_transitions(id) ON DELETE SET NULL`
- `parallel_branch_edge_id TEXT REFERENCES workflow_edges(id) ON DELETE SET NULL`
- `created_at_unix_ms INTEGER NOT NULL`
- `updated_at_unix_ms INTEGER NOT NULL`

`task_runs`

- `id TEXT PRIMARY KEY`
- `task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE`
- `placement_id TEXT NOT NULL REFERENCES task_node_placements(id) ON DELETE CASCADE`
- `node_id TEXT NOT NULL REFERENCES workflow_nodes(id) ON DELETE RESTRICT`
- `session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL`
- `state TEXT NOT NULL`
- `claim_id TEXT NOT NULL DEFAULT ''`
- `claimed_by TEXT NOT NULL DEFAULT ''`
- `claimed_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `state_generation INTEGER NOT NULL DEFAULT 0`
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
- `edge_key TEXT NOT NULL DEFAULT ''`
- `target_node_id TEXT REFERENCES workflow_nodes(id) ON DELETE SET NULL`
- `target_placement_id TEXT REFERENCES task_node_placements(id) ON DELETE SET NULL`
- `state TEXT NOT NULL` (`pending|queued|completed|blocked`)
- `context_mode TEXT NOT NULL DEFAULT ''`
- `requires_approval INTEGER NOT NULL DEFAULT 0`
- `input_bindings_json TEXT NOT NULL DEFAULT '{}'`
- `payload_requirements_json TEXT NOT NULL DEFAULT '{}'`
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
- `task_runs(claim_id)`
- `task_runs(task_id, created_at_unix_ms DESC)`
- `task_transitions(task_id, created_at_unix_ms DESC)`
- `task_transition_edges(task_transition_id, state)`
- `task_comments(task_id, updated_at_unix_ms DESC)`

Schema/domain validation must ensure workflow-scoped references do not cross workflows. Implement this with composite foreign keys where practical, and domain validation otherwise.

### Core Query Shapes

- Load project board: project workflow links, workflow nodes, active task placements, active/waiting/interrupted run summaries.
- Create task: allocate project sequence atomically, create task, create start-node placement.
- Move task manually: validate graph/input/continuation, create transition log, create pending approval state before queue.
- Claim next run: count running runs globally, select queued run ordered by queued time, atomically mark running.
- Complete run: persist transition payload, validate transition group/edges, create target placements/runs or pending approvals, mark source placement completed.
- Join check: use `parallel_batch_transition_id` and `parallel_branch_edge_id` to aggregate one completed branch result per expected fan-out edge.
- Resume interrupted run: validate session/worktree still available, mark queued/running through scheduler, continue existing session.

## Implementation Plan

Follow the execution checklist in `docs/dev/async-workflow-implementation-checklist.md` during implementation. This section defines slice boundaries and completion intent; the checklist is the lower-level handoff tracker with tests, smoke checks, and verification steps.

Use TDD for production implementation. Each slice should leave the repo in a buildable state, with deterministic tests before the next slice begins. Prefer fake provider/model adapters for workflow tests while keeping vertical runtime/tool integration on real runtime code paths; avoid real LLM calls in automated tests.

### Slice 1: Workflow Domain And Graph Validation

Build pure domain types and validation in `server/workflow` without DB or runtime dependencies.

Scope:

- Define workflow definition, node, transition group, edge, output schema, context-preservation mode, task/run/placement/transition identifiers, and validation error types.
- Validate one start node, start-node constraints, terminal sink constraints, no detached islands, every non-terminal can reach a terminal, workflow-scoped references, node-kind constraints, transition group fan-out shape, payload requirement references, output schema field names, and context-preservation mode values.
- Validate project-context dependencies through an injected role resolver so missing subagent roles fail before scheduling.

Completion criteria:

- Unit tests cover the default backlog-agent-done workflow, cycles and self-loops allowed, detached island rejected, unreachable terminal rejected, terminal outgoing edge rejected, missing start rejected, multi-start rejected, bad transition-group edge rejected, invalid payload requirement rejected, invalid context mode rejected, and missing subagent role rejected.
- Validation returns structured errors with stable codes useful for CLI/API display.

### Slice 2: Metadata Schema, Queries, And Store

Add workflow/task persistence to metadata storage before runtime behavior.

Scope:

- Add migrations for project keys/task counters, workflow definitions, nodes, transition groups, edges, project workflow links, tasks, placements, runs, transitions, transition edge snapshots, and task comments.
- Add sqlc queries and store methods for workflow CRUD, project workflow linking/default selection, task short-ID allocation, task creation at start node, placement/run state updates, transition logging, task views, comments, and guarded destructive edits.
- Store edge snapshots for pending approvals so later graph edits do not change approval meaning.

Completion criteria:

- Migrations apply cleanly to empty DB and an existing metadata DB fixture.
- Store tests prove project key backfill/collision handling, atomic task sequence allocation, one default workflow per project, task creation creates exactly one start placement, comment add/replace/soft-delete behavior, transition snapshots survive graph edits, and guarded graph deletes reject non-terminal task references.
- `./scripts/test.sh ./server/metadata/... ./server/workflow/...` passes.

### Slice 3: API Contracts, Service Layer, And Read Models

Expose typed backend API/read-model shapes needed by CLI and future UI/POC GUI without runtime execution. These are implementation contracts, not public compatibility guarantees before Builder 2.0.

Scope:

- Add workflow/task DTOs under `shared/serverapi`, route-shaped service interfaces in `shared/servicecontract`, loopback/remote client support, transport routes, and server service composition.
- Add `server/workflowview` read models for project boards, task detail, transition history, run summaries, comments, and validation results.
- Keep API validation strict at boundaries while domain validation remains reusable by CLI and services.

Completion criteria:

- Contract tests cover request validation, project default workflow resolution, stable board ordering by workflow node order, task detail with active placements/runs, transition history ordering, and deleted comments hidden by default.
- Loopback client and remote route tests exercise same service methods.
- UI/POC GUI can obtain board and task detail views without reading session transcripts or `events.jsonl`.
- POC GUI integration goes through a thin adapter layer because backend workflow DTOs/read models can break before Builder 2.0.

### Slice 4: Minimal Workflow And Task CLI

Add clunky but complete backend-testing commands for CRUD/read/comment/validation before automation. These commands are for engineering validation and agent usage, not for Nikita-led manual QA. Full manual moves and approvals land later; early `task move`/`task approve` commands may be explicit unsupported placeholders.

Scope:

- Implement minimal `builder workflow` commands for create, node add, edge add, link, validate, and inspect.
- Implement minimal `builder task` commands for create, list, show, resume placeholder, comment add/replace/delete, and optional unsupported placeholders for move/approve.
- Prefer stable IDs in output where later commands need row identifiers; approval uses task transition row ID, not user-defined transition ID.

Completion criteria:

- CLI tests or command-level tests create a workflow, link it to a project, create a task, list/show board/task views, add/replace/delete comments, validate graph errors, and verify manual move/approval placeholders fail loudly until their implementation slice.
- CLI output includes enough IDs for humans and agents to continue from terminal logs.
- A no-LLM coding-agent smoke check can be run against a temporary persistence root: create a real workflow graph, create tasks, inspect board/task views, verify comments plus IDs behave as expected, and verify manual move/approval commands fail loudly until their implementation slice.

### Slice 5: Task-Owned Worktree Primitive

Create lower-level worktree capability for task automation, separate from interactive controller leases.

Scope:

- Add service method to create/register a managed task worktree for a task when first workspace-requiring executable run is scheduled.
- Reuse existing branch/root collision handling and physical worktree operations.
- Enforce non-terminal task blockers for managed worktree deletion/retargeting.

Completion criteria:

- Worktree tests with temp repos prove branch name defaults to task short ID, repeated ensure calls are idempotent, collisions get deterministic safe names, and no interactive session/controller lease is required.
- Blocking tests prove non-terminal tasks prevent managed worktree deletion/retargeting.

### Slice 6: Durable Queue, Claims, And Recovery

Implement scheduling state and recovery without real model execution first.

Scope:

- Add queue service that claims `queued` runs with transactional compare-and-swap, claim IDs, claim metadata, global concurrency cap from config, and state generation/fencing.
- Add startup reconciliation for queued, running, waiting-for-question, interrupted, completed, failed, canceled, and pending approvals.
- Make completion/transition application one transaction so source run, source placement, transition log, target placements, and queued target runs commit atomically.

Completion criteria:

- Concurrent claim tests prove one run is claimed once and global concurrency is respected.
- Fencing tests prove stale claim completion cannot mutate a run after a newer generation.
- Recovery tests prove stale running runs become interrupted, queued runs stay queued, pending approvals stay pending, and interrupted runs are never auto-retried.
- Transaction tests prove failed transition application leaves no half-created target placements/runs.

### Slice 7: Runtime Completion Hook And `complete_node`

Add workflow-aware runtime control before running full workflow nodes.

Scope:

- Register static `complete_node` as workflow-control infrastructure available in workflow runs regardless of subagent role tool config.
- Carry active workflow run context into tool execution.
- Add preflight that rejects assistant responses where `complete_node` is mixed with any other tool call.
- Add terminal signal from accepted `complete_node` so the step loop persists the tool result and stops without another model turn.
- Treat normal assistant final answers as invalid workflow output and append a developer nudge.

Completion criteria:

- Runtime/tool tests prove `complete_node` outside workflow returns explicit error, mixed tool calls are rejected before side effects, missing `transition_id` is rejected only when multiple transition groups exist, invalid payload field and missing edge-required payload return structured tool errors, valid completion stops the run, and final-answer-only workflow output nudges and continues.
- Existing non-workflow tool execution behavior remains unchanged.

### Slice 8: Single-Agent `new_session` Vertical Slice

Connect persistence, queue, task worktree, runtime, and completion for the smallest useful async workflow.

Scope:

- Execute one agent node using `new_session`: task creation, explicit start/schedule action from backlog into executable node, worktree ensure, queued run claim, session creation, node prompt injection, fake/model runtime execution through real runtime/tool handling, `complete_node`, transition application, and terminal placement.
- Use fake provider/model adapters in tests while still exercising real runtime/tool handling for vertical completion behavior; reserve real provider use for manual QA later.

Completion criteria:

- Integration test creates workflow `backlog(start) -> agent -> done(terminal)`, creates a task, starts automation, completes via fake `complete_node`, and observes task placement in terminal node with stored payload/commentary.
- CLI can create/show the same flow against embedded server state.
- No test loads full `events.jsonl`.
- Real-provider smoke testing is optional and requires explicit manual approval before spending provider credits.

### Slice 9: Question Pause And Resume Proof

Prove existing `ask_question` infrastructure is sufficient for workflow runs, or stop and design the persistence upgrade.

Scope:

- Run a workflow node that calls `ask_question`, transitions run state to `waiting_for_question`, accepts an answer, and resumes same run/session.
- Test restart/reconciliation behavior around a pending question.
- If existing ask persistence cannot support this reliably, upgrade ask persistence as the source of truth before continuing; do not add a shadow task-question table.

Completion criteria:

- Tests prove question answer resumes the same workflow run/session and can complete with `complete_node`.
- Restart test proves pending ask can be rehydrated or run becomes interrupted with actionable resume path.
- Any required ask persistence upgrade has focused tests and keeps task question views derived from source-of-truth ask state.

### Slice 10: Context-Preservation Modes

Implement edge context semantics after single-session execution works.

Scope:

- Implement `new_session`, `continue_session`, and `compact_and_continue_session`.
- Enforce direct `continue_session` only when source and target use the same subagent/session contract.
- Allow role changes for `new_session` and `compact_and_continue_session` because they use fresh context boundary or compacted continuation input.

Completion criteria:

- Tests prove same-role `continue_session` appends to existing session, cross-role `continue_session` is rejected before queueing, `new_session` creates separate session across roles, and compact mode creates a compacted continuation input before target execution.
- Prompt/cache-sensitive behavior does not mutate prior transcript history.

### Slice 11: Approvals And Manual Moves

Implement human-controlled task movement and edge approval semantics.

Scope:

- Apply edge `requires_approval` by storing pending transition/edge snapshots after source run completion and queueing targets only after approval.
- Implement manual moves through real edge/transition metadata or explicit equivalent metadata.
- Require explicit user approval before queueing automation from manual moves into executable targets.
- Support backward moves that reuse stored payloads when validation allows.

Completion criteria:

- Tests prove approval by task transition row ID queues stored target edge snapshots, later graph edits do not change pending approval behavior, forward manual move validates provided payload, backward manual move can reuse stored payload, missing required payload is rejected, continuation without valid source session is rejected, and executable manual target pauses before queue.

### Slice 12: Fan-Out, Parallel Branches, And Joins

Add explicit parallel branch support after serial execution is stable.

Scope:

- Apply multi-edge transition groups by creating a parallel batch and one branch placement per edge.
- Carry `parallel_batch_transition_id` and `parallel_branch_edge_id` until branch reaches join or terminal.
- Implement join nodes that wait for all required branch identities, aggregate deterministic branch results, and continue through their outgoing transition group.

Completion criteria:

- Tests prove fan-out creates multiple active placements under one task, branches can complete in any order, join waits until all expected branches arrive, duplicate branch arrivals are rejected or ignored idempotently, aggregate result ordering is deterministic, and next node receives bound aggregate input.

### Slice 13: Recovery, Observability, And Hardening

Close operational gaps before UI work.

Scope:

- Add resume/cancel/fail commands and service methods with clear state transitions.
- Add structured logs/diagnostics around scheduler claims, run completion, transition application, validation blockers, and workflow runtime errors.
- Add role-drift validation at scheduling/resume time and actionable error surfaces in CLI/API.
- Update `docs/dev/decisions.md` only after decisions are stable and without staging unrelated user edits.

Completion criteria:

- Restart tests cover queued, running, interrupted, waiting-for-question, and pending approval tasks.
- CLI/API surfaces validation and orchestration errors with stable codes and actionable messages.
- `./scripts/test.sh ./server/workflow/... ./server/workflowruntime/... ./server/workflowview/... ./server/metadata/...` and `./scripts/build.sh --output ./bin/builder` pass once production code exists.

### Suggested First Coding Milestone

First implementation milestone should end after Slice 4:

- It gives durable workflow/task CRUD, validation, board/task read models, and comments without runtime risk.
- Builder can perform first internal no-LLM smoke check at this point by creating a real graph/task through CLI/API and validating status movement, comments, IDs, and read models.
- This milestone needs no real LLM calls.
- Nikita-led manual QA should be deferred until a usable GUI/POC exists on top of these APIs.

After Slice 4, continue runtime work with automated fake-model/fake-runtime tests through Slice 8. Slice 8 proves the product's core async promise: one task, one worktree, one agent node, structured completion, and terminal status. Real-agent QA should remain an explicit manual approval step because it spends provider credits and can be flaky for reasons unrelated to orchestration correctness.
