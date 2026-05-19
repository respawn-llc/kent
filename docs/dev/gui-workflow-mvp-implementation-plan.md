# GUI Workflow MVP Implementation Plan

Status: executable frontend implementation plan.

Date: 2026-05-16.

Product spec: `docs/dev/gui-workflow-mvp-prd.md`.

Stack spec: `docs/dev/gui-client-stack.md`.

Foundation checklist: `docs/dev/gui-foundation-checklist.md`.

Backend slice contract: `docs/dev/gui-workflow-backend-slice-plan.md`.

## Branch Baselines

At spec time, `gui-mvp` was rebased on `gui-bootstrap` through `d3b41eb4`.

At implementation start, `gui-mvp` was rebased on `gui-bootstrap` through `9ae3c8a1`.

Already present:

- `apps/desktop` Tauri/React/TypeScript skeleton.
- pnpm workspace, dependency policy, lint/typecheck/test scripts, Vitest, and Tauri config.
- Native bridge package boundary.
- GUI stack/foundation/use-case/checklist docs.
- Backend GUI workflow slices 0-5 for readiness, Home/project/workspace APIs, workflow board, Backlog/source workspace editing, actions/attention/questions/approvals, task detail/activity/comments/teleport.

Not yet present:

- Product app shell beyond foundation placeholder screen.
- Final UI kit.
- GUI API adapter layer.
- Real startup/readiness connection flow.
- Home, board, task detail, attention, comments, or teleport UI.

## Branch Readiness Anchors

Use these commits as branch-state anchors when resuming work. This list anchors prerequisite foundation/backend/spec commits. Do not chase every parent-branch review commit while planning; refresh this baseline only at the intended implementation rebases: once immediately before starting GUI implementation, and once after implementation before final verification.

- `cfbe5813 feat: bootstrap gui client workspace` - adds `apps/` Tauri/React/TypeScript skeleton, pnpm workspace, dependency policy, and native bridge boundary.
- `3fe29b56 feat: add gui server readiness APIs` - backend Slice 0 readiness/capabilities.
- `ed483ab7 feat: add gui project home APIs` - backend Slice 1 Home/project admin/project key/workspaces.
- `54143675 feat: add gui workflow board APIs` - backend Slice 2 workflow picker, selected board, groups, live updates.
- `036f3865 feat: add gui task source workspace APIs` - backend Slice 3 source workspace and Backlog editing.
- `df8f806f feat: add gui workflow attention APIs` - backend Slice 4 actions, attention inbox, questions, approvals.
- `493fe34d feat: add gui workflow task detail APIs` - backend Slice 5 task detail, activity feed, comments, teleport target.
- `485e5120 fix: stabilize gui task activity reads` - cursor activity paging, explicit bad-run teleport reason, batched activity read helpers.
- `fa8fe7f7 docs: clarify gui task activity cursor paging` - final GUI backend docs clarification before `gui-mvp` rebase.
- `ae9e627a fix: publish gui workflow refresh events` - workflow refresh invalidation and Home pagination fixes.
- `5a124fe0 fix: page gui task activity in storage` - storage-level task activity paging and question answer validation fixes.
- `b983a087 fix: harden gui workflow read models` - pointer-safe task detail/activity DTOs, board count fixes, and read-model hardening.
- `d3b41eb4 fix: surface workflow event persistence failures` - live-update event persistence and workflow mutation hardening.
- `2a63e501 fix: tighten gui policy checks` - GUI policy/lint tightening that implementation must satisfy.
- `9ae3c8a1 fix: enforce workflow node group ownership` - workflow node-group ownership hardening before GUI integration.
- `fac3f9ee chore: bump version to 1.3.0` - rebased version bump on top of `gui-bootstrap`.
- `fb85ec49 docs: finish gui workflow mvp spec` - final MVP PRD and frontend implementation plan.
- `e1093a69 docs: sync gui teleport planning anchors` - teleport CLI help sync, superseded banners, and initial branch anchors.
- `9933d2e6 docs: address gui mvp spec review` - external-review follow-up for branch anchors, source doc drift, and verification wording.
- `9a40e1eb docs: update gui mvp branch anchors` - branch anchor update after `b983a087`.

## Implementation Rules

- Build vertical slices. Each slice must have real data integration or explicit fixture-only boundary.
- Keep GUI thin. Server owns workflow/task/runtime truth.
- Feature code must not import Tauri APIs, raw server DTOs, raw transport, or `react-markdown`.
- Use paginated read models for all unbounded collections.
- No mutation replay after reconnect.
- No full `events.jsonl` reads.
- No Markdown tables in planning docs.
- Keep GUI/backend planning out of `docs/dev/async-workflow-implementation-checklist.md`.
- Use tests for business logic, adapters, route parsing, forms, and critical component interactions.

## Slice F0: Desktop Shell And UI Foundation

Goal: app shell can launch, render deterministic states, and host future workflow screens.

Existing foundation from bootstrap:

- [x] pnpm workspace under `apps/`.
- [x] Vite/React/TypeScript desktop app under `apps/desktop`.
- [x] Tauri v2 shell under `apps/desktop/src-tauri`.
- [x] Native bridge package boundary.
- [x] Dependency policy, lockfile, and package scripts.
- [x] Strict TypeScript, ESLint, Prettier, Vitest, and Testing Library.

Remaining work:

- [ ] Replace placeholder screen with startup-capable app shell.
- [ ] Add app-local UI kit with required base widgets.
- [ ] Add theme tokens for light/dark, typography, spacing, radius, shadow, and motion.
- [ ] Add i18n setup with static English locale files.
- [ ] Add `MarkdownText` shared component and import boundary rules.
- [ ] Add destination/routing infrastructure and dialog lifecycle primitives.
- [ ] Add Settings/diagnostics destination shell and route slot.
- [ ] Add relaunch restoration for last project board with safe Home fallback.
- [ ] Add global status/notification surface.
- [ ] Add local GUI log abstraction with redaction and 10 MB cap.
- [ ] Add reduced-motion/visual-determinism test mode.

Completion criteria:

- `pnpm install --frozen-lockfile`, `pnpm lint`, `pnpm typecheck`, and `pnpm test` pass under `apps/`.
- App renders startup shell in component tests.
- Feature code import boundaries fail tests/lint when violated.
- Native/Tauri APIs are unreachable from feature components except through bridge.

## Slice F1: API Client And Connection Model

Goal: GUI can call Builder server contracts safely and react to reconnect.

Checklist:

- [ ] Implement JSON-RPC client with typed request IDs and protocol errors.
- [ ] Implement WebSocket transport with capped reconnect/backoff.
- [ ] Reject pending requests on disconnect.
- [ ] Add bounded buffering policy.
- [ ] Add React Query client and cache invalidation conventions.
- [ ] Add Zod DTO adapter layer for server responses.
- [ ] Add typed error model for validation, transport loss, auth/readiness, capability mismatch, and runtime/action failure.
- [ ] Add subscription helper for invalidation events.
- [ ] Add fake transport/server fixtures for deterministic tests.
- [ ] Add contract drift checks against Go DTO/schema fixtures.

Completion criteria:

- Adapter tests prove raw DTOs do not reach feature components.
- Reconnect test proves no mutation replay and active queries refetch after reconnect.
- Feature code can consume typed read-model hooks without raw transport imports.

## Slice F2: Startup Readiness And Capabilities

Backend dependency: backend Slice 0, already present on this branch.

Goal: desktop attaches to configured server and shows safe startup states.

Checklist:

- [ ] Load GUI/server endpoint from Builder config/default host and port.
- [ ] Call readiness and capability endpoints.
- [ ] Render splash/loading during config/connectivity checks.
- [ ] Render startup error surface for all PRD failure causes.
- [ ] Show `builder service install` instruction when server is unavailable.
- [ ] Expose capability registry to feature gating.
- [ ] Add global connection-loss notification/status.
- [ ] Keep read-only cached state visible only when safe.
- [ ] Disable mutations while disconnected.

Completion criteria:

- Unit/component tests cover readiness success, server unavailable, auth not ready, protocol mismatch, capability mismatch, and unknown failure.
- Capability-unavailable UI shows visible explanation and disables dependent actions.

## Slice F3: Home, Projects, And Workspaces

Backend dependency: backend Slice 1 and attention data from Slice 4, already present on this branch.

Goal: user can pick/create project and understand project/workspace state.

Checklist:

- [ ] Build Home route with header, project list pane, and attention inbox pane.
- [ ] Implement paginated project list with latest-activity order.
- [ ] Render project key/name, primary workspace, updated time, and chips.
- [ ] Implement project open to default workflow board.
- [ ] Implement New Project with OS directory picker through native bridge.
- [ ] Implement Add Workspace with OS directory picker through native bridge.
- [ ] Implement path-resolution/binding flow for bound vs unbound workspace.
- [ ] Implement project creation form with name and project key.
- [ ] Render project setup blockers and no-valid-workflow blocker.
- [ ] Add local draft preservation for forms during disconnect/reconnect.

Completion criteria:

- Component tests cover list loading/empty/error/pagination states.
- Project key validation errors render from server/adapter error model.
- Bound workspace opens existing project; unbound workspace opens create flow.

## Slice F4: Workflow Board

Backend dependency: backend Slice 2, already present on this branch.

Goal: user can view selected project workflow as board and watch changes.

Checklist:

- [ ] Build project board route.
- [ ] Implement board read-model hook for selected workflow.
- [ ] Implement workflow picker in hover-expandable popup/menu.
- [ ] Implement Backlog left, workflow-node columns, group islands, and Done right.
- [ ] Render board cards with task ID, title, status, and workspace chip.
- [ ] Render Done as a fixed-right paginated node column.
- [ ] Subscribe to project/workflow invalidation events.
- [ ] Refresh board on reconnect.
- [ ] Open task detail dialog on card click.
- [ ] Render no-valid-workflow blocker and disable New Task.

Completion criteria:

- Board renders grouped and ungrouped workflow fixtures.
- Live invalidation test updates board through cache refetch.
- Reconnect test triggers full board refresh.

## Slice F5: Task Create, Backlog Edit, And Drag-To-Start

Backend dependency: backend Slice 3, already present on this branch.

Goal: user can create/edit Backlog tasks and start automation.

Checklist:

- [ ] Build New Task form/modal/surface from board action.
- [ ] Implement required title and optional body/details.
- [ ] Implement source workspace selector and single-workspace disabled chip.
- [ ] Default workspace from current context or project default.
- [ ] Create task in Backlog for currently selected workflow.
- [ ] Allow title/body/source workspace edit while task is Backlog.
- [ ] Disable Backlog edits after start/cancel.
- [ ] Implement drag from Backlog to first active node as immediate start.
- [ ] Render server validation errors.
- [ ] Refresh board/detail after create/edit/start.

Completion criteria:

- Form tests cover validation, workspace defaults, one-workspace display, and server error rendering.
- Drag-to-start test proves no confirmation path and correct mutation call.
- Started task no longer exposes Backlog edit controls.

## Slice F6: Attention, Questions, Approvals, And Actions

Backend dependency: backend Slice 4, already present on this branch.

Goal: user can resolve workflow blockers and control running tasks.

Checklist:

- [ ] Integrate Home attention inbox with pagination and newest-first order.
- [ ] Deep-link attention row to standalone task detail over Home.
- [ ] Render unresolved attention in task detail.
- [ ] Implement Resume action and contextual resume modal.
- [ ] Implement question answer UI with options, freeform/commentary, recommended marker, arrows/Enter, and Tab focus.
- [ ] Implement approval UI with stored snapshot fields and Approve-only action.
- [ ] Implement task Cancel with confirmation and no reason field.
- [ ] Implement task/card Interrupt when exactly one run is interruptible.
- [ ] Implement per-run Interrupt controls in detail for multi-run tasks.
- [ ] Preserve standalone detail after attention resolution and update Home inbox in background.

Completion criteria:

- Tests cover question answer modes, approval snapshot rendering, Approve-only behavior, Cancel confirmation, no Interrupt confirmation, and multi-run detail controls.
- Mutating controls are disabled while disconnected.

## Slice F7: Task Detail, Activity, Comments, And Teleport

Backend dependency: backend Slice 5, already present on this branch.

Goal: user can inspect task state, comment/history, and attach to terminal TUI session.

Checklist:

- [ ] Expand task detail dialog with identity, body, project/workflow, source workspace, status, actions, worktree, runs/sessions, and unresolved attention.
- [ ] Implement fixed header/content plus global scroll.
- [ ] Implement remembered user-resized dialog size.
- [ ] Implement full comment create/edit/delete.
- [ ] Implement newest-first paginated activity feed.
- [ ] Render comments and transitions from structured activity payloads.
- [ ] Render hidden/unavailable fields per PRD missing-field policy.
- [ ] Implement teleport target request.
- [ ] Implement native bridge default-terminal launch of `builder --continue <session-id>`.
- [ ] Render unavailable teleport reason and missing local Builder executable error.

Completion criteria:

- Activity feed pagination test handles inserted newer item without duplicate/skip.
- Comment CRUD updates detail/feed cache.
- Teleport command test proves interactive `builder --continue <session-id>` is used, not headless `builder run --continue`.
- Native bridge failure renders plain text cause.

## Cross-Cutting Verification

Run before handoff after code changes:

- [ ] `./scripts/test.sh` with no package args for full Go plus frontend test coverage, or targeted Go package args for Go-only checks.
- [ ] `./scripts/build.sh --output ./bin/builder`.
- [ ] GUI lint/typecheck/test/build commands under `apps/`.
- [ ] Tauri native check once toolchain exists: `cd apps/desktop/src-tauri && cargo check --locked`.
- [ ] `git diff --check`.
- [ ] Iterations of code review subagents with code_review role are no longer finding any issues to report.

Manual QA target for MVP:

- [ ] Start Builder service.
- [ ] Open desktop app.
- [ ] Create/open project.
- [ ] Open default workflow board.
- [ ] Create Backlog task.
- [ ] Drag task to first active node.
- [ ] Watch task status move.
- [ ] Resolve question/approval if encountered.
- [ ] Open task detail.
- [ ] Add/edit/delete comment.
- [ ] Inspect activity.
- [ ] Interrupt/resume/cancel where applicable.
- [ ] Teleport to terminal TUI session.
- [ ] Disconnect/reconnect server and verify refresh/draft policy.

## Handoff Notes

If implementation spans handoffs, future agents should read:

- `docs/dev/gui-workflow-mvp-prd.md`.
- `docs/dev/gui-client-stack.md`.
- This implementation plan.
- `docs/dev/gui-foundation-checklist.md`.
- `docs/dev/gui-workflow-backend-slice-plan.md`.
- `docs/dev/decisions.md`.
- `apps/AGENTS.md`.
- Root `AGENTS.md`.

They should first inspect `git status --short`, confirm current frontend/backend state, then continue from first unchecked slice item.
