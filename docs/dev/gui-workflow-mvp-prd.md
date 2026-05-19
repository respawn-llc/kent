# GUI Workflow MVP PRD

Status: final MVP product spec for implementation planning.

Date: 2026-05-16.

Sources: `docs/dev/gui-workflow-mvp-prd-checklist.md`, `docs/dev/gui-workflow-use-cases.md`, `docs/dev/gui-client-stack.md`, `docs/dev/gui-foundation-checklist.md`, and current `gui-mvp` branch state.

## Outcome

User can open the desktop app, attach to an existing Builder server, select or create a project, view an existing workflow as a Kanban board, add a task, start automation by dragging the task from Backlog into the first active workflow node, watch task movement/status updates, answer questions or approve transitions when blocked, inspect completed tasks, manage comments, and teleport into the task's terminal TUI session when needed.

MVP acceptance requires real Builder server integration for workflow/project/task data. Mock transport is allowed only for shell/prototype development and deterministic tests.

## Non-Goals

- No workflow graph authoring UI.
- No visual workflow editor.
- No built-in GUI chat.
- No full CLI/TUI replacement.
- No Windows/Linux release target.
- No rich scheduler/admin dashboard.
- No generated docs/checklist drift enforcement.
- No external telemetry or crash reporting.
- No server sidecar bundled or started by the Tauri app.
- No approval Reject action.
- No mutation replay after reconnect.

Workflow authoring remains CLI/agent/API-driven for MVP. GUI operates existing valid workflows and shows blockers when workflow setup is missing or invalid.

## Platform And Native Surface

First MVP ships on macOS.

The window uses:

- Native border, native shape, and native traffic-light controls integrated with app chrome.
- macOS blurred glass/vibrancy background.
- Island-style rounded surfaces over glass material.
- Readable contrast over blur in both light and dark themes.
- Reduced/deterministic blur and motion in tests/snapshots.

All native APIs stay behind bridge packages. Feature components never import Tauri APIs directly.

## Visual System

The UI direction is clean, elegant, effective.

MVP must provide reusable UI primitives for app shell, controls, dialogs, cards, badges, notifications, empty/loading/error states, Markdown content, and Kanban surfaces before feature screens duplicate styling.

Theme and typography:

- Montserrat for main UI.
- Monaspace Neon for IDs, paths, session IDs, branch names, code, commands, and log-like values.
- Dark and light themes.
- Config override when set; system/auto theme otherwise.
- Semantic tokens for palette, spacing, radius, shadow, typography, and motion.
- No hardcoded colors or fonts in feature components.

Motion:

- Animate user-visible transitions unless reduced motion is active.
- Prefer purposeful, quick transitions.
- Use shared element transitions where practical for project-to-board and card-to-detail continuity.

i18n:

- Use static English locale files through `i18next`/`react-i18next`.
- No runtime language switch in MVP.
- No hardcoded user-facing strings in components.

## Startup And Server Attachment

Desktop attaches to Builder config/default host and port only. Endpoint editing is deferred.

Startup flow:

- Launch into safe shell even if setup/backend is broken.
- Show splash/loading while GUI config and server connectivity initialize.
- Call readiness/capability APIs before enabling feature surfaces.
- Show main-shell startup errors, not only logs/toasts.
- If server is unreachable, show instructions to run `builder service install`.

Startup failures need summary-first plain text cause and next action for:

- Onboarding incomplete.
- Missing auth.
- Auth expired/not ready.
- Server not running or unreachable.
- Missing GUI/server config.
- Unreadable or invalid config.
- Readiness failure.
- Protocol/API version mismatch.
- Backend capability mismatch.
- Migration required.
- Unknown startup failure.

Deeper diagnostics go to local GUI log.

## Navigation

Home is project-first landing destination.

Required destinations:

- Startup/splash.
- Startup error.
- Home/project list with global attention inbox.
- Project workflow board.
- Task detail dialog.
- Contextual resume modal.
- Settings/diagnostics.

Navigation rules:

- Relaunch restores last project board when possible.
- User can return to Home/project list.
- Board has no persistent project-list sidebar in MVP.
- Project switching uses Home/back navigation, breadcrumbs, shortcuts, or lightweight top-level controls.
- Board workflow picker and primary board actions live in a hover-expandable non-modal popup/menu.
- Unpinned popup auto-collapses on unhover.
- Pinned popup becomes floating island/sidebar-like widget.
- Dialog close/back behavior is deterministic.
- Server disconnect/reconnect must not corrupt selected project/task route state.

## Home And Project Entry

Home header shows server/runtime setup info such as endpoint, Builder version, logo/app identity, and auth mode.

Home left subpane shows paginated projects sorted by latest activity descending.

Project rows show:

- Project key.
- Project name.
- Primary workspace path/status.
- Updated time.
- Attention/task count chips when backend provides them.

Clicking project row opens that project's default workflow board.

Home right subpane shows global paginated attention inbox sorted newest activity first.

Attention rows show:

- Task short ID.
- Task title.
- Project key/name.
- Workflow.
- Attention type.
- Latest activity time.
- Small status/action hint.

Clicking Home attention row opens standalone task detail over Home without loading board.

Project actions:

- User can pick existing/recent project.
- User can create project through OS-native directory picker.
- User can add workspace through OS-native directory picker.
- If selected workspace already belongs to a project, app opens that project/workspace.
- If selected workspace is unbound, app opens project creation page with editable project name and project key.
- Project creation default name comes from selected directory basename.
- Project key follows backend validation/collision/immutability rules.

Projects with no valid linked/default workflow show board blocker/empty state, disable New Task, and point user to CLI/agent/API workflow setup.

## Workflow Board

Board scope is one selected project plus one selected workflow.

Board shows tasks from all project workspaces. Workspace appears as task/card context metadata, not primary board scope.

Workflow picker:

- Lives in board popup/menu.
- Orders project default workflow first, then most-recently-used workflows, then display name.
- New Task uses currently selected workflow.

Board structure:

- Backlog fixed on left.
- Workflow columns follow server/workflow-defined node order.
- Done fixed on right as a normal paginated node column and drop target.
- Done cards load through the same per-node infinite-scroll pagination as every other column.
- Grouped workflows render with group-aware UI. Initial preferred shape is group islands wrapping related columns inside horizontal board; implementation may adjust if evidence shows a better first pass.

Board card fields:

- Task ID.
- Task name.
- Rich backend-native status component.
- Workspace chip when project has multiple workspaces.

Card actions:

- Card click opens task detail dialog.
- Resume button appears only when task is resumable.
- Interrupt appears in same action slot when exactly one active run is interruptible.
- Card Interrupt acts immediately with no confirmation.
- If task has multiple active runs, card opens detail for per-run controls.

Board supports visual states for:

- Backlog/idle.
- Queued.
- Running.
- Interrupted.
- Approval-gated.
- Question-gated.
- Done/completed.
- Canceled.
- Validation-blocked.

Advanced board filtering/sorting is deferred. Lightweight board search may be added after core flow if cheap.

## Task Creation And Backlog Editing

New Task is only visible inside workflow/Kanban view.

Task creation form:

- Required title.
- Optional body/details.
- Plain multiline Markdown input.
- Source URL/import metadata hidden.
- Main/source workspace dropdown.

Workspace defaults:

- Current/opened workspace context when present.
- Otherwise project default/main workspace.
- If project has exactly one workspace, show compact disabled workspace selector/chip so context remains visible.

Task creation creates Backlog task. There is no Start button in MVP.

Backlog edit rules:

- Title/body/source workspace are editable while task is still in Backlog.
- Source workspace becomes immutable after start/worktree creation.
- User can see validation error text when task create/edit fails.

Starting automation:

- User drags Backlog task to first active workflow node.
- Drop starts automation immediately.
- No default confirmation.

Dragging to Done is allowed when the server reports Done as an allowed manual target. For MVP, Done drop is a user archive move: the server completes the current single active non-terminal placement, creates an active terminal/Done placement, and does not require edge output values or start automation.

## Task Detail

Task card click opens fixed-height, user-resizable task detail dialog.

Dialog layout:

- Global scroll for content.
- Fixed top task identity/content area.
- Comment composer under fixed identity/header.
- Newest-first paginated activity feed below composer.
- Dialog remembers last user-resized size.

Required identity/status fields:

- Task ID.
- Task name.
- Body/details rendered through shared Markdown.
- Project identity.
- Workflow identity.
- Source workspace.
- Current workflow node/status.
- Completion/done/cancel state.
- Server-provided action flags.

Conditional fields:

- Worktree path after worktree exists.
- Agent role and run status when runs exist.
- Session ID/name when run session exists.
- Assignee/column ownership where server provides it, even when unassigned.

Missing-field policy:

- Hide expected-not-yet-created fields when not meaningful.
- Show UX-continuity fields as empty/unassigned where useful.
- Render unexpected missing meaningful fields as unavailable/error states.

Allowed task edits:

- Title/body edits only while still in Backlog.
- Source URL remains hidden.

## Comments And Activity

Comments are full create/edit/delete in MVP.

Comment and task body format:

- Plain multiline input.
- Shared Markdown rendering.
- No WYSIWYG editor.
- Raw HTML disabled and sanitized.

Task activity feed:

- Unified feed for comments and changes.
- Newest-to-oldest.
- Paginated for older entries.
- Does not load full transcripts or `events.jsonl`.
- Uses server activity read model as source of truth.

Activity feed includes comments and transitions at minimum, plus other persisted useful events already provided by server read model.

Deleted comments are hidden by default unless backend later adds explicit delete audit rows.

## Attention, Resume, Questions, And Approvals

Home inbox lists and deep-links attention items. Answer/approval actions happen contextually in task detail/resume surfaces.

Top detail action opens next/highest-priority unresolved attention item. If multiple unresolved attention items exist, detail shows all unresolved feed items with their own inline controls.

Resume behavior:

- Interrupted task can use simple Resume.
- Resume opens contextual modal when user must answer question or approve transition.
- Detail provides in-context resume UX near feed item that caused pause.

Questions:

- Preserve full ask functionality.
- Show options plus blank commentary/freeform field.
- Support suggestion options, freeform/commentary input, and recommended marker.
- Support click or arrows plus Enter to submit.
- Support standard Tab focus navigation.
- Do not show source-origin label.

Approvals:

- Expose Approve only.
- Do not expose Reject until negative semantics are defined.
- Use Interrupt or Cancel for negative paths.
- Show stored approval snapshot: source node, transition label/id, target nodes, required output fields/values, commentary, graph revision, and stale warning when relevant.

Cancel:

- Task detail has Cancel.
- Cancel requires confirmation.
- Cancel does not ask for reason in MVP.

Interrupt:

- Detail shows Interrupt where Resume would appear when active/running.
- Interrupt acts immediately with no confirmation.
- Per-run inline Interrupt controls appear when multiple active runs/fan-out branches exist.
- Successful interrupt follows TUI semantics: task becomes interrupted/resumable and activity feed records `Interrupted by user`.

Standalone detail opened from Home attention stays open after resolution; feed/status update and Home inbox row is removed or resorted in background.

## Teleport

Teleport is temporary bridge to terminal TUI. Built-in GUI chat is later.

Task detail has Teleport when backend returns available target identifiers.

Backend returns identifiers only:

- Task ID.
- Run ID.
- Session ID.
- Project ID.
- Workspace ID.
- Worktree ID.
- CWD relpath.

GUI/native bridge owns local command launch. MVP command target is interactive TUI resume flow:

```sh
builder --continue <session-id>
```

Do not use `builder run --continue` for teleport because that is headless prompt execution, not interactive chat.

Teleport behavior:

- Open user's default terminal on client machine.
- Run local Builder executable with session ID from teleport target.
- If local Builder executable is unavailable, show plain-text failure.
- If backend target is unavailable, show backend failure reason.
- No backend-generated launch artifacts, signed tokens, or opaque launch files in MVP.

## Connection Loss

When connection is lost:

- Disable all mutating actions.
- Last cached state may remain visible.
- Show indefinite global notification/status until reconnect.
- Keep active form/comment drafts visible.
- Disable submit/mutating buttons.

On reconnect:

- Trigger full refresh.
- Resubscribe.
- Preserve local drafts.
- User save overwrites remote state regardless of whether remote changed while disconnected.
- Do not replay mutations automatically.

## Validation And Blockers

Server validation/read models are authoritative.

GUI must surface:

- Invalid/missing default workflow blockers.
- Project-key validation/collision/immutability errors.
- Task create/edit/start validation errors.
- Missing subagent role blockers.
- Stale graph revision warnings.
- Capability-unavailable explanations.
- Read-only browsing vs blocked mutation states.

Every visible failure should include plain cause and next action when one exists.

## Backend Dependency

Real integration depends on server APIs from GUI workflow backend slices:

- Slice 0: readiness and capabilities.
- Slice 1: Home/project admin/project key/workspaces.
- Slice 2: workflow picker, selected board, groups, live updates.
- Slice 3: task source workspace and Backlog editing.
- Slice 4: actions, attention inbox, questions, approvals.
- Slice 5: task detail, activity feed, comments, teleport target.

Current `gui-mvp` branch contains those backend slices from `gui-bootstrap`; frontend implementation should consume them through GUI adapter layers instead of adding local workflow truth.

## MVP Acceptance

MVP is accepted when:

- Desktop app starts on macOS and shows safe startup/readiness states.
- User can attach to an already-running Builder server.
- User can create/open project and see project identity/workspaces.
- User can open default workflow board or see clear no-valid-workflow blocker.
- User can create Backlog task with title/body/workspace.
- User can drag Backlog task into first active node and start automation.
- Board updates after task state changes and after reconnect refresh.
- User can open task detail, see identity/status/worktree/session/run data, and edit Backlog-only fields.
- User can create/edit/delete comments.
- User can inspect newest-first paginated activity.
- User can answer questions and approve transitions from contextual task detail/resume UI.
- User can interrupt, cancel, and resume where backend action flags allow it.
- User can teleport to terminal TUI with `builder --continue <session-id>` when target is available.
- Mutations are disabled while disconnected and drafts survive reconnect.
- GUI tests, typecheck, lint, web build, Tauri check, and required Go/server contract tests pass.
