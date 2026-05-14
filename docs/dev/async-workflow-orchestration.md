# Async Workflow Orchestration

## Purpose

Design Builder's backend foundation for asynchronous, configurable agent pipelines before frontend implementation. The feature turns Builder from a manually driven terminal coding-agent harness into an orchestrator for project-scoped workflows where tasks move through graph nodes, Kanban statuses, agent workers, review loops, and merge/cleanup stages.

Frontend design is intentionally out of scope for this document except where backend contracts must support later workflow/Kanban UI, question inbox, task views, and status visualization.

## Current Idea

- Users define workflows made of nodes, transition groups, and edges.
- Nodes are visible Kanban/status identity and execution identity.
- A task entering an executable node can start an agent run automatically.
- Agent nodes use configured subagent roles, custom prompts, workflow completion control, and goal-like autonomous looping.
- Workflow completion returns a selected transition group plus structured top-level string output fields. It can use structured output or dynamic `complete_node` tool mode. User questions use `ask_question`; runtime failures and cancellations are orchestration outcomes.
- Review nodes can emit findings and move tasks back to implementation; architecture/design nodes can send underspecified work back to design.
- Work should run asynchronously through a scheduler rebuilt from durable task/run intent with a global concurrency limit to avoid rate limits.
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

- Existing `ask_question` resume must be proven before scheduler recovery depends on pending asks. If it fails, ask persistence must become durable source of truth before workflow question/resume work continues.
- Workflow completion needs runtime hook work so structured output or terminal tool calls stop workflow node execution cleanly.
- Task-owned worktree creation needs lower-level worktree primitives that do not require an interactive session controller lease.

## Product Decisions

Decisions will be recorded here during the planning interview.

- V1's smallest testable vertical slice is backend/API/CLI first: create a task, auto-run at least one agent node in a worktree, capture structured completion, and move task status. The CLI can be clunky and removable; it exists to test backend behavior before GUI investment.
- `Task` is the primary durable work item. Existing Builder sessions are execution artifacts under tasks, not the task itself. One task may accumulate many sessions through loops, branches, retries, and complex chains.
- Task creation creates a durable task at the workflow start node. Automation starts only through an explicit task-start operation that applies the start node's single-edge outgoing transition group and then auto-runs through automatic nodes until the task reaches a terminal node or blocks on a user question, manual gate, error, capacity limit, or other explicit stop condition.
- Workflow definitions may rely on TOML-configured subagent roles. This creates config drift risk; v1 accepts fail-fast validation rather than inventing a full stable workflow file/schema solution immediately.
- Builder should support the major agentic workflow patterns from the Anthropic article in some form: prompt chaining, routing, parallelization with aggregation, orchestrator-workers, evaluator-optimizer loops, and open-ended autonomous agents.
- Per-edge context preservation must be configurable in v1 with at least three modes: `new_session`, `continue_session`, and `compact_and_continue_session`.
- V1 workflow definitions are SQLite-authoritative and created/edited through backend API plus a minimal CLI. No stable graph file format is required in v1.
- Workflow definitions should be globally reusable. Projects link to workflow definitions rather than copying graph definitions. Workflow validation is project-contextual because subagent roles and workspace config may differ by project.
- Workflow definitions may be saved, linked, and made a project default while graph/project-context validation fails. This supports GUI draft editing and lets validation surfaces accumulate all safe actionable errors. Task creation, task start, and runtime scheduling reject invalid workflows instead of creating invalid work.
- A project can link multiple workflows and has one default workflow for task creation. Invalid default workflows are allowed, but task creation against an invalid default fails with accumulated validation errors.
- V1 does not snapshot/version workflow definitions for existing tasks. Workflows still carry a monotonic graph revision for traceability. Tasks, runs, transitions, and approval/edge snapshots store the revision they observed; anything snapshotted keeps using that snapshot, and anything not snapshotted uses the current workflow graph/config at execution time. Behavior-affecting workflow edits are allowed while tasks exist; UI/API should warn that active tasks may change behavior. Destructive graph deletes are still guarded: behavior-affecting removal is blocked by non-terminal task references, and physical row deletion is blocked by any task history reference. When only terminal task history references a graph element, UI/API may archive or hide it rather than physically deleting it.
- Node config and edge config are distinct. Nodes configure agent runs: subagent role, prompt, output schema, limits, and run stop conditions. Edges configure transitions: next node, human approval/manual interaction, context preservation, input bindings, routing, and join/aggregation behavior.
- Subagent role is the executable node's assignee. There is no separate assignee field. UI can display subagent roles as assignees for convenience.
- Workflow nodes select existing subagent roles only; no per-node model/provider/tool/auth overrides. Subagent roles define agent identity fully.
- V1 should keep node identity equal to visible Kanban column/status identity. Multiple executable nodes sharing one column creates ambiguous manual moves and unclear debugging. Later UI can add display grouping if needed.
- Workflows can contain executable agent nodes, terminal nodes, and join nodes. Approval remains an edge property, not a separate manual-node requirement.
- Workflow creation should auto-create default `backlog` and `done` nodes as ordinary editable nodes. This avoids hardcoded unmapped statuses while keeping setup ergonomic.
- V1 workflows have exactly one start node. The start node is non-executable and has no inputs. For task creation/automation validation, the start node must have exactly one outgoing transition group, and that group must contain exactly one edge targeting an agent node so `task start` is unambiguous and always seeds a worktree-backed executable run. Multiple start nodes are expected later and should not be made architecturally difficult.
- Terminal nodes are strict sinks. Manual reopen/rework is a user override execution, not a durable graph transition or graph mutation.
- Workflow validation has context modes. Draft validation reports semantic errors but does not block saving, linking, or default selection; storage invariants such as valid IDs, valid references, valid enum values, unique keys, and exactly one start node are still enforced. Task creation and execution validation are project-contextual, accumulate all safe actionable errors, and reject invalid graph/role/input configurations. Execution-valid graphs reject detached islands: every node must be reachable from the start node, every non-terminal node must be able to reach a terminal node, self-loops/cycles are allowed outside restricted fan-out branch paths, and terminal nodes cannot auto-run.
- Parallel joins always wait for all required inputs in v1. Racing/first-success semantics are out of scope.
- Join nodes are non-agent fan-in points. They aggregate inbound transition output values into a deterministic results collection and then follow their outgoing edge. If synthesis is needed, put an agent node after the join.
- Parallel branches are ordinary workflow nodes that happen to run concurrently. They are not subtasks and do not require a separate child-task model. A task may have multiple active node placements/runs while explicit fan-out is active.
- Fan-out uses transition groups. `transition_id` selects a transition group; a transition group can contain one edge or multiple edges. Multiple edges in the group create parallel node placements/runs that later converge at a statically derived join. V1 fan-out topology must have exactly one unambiguous nearest common join reachable from every branch, with no branch terminal, nested fan-out, or cycle before that join.
- Orchestrator-workers should not dynamically create workflow nodes or Kanban columns in v1. An orchestrator is an ordinary agent node that may use existing subagent/session infrastructure inside its run or feed statically defined graph branches.
- Agent nodes complete by producing a structured workflow completion, not by returning natural language. The completion chooses a user-defined outgoing transition when the node has more than one outgoing transition group and supplies node output fields. Runtime failure, cancellation, and unanswered questions are orchestration outcomes, not model-selected terminal statuses.
- Workflow nodes support two model-facing completion modes: structured output and dynamic `complete_node` tool. A temporary global config setting selects `auto`, `structured_output`, or `tool`; there is no workflow/node override. `auto` chooses structured output when provider capabilities support it and dynamic tool mode otherwise. Forced `structured_output` fails fast with an actionable error when unsupported. Forced `tool` always uses dynamic tool mode.
- Workflow runs should treat a normal assistant final answer as invalid output. Runtime should append a nudge and continue until the model produces valid workflow completion output, calls `ask_question`, is interrupted, is canceled, hits the protocol cap, or hits a runtime error.
- `complete_node` is workflow control infrastructure and is available in tool completion mode regardless of subagent role tool config.
- User questions use existing `ask_question` tool-call/session infrastructure. A model does not report `needs_user_input` as a completion status; it calls `ask_question`, and the run pauses until answered. V1 should not introduce a separate task-question projection. If existing ask infrastructure cannot reliably resume workflow asks, upgrade ask persistence as the source of truth instead of adding a shadow task-question table.
- Node output schemas are user-authored but intentionally flat. Fields have stable names, user-facing descriptions, and string values; arrays, nested objects, and mixed scalar types are out of scope for v1. String-only fields keep UI/query/schema generation tractable while allowing users to stringify richer content when needed.
- Model-facing completion schemas expose `transition_id`, optional `commentary`, and custom node output fields as top-level properties. They do not hide user fields inside a generic nested object.
- Completion schemas expose only `transition_id`, never `next_node`. The selected transition group derives target nodes and transition behavior.
- Every completion schema includes optional `commentary` as a visible, pass-along string escape hatch for content not captured by configured fields.
- Custom completion fields are optional in the generated schema. The selected edge may impose output requirements after `transition_id` is known; required fields must be present as trimmed non-empty strings. If required fields are missing or empty, runtime returns a structured validation nudge and keeps the same run going.
- Workflow schema metadata, completion output values, commentary, and task comments have fixed pragmatic size limits in v1 so model output cannot create unbounded DB/provider payloads: output field name `<= 64` chars, output field description `<= 1000` chars, output value `<= 64 KiB`, commentary `<= 64 KiB`, and task comment body `<= 256 KiB`.
- Edge approval is a boolean edge property. When approval is required, the source run finishes and the node transition waits before scheduling the target run. If any edge in a selected transition group requires approval, the whole transition group waits for one approval before any target placement/run starts.
- A task owns one managed worktree by default. All executable agent nodes require and reuse it.
- Builder creates the task worktree when `task start` requests the first executable run. In the default pipeline this coincides with moving the task from backlog into executable work.
- Task worktree branch creation should reuse existing worktree logic. The branch name is the task short ID.
- Direct `continue_session` requires the same subagent role name across source and target nodes. Same-role config drift does not block continuation: the immutable persisted session setup remains authoritative for snapshotted model/provider/tool/system-prompt fields, while non-snapshotted workflow inputs use current graph/config. Direct continuation across roles is invalid because it cannot apply the target role contract and invalidates cache assumptions. `new_session` and `compact_and_continue_session` may cross roles because they use a fresh context boundary; compact mode carries a handoff document rather than preserving direct prompt-cache continuity.
- Workflow runtime must enforce mandatory cheap protocol caps. Repeated normal final answers or invalid workflow-completion attempts stop the run as `interrupted` with actionable reason metadata after `[workflow].max_final_answer_violations = 3` or `[workflow].max_invalid_completion_attempts = 5`. No wall-clock cap is required for v1.
- The scheduler is durable enough to rebuild from SQLite but should not store durable run states for pending scheduler work or active runtime ownership. Runnable work is derived from active executable placements with approved automation intent and no terminal run outcome. Active execution is derived from live runtime/scheduler state.
- The scheduler does not own runtime leases. Runtime leases remain execution-control state, similar to current client/frontend leases, not durable scheduling authority.
- If execution stops mid-node, mark the run `interrupted`, preserve session transcript and dirty worktree state, and require human resume. Resume continues the interrupted session/run instead of rerunning the whole node from scratch.
- V1 has no automatic retry. Runtime failures, user cancellation of active work, crashes, model/runtime interruptions, and fixable scheduling validation blockers converge on an interrupted outcome with reason metadata and explicit human resume. `failed` is reserved for unrecoverable corrupted orchestration state.
- Concurrency limit is global only and configured in `config.toml`.
- A task may have multiple active node placements/runs only when the workflow graph explicitly fans out into parallel branches; otherwise task execution is single-active-run.
- Terminal-node placements remain active sink placements. Task/board read models infer done from an active placement whose node kind is terminal.
- Task cancellation is task-level metadata, not a synthetic terminal placement. Canceling a task records cancellation timestamp/reason, interrupts active runs with a cancel reason, and suppresses scheduler automation for that task.
- Task required fields are title, short ID, and body. Task metadata should be designed for future import/export and may include a source URL for imported external work.
- Task short IDs are project-scoped sequential keys with a project key prefix, e.g. `BLD-123`. Project keys are uppercase, globally unique within a persistence root, 2-8 characters, and match `^[A-Z][A-Z0-9]{1,7}$`. Project creation should choose the key explicitly; default suggestion can use the first three letters of the project name.
- Existing projects without a project key should get one from the default project-name logic when task support is initialized, with collision handling. Project keys are immutable after a project has tasks; existing task short IDs keep their historical key forever.
- If a workflow references a subagent role that no longer exists in effective config, task creation/start/scheduling blocks with validation errors before starting the run. Same-name subagent setting changes are accepted for fresh sessions and do not invalidate same-role direct continuation, which keeps the persisted session setup.
- Agents may add, replace, and soft-delete task comments through CLI/API task management, not model-callable comment tools. A skill or reminder should teach workflow agents the CLI. Comments should record author/source agent when available and stay in Builder persistence, not files in the worktree. Task comments are not injected automatically into agent context; agents read them through CLI when needed. The first CLI milestone does not require machine-readable JSON output or workflow-specific environment variables.
- Workflow runtime should reuse headless/`builder run` infrastructure where it fits: session launch planning, runtime wiring, logging, progress/event publication, subagent role handling, and mode prompts. `RunPromptService.RunPrompt` itself should not be workflow completion authority because it is a one-shot final-string API, while workflow nodes need durable run identity, structured completion, interruption, and resume.
- Existing user goal state should not be reused as workflow autonomy state. Workflow needs a goal-like loop shape, but task/node/run identity must own completion, interruption, and resume semantics.
- Task lifecycle state should derive from node placement/run state plus task-level cancellation metadata rather than a separate task status enum. The task's node placement is the workflow/Kanban state; active/interrupted/done conditions come from runs and terminal nodes.
- CLI is an internal backend-testing and agent-control surface, not the primary manual QA surface for users. User manual QA should wait until there is a usable GUI/POC backed by the workflow APIs.
- Workflow API/read-model shapes do not need public stability before Builder 2.0. A parallel POC GUI can consume them, but it should expect breaking changes while workflow orchestration is under active development.
- The POC GUI should sit behind a thin workflow API adapter layer so backend DTO/read-model churn does not spread through UI code.

## Completion Control Schema

Workflow completion is a runtime contract with two model-facing encodings:

- Structured output mode: Builder sets `llm.StructuredOutput` for the node run.
- Tool mode: Builder exposes a dynamic `complete_node` tool schema for the node run.

Both modes use the same logical schema:

- `transition_id`: optional string. Runtime requires it when a node has more than one outgoing transition group and validates it against valid transition IDs.
- `commentary`: optional catch-all string field; visible to the user and passed along to the next node by default.
- User-authored node output fields as top-level string properties, each with a stable field name and description.

Example logical schema:

```json
{
  "transition_id": "changes_requested",
  "commentary": "Review found one blocking issue.",
  "review_findings": "The retry path drops the original error.",
  "verification": "Unit tests were not run because this is a design review node."
}
```

Prefer `transition_id` over `next_node` because transition groups and edges own approval, context preservation, input bindings, fan-out, and routing semantics. Target nodes are derived from the selected transition group.

Node-specific field guidance belongs in both the generated schema descriptions and workflow-mode developer prompt. Selected-edge validation checks output requirements. Example: a review node can define `review_findings` as an available output field, while only the `changes_requested` edge requires it.

Dynamic structured-output and dynamic tool schemas can affect prompt-cache continuity if workflow configuration changes mid-run. This is accepted because mid-run workflow graph/schema edits should be rare, and the clearer model-facing schema is more important than preserving a static generic payload shape.

## Graph Revision Policy

`workflows.graph_revision` increments on every create, update, archive, restore, or delete operation affecting `workflow_nodes`, `workflow_transition_groups`, or `workflow_edges`, including node prompts, roles, output fields, display/order metadata, edge context modes, input bindings, output requirements, approval flags, and transition display metadata. Workflow name/description edits do not need to bump graph revision unless those values are included in model-facing workflow prompts.

Run start snapshots the effective model-facing node contract: node key/display/kind, subagent role name, prompt template/render inputs, output field schema, outgoing transition groups, edge keys/targets, context modes, approval flags, input bindings, output requirements, completion schema, and workflow revision. The running model completes against that run-start snapshot even if the workflow graph changes mid-run. Transition application persists transition-edge snapshots from that run-start snapshot; target node execution uses current graph/config when a target run is later scheduled unless that target run already has its own run-start snapshot.

Graph revision mismatch is therefore traceability and warning data, not full versioning. Resume of an interrupted run continues with the run-start snapshot. New runs created after graph edits use the current graph/config after project-context validation.

## Identifier And Key Rules

Use opaque server-generated row IDs for internal primary keys. Use human/model-facing keys where workflows need stable names:

- `node_key`, `transition_id`, `edge_key`, node output field names, and bound input names use `^[a-z][a-z0-9_]{0,63}$`.
- Project keys use `^[A-Z][A-Z0-9]{1,7}$`.
- Task short IDs are immutable strings formed as `<PROJECT_KEY>-<project_task_seq>`.
- Display names are user-facing labels and are not identifiers. Store/service validation keeps display names trimmed non-empty and `<= 120` chars, but workflow references must not depend on display names.

### Workflow Mode Prompting

Workflow node runs need dedicated developer instructions, similar to headless/subagent mode prompts. Add a prompt source such as `prompts/workflow_mode_prompt.md` and inject it only for workflow runs.

Injection point: `server/workflowruntime` should use the same session launch/runtime wiring path extracted from `builder run`/headless mode. Inject `workflow_mode_prompt.md` as a developer-role mode prompt during runtime preparation, after the generic headless/subagent mode context is established and before the workflow node prompt is submitted. Scheduler and CLI code must not assemble duplicate workflow-mode prompts.

The prompt must explain:

- task identity: task short ID, title, body, current project/worktree, and current node;
- agent identity: the subagent role is the node assignee;
- workflow mechanics: complete the current node by producing workflow completion output, not by writing a normal final answer;
- completion mode: either return structured JSON matching the active schema or call `complete_node`, depending on runtime mode;
- transition mechanics: choose `transition_id`, never a raw next node;
- output fields: fill top-level custom fields according to their descriptions;
- validation loop: if Builder rejects completion output, fix the output and continue the same run;
- questions: use `ask_question` only for user-blocking ambiguity;
- task comments: use workflow task CLI/API when comments are needed; comments are not automatically injected;
- interruptions/resume: preserve work and continue from current task/run context;
- context-preservation mode: respect whether this run is new, continued, or compacted from a prior run;
- ordinary final answers are invalid workflow protocol unless the runtime explicitly exits non-workflow mode.

## Input Binding Direction

Edges use typed input bindings from source fields to target input names. Target node prompts can reference bound input names with simple template placeholders such as `{{review_comments}}`.

Canonical binding shape:

```json
{
  "review_comments": {
    "source": "transition_output",
    "field": "review_findings"
  },
  "task_title": {
    "source": "task",
    "field": "title"
  }
}
```

Allowed binding sources:

- `task`: `short_id`, `title`, `body`, `source_url`;
- `transition_output`: `commentary` plus configured source-node output field names;
- `join`: deterministic join aggregate values available after a join node.

Template placeholders use exact `{{input_name}}` syntax, with no expressions, conditionals, loops, or nested templates. Validation rejects unknown placeholders, invalid binding target names, and bindings that reference unknown source fields. Unused bindings are allowed so read models and future prompts can show resolved context without forcing prompt usage.

## Manual Move Requirements

Manual moves are node transitions initiated by the user instead of the source node's completion tool:

- A manual move must use an edge or synthesize equivalent edge input metadata explicitly.
- Moving backward can reuse the latest stored transition output values and task metadata from prior completed runs.
- Moving to an executable node should pause before automation starts and require explicit user approval to start from the manually selected node.
- If the selected transition context-preservation mode requires continuing a session and no valid source session exists, the move is rejected. The user must choose a transition that can use `new_session` or provide a valid continuation source.
- Manual move implementation needs regression tests for forward moves with provided output values, backward moves reusing existing output values, rejection when required output is absent, rejection when continuation is required but unavailable, and approval-before-automation behavior.

## Audit Direction

Keep normalized rows for tasks/runs/transitions/comments and durable transition logs for debugging and UI history. Do not design a full event-sourced task system for v1.

## Domain Vocabulary

See `docs/dev/TERMINOLOGY.md`.

## Backend Architecture Draft

### Composition

Add a server-owned workflow orchestration layer above sessions/runtimes:

- `server/workflow`: pure domain types, graph validation, validation contexts, state machines, and domain errors. No DB, runtime, CLI, or transport imports.
- `server/workflowstore`: metadata-store adapter and typed transactional persistence methods.
- `server/workflowsvc`: application/use-case orchestration for workflow/task CRUD, task creation, task start, comments, approvals, manual moves, validation, cancellation, and resume.
- `server/workflowscheduler`: runnable derivation, global concurrency, startup reconciliation, worker ownership, and scheduler lifecycle.
- `server/workflowruntime`: execution adapter that prepares sessions, starts/resumes node runs, injects node prompts, handles workflow completion, and applies context-preservation modes.
- `server/workflowview`: read-model service for project boards, task detail, run/transition history, and CLI-friendly views.
- `shared/serverapi/workflow.go`: serializable request/response DTOs and validation for workflow/task APIs.
- `shared/servicecontract`: workflow service interfaces following existing route-shaped contract pattern.
- `shared/client`: loopback/remote workflow clients.
- `server/transport`: RPC routes for workflow/task operations.
- `cli/builder`: minimal `workflow` and `task` commands for backend testing and agent CLI usage.

`server/core` should compose workflow services from the metadata store, runtime registry, session runtime service, worktree service, auth manager, and config. The workflow scheduler starts with the server process and stops with core shutdown.

These package names are the implementation default. If implementation recon discovers a concrete conflict, update this spec, checklist, and decisions before coding around it.

### Runtime Model

Workflow runtime is not `RunPromptService.RunPrompt`, because that service returns one final assistant string as the user-visible result. Workflow runtime needs durable run identity, structured completion, interruption, resume, and scheduler integration. Reuse lower-level runtime/session pieces and extract common headless pieces where useful:

- Session planning from `server/launch` and subagent role resolution.
- Runtime activation/control from `server/sessionruntime` and `server/runtimecontrol`.
- Step execution, compaction, queued user-message flushing, structured output, tool execution, and transcript persistence from `server/runtime`.
- Worktree creation/switching from `server/worktree`.
- Existing `ask_question` tool/session flow.
- `builder run`/headless infrastructure for session launch, runtime wiring, logging, progress publication, subagent role overrides, and mode-prompt injection.

Do not reuse user goal state as workflow state. Workflow autonomy uses a goal-like loop shape:

1. Prepare workflow-mode developer instructions and node prompt from task, node config, edge input bindings, comments accessible through CLI, and any transition output values.
2. Start or resume a session according to context-preservation mode.
3. Select completion mode from temporary global config: `auto`, `structured_output`, or `tool`. There is no workflow/node override; `auto` chooses structured output when provider capabilities support it and dynamic tool mode otherwise.
4. Run model turns until one of: valid structured output, valid `complete_node`, `ask_question`, interruption, protocol cap, or runtime error.
5. Treat normal assistant final answers as invalid output, append a developer nudge, increment the final-answer violation counter, and interrupt with actionable reason metadata after the cap.
6. On accepted workflow completion, persist transition output values and stop the node run without sending another model turn.

Workflow workers need server-owned runtime activation/resume. They must not fake frontend controller ownership or reuse frontend leases as scheduling/liveness authority.

### Completion Runtime

`complete_node` is workflow-control infrastructure exposed only when a session is executing a workflow node in tool completion mode. It is available regardless of subagent role tool config.

Dynamic tool schema:

```json
{
  "transition_id": "string",
  "commentary": "string",
  "user_defined_field_name": "string"
}
```

Runtime validation:

- If not in a workflow run, return a tool error.
- Require exactly one `complete_node` tool call and no other tool calls in the assistant response. If `complete_node` is duplicated or mixed with other tool calls, reject completion during tool-call preflight before any side-effecting tool executes, then nudge the model to retry cleanly.
- If multiple transition groups are available, require `transition_id`.
- Validate `transition_id` against source node transition groups.
- Validate field names against node output schema.
- Validate output values are strings and do not exceed configured/default size limits.
- Validate selected-edge output requirements after transition group selection. Required output fields must be present and trimmed non-empty.
- On validation failure, return structured validation errors and append a developer nudge; keep the run active until the invalid-completion cap is reached.
- On repeated validation failure past the cap, interrupt the run with actionable reason metadata.
- On success, persist transition log, mark source run completed, apply approval/scheduler/fan-out/join rules.

Structured output mode performs the same validation against the assistant's structured JSON response. Tool mode needs a workflow completion signal so tool execution can terminate the node run immediately after persisting the tool result.

### Node Transitions

Automatic node transitions come from accepted workflow completion outputs. Manual moves are user override executions with stricter validation:

- They must choose a real edge/transition group or provide equivalent edge input metadata.
- They can reuse stored output values from prior completed runs when moving backward.
- They pause before automation and require explicit user approval to start the target run.
- They reject continuation modes when no valid source session exists.

Edge approvals persist as pending transition logs. Approval means: approve selected transition output from a specific completed run/edge. After approval, Builder schedules target placements/runs from the stored output values.

If any edge in a selected transition group requires approval, the whole transition group waits before any target placement/run starts.

Pending approvals must store resolved transition group, edge set, graph revision, source node display/key snapshot, transition display snapshot, target node display/key/kind snapshot, and effective edge config snapshots so later graph edits do not change what the user approves.

Every applied transition, not only pending approvals, stores transition-edge snapshot rows. Fan-out join readiness uses those snapshot rows from the accepted source transition as the expected edge set, not the live workflow graph after later edits.

### Parallelism And Joins

Transition groups model fan-out. A selected transition group can contain multiple edges. Each edge creates a target node placement and, for executable nodes, automation intent that the scheduler can start. These are still one task, not subtasks.

Fan-out creates a parallel batch. The accepted source transition log is the batch identity; each branch placement records the fan-out edge snapshot that created that branch and carries that branch identity until the branch reaches the derived join. Runtime expected wait set is the persisted transition-edge snapshot rows from that accepted transition.

Join nodes are non-agent fan-in points:

- At validation time, every multi-edge transition group must have exactly one unambiguous nearest common join reachable from every branch target.
- Branch paths before that join may not terminate, enter nested fan-out, or contain cycles. Ambiguous/complex fan-out topology is rejected in v1.
- They wait for exactly one completed branch arrival for each expected edge in the parallel batch.
- They aggregate inbound output values into a deterministic results collection keyed by branch identity and source node.
- They then follow their outgoing transition group.
- Agent synthesis belongs in a normal agent node after the join.

### Scheduler And Recovery

The workflow scheduler has durable inputs in SQLite, but pending scheduler work and active runtime ownership are not durable run states.

Durable state:

- An active node placement plus approved automation intent means a run is runnable.
- A run with `completed_at_unix_ms` is completed.
- A run with `interrupted_at_unix_ms` is interrupted and requires explicit resume.
- A run linked to a pending ask is waiting for user input; task views derive this from ask/run associations.
- Pending approval transitions remain durable transition rows.
- User cancellation of active work and runtime errors become interrupted outcomes with reason metadata. Task-level cancellation additionally records task cancellation metadata.

Live state:

- Pending work ordering is scheduler memory derived from runnable placements/runs.
- Active execution is derived from the live runtime registry/scheduler ownership.
- Concurrency uses `[workflow].concurrency` from the workflow config surface.

Startup reconciliation:

- Rebuild runnable work from active executable placements with approved automation intent, no terminal run outcome, and no task-level cancellation.
- Leave completed runs and pending approval transitions as-is.
- Keep waiting-for-question only if a pending-ask resolver can rehydrate the pending ask from source-of-truth ask/session state; otherwise mark interrupted and resume through the existing session transcript.
- If a run has started but has no terminal outcome and no live runtime owns it after startup, mark it interrupted with a restart/shutdown reason.
- Do not auto-retry interrupted runs.
- Explicit resume continues the interrupted session/run from its current transcript/worktree state.

Completion/transition application still needs idempotency. Use a run generation/fence or equivalent compare-and-swap so a stale runtime callback cannot mutate a run after it has been interrupted, resumed, or superseded. Run completion and transition application should remain one SQLite transaction.

Pending asks must rehydrate through the ask subsystem, not by scanning transcripts. The scheduler depends on a boundary such as `PendingAskResolver.CanRehydrate(sessionID, runID, askID)`. A resolver may return true for a live runtime or a future durable ask record. It must not read full `events.jsonl`; if unresolved pending asks cannot be rehydrated from source-of-truth ask state, startup marks the workflow run interrupted with an actionable resume reason.

### Workflow Config Surface

Workflow runtime config lives under `[workflow]`:

- `completion_mode = "auto"`: one of `auto`, `structured_output`, or `tool`.
- `concurrency = 5`: positive integer global automated-run concurrency.
- `max_final_answer_violations = 3`: positive integer per-run final-answer protocol cap.
- `max_invalid_completion_attempts = 5`: positive integer per-run invalid-completion protocol cap.

Invalid workflow config values fail config validation before workflow workers start. No wall-clock runtime cap is required for v1.

The counters count protocol failures since the last valid workflow progress signal for that run. A valid `ask_question` pause or accepted workflow completion stops/resets the failure loop; alternating final-answer and invalid-completion failures do not reset either counter. Hitting either cap interrupts the run with reason `workflow_protocol_cap_exceeded` and detail metadata containing the cap, counters, and last validation errors.

### Worktrees

A task owns one managed worktree by default. All agent nodes require it. Builder creates it on task start before the first executable run is scheduled. Branch name is the task short ID and should reuse existing worktree branch/root collision handling.

Worktree deletion/retargeting must treat non-terminal tasks referencing a managed worktree as blockers.

Workflow worktree creation needs lower-level worktree primitives that create/register task worktrees without requiring a session controller lease or switching an interactive session.

### CLI Surface

Minimal testing-oriented commands:

- `builder workflow create <name>`
- `builder workflow list`
- `builder workflow node add <workflow> --key <node-key> --kind agent|join|terminal|start --prompt <text> --agent <role>`
- `builder workflow edge add <workflow> --from <source-node-key> --transition <transition-id> --edge-key <edge-key> --to <target-node-key> --context new_session|continue_session|compact_and_continue_session`
- `builder workflow link <project> <workflow> [--default]`
- `builder workflow unlink <project> <workflow>`
- `builder workflow default <project> <workflow>`
- `builder workflow validate <workflow> [--project <project>]`
- `builder workflow inspect <workflow>`
- `builder task create --title <title> --body <body> [--workflow <workflow>]`
- `builder task start <short-id>`
- `builder task list [--project <project>]`
- `builder task show <short-id>`
- `builder task move <short-id> <node> --placement <placement-id> [--edge <edge-id>] [--output field=value ...]`
- `builder task approve <task-transition-id>`
- `builder task resume <short-id>`
- `builder task cancel <short-id> [--reason <reason>]`
- `builder task comment add <short-id> --body <text>`
- `builder task comment list <short-id>`
- `builder task comment replace <comment-id> --body <text>`
- `builder task comment delete <comment-id>`

The exact CLI can be clunky; it exists to exercise backend behavior and teach agents task CLI usage.
Commands whose backend semantics land later may initially fail loudly as unsupported placeholders rather than implementing partial behavior. In particular, full manual moves and approvals belong with the approval/manual-move slice, not the first CLI CRUD slice. Machine-readable `--json` output and workflow-specific environment variables are not required in the first CLI milestone.

## Data Model Draft

Use SQLite for structured workflow/task state. Keep transcripts and large session artifacts file-backed through existing session persistence.

### Existing Tables To Extend

- `projects`
  - Add `project_key TEXT` with global uniqueness in a persistence root. Store/service validation enforces uppercase `^[A-Z][A-Z0-9]{1,7}$` and immutability after tasks exist.
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
- `graph_revision INTEGER NOT NULL DEFAULT 1`
- `created_at_unix_ms INTEGER NOT NULL`
- `updated_at_unix_ms INTEGER NOT NULL`
- `metadata_json TEXT NOT NULL DEFAULT '{}'`

Start node identity is derived from `workflow_nodes.kind = 'start'`. Enforce one start node per workflow with a partial unique index for v1 instead of storing `workflows.start_node_id`. This is a hard storage invariant. Draft workflows may carry semantic validation errors such as missing edges or missing roles, but they must still satisfy storage invariants such as valid IDs, valid enum values, unique keys, valid references, and exactly one start node.

`workflow_nodes`

- `id TEXT PRIMARY KEY`
- `workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE`
- `node_key TEXT NOT NULL`
- `kind TEXT NOT NULL` (`start|agent|join|terminal`)
- `display_name TEXT NOT NULL`
- `subagent_role TEXT NOT NULL DEFAULT ''`
- `prompt_template TEXT NOT NULL DEFAULT ''`
- `output_fields_json TEXT NOT NULL DEFAULT '[]'`
- `sort_order INTEGER NOT NULL DEFAULT 0`
- `metadata_json TEXT NOT NULL DEFAULT '{}'`
- unique `(workflow_id, node_key)`

`output_fields_json` stores an ordered array of user-authored output field definitions. Each field has a stable `name`, a display label if needed, a required `description`, and string-only values at completion time. Edge-owned output requirements decide which fields must be present for a selected transition.

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
- `edge_key TEXT NOT NULL`
- `target_node_id TEXT NOT NULL REFERENCES workflow_nodes(id) ON DELETE CASCADE`
- `requires_approval INTEGER NOT NULL DEFAULT 0`
- `context_mode TEXT NOT NULL`
- `input_bindings_json TEXT NOT NULL DEFAULT '{}'`
- `output_requirements_json TEXT NOT NULL DEFAULT '{}'`
- `sort_order INTEGER NOT NULL DEFAULT 0`
- unique `(transition_group_id, edge_key)`

`project_workflow_links`

- `id TEXT PRIMARY KEY`
- `project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE`
- `workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE RESTRICT`
- `is_default INTEGER NOT NULL DEFAULT 0`
- `unlinked_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `created_at_unix_ms INTEGER NOT NULL`
- `updated_at_unix_ms INTEGER NOT NULL`
- partial unique active workflow link per project where `(project_id, workflow_id)` and `unlinked_at_unix_ms = 0`
- partial unique default per project on `(project_id)` where `is_default = 1 AND unlinked_at_unix_ms = 0`

Unlink semantics are soft-disable when terminal task history references the link. Physical delete is allowed only when no tasks reference the link. Unlink is rejected while non-terminal tasks reference the link. Unlinking the current default requires choosing a replacement default if any other active link remains; task creation without an explicit workflow requires an active default.

`tasks`

- `id TEXT PRIMARY KEY`
- `project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE`
- `project_workflow_link_id TEXT NOT NULL REFERENCES project_workflow_links(id) ON DELETE RESTRICT`
- `workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE RESTRICT`
- `workflow_revision_seen INTEGER NOT NULL`
- `task_seq INTEGER NOT NULL`
- `short_id TEXT NOT NULL`
- `title TEXT NOT NULL`
- `body TEXT NOT NULL`
- `source_url TEXT NOT NULL DEFAULT ''`
- `managed_worktree_id TEXT REFERENCES worktrees(id) ON DELETE SET NULL`
- `canceled_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `cancellation_reason TEXT NOT NULL DEFAULT ''`
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
- `run_generation INTEGER NOT NULL DEFAULT 0`
- `workflow_revision_seen INTEGER NOT NULL`
- `automation_requested_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `created_at_unix_ms INTEGER NOT NULL`
- `updated_at_unix_ms INTEGER NOT NULL`
- `started_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `completed_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `interrupted_at_unix_ms INTEGER NOT NULL DEFAULT 0`
- `interruption_reason TEXT NOT NULL DEFAULT ''`
- `interruption_detail_json TEXT NOT NULL DEFAULT '{}'`
- `waiting_ask_id TEXT NOT NULL DEFAULT ''`
- `final_answer_violation_count INTEGER NOT NULL DEFAULT 0`
- `invalid_completion_count INTEGER NOT NULL DEFAULT 0`
- `run_start_snapshot_json TEXT NOT NULL DEFAULT '{}'`
- `metadata_json TEXT NOT NULL DEFAULT '{}'`

`task_transitions`

- `id TEXT PRIMARY KEY`
- `task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE`
- `source_run_id TEXT REFERENCES task_runs(id) ON DELETE SET NULL`
- `source_placement_id TEXT REFERENCES task_node_placements(id) ON DELETE SET NULL`
- `source_node_id TEXT REFERENCES workflow_nodes(id) ON DELETE SET NULL`
- `source_node_key TEXT NOT NULL DEFAULT ''`
- `source_node_display_name TEXT NOT NULL DEFAULT ''`
- `transition_group_id TEXT REFERENCES workflow_transition_groups(id) ON DELETE SET NULL`
- `transition_id TEXT NOT NULL`
- `transition_display_name TEXT NOT NULL DEFAULT ''`
- `workflow_revision_seen INTEGER NOT NULL`
- `actor TEXT NOT NULL` (`agent|user|system`)
- `state TEXT NOT NULL` (`pending_approval|approved|applied|rejected|invalid`)
- `commentary TEXT NOT NULL DEFAULT ''`
- `output_values_json TEXT NOT NULL DEFAULT '{}'`
- `created_at_unix_ms INTEGER NOT NULL`
- `applied_at_unix_ms INTEGER NOT NULL DEFAULT 0`

`task_transition_edges`

- `id TEXT PRIMARY KEY`
- `task_transition_id TEXT NOT NULL REFERENCES task_transitions(id) ON DELETE CASCADE`
- `workflow_edge_id TEXT REFERENCES workflow_edges(id) ON DELETE SET NULL`
- `edge_key TEXT NOT NULL DEFAULT ''`
- `workflow_revision_seen INTEGER NOT NULL`
- `target_node_id TEXT REFERENCES workflow_nodes(id) ON DELETE SET NULL`
- `target_node_key TEXT NOT NULL DEFAULT ''`
- `target_node_display_name TEXT NOT NULL DEFAULT ''`
- `target_node_kind TEXT NOT NULL DEFAULT ''`
- `target_placement_id TEXT REFERENCES task_node_placements(id) ON DELETE SET NULL`
- `state TEXT NOT NULL` (`pending|applied|completed|blocked`)
- `context_mode TEXT NOT NULL DEFAULT ''`
- `requires_approval INTEGER NOT NULL DEFAULT 0`
- `input_bindings_json TEXT NOT NULL DEFAULT '{}'`
- `output_requirements_json TEXT NOT NULL DEFAULT '{}'`
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

- `project_workflow_links(project_id) WHERE is_default = 1 AND unlinked_at_unix_ms = 0` unique partial
- `project_workflow_links(project_id, is_default) WHERE unlinked_at_unix_ms = 0`
- `project_workflow_links(project_id, workflow_id) WHERE unlinked_at_unix_ms = 0` unique partial
- `project_workflow_links(workflow_id)`
- `projects(project_key)` unique where `project_key != ''`
- `workflow_nodes(workflow_id, sort_order)`
- `workflow_nodes(workflow_id) WHERE kind = 'start'` unique partial
- `workflow_transition_groups(source_node_id, transition_id)`
- `workflow_edges(transition_group_id, sort_order)`
- `workflow_edges(target_node_id)`
- `tasks(project_workflow_link_id)`
- `tasks(managed_worktree_id)`
- `tasks(project_id, updated_at_unix_ms DESC)`
- `tasks(project_id, short_id)`
- `task_node_placements(task_id, state)`
- `task_node_placements(node_id, state)`
- `task_node_placements(parallel_batch_transition_id, parallel_branch_edge_id, state)`
- `task_runs(placement_id)`
- `task_runs(session_id)`
- `task_runs(automation_requested_at_unix_ms, id) WHERE automation_requested_at_unix_ms > 0 AND completed_at_unix_ms = 0 AND interrupted_at_unix_ms = 0`
- `task_runs(started_at_unix_ms, completed_at_unix_ms, interrupted_at_unix_ms)`
- `task_runs(task_id, created_at_unix_ms DESC)`
- `task_transitions(task_id, created_at_unix_ms DESC)`
- `task_transition_edges(task_transition_id, state)`
- `task_transition_edges(workflow_edge_id)`
- `task_transition_edges(target_placement_id)`
- `task_comments(task_id, deleted_at_unix_ms, updated_at_unix_ms DESC)`

### Checks And Store Invariants

SQLite migrations should encode DB-level checks where practical and store/domain tests should cover invariants SQLite cannot express cleanly.

- Enable and test SQLite foreign-key enforcement in metadata DB setup.
- Add enum checks for node kind, context mode, placement state, transition actor/state, transition-edge state, and comment author kind.
- Add boolean checks for `requires_approval`, `is_default`, and similar 0/1 columns.
- Add non-negative checks for Unix timestamps, task sequences, graph revisions, run generations, and protocol counters.
- Add `json_valid` checks for JSON columns, including run/transition snapshots, when SQLite JSON1 is available in the shipped build; otherwise canonicalize/validate JSON in store methods before insert/update.
- Add partial/transactional guards so a task has only the allowed active placements, only one active/runnable run per placement, no executable run for start/join/terminal nodes, no duplicate target placement for one transition-edge snapshot, and no duplicate branch arrival for a parallel batch edge at a join.
- Add store validation for workflow-scoped references so tasks, placements, runs, transitions, groups, nodes, and edges cannot cross project/workflow/task boundaries. Use composite foreign keys where practical and domain validation otherwise.
- Add transaction/state-machine tests for graph mutation plus validation, task start, run completion, approval, cancellation, resume, and transition application idempotency.

### Core Query Shapes

- Load project board: project workflow links, workflow nodes, active task placements, active/waiting/interrupted run summaries.
- Create task: validate linked/default workflow in project context, allocate project sequence atomically, create task, create start-node placement, and store observed graph revision.
- Start task: validate current workflow in project context, apply the start node's single outgoing transition group, ensure task worktree, create target placement/run automation intent, and store observed graph revision.
- Move task manually: validate graph/input/continuation, create transition log, create pending approval state before automation can start.
- Rebuild runnable work: find active executable placements with approved automation intent, no terminal run outcome, no task cancellation, and no pending ask/approval, ordered by automation request time.
- Start next run: count live active workflow runtimes globally, select next runnable run in memory, and start runtime work.
- Complete run: persist transition output values, validate transition group/edges, create target placements/runs or pending approvals, mark source placement completed.
- Join check: derive expected fan-out edge set from the accepted source transition group and aggregate exactly one completed branch result per expected edge.
- Resume interrupted run: validate session/worktree still available, clear interruption by creating or advancing a run generation, and let scheduler continue existing session.
- Cancel task: record task-level cancellation, interrupt active runs with cancellation reason, and suppress scheduler automation for that task.

## Implementation Plan

Follow the execution checklist in `docs/dev/async-workflow-implementation-checklist.md` during implementation. This section defines slice boundaries and completion intent; the checklist is the lower-level handoff tracker with tests, smoke checks, and verification steps.

Use TDD for production implementation. Each slice should leave the repo in a buildable state, with deterministic tests before the next slice begins. Prefer fake provider/model adapters for workflow tests while keeping vertical runtime/tool integration on real runtime code paths; avoid real LLM calls in automated tests.

### Slice 1: Workflow Domain And Graph Validation

Build pure domain types and validation in `server/workflow` without DB or runtime dependencies.

Scope:

- Define workflow definition, node, transition group, edge, output field schema, context-preservation mode, task/run/placement/transition identifiers, and validation error types.
- Validate draft/task-creation/execution contexts; one start node; start-node constraints; execution-valid start outgoing shape; terminal sink constraints; no detached islands; every non-terminal can reach a terminal; workflow-scoped references; node-kind constraints; transition group fan-out/join topology; output requirement references; output field names and size limits; input bindings/templates; and context-preservation mode values.
- Validate project-context dependencies through an injected role resolver so missing subagent roles fail before scheduling.

Completion criteria:

- Unit tests cover invalid draft save reporting, task-creation/execution validation blocking, the default backlog-agent-done workflow, cycles and self-loops allowed outside fan-out branch paths, detached island rejected, unreachable terminal rejected, terminal outgoing edge rejected, missing start rejected, multi-start rejected, ambiguous start rejected, bad transition-group edge rejected, invalid output requirement rejected, invalid input binding/template rejected, invalid context mode rejected, invalid fan-out/join topology rejected, and missing subagent role rejected.
- Validation returns structured errors with stable codes useful for CLI/API display.

### Slice 2: Metadata Schema, Queries, And Store

Add workflow/task persistence to metadata storage before runtime behavior.

Scope:

- Add migrations for project keys/task counters, workflow definitions, graph revisions, nodes, transition groups, edges, project workflow links, tasks, cancellation metadata, placements, runs, protocol counters, transitions, transition edge snapshots, and task comments.
- Add DB checks/indexes and sqlc/store methods for workflow CRUD, project workflow linking/default/unlink semantics, task short-ID allocation, task creation at start node, task start, placement/run state updates, transition logging, task views, comments, cancellation, and guarded destructive edits.
- Store graph revision and edge/node/transition snapshots for pending approvals so later graph edits do not change approval meaning.

Completion criteria:

- Migrations apply cleanly to empty DB and an existing metadata DB fixture.
- Store tests prove project key backfill/collision/immutability, DB enum/check constraints, atomic task sequence allocation, one active default workflow per project, invalid default can exist but task creation rejects it, task creation creates exactly one start placement, task start creates runnable automation, comment add/replace/soft-delete behavior, transition snapshots survive graph edits, terminal-only workflow unlink preserves history, and guarded graph deletes reject non-terminal task references.
- `./scripts/test.sh ./server/metadata/... ./server/workflow/... ./server/workflowstore/...` passes.

### Slice 3: API Contracts, Service Layer, And Read Models

Expose typed backend API/read-model shapes needed by CLI and future UI/POC GUI without runtime execution. These are implementation contracts, not public compatibility guarantees before Builder 2.0.

Scope:

- Add workflow/task DTOs under `shared/serverapi`, route-shaped service interfaces in `shared/servicecontract`, loopback/remote client support, transport routes, task-start/cancel methods, validation-mode requests, and server service composition.
- Add `server/workflowview` read models for project boards, task detail, transition history, run summaries, comments, validation results, terminal/done state, cancellation, pending approval, and interrupted states.
- Keep API validation strict at boundaries while domain validation remains reusable by CLI and services.

Completion criteria:

- Contract tests cover request validation, project default workflow resolution, stable board ordering by workflow node order, task detail with active placements/runs, transition history ordering, and deleted comments hidden by default.
- Loopback client and remote route tests exercise same service methods.
- UI/POC GUI can obtain board and task detail views without reading session transcripts or `events.jsonl`.
- POC GUI integration goes through a thin adapter layer because backend workflow DTOs/read models can break before Builder 2.0.

### Slice 4: Minimal Workflow And Task CLI

Add clunky but complete backend-testing commands for CRUD/read/comment/validation before automation. These commands are for engineering validation and agent usage, not for Nikita-led manual QA. Full manual moves and approvals land later; early `task move`/`task approve` commands may be explicit unsupported placeholders.

Scope:

- Implement minimal `builder workflow` commands for create, list, node add, edge add, link, unlink, default, validate, and inspect.
- Implement minimal `builder task` commands for create, start placeholder or implementation depending on service readiness, list, show, resume/cancel placeholders, comment add/list/replace/delete, and optional unsupported placeholders for move/approve.
- Prefer stable IDs in output where later commands need row identifiers; approval uses task transition row ID, not user-defined transition ID.

Completion criteria:

- CLI tests or command-level tests create a workflow, link it to a project, create a task, list/show board/task views, add/replace/delete comments, validate graph errors, and verify manual move/approval placeholders fail loudly until their implementation slice.
- CLI output includes enough IDs for humans and agents to continue from terminal logs.
- A no-LLM coding-agent smoke check can be run against a temporary persistence root: create a real workflow graph, create tasks, inspect board/task views, verify comments plus IDs behave as expected, and verify manual move/approval commands fail loudly until their implementation slice.

### Slice 5: Task-Owned Worktree Primitive

Create lower-level worktree capability for task automation, separate from interactive controller leases.

Scope:

- Add service method to create/register a managed task worktree for a task on task start before the first executable run is scheduled.
- Reuse existing branch/root collision handling and physical worktree operations.
- Enforce non-terminal task blockers for managed worktree deletion/retargeting.

Completion criteria:

- Worktree tests with temp repos prove branch name defaults to task short ID, repeated ensure calls are idempotent, collisions get deterministic safe names, and no interactive session/controller lease is required.
- Blocking tests prove non-terminal tasks prevent managed worktree deletion/retargeting.

### Slice 5.5: Full Non-Agent E2E Smoke Check

Run a dedicated no-LLM E2E smoke after the Slice 4 CLI and Slice 5 worktree primitive exist.

Completion criteria:

- A coding agent can create a real workflow graph, link it to a project, create tasks, inspect board/task views, manage comments, validate unsupported manual/approval placeholders if still unsupported, and ensure task-owned worktree creation works from a temporary persistence root.
- The smoke uses embedded-local server wiring and does not touch Nikita's real persistence root or provider credits.

### Slice 6: Scheduler, Runnable Derivation, And Recovery

Implement scheduling state and recovery without real model execution first.

Scope:

- Add scheduler service that rebuilds runnable work from durable active placements, automation intent, pending asks, interrupted/completed outcomes, task cancellation, and pending approvals.
- Keep pending-work ordering and active-runtime ownership in memory, not durable run states.
- Add startup reconciliation for runnable, active-runtime-derived, waiting-for-question, interrupted, completed, and pending approval work.
- Define and test pending-ask rehydration boundary before waiting-for-question recovery depends on it.
- Make completion/transition application one transaction so source run, source placement, transition log, target placements, and target automation intent commit atomically.

Completion criteria:

- Scheduler tests prove one live runtime starts per runnable run and global concurrency is respected.
- Fencing tests prove stale runtime completion cannot mutate a run after a newer generation.
- Recovery tests prove runnable work is rebuilt, orphaned started runs become interrupted, pending approvals stay pending, and interrupted runs are never auto-retried.
- Transaction tests prove unsuccessful transition application leaves no half-created target placements/runs or automation intent.

### Slice 7: Workflow Prompting And Completion Runtime

Add workflow-aware runtime control before running full workflow nodes.

Scope:

- Add workflow-mode developer prompt source and injection.
- Add temporary global workflow completion mode config: `auto|structured_output|tool`, with no workflow/node override.
- Generate structured-output schema or dynamic `complete_node` schema with top-level node output fields.
- Carry active workflow run context into structured-output and tool execution.
- Add preflight that rejects assistant responses where `complete_node` is mixed with any other tool call.
- Add terminal signal from accepted `complete_node` so the step loop persists the tool result and stops without another model turn.
- Treat normal assistant final answers as invalid workflow output, append a developer nudge, and enforce mandatory protocol caps for final-answer violations and invalid completion attempts.

Completion criteria:

- Runtime tests prove workflow prompt injection, structured output completion, dynamic tool completion, `complete_node` outside workflow explicit error, duplicate/mixed tool calls rejected before side effects, non-string/oversized output rejected, missing `transition_id` rejected only when multiple transition groups exist, invalid output field and missing/empty edge-required output return structured errors/nudges, caps interrupt runaway protocol failures, valid completion stops the run, and final-answer-only workflow output nudges and continues until cap.
- Existing non-workflow tool execution behavior remains unchanged.

### Slice 8: Single-Agent `new_session` Vertical Slice

Connect persistence, scheduler, task worktree, runtime, and completion for the smallest useful async workflow.

Scope:

- Execute one agent node using `new_session`: task creation, explicit `task start` action from start node into executable node, worktree ensure, runnable work rebuild/start, session creation, workflow prompt injection, fake/model runtime execution through real runtime/completion handling, transition application, and active terminal placement.
- Use fake provider/model adapters in tests while still exercising real runtime/tool handling for vertical completion behavior; reserve real provider use for manual QA later.

Completion criteria:

- Integration test creates workflow `backlog(start) -> agent -> done(terminal)`, creates a task, starts automation, completes via fake structured output and fake dynamic tool modes, and observes task placement in terminal node with stored output values/commentary.
- CLI can create/show the same flow against embedded server state.
- No test loads full `events.jsonl`.
- Real-provider smoke testing is optional and requires explicit manual approval before spending provider credits.

### Slice 9: Question Pause And Resume

Finish workflow question pause/resume behavior using the ask rehydration boundary proven during scheduler work, or stop and design the persistence upgrade.

Scope:

- Run a workflow node that calls `ask_question`, transitions run state to `waiting_for_question`, accepts an answer, and resumes same run/session.
- Test restart/reconciliation behavior around a pending question.
- If existing ask persistence cannot support this reliably, upgrade ask persistence as the source of truth before continuing; do not add a shadow task-question table.

Completion criteria:

- Tests prove question answer resumes the same workflow run/session and can complete with workflow completion.
- Restart test proves pending ask can be rehydrated or run becomes interrupted with actionable resume path.
- Any required ask persistence upgrade has focused tests and keeps task question views derived from source-of-truth ask state.

### Slice 10: Context-Preservation Modes

Implement edge context semantics after single-session execution works.

Scope:

- Implement `new_session`, `continue_session`, and `compact_and_continue_session`.
- Enforce direct `continue_session` only when source and target use the same subagent role name. Persisted session setup remains authoritative when same-role config drift exists.
- Allow role changes for `new_session` and `compact_and_continue_session` because they use fresh context boundary or compacted continuation input.

Completion criteria:

- Tests prove same-role `continue_session` appends to existing session and keeps immutable session setup despite current config drift, cross-role `continue_session` is rejected before scheduler start, `new_session` creates separate session across roles using current role config, and compact mode creates a compacted continuation input before target execution.
- Prompt/cache-sensitive behavior does not mutate prior transcript history.

### Slice 11: Approvals And Manual Moves

Implement human-controlled task movement and edge approval semantics.

Scope:

- Apply edge `requires_approval` by storing pending transition/edge snapshots after source run completion and starting targets only after approval. If any edge in a selected transition group requires approval, the whole group waits.
- Implement manual moves through real edge/transition metadata or explicit equivalent metadata.
- Require explicit user approval before automation starts from manual moves into executable targets.
- Support backward moves that reuse stored output values when validation allows.

Completion criteria:

- Tests prove approval by task transition row ID starts stored target edge snapshots, double approval is idempotent, later graph edits do not change pending approval behavior, mixed-approval fan-out waits as a whole group, forward manual move validates provided output values, backward manual move can reuse stored output values, missing required output is rejected, continuation without valid source session is rejected, and executable manual target pauses before automation.

### Slice 12: Fan-Out, Parallel Branches, And Joins

Add explicit parallel branch support after serial execution is stable.

Scope:

- Apply multi-edge transition groups by creating a parallel batch and one branch placement per edge.
- Carry `parallel_batch_transition_id` and `parallel_branch_edge_id` until branch reaches join or terminal.
- Implement static fan-out validation that derives exactly one nearest common join and rejects terminal-before-join, nested fan-out-before-join, cycles-before-join, and ambiguous branch topology.
- Implement join nodes that wait for one completed branch result per expected fan-out edge, aggregate deterministic branch results, and continue through their outgoing transition group.

Completion criteria:

- Tests prove fan-out creates multiple active placements under one task, invalid join topology is rejected, branches can complete in any order, join waits until all expected branches arrive, duplicate branch arrivals are rejected or ignored idempotently, aggregate result ordering is deterministic, and next node receives bound aggregate input.

### Slice 13: Recovery, Observability, And Hardening

Close operational gaps before UI work.

Scope:

- Add resume, task cancellation, interrupt/cancel-as-interrupted, and recover commands/service methods with clear transitions.
- Add structured logs/diagnostics around scheduler runnable selection, run completion, transition application, validation blockers, and workflow runtime errors.
- Add role-drift validation at scheduling/resume time and actionable error surfaces in CLI/API.
- Update `docs/dev/decisions.md` only after decisions are stable and without staging unrelated user edits.

Completion criteria:

- Restart tests cover runnable work rebuild, orphaned started runs becoming interrupted, already interrupted runs, waiting-for-question, pending approval tasks, and canceled tasks staying unscheduled.
- CLI/API surfaces validation and orchestration errors with stable codes and actionable messages.
- `./scripts/test.sh ./server/workflow/... ./server/workflowstore/... ./server/workflowsvc/... ./server/workflowscheduler/... ./server/workflowruntime/... ./server/workflowview/... ./server/metadata/...` and `./scripts/build.sh --output ./bin/builder` pass once production code exists.

### Suggested First Coding Milestone

First implementation milestone should end after Slice 4:

- It gives durable workflow/task CRUD, validation, board/task read models, and comments without runtime risk.
- Builder can perform first internal no-LLM smoke check at this point by creating a real graph/task through CLI/API and validating status movement, comments, IDs, and read models.
- This milestone needs no real LLM calls.
- Nikita-led manual QA should be deferred until a usable GUI/POC exists on top of these APIs.

After Slice 4, continue runtime work with automated fake-model/fake-runtime tests through Slice 8. Slice 8 proves the product's core async promise: one task, one worktree, one agent node, structured completion, and terminal status. Real-agent QA should remain an explicit manual approval step because it spends provider credits and can be flaky for reasons unrelated to orchestration correctness.
