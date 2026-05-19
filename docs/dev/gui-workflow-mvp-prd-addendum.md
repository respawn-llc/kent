# GUI Workflow MVP PRD Addendum

Status: active addendum for gap closure.

Date: 2026-05-17.

This document supersedes conflicting requirements in the locked GUI workflow MVP PRD, implementation plan, checklists, and stack notes. The locked source docs remain historical. Requirements not changed here still apply.

## Source Precedence

- Latest product decisions in this addendum override older docs.
- `docs/dev/gui-workflow-mvp-prd.md` remains the base PRD where not contradicted.
- `docs/dev/gui-workflow-mvp-gap-audit.md` is a gap inventory, not a product spec.
- `docs/dev/gui-workflow-gap-closure-plan.md` is the execution plan, not product truth.

## Scope

The goal remains PRD-complete Workflow MVP across phased implementation, not only a P0 slice.

The desktop GUI remains a remote-control client over an already-running Builder server. Server APIs/read models remain authoritative for workflows, projects, workspaces, tasks, runtime, scheduler state, validation, approvals, questions, comments, and persistence.

## Removed Or Superseded Requirements

- Settings UI is removed from this MVP. Do not add a settings route, settings button, or top app bar.
- Diagnostics destination is removed from visible MVP navigation. Do not add diagnostics route, settings route, or top-bar affordance. Startup failures should still be clear, and local logs may remain available through support/developer paths.
- Home runtime identity/header fluff is removed. Do not show endpoint, Builder version, auth mode, logo identity, or runtime metadata on Home.
- Home project row metadata is simplified. Rows show project key, project name, and primary workspace path only.
- Tailwind is accepted for the desktop GUI despite older stack notes rejecting it.
- Server/backend capability registry is removed. Do not build backend feature capability discovery/gating for MVP.

## Startup And Protocol

- Desktop validates client/server protocol compatibility.
- Protocol mismatch blocks the app before feature surfaces mount.
- Blocker title is `Update Builder`.
- The blocker shows desktop client protocol and server protocol values.
- Copy instructs the user to update Builder CLI/service and desktop app from the same build.
- The same blocker is used whether the client is newer than the server or older than the server.
- The blocker includes a retry action.
- JSON-RPC handshake enforces mismatch.
- Readiness exposes server protocol, build, and version information for blocking UX and startup error copy.
- Remove the `server.capabilities.get` API/route/client and the desktop startup capability call.
- Native/client capabilities for OS integrations remain separate from server protocol readiness. Use them only for terminal launch, directory picker, native windows, and similar local desktop affordances.

## Home And Project Entry

- Home keeps the fixed glass shell and project-first split: Projects and Inbox.
- Project rows stay simple: key, name, path.
- Project row pencil icon appears top-right aligned with the row title area and opens project edit.
- Home Inbox rows use `Inbox` terminology in UI.
- Home Inbox rows open task detail. Answer/approval controls live in task detail Inbox, not inline on Home.
- Relaunch restores the last valid project/workflow route when possible; fallback is Home.

## Project Edit And Workspaces

- Project edit is a full main-shell page route: `/projects/$projectId/edit`.
- Project edit is not a native child window.
- Back uses app/browser history when available; fallback is Home.
- Project key is read-only.
- Project name is editable.
- Project name validation matches project creation: 1-80 visible chars, no edge whitespace, one line.
- Project name and default workspace changes are saved by explicit Save.
- Back/navigation discards unsaved project name/default changes silently.
- Attach/detach workspace changes are immediate.
- Workspace list is backend-paginated and frontend infinite-scrolled from first implementation.
- Workspace list uses cursor pagination, sorted by attach time descending, page size 100.
- Workspace row shows only workspace path, default icon, and unlink icon.
- Workspace unlink icon uses unchain/unlink semantics, not trash semantics.
- Same workspace path can be linked to multiple different projects.
- Same workspace path is de-duplicated inside one project. Selecting an already-linked path focuses the existing row or shows an equivalent info state.
- Attaching a workspace never deletes files.
- Unlink hard-deletes the project workspace binding/row after validation passes. It never deletes files.
- Unlink is blocked when the workspace is the project default.
- Unlink is blocked when it is the project's only workspace.
- Unlink is blocked when active/non-terminal tasks depend on the workspace.
- Unlink is blocked when active sessions/runs depend on the workspace.
- Unlink is blocked when a Builder-managed owned worktree still depends on the workspace.
- Unlink is blocked when historical task references do not have a durable workspace path/name snapshot sufficient to keep history readable.
- Unlink must not cascade-delete session/task/worktree history. Backend must migrate or otherwise preserve durable workspace path/name snapshots before allowing unlink states that retain historical references.
- Unlink is allowed when only terminal historical tasks reference the workspace and their history remains readable through durable snapshots.
- Unlink confirmation is a simple modal, no type-to-confirm.
- Confirmation copy states app-state effects, that files stay on disk, that completed history remains readable, and that active work blocks unlink. Blocked states show concrete blockers.

## Workflow And Invalid Graphs

- Invalid/default-node-only workflows remain visible.
- Their workflow tasks remain visible.
- New Task remains available for invalid workflows and creates Backlog tasks.
- Backlog edits remain available for title, body, and source workspace while backend permits them.
- Comments remain available on existing tasks.
- Drag/start/run/manual move to workflow nodes or Done is disabled for invalid workflows.
- Cancel, interrupt, and resume follow server action flags for tasks that already have runs from an earlier valid workflow state.
- All tasks should appear in their server-reported board column/status. Non-startable Backlog tasks must not disappear.

## Board

- Board popup/menu follows the PRD action model unless explicitly changed here.
- Hover/focus behavior is allowed for the board popup.
- "No hover effects" means no browser-like hover color changes, highlights, or per-item animation effects.
- Popup open/close may animate using scale, opacity, and material reveal.
- Popup animation respects reduced-motion and deterministic test mode.
- Pinned popup persists as a floating island.
- Done is a fixed-right normal paginated node column and drop target.
- Done permissions, pagination, and status handling must stay server-authoritative.
- Board pagination uses infinite scroll only.

## Task Detail

- Task detail gets a full rewrite phase.
- Opening task detail from Home/Board/shell surfaces uses the reusable native child-window infrastructure when native windows are available.
- Browser/tests use in-app dialog fallback.
- Desktop direct task URL `/tasks/:taskId` renders a standalone inline detail page, not a child window.
- There is one global native task-detail window. Opening another task replaces its content.
- Native/Tauri window owns size and position. Do not keep custom remembered in-app dialog sizing for native detail.
- Board/Home remain behind the native window where applicable.
- Closing the child window after mutations blanket-refetches visible queries.
- Fixed header/actions and task description are always visible.
- `Inbox` area sits above tabs and shows only current blockers plus all answer/approval/resume controls.
- Contextual resume modal is superseded by the task detail Inbox area. Resume/next-blocker actions focus or reveal the relevant Inbox item instead of opening a separate modal.
- Tabs are `Comments`, `Activity`, and `Runs`.
- Default tab is `Comments`.
- Comments tab contains composer plus comment list/edit/delete.
- Comments tab shows a comment count badge.
- Activity tab is a compact timeline-like list with no mutation controls.
- Activity tab does not need a count badge.
- Runs tab contains runs, worktree/session info, teleport, and telemetry when too dense for header.
- Runs tab shows a run count badge.
- UI/product copy says `Inbox`. Backend/internal DTO names may remain `attention` for this MVP unless touched for other reasons.

## Connection Loss And Drafts

- Mutating actions are disabled while disconnected.
- Cached state may remain visible.
- Global disconnected status remains visible until reconnect.
- Preserve unsent local text drafts in memory while the window remains open.
- Preserve drafts for new task, comments, and editable task/project text.
- No offline mutation queue.
- No automatic mutation replay after reconnect.
- On reconnect, refresh server state. User submits preserved drafts manually.

## Verification

- Gap closure must include an acceptance screenshot proof set.
- Proof files live under `.builder/proofs/gui-gap-closure/`.
- Proof harness should use an isolated secondary Builder service on alternate port/persistence root.
- Do not restart or mutate the current agent service for proof.
- Required proof coverage: Home, project edit, valid workflow board, invalid workflow board, task detail, Inbox/question/approval controls, reconnect/draft behavior, and teleport availability/failure states.
