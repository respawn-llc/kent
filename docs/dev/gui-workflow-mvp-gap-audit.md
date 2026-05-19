# GUI Workflow MVP Gap Audit

Status: current implementation gap audit.

Date: 2026-05-17.

Audited sources:

- `docs/dev/gui-workflow-mvp-prd.md`
- `docs/dev/gui-workflow-mvp-implementation-plan.md`
- `docs/dev/gui-workflow-mvp-prd-checklist.md`
- `docs/dev/gui-workflow-use-cases.md`
- `docs/dev/gui-foundation-checklist.md`
- `docs/dev/gui-client-stack.md`
- `.builder/skills/ui-design/SKILL.md`
- Current dirty worktree under `apps/desktop`, `shared/serverapi`, `server/serverstatus`, and `server/workflowview`.

Audit method:

- Accepted repo root: `/Users/nek/.builder/worktrees/builder-cli-c2f75fc8-68f5-4deb-a23c-21cc5820436d/gui-mvp`.
- Direct code reads were performed from that root.
- One frontend subagent report was accepted because every cited path matched the accepted repo root.
- One doc-review subagent report was accepted after `--workspace "$PWD"` preflight confirmed the accepted repo root.
- Backend subagent reports were discarded when preflight showed repo root `/Users/nek/Developer/builder-cli`.

This document lists PRD-required items that are not implemented, partially implemented, or implemented incorrectly in the current worktree. It is not a fix plan and does not mark intentionally superseded product decisions as done unless the PRD/checklists were also updated.

## Summary

Core backend workflow slices and core frontend screens exist, but the GUI is not PRD-complete. Biggest gaps are Home metadata, startup diagnostics, Add Workspace, board action/menu semantics, task detail layout/actions, question/approval flows, reconnect/draft behavior, and capability/readiness fidelity.

Several later user decisions also conflict with the PRD. Those are listed separately under "Spec Drift To Resolve" because either implementation or PRD must change.

## Priority Index

P0 implementation blockers:

- Startup readiness causes are too generic.
- Capability registry is not real feature gating.
- Home header/runtime info, project-row metadata, attention-row context, Add Workspace, relaunch restore, and diagnostics are missing.
- Board action/menu semantics, card actions, drop permissions, Backlog filtering, and board pagination are incomplete or incorrect.
- Task detail layout, required fields, attention handling, question/approval flows, action flag use, reconnect/draft policy, and standalone Inbox detail behavior are incomplete or incorrect.
- Connection-loss and mutation-disconnect policy is incomplete.

P1 polish/completeness gaps:

- Native capability flags are inconsistent.
- Board status visualization and assignee/ownership display are too thin.
- Theme override, motion/shared transitions, i18n hardcoded strings, structured activity rendering, local diagnostics UI, browser capability degradation, and teleport executable handling are incomplete.

P2 deferred/optional:

- Board search remains unimplemented.

Spec drift:

- Tailwind is now implemented despite stack spec rejecting it.
- Invalid workflow visibility follows later user direction, not PRD text.
- Hover-expandable popup conflicts with no-hover direction.
- Settings destination conflicts with recent no-settings shell direction.

## Acceptance Blockers

Default priority for this section is P0: fix before claiming PRD-complete MVP. Items explicitly marked P1 are important follow-ups that do not block core workflow operation. Items are ordered by implementation urgency inside each product area.

### Startup Readiness Causes Are Too Generic

Status: implemented incorrectly.
Priority: P0.

Spec: startup failures need summary-first plain text causes and next actions for onboarding incomplete, missing auth, auth expired/not ready, server unreachable, missing/invalid config, readiness failure, protocol mismatch, capability mismatch, migration required, and unknown startup failure.

Current state: backend readiness returns a single generic `server_not_ready` cause when auth is not ready. GUI mostly shows that first cause or raw transport/config error text.

Evidence:

- `server/serverstatus/service.go:22-52` returns only generic `server_not_ready` and hardcodes the next action.
- `apps/desktop/src/features/startup/useStartup.ts:67-72` displays only the first readiness cause.

Impact: GUI cannot distinguish missing auth, onboarding, migration, protocol mismatch, or other PRD-required startup blockers.

### Capability Registry Is Not Real Feature Gating

Status: implemented incorrectly.
Priority: P0.

Spec: capability API must expose unavailable backend capabilities with reasons, and feature surfaces must disable dependent actions with explanations.

Current state: backend returns all MVP capabilities as available. Frontend only blocks startup if a required capability is unavailable; feature-level controls generally do not reference capability availability.

Evidence:

- `server/serverstatus/service.go:55-87` builds every capability with `Available: true`.
- `apps/desktop/src/features/startup/useStartup.ts:52-58` checks capabilities only during startup.
- Board/task/home actions gate mostly on connection state and action flags, not capability availability.

Impact: capability mismatch cannot degrade to clear read-only/disabled UI beyond startup.

### Native Capability Flags Are Inconsistent With Implementations

Status: implemented incorrectly.
Priority: P1.

Spec: native bridge capabilities must be explicit and capability-checked; browser implementations disable unavailable actions with explanations, while Tauri implementations expose supported native capabilities.

Current state: Tauri capabilities report clipboard read/write as available, but the Tauri clipboard methods throw unavailable errors. macOS vibrancy is configured at the Tauri window level but `macosVibrancy` reports false.

Evidence:

- `apps/desktop/packages/native-bridge/src/index.ts:235-241` reports Tauri clipboard methods unavailable.
- `apps/desktop/packages/native-bridge/src/index.ts:333-361` reports clipboard available and `macosVibrancy: false`.
- `apps/desktop/src-tauri/tauri.conf.json:20-31` configures transparent/window effects.

Impact: feature code cannot trust native capability flags.

### Home Header Runtime Info Missing

Status: not implemented.
Priority: P0.

Spec: Home header shows server/runtime setup info such as endpoint, Builder version, logo/app identity, and auth mode.

Current state: Home route renders Projects and Inbox panes only.

Evidence:

- `apps/desktop/src/features/home/HomeRoute.tsx:115-149` renders only the pane grid.
- `apps/desktop/src/appEnvironment.ts:28-35` exposes `endpoint`, but Home does not render it.

Impact: user cannot verify attached server/runtime context from Home.

### Settings And Diagnostics Destination Missing

Status: not implemented.
Priority: P0.

Spec: required destinations include Settings/diagnostics. Startup unknown errors also point the user to diagnostics.

Current state: route tree has Home, Project board, Task detail, and native project-create dialog only.

Evidence:

- `apps/desktop/src/app/routes.tsx:33-59` defines only Home, project, task, and project-create routes.
- `apps/desktop/src/i18n/en.ts:7` contains `app.diagnostics`, but no destination exists.

Impact: PRD-required diagnostics path is dead copy.

### Relaunch Restoration Missing

Status: not implemented.
Priority: P0.

Spec: relaunch restores last project board when possible, with safe Home fallback.

Current state: no persisted route/last project state was found.

Evidence:

- No `localStorage`, `sessionStorage`, persisted route, or last-project restoration code exists under `apps/desktop/src`.
- `apps/desktop/src/app/routes.tsx:33-43` routes `/` to Home and project board only when URL already contains project path.

Impact: app relaunch does not restore last board.

### Add Workspace Flow Missing

Status: not implemented.
Priority: P0.

Spec: user can add workspace through OS-native directory picker.

Current state: API mutation exists, but no visible Add Workspace control or flow calls it.

Evidence:

- `apps/desktop/src/features/home/useHomeData.ts:61-72` defines `useWorkspaceAttach`.
- `apps/desktop/src/features/home/HomeRoute.tsx:188-207` exposes one `+` New Project/workspace picker entry.
- `apps/desktop/src/features/home/HomeRoute.tsx:23-149` never calls `useWorkspaceAttach`.

Impact: GUI can create/open projects but cannot attach another workspace to an existing project.

### Home Project Rows Missing Required Metadata

Status: partially implemented.
Priority: P0.

Spec: project rows show project key, project name, primary workspace path/status, updated time, and attention/task count chips when backend provides them.

Current state: rows show project key, name, and primary workspace path only.

Evidence:

- `apps/desktop/src/features/home/HomeRoute.tsx:211-230` renders only key, name, and workspace path.

Impact: Home lacks activity/attention signal needed for project-first triage.

### Home Attention Rows Missing Required Metadata

Status: partially implemented.
Priority: P0.

Spec: attention rows show task short ID, task title, project key/name, workflow, attention type, latest activity time, and small status/action hint.

Current state: rows show attention kind, optional task short ID, optional title, message, and time. No project key/name, workflow name, or action hint is rendered.

Evidence:

- `apps/desktop/src/features/home/HomeRoute.tsx:270-296` renders only kind, task short ID/title, message, and time.
- `shared/serverapi/workflow.go:383-398` does not include structured project key/name or workflow display name fields.

Impact: Inbox context is incomplete. Recent workflow-name-in-message workaround is not a structured PRD implementation.

### Standalone Attention Detail Is Not Opened Over Home

Status: implemented incorrectly.

Spec: clicking Home attention row opens standalone task detail over Home without loading the board.

Current state: clicking task-backed attention navigates to `/tasks/$taskId`; that route renders only the dialog, not Home behind it.

Evidence:

- `apps/desktop/src/app/routes.tsx:46-50` maps `/tasks/$taskId` to `StandaloneTaskRoute`.
- `apps/desktop/src/features/task-detail/StandaloneTaskRoute.tsx:8-10` returns only `TaskDetailDialog`.

Impact: Home context disappears instead of staying as background destination.

### Board Control Popup Does Not Match PRD

Status: partially implemented.

Spec: workflow picker and primary board actions live in a hover-expandable non-modal popup/menu; unpinned popup auto-collapses on unhover; pinned popup becomes floating island/sidebar-like widget; popup contains Home/back, Inbox, New Task, Pin, and workflow list.

Current state: workflow picker is a simple `<details>` menu. New Task is a separate button. There is no pinning, hover expand/collapse, Inbox entry, or Home/back inside the board popup.

Evidence:

- `apps/desktop/src/features/board/BoardRoute.tsx:98-108` renders New Task outside the picker/menu.
- `apps/desktop/src/features/board/BoardRoute.tsx:171-201` implements the picker as a simple `<details>` menu.

Impact: board navigation/action model differs from spec.

### Board Card Actions Missing

Status: not implemented.

Spec: Resume appears on cards only when resumable; Interrupt appears in same action slot when exactly one active run is interruptible; multiple active runs open detail for per-run controls.

Current state: cards render title/body/status/workspace/time only. No Resume or Interrupt card action.

Evidence:

- `apps/desktop/src/features/board/BoardColumns.tsx:126-157`

Impact: common task control path requires opening detail even when PRD says card action should exist.

### Board Drop-To-Done Ignores Backend Permission

Status: implemented incorrectly.

Spec: dragging to Done is allowed only when backend permits manual transition.

Current state: any drop on Done calls `workflow.task.move` when connected and workflow is valid. It does not check a per-task/per-target permission fact.

Evidence:

- `apps/desktop/src/features/board/BoardRoute.tsx:71-83`
- `apps/desktop/src/api/client.ts:185-190`

Impact: UI offers/attempts a mutation that backend may reject instead of disabling forbidden drop.

### Board Read Model Lacks Target-Specific Move Permissions

Status: partially implemented.

Spec: dragging to Done is allowed only when backend permits manual transition, and GUI should use server-authoritative validation/action facts.

Current state: card action DTO has `canStart`, `canInterrupt`, `canResume`, and `canCancel`, but no target-node/manual-move permission list. Frontend therefore cannot reliably gate drop-to-Done or other manual transitions before calling `workflow.task.move`.

Evidence:

- `shared/serverapi/workflow.go:532-541`
- `apps/desktop/src/api/models.ts:121-130`
- `apps/desktop/src/features/board/BoardRoute.tsx:71-83`

Impact: correct frontend gating likely needs backend contract expansion.

### Board Backlog Filtering Can Hide Blocked Backlog Tasks

Status: likely implemented incorrectly.

Spec: board must show Backlog/idle and validation-blocked task states. Invalid/default-node-only workflows should still show the workflow and tasks while disabling run mutations.

Current state: Backlog column cards are selected by `card.actions.canStart`, not by actual Backlog placement/status. A Backlog task with `canStart=false` can disappear from Backlog.

Evidence:

- `apps/desktop/src/features/board/BoardModel.ts:8-20`

Impact: validation-blocked or otherwise non-startable Backlog tasks may be hidden.

### Board Pagination Not Implemented In UI

Status: partially implemented.

Spec: unbounded collections use pagination/infinite scroll. Board endpoint returns page token data.

Current state: frontend requests `page_size: 300` and `page_token: ""`, but ignores `board.nextPageToken` and has no board infinite loading.

Evidence:

- `apps/desktop/src/api/client.ts:114-128`
- `apps/desktop/src/features/board/BoardRoute.tsx:23-167`

Impact: projects with more than one board page silently omit tasks.

### Board Status Visualization Is Too Thin

Status: partially implemented.

Spec: board supports distinct visual states for Backlog/idle, queued, running, interrupted, approval-gated, question-gated, done/completed, canceled, and validation-blocked; rich status component uses spinner/icons/colors.

Current state: card status is one info badge with backend label. No spinner, icon, or state-specific treatment.

Evidence:

- `apps/desktop/src/features/board/BoardColumns.tsx:150-153`
- `apps/desktop/src/ui/Badge.tsx:13-28`

Impact: state changes are visible as text only, not the rich workflow board specified.

### Assignee/Column Ownership Missing

Status: not implemented.

Spec: assignee is represented per column, and task detail shows assignee/column ownership where server provides it.

Current state: columns render name and task count only; task detail renders runs and source workspace but no assignee/column owner.

Evidence:

- `apps/desktop/src/features/board/BoardColumns.tsx:83-109`
- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:189-205`

Impact: operator cannot see which role/agent owns a workflow column.

### Task Detail Dialog Not Resizable Or Size-Persistent

Status: not implemented.

Spec: task card click opens fixed-height, user-resizable task detail dialog; dialog remembers last user-resized size.

Current state: generic dialog has fixed max size and no resize handle/state persistence.

Evidence:

- `apps/desktop/src/ui/Dialog.tsx:32-53`
- `apps/desktop/src/features/task-detail/TaskDetailDialog.tsx:24-43`

Impact: detail dialog cannot be resized or restored per user preference.

### Task Detail Layout Does Not Match PRD

Status: implemented incorrectly.

Spec: dialog has global scroll, fixed top task identity/content area, comment composer under fixed identity/header, and newest-first paginated activity feed below composer.

Current state: detail content, edit form, actions, attention boxes, telemetry, teleport, comments, and activity are stacked. Activity feed is outside `TaskDetailContent`; comments list is separate from activity feed.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailDialog.tsx:24-43`
- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:27-60`
- `apps/desktop/src/features/task-detail/TaskDetailActivity.tsx:34-123`

Impact: comment/activity UX does not match required task-detail mental model.

### Task Detail Required Fields Are Incomplete

Status: partially implemented.

Spec: detail shows task ID/name/body, project identity, workflow identity, source workspace, current workflow node/status, completion/done/cancel state, action flags, worktree path, agent role/status, session ID/name, and ownership where provided.

Current state: detail shows short ID, title, body, project/workflow names, status label, source workspace, worktree path when present, run ID/status/session ID, and action buttons. It does not show full task ID, project key, current node names, explicit done/cancel state, server action flags, session name clearly, or ownership fields.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:29-44`
- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:189-205`

Impact: task detail is useful but not PRD-complete.

### Task Detail Does Not Render Structured Activity Payloads

Status: partially implemented.

Spec: activity feed renders comments and transitions from structured activity payloads.

Current state: activity feed rows render `item.summary` and timestamp only, ignoring structured `comment`, `transition`, `run`, and `attention` payloads parsed by the adapter.

Evidence:

- `apps/desktop/src/api/models.ts:275-287`
- `apps/desktop/src/features/task-detail/TaskDetailActivity.tsx:116-123`

Impact: feed cannot show approval snapshots, comments, run state, or transition details in a durable structured way.

### Top Attention Action Missing

Status: not implemented.

Spec: top detail action opens next/highest-priority unresolved attention item.

Current state: Resume button directly calls `workflow.task.resume`; question/approval sections are inline below actions. There is no "next attention" action selection.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:133-185`

Impact: detail does not guide user to the next blocker when multiple exist.

### Multiple Unresolved Attention Items Not Rendered

Status: implemented incorrectly.

Spec: if multiple unresolved attention items exist, detail shows all unresolved feed items with their own inline controls.

Current state: code selects only first question and first approval. Other unresolved questions/approvals are ignored.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:23-24` uses `detail.attention.find(...)`.
- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:47-57` renders at most one question and one approval.

Impact: user may not see or resolve all blockers.

### Contextual Resume Modal Missing

Status: not implemented.

Spec: Resume opens contextual modal when user must answer question or approve transition.

Current state: Resume button immediately calls `workflow.task.resume`; question and approval controls are separate inline boxes.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:146-153`

Impact: resume flow does not match contextual modal requirement.

### Question UI Missing Required Keyboard Behavior

Status: partially implemented.

Spec: question flow supports options, freeform/commentary input, recommended marker, click or arrows plus Enter to submit, and standard Tab focus navigation.

Current state: options are clickable buttons and textarea supports Tab naturally. No Arrow/Enter option selection/submission behavior is implemented.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailAttention.tsx:44-81`

Impact: question interaction is incomplete versus PRD.

### Question Option Plus Commentary Can Conflict

Status: likely implemented incorrectly.

Spec: question UI has suggestion options plus blank commentary/freeform field.

Current state: if user selects an option and types text, request sends both `selected_option_number` and `freeform_answer`. Backend Slice 4 progress says conflicting answer modes are rejected.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailAttention.tsx:27-35`
- `apps/desktop/src/api/client.ts:248-259`
- `docs/dev/gui-workflow-backend-implementation-progress.md` notes conflicting answer modes are rejected.

Impact: UI permits a user input combination likely rejected by server.

### Approval Snapshot Incomplete

Status: partially implemented.

Spec: approval UI shows stored approval snapshot: source node, transition label/id, target nodes, required output fields/values, commentary, graph revision, and stale warning when relevant.

Current state: approval UI shows source node, transition name/id, output values, and graph revision. It does not render target nodes, required output fields, commentary, or stale warning.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailAttention.tsx:103-117`
- `apps/desktop/src/api/models.ts:229-250` includes edges/commentary that UI does not render.

Impact: approve decision lacks required context.

### Detail Interrupt Ignores Server Action Flags

Status: implemented incorrectly.

Spec: Interrupt appears where Resume would appear when active/running; per-run controls appear when multiple active runs/fan-out branches exist; actions should follow server-provided flags.

Current state: interrupt buttons are rendered for every locally derived active run. Code does not check `detail.actions.canInterrupt`, `interruptRunID`, or `needsDetailForInterrupt`.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:25`
- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:155-164`

Impact: UI can expose interrupt controls when server action flags do not permit them.

### Home Inbox Does Not Update After Standalone Resolution

Status: partially implemented.

Spec: standalone detail opened from Home attention stays open after resolution; feed/status update and Home inbox row is removed or resorted in background.

Current state: task mutations invalidate task and activity queries only. Global attention/Home queries are not invalidated by detail mutations.

Evidence:

- `apps/desktop/src/features/task-detail/useTaskDetailData.ts:42-45`

Impact: resolved attention may remain stale in Home inbox until unrelated refresh/subscription.

### Connection-Loss Notification Is Dismissible

Status: implemented incorrectly.

Spec: connection loss shows indefinite global notification/status until reconnect.

Current state: disconnect notice can be dismissed. It is only re-pushed on connection phase changes, so it can stay hidden while still disconnected.

Evidence:

- `apps/desktop/src/features/startup/StartupGate.tsx:20-31`
- `apps/desktop/src/ui/StatusSurface.tsx:43-50`

Impact: user can dismiss required offline warning while mutations remain disabled.

### Disconnect Mutation Gating Is Incomplete

Status: implemented incorrectly.

Spec: when disconnected, all mutating actions are disabled.

Current state: many actions are disabled from connection state, but project creation submit only checks `isCreating`, not connection phase. Some native child-window flows do not receive a connection-disabled prop.

Evidence:

- `apps/desktop/src/features/home/ProjectCreateForm.tsx:111-136`

Impact: project create can be submitted while disconnected and fail through transport rather than being disabled.

### Draft Preservation Across Reconnect Is Not Guaranteed

Status: partially implemented.

Spec: active form/comment drafts stay visible while disconnected; after reconnect refresh, preserved user drafts win on save.

Current state: some draft text is local component state, but `TaskEditForm` is keyed by `detail.updatedAt`, so a detail refresh can remount and drop unsaved title/body/workspace edits. New task/comment drafts are also component-local only and are not explicitly protected from route/query remounts.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:45`
- `apps/desktop/src/features/tasks/NewTaskDialog.tsx:42-46`
- `apps/desktop/src/features/task-detail/TaskDetailActivity.tsx:20-31`

Impact: reconnect/refetch can lose drafts, violating PRD reconnect policy.

### Theme Config Override Missing

Status: not implemented.

Spec: dark/light themes support config override when set; system/auto otherwise.

Current state: CSS uses `prefers-color-scheme`. No Builder config/theme override path found.

Evidence:

- `apps/desktop/src/styles.css:66-86`
- `apps/desktop/src/appEnvironment.ts:13-35` resolves server endpoint/persistence root only.

Impact: operator cannot force theme via config.

### User-Visible Transitions Mostly Missing

Status: partially implemented.

Spec: animate every user-visible transition unless reduced motion is active; shared element transitions where practical for project-to-board and card-to-detail.

Current state: there are minor field/error transitions and spinner animation, but no route/dialog/card shared-element transitions or broad motion system.

Evidence:

- `apps/desktop/src/ui/Field.tsx:37-40`
- `apps/desktop/src/ui/Dialog.tsx:22-55`
- `apps/desktop/src/features/board/BoardColumns.tsx:126-157`

Impact: visual system does not meet PRD motion bar.

### Hardcoded User-Facing Strings Remain

Status: partially implemented.

Spec: no hardcoded user-facing strings in components.

Current state: most feature copy uses i18n, but some startup/native strings remain hardcoded.

Evidence:

- `apps/desktop/src/features/startup/useStartup.ts:63-72` returns `"Unknown startup failure"` and `"Readiness check failed."`.
- `apps/desktop/packages/native-bridge/src/index.ts:291-303` uses `"Create project"` native title.

Impact: i18n boundary is not complete.

### Local Builder Executable For Teleport Is Not Configurable

Status: partially implemented.

Spec: teleport runs local Builder executable and shows plain-text failure if unavailable.

Current state: teleport uses `builder` from `PATH`, validates `builder --help`, and does not expose a GUI/config path override. This meets the minimal failure case but can fail for users running a different local binary path than the desktop app environment sees.

Evidence:

- `apps/desktop/src-tauri/src/lib.rs:114-125`

Impact: teleport works only when `builder` is visible in Tauri process `PATH`.

## Lower-Priority Or Follow-Up Gaps

### Local Diagnostics Log Has No UI

Status: partially implemented.

Spec: deeper diagnostics go to local GUI log; diagnostics destination is required.

Current state: bounded/redacted log writing exists, but no GUI diagnostics screen or "open log" action exists.

Evidence:

- `apps/desktop/src/app/logging.ts:21-67`
- `apps/desktop/src-tauri/src/lib.rs:66-84`

### Browser Native Capability Degradation Is Incomplete

Status: partially implemented.

Spec: browser implementation uses real browser APIs where available, no-ops only cosmetic shell features, and disables terminal/updater/window controls with explicit explanations.

Current state: capabilities are present, but most explanations are generic thrown errors or button-disabled text. No unified capability-unavailable surface exists for feature controls.

Evidence:

- `apps/desktop/packages/native-bridge/src/index.ts:125-228`
- `apps/desktop/src/features/task-detail/TaskDetailContent.tsx:209-242`

### Comments Are Not Integrated Into Unified Activity UX

Status: partially implemented.

Spec: activity feed is unified feed for comments and changes; comment composer sits above newest-first activity feed.

Current state: comments have a separate comment list and CRUD controls, while activity feed also exists separately.

Evidence:

- `apps/desktop/src/features/task-detail/TaskDetailActivity.tsx:34-123`

### Board Search Is Unimplemented

Status: deferred idea, not blocker.

Spec: lightweight board search may be added after core flow if cheap.

Current state: no board search.

Evidence:

- `apps/desktop/src/features/board/BoardRoute.tsx:23-201`

## Spec Drift To Resolve

These are not pure implementation bugs because later decisions or current work intentionally diverged from source docs. PRD/checklists should be updated if these decisions stand.

### Tailwind Now Conflicts With `gui-client-stack.md`

Current implementation uses Tailwind v4. `docs/dev/gui-client-stack.md` still says "Do not add Tailwind or vanilla-extract for MVP." Later user request explicitly asked for full Tailwind migration. Update stack spec if Tailwind is accepted.

Evidence:

- `apps/desktop/src/styles.css:1`
- `apps/desktop/vite.config.ts:1-7`
- `docs/dev/gui-client-stack.md:122-126`

### Invalid Workflow Visibility Changed

PRD says projects with no valid linked/default workflow show blocker/empty state and disable New Task. Latest user decision says invalid/default-node-only workflows should still be visible; tasks should not be runnable. Current code follows latest user direction for invalid workflows, but PRD still needs update.

Evidence:

- `apps/desktop/src/features/board/BoardRoute.tsx:46-50`
- `apps/desktop/src/features/board/BoardRoute.tsx:94-107`
- `docs/dev/gui-workflow-mvp-prd.md:166`

### Hover-Expandable Popup Conflicts With No-Hover Direction

PRD requires hover-expandable popup/menu. Current locked GUI instructions say global hover effects are disabled and should not be re-added. Need product decision: replace hover behavior in PRD or explicitly allow this one control.

Evidence:

- `docs/dev/gui-workflow-mvp-prd.md:120-122`
- Current conversation/handoff product constraint: no global hover effects.

### Settings Destination Conflicts With Recent "No Settings" Direction

PRD requires Settings/diagnostics destination. Recent GUI shell direction says no top app bar/settings. Diagnostics may still be required, but PRD should clarify whether Settings is removed or diagnostics remains as a hidden/support route.

Evidence:

- `docs/dev/gui-workflow-mvp-prd.md:104-112`
- `docs/dev/gui-foundation-checklist.md:47`
- `apps/desktop/src/app/routes.tsx:33-59` has neither settings nor diagnostics.

## Accepted Subagent Results

- Background shell `2310`, subagent continuation `4431b52a-3b95-4636-84d4-d9f6c11faeb6`: accepted. Preflight repo root matched accepted root. Scope was doc-review only; findings were used to tighten priority notes and subagent audit notes.

## Discarded Subagent Results

- Background shell `2007`, subagent continuation `378a714e-5b9f-49ef-a8bf-ef98742a727c`: rejected. Reported repo root `/Users/nek/Developer/builder-cli`, not accepted repo root.
- Direct local-binary subagent continuation `ad7b2070-dc53-4752-941e-f44b5dace05f`: rejected. Preflight reported `pwd` and repo root `/Users/nek/Developer/builder-cli` despite invoking `./bin/builder` from current worktree.

Subagent policy after this audit: invoke Builder subagents with `--workspace "$PWD"` plus prompt preflight requiring `pwd` and `git rev-parse --show-toplevel`; discard output unless both match the accepted repo root.
