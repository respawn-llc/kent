# GUI Workflow Gap Closure Plan

Status: implemented through Phase 9 proof capture.

Date: 2026-05-17.

Source inputs:

- `docs/dev/gui-workflow-mvp-prd.md`
- `docs/dev/gui-workflow-mvp-prd-addendum.md`
- `docs/dev/gui-workflow-mvp-gap-audit.md`
- `.builder/plans/gui-gap-closure-interview.md`

Do not update older locked PRD/checklist docs while executing this plan. Use the addendum as product truth for superseded decisions.

## Completion Criteria

- All P0 gaps from `docs/dev/gui-workflow-mvp-gap-audit.md` are either implemented or explicitly superseded by `docs/dev/gui-workflow-mvp-prd-addendum.md`.
- All changed backend business logic has tests.
- Desktop unit/component tests pass for changed flows.
- `./scripts/test.sh` passes, or any failure is unrelated and documented with evidence.
- `./scripts/build.sh --output ./bin/builder` passes before handoff after code changes.
- Screenshot proof set exists under `.builder/proofs/gui-gap-closure/`.
- Proof manifest maps each acceptance case to screenshot paths and seeded fixture state.

## Audit Crosswalk

Each audit heading below maps to the phase that closes it or to the addendum section that explicitly supersedes it.

| Audit heading | Priority | Disposition |
| --- | --- | --- |
| Startup Readiness Causes Are Too Generic | P0 | Phase 1 |
| Capability Registry Is Not Real Feature Gating | P0 | Superseded by addendum; remove in Phase 1 |
| Native Capability Flags Are Inconsistent With Implementations | P1 | Phase 8 |
| Home Header Runtime Info Missing | P0 | Superseded by addendum; do not implement |
| Settings And Diagnostics Destination Missing | P0 | Superseded by addendum; do not implement visible routes |
| Relaunch Restoration Missing | P0 | Phase 7 |
| Add Workspace Flow Missing | P0 | Phases 2-3 |
| Home Project Rows Missing Required Metadata | P0 | Superseded by addendum; simplified rows plus edit pencil in Phase 3 |
| Home Attention Rows Missing Required Metadata | P0 | Phase 6, with UI terminology changed to Inbox |
| Standalone Attention Detail Is Not Opened Over Home | P0 | Phase 6, with UI terminology changed to Inbox |
| Board Control Popup Does Not Match PRD | P0 | Phase 5 |
| Board Card Actions Missing | P0 | Phase 5 |
| Board Drop-To-Done Ignores Backend Permission | P0 | Phase 5 |
| Board Read Model Lacks Target-Specific Move Permissions | P0 | Phase 5 |
| Board Backlog Filtering Can Hide Blocked Backlog Tasks | P0 | Phases 4-5 |
| Board Pagination Not Implemented In UI | P0 | Phase 5 |
| Board Status Visualization Is Too Thin | P1 | Phase 5 |
| Assignee/Column Ownership Missing | P1 | Phases 5-6 |
| Task Detail Dialog Not Resizable Or Size-Persistent | P0 | Superseded by native window sizing in Phase 6 |
| Task Detail Layout Does Not Match PRD | P0 | Superseded by addendum layout; implement in Phase 6 |
| Task Detail Required Fields Are Incomplete | P0 | Phase 6 |
| Task Detail Does Not Render Structured Activity Payloads | P0 | Phase 6 |
| Top Attention Action Missing | P0 | Phase 6, with UI terminology changed to Inbox |
| Multiple Unresolved Attention Items Not Rendered | P0 | Phase 6, with UI terminology changed to Inbox |
| Contextual Resume Modal Missing | P0 | Superseded by task detail Inbox focus/reveal behavior in Phase 6 |
| Question UI Missing Required Keyboard Behavior | P0 | Phase 6 |
| Question Option Plus Commentary Can Conflict | P0 | Phase 6 |
| Approval Snapshot Incomplete | P0 | Phase 6 |
| Detail Interrupt Ignores Server Action Flags | P0 | Phase 6 |
| Home Inbox Does Not Update After Standalone Resolution | P0 | Phase 6 |
| Connection-Loss Notification Is Dismissible | P0 | Phase 7 |
| Disconnect Mutation Gating Is Incomplete | P0 | Phase 7 |
| Draft Preservation Across Reconnect Is Not Guaranteed | P0 | Phase 7 |
| Theme Config Override Missing | P1 | Phase 8 |
| User-Visible Transitions Mostly Missing | P1 | Phases 5, 6, and 8 |
| Hardcoded User-Facing Strings Remain | P1 | Phase 8 |
| Local Builder Executable For Teleport Is Not Configurable | P1 | Phase 8 |
| Local Diagnostics Log Has No UI | P1 | Superseded by addendum; no visible diagnostics UI |
| Browser Native Capability Degradation Is Incomplete | P1 | Phase 8 |
| Comments Are Not Integrated Into Unified Activity UX | P1 | Superseded by addendum tabs; Comments and Activity are separate in Phase 6 |
| Board Search Is Unimplemented | P2 | Deferred optional; no phase |
| Tailwind Now Conflicts With `gui-client-stack.md` | Drift | Superseded by addendum |
| Invalid Workflow Visibility Changed | Drift | Phase 4 |
| Hover-Expandable Popup Conflicts With No-Hover Direction | Drift | Clarified by addendum; implement in Phase 5 |
| Settings Destination Conflicts With Recent "No Settings" Direction | Drift | Superseded by addendum |

## Phase 0: Planning Docs

- [x] Create gap audit.
- [x] Interview product decisions.
- [x] Create PRD addendum.
- [x] Create gap-closure execution plan.

## Phase 1: Startup Protocol And Capability Removal

Goal: replace fake backend capability responses with strict protocol/version compatibility and clear startup blockers.

- [x] Add/verify protocol-version source of truth shared by server and desktop.
- [x] Enforce protocol mismatch in JSON-RPC handshake.
- [x] Extend readiness response with server protocol, build, and version fields.
- [x] Render `Update Builder` blocker with client protocol, server protocol, same-build update instruction, and retry.
- [x] Remove `server.capabilities.get` server route/service/DTO/client call.
- [x] Keep native/client capability checks only in native bridge domain.
- [x] Update startup tests for reachable, unreachable, readiness failure, and protocol mismatch.
- [x] Add backend/server contract tests for readiness protocol fields and mismatch behavior.

Phase 1 progress note: JSON-RPC handshake mismatch was already covered by server transport tests; this phase kept that behavior and added desktop startup mismatch coverage. Backend capability route/client/DTO/service code is removed. The protocol version is loaded from `shared/protocol/version.json` by both Go and desktop TypeScript. Readiness now exposes protocol, version, and build identity.

Acceptance:

- Desktop does not mount feature surfaces on protocol mismatch.
- No backend capability route/client remains.
- Startup copy matches addendum.

## Phase 2: Project/Workspace Backend Foundations

Goal: server exposes durable project edit and workspace binding operations required by GUI.

- [x] Add project detail/read model for edit page, including project key/name/default workspace and paginated workspaces.
- [x] Add cursor-paginated workspace list sorted by attach time descending with page size support.
- [x] Add project name update mutation using creation-equivalent validation.
- [x] Add default workspace update mutation.
- [x] Add workspace attach mutation behavior that de-dupes within a project and allows same path across projects.
- [x] Add guarded workspace unlink mutation.
- [x] Implement unlink blockers: default workspace, only workspace, active/non-terminal tasks, active sessions/runs, managed owned worktree, missing durable history snapshot.
- [x] Migrate workspace-dependent session/task/worktree references so unlink never cascade-deletes history.
- [x] Preserve history readability through durable workspace path/name snapshots before allowing unlink.
- [x] Emit project/home invalidation events or equivalent subscription facts after name/default/attach/unlink changes.
- [x] Add backend tests for validation, pagination, duplicate policy, unlink blockers, successful unlink, and history readability.

Phase 2 progress note: project edit read model, project name update, default workspace update, workspace list cursor pagination/sort, cross-project same-path workspace attach, guarded unlink, workspace history snapshot migration, task/session history preservation, and project invalidation events are implemented in backend contracts/services/store with focused tests. Legacy unscoped first workspace registration remains convergent while explicit project attach/create can reuse the same path across projects.

Acceptance:

- GUI can implement project edit without local truth or direct DB reads.
- Unsafe unlink states are blocked with structured reasons.
- Terminal historical tasks remain readable after allowed unlink.

## Phase 3: Project Edit GUI

Goal: implement native-feeling project edit page inside fixed shell.

- [x] Add `/projects/$projectId/edit` route.
- [x] Add Home pencil icon on project row title area.
- [x] Add project edit data hooks/adapters/Zod schemas.
- [x] Render read-only project key, editable project name, default workspace selector, and workspace list.
- [x] Validate project name client-side using creation-equivalent rules.
- [x] Use explicit Save for project name/default workspace.
- [x] Discard unsaved name/default changes on Back/navigation.
- [x] Attach workspace through native directory picker.
- [x] De-dupe same-project selected path by focusing existing row/info state.
- [x] Render workspace rows as path + default icon + unlink icon only.
- [x] Implement immediate unlink with simple confirmation modal and structured blocker display.
- [x] Implement infinite scroll over backend cursor pages, no load-more/page buttons.
- [x] Add component tests for save, validation, attach, de-dupe, unlink confirm/blockers, pagination, and Back fallback.

Phase 3 progress note: desktop Project Edit now uses typed server RPC contracts for edit read/update/default/attach/unlink, renders the full fixed-shell `/projects/$projectId/edit` page, adds the Home row pencil entry, validates project names client-side, saves name/default explicitly, attaches workspaces via native picker, de-dupes loaded same-project workspace paths with an info/highlight state, confirms unlink with files/history/active-work copy, displays server blockers, and uses cursor-backed infinite scroll only. Component/API coverage includes route render, validation/save, default save, Home pencil navigation, duplicate attach info, attach mutation, unlink blockers, pagination, and Back fallback.

Acceptance:

- Project edit supports name/default/attach/unlink per addendum.
- No global/root scrolling.
- No deleted files semantics are implied by UI.

## Phase 4: Invalid Workflow And Task Mutation Gating

Goal: show invalid workflows while preventing execution mutations.

- [x] Ensure invalid/default-node-only workflows render board and tasks.
- [x] Allow New Task on invalid workflows, creating Backlog tasks.
- [x] Allow Backlog title/body/source workspace edits where backend permits.
- [x] Allow comments on existing tasks.
- [x] Disable drag/start/run/manual move to workflow nodes/Done when workflow invalid.
- [x] Keep cancel/interrupt/resume governed by server action flags for pre-existing runs.
- [x] Surface clear disabled reasons from server validation.
- [x] Add tests for invalid workflow create/edit/comment vs disabled run/move behavior.

Phase 4 progress note: invalid/default-node-only workflows remain visible and now keep New Task enabled for Backlog creation. Backend task creation permits invalid linked workflows when a Backlog/start node exists, while task start still validates workflow execution and blocks invalid graphs before automation. Existing Backlog task edits and comments stay allowed until automation starts. Board drag/drop execution is disabled before mutation when the selected workflow is invalid, validation messages are rendered from server read-model errors, and Backlog filtering now keeps Backlog-status tasks visible even when they are not startable.

Acceptance:

- Invalid workflows are visible and usable for backlog/comment work.
- Execution actions are blocked before mutation attempts.

## Phase 5: Board Popup, Actions, Pagination, Status

Goal: make board behavior match PRD plus addendum.

- [x] Replace simple workflow `<details>` picker with hover/focus board popup/menu.
- [x] Include PRD board popup actions: Home/back, Inbox entry, New Task, Pin, workflow list, and primary board controls.
- [x] Implement pin as persistent floating island.
- [x] Use open/close scale/opacity/material reveal only; no per-item hover effects.
- [x] Respect reduced-motion and deterministic test mode.
- [x] Expand board read model/client DTOs with target-specific manual move/drop permissions where missing.
- [x] Gate drop-to-Done and manual moves by server permission facts.
- [x] Render card Resume/Interrupt action slot per PRD and server flags.
- [x] For multiple active runs, card opens detail for per-run controls.
- [x] Fix Backlog filtering so non-startable tasks remain visible in server-reported column/status.
- [x] Implement board infinite pagination.
- [x] Improve rich status visualization for required states without hardcoded feature colors.
- [x] Render assignee/ownership where server provides it.
- [x] Add board tests for popup behavior, pin, action gating, invalid workflow, pagination, Done permissions, and status rendering.

Phase 5 progress note: the board now uses a hover/focus popup menu instead of the workflow `<details>` picker, with Home, Inbox, New Task, pin/unpin, and workflow actions inside the glass island. The popup uses scale/opacity reveal through motion tokens, so reduced motion remains deterministic. Board data now loads through cursor-backed infinite query pages and columns fetch the next page on scroll-end without buttons. Backend board cards expose server-owned manual move target node IDs; desktop drop-to-Done/manual move checks those facts before mutation. Backlog filtering keeps Backlog-status tasks visible even when they are not startable. Cards now render status tone, run count, assignee/role where present, and Resume/Interrupt/detail action slots from server flags.

Acceptance:

- Board never hides blocked/non-startable tasks.
- Board does not offer unauthorized transitions.
- Popup matches PRD interaction model without rejected hover effects.

## Phase 6: Task Detail Native Window And Inbox

Goal: rebuild task detail around native child-window UX and current Inbox model.

- [x] Introduce reusable task-detail window launcher through native bridge, reusing native dialog infrastructure.
- [x] Implement one global native task-detail window; opening a different task replaces content.
- [x] Keep browser/tests on in-app fallback.
- [x] Render direct `/tasks/:taskId` desktop route as standalone inline detail page.
- [x] Drop custom remembered in-app dialog size for native task detail.
- [x] Rebuild layout with fixed header/actions + always-visible description.
- [x] Move current blockers and controls into `Inbox` area above tabs.
- [x] Replace contextual resume modal behavior with Inbox focus/reveal behavior.
- [x] Render all unresolved blockers with their own controls.
- [x] Keep Home Inbox controls out of Home rows; rows open detail.
- [x] Complete Home Inbox row structured context where still required by base PRD: task short ID, title, project key/name, workflow, Inbox type, activity time, and action hint.
- [x] Implement `Comments`, `Activity`, `Runs` tabs with default Comments.
- [x] Comments tab: composer, list, edit, delete, count badge.
- [x] Activity tab: compact timeline, no controls, no count badge.
- [x] Runs tab: runs/worktree/session/teleport/telemetry and count badge.
- [x] Implement question keyboard behavior: option selection, arrows, Enter submit, Tab navigation, recommended marker.
- [x] Prevent conflicting question answer modes where backend rejects them.
- [x] Complete approval snapshot rendering: target nodes, required output fields, commentary, graph revision, stale warning.
- [x] Gate interrupt/cancel/resume by server action flags, including per-run controls.
- [x] Confirm Cancel; Interrupt remains immediate.
- [x] Blanket-refetch visible queries when task-detail child window closes after mutations.
- [x] Add tests for native/fallback routing, direct task URL inline mode, Inbox controls, tabs, comments CRUD, activity rendering, approvals/questions, action flags, and cache refresh.

Phase 6 progress note: task detail now opens from Home and Board through a native-bridge task-detail launcher when native task windows are available, with browser/tests falling back to the in-app route/dialog path. The native task detail window uses one global label and sends a replace-content event when another task is opened. Direct `/tasks/$taskId` renders a standalone inline page in the desktop shell instead of modal chrome. The shared detail surface now keeps header and description visible, shows current blockers plus resume/interrupt/cancel controls in an `Inbox` area, renders all question/approval blockers independently, and exposes Comments, Activity, and Runs tabs with default Comments. Question answers now avoid sending option and freeform modes together; approval snapshots include target nodes/commentary/output values/revision/stale warning. Task detail mutations invalidate task/activity plus visible Home Inbox/board/project query families, and native child-window mutations notify the main window for blanket refresh.

Acceptance:

- Task detail matches addendum layout.
- All Inbox blockers are visible and actionable from detail.
- Home/Board update after detail mutations.

## Phase 7: Reconnect, Drafts, Relaunch

Goal: make connection loss safe and deterministic.

- [x] Make disconnected global status non-dismissible until reconnect.
- [x] Disable every mutating action while disconnected, including native child-window flows.
- [x] Preserve in-memory drafts for new task, comments, task body/title, and project edit text while window remains open.
- [x] Avoid query refresh/remount patterns that drop drafts after reconnect.
- [x] On reconnect, refresh server state and keep user drafts for manual submit.
- [x] Ensure no offline mutation queue/replay exists.
- [x] Persist last valid project/workflow route.
- [x] Restore last valid route on relaunch; fallback Home if invalid/unavailable.
- [x] Add tests for disconnect gating, draft preservation, reconnect refresh, and route restoration.

Phase 7 progress note: disconnected status notices are now non-dismissible and clear only after reconnect. Board card resume/interrupt mutations are disabled while disconnected, matching existing New Task, Project Edit, and Task Detail gating. Reconnect now invalidates visible project, Inbox, board, project edit, workspace, task, activity, and pending-ask query families without queuing or replaying mutations. New Task, task edit, comments, and Project Edit drafts remain local while destinations stay mounted because reconnect refresh no longer remounts those forms or resets open dialog values. The desktop shell persists the last `/projects/$projectId?workflowId=...` board route in browser storage and restores it once per app session on Home startup, falling back to Home when no valid stored route exists or storage is unavailable.

Acceptance:

- Connection loss cannot submit mutations.
- Drafts survive reconnect while window remains open.
- Relaunch restores last valid project/workflow route.

## Phase 8: Visual, Teleport, And Native Capability Cleanup

Goal: close remaining P1 polish gaps while keeping terminal teleport usable and native capability flags truthful.

- [x] Ensure teleport uses `builder --continue <session-id>`.
- [x] Keep backend returning identifiers only.
- [x] Show plain-text backend unavailable reason when no teleport target exists.
- [x] Show plain-text native/local executable failure when local launch fails.
- [x] Decide and implement local Builder executable override only if existing config architecture supports it without adding settings UI; otherwise keep plain failure.
- [x] Make native bridge capability flags match implementations.
- [x] Keep browser fallback disabled states explicit.
- [x] Implement theme config override if supported by existing Builder GUI config; otherwise document config gap before coding UI.
- [x] Complete reduced-motion-aware user-visible transitions required outside board popup and task detail.
- [x] Move remaining hardcoded user-facing strings into locale files.
- [x] Add tests for target available/unavailable and native bridge failure handling where practical.

Phase 8 progress note: teleport still uses server-owned target discovery and local native launch with `builder --continue <session-id>`. The task detail Runs tab renders server unavailable reasons and native launch failures as plain text. Existing config did not include a GUI-safe local Builder executable path override, so desktop keeps `builder` from `PATH` and surfaces local executable errors. Native capability flags now match bridge implementations: Tauri clipboard uses the clipboard plugin, browser clipboard remains disabled with explicit failure, Tauri task detail has the required `get_all_windows` and event emit permissions for singleton retargeting, and unsupported native integrations stay false. A regression test now checks default Tauri permissions for bridge event/window APIs. Native context now exposes `theme=auto|light|dark` from config/env, desktop applies it through a root data attribute, and no settings UI was added. Shared state/status surfaces use reduced-motion-aware reveal/spinner behavior. Startup copy moved into locale resources; remaining hardcoded strings are intentionally unlocalized developer/system errors in transport, bridge, provider invariant, native listener diagnostic, bootstrap context, and root mount boundaries. Phase 9 proof capture should include forced light and forced dark theme screenshots for the new override.

Acceptance:

- Teleport proof can show available and failure states.
- Native capability flags do not claim unsupported features.

## Phase 9: Screenshot Proof Harness

Goal: produce repeatable acceptance evidence without touching current agent service.

- [x] Create `.builder/proofs/gui-gap-closure/`.
- [x] Add proof manifest describing fixture setup, service endpoint, screenshots, and acceptance mapping.
- [x] Launch isolated secondary Builder service on alternate port and persistence root.
- [x] Seed fixture projects/workflows/tasks through Builder CLI/API, not direct DB writes.
- [x] Capture Home/project list screenshot.
- [x] Capture project edit screenshot including workspace list and unlink blocker/confirmation.
- [x] Capture valid workflow board screenshot.
- [x] Capture invalid workflow board screenshot with disabled execution controls and enabled Backlog/comment affordances.
- [x] Capture task detail screenshot with header, description, Inbox, and tabs.
- [x] Capture question/approval Inbox controls.
- [x] Capture reconnect/draft behavior.
- [x] Capture teleport available state and failure state.
- [x] Store screenshots and manifest under `.builder/proofs/gui-gap-closure/`.

Phase 9 progress note: proof artifacts live under `.builder/proofs/gui-gap-closure/`. The harness used an isolated `./bin/builder serve` process on port 53182 with persistence root `/tmp/builder-gui-proof.bGwmNl/persistence`, plus temporary git workspaces under `/tmp/builder-gui-proof.bGwmNl/workspaces`. Fixtures were seeded with Builder CLI/API commands only. Screenshots cover Home in forced dark/light themes, Project Edit workspace management, valid workflow board, invalid workflow board, task detail layout, and a task detail Inbox runtime blocker. macOS Accessibility blocked scripted native tab clicks, so question/approval controls, reconnect/draft behavior, and teleport failure rendering are mapped to focused desktop tests in the manifest; teleport available state is backed by a live RPC target response from the isolated proof service. During proof capture, desktop task-detail parsing exposed null empty slices from Go JSON; the schema now normalizes null arrays and has a client regression test.

Acceptance:

- Every MVP acceptance case has a screenshot or manifest entry.
- Proof harness does not restart or mutate current agent service.

## Review And Build Gate

- [ ] Run focused backend tests for changed server packages during each backend phase.
- [ ] Run focused desktop tests for changed GUI flows during each GUI phase.
- [ ] Run `./scripts/test.sh` before final handoff.
- [ ] Run `./scripts/build.sh --output ./bin/builder` before final handoff after code changes.
- [ ] Use code review subagent after implementation for diff review against addendum and plan.
- [ ] Document any unrelated failing tests with command output and likely owner.
