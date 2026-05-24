## Checklist

- [x] Recon current workflow graph viewer, workflow APIs, validation, and mutation model.
- [x] V1 read-only inspector milestone: existing workflow definitions can be inspected from the editor through the global right sidebar; joins are visible as clickable merge diamonds; semantic node/edge colors are implemented with validation-error red overrides.
- [ ] Workstream 1: reach shared ownership/hierarchy model from actual DB ERD.
- [ ] Workstream 1: update product/navigation decisions for projects, workflows, tasks, and boards after hierarchy is agreed.
- [ ] Workstream 1: decide whether `workflow_events` hard-cutover belongs before or after navigation redesign.
- [ ] Workstream 2: revisit workflow editor product decisions after Workstream 1 is complete.
- [ ] Workstream 2: draft UX and interaction design. Blocked by Workstream 1.
- [ ] Workstream 2: draft server/API architecture. Blocked by Workstream 1.
- [ ] Workstream 2: draft implementation plan with test/QA criteria. Blocked by Workstream 1.
- [ ] Workstream 2: review plan for edge cases and future extensibility. Blocked by Workstream 1.

## Product Decisions

- UX redesign work is about how operators use top-level workflows, not only graph-editing mechanics.
- Revised UX direction: daily work should remain project-first. Users open a project such as Builder, Respawn, FlowMVI, or Blog, then choose one of the reusable workflows linked to that project.
- Workflows remain top-level reusable definitions in the data model, but are presented inside a project as linked process lenses, not owned child records.
- Home should keep Projects as the primary picker and global Attention as the secondary pane. A separate global Workflow Library entry point is needed for creating/editing reusable workflow definitions across projects without entering a project first.
- Project workflow management should use "Link workflow" language. Link workflow opens an intermediary picker listing all global workflows plus a "New workflow" action.
- "New workflow" from a project-originated Link workflow flow creates a global workflow definition, auto-links it to the originating project, and opens the workflow editor for that workflow.
- "New workflow" from the global Workflow Library creates a global workflow definition without project context unless the user explicitly links it later.
- Workflow Library is definition management only for this redesign. It lists/opens/creates/deletes workflows and manages project links; it does not create tasks or act as an aggregate task board.
- Projects stay first-class daily-work destinations because they provide task namespace, workspaces, source workspace defaults, and workflow-link context.
- Workflow editor/library routes may become global workflow-definition routes, but project-originated task/board routes remain project-scoped.
- Project-workflow pairing should be visible in the UI. A task derives its project/workflow execution context through `project_workflow_link_id -> project_workflow_links`; the UI should present this as a project-linked workflow, not as a hidden implementation detail.
- Blocking data-model prerequisite: complete full schema hardening from the generated-DDL audit before any new workflow GUI/sidebar work. This includes task-link normalization, task/runtime/session cross-link invariants, primary/main flag constraints, weak old-table constraints, read-model identity cleanup, and `workflow_events` removal/replacement.
- Decision: task-link normalization is a hard cutover. Remove duplicated `tasks.project_id` and `tasks.workflow_id` columns and update schema/code/read models in one breaking backend slice, with no compatibility shim around the old duplicated columns.
- Decision: remove soft-unlink semantics from `project_workflow_links`. Project-workflow links are active membership rows only; unlink hard-deletes an unused link, and deleting/retiring a workflow deletes the workflow definition and cascades/deletes tasks through the workflow/link cleanup path. If tasks exist for an accidental link, move/delete those tasks before unlinking rather than preserving an inactive timestamp row.
- Blocking preliminary task 1: approve the global right-side sidebar island UX/API contract before building the Link workflow intermediary flow. Contract must define destination types, result/cancel semantics, route-change auto-close, and local sidebar navigation.
- Blocking preliminary task 2: implement the shell/sidebar provider and host from the approved contract before building the Link workflow intermediary flow. Tauri native blocking windows should not be used for this workflow UX because they are unreliable/problematic and the product direction is moving away from them.
- Editing functionality builds on workflow editor v1, which is currently a read-only workflow definition viewer with global-sidebar inspection for workflow metadata, groups, nodes, joins, and edges.
- GUI remains a remote-control surface; server remains authoritative for workflow definitions, validation, persistence, project links, and events.
- V1 editor graph semantics are locked: editor renders join nodes as small inspectable merge diamonds, board/Kanban read models still omit join columns, edge colors communicate context mode, node colors communicate node kind, and validation-error red overrides normal semantic colors only for invalid graph entities.
- Next workflow-editor slice is the server-owned graph validate/preview/save substrate: GUI-local unsaved drafts remain the editing architecture, but the immediate implementation should expose graph-only `workflow.graph.validateDraft`, `workflow.graph.savePreview`, and `workflow.graph.save` contracts, add a shared graph edit policy, and keep the visible editor read-only.
- First editing release should support full graph CRUD for nodes, node groups, transition groups, and edges, including guarded delete/archive behavior.
- Editing uses a local draft session with Save/Discard. Server mutations apply only when the user saves.
- Canvas opens with ELK-computed layout. User may drag nodes/groups during the edit session; coordinates are kept only in memory and reset to ELK on reopen.
- Editing a linked workflow is disallowed while it has active tasks. Editing is allowed only when existing tasks are all backlog or done.
- The editor opens entity edit surfaces in the global sidebar rather than Tauri native blocking windows or an always-visible inspector. Sidebar forms should follow task-detail-like spacing and island styling.
- Clicking a node opens its edit surface. Clicking an edge/arrow opens its edge edit surface.
- Dragging from node connectors creates arrows. Invalid drags to empty canvas snap back silently; invalid drags to invalid nodes snap back with a toast; valid drags open the edge/transition edit surface for required schema fields.
- A plus button in the top-left canvas tool island creates a new node via the node edit surface. After add, the new node appears at the current viewport center.
- Group creation should use the easiest acceptable UX among: separate group-plus button, drag node over node, or context menu grouping. Prefer the simplest robust implementation during architecture planning.
- Edge editing should support both simple drag creation and explicit transition-group/fan-out editing.
- V2 exposes advanced edge configuration: context mode, approval requirement, output requirements, and input bindings.
- Save should allow any graph that satisfies hard storage invariants, even if draft/execution validation reports issues.
- `workflows.graph_revision` remains minimal by design for now: a monotonic traceability/stale-warning scalar over mutable graph rows, not immutable graph versioning. Full graph revision/version history is deferred and should not block the workflow GUI redesign.
- Backlog/start deletion is out of scope for v2. Current storage and validation enforce exactly one `start` node, and replacement start-entry semantics would require a larger API/data-model change.
- Hide `start` from add/kind-change controls. Existing Backlog can be renamed where safe, but its kind stays fixed.
- Done/terminal deletion is allowed when at least one other terminal node remains. Otherwise block with a toast.
- Destructive delete impact is evaluated on Save, not at draft-edit time. This is required because v2 uses local Save/Discard and task state can change while the draft is open.
- Save should run a server-side impact check for the pending graph diff. If active tasks would be affected, Save is blocked. If only backlog/done tasks would be orphaned/deleted by removed nodes or edges, show a confirmation listing affected nodes/tasks before applying. Requested wording pattern: "XXX tasks will be **gone forever** along with the node. Proceed?"
- Dirty local draft conflicts with remote graph updates should keep the draft, show a conflict banner, and require Reload remote or Save against expected graph revision, where stale save rejects.
- Workflow creation/copy/link management should be part of the editing feature set:
  - Board/project workflow management gets a "Link workflow" action that opens the global right-side sidebar picker listing all reusable workflows and a "New workflow" action.
  - "New workflow" from this project-originated flow opens the editor and auto-links the new workflow to the current project.
  - Editor gets a pen icon that opens workflow metadata/settings in a sidebar.
  - Workflow settings include name/description and actions to link/unlink workflow to projects.
  - Project selection should be paginated and minimal, but hosted inside the new sidebar rather than a native blocking window.
- Add a global right-side sidebar island:
  - It should stretch out from the right side of the screen and be reusable from board/editor/other pages.
  - It should support typed sidebar destinations, local sidebar navigation, and returning a result to the opener/current screen.
  - Opening another main destination or navigating back/forward in the app nav stack auto-closes the sidebar.
  - Use this instead of adding native blocking windows for intermediary/picker/settings flows.
- To avoid dirty drafts becoming unsaveable because a task starts mid-edit, use a non-persisted edit-session lock. Opening an editable workflow acquires/heartbeats a process-local workflow edit lock; task starts/automation-changing actions for that workflow are blocked until Save/Discard/timeout. Save still performs authoritative transactional rechecks.

## Recon Notes

- Schema audit should use generated latest SQLite DDL from a migrated temp DB, not hand-composed migration reading. Current reproducible method: copy only `server/metadata/migrations/*.up.sql` into a temp migration dir, run Goose against a temp SQLite DB, then inspect `sqlite_schema` / `sqlite3 .schema`. GH issue #282 tracks adding a one-line repo command for this.
- Current route is `/projects/:projectId/workflows/:workflowId/editor` and is project-link gated. It loads project workflow links, board metadata, workflow definition, execution validation, then derives React Flow nodes/edges from ELK layout.
- `WorkflowGraphCanvas` is deliberately read-only today: controlled `nodes`/`edges`, node drag/connect disabled, selection/click inspection only, fit/zoom/inspect toolbar, no draft or authoring state.
- GUI API client only exposes read methods for workflow definitions/validation/project links. Server transport already has mutation routes for workflow create/update, node add/update, node group add/update/delete, transition group add/update, and edge add/update.
- Server service publishes linked-project workflow events for add/update mutations, so editor subscription/refetch already fits the editing model. Delete/archive node and delete edge exist in `workflowstore` but are not exposed through `workflowsvc`, `serverapi`, protocol routes, or GUI client.
- Store mutations increment graph revision per graph-affecting edit. Existing add/update requests are whole-entity replacement requests, not patch requests, so GUI must submit complete entity payloads or add dedicated patch/batch APIs.
- Validation supports draft/task creation/execution contexts. Draft mode intentionally relaxes the start-node outgoing shape but still validates hard invariants like identifiers, references, kinds, bindings, output fields, and graph topology errors.
- Runtime/task behavior snapshots workflow graph revision into tasks/runs/transitions. Destructive graph deletion has safeguards: deleting nodes/edges is blocked when non-terminal task references exist or task history references the entity. Archiving node exists for terminal-history cases but is not exposed.
- Node groups are persisted separately and currently used for visual grouping. Layout is not persisted; ELK recomputes deterministic topology layout on every definition/revision.
- Workflow terminology to preserve in UI/API docs: Workflow, Workflow Draft, Graph Revision, Node, Edge, Transition Group, Transition ID, Output Requirements, Input Bindings, Join, Project Workflow Link.
- Current V1 inspector is intentionally identity-based rather than snapshot-based: sidebar destinations carry workflow ID plus selected entity identity and resolve current `workflow.get`/`workflow.validate` React Query cache data on render. V2 edit forms should preserve this typed destination approach while swapping read-only sections for draft-backed forms.

## Schema Audit Caveats

### Blocking Before GUI

- Decision: all schema audit caveats below are blocking prerequisites before new workflow GUI/sidebar implementation starts.
- Fix approach by layer:
  - Task-link normalization: schema migration plus store/query/API/read-model refactor. Hard cutover; do not keep compatibility columns.
  - Runtime task cross-link invariants: schema migration using composite FKs where direct relationships can express the invariant, and triggers where the invariant depends on task -> project_workflow_link -> workflow/project derivation.
  - Session/workspace/worktree consistency: schema migration using composite FKs/unique keys where possible; service/store tests must prove mismatched rows are rejected.
  - Task source/managed worktree consistency: schema migration via derived-project triggers and composite `(source_workspace_id, managed_worktree_id)` enforcement where possible; store/worktree service tests must prove cross-project/workspace rows are rejected.
  - Primary/main flags and older weak constraints: schema migrations for boolean checks, uniqueness where safe, JSON validity, enum checks, plus service/query updates for any transitional backfill.
  - Project workflow link lifecycle and task status: read-model/store-query refactor so link identity and centralized status derivation are explicit.
  - `workflow_events`: hard-cutover persisted invalidation table to in-memory pub/sub before workflow GUI work depends on events.

### Missing DB-Enforced Invariants

- `tasks.project_id` and `tasks.workflow_id` duplicate facts derivable from `tasks.project_workflow_link_id`. Normalize this before GUI work.
- Runtime task tables rely on globally unique IDs without enough composite constraints:
  - `task_node_placements.node_id` can point at a node from a workflow different from the task's workflow.
  - `task_node_placements.created_by_transition_id` and `parallel_batch_transition_id` can point to transitions for another task.
  - `task_runs.task_id`, `placement_id`, `node_id`, and `session_id` can be mutually inconsistent or cross-task/cross-project/cross-workspace.
  - `task_transitions.source_run_id`, `source_placement_id`, `source_node_id`, and `transition_group_id` can be mutually inconsistent or cross-task/cross-workflow.
  - `task_transition_edges.workflow_edge_id`, `target_node_id`, and `target_placement_id` can be mutually inconsistent or cross-task/cross-workflow.
  - `task_comments.source_run_id` can point to a run for another task.
- `sessions.project_id`, `workspace_id`, and `worktree_id` are independent FKs. The DB does not enforce that a session workspace belongs to the session project or that a session worktree belongs to the session workspace.
- `tasks.managed_worktree_id` is only an FK to `worktrees(id)`. Unlike `source_workspace_id`, there is no DB trigger enforcing that the managed worktree belongs to the task's source workspace or project.
- `workspaces.is_primary` and `worktrees.is_main` have no `CHECK (IN (0, 1))` and no uniqueness/exactly-one constraint. The DB permits zero or multiple primary workspaces per project and zero or multiple main worktrees per workspace.
- `workflow_events` is a stringly typed invalidation feed with no project/workflow FKs and no resource/action enums. It should not be treated as domain truth.
- Older metadata tables have weaker DB constraints than newer workflow tables: unchecked JSON blobs, unconstrained text enums, and integer booleans without `CHECK` constraints.

### Read-Model / Service Conventions

- Current `project_workflow_links.unlinked_at_unix_ms` soft-unlink semantics are rejected. Replace with hard link deletion and explicit task move/delete behavior before unlink when tasks exist.
- Task status is a derived convention across active placements, terminal node kind, run interruption/completion, pending approvals, and cancellation fields. The DB does not provide a single canonical task status row.

### Ruthless Minimization Candidates

- Audit method: generated latest SQLite `.schema` from a temp migrated DB plus adversarial review.
- Approved eliminations:
  - Drop persisted `workflow_events` table and `workflow_events_project_sequence_idx`; replace with typed in-memory pub/sub plus reconnect refetch.
  - Drop `project_workflow_links.unlinked_at_unix_ms` and active-link filtered indexes; use hard link deletion and explicit task move/delete before unlink.
  - Replace `project_workflow_links.is_default` and `project_workflow_links_default_idx` with a project-level default workflow link pointer.
  - Drop duplicated `tasks.project_id` and `tasks.workflow_id`; derive via `tasks.project_workflow_link_id -> project_workflow_links`.
  - Drop task indexes that only support duplicated task project/workflow columns, replacing them with link-derived/query-specific indexes after normalization.
  - Drop `workflow_transition_groups.workflow_id`; derive workflow through `source_node_id -> workflow_nodes.workflow_id`.
  - Drop `workflow_edges.workflow_id`; derive workflow through `transition_group_id -> workflow_transition_groups -> source_node`.
  - Drop `task_runs.task_id` and `task_runs.node_id`; derive through `placement_id -> task_node_placements`.
  - Drop `task_node_placements.created_by_transition_id`; derive from `task_transition_edges.target_placement_id`.
  - Drop live workflow refs from historical transition snapshots where snapshots already exist: `task_transitions.source_node_id`, `task_transitions.transition_group_id`, `task_transition_edges.workflow_edge_id`.
  - Drop `task_transition_edges.workflow_revision_seen`; derive from parent `task_transitions.workflow_revision_seen`.
  - Drop empty/opaque metadata JSON columns with no mandatory product field: `projects.metadata_json`, `workflows.metadata_json`, `workflow_nodes.metadata_json`, `workflow_node_groups.metadata_json`, `workflow_transition_groups.metadata_json`, `workflow_edges.metadata_json`, `task_transition_edges.metadata_json`, `task_comments.metadata_json`, `runtime_leases.metadata_json`.
  - Drop persisted path availability on `workspaces`/`worktrees`; derive from filesystem/git inspection at read/sync boundaries.
  - Drop `workspaces.git_metadata_json` if no mandatory workspace-level Git fields exist; worktree Git metadata remains the execution/worktree detail source until separately redesigned.
  - Replace `workspaces.is_primary` with a project-level primary/default workspace pointer.
  - Drop `worktrees.is_main`; derive main worktree from workspace root / Git worktree inspection.
  - Minimize `runtime_leases` to the durable token facts actually validated, likely `id`, `session_id`, `created_at_unix_ms`; drop unused `client_id`, unused `metadata_json`, and duplicate `acquired_at_unix_ms`.
- Product-confirmation-needed eliminations:
  - `tasks.short_id` can be derived from immutable `project_key + task_seq`, but confirm task ID reuse/immutability expectations before dropping the stored string.
  - `projects.next_task_seq` can be replaced by transactional `MAX(task_seq)+1`, but this may reuse deleted task numbers unless a tombstone/counter rule is retained.
  - `sessions.launch_visible` is a sticky visibility cache; dropping it changes subtle session-list behavior unless visibility is redefined.
  - `task_runs.metadata_json` currently stores context/source-run hints; remove only after replacing those hints with typed relations or derivation.
  - `task_runs.workflow_revision_seen` can be derived from `run_start_snapshot_json`, but only after legacy/missing snapshot rows are migrated.
- Approved second-pass eliminations:
  - Drop `workspaces.display_name`; derive workspace display labels from `basename(canonical_root_path)` because workspace rename is not a distinct product operation.
  - Drop `worktrees.display_name`; derive worktree display labels from Git worktree/branch data or `basename(canonical_root_path)`.
  - Drop `runtime_leases.request_id`; runtime lease validation only needs the durable lease token and session ownership.
  - Drop `runtime_leases_session_idx`; runtime lease lookups are by primary key, not session scans.
  - Drop redundant `workspaces_project_idx`; `workspaces_project_canonical_root_idx` already has `project_id` as the leading key.
  - Drop redundant `workflow_transition_groups_source_transition_idx`; `UNIQUE(source_node_id, transition_id)` already creates an equivalent lookup index.
  - Drop redundant `tasks_project_short_id_idx` if `tasks.short_id` remains; `UNIQUE(project_id, short_id)` already creates an equivalent lookup index. If `short_id` is dropped, this index disappears with it.
  - Drop `task_comments.source_run_id`; comments remain task-local notes with author/body/timestamps, not run artifacts.
  - Replace comment soft-delete (`task_comments.deleted_at_unix_ms` plus filtered index) with hard delete unless deleted-comment audit/history is explicitly product-required.
  - Drop transition display snapshot duplicates (`task_transitions.source_node_key`, `source_node_display_name`, `transition_display_name`) after run-start snapshots become the sole historical display source.
- Still-unapproved second-pass candidates:
  - `tasks.source_url` is not approved for removal. Keep source URL as a structured task field unless a later explicit decision changes this.
  - Drop `task_comments.author_id` unless a concrete multi-agent/user identity display is made mandatory; `author_kind` is enough for current product behavior.
  - Drop transition-edge display snapshot duplicates (`task_transition_edges.edge_key`, `target_node_key`, `target_node_display_name`, `target_node_kind`) after run-start snapshots become the sole historical display source.
  - Drop transition-edge config snapshots (`context_mode`, `requires_approval`, `input_bindings_json`, `output_requirements_json`) after pending approval/application reads edge config from the source run-start snapshot.
  - Drop `sessions.first_prompt_preview`; derive the preview by bounded read of the first transcript event for session-list rows.
  - Drop `sessions.input_draft` if unsent prompt draft recovery is not mandatory.

## Open Questions

- Workflow editor implementation is blocked until the top-level ownership/navigation model is agreed. Existing editor decisions below are draft notes, not final implementation requirements.
- Decision: implement the Link workflow picker as the global right-side sidebar island. No fallback to native blocking windows.
- Guardrail: Workflow Library must not expose task creation in this redesign. Task creation remains project-originated because tasks require project, workspace, and project-workflow-link context.
- Need decide whether Projects should be renamed/reframed in UI as workspace groups, execution contexts, or kept as Projects.
- Visual artifacts should be `.drawio` only going forward because the user is inspecting diagrams in diagrams.net/draw.io.
- Need decide the concrete editing interaction model: inspector/forms, edge creation gestures, delete confirmations, and whether Save applies a batch or sequential mutations.
- Need determine how server should expose workflow active-task editability/blockers.
- Need determine whether full CRUD includes workflow-level create/copy/link management in this route or only graph definition editing for an existing linked workflow.
- Need decide whether changing node kind to/from `start` should be hidden or blocked in v2 forms to preserve the Backlog deletion decision.
- Do not use transfer-to-project semantics unless explicitly reintroduced. Workflows are reusable definitions; project association should be expressed as link/unlink.
- Temporary implementation debt: replace persisted `workflow_events` UI invalidation table with in-memory pub/sub and drop all related DB/code paths as a hard cutover, with no compatibility shim or fallback. Do not implement until ownership/navigation redesign is settled, and do not let future work assume the table remains available.

## UX Design

- Default mode is an editable graph canvas built from v1:
  - Initial layout comes from ELK.
  - Nodes/groups become draggable while the editor is open.
  - Dragged positions are kept in local React state only and are discarded on route close/reopen or Reload remote.
  - Existing fit/zoom/reset controls stay in the top-left island; reset can reset viewport, while reopening resets layout.
- Authoring controls:
  - Add node: plus button in the top-left tool island opens the node edit surface. On successful draft add, place the node at the current viewport center.
  - Add group: prefer a second top-left tool button that opens a group edit surface and creates an empty group. This is simpler than drag-over grouping/context menus and matches the requested acceptable options. Dragging nodes into groups can be deferred unless cheap.
  - Create edge: drag from a node connector/handle to a target node.
  - Invalid edge drag to empty canvas snaps back silently.
  - Invalid edge drag to an incompatible node snaps back and shows a toast.
  - Valid edge drag opens the edge/transition edit surface before adding to the local draft.
  - Click node opens node edit surface.
  - Click edge opens edge/transition edit surface.
- Entity edit surfaces:
  - Move away from native blocking windows for workflow UX. Prefer the global right-side sidebar island for intermediary, picker, settings, and entity-edit flows where practical.
  - Avoid nested blocking surfaces. Destructive confirmation on Save should be a single terminal sidebar/route-level confirmation state.
  - Form sections should be islands: identity, behavior, outputs, bindings/requirements, validation preview.
- Dirty state:
  - Route-level Save and Discard controls should be visible when local draft differs from source graph revision.
  - Validation issues update from local draft on every change using the same shared `WorkflowValidationIssues` component.
  - Local draft invalid for execution can still be saved if draft validation has no blocking hard errors.
- Conflict state:
  - If a subscription event changes the same workflow while local draft is dirty, keep the local draft and show a conflict banner.
  - Banner actions: Reload remote, keep editing. Save should use expected graph revision and reject stale definitions with a clear error if remote graph changed.
- Deletion:
  - Deleting Backlog/start is blocked in v2 with a toast.
  - Deleting Done/terminal is allowed only when another terminal node remains; otherwise block with a toast.
  - Deleting other graph entities is allowed in the local draft, but actual task/history impact is checked on Save.
  - Save confirmation should list affected nodes/edges and affected task count when persisted backlog/done tasks would be deleted as a consequence of removing referenced graph entities.
- Global sidebar:
  - Add a shell-level `SidebarHost` inside `AppChrome` beside `RouteTransitionFrame`.
  - Sidebar visual treatment: fixed right overlay, glass island, full-height below titlebar with left rounded corners, adaptive width around 420-560px and max `calc(100vw - margins)`.
  - Sidebar destinations should be terminal enough to avoid stacked blocking surfaces; internal sidebar navigation is allowed for picker flows, and child picker destinations return results to the previous sidebar screen.
  - Main route navigation, back/forward, or route unmount closes the sidebar and rejects/returns canceled for pending opener promises.
  - Initial sidebar destinations for this feature: workflow create, workflow settings, project picker.

## Architecture

- Keep GUI as a remote-control surface. The server remains authoritative for persisted workflow graph, draft hard validation, task-impact analysis, conflict detection, graph revision increments, destructive task deletion, and event publishing.
- Add a GUI-local `WorkflowDraft` model:
  - Source: current `WorkflowDefinition` plus `graphRevision`.
  - Draft contains normalized maps/arrays for workflow record, node groups, nodes, transition groups, and edges.
  - Draft tracks in-memory layout positions separately from graph definition data.
  - Draft IDs for new entities can be generated client-side with stable prefixed IDs for the draft session; server validates uniqueness on Save.
  - Provide pure reducers/actions: add/update/delete node, add/update/delete group, add/update/delete transition group, add/update/delete edge, move node/group position, reset from remote.
- Add local draft validation:
  - Prefer extracting a shared validation adapter so the route can validate a `WorkflowDraft` without persisting. Since current `workflow.validate` only validates persisted workflows, add a server endpoint that validates a supplied draft definition in `draft` and `execution` contexts.
  - UI should show draft validation immediately after edits. If round-trip validation is too slow, debounce and show previous validation while pending.
- Add batch save APIs rather than issuing sequential existing add/update calls:
  - Existing per-entity mutation APIs are useful for CLI but do not fit local Save/Discard, conflict detection, or destructive-impact confirmation.
  - Proposed endpoint: `workflow.graph.savePreview` with `workflow_id`, `expected_graph_revision`, and full draft definition. It returns draft validation, execution validation, active-task blockers, destructive impact, and whether confirmation is required.
  - Proposed endpoint: `workflow.graph.save` with the same payload plus destructive confirmation. It recomputes validation/impact in one transaction, rejects stale graph revisions, rejects active-task blockers, rejects unconfirmed or changed destructive impact, applies the diff, increments graph revision, records linked-project events, and returns the saved definition plus validations.
  - If implementation wants fewer endpoints, `workflow.graph.save` can return a typed `confirmation_required` response before applying, and the GUI retries with confirmation.
- Server save semantics:
  - Validate supplied draft in `draft` context. Reject if `HasBlockingErrors`.
  - Allow semantic/execution validation errors to persist as a Workflow Draft.
  - Disallow editing while workflow has active tasks. Active means any task whose active/waiting placement is not start/backlog or terminal/done, any pending approval, any non-completed/non-interrupted run needing runtime ownership, or any other non-terminal automation state. Backlog and terminal-only tasks are allowed.
  - Compute destructive impact from deleted/changed graph entity references before applying. If deletion would orphan persisted backlog/done tasks, return impact for confirmation. If active tasks are impacted, block.
  - Apply graph diff transactionally. Delete impacted tasks only after confirmation and only if they still match the preview's safe backlog/done criteria.
  - Preserve graph revision as the conflict boundary. Save requires expected current graph revision.
- Add process-local workflow edit locks:
  - Implement in `workflowsvc`, not persisted in metadata DB.
  - API shape: begin/acquire edit session, heartbeat, release. Responses include token, expiry, current graph revision, and blockers.
  - Lock registry is protected by a mutex and keyed by workflow ID. Locks expire by TTL when heartbeat stops.
  - Begin lock first checks no active workflow tasks. If another live lock exists, return a typed blocker.
  - Task-starting / automation-changing service methods check the lock registry and reject when another editor owns a lock for the workflow. At minimum: start task, manual move out of backlog/done, approve transition, resume run, and scheduler enqueue paths that can create new active work.
  - Save requires the caller's lock token and recomputes active-task blockers in the same authoritative save path. If the server restarted or the lock expired, Save must fail with a recoverable "edit session expired" error and the GUI can try to reacquire if no active tasks exist.
  - Because locks are non-persisted, server restart cannot permanently block a workflow. Reconnect path must treat locks as lost and reacquire.
- Delete API/store changes:
  - Do not expose raw `DeleteNode`/`DeleteEdge` directly to GUI as independent operations in v2. Route all local draft deletes through batch save so Save can evaluate active tasks and destructive impact once.
  - Add internal store helpers for graph diff application and safe destructive task cleanup instead of bypassing FK constraints manually.
  - Existing store delete guards are still useful for no-history deletes, but the batch path needs explicit task-impact handling because current guards block any non-terminal task references and any history references.
- React Flow integration:
  - Switch from controlled read-only graph arrays to draft-backed controlled nodes/edges with `onNodesChange`, `onConnectStart`/`onConnectEnd` or `onConnect`.
  - Keep custom node/edge renderers and visible handles.
  - Add selection/click handlers that open the relevant sidebar edit surface.
  - Keep ELK layout adapter as source layout; merge local position overrides after layout.
- Project scoping:
  - Keep route blocked for unlinked workflows.
  - Add editability query/response to workflow editor data. If not editable, graph can remain viewable but authoring controls and Save are disabled with a visible reason.
  - Continue subscribing to linked project workflow events. Clean remote updates can refetch; dirty remote updates trigger conflict banner.
- Workflow creation/linking:
  - Add GUI API methods for workflow create/list/link/unlink/default flows as needed.
  - Project "Link workflow" uses the global right-side sidebar island, listing all global workflows with linked/unlinked state for the current project, supporting linking an existing workflow, and exposing "New workflow".
  - Project-originated "New workflow" should create the workflow, link it to the current project, optionally set it as selected/default according to product decision, and open the editor.
  - Global Workflow Library should list all workflows, open existing workflows in the editor, and create workflows without implicit project linkage.
  - Workflow settings sidebar should update workflow metadata through batch/dedicated update endpoint and use project picker result to link/unlink projects.

## Implementation Plan

- Draft implementation slices:
  - Schema audit checkpoint: audit caveats from generated latest SQLite DDL (`sqlite_schema` after applying embedded up migrations to a temp DB), not by manually correlating queries with migration files.
  - Blocking task-link normalization checkpoint: hard-cutover `tasks` to store `project_workflow_link_id` as the single FK for project-workflow pairing, remove duplicated `tasks.project_id`/`tasks.workflow_id`, derive task project/workflow through joins/read models, update store queries/API DTO assembly, and add migration/tests before any GUI work.
  - Blocking sidebar UX/API checkpoint: approve destination types, result/cancel semantics, route-change auto-close behavior, local sidebar navigation, and visual shell constraints before Link workflow implementation starts.
  - Blocking sidebar implementation checkpoint: implement shell/sidebar provider/host, typed destination registry, result/cancel contract, route-change auto-close, and tests.
  - Server contracts: draft validate, save preview/save response types, edit blockers/impact DTOs, protocol routes, gateway handlers, remote client methods, tests.
  - Store/service: process-local edit locks, workflow editability query, graph diff planner, transactional save, destructive impact detection, safe task cleanup for confirmed backlog/done task deletion, linked-project event fan-out, tests.
  - GUI API: models/schemas/client methods for draft validation/save/editability and typed error/confirmation responses, tests.
  - GUI draft state: pure draft model/reducers, diff/dirty detection, validation query integration, conflict handling, tests.
  - Canvas editing: draggable nodes/groups, local coordinate overrides, add buttons, connector drag behavior, node/edge click selection, tests.
  - Sidebar editing surfaces: workflow/node/group/edge forms with advanced edge fields and task-detail-style island layout, tests.
  - Save/Discard UX: dirty toolbar, save preview, destructive confirmation, stale revision handling, disabled/edit blocker states, tests.
  - Workflow Library/linking guardrail: library surfaces remain definition management only; task creation stays project-originated and must not be added to the library route.
  - QA/docs: browser proof for add/edit/save, invalid draft validation, edge drag, conflict banner where feasible, docs/release notes if public docs mention workflow editor read-only status.
