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
- The canvas renders start/backlog, agent, join, terminal/done, disconnected nodes, and node groups.
- Join nodes are inspectable internal merge plumbing. Board/Kanban read models still omit join columns.
- GUI-authored node groups are execution-shaped parallel groups. A saved node group contains branch nodes and one join; its fan-out is represented by one transition group with multiple outgoing edges.
- One-node node groups may exist only as unsaved invalid drafts while the operator is building the parallel group.
- Agent nodes show name plus assignee role. Non-agent/no-role nodes have blank role line.
- Node kind color communicates kind: start primary/blue, agent neutral/gray, join secondary/yellow, terminal success/green.
- Edge color communicates context-preservation mode: `new_session` primary/blue, `continue_session` neutral/gray, and `compact_and_continue_session` secondary/yellow.
- Validation-error red is reserved for invalid graph entities and overrides normal semantic colors.
- Edge labels show transition group display name if present, otherwise `transition_id`; fan-out multi-edge groups append edge key only when needed to disambiguate.
- Node groups render visually as labeled branch islands. The owned Join renders outside the island to the right, vertically centered with the island, while remaining owned by the node group. Branch-to-Join edge routes are normalized into root canvas coordinates before rendering. Empty node groups are not a saved workflow concept.
- Canvas layout is deterministic client-side ELK layout from graph structure. Coordinates are not persisted.
- Layout orientation is left-to-right.
- Initial viewport fits the whole graph on first open. Live refetch preserves pan/zoom, clears stale selection through current React Flow state, and shows workflow-updated feedback.
- Canvas controls are inspect workflow, zoom in/out, fit-to-view, and reset zoom in a top-left floating island. There is no minimap.
- The canvas legend starts collapsed in the bottom-left corner and uses a help/question-mark affordance when collapsed.
- The canvas add-node affordance uses a plain plus icon. Zoom controls use zoom-specific icons or a visually separate zoom control so `+` consistently means add.
- Keyboard shortcuts are zoom in/out, reset, fit-to-view, and delete selected editable graph entities.
- Expected scale is optimized for 5-50 nodes and 5-100 edges, with graceful behavior up to roughly 200 nodes.

## Draft Editing

- Editing uses GUI route-local draft state. Server mutations apply only on Save.
- Draft state owns workflow metadata, agent-node editable fields, required inputs, join provider selections, dirty state, remote conflicts, and draft version counters.
- Workflow metadata editing includes workflow name and description through the workflow inspector/settings sidebar.
- Agent node editing includes display name, key, assignee role, prompt template, and required input fields.
- Agent prompt editing shows placeholder chips directly below the prompt field. Required input placeholders appear first in declaration order and use the primary color; built-in task/node placeholders follow and use muted outline styling. Clicking a chip inserts the exact Go-template placeholder at the prompt cursor, or at the end when the prompt field is not focused.
- Start/backlog and terminal node editing includes display name and key. Their kind, execution config, and required inputs stay fixed by domain validation.
- Required input fields are consuming-node-owned, draggable sidebar islands.
- New required inputs insert at the top of the node input list.
- Required input names render as bold ellipsized inline titles that become compact inputs when clicked.
- Required input descriptions use the placeholder-only `Model-facing description` input.
- Required input deletion uses a red title-row trash icon.
- The assignee dropdown is sourced from server readiness `subagent_roles`.
- If a workflow references a legacy role no longer configured, the current legacy role remains visible/selectable instead of forcing the placeholder state.
- Join editing shows downstream required inputs derived by the server and stores one provider selection per input.
- Join provider selection points to an actual incoming edge into that join.
- Source-node output fields define reusable outputs that prompt templates can reference through `.Nodes.<node_key>.<output_name>`. Edge input bindings and edge output requirements are not user-authored editor concepts. The server derives them from consuming-node required inputs, prompt node-output references, graph topology, and join provider selections.
- Agent node inspectors show read-only `Provides` summaries so operators can understand what the server will ask an agent to produce.
- Inspector validation sections keep their section header and render errors as plain bullet lists without card containers or code chips.
- Edge inspectors show the source-to-target node relationship as an equal-width route graphic at the top of the route/config island, plus read-only derived input bindings, derived provision requirements, provider requirements, and validation issues.
- Edge inspectors edit route/config facts: transition group display name, transition ID, edge key, approval flag, context-preservation mode, and context source. Context source remains visible but disabled for `new_session` edges. Context mode, context source, and approval remain visible but disabled for the edge emitted by the Start node. Disabled route controls explain that they are not applicable for the current edge configuration. Edge targets are assigned through canvas connections instead of inspector dropdowns.
- Unsupported graph entities use read-only sidebar inspection with clear unavailable-editing behavior.
- Topology editing includes adding and deleting agent/terminal nodes, node groups, and edges, drag-connecting edges on the canvas, editing edge route/config facts, and creating/removing node group membership.
- Add node is a canvas action, not a right-sidebar form. It creates unconnected agent or terminal nodes; draft/execution validation explains unreachable or incomplete graph states until the operator wires them.
- Drag-connecting from a source node to a target node creates a new transition group by default, with one edge to the target node.
- Adding an edge to an existing transition group is an explicit fan-out/group action, not the default drag-connect behavior.
- Node deletion cascades incident edge deletion and removes transition groups that become empty from that deletion.
- Deleting the final edge in a transition group removes that transition group.
- Deletion is available through keyboard delete/backspace, context menu actions, and inspector trash actions where the selected entity is editable.
- Direct join creation is not exposed as generic node creation. Joins are created through node group/parallelism editing.

## Save, Validation, And Conflicts

- Route-level Save/Discard appears in one bottom-right workflow-editor status island.
- The status island owns unsaved state, validation issues, save blockers, remote conflict state, and save errors.
- Draft validation and execution validation are shown separately. Draft validation blocks graph-dirty saves when it has blocking errors.
- Execution validation errors remain visible but do not block metadata-only saves.
- Metadata-only and no-op saves bypass graph edit policy and active-work blockers.
- Actual graph changes run save preview before save.
- Save preview returns draft validation, execution validation, active-task blockers, destructive/removal impact, and confirmation requirement.
- Destructive graph saves are confirmed inside the bottom-right workflow-editor status island. The editor does not open a modal or sidebar for graph-save confirmation.
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
- The workflow editor toolbar Add node control opens its node-kind popup on hover or focus. Clicking the toolbar button itself does not create a node or toggle the popup.

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
- Backlog/start deletion is out of scope. Blocked graph deletes surface as toast feedback. Hide `start` from add/kind-change controls. Existing Backlog can be renamed where safe, but kind stays fixed.
- Start node outgoing edges may be edited in drafts, but execution validation requires exactly one start transition group with exactly one edge targeting an agent node.
- Done/terminal deletion is allowed only when at least one other terminal node remains; otherwise block with toast.
- Saved node groups must be execution-shaped parallel groups. A node group without enough branch nodes or without exactly one owned join blocks save validation.
- Dragging a node in the workflow editor changes node group membership, not persisted canvas position. The real node remains in its derived layout position and a drag ghost follows the pointer. Canvas layout remains derived from the graph.
- Node group drag/drop is validated as a membership operation. If the editor cannot safely infer the source node or transition group needed for fan-out wiring, the drop is blocked instead of committing invalid membership.
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
- Q: What does drag-connecting an edge from a node with existing outgoing transitions do? A: It creates a new transition group by default. Fan-out into an existing transition group is explicit.
- Q: Should add-node create an incoming edge automatically? A: No. New nodes are unconnected until the operator wires them.
- Q: Which node kinds does generic add-node expose? A: Agent and terminal. Start is fixed, and join is created through node group/parallelism editing.
- Q: Where is destructive graph-save confirmation shown? A: In the workflow-editor status island.
- Q: What does dragging a node mean in the workflow editor? A: Node group membership DnD, not canvas repositioning.
