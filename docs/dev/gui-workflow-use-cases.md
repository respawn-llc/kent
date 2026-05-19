# GUI Workflow Use-Case Inventory

> Superseded for MVP implementation: use `docs/dev/gui-workflow-mvp-prd.md` for active product behavior and `docs/dev/gui-workflow-mvp-implementation-plan.md` for implementation sequence. This file remains a broader inventory/reference.

Status: superseded for MVP implementation; broad full-scope inventory reference only.

Date: 2026-05-16.

## Purpose

Define the GUI-visible work scope from the async workflow orchestration plan. This inventory intentionally includes use cases beyond the first MVP so we can reason about product shape, backend dependencies, and feature sequencing before screen design.

Source backend plan: `docs/dev/async-workflow-orchestration.md`.

Related broader GUI parity inventory: `docs/dev/gui-cli-parity-checklist.md`.

Related final MVP PRD: `docs/dev/gui-workflow-mvp-prd.md`.

Related MVP PRD-writing checklist: `docs/dev/gui-workflow-mvp-prd-checklist.md`.

Related GUI design skill: `.builder/skills/ui-design/SKILL.md`.

## Ordering Rule

Keep MVP/foundation sections in dependency order: macOS/native surface, visual design system, navigation/lifecycle, startup/backend readiness, project entry, workflow board, task creation/detail/teleport. Keep open PRD decisions as a final appendix.

Exhaustiveness claim: none. This is a manual, source-reviewed work-scope inventory for the current PRD text, not proof that every future product edge case or runtime state is captured.

## Scope Tags

- `MVP`: first-class first GUI scope unless later product decisions move it.
- `PRD decision`: candidate MVP scope that must be explicitly accepted or cut before implementation.
- `Later`: known GUI scope after first MVP or behind later backend slices.
- `Agent/CLI for MVP`: needed for product workflow, but acceptable outside GUI initially.
- `Non-GUI MVP`: backend/CLI/agent-control capability that informs GUI but is not a direct GUI surface yet.

## Product Decision: First GUI MVP Cut

This section is a prioritization note, not the scope of this document. The full use-case inventory below remains the canonical work-scope list.

The first GUI MVP operates existing workflows. Workflow authoring can remain agent/CLI-driven while the desktop foundation and workflow runtime surfaces stabilize.

Backend workflow endpoints/read models come before real workflow UI integration. GUI shell/prototype work can proceed with local fixtures, but MVP workflow surfaces require real Builder server integration.

Delivery uses vertical slices: backend plumbing/read models first, then GUI integration for that slice.

First-class MVP surfaces:

- Use macOS blurred glass/vibrancy window background.
- Use consistent UI brand, palette, fonts, design tokens, and locked main widgets.
- Use navigation stack, dialogs, and destination lifecycle infrastructure for view-models/subscriptions/process cleanup.
- Launch app and see plain-text setup/startup problem causes for onboarding incomplete, missing auth, server unavailable, missing/invalid config, readiness failure, protocol mismatch, capability mismatch, and unknown failures.
- Connect to Builder server.
- Load app/server config and capability registry.
- Use project-first navigation: app opens to project picker/recent projects, supports project creation through OS-native directory picker, and scopes board/task routes under selected project.
- Navigate between project picker, board, task detail dialog, settings, diagnostics, and future workflow-adjacent surfaces.
- Show global feedback such as toasts/errors/progress.
- Operate existing workflow tasks on a Kanban board.
- Add tasks from GUI.
- View completed tasks.
- Watch tasks move visually through workflow states.
- Open task cards into a detail dialog.
- Inspect task worktree path, agent role, agent status, session ID/name, and teleport action from detail.
- Teleport into task session chat in terminal through OS-native open/process commands.
- Display clear validation/config-drift blockers from server read models.

Open PRD decisions are tracked in the final appendix.

Deferred from MVP:

- Workflow graph editor.
- Built-in GUI chat UI.
- Fan-out/join authoring UI.
- Rich scheduler admin dashboard.
- Workflow import/export/versioning.
- Full transcript debugger.
- Manual move UI beyond minimal server-supported recovery actions.
- Windows/Linux release targets.

## Backend Readiness Map

This map keeps GUI sequencing concrete. "Ready now" means the GUI can build the surface with local fixtures/mock transport before workflow APIs exist. "Backend-gated" means the surface should wait for the named GUI workflow backend slice in `docs/dev/gui-workflow-backend-slice-plan.md` before real integration.

- [x] Backend slice/API plan is tracked in `docs/dev/gui-workflow-backend-slice-plan.md`.
- [ ] Ready now: App shell, splash, navigation, global toasts, and rendering states. Use local fixtures and app-level state.
- [ ] Partly ready now: Server connectivity, config loading, and capability registry. Existing `/healthz` and `/readyz` can prove connectivity. Capability/config registry needs a small server API contract before real feature gating.
- [ ] Backend-gated: Project picker and Home project rows. Depends on GUI backend Slice 1 Home/project admin/project key/workspaces.
- [ ] Backend-gated: Board read-only views. Depends on GUI backend Slice 2 workflow picker, selected board, groups, and live updates.
- [ ] Backend-gated: Task create, Backlog editing, source workspace, drag-to-start, and worktree/branch display. Depends on GUI backend Slice 3 task source workspace and Backlog editing.
- [ ] Backend-gated: Cancel, resume, interrupt, attention rows, questions, and approvals. Depends on GUI backend Slice 4 actions, attention inbox, questions, and approvals.
- [x] Backend-gated: Task detail read-only views, activity feed, comments, and teleport identifiers. Depends on GUI backend Slice 5 task detail, activity feed, comments, and teleport target.
- [ ] Backend-gated: Manual moves/recovery controls. Later than MVP unless promoted into a GUI backend slice.

## PRD Coverage Audit

This checklist maps major sections of `docs/dev/async-workflow-orchestration.md` to GUI-visible inventory areas. `Covered` means relevant GUI-facing use cases are listed below. `Partial` means the PRD section is mostly backend-owned, with only user-visible diagnostics or settings represented here. `Non-GUI` means no direct GUI use case was identified beyond consuming server APIs/read models.

- [x] Covered: Purpose, Current Idea, Known Constraints. Mapped to app/server setup, project/workflow setup, board/task state, validation/config drift, and no full transcript loading.
- [x] Covered: External Reference. Mapped to workflow pattern support for prompt chaining, routing, parallelization, orchestrator-workers, evaluator-optimizer loops, and autonomous agent nodes.
- [x] Partial: Remaining Implementation Risks. Backend-owned proof work; GUI-visible outcomes are mapped to questions, live runs/completion output, worktree/session context, scheduler/recovery, and validation/config drift.
- [x] Covered: Product Decisions. Mapped to project/workflow setup, workflow definition management, graph authoring, task lifecycle, board state, worktree/session context, and scheduler/recovery.
- [x] Covered: Completion Control Schema. Mapped to live runs and completion output, workflow graph authoring, and validation/config drift.
- [x] Covered: Graph Revision Policy. Mapped to workflow definition management, task detail/history, approvals, and validation/config drift.
- [x] Covered: Identifier And Key Rules. Mapped to project/workflow setup, workflow definition management, graph authoring, and task lifecycle.
- [x] Partial: Workflow Mode Prompting. Mapped to live runs and completion output plus task detail/history/comments. Prompt injection itself is backend-owned; GUI may expose debug/read-model summaries later.
- [x] Covered: Input Binding Direction. Mapped to workflow graph authoring, workflow pattern support, and parallel branches/joins.
- [x] Covered: Manual Move Requirements. Mapped to manual moves/rework, approvals, and validation/config drift.
- [x] Covered: Audit Direction. Mapped to task detail/history/comments, approvals, and parallel branches/joins.
- [x] Partial: Domain Vocabulary. Backend-owned terminology; GUI-visible labels are reflected in task, workflow, node, transition, run, question, approval, scheduler, and comment surfaces below.
- [x] Partial: Backend Architecture Draft and Runtime Model. Backend-owned architecture; GUI dependencies appear in readiness checklist, app/server setup, live runs, and API/read-model assumptions.
- [x] Covered: Completion Runtime and Node Transitions. Mapped to live runs and completion output, approvals, manual moves/rework, and validation/config drift.
- [x] Covered: Parallelism And Joins. Mapped to workflow pattern support, parallel branches/joins, and board/task state.
- [x] Covered: Scheduler And Recovery. Mapped to scheduler/recovery/runtime control, questions, approvals, and live runs.
- [x] Covered: Workflow Config Surface. Mapped to scheduler/recovery/runtime control and app/server setup.
- [x] Covered: Worktrees. Mapped to worktree/session context, task lifecycle, and board/task state.
- [x] Partial: CLI Surface. Mapped to CLI and agent-control surfaces. GUI authoring is deferred, but agent/CLI workflow setup remains part of MVP operating model.
- [x] Partial: Data Model Draft, Indexes, Checks, Core Query Shapes. Mostly backend-owned; GUI-visible read models, history, validation, blockers, comments, and snapshots are covered.
- [x] Covered: Implementation Plan. Backend readiness checklist ties GUI surfaces to backend slices.

## Full PRD Coverage Inventory

This inventory tracks user-facing capabilities described by `docs/dev/async-workflow-orchestration.md`, including items outside first GUI MVP. Backend-only invariants are included only when they need a GUI surface, warning, setting, or debug explanation.

### macOS Window And Native Surface

- [x] MVP decision: User gets macOS blurred glass/vibrancy window background.
- [x] MVP decision: User gets native border, native shape, and native traffic-light/window controls integrated into app chrome.
- [x] MVP decision: User gets island-style floating rounded surfaces over the system glass background.
- [ ] MVP: User gets readable foreground surfaces over blurred glass background.
- [x] MVP decision: GUI feature components do not import Tauri/native APIs directly.
- [ ] Later: User gets equivalent visual system across future non-macOS targets if those targets ship.

### Visual Design System

- [ ] MVP: User gets consistent app chrome, navigation, dialogs, board, cards, forms, badges, toasts, empty/loading/error states, and typography.
- [x] MVP decision: User gets clean, elegant, effective island-style UI encoded as the core design principle.
- [x] MVP decision: User gets Builder/TUI-adjacent brand direction encoded in reusable UI tokens.
- [x] MVP decision: User gets Montserrat as main UI font and Monaspace Neon as monospace font.
- [x] MVP decision: User gets dark and light themes with config-based override and auto/system default.
- [x] MVP decision: Palette starts from current TUI and docs-site colors, with architecture for dynamic overrides and easy future changes.
- [x] MVP decision: Shared UI/theme source of truth starts app-local in `apps/desktop/src/ui/theme`; `apps/shared/*` stays reserved until a second GUI app consumer exists.
- [x] MVP decision: GUI uses `i18next`/`react-i18next` with static English locale files, no runtime language switch, and no hardcoded user-facing strings in components.
- [x] MVP decision: UI kit uses Lucide as icon pack.
- [ ] MVP: User can see global toasts/errors/progress for background operations.
- [ ] MVP: User can see standard empty/loading/success/warning/error rendering states.
- [ ] MVP: User gets animated UI transitions, including shared element transitions where practical.
- [x] MVP decision: Accessibility/keyboard-complete pass is not required for MVP.
- [x] MVP decision: UI strings are i18n-ready; no hardcoded user-facing strings in components.

### Navigation, Dialogs, And Lifecycles

- [x] MVP decision: Project picker coincides with Home/inbox/main destination and app-startup destination.
- [x] MVP decision: Home/project list is a full-screen destination, not modal and not permanent sidebar-only UI.
- [x] MVP decision: Relaunch restores last project board when possible and still allows navigating back to Home/project list.
- [x] MVP decision: Task details start as dialogs; right-side pane remains an allowed variant to test later.
- [x] MVP decision: Board starts without persistent sidebar to preserve Kanban width.
- [x] MVP decision: Project switching starts through Home/back navigation, breadcrumbs, shortcuts, or lightweight top-level controls, not persistent project list sidebar.
- [x] MVP decision: A hover-expandable non-modal popup is a candidate place for main nav/actions and workflow picker, to define later.
- [x] MVP decision: Hover-expandable non-modal popup contains navigation destinations such as Home/back and Inbox.
- [x] MVP decision: Hover-expandable non-modal popup contains New Task action.
- [x] MVP decision: Hover-expandable non-modal popup contains Pin action; unpinned state auto-collapses on unhover.
- [x] MVP decision: Pinned expanded popup becomes a floating-island-sidebar-like widget.
- [x] MVP decision: Expanded popup lists workflows; pagination is deferred unless implementation proves necessary.
- [ ] MVP: User can navigate from Home/project picker to project-scoped board, task detail dialog, settings, and diagnostics.
- [x] MVP decision: Home attention inbox lists questions and approvals; separate question/approval queue destinations are not required for MVP.
- [ ] MVP: User can use navigation stack and dialogs with deterministic destination lifecycle for view-models, subscriptions, native processes, and cleanup.
- [ ] MVP: User can rely on deterministic navigation stack and dialog close/back behavior.
- [ ] MVP: User does not lose project/task context when dialogs open/close or server reconnects.
- [x] MVP decision: Board top-level navigation/sidebar model preserves board space and avoids persistent project switching sidebar for initial MVP.

### Startup Safety And Setup Errors

- [ ] MVP: User can launch desktop app and see splash/loading while GUI config and server connectivity initialize.
- [ ] MVP: User can launch desktop app and see plain-text cause for onboarding incomplete.
- [ ] MVP: User can launch desktop app and see plain-text cause for missing auth.
- [ ] MVP: User can launch desktop app and see plain-text cause for server not running or unreachable.
- [ ] MVP: User can launch desktop app and see plain-text cause for missing GUI/server config file.
- [ ] MVP: User can launch desktop app and see plain-text cause for unreadable or invalid config.
- [ ] MVP: User can launch desktop app and see plain-text cause for server readiness failure.
- [ ] MVP: User can launch desktop app and see plain-text cause for protocol/API version mismatch.
- [ ] MVP: User can launch desktop app and see plain-text cause for backend capability mismatch.
- [ ] MVP: User can launch desktop app and see plain-text cause for unknown startup/setup failure.
- [ ] MVP: User can connect using Builder config/default server host and port.
- [x] MVP decision: Endpoint editing is deferred from MVP.
- [ ] MVP: User can see health/readiness/version/capability state for the connected server.
- [ ] MVP: User can see auth/config/migration/version failures as actionable errors.
- [ ] MVP: User can use disabled states when a backend capability is unavailable.
- [x] MVP decision: Desktop does not try to start Builder server.
- [x] MVP decision: If server is not running, app shows instructions to run `builder service install`.
- [x] MVP decision: Global reusable notification/status surface is MVP infrastructure.
- [x] MVP decision: Global notification/status surface is used for connection issues, warnings, update notifications, and long-lived cross-page messages.
- [x] MVP decision: Global notification/status surface may use a library/generic component; exact strip/snackbar/banner visual treatment is not locked yet.
- [x] Out of scope: Desktop does not start or bundle a Builder server sidecar; first workflow MVP is attach-only.

### Backend/API Readiness

- [ ] MVP: Server exposes setup/connectivity readiness with typed causes for startup safety UI.
- [ ] MVP: Server exposes protocol/API version and capability registry.
- [ ] MVP: Server exposes paginated project list/open/create read model/action needed by Home.
- [ ] MVP: Server exposes workspace path resolution/binding plan for New Project and Add Workspace flows.
- [ ] MVP: Server exposes workspace list for each project so task creation can choose main workspace.
- [ ] MVP: Server exposes or derives a project default/main workspace so task creation can avoid burdening the user when no workspace context is selected.
- [ ] MVP: Backend stores selected task workspace before start; current task schema is project-scoped and only links to workspace through `managed_worktree_id` after task worktree creation.
- [ ] MVP: Server exposes global paginated attention inbox for approvals, questions, interrupted tasks, and similar operator-attention items, or equivalent merged read model for Home.
- [ ] MVP: Server exposes project-wide workflow board read model for selected project/workflow, including tasks from all project workspaces.
- [ ] MVP: Server exposes task create action.
- [ ] MVP: Server exposes task create/edit input for main workspace selection before task starts.
- [ ] MVP: Server exposes drag/drop task movement/start action for backlog-to-first-active-node start.
- [ ] MVP: Server exposes task interrupt action for active/running task sessions.
- [x] MVP: Server exposes task detail read model with worktree path, agent role, agent status, session ID/name, current node/status, and completion state.
- [x] MVP: Server exposes task/session attach identifiers needed by local terminal teleport.
- [x] MVP: Server exposes paginated unified task activity feed endpoint for comments and changes.
- [x] MVP: Task activity feed includes comments and transitions at minimum.
- [x] MVP: Task activity feed may include other useful entries only when already stored and ready to query; do not invent new tables or data types only for the feed.
- [ ] MVP: Server exposes enough task state changes for visual board updates.
- [x] MVP decision: Board data refresh uses initial snapshot plus WebSocket updates.
- [x] MVP decision: Reconnect triggers full board refresh.
- [x] Current backend finding: project creation API supports display name and workspace root; project-key editing exists in metadata/store but is not currently exposed on project create API.
- [ ] MVP: Backend project administration API exposes project-key create/edit support for project creation, with collision validation and existing immutability rules.

### Project Entry

- [x] MVP decision: Home header shows main setup/runtime info such as server host/port, Builder version, logo, and auth mode.
- [x] MVP decision: Home left subpane shows paginated project list.
- [x] MVP decision: Home project list sorts by latest activity descending.
- [x] MVP decision: Home project list rows show project key/name, primary workspace path/status, updated time, and attention/task count chips when backend provides them.
- [x] MVP decision: Clicking a Home project row opens that project's default workflow board.
- [x] MVP decision: Home right subpane shows global merged paginated attention inbox for approvals, questions, interrupted tasks, and similar items.
- [x] MVP decision: Home attention inbox sorts newest activity first across all attention items.
- [x] MVP decision: Home attention inbox rows show task short ID, task title, project key/name, workflow, attention type, latest activity time, and small status/action hint.
- [x] MVP decision: Clicking a Home attention inbox row opens standalone task detail route over Home without loading the board.
- [x] MVP decision: User uses dedicated New Project and Add Workspace buttons; app is global, not tied to startup cwd.
- [x] MVP decision: If selected workspace already belongs to a project, app opens that project/workspace.
- [x] MVP decision: If selected workspace is unbound, app opens project creation page with editable project name and explicit confirmation.
- [x] MVP decision: Project creation default name comes from selected directory basename.
- [x] MVP decision: Project creation form includes editable project name and project key before confirm.
- [ ] MVP: Backend supports editing project key during project creation and validates collisions/immutability.
- [x] MVP decision: Additional configurable project DB fields are per-field decisions, not a blanket "expose everything" rule.
- [ ] MVP: GUI makes configurable project DB fields available where each field has clear operator value and backend safely supports it.
- [x] MVP decision: MVP accounts for multiple workspaces per project.
- [x] MVP decision: Project has a default/main workspace; additional workspaces are complementary/optional context by default.
- [ ] MVP: User can list/open projects.
- [ ] MVP: User can create a project by selecting a main workspace directory with an OS-native directory picker.
- [ ] MVP: User can open the selected project/workspace context returned by the backend.
- [ ] MVP: User can see project key and task short-ID prefix.
- [ ] MVP: User can see missing/invalid project-key blockers before creating tasks.
- [ ] MVP: User can see linked workflows and project default workflow.
- [ ] MVP: User can see when default workflow is invalid and task creation/start is blocked.
- [x] MVP decision: If a project has no valid linked/default workflow, board shows blocker/empty state, disables New Task, and points user to CLI/agent/API workflow setup for MVP.
- [ ] Later: User can create/edit project key where backend allows it.
- [ ] Later: User can resolve project-key backfill/collision issues for existing projects if backend needs user input.
- [ ] Later: User can link/unlink workflows and choose default workflow from GUI.
- [ ] Later: User can understand workflows are globally reusable and linked to projects, not copied per project.
- [ ] Later: User can see all projects that link a workflow before editing behavior-affecting graph pieces.
- [ ] Later: User can keep an invalid workflow linked/defaulted as a draft while seeing that task creation/start will reject it.
- [ ] Later: User can see unlink/delete/archive blockers when non-terminal tasks or task history reference a workflow/link/node/edge.

### Workflow Board

- [ ] MVP: User can view Kanban board with tasks grouped by workflow node/status.
- [x] MVP decision: Board shows one selected workflow at a time.
- [x] MVP decision: Board scope is project-wide for selected project and selected workflow, not limited to one selected workspace.
- [x] MVP decision: Board shows tasks from all project workspaces; workspace is card/context metadata instead of primary board scope.
- [x] MVP decision: Workflow picker lives in menu component.
- [x] MVP decision: Workflow picker orders project default workflow first, then most-recently-used workflows, then display name.
- [x] MVP decision: Board columns follow workflow-defined node order.
- [x] MVP decision: Backlog is fixed on the left.
- [x] MVP decision: Done is fixed on the right.
- [x] MVP decision: Workflow groups are rendered in MVP with group-aware board UI; grouped workflows are not flattened, blocked, or ignored.
- [x] MVP decision: Group board UX is implementation-led for MVP: build the best first pass, then Nikita will QA and complain if it is bad.
- [x] MVP decision: Initial preferred group-board shape is group islands wrapping related columns inside the horizontal board, unless implementation evidence shows a better approach.
- [ ] MVP: User can see node identity as visible status/column identity.
- [ ] MVP: User can see assignee/subagent role represented per column, not as independent per-task ownership.
- [x] MVP decision: Task cards show backend-native agent/task status verbatim instead of aggregating into a compact UI vocabulary.
- [x] MVP decision: Board card fields are limited to task ID, task name, and rich status component for MVP.
- [x] MVP decision: Rich status component uses spinner for in-progress states and icons/colors for other states.
- [ ] MVP: User can see task ID on task cards.
- [ ] MVP: User can see task name on task cards.
- [x] MVP decision: Task cards show workspace chip/label when project has multiple workspaces.
- [ ] MVP: User can distinguish backlog/not started, runnable, running, waiting for question, waiting for approval, interrupted, canceled, and done.
- [ ] MVP: User can see terminal-node placement as done state.
- [ ] MVP: User can view completed tasks.
- [x] MVP decision: Done is a normal paginated node column and drop target.
- [x] MVP decision: Done uses the same per-node infinite-scroll card pagination as every other column.
- [x] MVP decision: User can drag tasks to Done manually when backend permits manual transition.
- [ ] MVP: User can see active placement(s) for a task.
- [ ] MVP: User can see that ordinary serial execution has one active placement and fan-out can create multiple active placements.
- [ ] MVP: User can see subagent role as assignee for executable nodes.
- [ ] MVP: User can open task detail from board.
- [ ] MVP: User can open task detail dialog by clicking a board card.
- [x] MVP decision: Task card shows Resume button only when task is resumable.
- [x] MVP decision: Task card shows Interrupt button in the same action slot when task session is active/running.
- [x] MVP decision: Task card Interrupt acts immediately with no confirmation.
- [x] MVP decision: Task card Interrupt is available only when exactly one active run is interruptible; tasks with multiple active runs open task detail for per-run inline Interrupt controls.
- [ ] MVP: User can refresh/reconnect without board implementation reading full transcripts or `events.jsonl`.
- [x] MVP decision: Advanced board filters/sorting are deferred from MVP.
- [ ] Idea: User can use lightweight board search after core flow if cheap.
- [ ] Later: User can filter/sort/group boards by workflow, state, assignee/subagent role, blockers, and update time.

### Task Creation

- [x] MVP decision: Task creation form has required title and optional body/details.
- [x] MVP decision: Task body/details input is plain multiline text with shared Markdown rendering; no WYSIWYG editor in MVP.
- [x] MVP decision: Markdown rendering uses shared `MarkdownText` built on `react-markdown`, `remark-gfm`, and `rehype-sanitize`.
- [x] MVP decision: Markdown does not render raw HTML; use `skipHtml` or equivalent, do not add `rehype-raw`, and sanitize output anyway.
- [x] MVP decision: Markdown links use a safe-protocol allowlist, open external links through native bridge helper, and add `rel="noreferrer"`.
- [x] MVP decision: Markdown code/pre blocks use theme-token styling; syntax highlighting is deferred.
- [x] MVP decision: Feature components do not import `react-markdown` directly.
- [x] MVP decision: Source URL/import metadata is hidden in MVP UI and remains backend-only/future-facing.
- [x] MVP decision: Workflow picker lives outside the basic task form, in hover-expandable board controls.
- [x] MVP decision: Task creation form includes main workspace dropdown.
- [x] MVP decision: Task creation defaults to current/opened workspace context when present; otherwise it defaults to project default/main workspace.
- [x] MVP decision: When project has exactly one workspace, task form shows compact disabled workspace selector/chip so user sees context without extra choice.
- [x] MVP decision: Task main workspace can be edited before task starts and becomes immutable after start.
- [x] MVP decision: New Task plus button is only visible inside a workflow/Kanban view, so task creation always has a selected workflow context.
- [x] MVP decision: New tasks use the currently selected board workflow.
- [x] MVP decision: Project default workflow selects the initial workflow/Kanban view when opening a project, not a separate default inside the task form.
- [x] MVP decision: Task creation creates a backlog task by default.
- [x] MVP decision: No Start button in MVP.
- [x] MVP decision: User starts automation by dragging backlog task to the first active workflow node.
- [x] MVP decision: Dragging a backlog task to the first active node starts automation immediately without a default confirmation.
- [ ] MVP: User can choose linked workflow or project default workflow.
- [ ] MVP: User can create task without editing workflow definition.
- [ ] MVP: User can see that automation starts from the start node's single outgoing transition and then runs until terminal, question, approval/manual gate, error, capacity, interruption, or cancellation.
- [ ] MVP: User can see project-scoped short ID such as `BLD-123`.
- [ ] MVP: User can see task validation errors accumulated by server.
- [x] MVP decision: Task cancellation uses a cancel confirmation only; MVP does not ask for a cancellation reason.
- [ ] MVP: User can cancel task from task detail.
- [ ] MVP: User can see canceled tasks no longer schedule automation.
- [ ] MVP: User can see cancellation as task-level metadata, not as a synthetic terminal workflow status.
- [ ] Later: User can edit task metadata if backend adds edit operations.
- [ ] Later: User can import tasks from external sources using source URL/import metadata.

### Task Detail Dialog

- [ ] MVP: User can see task title, body, workflow, short ID, current node(s), worktree, cancellation state, and timestamps.
- [ ] MVP: User can see task detail in a dialog opened from the board.
- [ ] MVP: User can see worktree path in task detail.
- [ ] MVP: User can see agent role in task detail.
- [ ] MVP: User can see agent status in task detail.
- [ ] MVP: User can see session ID/name in task detail.
- [x] MVP decision: User can edit title/body only while task is still in Backlog.
- [x] MVP decision: Source URL is hidden from task detail in MVP.
- [x] MVP decision: Task detail hides expected-not-yet-created fields when they are not useful, such as worktree before start.
- [x] MVP decision: Task detail shows UX-continuity fields like assignee even when empty/unassigned.
- [x] MVP decision: Unexpected missing meaningful task fields render as unavailable/error states.
- [x] MVP decision: Task detail supports full task comment create/edit/delete.
- [x] MVP decision: Task comments use plain multiline text with shared Markdown rendering; no WYSIWYG editor in MVP.
- [x] MVP decision: Task detail has Cancel and Resume buttons.
- [x] MVP decision: Task detail shows Interrupt where Resume would appear when task session is active/running.
- [x] MVP decision: Task detail Interrupt acts immediately with no confirmation.
- [x] MVP decision: Task detail exposes per-run inline Interrupt controls when a task has multiple active runs/fan-out branches.
- [x] MVP decision: Successful interrupt follows TUI semantics: task becomes interrupted/resumable and activity feed records `Interrupted by user`.
- [x] MVP decision: Task detail has no Start button.
- [x] MVP decision: Resume opens contextual modal flow when resumption requires answering questions or approving transitions.
- [x] MVP decision: Task detail provides in-context resume UX in the activity feed/timeline near the item that caused the pause.
- [x] MVP decision: Approval resume flow lets user approve transition/action from task detail.
- [x] MVP decision: Approval resume flow exposes Approve only; Reject is not shown until negative semantics are product-defined.
- [x] MVP decision: Approval UI shows stored approval snapshot: source node, transition label/id, target node(s), required output fields/values, commentary, graph revision, and stale warning if relevant.
- [x] MVP decision: Question resume flow uses a question-answer component inspired by current TUI question picker.
- [x] MVP decision: Question resume flow supports suggestion options, freeform/commentary input, recommended marker when present, and no source-origin label.
- [x] MVP decision: Question UI uses ordinary controls: options plus blank commentary/freeform field below, click or arrows plus Enter to submit, standard Tab focus navigation.
- [x] MVP decision: Interrupted task resume flow can be a simple Resume button.
- [x] MVP decision: If a task has multiple unresolved attention items, task detail shows all unresolved feed items with their own inline action controls; top detail action opens the next/highest-priority unresolved item.
- [x] MVP decision: Task detail dialog is fixed-height and remembers last user-resized size.
- [x] MVP decision: Task detail dialog content is globally scrollable.
- [x] MVP decision: Task detail keeps top task identity/content items such as title, description, and chips fixed.
- [x] MVP decision: Task detail has paginated unified activity feed for comments and changes.
- [x] MVP decision: Task detail activity feed order is newest-to-oldest, with pagination for older entries.
- [x] MVP decision: Comment composer sits under fixed task identity/header and above the newest-first activity feed.
- [x] MVP decision: Standalone task detail opened from Home attention stays open after attention resolution; feed/status update and Home inbox row is removed or resorted in background.
- [ ] MVP: User can see run history by node, subagent role, session, start/end/interruption times, and interruption reason.
- [ ] MVP: User can see transition history with actor, selected `transition_id`, commentary, output fields, approval state, target nodes, and graph revision.
- [x] MVP decision: User can add, replace, soft-delete, and list task comments when backend permits.
- [x] MVP decision: User can see comment author/source agent where available.
- [x] MVP decision: User can understand task comments live in Builder persistence, not files in the worktree.
- [x] MVP decision: User can understand comments are not automatically injected into agent context.
- [ ] MVP: User can navigate from task run to session/transcript detail where available.
- [ ] Later: User can inspect raw transition-edge snapshots for deep debugging.
- [ ] Later: User can see durable transition logs for audit/debugging without needing event-sourced replay.

### Task Session Teleport

- [ ] MVP: User can teleport from task detail to task session chat in terminal.
- [x] MVP decision: Teleport to Builder TUI is a temporary placeholder shim, not strategic infrastructure.
- [x] MVP decision: Built-in GUI chat UI is planned after MVP and replaces teleport escape hatch.
- [x] MVP decision: GUI opens user's default terminal on the client machine and runs local Builder TUI attach flow.
- [x] MVP decision: GUI does not use backend-generated launch artifacts or signed/opaque launch tokens for MVP teleport.
- [x] MVP decision: If local Builder executable is unavailable, teleport fails with plain text error; no resolution flow required.
- [ ] MVP: User can open task session chat in terminal through OS-native open/process command instead of built-in GUI chat.
- [ ] MVP: User can see teleport failure as plain-text problem cause.
- [x] MVP decision: Exact local Builder TUI attach command is `builder --continue <session-id>`.

### Connection Loss

- [x] MVP decision: When connection is lost, no mutating action is available.
- [x] MVP decision: App may show last cached state while disconnected.
- [x] MVP decision: App shows an indefinite global notification/status message until reconnect.
- [x] MVP decision: Exact connection-loss notification treatment can be library/generic component; global reusable infrastructure is mandatory.
- [x] MVP decision: On reconnect, app triggers full refresh.
- [x] MVP decision: During disconnect, active form/comment drafts stay visible and submit/mutating buttons are disabled.
- [x] MVP decision: After reconnect refresh, preserved user drafts win on save and overwrite remote state regardless of whether remote state is newer or older.

### Workflow Definition Management

- [ ] Later: User can create workflow definitions.
- [ ] Later: User can list, inspect, rename, describe, archive, and restore workflows.
- [ ] Later: User gets default start/backlog and done nodes on workflow creation.
- [ ] Later: User can save/link/default invalid draft workflows when storage invariants pass.
- [ ] Later: User can see storage-invariant errors that always block saving: invalid IDs, duplicate keys, invalid enum values, invalid references, and not exactly one start node.
- [ ] Later: User can see graph revision and warnings that edits may affect active tasks.
- [ ] Later: User can see what task/run/transition graph revision observed when debugging behavior after edits.
- [ ] Later: User can see blocked destructive edits and choose archive/hide where physical delete is unsafe.
- [ ] Agent/CLI for MVP: Workflow authoring can be done by Builder agents through CLI/API before GUI authoring exists.

### Workflow Graph Authoring

- [ ] Later: User can add/edit/archive start, agent, join, and terminal nodes.
- [ ] Later: User can edit node key, display name, sort/order, prompt template, subagent role, and output fields.
- [ ] Later: User can define flat string output fields with stable names, descriptions, and labels.
- [ ] Later: User can see field-size validation for output field names, descriptions, output values, commentary, and task comment bodies.
- [ ] Later: User can add transition groups and edges.
- [ ] Later: User can configure transition ID/display and edge key/target/order.
- [ ] Later: User can configure edge context mode: `new_session`, `continue_session`, `compact_and_continue_session`.
- [ ] Later: User can configure edge approval requirement.
- [ ] Later: User can configure edge input bindings from task fields, transition output, or join aggregate output.
- [ ] Later: User can see exact allowed input binding sources and placeholder names before saving prompts.
- [ ] Later: User can see placeholder validation errors for unknown inputs, invalid binding target names, unsupported expressions, and references to unknown source fields.
- [ ] Later: User can configure edge output requirements.
- [ ] Later: User can validate topology constraints: exactly one start node, start outgoing shape, terminal sink, reachability, terminal reachability, valid cycles, fan-out join shape, no invalid detached islands.
- [ ] Later: User can see node-kind constraints: start/join/terminal are non-agent nodes, terminal nodes are strict sinks, and terminal auto-run is invalid.
- [ ] Later: User can see start-node constraints: exactly one start node, no inputs, non-executable, and task start must have one unambiguous agent target.
- [ ] Later: User can validate role/config constraints, including missing subagent roles and invalid cross-role `continue_session`.
- [ ] Later: User can see that agent node assignee equals the selected subagent role and that per-node model/provider/tool/auth overrides are not supported.

### Workflow Pattern Support

- [ ] Later: User can model prompt chains as serial agent nodes.
- [ ] Later: User can model routing through transition groups selected by `transition_id`.
- [ ] Later: User can model evaluator-optimizer loops with review nodes and return edges.
- [ ] Later: User can model orchestrator-worker flows without dynamic node creation.
- [ ] Later: User can model parallelization through fan-out transition groups and joins.
- [ ] Later: User can model autonomous agent nodes with workflow completion authority.

### Worktree And Session Context

- [ ] MVP: User can see managed task worktree and branch after task start.
- [ ] MVP: User can see that one task owns/reuses one managed worktree by default.
- [ ] MVP: User can see that branch name defaults to task short ID and when collision handling chose a different safe physical name.
- [ ] MVP: User can see task worktree creation failure as a start blocker.
- [ ] MVP: User can see session(s) created under a task instead of treating session as task identity.
- [ ] Later: User can see worktree deletion/retarget blockers caused by non-terminal tasks.
- [ ] Later: User can inspect context-preservation mode used for each transition/run.
- [ ] Later: User can see when direct continuation is blocked because source/target roles differ.

### Live Runs And Completion Output

- [ ] MVP: User can see queued/runnable/running/waiting/interrupted/completed run state.
- [ ] MVP: User can watch high-level run progress/events through server read models.
- [ ] MVP: User can see current node and subagent role for active run.
- [ ] MVP: User can see workflow completion output: `transition_id`, commentary, and custom output fields.
- [ ] MVP: User can see selected transition target(s) derived from transition group/edges.
- [ ] MVP: User can see workflow protocol errors such as normal final answer, invalid completion payload, missing output requirements, and protocol-cap interruption.
- [ ] MVP: User can see runtime failure, user cancellation, unanswered question, and scheduling validation blockers as orchestration outcomes, not model-selected statuses.
- [ ] MVP: User can see when a run is interrupted and requires explicit human resume instead of automatic retry.
- [ ] Later: User can inspect completion mode config/effective mode: `auto`, `structured_output`, or `tool`.
- [ ] Later: User can inspect structured-output vs `complete_node` tool completion details for debugging.
- [ ] Later: User can see provider capability errors when forced structured-output mode is unsupported.
- [ ] Later: User can inspect final-answer violation and invalid-completion counters for a run.

### Questions

- [x] MVP decision: Home attention inbox can list pending `ask_question` items and deep-link to task/detail.
- [x] MVP decision: Global question inbox rows are list/deep-link only in MVP.
- [x] MVP decision: User answers questions through contextual Resume modal when task resumption requires an answer.
- [ ] MVP: User can see task/run/node context for each question.
- [ ] MVP: User can see whether answer resumed same run/session.
- [ ] MVP: User can see actionable interrupted state if pending ask cannot rehydrate after restart.

### Approvals

- [x] MVP decision: Home attention inbox can list pending transition approvals and deep-link to task/detail.
- [x] MVP decision: Global approval inbox rows are list/deep-link only in MVP.
- [x] MVP decision: User approves transitions through contextual Resume modal when task resumption requires approval.
- [ ] MVP: User can inspect source task, source node, selected transition, commentary, output fields, target node(s), context mode, and edge snapshots.
- [ ] MVP: User can approve transition by durable transition identity.
- [ ] MVP: User can see mixed-edge approval groups wait as a whole group.
- [ ] MVP: User can see graph edits after approval request do not change pending approval meaning.
- [ ] MVP: User can see approval applies to a specific completed run/transition snapshot, not the live graph.
- [ ] Later: User can reject/hold approvals if backend exposes rejected/hold operations.

### Manual Moves And Rework

- [ ] Later: User can manually move a task placement using a real edge/transition or equivalent edge metadata.
- [ ] Later: User can provide required output values for forward manual moves.
- [ ] Later: User can reuse stored output values for backward moves when valid.
- [ ] Later: User can see manual move blockers: missing required output, invalid edge, invalid workflow, continuation required but no valid source session.
- [ ] Later: User can move into executable node and keep automation paused until explicit start approval.
- [ ] Later: User can reopen/rework terminal tasks as user override when backend exposes the operation.

### Parallel Branches And Joins

- [ ] MVP: User can understand one task may have multiple active placements when backend fan-out exists.
- [ ] Later: User can see one task with multiple concurrent branch placements.
- [ ] Later: User can see per-branch run state and branch identity.
- [ ] Later: User can see join waiting state: completed branches vs required branches.
- [ ] Later: User can see expected join inputs derived from persisted transition-edge snapshots, not the edited live graph.
- [ ] Later: User can inspect deterministic joined outputs.
- [ ] Later: User can see branches complete in any order while join waits for every required branch.
- [ ] Later: User can see invalid fan-out topology errors during workflow authoring.
- [ ] Later: User can author fan-out transition groups and join nodes.

### Scheduler, Recovery, And Runtime Control

- [ ] MVP: User can see runnable vs blocked work.
- [ ] MVP: User can see interruption reason: runtime error, shutdown recovery, protocol cap, validation blocker, user cancellation, missing role/config, or unresolved ask.
- [x] MVP decision: User can resume interrupted work from task card when resumable and from task detail.
- [x] MVP decision: User can cancel task/run from task detail through task-level cancellation semantics.
- [ ] MVP: User can see tasks are not auto-retried after interruption.
- [ ] MVP: User can see unrecoverable/corrupted orchestration state distinctly from recoverable interruption if backend reports `failed`.
- [ ] MVP: User can see startup recovery outcomes after server restart: runnable rebuilt, pending approvals preserved, orphaned active runs interrupted, unresolved asks interrupted.
- [ ] Later: User can see global workflow concurrency setting and effective capacity.
- [ ] Later: User can see scheduler diagnostics around runnable selection, active runtime ownership, recovery, and validation blockers.
- [ ] Later: User can edit `[workflow]` config values when GUI settings supports server config mutation.
- [ ] Later: User can view/edit workflow runtime config: `completion_mode`, `concurrency`, `max_final_answer_violations`, and `max_invalid_completion_attempts`.
- [ ] Later: User can see invalid workflow config values as server startup/worker-start blockers.

### Validation And Config Drift

- [ ] MVP: User can see validation errors with stable codes and actionable messages.
- [ ] MVP: User can distinguish save-time warnings from task-creation/start/scheduling blockers.
- [ ] MVP: User can see missing subagent role blockers.
- [ ] MVP: User can see stale graph revision warnings on active tasks.
- [ ] MVP: User can see invalid workflow/default workflow blockers before task start.
- [ ] MVP: User can see same-name subagent config drift behavior: fresh sessions use current config while existing continued sessions keep persisted setup.
- [ ] Later: User can run draft/task-creation/execution validation modes explicitly from workflow editor.
- [ ] Later: User can inspect graph/project-context validation separately.

### CLI And Agent-Control Surfaces

- [ ] Non-GUI MVP: Builder agents can create/edit workflows through CLI/API while GUI authoring is deferred.
- [ ] Non-GUI MVP: CLI remains internal backend-testing and agent-control surface, not primary manual QA surface.
- [ ] Later: GUI can show equivalent command/API diagnostics if useful for power users.

## PRD Decisions To Resolve

- [x] Decide whether task comments remain MVP or move later: full comment create/edit/delete is MVP.
- [x] Decide whether workflow question inbox/answering remains MVP or moves later: Home inbox list/deep-link is MVP; answer flow is contextual Resume modal only.
- [x] Decide whether workflow transition approval queue/action remains MVP or moves later: Home inbox list/deep-link is MVP; approval flow is contextual Resume modal only.
- [x] Decide whether explicit task start/cancel/resume actions are MVP or only later task management: no Start button; Cancel and Resume are MVP.
- [x] Decide active-session card/detail action: show Interrupt where Resume would be; Resume appears only when paused/resumable.
- [x] Decide Interrupt confirmation: Interrupt acts immediately with no confirmation.
- [x] Decide interrupt success outcome: same as TUI, task becomes interrupted/resumable and feed records `Interrupted by user`.
- [x] Decide whether MVP uses WebSocket JSON-RPC, HTTP polling endpoints, or both: initial snapshot plus WebSocket updates; full refresh on reconnect.
- [x] Decide whether task-session teleport identifies target by session ID, task ID, or backend-provided identifiers: backend returns task/session attach identifiers; GUI/native bridge opens the user's default terminal and runs `builder --continue <session-id>`.
- [x] Decide whether MVP accessibility/keyboard-complete pass is required: no; i18n-ready strings are mandatory.
- [x] Decide task detail missing-field rendering: hide expected-not-yet-created fields when not meaningful, show UX-continuity fields such as assignee as unassigned/empty, and render unexpected missing meaningful fields as unavailable/error states.
- [x] Decide task detail history shape: fixed-height resizable dialog with fixed top task identity/content and paginated unified activity feed for comments and changes.
- [x] Decide hover-expandable popup contents: Home/back, Inbox, New Task, Pin, and workflow list; unpinned auto-collapses on unhover; pinned becomes floating-island-sidebar-like.
- [x] Decide global status messaging direction: reusable global notification/status surface is mandatory; exact strip/snackbar/banner treatment is not locked.
- [x] Decide how teleport launch artifacts are generated, opened, secured, and cleaned up: no launch artifacts in MVP.
- [x] Decide whether mockups are required before implementation: no; iterate directly in code.
- [x] Decide manual QA shape: Nikita will QA manually during implementation; no separate QA script now.
- [x] Decide board workflow scope: one selected workflow at a time, selected through menu component.
- [x] Decide board card fields: task ID, task name, and rich status component only for MVP.
- [x] Decide Builder TUI teleport strategy: temporary placeholder shim only; built-in GUI chat follows after MVP.
- [x] Decide board workspace scope after backend task/workspace model update: project-wide board for selected project/workflow; cards show workspace context when needed, and additional workspaces are complementary/optional by default.
- [x] Decide task main workspace default: current/opened workspace context if present, otherwise project default/main workspace.
- [x] Decide project-key API scope for MVP: add project-key create/edit support to project administration API/UI with collision validation and existing immutability rules.
- [x] Decide workflow group MVP rendering: build the best implementation-led MVP first pass; initial preferred shape is group islands wrapping related columns, with Nikita QA driving changes if it feels bad.
- [x] Decide exact backend slice order: connectivity/capabilities; Home/project admin/key/workspaces; workflow picker plus selected board/groups/live updates; task create/backlog/workspace default/drag-to-start; interrupt/cancel/resume/inbox/questions/approvals; detail feed/comments/teleport.
- [x] Decide Home attention inbox ordering: newest activity first across all attention items.
- [x] Decide Home attention inbox row content: task short ID, title, project key/name, workflow, attention type, latest activity time, and small status/action hint.
- [x] Decide Home project list rows: latest-activity descending rows with project key/name, primary workspace path/status, updated time, and attention/task count chips when backend provides them.
- [x] Decide Home project row click: opens that project's default workflow board.
- [x] Decide Home attention row click: opens standalone task detail route over Home without loading the board.
- [x] Decide task detail activity feed ordering: newest-to-oldest with pagination for older entries.
- [x] Decide comment composer location: under fixed task identity/header and above the newest-first activity feed.
- [x] Decide task creation workflow default: New Task is only available inside a workflow/Kanban view and uses the currently selected board workflow.
- [x] Decide task cancellation reason requirement: MVP cancel uses confirmation only and does not ask for a reason.
- [x] Decide drag-to-start confirmation: no default confirmation; dropping a backlog task onto the first active node starts automation immediately.
- [x] Decide task body/comment format: plain multiline input with shared Markdown rendering, no WYSIWYG editor.
- [x] Decide Markdown renderer: shared `MarkdownText` built on `react-markdown`, `remark-gfm`, and `rehype-sanitize`, with raw HTML disabled and sanitized output.
- [x] Decide project-create advanced fields policy: no blanket exposure; decide additional DB/config fields per field based on operator value and backend safety.
- [x] Decide desktop server endpoint selection: use Builder config/default host and port only for MVP; endpoint editing is deferred.
- [x] Decide no-valid-workflow project state: board blocker/empty state, New Task disabled, and pointer to CLI/agent/API workflow setup.
- [x] Decide workflow picker ordering: project default first, then most-recently-used, then display name.
- [x] Decide single-workspace task form display: compact disabled workspace selector/chip.
- [x] Decide disconnect draft policy: keep local draft visible, disable submit while disconnected, refresh on reconnect, and let preserved user draft overwrite remote state on save.
- [x] Decide approval negative action: expose Approve only in MVP; use Interrupt/Cancel for negative path until Reject semantics are product-defined.
- [x] Decide approval content: stored approval snapshot with source node, transition label/id, target node(s), output fields/values, commentary, graph revision, and stale warning when relevant.
- [x] Decide question answer UI: preserve full ask functionality, but use normal controls with options plus blank commentary/freeform field, click or arrows plus Enter, standard Tab focus, recommended marker, and no source-origin label.
- [x] Decide multiple unresolved attention item UI: all unresolved feed items get inline action controls; top action opens next/highest-priority item.
- [x] Decide post-resolution standalone detail behavior: stay in detail, update feed/status, remove or resort Home inbox row in background.
