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

- `server/workflow` does not exist yet and is the locked pure domain package for Slice 1.
- `server/workflowstore`, `server/workflowsvc`, `server/workflowscheduler`, `server/workflowruntime`, `server/workflowrunner`, and `server/workflowview` do not exist yet and are intended package additions in later slices.
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

- [x] Inspect existing Go package naming and error conventions near `server/metadata`, `server/projectview`, and `shared/serverapi`.
- [x] Confirm no existing workflow package or graph validator exists.
- [x] Identify subagent role/config lookup abstraction currently used by runtime/subagents.
- [x] Confirm locked package boundary still has no concrete naming conflict: `server/workflow` pure, with sibling `server/workflowstore`, `server/workflowsvc`, `server/workflowscheduler`, `server/workflowruntime`, and `server/workflowview`.
- [x] Record any discovered naming conflicts in this checklist before coding.

Recon notes: existing packages use package-local services/stores and plain Go errors at the server boundary; shared DTOs validate with `Validate() error`. No workflow package or validator exists. Runtime subagent roles resolve through `shared/config` plus `server/launch`; Slice 1 keeps a pure `workflow.RoleResolver` interface and lets later service/runtime adapters bridge to config. No locked package-name conflicts found.

### 1.2 Red Tests

- [x] Add `server/workflow` package test file for graph validation.
- [x] Define exact validation error code names before implementation and assert them in tests.
- [x] Add valid fixture helper for default workflow: `backlog(start) -> agent -> done(terminal)`.
- [x] Add test: invalid draft workflow can be represented and returns accumulated semantic validation errors without blocking save when hard storage invariants still hold.
- [x] Add test: task-creation/execution validation mode rejects invalid graph with accumulated errors.
- [x] Add test: valid default workflow passes.
- [x] Add test: exactly one start node required.
- [x] Add test: missing start node rejected.
- [x] Add test: multiple start nodes rejected.
- [x] Add test: start node must be non-executable, have no subagent role, have no prompt, and have no output requirements.
- [x] Add test: start node incoming edges are rejected unless spec is explicitly changed.
- [x] Add test: task-creation/execution validation requires start node to have exactly one outgoing transition group.
- [x] Add test: task-creation/execution validation rejects start transition groups with more than one edge.
- [x] Add test: task-creation/execution validation rejects start transition target that is not an agent node.
- [x] Add test: draft workflows may save semantic validation errors but still require hard storage invariants such as one start node, valid identifiers, valid references, and unique keys.
- [x] Add test: terminal node cannot have outgoing edges.
- [x] Add test: terminal node must be non-executable and have no subagent role/prompt.
- [x] Add test: join node must be non-executable and have no subagent role/prompt.
- [x] Add test: join node cannot also be terminal/start.
- [x] Add test: join node outgoing shape is valid for v1, with exactly one transition group unless routing is explicitly added.
- [x] Add test: every node reachable from start.
- [x] Add test: every non-terminal node can reach terminal.
- [x] Add test: detached island rejected.
- [x] Add test: cycles are allowed when terminal remains reachable.
- [x] Add test: self-loop is allowed when terminal remains reachable.
- [x] Add test: edge belongs to an existing transition group in the same workflow, with source derived from the group.
- [x] Add test: duplicate node IDs are rejected.
- [x] Add test: duplicate edge IDs are rejected.
- [x] Add test: missing/invalid node key is rejected; valid keys match `^[a-z][a-z0-9_]{0,63}$`.
- [x] Add test: missing/invalid transition ID is rejected; valid transition IDs match `^[a-z][a-z0-9_]{0,63}$`.
- [x] Add test: missing/invalid edge key is rejected; valid edge keys match `^[a-z][a-z0-9_]{0,63}$`.
- [x] Add test: transition group ID unique workflow-wide.
- [x] Add test: transition IDs unique per source node.
- [x] Add test: transition group must contain at least one edge.
- [x] Add test: edge keys unique per transition group.
- [x] Add test: edge target node must exist.
- [x] Add test: workflow-scoped references cannot cross workflow definitions.
- [x] Add test: output schema field names reject empty/duplicate/invalid identifiers.
- [x] Add test: output schema field descriptions are required and size-limited.
- [x] Add test: output field names are capped at 64 chars and descriptions at 1000 chars.
- [x] Add test: output requirements must reference known source node output fields.
- [x] Add test: input bindings validate source field/task metadata references.
- [x] Add test: prompt/template placeholders validate against bound input names.
- [x] Add test: context mode must be one of `new_session`, `continue_session`, `compact_and_continue_session`.
- [x] Add test: multi-edge fan-out has exactly one unambiguous nearest common join.
- [x] Add test: fan-out branch terminal before join is rejected.
- [x] Add test: nested fan-out before derived join is rejected.
- [x] Add test: cycle before derived join is rejected.
- [x] Add test: agent node requires valid subagent role.
- [x] Add test: missing subagent role returns stable validation code.
- [x] Add test: validation returns all relevant structured errors where safe, not first string-only failure.

Initial validation error code names:

- [x] `workflow.validation.missing_workflow_id`
- [x] `workflow.validation.missing_node_id`
- [x] `workflow.validation.duplicate_node_id`
- [x] `workflow.validation.missing_node_key`
- [x] `workflow.validation.invalid_node_key`
- [x] `workflow.validation.duplicate_node_key`
- [x] `workflow.validation.missing_start_node`
- [x] `workflow.validation.multiple_start_nodes`
- [x] `workflow.validation.invalid_start_node`
- [x] `workflow.validation.invalid_start_outgoing_shape`
- [x] `workflow.validation.terminal_has_outgoing_edge`
- [x] `workflow.validation.terminal_is_executable`
- [x] `workflow.validation.join_is_executable`
- [x] `workflow.validation.invalid_join_node`
- [x] `workflow.validation.invalid_join_outgoing_shape`
- [x] `workflow.validation.node_unreachable_from_start`
- [x] `workflow.validation.non_terminal_cannot_reach_terminal`
- [x] `workflow.validation.missing_transition_group_id`
- [x] `workflow.validation.duplicate_transition_group_id`
- [x] `workflow.validation.empty_transition_group`
- [x] `workflow.validation.missing_transition_id`
- [x] `workflow.validation.invalid_transition_id`
- [x] `workflow.validation.duplicate_transition_id`
- [x] `workflow.validation.edge_transition_group_missing`
- [x] `workflow.validation.missing_edge_id`
- [x] `workflow.validation.duplicate_edge_id`
- [x] `workflow.validation.missing_edge_key`
- [x] `workflow.validation.invalid_edge_key`
- [x] `workflow.validation.duplicate_edge_key`
- [x] `workflow.validation.edge_target_missing`
- [x] `workflow.validation.cross_workflow_reference`
- [x] `workflow.validation.invalid_output_field`
- [x] `workflow.validation.duplicate_output_field`
- [x] `workflow.validation.output_field_description_required`
- [x] `workflow.validation.output_schema_too_large`
- [x] `workflow.validation.unknown_output_requirement`
- [x] `workflow.validation.invalid_input_binding`
- [x] `workflow.validation.invalid_template_placeholder`
- [x] `workflow.validation.invalid_context_mode`
- [x] `workflow.validation.invalid_fanout_join_topology`
- [x] `workflow.validation.agent_role_required`
- [x] `workflow.validation.agent_role_missing`
- [x] `workflow.validation.invalid_node_kind`
- [x] `workflow.validation.invalid_display_name`

### 1.3 Implement Domain Types

- [x] Define typed identifiers for workflow, node, transition group, edge, task, placement, run, transition.
- [x] Define node kinds: start, agent, join, terminal.
- [x] Define context-preservation modes.
- [x] Define node output schema field type with string-only fields.
- [x] Define output requirement type.
- [x] Define output/comment size constants: field name 64 chars, field description 1000 chars, output value 64 KiB, commentary 64 KiB, task comment 256 KiB.
- [x] Define model-facing key regex constant `^[a-z][a-z0-9_]{0,63}$` for node keys, transition IDs, edge keys, output field names, and binding names.
- [x] Define validation context type: draft, task creation, execution.
- [x] Define input binding and template placeholder domain types.
- [x] Define workflow definition aggregate.
- [x] Define project-context validation input with role resolver interface.
- [x] Define validation error code type.
- [x] Define validation error struct with code, message, and related IDs.
- [x] Keep JSON/DB-specific tags out of pure domain types unless unavoidable.

### 1.4 Implement Validation

- [x] Validate required IDs and keys.
- [x] Validate identifier/key formats and size limits.
- [x] Validate display names are trimmed non-empty and capped at 120 chars.
- [x] Validate one start node.
- [x] Validate node kind constraints.
- [x] Validate start, join, and terminal execution config constraints.
- [x] Validate transition group and edge source/target references.
- [x] Validate uniqueness constraints.
- [x] Validate graph reachability from start.
- [x] Validate terminal reachability from every non-terminal node.
- [x] Validate cycles/self-loops do not fail by themselves.
- [x] Validate output requirement references.
- [x] Validate input binding references and template placeholders.
- [x] Validate fan-out join topology restrictions.
- [x] Validate context mode values.
- [x] Validate agent role references through injected resolver.
- [x] Return structured validation errors with stable codes.

### 1.5 Slice Verification

- [x] Run `./scripts/test.sh ./server/workflow/...`.
- [x] Run `./scripts/build.sh --output ./bin/builder`.
- [x] Review `server/workflow` files for package cohesion and no DB/runtime imports.
- [x] Commit slice with message like `feat: add workflow graph validation`.

## Slice 2: Metadata Schema, Queries, And Store

Goal: persist workflow definitions, project links, tasks, placements, runs, transitions, and comments in SQLite.

### 2.1 Recon

- [x] Inspect current migration numbering in `server/metadata/migrations`.
- [x] Inspect `server/metadata/queries.sql` style and sqlc generated package shape.
- [x] Inspect existing store test helpers and fixture DB setup.
- [x] Inspect project/workspace/worktree/session table schemas.
- [x] Decide whether project task counter lives on `projects` or separate counter table; update spec/checklist if changed.

Recon notes: next migration is `00005`. SQLC uses `sqlc.yaml` with `server/metadata/queries.sql` and generated package `server/metadata/sqlitegen`; `sqlc` exists at `/opt/homebrew/bin/sqlc`. Metadata tests use `Open`, `OpenAtPath`, `openDatabaseAtVersionForTest`, `openDatabaseAtPathWithoutMigrationsForTest`, and `newMetadataTestStore`. Project task counter will live on `projects.next_task_seq` with `projects.project_key`, matching the spec's preferred column path and keeping atomic sequence allocation in one project row.

### 2A: Schema, Migrations, And SQLC

Goal: add DB shape and generated query surface before high-level store behavior.

### 2A.1 Red Migration/SQL Tests

- [x] Add migration test for empty DB.
- [x] Add migration test for existing metadata DB fixture.
- [x] Add test that SQLite foreign-key enforcement is enabled in metadata DB setup.
- [x] Add test for project key backfill using default project-name logic.
- [x] Add test for explicit project key storage and validation against `^[A-Z][A-Z0-9]{1,7}$`.
- [x] Add test for project key collision handling.
- [x] Add test that project key changes are rejected after tasks exist.
- [x] Add tests that invalid enum, boolean, timestamp, revision, and protocol counter values fail DB/store validation.
- [x] Add test for invalid JSON rejection or canonicalization for JSON columns.
- [x] Add test that `workflows.start_node_id` does not exist and start node is enforced by partial unique index on node kind.
- [x] Add test that `workflow_edges.source_node_id` does not exist and source is derived from transition group.
- [x] Add test that graph-affecting workflow edits increment `graph_revision`.
- [x] Add test graph revision does not bump on workflow name/description edits unless those values become model-facing.
- [x] Add test running node completion validates against run-start snapshot after workflow graph changes.
- [x] Add test that circular transition/placement references can be inserted with SQLite constraints, or choose nullable/domain-validated references and test that path.
- [x] Add test for atomic task sequence allocation under concurrent creates.
- [x] Add test for task short ID format and uniqueness per project.
- [x] Add test that non-empty project keys are globally unique in the persistence root.
- [x] Add test for task comment size limit.

### 2A.2 Migrations

- [x] Add project key column and task sequence storage.
- [x] Add unique index/constraint for non-empty `projects.project_key`.
- [x] Add `workflows`.
- [x] Add `workflow_nodes`.
- [x] Add `workflow_transition_groups`.
- [x] Add `workflow_edges`.
- [x] Add `project_workflow_links`.
- [x] Add `tasks`.
- [x] Add `task_node_placements`.
- [x] Add `task_runs`.
- [x] Add `task_transitions`.
- [x] Add `task_transition_edges`.
- [x] Add `task_comments`.
- [x] Add indexes listed in spec.
- [x] Add CHECK constraints listed in spec.
- [x] Add `graph_revision` on workflows and observed revision fields on tasks/runs/transitions/snapshots.
- [x] Add run-start effective node contract snapshot storage for completion validation.
- [x] Add task cancellation metadata fields.
- [x] Add protocol violation counters on runs.
- [x] Add run-start snapshot JSON column on runs and validate/canonicalize it with other JSON columns.
- [x] Add project workflow link `unlinked_at_unix_ms` and default partial unique index scoped to linked rows.
- [x] Use a partial unique active link index for `(project_id, workflow_id)` where `unlinked_at_unix_ms = 0`, so terminal-history links can remain while a workflow is later relinked.
- [x] Add partial unique default index on `(project_id)` where `is_default = 1 AND unlinked_at_unix_ms = 0`.
- [x] Add composite foreign keys where practical for workflow-scoped references.
- [x] Add domain/store validation where SQLite cannot enforce scope cleanly.
- [x] Resolve transition/placement circular-reference insertion explicitly; do not leave it as accidental SQLite behavior.

### 2A.3 SQLC

- [x] Add sqlc queries for workflow CRUD.
- [x] Add sqlc queries for graph revision increments on graph-affecting edits.
- [x] Add sqlc queries for nodes/groups/edges CRUD.
- [x] Add sqlc queries for project workflow links.
- [x] Add sqlc queries for project workflow unlink/default semantics.
- [x] Add sqlc queries for project key/task sequence allocation.
- [x] Add sqlc queries for task create/read/list.
- [x] Add sqlc queries for task start and task cancellation.
- [x] Add sqlc queries for placement create/update/read.
- [x] Add sqlc queries for run create/update/read.
- [x] Add sqlc queries for transition log and transition edge snapshots.
- [x] Add sqlc queries for task comments.
- [x] Regenerate sqlc output using existing repo command/pattern.

### 2B: Workflow, Link, And Task Store

Goal: typed transactional store methods for workflows, project links, tasks, placements, and runs.

### 2B.1 Red Store Tests

- [x] Add test for workflow create/update/read/list.
- [x] Add test for workflow creation auto-creating editable `backlog` start node and `done` terminal node.
- [x] Add test for node/transition group/edge persistence.
- [x] Add test for project-workflow link create/list/delete.
- [x] Add test project workflow unlink rejects when non-terminal tasks reference link.
- [x] Add test project workflow unlink soft-disables link and preserves terminal task history.
- [x] Add test unlinking current default requires replacement default when other active links remain.
- [x] Add test for exactly one default workflow link per project.
- [x] Add test invalid workflow can be linked/defaulted but task creation rejects with accumulated validation errors.
- [x] Add test for task create selecting default workflow.
- [x] Add test for task create with explicit workflow.
- [x] Add test for task create creates exactly one start node placement.
- [x] Add test for same task short sequence allowed in different projects but short ID uniqueness enforced within one project.
- [x] Add test that task creation rejects workflow not linked to task project.
- [x] Add test task creation rejects invalid linked/default workflow.
- [x] Add test `task start` applies start node's single outgoing transition group.
- [x] Add test `task start` rejects stale/invalid workflow with accumulated validation errors.
- [x] Add test for placement state transitions.
- [x] Add test for run create/update state fields.
- [x] Add test terminal placement remains active and read models infer done.
- [x] Add test task cancellation records task metadata, interrupts active runs, and suppresses scheduler.

### 2B.2 Store Implementation

- [x] Add store methods wrapping workflow create/default-node creation in one transaction.
- [x] Add store methods for project workflow links and default link changes.
- [x] Add store methods for workflow unlink semantics.
- [x] Add store methods for atomic task sequence allocation and task creation.
- [x] Add store methods for task start and task cancellation.
- [x] Add store methods for placement and run state updates.
- [x] Keep store methods typed; avoid passing raw JSON strings across domain boundaries where avoidable.

### 2C: Transitions, Comments, And Guards

Goal: transition/comment history and guarded graph mutation behavior.

### 2C.1 Red Store Tests

- [x] Add test for transition log create/read ordering.
- [x] Add test for transition edge snapshot persistence.
- [x] Add test transition snapshot stores graph revision plus source node, transition, target node, and effective edge config snapshots.
- [x] Add test every applied transition stores transition-edge snapshots, not only approvals.
- [x] Add test for pending approval snapshot not changing after graph edit.
- [x] Add test for comment add.
- [x] Add test for comment body size limit.
- [x] Add test for comment full-text replace.
- [x] Add test for comment soft-delete.
- [x] Add test for deleted comments hidden by default.
- [x] Add test for guarded graph delete rejected when non-terminal tasks reference graph element.
- [x] Add test physical graph row delete is rejected while any task history references the graph element.
- [x] Add test terminal-only graph references allow archive/hide semantics but not physical row deletion.
- [x] Add test for guarded graph delete allowed when safe.

### 2C.2 Store Implementation

- [x] Add store methods for transition log and edge snapshots.
- [x] Add store methods for task comments.
- [x] Add guarded graph-delete checks.
- [x] Add transactional helpers for multi-row transition/comment operations.

### 2.5 Slice Verification

- [x] Run metadata migration tests.
- [x] Run `./scripts/test.sh ./server/metadata/... ./server/workflow/... ./server/workflowstore/...`.
- [x] Run `./scripts/build.sh --output ./bin/builder`.
- [x] Commit slice with message like `feat: persist workflow tasks`.

## Slice 3: API Contracts, Service Layer, And Read Models

Goal: typed backend service and read surfaces for CLI and POC GUI. Contracts are mutable pre-2.0.

### 3.1 Recon

- [x] Inspect `shared/serverapi` route DTO patterns.
- [x] Inspect `shared/servicecontract` interface patterns.
- [x] Inspect `shared/client` loopback/remote client patterns.
- [x] Inspect `server/transport` route registration patterns.
- [x] Inspect `shared/protocol` method constant and route-name patterns.
- [x] Inspect existing read-model packages such as `server/projectview`, `server/sessionview`, and `server/runtimeview`.

### 3A: DTOs, Service Contracts, Clients, And Transport

Goal: route-shaped typed API surface without large read-model work.

### 3A.1 Red Tests

- [x] Add DTO validation tests for workflow create/update requests.
- [x] Add DTO validation tests for node/edge creation requests.
- [x] Add DTO validation tests for model-facing key regex and display-name `1..120` char limit after trimming.
- [x] Add DTO validation tests for task create/start/move/cancel/comment requests.
- [x] Add DTO/API tests for explicit project key create/update path, or document default-only task key support as a locked decision before skipping.
- [x] Add service test for project default workflow resolution.
- [x] Add service test for workflow validation endpoint using domain validator.
- [x] Add service test for workflow validation endpoint modes: draft, task creation, execution.
- [x] Add service test: cannot create task with workflow not linked to project.
- [x] Add service test: task creation rejects invalid default workflow with accumulated errors.
- [x] Add service test: task start validates current graph and applies start transition when implementation slice enables it.
- [x] Add service test: default workflow resolves within project only.
- [x] Add service test: project workflow unlink rejects non-terminal task references and soft-disables terminal-only links.
- [x] Add loopback client test for at least one workflow route.
- [x] Add remote/transport route test for same route if repo has route test pattern.

### 3A.2 Implementation

- [x] Add `shared/serverapi/workflow.go` DTOs.
- [x] Add validation helpers with stable error codes.
- [x] Add `shared/servicecontract` workflow interface.
- [x] Add `shared/client` workflow client methods.
- [x] Add protocol/method constants if current transport pattern requires them.
- [x] Add `server/workflowsvc` service implementation and compose it from `server/core`.
- [x] Add transport route registration.

### 3B: Workflow Views

Goal: board and task read models for CLI and POC GUI adapter.

### 3B.1 Red Tests

- [x] Add read-model test for board node ordering.
- [x] Add read-model test for board active placement summaries.
- [x] Add read-model test for task detail including placements, runs, transitions, comments.
- [x] Add read-model test for transition history ordering.
- [x] Add read-model test for deleted comments hidden by default.
- [x] Add read-model test for active terminal placement projecting task done.
- [x] Add read-model test for interrupted run reason metadata.
- [x] Add read-model test for canceled task suppressing runnable state.
- [x] Add read-model test for invalid default workflow/task-create validation errors.

### 3B.2 Implementation

- [x] Add `server/workflowview` read-model package.
- [x] Ensure read models do not read session transcripts or `events.jsonl`.
- [x] Document mutable pre-2.0 contract expectation in code comments only where useful.

### 3.4 Slice Verification

- [x] Run `./scripts/test.sh ./shared/serverapi/... ./shared/servicecontract/... ./shared/client/... ./server/workflow/... ./server/workflowstore/... ./server/workflowsvc/... ./server/workflowview/... ./server/transport/...`.
- [x] Run `./scripts/build.sh --output ./bin/builder`.
- [x] Commit slice with message like `feat: expose workflow task APIs`.

## Slice 4: Minimal Workflow And Task CLI

Goal: internal CLI harness and agent-control surface for workflow/task CRUD, comments, and validation. Not Nikita manual QA surface. Full manual moves and approvals are implemented in Slice 11.

### 4.1 Recon

- [x] Inspect command structure in `cli/builder/main.go`.
- [x] Inspect existing command files for flag parsing and client setup.
- [x] Inspect embedded/remote serverbridge usage for non-interactive commands.
- [x] Decide output format conventions for IDs and errors.
- [x] Plan dedicated workflow/task command files; keep `cli/builder/main.go` dispatch thin.

### 4.2 Command Parser/Handler Tests

- [x] Add command test for `builder workflow create`.
- [x] Add command test for `builder workflow list`.
- [x] Add command test that `builder workflow create` auto-creates `backlog` and `done`.
- [x] Add command test for `builder workflow node add` adding extra agent/join nodes.
- [x] Add command test for `builder workflow edge add`.
- [x] Add command test for `builder workflow link`.
- [x] Add command test for `builder workflow unlink`.
- [x] Add command test for `builder workflow default`.
- [x] Add command test for `builder workflow validate`.
- [x] Add command test for `builder workflow inspect`.
- [x] Add command test for `builder task create`.
- [x] Add command test for `builder task start`.
- [x] Add command test for `builder task list`.
- [x] Add command test for `builder task show`.
- [x] Add command test that `builder task move` fails loudly/nonzero until Slice 11.
- [x] Add command test that `builder task approve` fails loudly/nonzero until Slice 11.
- [x] Add command test that `builder task resume` fails loudly/nonzero until runtime/resume slices.
- [x] Add command test for `builder task cancel`.
- [x] Add command test for `builder task comment add`.
- [x] Add command test for `builder task comment list`.
- [x] Add command test for `builder task comment replace`.
- [x] Add command test for `builder task comment delete`.
- [x] Add command test that CLI output includes IDs required by next commands.
- [x] Add command test confirming no `--json` contract is required in first CLI milestone unless later decision changes.
- [x] Add command test for actionable validation error output.

### 4.3 CLI Implementation

- [x] Implement `builder workflow create <name>`.
- [x] Implement `builder workflow list`.
- [x] Implement `builder workflow node add <workflow> --key <node-key> --kind start|agent|join|terminal --prompt <text> --agent <role>`.
- [x] Implement `builder workflow edge add <workflow> --from <source-node-key> --transition <transition-id> --edge-key <edge-key> --to <target-node-key> --context <mode>`.
- [x] Implement `builder workflow link <project> <workflow> [--default]`.
- [x] Implement `builder workflow unlink <project> <workflow>`.
- [x] Implement `builder workflow default <project> <workflow>`.
- [x] Implement `builder workflow validate <workflow> [--project <project>]`.
- [x] Implement `builder workflow inspect <workflow>`.
- [x] Implement `builder task create --title <title> --body <body> [--workflow <workflow>]`.
- [x] Implement `builder task start <short-id>`.
- [x] Implement `builder task list [--project <project>]`.
- [x] Implement `builder task show <short-id>`.
- [x] Add `builder task move ...` as explicit unsupported command until Slice 11, if reserving command shape is useful.
- [x] Add `builder task approve ...` as explicit unsupported command until Slice 11, if reserving command shape is useful.
- [x] Implement `builder task resume <short-id>` as placeholder or explicit unsupported command until runtime slice.
- [x] Implement `builder task cancel <short-id>`.
- [x] Implement `builder task comment add <short-id>`.
- [x] Implement `builder task comment list <short-id>`.
- [x] Implement `builder task comment replace <comment-id> --body <text>`.
- [x] Implement `builder task comment delete <comment-id>`.
- [x] Ensure command errors return nonzero exit and stable message.
- [x] Ensure CLI does not bypass service/store invariants.
- [x] Put workflow/task command handlers in dedicated files or package-local command structs; keep `main.go` dispatch thin.

### 4.4 Internal Smoke Check

- [x] Run smoke against a temp persistent root and embedded-local server wiring, not the user's real daemon/root.
- [x] Create temporary persistence root.
- [x] Write temp Builder config/workspace config defining role `workflow-test` for contextual workflow validation.
- [x] Create or bind a test project/workspace through existing setup commands.
- [x] Create workflow.
- [x] Confirm workflow has auto-created editable `backlog` start node and `done` terminal node.
- [x] Add agent node referencing temp role `workflow-test`.
- [x] Add start-to-agent edge.
- [x] Add agent-to-done edge.
- [x] Link workflow to project as default.
- [x] Validate workflow.
- [x] Create task with title/body.
- [x] Confirm task creation against an invalid workflow fails with accumulated errors.
- [x] List tasks and confirm short ID.
- [x] Show task and confirm start placement.
- [x] Add task comment.
- [x] Replace task comment.
- [x] Delete task comment.
- [x] Confirm `task move`, `task approve`, and `task resume` are unsupported; `task start` and `task cancel` are implemented in Slice 4.
- [x] Capture exact smoke commands in PR/commit notes or checklist update if useful.

### 4.5 Milestone Verification

- [x] Run `./scripts/test.sh ./server/workflow/... ./server/workflowstore/... ./server/workflowsvc/... ./server/workflowview/... ./server/metadata/... ./shared/serverapi/... ./shared/client/... ./server/transport/... ./cli/builder/...`.
- [x] Run `./scripts/build.sh --output ./bin/builder`.
- [x] Confirm no real LLM/provider calls happened.
- [x] Commit slice with message like `feat: add workflow task cli`.
- [ ] Stop for implementation review before runtime internals if needed.

## Slice 5: Task-Owned Worktree Primitive

Goal: task-managed worktree creation/registering without interactive session/controller lease.

### 5.1 Recon

- [x] Inspect `server/worktree` service public methods.
- [x] Inspect `shared/serverapi/worktree.go`.
- [x] Inspect worktree DB tables and existing worktree tests.
- [x] Identify where controller lease assumptions enter worktree creation.
- [x] Identify reusable branch/root collision helpers.

### 5.2 Red Tests

- [x] Add temp repo test for ensuring task worktree creates branch named task short ID.
- [x] Add test for repeated ensure returning existing managed worktree.
- [x] Add test for branch/root name collision handling.
- [x] Add test that task worktree ensure does not require controller lease.
- [x] Add test that task start can seed/ensure the managed worktree before runnable automation is recorded.
- [x] Add test that non-terminal task blocks managed worktree deletion.
- [x] Add test that terminal task allows cleanup when other blockers absent.

### 5.3 Implementation

- [x] Add task-owned worktree ensure method to worktree service or focused adapter.
- [x] Register managed worktree ID on task metadata/store transaction.
- [x] Reuse existing physical worktree creation.
- [x] Reuse existing root/branch collision behavior.
- [x] Add blocker query/service path for non-terminal tasks using managed worktree.
- [x] Keep interactive session switching separate from task worktree creation.

### 5.4 Verification

- [x] Run `./scripts/test.sh ./server/worktree/... ./server/metadata/... ./server/workflow/...`.
- [x] Run `./scripts/build.sh --output ./bin/builder`.
- [x] Commit slice with message like `feat: add task worktree primitive`.

## Slice 5.5: Full Non-Agent E2E Smoke Check

Goal: dedicated no-LLM manual smoke through real CLI/API/backend state before runtime/agent loop work.

- [x] Run smoke against a temp persistent root and embedded-local server wiring, not Nikita's real daemon/root.
- [x] Create or bind a test project/workspace.
- [x] Create a real workflow graph with start, agent, and terminal nodes.
- [x] Link workflow to project as default.
- [x] Validate workflow.
- [x] Create multiple tasks with title/body.
- [x] Inspect board view.
- [x] Inspect task detail view.
- [x] Add, replace, and soft-delete task comments.
- [x] Verify short IDs and stable row IDs needed by future commands.
- [x] Ensure task-owned worktree creation works from the temp root.
- [x] If `task start` is implemented by this point, run it and confirm the worktree is created on start; otherwise confirm the command fails loudly as unsupported until the scheduler slice.
- [x] Verify unsupported manual move/approval commands still fail loudly if Slice 11 is not implemented yet.
- [x] Capture exact smoke commands and results in implementation notes or this checklist.
- [x] Confirm no provider calls happened.

## Slice 6: Scheduler, Runnable Derivation, And Recovery

Goal: scheduler rebuilds runnable workflow work from durable placement/run intent, while pending-work ordering and active runtime ownership stay in live scheduler/runtime state.

### 6.1 Red Tests

- [x] Add config schema/default test for workflow global concurrency defaulting to `5`.
- [x] Add config validation test for invalid workflow concurrency values.
- [x] Add config schema/default test for protocol caps: `[workflow].max_final_answer_violations = 3` and `[workflow].max_invalid_completion_attempts = 5`.
- [x] Add config validation test for invalid protocol cap values.
- [x] Add config surface test covering `[workflow].completion_mode`, `[workflow].concurrency`, `[workflow].max_final_answer_violations`, and `[workflow].max_invalid_completion_attempts` together.
- [x] Add composition-root test that `server/core` wires workflow store/service/scheduler and stops scheduler during core shutdown.
- [x] Add test for `StartTaskAutomation` validating current workflow/project context before scheduling.
- [x] Add test for `StartTaskAutomation` ensuring task worktree before recording runnable automation intent.
- [x] Add test for `StartTaskAutomation` applying the start node's single outgoing transition group.
- [x] Add test for selecting oldest runnable run from automation request time.
- [x] Add test for global concurrency cap.
- [x] Add concurrent scheduler race test proving one live runtime starts per runnable run.
- [x] Add test proving no durable state is written for pending scheduler work or active runtime ownership.
- [x] Add test canceled tasks never become runnable.
- [x] Add stale runtime completion rejected by generation/fence.
- [x] Add test for runnable work rebuilt on startup.
- [x] Add test for orphaned started run becoming interrupted on startup.
- [x] Add test for waiting-for-question retained when ask can rehydrate.
- [x] Add test for waiting-for-question becoming interrupted when ask cannot rehydrate.
- [x] Add test for `PendingAskResolver.CanRehydrate(sessionID, runID, askID)` boundary behavior.
- [x] Add test that pending ask recovery does not scan or load full `events.jsonl`.
- [x] Add test scheduling validation blockers become interrupted runs with stable reason metadata.
- [x] Add test for pending approval retained on startup.
- [x] Add test that interrupted runs are never auto-retried.
- [x] Add test shutdown begins: scheduler stops taking new claims while preserving in-flight interruption semantics.
- [x] Add transaction rollback test for unsuccessful transition application.

### 6.2 Implementation

- [x] Add scheduler service under `server/workflowscheduler`.
- [x] Implement `StartTaskAutomation` use case in `server/workflowsvc`.
- [x] Wire `server/workflowstore`, `server/workflowsvc`, and `server/workflowscheduler` from `server/core` composition root.
- [x] Add config fields/read for `[workflow]` config surface: `completion_mode`, `concurrency`, `max_final_answer_violations`, and `max_invalid_completion_attempts`.
- [x] Add config validation for invalid workflow config values.
- [x] Implement runnable work derivation from active placements, automation intent, terminal outcomes, pending ask/approval state, and task cancellation.
- [x] Keep pending-work ordering and active runtime ownership in memory.
- [x] Define worker identity format and stale live-ownership strategy.
- [x] Add DB-busy claim retry/backoff strategy.
- [x] Store and check run generation/fence for stale runtime callbacks.
- [x] Implement completion path requiring matching run generation.
- [x] Implement startup reconciliation.
- [x] Integrate `PendingAskResolver.CanRehydrate(sessionID, runID, askID)` into startup reconciliation before preserving waiting-for-question.
- [x] Implement atomic transition application transaction.
- [x] Add structured logs for scheduler selection/recovery/transition outcomes.

### 6.3 Verification

- [x] Run scheduler tests with race-sensitive cases repeatedly.
- [x] Run `./scripts/test.sh ./server/workflow/... ./server/workflowstore/... ./server/workflowsvc/... ./server/workflowscheduler/... ./server/metadata/...`.
- [x] Run `./scripts/build.sh --output ./bin/builder`.
- [x] Commit slice with message like `feat: add workflow scheduler`.

## Runtime Test Adapter Boundary

Define this boundary before Slice 7 implementation, then reuse it in Slice 8. Fake provider/model transport is allowed; fake workflowruntime that bypasses real runtime/tool execution is allowed only for scheduler tests, not vertical completion tests.

- [x] Define fake provider/model adapter interface before Slice 7 runtime integration tests.
- [x] Adapter must simulate model output and tool calls without provider network calls.
- [x] Adapter must expose deterministic scripted steps: final answer, tool-call batch, `ask_question`, runtime error, and cancellation where needed by tests.
- [x] Adapter must record session/run/worktree inputs so tests can assert prompt/context/worktree behavior.
- [x] At least one Slice 8 vertical integration path must feed fake model output through the real runtime step loop and real workflow completion handling.
- [x] Real-provider smoke must remain outside automated tests and behind Nikita approval.

## Slice 7: Workflow Prompting And Completion Runtime

Goal: runtime can identify workflow run context, inject workflow-mode instructions, expose structured-output or dynamic tool completion, validate completion, and stop node run cleanly.

### 7.1 Recon

- [x] Inspect `server/tools/definitions.go`.
- [x] Inspect `shared/toolspec/toolspec.go`.
- [x] Inspect `server/runtime/tool_executor.go`.
- [x] Inspect `server/runtime/step_executor.go`.
- [x] Inspect `server/runtimewire` workflow-relevant runtime construction.
- [x] Inspect `server/runprompt` headless launch/wiring/progress patterns for reusable workflow runtime pieces.
- [x] Inspect `prompts/headless_mode_prompt.md` and headless prompt injection path before designing workflow mode prompt.
- [x] Inspect `server/sessionruntime` and `server/runtimecontrol` activation/control boundaries.
- [x] Inspect runtime tests for tool call execution and final answer handling.
- [x] Identify where tool-call batch preflight belongs.
- [x] Inspect `server/llm` structured output request support and reviewer structured-output usage.

### 7.2 Red Tests

- [x] Add prompt test for `prompts/workflow_mode_prompt.md` content and injection.
- [x] Add prompt test that workflow mode prompt is injected through the workflowruntime/headless runtime preparation path before the node prompt, not assembled by scheduler/CLI.
- [x] Add config test for temporary global completion mode `auto|structured_output|tool` with no workflow/node override.
- [x] Add test that `auto` selects structured output when provider capabilities support it and dynamic tool mode otherwise.
- [x] Add test that forced `structured_output` fails fast with actionable error when unsupported.
- [x] Add test that forced `tool` always uses dynamic tool mode.
- [x] Add schema generation test for structured output with top-level custom fields and descriptions.
- [x] Add schema generation test for dynamic `complete_node` tool with top-level custom fields and descriptions.
- [x] Add runtime test: `complete_node` outside workflow returns not-in-workflow error.
- [x] Add runtime test: `complete_node` tool schema is not advertised outside workflow tool-completion runs.
- [x] Add runtime test: `complete_node` available despite subagent role tool config.
- [x] Add runtime test: mixed `complete_node` plus another tool is rejected before side effects.
- [x] Add runtime test: two `complete_node` calls in one assistant response are rejected before side effects.
- [x] Add runtime test: any side-effecting tool mixed with `complete_node` does not execute.
- [x] Add runtime test: structured output completion accepted when configured/supported.
- [x] Add runtime test: `auto` falls back to tool mode when structured output is unsupported.
- [x] Add runtime test: missing transition ID accepted when one outgoing transition group.
- [x] Add runtime test: missing transition ID rejected when multiple groups.
- [x] Add runtime test: empty `transition_id` is rejected when transition ID is required.
- [x] Add runtime test: invalid transition ID rejected.
- [x] Add runtime test: unknown output field rejected.
- [x] Add runtime test: non-string output value rejected.
- [x] Add runtime test: oversized output/commentary rejected.
- [x] Add runtime test: missing and empty edge-required output rejected after transition selection.
- [x] Add runtime test: unknown output field plus missing required output returns deterministic structured errors.
- [x] Add runtime test: no outgoing transition group gives actionable validation error.
- [x] Add runtime test: valid completion persists structured/tool completion result and stops without another model turn.
- [x] Add runtime test: normal final answer in workflow run gets developer nudge and continues.
- [x] Add runtime test: consecutive final-answer protocol violations hit cap and interrupt the run.
- [x] Add runtime test: consecutive invalid completions hit cap and interrupt the run.
- [x] Add regression test: non-workflow tool execution unchanged.

### 7.3 Implementation

- [x] Add workflow-mode prompt source and runtime injection.
- [x] Add temporary global workflow completion mode config.
- [x] Implement completion mode precedence: global config only, `auto` provider-capability check, forced structured-output error on unsupported provider, forced tool mode bypassing structured output.
- [x] Implement workflow protocol cap config under `[workflow]` with defaults `max_final_answer_violations = 3` and `max_invalid_completion_attempts = 5`.
- [x] Add structured-output schema generator.
- [x] Add dynamic `complete_node` tool schema generator.
- [x] Add workflow run context carrier into runtime structured-output/tool execution.
- [x] Add tool-call preflight for mixed `complete_node`.
- [x] Add exactly-one-completion preflight before any tool side effects.
- [x] Add completion validation hook into workflow service for both modes.
- [x] Add terminal signal from tool execution to step loop.
- [x] Add final-answer invalid-output nudge for workflow runs.
- [x] Persist/increment protocol violation counters and interrupt after cap.
- [x] Keep prompt/tool definitions centralized.

### 7.4 Verification

- [x] Run `./scripts/test.sh ./server/runtime/... ./server/tools/... ./shared/toolspec/... ./server/workflow/...`.
- [x] Run `./scripts/build.sh --output ./bin/builder`.
- [x] Commit slice with message like `feat: add workflow completion tool`.

## Slice 8: Single-Agent `new_session` Vertical Slice

Goal: one executable workflow node can run asynchronously through scheduler/session/worktree/runtime with fake model completion.

### 8.1 Red Tests

- [x] Add integration test for `backlog(start) -> agent -> done(terminal)`.
- [x] Add test task creation then explicit `task start` action marks first executable run runnable without relying on full manual-move semantics.
- [x] Add test scheduling executable node ensures task worktree.
- [x] Add test scheduler start creates new Builder session.
- [x] Add test workflow-mode prompt includes task title/body, node identity, completion mode, and bound transition output values.
- [x] Add fake provider/model flow that drives real runtime step loop and structured-output completion.
- [x] Add fake provider/model flow that drives real runtime step loop and dynamic `complete_node` completion.
- [x] Add test transition application moves task to done terminal node.
- [x] Add test commentary and output values stored in transition log.
- [x] Add test no full `events.jsonl` read occurs.
- [x] Add CLI-backed integration/smoke test if practical. Not rerun for Slice 8 because automated fake-model backend integration covers the async runtime path without provider calls; CLI/API harness was already covered in Slice 4 and Slice 5.5.
- [x] Add test executable run is not started if role disappeared after workflow validation.
- [x] Add test role-drift blocker surfaces stable validation code.
- [x] Add test worker starts and stops with server core lifecycle.
- [x] Add test shutdown cancels in-flight fake run and preserves interrupted state.
- [x] Add test two workers do not double-start same run.

### 8.2 Implementation

- [x] Add workflow worker loop owned by server core lifecycle.
- [x] Add server-owned runtime activation/resume path, reusing suitable `builder run`/headless launch and runtime wiring pieces; do not fake frontend controller lease.
- [x] Add new-session creation path for workflow run.
- [x] Inject workflow node prompt/developer guidance.
- [x] Ensure task worktree before workspace-requiring executable run.
- [x] Connect runnable scheduler selection to runtime start.
- [x] Connect valid structured-output and dynamic-tool completion to transition application.
- [x] Mark source run/placement completed.
- [x] Create terminal placement on done node.
- [x] Surface run state in board/task read models.

### 8.3 Automated Verification

- [x] Run integration test with fake provider/model adapter through real runtime/tool handling.
- [x] Run `./scripts/test.sh ./server/workflow/... ./server/workflowstore/... ./server/workflowsvc/... ./server/workflowscheduler/... ./server/workflowruntime/... ./server/workflowrunner/... ./server/worktree/... ./server/runtime/... ./server/metadata/...`.
- [x] Run `./scripts/build.sh --output ./bin/builder`.
- [x] Confirm no real provider calls happened.
- [x] Commit slice with message like `feat: run workflow agent node`.

### 8.4 Nikita Approval Gate

- [ ] Ask Nikita before any real-provider smoke test.
- [ ] If approved, define exact provider/model, max expected cost, and stop condition.
- [ ] Prefer POC GUI for Nikita-led QA when available.
- [ ] If no GUI exists yet, keep real-provider QA optional and do not block backend progress.

## Slice 9: Question Pause And Resume

Goal: workflow runs use existing `ask_question` source of truth for pause/resume, using the rehydration boundary proven during Slice 6 or stopping to upgrade ask persistence.

### 9.1 Red Tests

- [ ] Add test workflow run calls `ask_question`.
- [ ] Add test run state becomes `waiting_for_question`.
- [ ] Add test answer resumes same run/session.
- [ ] Add test resumed run completes with workflow completion.
- [ ] Add restart/reconciliation test with pending ask using Slice 6 rehydration boundary.
- [ ] Add failure test for missing/unrehydratable pending ask becoming interrupted if not already covered in Slice 6.

### 9.2 Implementation

- [ ] Wire ask pause state from runtime to workflow run state.
- [ ] Rehydrate pending ask on startup through the `PendingAskResolver` path proven in Slice 6.
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
- [ ] Add test same-role `continue_session` keeps immutable persisted session setup when current role config drifted.
- [ ] Add test cross-role `continue_session` rejected before scheduler start.
- [ ] Add test `compact_and_continue_session` creates compacted continuation input.
- [ ] Add test compact mode can cross roles.
- [ ] Add test prior transcript history remains immutable.
- [ ] Add test cache-sensitive session setup is not mutated.

### 10.2 Implementation

- [ ] Implement edge context mode selection in scheduler/runtime adapter.
- [ ] Implement same-role check for `continue_session`; do not require current role config to match immutable session setup.
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
- [ ] Add test approval by task transition row ID starts stored target edge snapshot.
- [ ] Add test duplicate approval is idempotent.
- [ ] Add test multi-edge transition group waits as a whole when any edge requires approval.
- [ ] Add test graph edit after pending approval does not alter approved behavior.
- [ ] Add test rejection path marks pending transition rejected.
- [ ] Add test forward manual move validates supplied output values.
- [ ] Add test backward manual move reuses stored output values when valid.
- [ ] Add test missing required output rejected.
- [ ] Add test continuation-required manual move rejected without valid source session.
- [ ] Add test executable manual target pauses before automation and requires explicit approval.

### 11.2 Implementation

- [ ] Persist pending approval transition and edge snapshots.
- [ ] Ensure every applied transition stores transition-edge snapshots; approvals only change pending/approval behavior.
- [ ] Add approval service method by task transition row ID.
- [ ] Make approval idempotent and apply the whole transition group when any selected edge requires approval.
- [ ] Add rejection/cancel service method if needed by UI/CLI.
- [ ] Implement manual move validation against edge/equivalent metadata.
- [ ] Implement output value reuse for backward moves.
- [ ] Implement explicit approve-before-automation for executable manual target.
- [ ] Update CLI/API/read models for pending approvals.
- [ ] Replace Slice 4 unsupported `builder task move` placeholder with working manual move command.
- [ ] Replace Slice 4 unsupported `builder task approve` placeholder with working approval command.

### 11.3 Verification

- [ ] Run approval/manual move tests.
- [ ] Run `./scripts/test.sh ./server/workflow/... ./server/workflowsvc/... ./server/workflowview/... ./cli/builder/...`.
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
- [ ] Add test static derivation finds one unambiguous nearest common join.
- [ ] Add test ambiguous nearest common join topology rejected.
- [ ] Add test nested fan-out before join rejected.
- [ ] Add test cycle before join rejected.
- [ ] Add test terminal before join rejected.
- [ ] Add test join waits until all expected branch identities arrive.
- [ ] Add test duplicate branch arrival is idempotently ignored or rejected.
- [ ] Add test missing branch keeps join waiting.
- [ ] Add test deterministic aggregate ordering by branch identity/source node.
- [ ] Add test joined aggregate binds into next node input.
- [ ] Add test terminal branch does not accidentally satisfy unrelated join.

### 12.2 Implementation

- [ ] Implement fan-out transition application.
- [ ] Implement static join derivation validation for multi-edge transition groups.
- [ ] Persist parallel batch and branch edge identity on placements.
- [ ] Use persisted transition-edge snapshot rows from the accepted fan-out transition as join expected wait set.
- [ ] Implement join readiness query against expected fan-out edge set.
- [ ] Reject or explicitly pause manual moves into/out of active parallel batches until dedicated UX exists.
- [ ] Implement deterministic aggregation format.
- [ ] Implement join auto-transition through outgoing transition group.
- [ ] Update board/task read models for multiple active placements.
- [ ] Update CLI/API output for branch placements.

### 12.3 Verification

- [ ] Run fan-out/join tests.
- [ ] Run `./scripts/test.sh ./server/workflow/... ./server/workflowstore/... ./server/workflowview/... ./server/metadata/...`.
- [ ] Run `./scripts/build.sh --output ./bin/builder`.
- [ ] Commit slice with message like `feat: add workflow fanout joins`.

## Slice 13: Recovery, Observability, And Hardening

Goal: operationally usable workflow backend ready for GUI-driven QA.

### 13.1 Red Tests

- [ ] Add restart test for runnable work rebuild.
- [ ] Add restart test for orphaned started run becoming interrupted.
- [ ] Add restart test for interrupted run staying interrupted.
- [ ] Add restart test for waiting-for-question.
- [ ] Add restart test for pending approval.
- [ ] Add test canceled task remains unscheduled after restart.
- [ ] Add test resume interrupted run continues same session/run/worktree.
- [ ] Add test scheduling validation blocker records interrupted reason metadata.
- [ ] Add test user cancel records interrupted outcome with cancel reason.
- [ ] Add test runtime error records interrupted outcome with error reason.
- [ ] Add test role drift at scheduling time.
- [ ] Add test role drift at resume time.
- [ ] Add test CLI/API surfaces stable error code.

### 13.2 Implementation

- [ ] Implement resume service and CLI.
- [ ] Implement task cancellation service and CLI with task-level cancellation metadata.
- [ ] Implement cancel-as-interrupted service behavior for active runs.
- [ ] Implement runtime-error interruption transitions.
- [ ] Add structured logs around scheduler runnable selection.
- [ ] Add structured logs around run completion.
- [ ] Add structured logs around transition application.
- [ ] Add structured logs around validation blockers.
- [ ] Add structured logs around workflow runtime errors.
- [ ] Add role-drift validation at scheduling and resume.
- [ ] Update read models for interrupted states and interruption reasons.
- [ ] Update docs/dev decisions if new locked decisions emerged.

### 13.3 Verification

- [ ] Run full workflow package tests.
- [ ] Run `./scripts/test.sh ./server/workflow/... ./server/workflowstore/... ./server/workflowsvc/... ./server/workflowscheduler/... ./server/workflowruntime/... ./server/workflowview/... ./server/metadata/... ./cli/builder/...`.
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
