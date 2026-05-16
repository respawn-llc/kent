# GUI Workflow Backend Slice Plan

Status: planning contract and executable checklist for backend work needed before real GUI workflow integration.

Date: 2026-05-16.

Implementation gate: do not start Slice 0 until Nikita accepts this checklist shape and explicitly asks to start backend implementation.

## Purpose

Define the Go server API/read-model/action slices needed by the Builder desktop GUI workflow MVP. The GUI stays a remote-control client. The server remains authoritative for projects, workspaces, workflows, tasks, runs, scheduling, questions, approvals, worktrees, sessions, validation, and persistence.

Contracts in this document are mutable before Builder 2.0, but each slice should be implemented test-first and kept buildable. Checklist status in this document is the handoff-safe source of truth for backend GUI workflow progress.

## Scope

In scope:

- Connectivity, readiness, capabilities, and attach diagnostics for desktop startup.
- Home project list and global attention inbox read models.
- Project create/admin support needed by GUI project creation, including editable project key.
- Project workspace list/default workspace support.
- Workflow picker and selected-workflow board read model.
- Group-aware board data.
- Task creation/editing in Backlog with selected main/source workspace.
- Drag-to-start, interrupt, cancel, resume, approval, and question actions.
- Task detail read model, paginated activity feed, comments, and terminal teleport target data.
- WebSocket subscription/invalidation events for project/workflow/task changes.

Out of scope:

- GUI workflow authoring.
- Browser-hosted web UI implementation.
- Built-in GUI chat replacing teleport.
- Reject approval UX.
- Full transcript/event replay in GUI.
- Loading full `events.jsonl`.

## Existing Backend State

Project/workspace API already exists:

- `shared/serverapi/project_view.go` exposes `ProjectList`, path resolution, binding plan, project create, workspace attach/rebind, project overview, and project session list.
- `server/projectview/service.go` implements those methods over `server/metadata`.
- `server/metadata/migrations/00001_metadata_core.up.sql` has `projects`, `workspaces`, `worktrees`, and `sessions`; `workspaces.is_primary` exists.
- `server/metadata/migrations/00005_workflow_orchestration.up.sql` added `projects.project_key` and `projects.next_task_seq`.
- `server/metadata/store.go` has `SetProjectKey`, project key allocation/backfill behavior, and workspace listing.

Workflow/task API already exists:

- `shared/serverapi/workflow.go` exposes workflow CRUD/link/validate, task create/start/resume/approve/move/cancel/comment/list/get, board get, and task get.
- `shared/servicecontract/services.go`, `shared/client/workflow.go`, `shared/client/remote.go`, `server/transport/gateway_unary_handlers.go`, and `shared/rpccontract/routes.go` wire workflow JSON-RPC routes.
- `server/workflowsvc` owns use-case orchestration.
- `server/workflowstore` owns persistence, task creation/start, transitions, approvals, comments, manual moves, run interruption/resume, and waiting ask markers.
- `server/workflowview` owns current board/task read models.
- `server/workflowscheduler`, `server/workflowrunner`, and `server/workflowruntime` already exist for async runtime/scheduler slices.
- `server/worktree.Service.EnsureTaskWorktree` creates the managed task worktree on task start and currently chooses primary project workspace.

Current gaps for GUI MVP:

- `ProjectCreateRequest` does not expose project key.
- Project/home list DTOs do not expose project key, attention count, task count, workflow count, or GUI-friendly primary workspace metadata.
- Home project list and attention inbox are not paginated.
- `WorkflowTaskCreateRequest` currently requires non-empty `body`, but GUI body/details is optional.
- Tasks do not store selected pre-start source/main workspace. `tasks.managed_worktree_id` appears only after worktree creation.
- `EnsureTaskWorktree` chooses primary workspace instead of task-selected source workspace.
- Current board endpoint returns all workflows for a project and is not selected-workflow, group-aware, paginated, or status/action rich.
- Workflow visual/node groups are not modeled separately from transition groups.
- No project/workflow live-update subscription exists.
- No global task attention inbox exists.
- No public task interrupt action exists.
- Resume is task-scoped and rejects multiple interrupted runs; GUI needs per-run controls for fan-out/multi-active tasks.
- Task detail returns arrays, not a paginated newest-first activity feed.
- Approval snapshot fields exist durably, but current DTO omits enough source/target/graph/requirements data for GUI approval UI.
- Question details are session-scoped through ask view; task attention/detail needs a task-scoped bridge to those ask records.
- No single teleport target endpoint returns task run/session attach identifiers.

## Contract Rules

- Use `shared/serverapi` DTOs with explicit JSON tags for new GUI-facing wire shapes.
- Keep existing `clientui` DTOs available for CLI/TUI, but do not base new GUI read models on `time.Time` or untagged structs.
- Prefer adding narrow GUI-ready read models over expanding generic legacy list methods when old shapes would become ambiguous.
- Use unix-millisecond timestamps in new GUI DTOs.
- Use stable enum strings for statuses and attention types.
- Use paginated read models for Home, attention, task activity, and any potentially unbounded lists.
- Return server-native status/action facts; GUI may render them, but server owns task/run/workflow state truth.
- Do not include raw transcript entries or require `events.jsonl`.
- Mutations return enough changed IDs for immediate cache updates, but GUI still refetches authoritative read models after mutation or reconnect.
- WebSocket events are invalidation/resource-change events, not full board payloads.

## Connectivity, Readiness, Capabilities

Goal: desktop can attach to the configured server and know what is safe to show.

New server API:

- `server.readiness.get`
  - Request: `ServerReadinessRequest{}`
  - Response: `ServerReadinessResponse`
  - Fields:
    - `ready bool`
    - `server_id string`
    - `server_version string`
    - `protocol_version string`
    - `auth_ready bool`
    - `auth_required bool`
    - `endpoint string`
    - `causes []ServerReadinessCause`
  - Cause fields:
    - `code string`
    - `severity string`
    - `summary string`
    - `next_action string`
    - `diagnostic_id string`

- `server.capabilities.get`
  - Request: `ServerCapabilitiesRequest{}`
  - Response: `ServerCapabilitiesResponse`
  - Fields:
    - `capabilities []ServerCapability`
    - `server_version string`
    - `protocol_version string`
  - Capability fields:
    - `id string`
    - `available bool`
    - `reason string`
    - `required_for_mvp bool`

Initial GUI-relevant capability IDs:

- `gui.home`
- `project.key.create`
- `project.workspace.list`
- `workflow.board.selected`
- `workflow.board.groups`
- `workflow.live_updates`
- `workflow.task.source_workspace`
- `workflow.task.create`
- `workflow.task.edit_backlog`
- `workflow.task.start`
- `workflow.task.interrupt`
- `workflow.task.resume`
- `workflow.task.cancel`
- `workflow.attention.list`
- `workflow.task.activity`
- `workflow.task.comments`
- `workflow.task.teleport`

Implementation notes:

- Keep JSON-RPC handshake as the protocol-version gate.
- Readiness is summary-first for GUI UX; deep diagnostics stay in local logs.
- Missing/expired auth uses the same generic readiness failure path as other startup blockers.
- Route auth policy should allow pre-server-auth readiness/auth status where needed, but mutating actions stay server-auth gated.

Tests:

- Readiness returns protocol/server version and stable causes.
- Capability list includes required MVP capabilities.
- Capability unavailable returns reason and disables dependent action in adapter tests.
- Auth missing produces generic readiness cause, not special auth-only flow.

## Home, Project Admin, Project Key, Workspaces

Goal: Home can show projects and workspace context, and GUI project creation can set editable project key.

New server API:

- `project.home.list`
  - Request: `ProjectHomeListRequest`
  - Fields:
    - `page_size int`
    - `page_token string`
  - Response: `ProjectHomeListResponse`
  - Fields:
    - `projects []ProjectHomeSummary`
    - `next_page_token string`
    - `generated_at_unix_ms int64`
    - `latest_event_sequence int64`

- `project.workspace.list`
  - Request: `ProjectWorkspaceListRequest`
  - Fields:
    - `project_id string`
  - Response: `ProjectWorkspaceListResponse`
  - Fields:
    - `project_id string`
    - `workspaces []ProjectWorkspaceSummary`
    - `default_workspace_id string`

Changed server API:

- `ProjectCreateRequest`
  - Add `project_key string`
  - Keep `display_name string`
  - Keep `workspace_root string`
  - Validate non-empty `project_key` with project-key rules: uppercase, globally unique, 2-8 chars, `^[A-Z][A-Z0-9]{1,7}$`.
  - Omitted `project_key` keeps existing allocation/backfill behavior for current callers.

- `ProjectCreateResponse`
  - Include project key in `ProjectBinding`.

- `ProjectBinding`, `ProjectListResponse`, and `ProjectGetOverviewResponse`
  - Include `project_key` where project identity is shown.

New DTO fields:

- `ProjectHomeSummary`
  - `project_id string`
  - `project_key string`
  - `display_name string`
  - `primary_workspace ProjectWorkspaceSummary`
  - `default_workflow_id string`
  - `default_workflow_name string`
  - `default_workflow_valid bool`
  - `updated_at_unix_ms int64`
  - `task_count int`
  - `attention_count int`
  - `workflow_count int`

- `ProjectWorkspaceSummary`
  - `workspace_id string`
  - `display_name string`
  - `root_path string`
  - `availability string`
  - `is_primary bool`
  - `updated_at_unix_ms int64`

Store/query changes:

- Add paginated project-home query sorted by latest activity descending.
- Latest activity should consider project/workspace/session/task activity where cheap and indexed.
- Add the monotonic project/global event sequence foundation used by Home watermarks; Slice 2 wires the WebSocket subscription on top.
- Add an attention-count helper over the same durable sources later exposed by the Slice 4 attention inbox: pending approvals, waiting asks, interrupted runs, and read-model-only validation blockers.
- Project key creation should happen in the same transaction as project/workspace creation.
- Expose project key collision as typed validation error, not raw SQLite unique text.
- `project.workspace.list` can initially reuse existing `ListProjectWorkspaces` query.
- Do not add primary-workspace switching unless GUI needs it; first workspace remains primary/default.

Dependency note:

- Slice 1 owns Home fields and the shared foundations they need, but not every consumer API. `latest_event_sequence` is backed by the event sequence foundation until Slice 2 adds subscriptions. `attention_count` is backed by a durable count helper until Slice 4 adds paginated attention lists and actions.

Tests:

- Project create accepts explicit key and rejects collision/invalid key.
- Project create remains atomic when key insert fails.
- Existing create clients keep working if `project_key` omitted, using current default/backfill behavior.
- Home list is paginated and sorted by latest activity descending.
- Home summary includes project key and primary workspace metadata.
- Home summary includes default workflow ID/name/validity so clicking a project can open the default workflow board without an extra workflow list round trip.
- Home summary represents no valid default workflow as `default_workflow_valid=false`; GUI opens the blocker state and keeps New Task disabled.
- Home summary includes event watermark and attention count from the durable attention sources available at that slice.
- Workspace list returns primary/default workspace and all attached workspaces.

## Task Source Workspace And Backlog Editing

Goal: GUI can create Backlog tasks with optional body and selected main/source workspace before automation starts.

Schema/store changes:

- Add `tasks.source_workspace_id TEXT`.
- Enforce source workspace belongs to the task project with a composite database invariant, such as `FOREIGN KEY (project_id, source_workspace_id) REFERENCES workspaces(project_id, id)` plus the required unique index on `workspaces(project_id, id)`, or an equivalent checked transaction if migration constraints force it.
- Backfill existing tasks to project primary workspace when possible; allow empty only for legacy/edge cases and fall back to primary during worktree creation.
- Treat `source_workspace_id` as immutable after task start.
- Change task body storage validation so empty body is valid.

Changed server API:

- `WorkflowTaskCreateRequest`
  - Add `source_workspace_id string`
  - Keep `project_id`, `workflow_id`, `title`, `body`, `source_url`
  - `title` required.
  - `body` optional.
  - `source_workspace_id` optional for existing callers; service defaults to project primary workspace when omitted.

- `WorkflowTaskSummary`
  - Add `source_workspace_id string`
  - Add `source_workspace ProjectWorkspaceSummary`
  - Add `body_preview string`
  - Add `created_at_unix_ms int64`
  - Add `updated_at_unix_ms int64`

- `WorkflowTaskDetail`
  - Include full `body`, `source_url`, and source workspace.
  - Keep `source_url` backend-only; GUI hides it in MVP.

New server API:

- `workflow.task.update`
  - Request: `WorkflowTaskUpdateRequest`
  - Fields:
    - `task_id string`
    - `title string`
    - `body string`
    - `source_workspace_id string`
  - Response: `WorkflowTaskUpdateResponse`
  - Fields:
    - `task WorkflowTaskSummary`

Rules:

- Update title/body/source workspace only while task is still in Backlog/start placement and has no runs.
- Source workspace must belong to the task project.
- Worktree service uses `tasks.source_workspace_id` first, then primary workspace fallback only for legacy rows.
- GUI label is "main workspace"; backend field can be `source_workspace_id` to match worktree source semantics.

Tests:

- Create task with selected workspace stores `source_workspace_id`.
- Create task without body succeeds.
- Create task without workspace defaults to primary workspace.
- Create task rejects workspace from another project.
- Update task fields works before start.
- Update rejects after task start or cancellation.
- `EnsureTaskWorktree` creates worktree from task source workspace.

## Workflow Picker, Board, Groups, Live Updates

Goal: selected project/workflow board can render columns, groups, cards, and live invalidation.

Changed server API:

- `workflow.board.get`
  - Request: `WorkflowBoardRequest`
  - Fields:
    - `project_id string`
    - `workflow_id string`
    - `done_preview_limit int`
    - `page_size int`
    - `page_token string`
  - Response: `WorkflowBoardResponse`
  - Fields:
    - `board WorkflowBoard`

New or expanded DTO fields:

- `WorkflowBoard`
  - `project ProjectBoardProject`
  - `selected_workflow WorkflowPickerItem`
  - `workflows []WorkflowPickerItem`
  - `groups []WorkflowBoardGroup`
  - `columns []WorkflowBoardColumn`
  - `cards []WorkflowBoardTaskCard`
  - `done_preview []WorkflowBoardTaskCard`
  - `next_page_token string`
  - `generated_at_unix_ms int64`
  - `latest_event_sequence int64`

- `WorkflowPickerItem`
  - `workflow_id string`
  - `display_name string`
  - `description string`
  - `graph_revision int64`
  - `is_project_default bool`
  - `valid_for_task_creation bool`
  - `validation_errors []WorkflowValidationError`
  - `unlinked_at_unix_ms int64`

- `WorkflowBoardGroup`
  - `group_id string`
  - `key string`
  - `display_name string`
  - `sort_order int`
  - `node_ids []string`

- `WorkflowBoardColumn`
  - `node WorkflowBoardNodeSummary`
  - `group_id string`
  - `sort_order int`
  - `is_backlog bool`
  - `is_done bool`
  - `task_count int`

- `WorkflowBoardNodeSummary`
  - `node_id string`
  - `key string`
  - `kind string`
  - `display_name string`
  - `assignee_role string`
  - `sort_order int`

- `WorkflowBoardTaskCard`
  - `task_id string`
  - `short_id string`
  - `title string`
  - `workflow_id string`
  - `active_node_ids []string`
  - `source_workspace ProjectWorkspaceSummary`
  - `status WorkflowTaskStatus`
  - `actions WorkflowTaskActions`
  - `updated_at_unix_ms int64`

- `WorkflowTaskStatus`
  - `kind string`
  - `label string`
  - `native_state string`
  - `node_ids []string`
  - `run_ids []string`
  - `attention_types []string`

- `WorkflowTaskActions`
  - `can_start bool`
  - `can_interrupt bool`
  - `interrupt_run_id string`
  - `can_resume bool`
  - `resume_run_id string`
  - `can_cancel bool`
  - `needs_detail_for_interrupt bool`
  - `needs_detail_for_resume bool`

Workflow group model:

- Add first-class optional visual node groups rather than overloading transition groups.
- Proposed schema:
  - `workflow_node_groups(id, workflow_id, group_key, display_name, sort_order, metadata_json)`
  - `workflow_nodes.group_id` nullable reference.
- Minimal authoring mutation contract:
  - `WorkflowDefinition` adds `node_groups []WorkflowNodeGroup`.
  - `WorkflowNode` and `WorkflowNodeAddRequest` add optional `group_key string`.
  - `WorkflowNodeGroup` fields: `group_id string`, `workflow_id string`, `group_key string`, `display_name string`, `sort_order int`, `metadata_json string`.
  - Add incremental authoring routes matching the current workflow API shape: `workflow.nodeGroup.add`, `workflow.nodeGroup.update`, and `workflow.nodeGroup.delete`.
  - Group add/update/delete requests carry `workflow_id`, optional `group_id`, `group_key`, `display_name`, `sort_order`, and `metadata_json` as relevant.
  - Group keys are unique within a workflow, stable across graph revisions, and use the same key style as node keys.
  - Node group assignment is validated when adding/updating nodes and when deleting groups.
- Grouped workflows with group metadata render group islands.
- Workflows without group metadata return one implicit ungrouped group or empty `groups` with groupless columns.
- Group metadata changes are graph-affecting and increment `workflows.graph_revision`.
- Add service/store/API support for group metadata before relying on grouped board read models.
- GUI MVP consumes groups but does not author them; CLI/API workflow authoring can seed groups through the workflow definition path.
- Add internal CLI support only if existing workflow authoring cannot seed grouped workflows for fake-runtime/manual QA.

Live updates:

- Add `workflow.subscribeProject`
  - Request: `WorkflowProjectSubscribeRequest`
  - Fields:
    - `project_id string`
    - `workflow_id string`
    - `after_sequence int64`
  - Event: `WorkflowProjectEvent`
  - Event fields:
    - `event_id string`
    - `sequence int64`
    - `project_id string`
    - `workflow_id string`
    - `resource string`
    - `resource_id string`
    - `change string`
    - `occurred_at_unix_ms int64`
- Event resources include `project`, `workspace`, `workflow`, `board`, `task`, `run`, `transition`, `comment`, and `attention`.
- Use one monotonic global event sequence for GUI workflow invalidations. Scoped reads return the latest global sequence observed at generation time. Scoped subscriptions filter events but keep the same `after_sequence` namespace.
- Board and Home read models include the latest event sequence as a snapshot watermark. Clients subscribe with that watermark to avoid fetch-then-subscribe races.
- `project_id` is optional for the subscription. Empty `project_id` subscribes to global Home invalidations across projects; non-empty `project_id` subscribes to one project. `workflow_id` is valid only with non-empty `project_id`.
- GUI treats events as invalidations and refetches relevant read models.
- Reconnect always performs full refresh; no mutation replay.

Tests:

- Board request with selected workflow returns only selected workflow cards.
- Workflow picker includes default flag and validation blockers.
- Columns use workflow node sort order with start/backlog left and terminal/done separately identifiable.
- Board columns expose `WorkflowBoardNodeSummary` and do not leak prompt templates or output schemas from authoring DTOs.
- Done preview respects limit.
- Task cards include source workspace chip metadata for multi-workspace projects.
- Card action flags follow single active run interrupt decision.
- Grouped workflow returns group metadata and node membership.
- Ungrouped workflow returns deterministic ungrouped representation.
- Workflow node group routes validate group keys, node assignments, and graph revision bumps for group metadata changes.
- Grouped workflow is seedable via API, CLI, or test fixture without direct DB writes.
- Subscription emits invalidation events for task create/start/comment/transition/cancel.
- Subscription can resume from board snapshot watermark and does not lose mutations between fetch and subscribe.
- Empty-project subscription resumes from Home snapshot watermark and does not lose global Home/attention mutations between fetch and subscribe.

## Actions, Attention Inbox, Questions, Approvals

Goal: GUI can operate active workflow tasks and Home can route attention items to task detail.

New server API:

- `workflow.task.interrupt`
  - Request: `WorkflowTaskInterruptRequest`
  - Fields:
    - `task_id string`
    - `run_id string`
  - Response: `WorkflowTaskInterruptResponse`
  - Fields:
    - `task_id string`
    - `run_id string`
    - `interrupted_at_unix_ms int64`
    - `reason string`

- `workflow.attention.list`
  - Request: `WorkflowAttentionListRequest`
  - Fields:
    - `project_id string`
    - `page_size int`
    - `page_token string`
  - Response: `WorkflowAttentionListResponse`
  - Fields:
    - `items []WorkflowAttentionItem`
    - `next_page_token string`
    - `generated_at_unix_ms int64`
    - `latest_event_sequence int64`

`project_id` is optional. Empty `project_id` returns the global Home attention inbox across projects; non-empty `project_id` filters to one project.

- `workflow.task.attention.list`
  - Request: `WorkflowTaskAttentionListRequest`
  - Fields:
    - `task_id string`
  - Response: `WorkflowTaskAttentionListResponse`
  - Fields:
    - `items []WorkflowAttentionItem`

Changed server API:

- `WorkflowTaskResumeRequest`
  - Add optional `run_id string`.
  - If omitted and exactly one interrupted run exists, preserve current task-level behavior.
  - If multiple interrupted runs exist, return typed conflict requiring `run_id`.

- `WorkflowTaskApproveRequest`
  - Rename the GUI-facing field to `task_transition_id`.
  - It identifies the durable `task_transitions.id` row, not workflow `transition_id`.
  - GUI exposes Approve only; do not add Reject to GUI MVP.

- `workflow.task.question.answer`
  - Request: `WorkflowTaskQuestionAnswerRequest`
  - Fields:
    - `task_id string`
    - `run_id string`
    - `ask_id string`
    - `selected_option_number int`
    - `freeform_answer string`
    - `client_request_id string`
  - Response: `WorkflowTaskQuestionAnswerResponse`
  - Fields:
    - `task_id string`
    - `run_id string`
    - `ask_id string`
    - `resumed bool`

Attention item fields:

- `attention_id string`
- `type string`
  - `question`
  - `approval`
  - `interrupted_run`
  - `validation_blocker`
- `project_id string`
- `project_key string`
- `project_name string`
- `workflow_id string`
- `workflow_name string`
- `task_id string`
- `task_short_id string`
- `task_title string`
- `node_id string`
- `node_name string`
- `run_id string`
- `session_id string`
- `ask_id string`
- `task_transition_id string`
- `workflow_transition_id string`
- `latest_activity_unix_ms int64`
- `summary string`
- `action_hint string`
- `priority int`

Rules:

- Home attention inbox sorts newest activity first.
- Attention read model includes approvals from `task_transitions.state = 'pending_approval'`.
- Attention read model includes waiting questions from `task_runs.waiting_ask_id`.
- Attention read model includes interrupted active agent runs.
- `validation_blocker` is read-model-only. It is derived from interrupted run reason metadata or workflow validation state and must not introduce a separate durable validation-blocker table.
- Question details are resolved through a task/run/ask bridge when task detail opens contextual resume.
- Question answer action validates that `ask_id` belongs to the task run/session, then delegates to the existing prompt-control answer path with server-owned workflow authority instead of requiring GUI to hold a TUI controller lease.
- Question answer accepts exactly one answer mode. `selected_option_number` is used for option asks, `freeform_answer` is used for freeform asks, and conflicting/empty mode input returns typed validation. `client_request_id` reuses existing prompt-control request memo/idempotency semantics scoped to task/run/ask and must not add a separate durable/shared dedup table.
- If ask details cannot rehydrate, represent it as interrupted/validation attention with actionable resume path.
- Board/card Interrupt is available only when exactly one active run is interruptible.
- Task detail exposes per-run inline Interrupt controls when multiple active runs/fan-out branches exist.
- Task-level `interrupt` without `run_id` fails with typed conflict when multiple active interruptible runs exist.
- Interrupt records reason `user_interrupted` and detail enough for activity feed to render `Interrupted by user`.
- Cancel remains task-level, confirmation-only in GUI, with no reason field from GUI; backend reason can default to `user_canceled`.

Tests:

- Global attention list returns approvals, waiting asks, and interrupted runs across projects in newest-first order.
- Project-filtered attention list returns the same item types for one project.
- Attention list rows include required Home row data.
- Attention item resolves to standalone task detail.
- Approve uses `task_transition_id` and applies stored transition snapshot, not current graph.
- Question attention uses ask ID/session ID and handles missing ask rehydration.
- Task-scoped question answer succeeds without a GUI-held session controller lease.
- Task interrupt without run ID succeeds for one active run.
- Task interrupt without run ID returns conflict for multiple active runs.
- Task interrupt with run ID interrupts only that run.
- Resume without run ID succeeds for one interrupted run and conflicts for multiple.
- Cancel suppresses scheduling and interrupts active runs.

## Task Detail, Activity Feed, Comments, Teleport

Goal: task detail dialog has complete server-backed identity, status, feed, comments, and temporary TUI teleport data.

Changed server API:

- `workflow.task.get`
  - Keep `task_id` request.
  - Expand `WorkflowTaskDetail` with:
    - project summary/key/name
    - workflow summary/default flag
    - full title/body/source URL
    - source workspace
    - managed worktree summary/path when created
    - active placements with node display names
    - runs with role, session ID/name when available, status, generation, timestamps, waiting ask ID
    - unresolved attention items
    - action flags

New server API:

- `workflow.task.activity.list`
  - Request: `WorkflowTaskActivityListRequest`
  - Fields:
    - `task_id string`
    - `page_size int`
    - `page_token string`
  - Response: `WorkflowTaskActivityListResponse`
  - Fields:
    - `items []WorkflowTaskActivityItem`
    - `next_page_token string`
    - `generated_at_unix_ms int64`

- `workflow.task.teleportTarget.get`
  - Request: `WorkflowTaskTeleportTargetRequest`
  - Fields:
    - `task_id string`
    - `run_id string`
  - Response: `WorkflowTaskTeleportTargetResponse`
  - Fields:
    - `available bool`
    - `task_id string`
    - `run_id string`
    - `session_id string`
    - `project_id string`
    - `workspace_id string`
    - `worktree_id string`
    - `cwd_relpath string`
    - `failure_reason string`

Activity item fields:

- `activity_id string`
- `type string`
  - `comment`
  - `transition`
  - `run_started`
  - `run_completed`
  - `run_interrupted`
  - `task_canceled`
- `task_id string`
- `occurred_at_unix_ms int64`
- `updated_at_unix_ms int64`
- `actor string`
- `summary string`
- `comment WorkflowTaskComment`
- `transition WorkflowTaskTransition`
- `run WorkflowRun`
- `attention WorkflowAttentionItem`

Expanded transition DTO fields:

- `source_node_id string`
- `source_node_key string`
- `source_node_display_name string`
- `transition_group_id string`
- `transition_display_name string`
- `workflow_revision_seen int64`
- `actor string`
- `applied_at_unix_ms int64`
- `edges []WorkflowTransitionEdge`

Expanded transition edge DTO fields:

- `edge_key string`
- `target_node_id string`
- `target_node_key string`
- `target_node_display_name string`
- `target_node_kind string`
- `requires_approval bool`
- `context_mode string`
- `output_requirements []WorkflowOutputRequirement`
- `input_bindings []WorkflowInputBinding`
- `workflow_revision_seen int64`

Comment DTO changes:

- Add `created_at_unix_ms`.
- Preserve `updated_at_unix_ms`.
- Soft-deleted comments stay hidden by default.

Rules:

- Activity feed sorts newest-to-oldest and paginates older entries.
- Feed is backed by existing `task_comments`, `task_transitions`, task cancellation metadata, and `task_runs`; do not add a separate event table solely for GUI.
- Comments remain full CRUD in MVP.
- Teleport endpoint returns identifiers only. GUI/native bridge owns opening terminal and exact local CLI command once CLI attach surface is final.
- Teleport unavailable if there is no task run session yet; return plain failure reason.

Tests:

- Task detail includes identity, source workspace, managed worktree when created, active node/status, run/session, cancellation, and attention.
- Activity feed merges comments, transitions, run interruptions, and task cancellation newest-first.
- Activity pagination is stable under same-timestamp rows.
- Approval activity includes stored source node, transition label/id, target nodes, requirements, commentary, graph revision, and stale-warning inputs.
- Comment create/edit/delete updates feed and live-update invalidations.
- Teleport target returns session/project/workspace/worktree IDs for a run with session.
- Teleport target returns unavailable reason for pre-start/no-session task.

## Executable Implementation Checklist

Use this section as the backend implementation tracker. Work one slice at a time. Do not mark a slice complete until its tests, build, docs sync, and status notes are done. If implementation discovers a contract mismatch, update the contract section above and this checklist in the same change.

Current status:

- [x] Backend recon summarized.
- [x] GUI backend contracts drafted.
- [x] Smart reviewer gaps integrated.
- [x] Executable per-slice implementation checklist added.
- [x] Nikita accepted implementation by setting goal to implement slices 0-5.
- [x] Slice 0 implementation complete.
- [x] Slice 1 Home/project admin/project key/workspaces implementation complete.
- [ ] Next action: implement Slice 2 workflow picker, selected board, groups, and live updates.

Source-of-truth rules:

- Keep GUI/backend additions in this document and related GUI docs only.
- Do not add GUI backend scope to `docs/dev/async-workflow-implementation-checklist.md`.
- Keep `docs/dev/gui-workflow-mvp-prd-checklist.md` and `docs/dev/gui-workflow-use-cases.md` synced when behavior or MVP scope changes.
- Update `docs/dev/decisions.md` only when Nikita makes a product or architecture decision.
- Preserve no-table Markdown style in GUI planning docs.

Common slice loop:

- [ ] Re-read this checklist and the slice contract before starting the next unchecked item.
- [ ] Recon current package APIs and tests for the slice; avoid duplicating existing routes, DTOs, stores, or read models.
- [ ] Write failing tests first for changed business logic and wire contracts.
- [ ] Implement DTOs, routes, service contracts, client methods, transport dispatch, stores/views/services, and migrations needed by that slice.
- [ ] Keep each intermediate commit/build state coherent; do not strand partially wired JSON-RPC methods.
- [ ] Run the exact verification commands listed for the current slice. If extra packages were edited, add them to the listed test command before handoff.
- [ ] Update this checklist status before handoff.

### Slice 0 Checklist: Connectivity, Readiness, Capabilities

Status: complete.

Goal: desktop can attach to the configured server, show startup blockers safely, and gate GUI features by server capability.

Implementation checklist:

- [x] Recon existing handshake, auth readiness, server version, health/ready endpoints, route auth policy, client startup paths, and JSON-RPC method registration.
- [x] Add failing tests for `server.readiness.get` DTO validation, route registration, client call path, and auth/startup blocker summary.
- [x] Add failing tests for `server.capabilities.get` returning all required MVP capability IDs with available/reason metadata.
- [x] Add `shared/serverapi` DTOs with explicit JSON tags and unix-ms timestamps if any timestamp is needed.
- [x] Add method constants, route registration, service contract, remote client method, and unary transport dispatch.
- [x] Implement readiness service composition using existing server/auth/bootstrap state instead of duplicating auth logic.
- [x] Implement capability registry with stable IDs from the contract above.
- [x] Ensure readiness is available early enough for desktop startup while mutating routes remain auth-gated.
- [x] Add typed generic auth/startup blocker cause behavior.
- [x] Sync docs if capability IDs or readiness cause semantics change.

Completion criteria:

- [x] `server.readiness.get` returns version, protocol, server ID, auth flags, ready flag, endpoint, and typed causes.
- [x] Missing/expired auth produces generic readiness blocker, not a special GUI auth flow.
- [x] `server.capabilities.get` returns every MVP capability ID.
- [x] Verification commands pass:
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/auth ./server/bootstrap ./server/embedded ./server/core ./server/serve ./cli/app`
  - `./scripts/build.sh --output ./bin/builder`

### Slice 1 Checklist: Home, Project Admin, Project Key, Workspaces

Status: complete.

Goal: Home can list projects, open the default workflow board without extra discovery, and create projects with editable keys.

Implementation checklist:

- [x] Recon `server/projectview`, `server/metadata`, existing SQLC queries, project binding DTOs, project create tests, workspace list behavior, and workflow link/default metadata.
- [x] Add failing tests for explicit `project_key` create, invalid key, collision, transactional rollback, and omitted-key compatibility.
- [x] Add failing tests for `project.home.list` pagination, latest-activity sort, project key, primary workspace summary, counts, default workflow ID/name/validity, and event sequence watermark.
- [x] Add failing tests for no-valid-default-workflow Home summary so GUI can open a blocker state and keep New Task disabled.
- [x] Add failing tests for `project.workspace.list` returning all workspaces plus default/primary workspace.
- [x] Expose Home watermark field; current foundation returns `0` until Slice 2 adds monotonic subscription sequence storage/delivery.
- [x] Add attention-count helper over pending approvals, waiting asks, interrupted runs, and read-model-only validation blockers.
- [x] Add/adjust SQL queries and store methods for paginated Home summaries without full scans over unbounded data.
- [x] Wire `project.home.list` and `project.workspace.list` through DTOs, method constants, route registry, service contract, client, and transport.
- [x] Add `project_key` to project create request/response and project identity DTOs where GUI displays identity.
- [x] Validate project keys with typed validation errors and preserve default/backfill behavior for existing callers.
- [x] Keep project key creation atomic with project/workspace creation.
- [x] Sync GUI PRD/use-case docs if Home fields or create behavior change.

Completion criteria:

- [x] GUI can create project with chosen key.
- [x] Existing project create callers still work without sending key.
- [x] Home summary has default workflow ID/name/validity.
- [x] Home summary exposes event watermark and attention count without depending on the Slice 4 paginated attention API.
- [x] Home and workspace list routes are paginated/typed and client-callable.
- [x] Verification commands pass:
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/projectview ./server/metadata ./server/workflowstore ./server/workflowview ./server/core ./server/serve ./cli/app`
  - `./scripts/build.sh --output ./bin/builder`

### Slice 2 Checklist: Workflow Picker, Selected Board, Groups, Live Updates

Status: not started.

Goal: GUI can render one selected workflow board with picker, groups, task cards, action facts, done preview, and race-safe invalidations.

Implementation checklist:

- [ ] Recon `server/workflowview`, workflow graph/link/default storage, scheduler/store task state, existing streaming routes, and project/session event plumbing.
- [ ] Add failing tests for selected-workflow board returning only selected workflow cards.
- [ ] Add failing tests for picker ordering by default, MRU, then display name.
- [ ] Add failing tests for picker default flag, validation blockers, display names, graph revisions, and unlinked workflow handling.
- [ ] Add failing tests that board columns expose `WorkflowBoardNodeSummary` and do not leak authoring prompt templates/output schemas.
- [ ] Add failing tests for column order, Backlog left, Done preview limit, card statuses, action flags, and multi-active interrupt detail requirement.
- [ ] Add failing tests for grouped workflow metadata, deterministic ungrouped representation, and graph revision bump on group metadata changes.
- [ ] Add failing tests for `workflow.subscribeProject` invalidation events, monotonic sequence, `after_sequence`, and snapshot watermark race safety.
- [ ] Add visual group schema/store/service/API support before relying on grouped board output.
- [ ] Extend workflow definition create/update to accept optional visual groups and node group assignments for CLI/API seeding.
- [ ] Implement selected board read model and picker DTOs using server-native state facts.
- [ ] Implement card action fact computation without GUI-only state.
- [ ] Implement project/workflow invalidation event storage or sequence source with monotonic ordering.
- [ ] Wire `workflow.board.get` and `workflow.subscribeProject` through contracts, clients, and transport/streaming layers.
- [ ] Support empty-project subscription for global Home invalidations from Home read-model watermarks.
- [ ] Use legacy/default workspace fallback for card workspace summaries until Slice 3 makes task source workspace authoritative.
- [ ] Sync GUI docs if board grouping, status, or action semantics change.

Completion criteria:

- [ ] Board route returns selected workflow board, picker, columns, groups, cards, done preview, and latest event sequence.
- [ ] Live update subscription can resume from read-model watermark without lost invalidations.
- [ ] Board DTO contains no workflow authoring prompt/template internals.
- [ ] Group metadata is first-class and revisioned.
- [ ] Picker orders default workflow first, then MRU, then display name.
- [ ] Verification commands pass:
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/workflowsvc ./server/workflowstore ./server/workflowview ./server/workflowscheduler`
  - `./scripts/build.sh --output ./bin/builder`

### Slice 3 Checklist: Task Source Workspace And Backlog Editing

Status: not started.

Goal: GUI can create Backlog tasks with optional body and selected main/source workspace, then edit those fields until automation starts.

Boundary note: drag-to-start is user-facing task operation scope, but Slice 3 verifies `workflow.task.start` drop semantics because source-workspace and worktree-source selection belong with Backlog/source workspace implementation. Slice 4 owns interrupt, cancel, resume, inbox, approval, and question actions.

Implementation checklist:

- [ ] Recon task schema/store/create/start/update paths, worktree creation, workspace foreign keys, and task detail/board read models.
- [ ] Add failing migration/store tests for `tasks.source_workspace_id`, project-bound workspace invariant, backfill/default behavior, and legacy fallback.
- [ ] Add failing service/API tests for task create with selected workspace, omitted body, omitted workspace defaulting to primary, and foreign-project workspace rejection.
- [ ] Add failing tests for `workflow.task.update` editing title/body/source workspace before start and rejecting edits after start/cancel.
- [ ] Add failing tests for GUI drag-to-start/drop semantics using `workflow.task.start`: Backlog task dropped onto first active node starts immediately, uses selected workflow/source workspace, and emits invalidation.
- [ ] Add failing tests that `EnsureTaskWorktree` uses task source workspace before primary fallback.
- [ ] Add migration with DB-level project-bound invariant when feasible; otherwise enforce equivalent checked transaction and document reason in code.
- [ ] Update task create validation so body is optional.
- [ ] Add task source workspace fields to summary/detail/card DTOs and read models.
- [ ] Wire `workflow.task.update` through DTOs, route registry, service contract, client, and transport.
- [ ] Confirm existing `workflow.task.start` is sufficient for GUI drop-to-start or narrow its request/validation without adding a visible Start button concept.
- [ ] Update worktree service to select source workspace first.
- [ ] Sync GUI docs if pre-start edit rules or workspace labels change.

Completion criteria:

- [ ] New tasks persist source workspace and optional body.
- [ ] Task source workspace is immutable after first run/start.
- [ ] Worktree creation uses selected source workspace.
- [ ] Drag-to-start behavior is covered by server tests and invalidation events.
- [ ] Board/task detail expose source workspace from persisted task state.
- [ ] Verification commands pass:
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/metadata ./server/workflowsvc ./server/workflowstore ./server/workflowview ./server/worktree`
  - `./scripts/build.sh --output ./bin/builder`

### Slice 4 Checklist: Actions, Attention Inbox, Questions, Approvals

Status: not started.

Goal: GUI can operate active tasks and Home can route questions, approvals, and interrupted runs into task detail.

Implementation checklist:

- [ ] Recon workflow run interrupt/resume internals, approval transitions, waiting ask markers, prompt-control answer path, cancellation behavior, and existing comments/transition persistence.
- [ ] Add failing tests for global and project-filtered `workflow.attention.list` returning approvals, waiting asks, interrupted runs, and validation blockers newest-first.
- [ ] Add failing tests for `workflow.task.attention.list` resolving all unresolved task attention items.
- [ ] Add failing tests that `workflow.attention.list` includes `latest_event_sequence` and can pair with empty-project subscription without fetch/subscribe races.
- [ ] Add failing tests for interrupt: no `run_id` succeeds with one active run, conflicts with multiple active runs, and specific `run_id` interrupts only that run.
- [ ] Add failing tests for resume: no `run_id` succeeds with one interrupted run and conflicts with multiple interrupted runs.
- [ ] Add failing tests that approval uses GUI field `task_transition_id` and applies stored transition snapshot, not current graph.
- [ ] Add failing tests for task-scoped question answer validating task/run/ask membership, rejecting conflicting answer modes, enforcing idempotent `client_request_id`, and succeeding without GUI-held controller lease.
- [ ] Add failing tests that cancel suppresses scheduling and interrupts active runs with backend default reason.
- [ ] Implement attention read model over durable approval, ask, interrupted-run, and validation-blocker sources.
- [ ] Wire `workflow.task.interrupt`, `workflow.attention.list`, `workflow.task.attention.list`, and `workflow.task.question.answer`.
- [ ] Add optional `run_id` to resume request while preserving existing unambiguous task-level behavior.
- [ ] Rename/introduce GUI approval field `task_transition_id`; keep compatibility only if needed by existing non-GUI clients.
- [ ] Emit live-update invalidations for attention/action state changes.
- [ ] Sync GUI docs if attention priority, action conflicts, or approval/question behavior changes.

Completion criteria:

- [ ] Home attention inbox can list all MVP attention types globally and per project.
- [ ] Attention list includes an event watermark usable with empty-project subscription.
- [ ] Task detail can operate per-run interrupt/resume for fan-out or multi-active tasks.
- [ ] GUI can answer task-scoped questions without taking a TUI controller lease.
- [ ] GUI approval path uses `task_transition_id`.
- [ ] Verification commands pass:
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/workflowsvc ./server/workflowstore ./server/workflowview ./server/workflowscheduler ./server/workflowrunner ./server/workflowruntime ./server/runprompt ./server/primaryrun ./server/registry`
  - `./scripts/build.sh --output ./bin/builder`

### Slice 5 Checklist: Task Detail, Activity Feed, Comments, Teleport

Status: not started.

Goal: task detail dialog has complete server-backed identity, status, feed, comments, and temporary TUI teleport target data.

Implementation checklist:

- [ ] Recon existing task get/detail, comments CRUD, transition persistence, run/session metadata, cancellation metadata, and CLI/TUI attach identifiers.
- [ ] Add failing tests that expanded `workflow.task.get` includes project/workflow identity, source workspace, managed worktree, placements, runs/sessions, unresolved attention, cancellation, and action flags.
- [ ] Add failing tests for `workflow.task.activity.list` merging comments, transitions, run starts/completions/interruptions, and task cancellation newest-first.
- [ ] Add failing tests for stable activity pagination under same-timestamp rows.
- [ ] Add failing tests that approval activity includes stored source node, transition label/id, target nodes, requirements, commentary, graph revision, and stale-warning inputs.
- [ ] Add failing tests that comment create/edit/delete updates activity feed and emits live-update invalidations.
- [ ] Add failing tests for `workflow.task.teleportTarget.get` returning identifiers for runs with sessions and unavailable reason for pre-start/no-session tasks.
- [ ] Expand task detail read model without loading full transcripts or `events.jsonl`.
- [ ] Implement activity feed as a read model over existing durable comments, transitions, task runs, and cancellation data; do not add a separate GUI event table solely for feed.
- [ ] Wire activity and teleport routes through DTOs, service contract, client, and transport.
- [ ] Keep teleport endpoint identifier-only; GUI/native bridge owns terminal launch and local CLI command.
- [ ] Sync GUI docs if task detail, feed item kinds, or teleport contract changes.

Completion criteria:

- [ ] Task detail route gives GUI enough data for fixed header, actions, unresolved attention, comments, feed, and teleport button state.
- [ ] Activity feed paginates newest-first from durable data without transcript replay.
- [ ] Comments remain full CRUD and visible in feed.
- [ ] Teleport target returns plain unavailable reason when no attachable session exists.
- [ ] Verification commands pass:
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/workflowsvc ./server/workflowstore ./server/workflowview ./server/session ./server/runtimeview`
  - `./scripts/build.sh --output ./bin/builder`

## Verification Policy

Before handing off completed docs-only plan edits:

- Run no-table check for touched GUI docs.
- Run trailing-whitespace check for touched GUI docs.
- Run `git diff --check` for touched docs/package files.
- Verify `git diff -- docs/dev/async-workflow-implementation-checklist.md` remains empty unless Nikita explicitly requested editing that file.

Before handing off completed backend code:

- Run targeted Go tests for edited packages through `./scripts/test.sh`.
- Run full build through `./scripts/build.sh --output ./bin/builder` after production Go changes.
- Run broader `./scripts/test.sh` before final MVP integration handoff when slice interactions become coupled.
- Do not run real-provider workflow QA without explicit Nikita approval.
