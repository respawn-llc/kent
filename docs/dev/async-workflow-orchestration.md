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
- Parallel joins always wait for all required inputs in v1. Racing/first-success semantics are out of scope.
- Join nodes are non-agent fan-in points. They aggregate inbound transition payloads into a deterministic results collection and then follow their outgoing edge. If synthesis is needed, put an agent node after the join.
- Parallel branches are ordinary workflow nodes that happen to run concurrently. They are not subtasks and do not require a separate child-task model. A task may have multiple active node placements/runs while explicit fan-out is active.
- Orchestrator-workers should not dynamically create workflow nodes or Kanban columns in v1. An orchestrator is an ordinary agent node that may use existing subagent/session infrastructure inside its run or feed statically defined graph branches.
- Agent nodes complete by calling a node-specific completion tool, not by returning natural language. The completion payload chooses a user-defined outgoing transition when the node has more than one outgoing edge and supplies node output fields. Runtime failure, cancellation, and unanswered questions are orchestration states, not model-selected terminal statuses.
- Workflow runs should treat a normal assistant final answer as invalid output. Runtime should append a nudge and continue until the model calls the completion tool, calls `ask_question`, is canceled, or hits a runtime error.
- The model-facing completion control should be a static workflow-only tool, not a CLI command and not a dynamically generated per-node tool schema. Recommended shape: `complete_node` with `transition_id`, optional `commentary`, and `payload` as a flat `map[string]string`. Runtime validates the payload against active task/run/node state. If called outside an active workflow run, it returns an explicit not-in-workflow error.
- `complete_node` is workflow control infrastructure and is available in every workflow run regardless of subagent role tool config.
- User questions use existing `ask_question` tool-call/session infrastructure. A model does not report `needs_user_input` as a completion status; it calls `ask_question`, and the run pauses until answered. V1 should not introduce a separate durable task-question source of truth unless existing session transcript/resume semantics prove insufficient.
- Node output schemas are user-authored but intentionally flat. Fields are strings; arrays, nested objects, and mixed scalar types are out of scope for v1. String-only fields keep UI/query/schema generation tractable while allowing users to stringify richer content when needed.
- Completion tools expose only `transition_id`, never `next_node`. The selected edge derives the target node and transition behavior.
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
- If a workflow references a subagent role that no longer exists in effective config, the node transition blocks with a validation error before scheduling the run. Same-name subagent setting changes are accepted.
- Agents may add, replace, and soft-delete task comments through CLI/API task management, not model-callable comment tools. A skill or reminder should teach workflow agents the CLI. Comments should record author/source agent when available and stay in Builder persistence, not files in the worktree.
- `RunPromptService` should not back workflow nodes. It is a one-shot final-string API, while workflow nodes need durable runs, structured completion, interruption, and resume.
- Existing user goal state should not be reused as workflow autonomy state. Workflow needs a goal-like loop shape, but task/node/run identity must own completion, interruption, and resume semantics.
- Task lifecycle state should derive from node placement/run state rather than a separate task status enum. The task's node placement is the workflow/Kanban state; blocked/running/interrupted/done conditions come from runs and terminal nodes.

## Completion Control Schema

The static `complete_node` tool should have a stable schema:

- `transition_id`: optional string. Runtime requires it when a node has more than one outgoing edge and validates it against valid outgoing edge IDs.
- `commentary`: optional catch-all string field; visible to the user and passed along to the next node by default.
- `payload`: optional flat string map for user-defined fields such as `review_findings`, `verification`, `architecture_notes`, or `merge_notes`.

Prefer `transition_id` over `next_node` because the edge owns approval, context preservation, input/output bindings, and routing semantics. The target node is derived from the selected edge.

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

To be drafted after PRD decisions are explicit.

## Data Model Draft

To be drafted after PRD decisions are explicit.

## Implementation Plan

To be drafted after architecture review.
