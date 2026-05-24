# Workflow Editor Spec

## Product Direction

- Workflow editor/library UX is about how operators use top-level reusable workflows, not only graph-editing mechanics.
- Daily work remains project-first. Users open a project, then choose one linked reusable workflow.
- Workflows are top-level reusable definitions in the data model, presented inside projects as linked process lenses rather than owned children.
- Projects stay first-class daily-work destinations because they provide task namespace, workspaces, source workspace defaults, and project-workflow-link context.
- Home keeps Projects as the primary picker and global Inbox as the secondary pane.
- A global Workflow Library entry point exists for creating/editing reusable workflow definitions across projects without entering a project first.
- Workflow Library is definition management only. It lists/opens/creates/deletes workflows and manages project links. It does not create tasks or act as an aggregate task board.
- Task creation remains project-originated because tasks require project, workspace, and project-workflow-link context.
- Project-workflow pairing is visible in UI. A task derives project/workflow execution context through `project_workflow_link_id -> project_workflow_links`.
- Do not use transfer-to-project semantics unless explicitly reintroduced. Workflows are reusable definitions and project association is link/unlink.

## Implemented Editor Scope

- The current editor is an input-first workflow definition editor with a graph canvas, route-local draft state, sidebar edit forms, validation, conflict handling, and atomic Save/Discard.
- The editor route accepts project context when opened from a board and also supports global workflow-definition context for Workflow Library usage.
- A project-scoped editor route is project-link gated. Direct project-context route with an unlinked workflow shows a blocker rather than displaying the workflow.
- Runtime/task state remains in Kanban and task detail. The editor edits workflow definitions, not live task execution.
- The canvas renders start/backlog, agent, join, terminal/done, disconnected nodes, and empty groups.
- Join nodes are inspectable internal merge plumbing. Board/Kanban read models still omit join columns.
- Agent nodes show name plus assignee role. Non-agent/no-role nodes have blank role line.
- Node kind color communicates kind: start primary/blue, agent neutral/gray, join secondary/yellow, terminal success/green.
- Edge color communicates context-preservation mode: `new_session` primary/blue, `continue_session` neutral/gray, and `compact_and_continue_session` secondary/yellow.
- Validation-error red is reserved for invalid graph entities and overrides normal semantic colors.
- Edge labels show transition group display name if present, otherwise `transition_id`; fan-out multi-edge groups append edge key only when needed to disambiguate.
- Groups render visually as non-interactive boxes with labels. Empty groups render with an `Empty group` placeholder.
- Canvas layout is deterministic client-side ELK layout from graph structure. Coordinates are not persisted.
- Layout orientation is left-to-right.
- Initial viewport fits the whole graph on first open. Live refetch preserves pan/zoom, clears stale selection through current React Flow state, and shows workflow-updated feedback.
- Canvas controls are inspect workflow, zoom in/out, fit-to-view, and reset zoom in a top-left floating island. There is no minimap.
- Keyboard shortcuts are `+`/`-` zoom, `0` reset, and `f` fit.
- Expected scale is optimized for 5-50 nodes and 5-100 edges, with graceful behavior up to roughly 200 nodes.

## Draft Editing

- Editing uses GUI route-local draft state. Server mutations apply only on Save.
- Draft state owns workflow metadata, agent-node editable fields, required inputs, join provider selections, dirty state, remote conflicts, and draft version counters.
- Workflow metadata editing includes workflow name and description through the workflow inspector/settings sidebar.
- Agent node editing includes display name, key, assignee role, prompt template, and required input fields.
- Required input fields are consuming-node-owned, draggable sidebar islands.
- New required inputs insert at the top of the node input list.
- Required input names render as bold ellipsized inline titles that become compact inputs when clicked.
- Required input descriptions use the placeholder-only `Model-facing description` input.
- Required input deletion uses a red title-row trash icon.
- The assignee dropdown is sourced from server readiness `subagent_roles`.
- If a workflow references a legacy role no longer configured, the current legacy role remains visible/selectable instead of forcing the placeholder state.
- Join editing shows downstream required inputs derived by the server and stores one provider selection per input.
- Join provider selection points to an actual incoming edge into that join.
- Source-node output fields, edge input bindings, and edge output requirements are not user-authored editor concepts. The server derives them from consuming-node required inputs, graph topology, and join provider selections.
- Agent node inspectors show read-only `Provides` summaries so operators can understand what the server will ask an agent to produce.
- Edge inspectors show route/config facts and read-only derived input bindings, derived provision requirements, provider requirements, and validation issues.
- Start/backlog, terminal/done, group, and unsupported graph entities use read-only sidebar inspection with clear unavailable-editing behavior.
- Topology CRUD is not part of the current GUI editor scope; the current editor edits workflow metadata, agent nodes, required inputs, and join provider selections.

## Save, Validation, And Conflicts

- Route-level Save/Discard appears in one bottom-right workflow-editor status island.
- The status island owns unsaved state, validation issues, save blockers, remote conflict state, and save errors.
- Draft validation and execution validation are shown separately. Draft validation blocks graph-dirty saves when it has blocking errors.
- Execution validation errors remain visible but do not block metadata-only saves.
- Metadata-only and no-op saves bypass graph edit policy and active-work blockers.
- Actual graph changes run save preview before save.
- Save preview returns draft validation, execution validation, active-task blockers, destructive/removal impact, and confirmation requirement.
- Save recomputes validation and impact transactionally, rejects stale workflow versions, rejects active blockers, rejects unconfirmed or changed destructive impact, applies metadata and graph changes atomically, increments workflow version once, publishes linked-project events, and returns the saved definition plus validations.
- Workflow definitions use one monotonic `version` over persisted definition changes. Metadata-only changes and graph changes each increment it once; combined metadata+graph saves also increment it once; no-op saves increment neither.
- If a subscription event changes the same workflow while the local draft is dirty, keep the local draft and show a conflict banner.
- Conflict banner actions are Reload remote and Keep editing.
- Save uses the expected workflow version and stale save rejects clearly.
- Workflow-scoped subscriptions are required so global Workflow Library editor mode gets the same reactive conflict behavior as project-linked editing.

## Workflow Library And Linking

- Project workflow management uses `Link workflow` language.
- Link workflow opens a global right-side sidebar picker listing all reusable workflows and a `New workflow` action.
- `New workflow` from project-originated Link workflow creates a global workflow definition, auto-links it to the originating project, and opens the editor.
- Project-originated `New workflow` uses default policy `if_project_has_none`: it becomes the project default only when the project has no default workflow yet. It does not replace an existing default workflow.
- `New workflow` from global Workflow Library creates a global workflow definition without implicit project linkage unless the user explicitly links it later.
- Workflow editor/library routes may be global workflow-definition routes, but project-originated task/board routes remain project-scoped.
- The editor may own current-workflow settings/delete actions. Workflow Library/sidebar owns create/copy/link entry points.
- Workflow settings include name/description and actions to link/unlink workflow to projects.
- Project selection for workflow settings/linking is paginated and minimal, hosted inside the sidebar rather than a native blocking window.

## Global Sidebar

- Workflow intermediary, picker, settings, and entity-edit flows use a global right-side sidebar island.
- Do not use Tauri native blocking windows for workflow UX.
- Sidebar stretches from the right side of the screen and is reusable from board/editor/other pages.
- Sidebar supports typed destinations, local sidebar navigation, and returning result/cancel to opener/current screen.
- Opening another main destination or navigating back/forward closes the sidebar and rejects/returns canceled for pending opener promises.
- Sidebar visual treatment: fixed right overlay, glass island, full-height below titlebar, left rounded corners, adaptive width around 420-560px and max `calc(100vw - margins)`.
- Sidebar destinations should be terminal enough to avoid stacked blocking surfaces. Child picker destinations may return results to previous sidebar screen.

## Editing Constraints

- GUI remains a remote-control surface. Server remains authoritative for definitions, validation, persistence, project links, events, task-impact analysis, workflow version, and destructive cleanup.
- Editing a linked workflow is disallowed while it has active tasks.
- Editing is allowed only when existing tasks are all backlog or done.
- Active means any task whose active/waiting placement is not start/backlog or terminal/done, any pending approval, any non-completed/non-interrupted run needing runtime ownership, or any other non-terminal automation state.
- Backlog/start deletion is out of scope. Hide `start` from add/kind-change controls. Existing Backlog can be renamed where safe, but kind stays fixed.
- Done/terminal deletion is allowed only when at least one other terminal node remains; otherwise block with toast.
- Destructive delete impact is evaluated on Save, not at draft edit time.
- Save runs server-side impact check for pending graph diff.
- If active tasks would be affected, Save is blocked.
- If only backlog/done tasks would be orphaned/deleted by removed nodes or edges, show confirmation listing affected nodes/tasks before applying.
- Requested destructive wording pattern: `XXX tasks will be **gone forever** along with the node. Proceed?`
- Workflow graph saves never delete or move tasks; whole-workflow deletion is the task-deleting path.

## Q/A Decisions Preserved

- Q: Should runtime/task overlay appear in editor? A: No; runtime is handled by Kanban/task detail.
- Q: Should workflow editor UX use native blocking windows? A: No; use the global right-side sidebar.
- Q: Should workflow library create tasks? A: No; it is definition management only.
- Q: Is task-link normalization compatible? A: No compatibility shim; hard cutover.
- Q: Should project-originated `New workflow` replace an existing project default? A: No; link it and open the editor, but set it as default only when the project has no default.
- Q: Where do workflow-level management actions belong? A: Editor may own current-workflow settings/delete, while create/copy/link remain in Workflow Library/sidebar.
