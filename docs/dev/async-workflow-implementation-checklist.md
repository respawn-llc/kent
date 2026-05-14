# Async Workflow Implementation Checklist

This checklist is the execution tracker for `docs/dev/async-workflow-orchestration.md`. Keep it updated while implementing. Mark items complete only after code, tests, and verification for that item are done.

Do not treat this as a replacement for the spec. If spec and checklist conflict, stop and update both before continuing.

## Working Rules

- [ ] Work one slice at a time, in order, unless Nikita explicitly changes priority.
- [ ] Use TDD for production behavior: write failing tests first, implement, refactor.
- [ ] Keep each slice buildable before moving to the next slice.
- [ ] Commit after each completed slice or stable sub-slice.
- [ ] Do not run real-provider workflow QA without explicit Nikita approval.
- [ ] Use fake model/runtime adapters for automated workflow runtime tests.
- [ ] Keep CLI as internal harness/agent surface, not Nikita's manual QA surface.
- [ ] Treat workflow API/read-model DTOs as mutable before Builder 2.0.
- [ ] Keep POC GUI integration behind a thin adapter layer.
- [ ] Never load full `events.jsonl` in workflow code or tests.
- [ ] Update `docs/dev/decisions.md` when a new locked product/architecture decision is made.
- [ ] When a slice changes a locked decision, update `docs/dev/decisions.md` first, then this checklist/spec.
- [ ] Avoid staging unrelated user changes; inspect `git status --short` before every commit.
- [ ] Use this checklist as the handoff contract during implementation; do not invent parallel task trackers.
- [ ] Mark checklist items incrementally while implementing so handoffs resume from exact state.

## Repo Path Assumptions

Checked before implementation in this worktree:

- `server/workflow` does not exist yet and is the intended pure domain package for Slice 1.
- `server/workflowview` and `server/workflowruntime` do not exist yet and are intended package additions in later slices.
- Existing metadata paths are `server/metadata/migrations`, `server/metadata/queries.sql`, `server/metadata/store.go`, and `server/metadata/sqlitegen`.
- Existing shared boundary paths are `shared/serverapi`, `shared/servicecontract`, and `shared/client`.
- Existing transport path is `server/transport`.
- Existing CLI command path is `cli/builder`.
- Existing composition/lifecycle paths relevant to workflow workers are `server/core`, `server/bootstrap`, `server/embedded`, and `server/serve`.
- Existing runtime activation/control paths are `server/runtimewire`, `server/sessionruntime`, `server/runtimecontrol`, `server/launch`, and `server/registry`.
- Existing worktree and tool-schema paths are `server/worktree`, `shared/serverapi/worktree.go`, `server/tools/definitions.go`, and `shared/toolspec`.
- Existing RPC/method boundary path is `shared/protocol`.
- If these paths change before a later slice starts, update this section before coding that slice.

## Slice 1: Workflow Domain And Graph Validation

Goal: pure workflow domain package with domain types and graph validation. No DB, runtime, CLI, or transport dependencies.

### 1.1 Recon

- [ ] Inspect existing Go package naming and error conventions near `server/metadata`, `server/projectview`, and `shared/serverapi`.
- [ ] Confirm no existing workflow package or graph validator exists.
- [ ] Identify subagent role/config lookup abstraction currently used by runtime/subagents.
- [ ] Decide permanent package boundary before coding: keep `server/workflow` pure and put persistence/scheduler/runtime adapters in sibling packages, or choose a different pure-domain package name and update the spec/checklist.
- [ ] Record any discovered naming conflicts in this checklist before coding.

### 1.2 Red Tests

- [ ] Add `server/workflow` package test file for graph validation.
- [ ] Define exact validation error code names before implementation and assert them in tests.
- [ ] Add valid fixture helper for default workflow: `backlog(start) -> agent -> done(terminal)`.
- [ ] Add test: valid default workflow passes.
- [ ] Add test: exactly one start node required.
- [ ] Add test: missing start node rejected.
- [ ] Add test: multiple start nodes rejected.
- [ ] Add test: start node must be non-executable, have no subagent role, have no prompt, and have no output/payload requirements.
- [ ] Add test: start node incoming edges are rejected unless spec is explicitly changed.
- [ ] Add test: terminal node cannot have outgoing edges.
- [ ] Add test: terminal node must be non-executable and have no subagent role/prompt.
- [ ] Add test: join node must be non-executable and have no subagent role/prompt.
- [ ] Add test: join node cannot also be terminal/start.
- [ ] Add test: join node outgoing shape is valid for v1, with exactly one transition group unless routing is explicitly added.
- [ ] Add test: every node reachable from start.
- [ ] Add test: every non-terminal node can reach terminal.
- [ ] Add test: detached island rejected.
- [ ] Add test: cycles are allowed when terminal remains reachable.
- [ ] Add test: self-loop is allowed when terminal remains reachable.
- [ ] Add test: transition group source must match edge source.
- [ ] Add test: transition group ID unique workflow-wide.
- [ ] Add test: transition IDs unique per source node.
- [ ] Add test: transition group must contain at least one edge.
- [ ] Add test: edge keys unique per transition group.
- [ ] Add test: edge target node must exist.
- [ ] Add test: workflow-scoped references cannot cross workflow definitions.
- [ ] Add test: output schema field names reject empty/duplicate/invalid identifiers.
- [ ] Add test: payload requirements must reference known source node output fields.
- [ ] Add test: context mode must be one of `new_session`, `continue_session`, `compact_and_continue_session`.
- [ ] Add test: agent node requires valid subagent role.
- [ ] Add test: missing subagent role returns stable validation code.
- [ ] Add test: validation returns all relevant structured errors where safe, not first string-only failure.

Initial validation error code names:

- [ ] `workflow.validation.missing_workflow_id`
- [ ] `workflow.validation.missing_node_id`
- [ ] `workflow.validation.duplicate_node_key`
- [ ] `workflow.validation.missing_start_node`
- [ ] `workflow.validation.multiple_start_nodes`
- [ ] `workflow.validation.invalid_start_node`
- [ ] `workflow.validation.terminal_has_outgoing_edge`
- [ ] `workflow.validation.terminal_is_executable`
- [ ] `workflow.validation.join_is_executable`
- [ ] `workflow.validation.invalid_join_node`
- [ ] `workflow.validation.invalid_join_outgoing_shape`
- [ ] `workflow.validation.node_unreachable_from_start`
- [ ] `workflow.validation.non_terminal_cannot_reach_terminal`
- [ ] `workflow.validation.missing_transition_group_id`
- [ ] `workflow.validation.duplicate_transition_group_id`
- [ ] `workflow.validation.empty_transition_group`
- [ ] `workflow.validation.duplicate_transition_id`
- [ ] `workflow.validation.transition_group_source_mismatch`
- [ ] `workflow.validation.missing_edge_id`
- [ ] `workflow.validation.duplicate_edge_key`
- [ ] `workflow.validation.edge_source_mismatch`
- [ ] `workflow.validation.edge_target_missing`
- [ ] `workflow.validation.cross_workflow_reference`
- [ ] `workflow.validation.invalid_output_field`
- [ ] `workflow.validation.duplicate_output_field`
- [ ] `workflow.validation.unknown_payload_requirement`
- [ ] `workflow.validation.invalid_context_mode`
- [ ] `workflow.validation.agent_role_required`
- [ ] `workflow.validation.agent_role_missing`
- [ ] `workflow.validation.invalid_node_kind`

### 1.3 Implement Domain Types

- [ ] Define typed identifiers for workflow, node, transition group, edge, task, placement, run, transition.
- [ ] Define node kinds: start, agent, join, terminal.
- [ ] Define context-preservation modes.
- [ ] Define node output schema field type with string-only fields.
- [ ] Define payload requirement type.
- [ ] Define workflow definition aggregate.
- [ ] Define project-context validation input with role resolver interface.
- [ ] Define validation error code type.
- [ ] Define validation error struct with code, message, and related IDs.
- [ ] Keep JSON/DB-specific tags out of pure domain types unless unavoidable.

### 1.4 Implement Validation

- [ ] Validate required IDs and keys.
- [ ] Validate one start node.
- [ ] Validate node kind constraints.
- [ ] Validate start, join, and terminal execution config constraints.
- [ ] Validate transition group and edge source/target references.
- [ ] Validate uniqueness constraints.
- [ ] Validate graph reachability from start.
- [ ] Validate terminal reachability from every non-terminal node.
- [ ] Validate cycles/self-loops do not fail by themselves.
- [ ] Validate payload requirement references.
- [ ] Validate context mode values.
- [ ] Validate agent role references through injected resolver.
- [ ] Return structured validation errors with stable codes.

### 1.5 Slice Verification

- [ ] Run `./scripts/test.sh ./server/workflow/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Review `server/workflow` files for package cohesion and no DB/runtime imports.
- [ ] Commit slice with message like `feat: add workflow graph validation`.

## Slice 2: Metadata Schema, Queries, And Store

Goal: persist workflow definitions, project links, tasks, placements, runs, transitions, and comments in SQLite.

### 2.1 Recon

- [ ] Inspect current migration numbering in `server/metadata/migrations`.
- [ ] Inspect `server/metadata/queries.sql` style and sqlc generated package shape.
- [ ] Inspect existing store test helpers and fixture DB setup.
- [ ] Inspect project/workspace/worktree/session table schemas.
- [ ] Decide whether project task counter lives on `projects` or separate counter table; update spec/checklist if changed.

### 2A: Schema, Migrations, And SQLC

Goal: add DB shape and generated query surface before high-level store behavior.

### 2A.1 Red Migration/SQL Tests

- [ ] Add migration test for empty DB.
- [ ] Add migration test for existing metadata DB fixture.
- [ ] Add test for project key backfill using default project-name logic.
- [ ] Add test for explicit project key storage and validation.
- [ ] Add test for project key collision handling.
- [ ] Add test that circular transition/placement references can be inserted with SQLite constraints, or choose nullable/domain-validated references and test that path.
- [ ] Add test for atomic task sequence allocation under concurrent creates.
- [ ] Add test for task short ID format and uniqueness per project.

### 2A.2 Migrations

- [ ] Add project key column and task sequence storage.
- [ ] Add `workflows`.
- [ ] Add `workflow_nodes`.
- [ ] Add `workflow_transition_groups`.
- [ ] Add `workflow_edges`.
- [ ] Add `project_workflow_links`.
- [ ] Add `tasks`.
- [ ] Add `task_node_placements`.
- [ ] Add `task_runs`.
- [ ] Add `task_transitions`.
- [ ] Add `task_transition_edges`.
- [ ] Add `task_comments`.
- [ ] Add indexes listed in spec.
- [ ] Add composite foreign keys where practical for workflow-scoped references.
- [ ] Add domain/store validation where SQLite cannot enforce scope cleanly.
- [ ] Resolve transition/placement circular-reference insertion explicitly; do not leave it as accidental SQLite behavior.

### 2A.3 SQLC

- [ ] Add sqlc queries for workflow CRUD.
- [ ] Add sqlc queries for nodes/groups/edges CRUD.
- [ ] Add sqlc queries for project workflow links.
- [ ] Add sqlc queries for project key/task sequence allocation.
- [ ] Add sqlc queries for task create/read/list.
- [ ] Add sqlc queries for placement create/update/read.
- [ ] Add sqlc queries for run create/update/read.
- [ ] Add sqlc queries for transition log and transition edge snapshots.
- [ ] Add sqlc queries for task comments.
- [ ] Regenerate sqlc output using existing repo command/pattern.

### 2B: Workflow, Link, And Task Store

Goal: typed transactional store methods for workflows, project links, tasks, placements, and runs.

### 2B.1 Red Store Tests

- [ ] Add test for workflow create/update/read/list.
- [ ] Add test for workflow creation auto-creating editable `backlog` start node and `done` terminal node.
- [ ] Add test for node/transition group/edge persistence.
- [ ] Add test for project-workflow link create/list/delete.
- [ ] Add test for exactly one default workflow link per project.
- [ ] Add test for task create selecting default workflow.
- [ ] Add test for task create with explicit workflow.
- [ ] Add test for task create creates exactly one start node placement.
- [ ] Add test for same task short sequence allowed in different projects but short ID uniqueness enforced within one project.
- [ ] Add test that task creation rejects workflow not linked to task project.
- [ ] Add test for placement state transitions.
- [ ] Add test for run create/update state fields.

### 2B.2 Store Implementation

- [ ] Add store methods wrapping workflow create/default-node creation in one transaction.
- [ ] Add store methods for project workflow links and default link changes.
- [ ] Add store methods for atomic task sequence allocation and task creation.
- [ ] Add store methods for placement and run state updates.
- [ ] Keep store methods typed; avoid passing raw JSON strings across domain boundaries where avoidable.

### 2C: Transitions, Comments, And Guards

Goal: transition/comment history and guarded graph mutation behavior.

### 2C.1 Red Store Tests

- [ ] Add test for transition log create/read ordering.
- [ ] Add test for transition edge snapshot persistence.
- [ ] Add test for pending approval snapshot not changing after graph edit.
- [ ] Add test for comment add.
- [ ] Add test for comment full-text replace.
- [ ] Add test for comment soft-delete.
- [ ] Add test for deleted comments hidden by default.
- [ ] Add test for guarded graph delete rejected when non-terminal tasks reference graph element.
- [ ] Add test for guarded graph delete allowed when safe.

### 2C.2 Store Implementation

- [ ] Add store methods for transition log and edge snapshots.
- [ ] Add store methods for task comments.
- [ ] Add guarded graph-delete checks.
- [ ] Add transactional helpers for multi-row transition/comment operations.

### 2.5 Slice Verification

- [ ] Run metadata migration tests.
- [ ] Run `./scripts/test.sh ./server/metadata/... ./server/workflow/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: persist workflow tasks`.

## Slice 3: API Contracts, Service Layer, And Read Models

Goal: typed backend service and read surfaces for CLI and POC GUI. Contracts are mutable pre-2.0.

### 3.1 Recon

- [ ] Inspect `shared/serverapi` route DTO patterns.
- [ ] Inspect `shared/servicecontract` interface patterns.
- [ ] Inspect `shared/client` loopback/remote client patterns.
- [ ] Inspect `server/transport` route registration patterns.
- [ ] Inspect `shared/protocol` method constant and route-name patterns.
- [ ] Inspect existing read-model packages such as `server/projectview`, `server/sessionview`, and `server/runtimeview`.

### 3A: DTOs, Service Contracts, Clients, And Transport

Goal: route-shaped typed API surface without large read-model work.

### 3A.1 Red Tests

- [ ] Add DTO validation tests for workflow create/update requests.
- [ ] Add DTO validation tests for node/edge creation requests.
- [ ] Add DTO validation tests for task create/move/comment requests.
- [ ] Add DTO/API tests for explicit project key create/update path, or document default-only task key support as a locked decision before skipping.
- [ ] Add service test for project default workflow resolution.
- [ ] Add service test for workflow validation endpoint using domain validator.
- [ ] Add service test: cannot create task with workflow not linked to project.
- [ ] Add service test: default workflow resolves within project only.
- [ ] Add loopback client test for at least one workflow route.
- [ ] Add remote/transport route test for same route if repo has route test pattern.

### 3A.2 Implementation

- [ ] Add `shared/serverapi/workflow.go` DTOs.
- [ ] Add validation helpers with stable error codes.
- [ ] Add `shared/servicecontract` workflow interface.
- [ ] Add `shared/client` workflow client methods.
- [ ] Add protocol/method constants if current transport pattern requires them.
- [ ] Add server workflow service composition.
- [ ] Add transport route registration.

### 3B: Workflow Views

Goal: board and task read models for CLI and POC GUI adapter.

### 3B.1 Red Tests

- [ ] Add read-model test for board node ordering.
- [ ] Add read-model test for board active placement summaries.
- [ ] Add read-model test for task detail including placements, runs, transitions, comments.
- [ ] Add read-model test for transition history ordering.
- [ ] Add read-model test for deleted comments hidden by default.

### 3B.2 Implementation

- [ ] Add `server/workflowview` read-model package.
- [ ] Ensure read models do not read session transcripts or `events.jsonl`.
- [ ] Document mutable pre-2.0 contract expectation in code comments only where useful.

### 3.4 Slice Verification

- [ ] Run `./scripts/test.sh ./shared/serverapi/... ./shared/servicecontract/... ./shared/client/... ./server/workflow/... ./server/workflowview/... ./server/transport/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: expose workflow task APIs`.

## Slice 4: Minimal Workflow And Task CLI

Goal: internal CLI harness and agent-control surface for workflow/task CRUD, comments, and validation. Not Nikita manual QA surface. Full manual moves and approvals are implemented in Slice 11.

### 4.1 Recon

- [ ] Inspect command structure in `cli/builder/main.go`.
- [ ] Inspect existing command files for flag parsing and client setup.
- [ ] Inspect embedded/remote serverbridge usage for non-interactive commands.
- [ ] Decide output format conventions for IDs and errors.
- [ ] Plan dedicated workflow/task command files; keep `cli/builder/main.go` dispatch thin.

### 4.2 Command Parser/Handler Tests

- [ ] Add command test for `builder workflow create`.
- [ ] Add command test that `builder workflow create` auto-creates `backlog` and `done`.
- [ ] Add command test for `builder workflow node add` adding extra agent/join nodes.
- [ ] Add command test for `builder workflow edge add`.
- [ ] Add command test for `builder workflow link`.
- [ ] Add command test for `builder workflow validate`.
- [ ] Add command test for `builder workflow inspect`.
- [ ] Add command test for `builder task create`.
- [ ] Add command test for `builder task list`.
- [ ] Add command test for `builder task show`.
- [ ] Add command test that `builder task move` fails loudly/nonzero until Slice 11.
- [ ] Add command test that `builder task approve` fails loudly/nonzero until Slice 11.
- [ ] Add command test that `builder task resume` fails loudly/nonzero until runtime/resume slices.
- [ ] Add command test for `builder task comment add`.
- [ ] Add command test for `builder task comment replace`.
- [ ] Add command test for `builder task comment delete`.
- [ ] Add command test that CLI output includes IDs required by next commands.
- [ ] Add command test for actionable validation error output.

### 4.3 CLI Implementation

- [ ] Implement `builder workflow create <name>`.
- [ ] Implement `builder workflow node add <workflow> --id <id> --kind start|agent|join|terminal --prompt <text> --agent <role>`.
- [ ] Implement `builder workflow edge add <workflow> --from <node> --transition <id> --to <node> --context <mode>`.
- [ ] Implement `builder workflow link <project> <workflow> [--default]`.
- [ ] Implement `builder workflow validate <workflow> [--project <project>]`.
- [ ] Implement `builder workflow inspect <workflow>`.
- [ ] Implement `builder task create --title <title> --body <body> [--workflow <workflow>]`.
- [ ] Implement `builder task list [--project <project>]`.
- [ ] Implement `builder task show <short-id>`.
- [ ] Add `builder task move ...` as explicit unsupported command until Slice 11, if reserving command shape is useful.
- [ ] Add `builder task approve ...` as explicit unsupported command until Slice 11, if reserving command shape is useful.
- [ ] Implement `builder task resume <short-id>` as placeholder or explicit unsupported command until runtime slice.
- [ ] Implement `builder task comment add <short-id>`.
- [ ] Implement `builder task comment replace <comment-id>`.
- [ ] Implement `builder task comment delete <comment-id>`.
- [ ] Ensure command errors return nonzero exit and stable message.
- [ ] Ensure CLI does not bypass service/store invariants.
- [ ] Put workflow/task command handlers in dedicated files or package-local command structs; keep `main.go` dispatch thin.

### 4.4 Internal Smoke Check

- [ ] Run smoke against a temp persistent root and embedded-local server wiring, not the user's real daemon/root.
- [ ] Create temporary persistence root.
- [ ] Write temp Builder config/workspace config defining role `workflow-test` for contextual workflow validation.
- [ ] Create or bind a test project/workspace through existing setup commands.
- [ ] Create workflow.
- [ ] Confirm workflow has auto-created editable `backlog` start node and `done` terminal node.
- [ ] Add agent node referencing temp role `workflow-test`.
- [ ] Add start-to-agent edge.
- [ ] Add agent-to-done edge.
- [ ] Link workflow to project as default.
- [ ] Validate workflow.
- [ ] Create task with title/body.
- [ ] List tasks and confirm short ID.
- [ ] Show task and confirm start placement.
- [ ] Add task comment.
- [ ] Replace task comment.
- [ ] Delete task comment.
- [ ] Confirm `task move` and `task approve` are unavailable or unsupported until their implementation slices.
- [ ] Capture exact smoke commands in PR/commit notes or checklist update if useful.

### 4.5 Milestone Verification

- [ ] Run `./scripts/test.sh ./server/workflow/... ./server/workflowview/... ./server/metadata/... ./shared/serverapi/... ./shared/client/... ./server/transport/... ./cli/builder/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Confirm no real LLM/provider calls happened.
- [ ] Commit slice with message like `feat: add workflow task cli`.
- [ ] Stop for implementation review before runtime internals if needed.

## Slice 5: Task-Owned Worktree Primitive

Goal: task-managed worktree creation/registering without interactive session/controller lease.

### 5.1 Recon

- [ ] Inspect `server/worktree` service public methods.
- [ ] Inspect `shared/serverapi/worktree.go`.
- [ ] Inspect worktree DB tables and existing worktree tests.
- [ ] Identify where controller lease assumptions enter worktree creation.
- [ ] Identify reusable branch/root collision helpers.

### 5.2 Red Tests

- [ ] Add temp repo test for ensuring task worktree creates branch named task short ID.
- [ ] Add test for repeated ensure returning existing managed worktree.
- [ ] Add test for branch/root name collision handling.
- [ ] Add test that task worktree ensure does not require controller lease.
- [ ] Add test that non-terminal task blocks managed worktree deletion.
- [ ] Add test that terminal task allows cleanup when other blockers absent.

### 5.3 Implementation

- [ ] Add task-owned worktree ensure method to worktree service or focused adapter.
- [ ] Register managed worktree ID on task metadata/store transaction.
- [ ] Reuse existing physical worktree creation.
- [ ] Reuse existing root/branch collision behavior.
- [ ] Add blocker query/service path for non-terminal tasks using managed worktree.
- [ ] Keep interactive session switching separate from task worktree creation.

### 5.4 Verification

- [ ] Run `./scripts/test.sh ./server/worktree/... ./server/metadata/... ./server/workflow/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: add task worktree primitive`.

## Slice 6: Durable Queue, Claims, And Recovery

Goal: SQLite-backed runnable workflow queue with CAS claims, fencing, global concurrency, and startup reconciliation.

### 6.1 Red Tests

- [ ] Add config schema/default test for workflow global concurrency defaulting to `5`.
- [ ] Add config validation test for invalid workflow concurrency values.
- [ ] Add test for selecting oldest queued run.
- [ ] Add test for global concurrency cap.
- [ ] Add concurrent claim race test proving one claim wins.
- [ ] Add test for claim metadata persisted.
- [ ] Add stale claim completion rejected by generation/fence.
- [ ] Add test for queued state retained on startup.
- [ ] Add test for running state becoming interrupted on startup.
- [ ] Add test for waiting-for-question retained when ask can rehydrate.
- [ ] Add test for waiting-for-question becoming interrupted when ask cannot rehydrate.
- [ ] Add test for pending approval retained on startup.
- [ ] Add test that interrupted runs are never auto-retried.
- [ ] Add transaction rollback test for failed transition application.

### 6.2 Implementation

- [ ] Add queue service under pure workflow package or separate cohesive scheduler package according to Slice 1 package-boundary decision.
- [ ] Add config field/read for global workflow concurrency default 5.
- [ ] Add config validation for invalid values.
- [ ] Implement transactional claim from `queued` to `running`.
- [ ] Store `claim_id`, `claimed_by`, `claimed_at_unix_ms`, and `state_generation`.
- [ ] Implement completion path requiring matching claim/generation.
- [ ] Implement startup reconciliation.
- [ ] Implement atomic transition application transaction.
- [ ] Add structured logs for claim/recovery/transition outcomes.

### 6.3 Verification

- [ ] Run queue tests with race-sensitive cases repeatedly.
- [ ] Run `./scripts/test.sh ./server/workflow/... ./server/metadata/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: add workflow queue`.

## Runtime Test Adapter Boundary

Define this boundary before Slice 7 implementation, then reuse it in Slice 8. Fake provider/model transport is allowed; fake workflowruntime that bypasses real runtime/tool execution is allowed only for scheduler tests, not vertical completion tests.

- [ ] Define fake provider/model adapter interface before Slice 7 runtime integration tests.
- [ ] Adapter must simulate model output and tool calls without provider network calls.
- [ ] Adapter must expose deterministic scripted steps: final answer, tool-call batch, `ask_question`, runtime error, and cancellation where needed by tests.
- [ ] Adapter must record session/run/worktree inputs so tests can assert prompt/context/worktree behavior.
- [ ] At least one Slice 8 vertical integration path must feed fake model output through the real runtime step loop and real `complete_node` tool handling.
- [ ] Real-provider smoke must remain outside automated tests and behind Nikita approval.

## Slice 7: Runtime Completion Hook And `complete_node`

Goal: runtime can identify workflow run context, expose static `complete_node`, validate completion, and stop node run cleanly.

### 7.1 Recon

- [ ] Inspect `server/tools/definitions.go`.
- [ ] Inspect `shared/toolspec/toolspec.go`.
- [ ] Inspect `server/runtime/tool_executor.go`.
- [ ] Inspect `server/runtime/step_executor.go`.
- [ ] Inspect `server/runtimewire` workflow-relevant runtime construction.
- [ ] Inspect `server/sessionruntime` and `server/runtimecontrol` activation/control boundaries.
- [ ] Inspect runtime tests for tool call execution and final answer handling.
- [ ] Identify where tool-call batch preflight belongs.

### 7.2 Red Tests

- [ ] Add tool definition test for static `complete_node` schema.
- [ ] Add runtime test: `complete_node` outside workflow returns not-in-workflow error.
- [ ] Add runtime test: `complete_node` available despite subagent role tool config.
- [ ] Add runtime test: mixed `complete_node` plus another tool is rejected before side effects.
- [ ] Add runtime test: missing transition ID accepted when one outgoing transition group.
- [ ] Add runtime test: missing transition ID rejected when multiple groups.
- [ ] Add runtime test: invalid transition ID rejected.
- [ ] Add runtime test: unknown payload field rejected.
- [ ] Add runtime test: missing edge-required payload rejected after transition selection.
- [ ] Add runtime test: valid completion persists tool result and stops without another model turn.
- [ ] Add runtime test: normal final answer in workflow run gets developer nudge and continues.
- [ ] Add regression test: non-workflow tool execution unchanged.

### 7.3 Implementation

- [ ] Add `complete_node` tool definition and schema.
- [ ] Add workflow run context carrier into runtime/tool execution.
- [ ] Add tool-call preflight for mixed `complete_node`.
- [ ] Add completion validation hook into workflow service.
- [ ] Add terminal signal from tool execution to step loop.
- [ ] Add final-answer invalid-output nudge for workflow runs.
- [ ] Keep prompt/tool definitions centralized.

### 7.4 Verification

- [ ] Run `./scripts/test.sh ./server/runtime/... ./server/tools/... ./shared/toolspec/... ./server/workflow/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: add workflow completion tool`.

## Slice 8: Single-Agent `new_session` Vertical Slice

Goal: one executable workflow node can run asynchronously through queue/session/worktree/runtime with fake model completion.

### 8.1 Red Tests

- [ ] Add integration test for `start -> agent -> done`.
- [ ] Add test task creation then explicit start/schedule action queues first executable run without relying on full manual-move semantics.
- [ ] Add test scheduling executable node ensures task worktree.
- [ ] Add test run claim creates new Builder session.
- [ ] Add test node prompt includes task title/body and bound transition payload.
- [ ] Add fake provider/model flow that drives real runtime step loop and real `complete_node` handling.
- [ ] Add test transition application moves task to done terminal node.
- [ ] Add test commentary and payload stored in transition log.
- [ ] Add test no full `events.jsonl` read occurs.
- [ ] Add CLI-backed integration/smoke test if practical.
- [ ] Add test executable run is not queued/started if role disappeared after workflow validation.
- [ ] Add test role-drift blocker surfaces stable validation code.
- [ ] Add test worker starts and stops with server core lifecycle.
- [ ] Add test shutdown cancels in-flight fake run and preserves interrupted state.
- [ ] Add test two workers do not double-claim same run.

### 8.2 Implementation

- [ ] Add workflow worker loop owned by server core lifecycle.
- [ ] Add server-owned runtime activation/resume path; do not fake frontend controller lease.
- [ ] Add new-session creation path for workflow run.
- [ ] Inject workflow node prompt/developer guidance.
- [ ] Ensure task worktree before workspace-requiring executable run.
- [ ] Connect queue claim to runtime start.
- [ ] Connect valid `complete_node` to transition application.
- [ ] Mark source run/placement completed.
- [ ] Create terminal placement on done node.
- [ ] Surface run state in board/task read models.

### 8.3 Automated Verification

- [ ] Run integration test with fake runtime/model.
- [ ] Run `./scripts/test.sh ./server/workflow/... ./server/workflowruntime/... ./server/worktree/... ./server/runtime/... ./server/metadata/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Confirm no real provider calls happened.
- [ ] Commit slice with message like `feat: run workflow agent node`.

### 8.4 Nikita Approval Gate

- [ ] Ask Nikita before any real-provider smoke test.
- [ ] If approved, define exact provider/model, max expected cost, and stop condition.
- [ ] Prefer POC GUI for Nikita-led QA when available.
- [ ] If no GUI exists yet, keep real-provider QA optional and do not block backend progress.

## Slice 9: Question Pause And Resume Proof

Goal: workflow runs can use existing `ask_question` source of truth or force an ask persistence upgrade.

### 9.1 Red Tests

- [ ] Add test workflow run calls `ask_question`.
- [ ] Add test run state becomes `waiting_for_question`.
- [ ] Add test answer resumes same run/session.
- [ ] Add test resumed run completes with `complete_node`.
- [ ] Add restart/reconciliation test with pending ask.
- [ ] Add failure test for missing/unrehydratable pending ask becoming interrupted.

### 9.2 Implementation

- [ ] Wire ask pause state from runtime to workflow run state.
- [ ] Rehydrate pending ask on startup if existing infra supports it.
- [ ] If infra fails proof, pause slice and design durable ask persistence upgrade as SSOT.
- [ ] Do not create shadow task-question projection table.
- [ ] Derive any task question view from source-of-truth ask state.

### 9.3 Verification

- [ ] Run ask/workflow tests.
- [ ] Run `./scripts/test.sh ./server/tools/askquestion/... ./server/runtime/... ./server/workflow/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message matching implemented path.

## Slice 10: Context-Preservation Modes

Goal: edge context modes work and enforce role/session contract constraints.

### 10.1 Red Tests

- [ ] Add test `new_session` creates separate session.
- [ ] Add test same-role `continue_session` appends/continues source session.
- [ ] Add test cross-role `continue_session` rejected before queueing.
- [ ] Add test `compact_and_continue_session` creates compacted continuation input.
- [ ] Add test compact mode can cross roles.
- [ ] Add test prior transcript history remains immutable.
- [ ] Add test cache-sensitive session setup is not mutated.

### 10.2 Implementation

- [ ] Implement edge context mode selection in scheduler/runtime adapter.
- [ ] Implement session contract comparison for `continue_session`.
- [ ] Implement new-session context injection.
- [ ] Implement compact-then-continue path using existing compaction primitives.
- [ ] Persist context mode used in transition edge snapshot/run metadata.

### 10.3 Verification

- [ ] Run context mode tests.
- [ ] Run `./scripts/test.sh ./server/workflowruntime/... ./server/runtime/... ./server/session/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: add workflow context modes`.

## Slice 11: Approvals And Manual Moves

Goal: edge approval and manual override transitions are durable, validated, and safe.

### 11.1 Red Tests

- [ ] Add test edge requiring approval creates pending transition after source completion.
- [ ] Add test approval by task transition row ID queues stored target edge snapshot.
- [ ] Add test graph edit after pending approval does not alter approved behavior.
- [ ] Add test rejection path marks pending transition rejected.
- [ ] Add test forward manual move validates supplied payload.
- [ ] Add test backward manual move reuses stored payload when valid.
- [ ] Add test missing required payload rejected.
- [ ] Add test continuation-required manual move rejected without valid source session.
- [ ] Add test executable manual target pauses before queue and requires explicit approval.

### 11.2 Implementation

- [ ] Persist pending approval transition and edge snapshots.
- [ ] Add approval service method by task transition row ID.
- [ ] Add rejection/cancel service method if needed by UI/CLI.
- [ ] Implement manual move validation against edge/equivalent metadata.
- [ ] Implement payload reuse for backward moves.
- [ ] Implement explicit approve-before-queue for executable manual target.
- [ ] Update CLI/API/read models for pending approvals.
- [ ] Replace Slice 4 unsupported `builder task move` placeholder with working manual move command.
- [ ] Replace Slice 4 unsupported `builder task approve` placeholder with working approval command.

### 11.3 Verification

- [ ] Run approval/manual move tests.
- [ ] Run `./scripts/test.sh ./server/workflow/... ./server/workflowview/... ./cli/builder/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: add workflow approvals`.

## Slice 12: Fan-Out, Parallel Branches, And Joins

Goal: transition groups can create parallel branch placements and join nodes can deterministically aggregate results.

### 12.1 Red Tests

- [ ] Add test multi-edge transition group creates one placement per edge.
- [ ] Add test parallel branch placements share `parallel_batch_transition_id`.
- [ ] Add test each branch carries `parallel_branch_edge_id`.
- [ ] Add test task can have multiple active placements only for explicit fan-out.
- [ ] Add test branches complete in any order.
- [ ] Add test join waits until all expected branch identities arrive.
- [ ] Add test duplicate branch arrival is idempotently ignored or rejected.
- [ ] Add test missing branch keeps join waiting.
- [ ] Add test deterministic aggregate ordering by branch identity/source node.
- [ ] Add test joined aggregate binds into next node input.
- [ ] Add test terminal branch does not accidentally satisfy unrelated join.

### 12.2 Implementation

- [ ] Implement fan-out transition application.
- [ ] Persist parallel batch and branch edge identity on placements.
- [ ] Implement join readiness query.
- [ ] Implement deterministic aggregation format.
- [ ] Implement join auto-transition through outgoing transition group.
- [ ] Update board/task read models for multiple active placements.
- [ ] Update CLI/API output for branch placements.

### 12.3 Verification

- [ ] Run fan-out/join tests.
- [ ] Run `./scripts/test.sh ./server/workflow/... ./server/workflowview/... ./server/metadata/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: add workflow fanout joins`.

## Slice 13: Recovery, Observability, And Hardening

Goal: operationally usable workflow backend ready for GUI-driven QA.

### 13.1 Red Tests

- [ ] Add restart test for queued run.
- [ ] Add restart test for running run becoming interrupted.
- [ ] Add restart test for interrupted run staying interrupted.
- [ ] Add restart test for waiting-for-question.
- [ ] Add restart test for pending approval.
- [ ] Add test resume interrupted run continues same session/run/worktree.
- [ ] Add test cancel run/task path.
- [ ] Add test fail state with actionable error.
- [ ] Add test role drift at scheduling time.
- [ ] Add test role drift at resume time.
- [ ] Add test CLI/API surfaces stable error code.

### 13.2 Implementation

- [ ] Implement resume service and CLI.
- [ ] Implement cancel service and CLI.
- [ ] Implement fail/error state transitions.
- [ ] Add structured logs around scheduler claims.
- [ ] Add structured logs around run completion.
- [ ] Add structured logs around transition application.
- [ ] Add structured logs around validation blockers.
- [ ] Add structured logs around workflow runtime errors.
- [ ] Add role-drift validation at scheduling and resume.
- [ ] Update read models for interrupted/failed/canceled states.
- [ ] Update docs/dev decisions if new locked decisions emerged.

### 13.3 Verification

- [ ] Run full workflow package tests.
- [ ] Run `./scripts/test.sh ./server/workflow/... ./server/workflowruntime/... ./server/workflowview/... ./server/metadata/... ./cli/builder/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: harden workflow recovery`.

## GUI/POC Integration Checkpoints

These are coordination points for Nikita's parallel GUI POC. Backend API shapes remain mutable before Builder 2.0.

- [ ] After Slice 3, confirm POC GUI can use adapter to read board/task/detail data if useful.
- [ ] After Slice 4, provide internal CLI/API smoke commands for GUI adapter reference.
- [ ] After Slice 8, coordinate first end-to-end fake-runtime GUI smoke if GUI exists.
- [ ] Before real-provider QA, ask Nikita for explicit approval and expected spend.
- [ ] During GUI QA, capture UX/API friction as decisions or follow-up tasks, not undocumented drift.

## Final Pre-Release Verification

Run when all intended workflow slices for release are complete.

- [ ] Run full test suite through `./scripts/test.sh`.
- [ ] Run release build through `./scripts/build.sh --output ./bin/builder`.
- [ ] Run no-LLM coding-agent smoke check from fresh persistence root.
- [ ] Run GUI/POC fake-runtime smoke if available.
- [ ] Run real-provider smoke only if Nikita explicitly approves.
- [ ] Verify docs and decisions are up to date.
- [ ] Verify no unrelated user changes are staged.
- [ ] Verify task/workflow code never reads full `events.jsonl`.
- [ ] Verify all new business logic has tests.
