# GUI Workflow MVP PRD Checklist

Status: draft checklist for a later PRD/planning session.

Date: 2026-05-16.

This is not the PRD. It is a working checklist for defining the first workflow GUI MVP. It records current product intent, open design choices, and implementation questions to resolve before writing the real PRD.

Goal: user can run real work through a workflow from the desktop app.

Related docs:

- `docs/dev/gui-workflow-use-cases.md` for full async-workflow GUI inventory.
- `docs/dev/gui-cli-parity-checklist.md` for eventual CLI/TUI replacement scope.
- `docs/dev/gui-foundation-checklist.md` for app foundation and shell setup.
- `.builder/skills/ui-design/SKILL.md` for locked GUI visual/design constraints.

## Ordering Rule

Keep MVP sections in dependency order: macOS/native surface, visual design system, navigation/lifecycle, startup/backend readiness, project entry, workflow board, task creation/detail/teleport, then open questions.

## MVP Outcome

- [ ] Define success as: user can open app, select/create project, view workflow Kanban board, add task, watch task move through workflow states, inspect completed tasks, and teleport into task session chat in terminal.
- [ ] Keep workflow graph editing outside MVP GUI; workflow edits can be performed through CLI/agent/API.
- [ ] Require real Builder server integration for task/workflow data before MVP is complete.
- [x] Backend workflow endpoints/read models come before real workflow UI integration; no workflow UI beyond shell/prototype until real read models exist.
- [x] Delivery uses vertical slices: backend plumbing/read models first, then GUI integration for that slice.
- [ ] Allow mock transport only for shell/prototype development, not for MVP acceptance.
- [ ] Target macOS only for first shipped MVP.
- [ ] Keep implementation portable where Tauri/React source boundaries allow it, but do not expand MVP QA or release scope beyond macOS.

## MVP Non-Goals

- [ ] No workflow graph authoring UI.
- [ ] No built-in chat UI.
- [ ] No full CLI/TUI feature replacement.
- [ ] No Windows/Linux release target.
- [ ] No full onboarding/resolution flows for setup errors.
- [ ] No rich scheduler/admin dashboard.
- [ ] No visual workflow editor.
- [ ] No generated docs/checklist drift enforcement.

## macOS Window And Native Surface

- [x] macOS window uses blurred glass/vibrancy background.
- [x] Blur/glass capability is mandatory for MVP.
- [x] Window uses native border, native shape, and native traffic-light/window controls integrated into app chrome.
- [x] Window background is system glass: macOS blurred glass for first MVP, Windows acrylic as analogous future material.
- [x] UI uses island-style floating surfaces with rounded corners over the glass background.
- [x] Tauri/native window APIs stay behind native bridge or shell boundary, not feature components.
- [ ] Window background, component surfaces, and text contrast remain readable over blur.
- [x] Blur/glass and motion are disabled or reduced in tests/snapshots so visual checks stay deterministic.

## Visual Design System

- [x] UI has locked Builder/TUI-adjacent brand direction for MVP.
- [x] UI style principles are clean, elegant, effective, island-based, and animation-first.
- [x] UI has palette tokens locked in code and designed for easy future palette replacement.
- [x] UI has font tokens locked in code: Montserrat for main UI and Monaspace Neon for monospace values.
- [x] UI supports dark and light themes, with config override when set and auto/system otherwise.
- [x] Initial color source should come from current TUI and docs-site palette rather than inventing a separate palette.
- [x] Shared UI/theme source of truth starts in `apps/shared/ui/theme`.
- [ ] UI has spacing/radius/shadow tokens locked in code.
- [ ] UI has main widgets locked in code: app shell, nav item, button, input, select, dialog, card, badge, toast, empty state, loading state, error state, Kanban column, Kanban card.
- [x] UI kit uses Lucide as icon pack.
- [ ] UI has consistent task status colors and labels.
- [ ] UI uses reusable components rather than one-off board-specific styling.
- [ ] Design system source is shared enough to avoid rewrite when later web/desktop surfaces grow.
- [ ] Every user-visible transition is animated unless reduced motion is enabled.
- [ ] Shared element transitions are supported where applicable, especially card-to-detail and project-to-board.
- [x] MVP does not require accessibility/keyboard-complete pass.
- [x] MVP uses `i18next`/`react-i18next` with static English locale files.
- [x] MVP has no runtime language switch and no non-English locales.
- [x] MVP requires no hardcoded user-facing strings in components so future locales can be added without source scavenging.

## Navigation, Dialogs, And Lifecycles

- [ ] App has navigation stack infrastructure.
- [ ] App has typed destinations for project picker, project board, task dialog, settings/diagnostics, and startup error surface.
- [ ] App has dialog infrastructure.
- [x] Project picker coincides with Home/inbox/main destination and app-startup destination.
- [x] Home/project list is a full-screen destination, not modal and not permanent sidebar-only UI.
- [x] Relaunch restores last project board when possible and still allows navigating back to Home/project list.
- [x] Task details start as dialogs; right-side pane remains an allowed variant to test later.
- [x] Board starts without persistent sidebar to preserve Kanban width.
- [x] Project switching starts through Home/back navigation, breadcrumbs, shortcuts, or lightweight top-level controls, not persistent project list sidebar.
- [x] A hover-expandable non-modal popup is a candidate place for main nav/actions and workflow picker, to define later.
- [x] Hover-expandable non-modal popup contains navigation destinations such as Home/back and Inbox.
- [x] Hover-expandable non-modal popup contains New Task action.
- [x] Hover-expandable non-modal popup contains Pin action; unpinned state auto-collapses on unhover.
- [x] Pinned expanded popup becomes a floating-island-sidebar-like widget.
- [x] Expanded popup lists workflows; pagination is deferred unless implementation proves necessary.
- [ ] Destinations own lifecycle hooks for view-models, subscriptions, background processes, and cleanup.
- [ ] Navigation supports deep links or restorable destinations where practical.
- [ ] Navigation handles server disconnect/reconnect without corrupting selected project/task state.
- [ ] Back/close behavior is deterministic for dialogs and nested board destinations.
- [x] Board top-level navigation/sidebar model preserves board space and avoids persistent project switching sidebar for initial MVP.

## Startup Safety And Setup Errors

- [ ] App can launch into a safe shell even when backend/setup is broken.
- [ ] App shows startup/loading state while local GUI config and server connectivity initialize.
- [ ] App shows plain-text problem cause for onboarding incomplete.
- [ ] App shows plain-text problem cause for missing auth.
- [ ] App shows plain-text problem cause for server not running or unreachable.
- [ ] App shows plain-text problem cause for missing GUI/server config file.
- [ ] App shows plain-text problem cause for unreadable or invalid config.
- [ ] App shows plain-text problem cause for server readiness failure.
- [ ] App shows plain-text problem cause for protocol/API version mismatch.
- [ ] App shows plain-text problem cause for backend capability mismatch.
- [ ] App shows plain-text problem cause for unknown startup failure.
- [ ] Resolution flows are not required in MVP; text explanation is enough.
- [x] Desktop does not try to start Builder server in MVP.
- [x] When server is not running, app shows instructions to run `builder service install`.
- [ ] Startup failures are visible in main shell, not only logs/toasts.
- [x] Global reusable notification/status surface is MVP infrastructure.
- [x] Global notification/status surface is used for connection issues, warnings, update notifications, and long-lived cross-page messages.
- [x] Global notification/status surface may use a library/generic component; exact strip/snackbar/banner visual treatment is not locked yet.
- [x] Desktop server endpoint selection uses Builder config/default host and port only in MVP.
- [x] If configured/default endpoint is unreachable, app shows startup error and `builder service install` instructions; endpoint editing is deferred.

## Backend/API Readiness

- [x] Backend slice/API plan is tracked in `docs/dev/gui-workflow-backend-slice-plan.md`.
- [ ] Server exposes setup/connectivity readiness with typed causes for startup safety UI.
- [ ] Server exposes protocol/API version and capability registry.
- [ ] Server exposes paginated project list/open/create read model/action needed by Home.
- [ ] Server exposes workspace path resolution/binding plan for New Project/Add Workspace flows.
- [ ] Server exposes workspace list for each project so task creation can choose main workspace.
- [ ] Server exposes or derives a project default/main workspace so task creation can avoid burdening the user when no workspace context is selected.
- [ ] Backend stores selected task workspace before start; current task schema is project-scoped and only links to workspace through `managed_worktree_id` after task worktree creation.
- [ ] Server exposes global paginated attention inbox for approvals, questions, and interrupted tasks, or equivalent merged read model for Home.
- [ ] Server exposes project-wide workflow board read model for selected project/workflow, including tasks from all project workspaces.
- [ ] Server exposes task create action.
- [ ] Server exposes task create/edit input for main workspace selection before task starts.
- [ ] Server exposes task detail read model with worktree path, agent role, agent status, session ID/name, current node/status, completion state.
- [ ] Server exposes drag/drop task movement/start action for backlog-to-first-active-node start.
- [ ] Server exposes task interrupt action for active/running task sessions.
- [ ] Server exposes task/session attach identifiers needed by local terminal teleport.
- [ ] Server exposes paginated unified task activity feed endpoint backed by existing persisted audit data only.
- [ ] Task activity feed includes comments and transitions at minimum.
- [ ] Task activity feed may include other useful entries only when already stored and ready to query; do not invent new tables or data types only for the feed.
- [ ] Server exposes enough task state changes for visual board updates.
- [x] Board data refresh uses initial snapshot plus WebSocket updates.
- [x] Reconnect triggers full board refresh.
- [x] Current backend project creation API supports display name and workspace root; project-key editing exists in metadata/store but is not currently exposed on project create API.
- [ ] Backend project administration API exposes project-key create/edit support for MVP project creation, with collision validation and existing immutability rules.

## Project Entry

- [x] App has Home as the project-first landing destination.
- [x] Home header shows main setup/runtime info such as server host/port, Builder version, logo, and auth mode.
- [x] Home left subpane shows paginated project list.
- [x] Home project list sorts by latest activity descending.
- [x] Home project list rows show project key/name, primary workspace path/status, updated time, and attention/task count chips when backend provides them.
- [x] Clicking a Home project row opens that project's default workflow board.
- [x] Home right subpane shows global merged paginated attention inbox for approvals, questions, interrupted tasks, and similar items.
- [x] Home attention inbox sorts newest activity first across all attention items.
- [x] Home attention inbox rows show task short ID, task title, project key/name, workflow, attention type, latest activity time, and small status/action hint.
- [x] Clicking a Home attention inbox row opens standalone task detail route over Home without loading the board.
- [x] User can pick existing/recent project.
- [x] User uses dedicated New Project and Add Workspace buttons; app is global, not tied to startup cwd.
- [x] If selected workspace already belongs to a project, app opens that project/workspace.
- [x] If selected workspace is unbound, app opens project creation page with editable project name and explicit confirmation.
- [x] Project creation default name comes from selected directory basename.
- [x] Project creation form includes editable project name and project key before confirm.
- [ ] Backend supports editing project key during project creation and validates collisions/immutability.
- [x] Additional configurable project DB fields are per-field MVP decisions, not a blanket "expose everything" rule.
- [ ] Prefer making configurable project DB fields available in GUI where each field has clear operator value and backend safely supports it.
- [x] MVP accounts for multiple workspaces per project.
- [x] Project has a default/main workspace; additional workspaces are treated as complementary/optional context by default.
- [ ] User can see selected project identity before creating tasks.
- [ ] User can see missing/invalid project setup blockers before task creation.
- [ ] User can navigate from selected project to workflow board.
- [x] If a project has no valid linked/default workflow, board shows blocker/empty state, disables New Task, and points user to CLI/agent/API workflow setup for MVP.

## Workflow Board

- [ ] User can view resulting workflow as Kanban board from existing workflow definition.
- [x] Board shows one selected workflow at a time.
- [x] Board scope is project-wide for selected project and selected workflow, not limited to one selected workspace.
- [x] Board shows tasks from all project workspaces; workspace is card/context metadata instead of primary board scope.
- [x] Workflow picker lives in menu component.
- [x] Workflow picker orders project default workflow first, then most-recently-used workflows, then display name.
- [ ] Board columns map to workflow nodes/statuses from server read model.
- [x] Board column order follows workflow-defined node order.
- [x] Backlog is fixed on the left.
- [x] Done is fixed on the right.
- [x] Workflow groups are rendered in MVP with group-aware board UI; grouped workflows are not flattened, blocked, or ignored.
- [x] Group board UX is implementation-led for MVP: build the best first pass, then Nikita will QA and complain if it is bad.
- [x] Initial preferred group-board shape is group islands wrapping related columns inside the horizontal board, unless implementation evidence shows a better approach.
- [ ] Assignee is represented per column, not per task.
- [ ] Board can show completed/done tasks.
- [ ] Board can show tasks moving visually through workflow states.
- [ ] Board can show queued tasks.
- [ ] Board can show running tasks.
- [ ] Board can show interrupted tasks.
- [ ] Board can show approval-gated tasks.
- [ ] Board can show idle/not-started tasks.
- [ ] Board can show done/completed tasks.
- [x] Done is an expandable drop target/dropbox, not a full always-expanded column.
- [x] Done shows a small recent-task preview by default, around 3-5 tasks.
- [x] Done expands to show more completed tasks when requested.
- [x] User can drag tasks to Done manually when backend permits manual transition.
- [x] Board card fields are limited to task ID, task name, and rich status component for MVP.
- [x] Rich status component uses spinner for in-progress states and icons/colors for other states.
- [ ] Board card shows task ID.
- [ ] Board card shows task name.
- [x] Board card shows workspace chip/label when project has multiple workspaces.
- [x] Board card shows backend-native agent/task status verbatim instead of aggregating into a compact UI vocabulary.
- [ ] Board card can be clicked to open task detail dialog.
- [x] Board card shows Resume button only when task is resumable.
- [x] Board card shows Interrupt button in the same action slot when task session is active/running.
- [x] Board card Interrupt acts immediately with no confirmation.
- [x] Board card Interrupt is available only when exactly one active run is interruptible; tasks with multiple active runs open task detail for per-run inline Interrupt controls.
- [x] Advanced board filters/sorting are deferred from MVP.
- [ ] Idea: add lightweight board search after core flow if cheap.

## Task Creation

- [ ] User can add task from GUI.
- [x] Task creation form has required title.
- [x] Task creation form has optional body/details.
- [x] Task body/details input is plain multiline text with shared Markdown rendering; no WYSIWYG editor in MVP.
- [x] Markdown rendering uses shared `MarkdownText` built on `react-markdown`, `remark-gfm`, and `rehype-sanitize`.
- [x] Markdown does not render raw HTML in MVP; use `skipHtml` or equivalent, do not add `rehype-raw`, and sanitize output anyway.
- [x] Markdown links use a safe-protocol allowlist, open external links through native bridge helper, and add `rel="noreferrer"`.
- [x] Markdown code/pre blocks use theme-token styling; syntax highlighting is deferred.
- [x] Feature components do not import `react-markdown` directly.
- [x] Source URL/import metadata is hidden in MVP UI and remains backend-only/future-facing.
- [x] Workflow picker lives outside the basic task form, in the hover-expandable non-modal popup/board controls.
- [x] Task creation form includes main workspace dropdown.
- [x] Task creation defaults to current/opened workspace context when present; otherwise it defaults to project default/main workspace.
- [x] When project has exactly one workspace, task form shows compact disabled workspace selector/chip so user sees context without extra choice.
- [x] Task main workspace can be edited before task starts and becomes immutable after start.
- [x] New Task plus button is only visible inside a workflow/Kanban view, so task creation always has a selected workflow context.
- [x] New tasks use the currently selected board workflow.
- [x] Project default workflow selects the initial workflow/Kanban view when opening a project, not a separate default inside the task form.
- [ ] User can choose workflow by switching the selected workflow/Kanban view.
- [ ] User can open project default workflow without extra selection.
- [ ] User can create task without editing workflow definition.
- [ ] User can see validation error text when task creation fails.
- [x] Task creation creates a backlog task by default.
- [x] No Start button in MVP.
- [x] User starts automation by dragging backlog task to the first active workflow node.
- [x] Dragging a backlog task to the first active node starts automation immediately without a default confirmation.

## Task Detail Dialog

- [ ] Task card click opens task detail in dialog.
- [ ] Dialog has destination lifecycle so view-model/process subscriptions attach on open and dispose on close.
- [ ] Dialog shows task ID.
- [ ] Dialog shows task name.
- [ ] Dialog shows worktree path.
- [ ] Dialog shows agent role.
- [ ] Dialog shows agent status.
- [ ] Dialog shows session ID/name.
- [ ] Dialog shows current workflow node/status.
- [ ] Dialog shows completion/done state for completed tasks.
- [ ] Dialog has button to teleport to terminal TUI session.
- [x] Dialog allows title/body edits only while task is still in Backlog.
- [x] Source URL is hidden from task detail in MVP.
- [x] Dialog hides expected-not-yet-created fields when they are not useful, such as worktree before start.
- [x] Dialog shows UX-continuity fields like assignee even when empty/unassigned.
- [x] Unexpected missing meaningful fields render as unavailable/error states.
- [x] Dialog supports full task comment create/edit/delete.
- [x] Task comments use plain multiline text with shared Markdown rendering; no WYSIWYG editor in MVP.
- [x] Dialog has Cancel and Resume buttons.
- [x] Dialog shows Interrupt where Resume would appear when task session is active/running.
- [x] Dialog Interrupt acts immediately with no confirmation.
- [x] Dialog exposes per-run inline Interrupt controls when a task has multiple active runs/fan-out branches.
- [x] Successful interrupt follows TUI semantics: task becomes interrupted/resumable and activity feed records `Interrupted by user`.
- [x] Task cancellation in MVP uses a cancel confirmation only; it does not ask for a cancellation reason.
- [x] Dialog has no Start button.
- [x] Resume opens contextual modal flow when resumption requires answering questions or approving transitions.
- [x] Task detail provides in-context resume UX in the activity feed/timeline near the item that caused the pause.
- [x] Approval resume flow lets user approve transition/action from task detail.
- [x] Approval resume flow exposes Approve only in MVP; Reject is not shown until negative semantics are product-defined.
- [x] Approval UI shows stored approval snapshot: source node, transition label/id, target node(s), required output fields/values, commentary, graph revision, and stale warning if relevant.
- [x] Question resume flow uses a question-answer component inspired by current TUI question picker.
- [x] Question resume flow supports suggestion options, freeform/commentary input, recommended marker when present, and no source-origin label.
- [x] Question UI uses ordinary controls: options plus blank commentary/freeform field below, click or arrows plus Enter to submit, standard Tab focus navigation.
- [x] Interrupted task resume flow can be a simple Resume button.
- [x] If a task has multiple unresolved attention items, task detail shows all unresolved feed items with their own inline action controls; top detail action opens the next/highest-priority unresolved item.
- [x] Dialog is fixed-height and remembers last user-resized size.
- [x] Dialog content is globally scrollable.
- [x] Dialog keeps top task identity/content items such as title, description, and chips fixed.
- [x] Dialog has paginated unified activity feed for comments and changes.
- [x] Dialog activity feed order is newest-to-oldest, with pagination for older entries.
- [x] Comment composer sits under fixed task identity/header and above the newest-first activity feed.
- [x] Standalone task detail opened from Home attention stays open after attention resolution; feed/status update and Home inbox row is removed or resorted in background.

## Task Session Teleport

- [ ] User can teleport from task detail to task session chat in terminal.
- [x] Teleport to Builder TUI is a temporary placeholder shim, not strategic infrastructure.
- [x] Built-in GUI chat UI is planned after MVP and replaces teleport escape hatch.
- [ ] Teleport uses OS-native open/process capability through native bridge.
- [x] GUI opens user's default terminal on the client machine and runs local Builder TUI attach flow.
- [x] GUI does not use backend-generated launch artifacts or signed/opaque launch tokens for MVP teleport.
- [x] If local Builder executable is unavailable, teleport fails with plain text error; no resolution flow required.
- [ ] Teleport failure shows plain-text problem cause.
- [ ] Decide exact local Builder TUI attach command/arguments once CLI surface is final.

## Connection Loss

- [x] When connection is lost, no mutating action is available.
- [x] App may show last cached state while disconnected.
- [x] App shows an indefinite global notification/status message until reconnect.
- [x] Exact connection-loss notification treatment can be library/generic component; global reusable infrastructure is mandatory.
- [x] On reconnect, app triggers full refresh.
- [x] During disconnect, active form/comment drafts stay visible and submit/mutating buttons are disabled.
- [x] After reconnect refresh, preserved user drafts win on save and overwrite remote state regardless of whether remote state is newer or older.

## PRD Questions To Resolve

- [x] What exact backend slices must be complete before real MVP integration starts? Lock order: connectivity/capabilities; Home/project admin/key/workspaces; workflow picker plus selected board/groups/live updates; task create/backlog/workspace default/drag-to-start; interrupt/cancel/resume/inbox/questions/approvals; detail feed/comments/teleport.
- [x] What is the minimal task creation form? Required title, optional body/details, source URL hidden/backend-only, workflow picker outside the basic form.
- [x] Does creating task also start automation by default? No. It creates backlog task; user drags to first active node to start.
- [x] What exact agent status vocabulary is canonical for card badges? Use backend-native status verbatim on cards, no compact UI aggregation.
- [x] What exact board column ordering is canonical? Workflow-defined node order, Backlog fixed left, Done fixed right.
- [x] Are completed tasks shown in same board, separate completed column, or separate completed view? Same board as expandable Done dropbox with 3-5 recent tasks by default.
- [x] What task detail fields are required vs optional if backend data is missing? Hide expected-not-yet-created fields when not meaningful, show UX-continuity fields such as assignee as unassigned/empty, and render unexpected missing meaningful fields as unavailable/error states.
- [x] What edit actions, if any, belong in MVP task dialog? Title/body edit only while task is still in Backlog; source URL hidden.
- [x] Are task comments MVP, read-only later detail, or fully deferred? Full create/edit/delete in task detail.
- [x] Are workflow question inbox and answer actions MVP, or only future workflow-state surfaces? Home inbox lists/deep-links only; answer flow appears through contextual Resume modal when needed.
- [x] Are transition approval queue and approve actions MVP, or only future workflow-state surfaces? Home inbox lists/deep-links only; approval flow appears through contextual Resume modal when needed.
- [x] Are task start/cancel/resume controls MVP, or is MVP limited to task creation plus visual progression? No Start button. Cancel and Resume in task detail; Resume also on cards when task is resumable.
- [x] What card/detail action appears for active task sessions? Interrupt appears where Resume would be; Resume appears only when paused/resumable.
- [x] What terminal command is canonical for teleport? Backend returns task/session attach identifiers; GUI/native bridge owns the local TUI attach command once final.
- [x] How should project creation map directory picker result to Builder project/workspace binding? Dedicated New Project/Add Workspace buttons resolve path; existing bound workspace opens project; unbound workspace opens project creation page with name confirmation.
- [x] Is multi-workspace per project visible in MVP or hidden behind selected workspace? Visible through main workspace dropdown in task creation/edit before start; immutable after start.
- [x] What is board workspace scope after backend task/workspace model update? Board is project-wide for selected project and selected workflow; cards show workspace context when needed, and additional workspaces are complementary/optional by default.
- [x] What is the task main workspace default? Current/opened workspace context if present, otherwise project default/main workspace.
- [x] Should MVP add project-key create/edit API support? Yes. Project creation includes editable project key and backend validates collisions/immutability.
- [x] How should workflow groups render in MVP board? Build the best implementation-led MVP first pass; initial preferred shape is group islands wrapping related columns, with Nikita QA driving changes if it feels bad.
- [x] How should Home attention inbox sort? Newest activity first across all attention items.
- [x] How should task detail activity feed sort? Newest-to-oldest with pagination for older entries.
- [x] Which workflow does New Task use? The New Task plus button exists only inside a workflow/Kanban view, so it uses the currently selected board workflow.
- [x] Should cancel require a reason? No. MVP cancel uses confirmation only and does not ask for a reason.
- [x] Should drag-to-start ask for confirmation? No default confirmation; dropping a backlog task onto the first active node starts automation immediately.
- [x] What format do task body/details and comments use? Plain multiline input with shared Markdown rendering, no WYSIWYG editor.
- [x] What Markdown renderer does MVP use? Shared `MarkdownText` built on `react-markdown`, `remark-gfm`, and `rehype-sanitize`, with raw HTML disabled and sanitized output.
- [x] Should project creation expose all DB fields? No blanket exposure; decide additional fields per field based on operator value and backend safety.
- [x] Should Interrupt ask for confirmation? No. Interrupt acts immediately.
- [x] What happens after Interrupt succeeds? Same as TUI: task becomes interrupted/resumable and feed records `Interrupted by user`.
- [x] Where does comment composer live in newest-first task detail? Under fixed task identity/header, above the activity feed.
- [x] What does Home attention inbox row show? Task short ID, title, project key/name, workflow, attention type, latest activity time, and small status/action hint.
- [x] How does desktop find server endpoint? Use Builder config/default host and port only for MVP; endpoint editing is deferred.
- [x] What should project with no valid workflow show? Board blocker/empty state, New Task disabled, and pointer to CLI/agent/API workflow setup.
- [x] How should workflow picker order workflows? Project default first, then most-recently-used, then display name.
- [x] What should Home project list show? Latest-activity descending rows with project key/name, primary workspace path/status, updated time, and attention/task count chips when backend provides them.
- [x] What does clicking a Home project row open? The project's default workflow board.
- [x] What does clicking a Home attention inbox row open? Standalone task detail route over Home without loading the board.
- [x] How does task form show workspace when project has exactly one workspace? Compact disabled workspace selector/chip.
- [x] What happens to drafts during disconnect/reconnect? Keep local draft visible, disable submit while disconnected, refresh on reconnect, and let preserved user draft overwrite remote state on save.
- [x] Should approval UI expose Reject? No. Expose Approve only in MVP; use Interrupt/Cancel for negative path until Reject semantics are product-defined.
- [x] What should approval UI show? Stored approval snapshot: source node, transition label/id, target node(s), output fields/values, commentary, graph revision, and stale warning when relevant.
- [x] How should question answer UI work? Preserve full ask functionality, but use normal controls: options plus blank commentary/freeform field, click or arrows plus Enter, standard Tab focus, recommended marker, no source-origin label.
- [x] How should multiple unresolved attention items show? All unresolved feed items get inline action controls; top action opens next/highest-priority item.
- [x] What happens after resolving attention from standalone Home task detail? Stay in detail, update feed/status, remove or resort Home inbox row in background.
- [x] What exact behavior should the hover-expandable non-modal popup use for workflow picker, nav, and actions? Include Home/back, Inbox, New Task, Pin, and workflow list; unpinned auto-collapses on unhover; pinned becomes floating-island-sidebar-like.
- [x] What errors must block UI actions vs allow read-only browsing? Connection loss disables all mutating actions; app may show last cached state only.
- [x] What accessibility baseline is mandatory for MVP? No accessibility/keyboard-complete pass; i18n-ready strings are mandatory.
- [x] Should task detail include history? Yes: paginated unified activity feed for comments and changes inside fixed-height resizable dialog.
- [x] How should teleport launch artifacts be generated, opened, secured, and cleaned up? Not applicable for MVP; GUI opens default terminal with local Builder TUI attach flow.
- [x] What screenshots/design mocks are needed before implementation starts? None; iterate directly in code.
- [x] Are mockups required before implementation? No; iterate directly in code.
- [x] What manual QA script proves "user can run real tasks using a workflow" end-to-end? No separate script now; Nikita will QA manually during implementation.
- [x] Who performs manual QA? Nikita will QA manually as implementation progresses.
