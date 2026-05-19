# GUI Workflow MVP Implementation Progress

Status: F0-F7 implemented in dirty worktree; cross-cutting verification, post-implementation rebase, and code-review-agent gate passed.

Goal: implement `docs/dev/gui-workflow-mvp-implementation-plan.md` slices F0-F7 according to `docs/dev/gui-workflow-mvp-prd.md`, then pass cross-cutting verification and code-review-agent review.

## Current Baseline

- Branch: `gui-mvp`.
- Pre-implementation rebase completed onto `gui-bootstrap` through `9ae3c8a1 fix: enforce workflow node group ownership`.
- Parent branch is expected to keep moving during review. Do not chase every parent commit during implementation.
- Rebase policy from Nikita: rebase once before implementation, implement, then rebase once after implementation before final verification.
- Current implementation plan already includes review-agent verification as a cross-cutting gate.

## Completed Before Implementation

- [x] Planning skill preparation/recon/design/code planning completed.
- [x] Final PRD written: `docs/dev/gui-workflow-mvp-prd.md`.
- [x] Implementation plan written: `docs/dev/gui-workflow-mvp-implementation-plan.md`.
- [x] Old source checklist/inventory docs marked superseded.
- [x] External review agents reviewed the plan/spec.
- [x] Review findings were addressed.
- [x] Teleport command locked as interactive `builder --continue <session-id>`.
- [x] Backend GUI slices F0 dependencies are present on branch via `gui-bootstrap` baseline.
- [x] Pre-implementation rebase performed.

## Slice Status

- [x] F0: Desktop shell and UI foundation.
- [x] F1: API client and connection model.
- [x] F2: Startup readiness and capabilities.
- [x] F3: Home, projects, and workspaces.
- [x] F4: Workflow board.
- [x] F5: Task create, Backlog edit, and drag-to-start.
- [x] F6: Attention, questions, approvals, and actions.
- [x] F7: Task detail, activity, comments, and teleport.
- [x] Cross-cutting verification.
- [x] Code-review-agent review passes with no findings.

## Next Step

Complete the Builder goal after final status handoff.

## Implementation Notes

- Desktop app now has service-backed app shell, providers, typed routes, i18n, UI primitives, Markdown wrapper, global status surface, and local GUI logging.
- GUI API boundary now uses typed JSON-RPC WebSocket transport, Zod response adapters, React Query hooks, fake transport tests, reconnect invalidation, pending-request rejection on disconnect, and subscription race coverage.
- Home supports project list, attention inbox, workspace picker/plan, project creation, and workspace attach.
- Board supports workflow picker, group-aware Kanban sections, Backlog/Done columns, live invalidation subscription, card detail routing, and drag-to-first-active-node start.
- Task flows support Backlog task create/edit/source workspace selection, disconnected mutation gating, questions with pending-ask suggestions, approve-only approvals, cancel confirmation, interrupt/resume controls, comments, activity, and teleport.
- Tauri bridge now owns directory picker, external links, terminal launch, and bounded local GUI log append. Terminal launch opens interactive `builder --continue <session-id>`, verified by Rust unit test.

## Verification Notes

Local verification passed after F0-F7 implementation and again after post-implementation rebase onto `origin/gui-bootstrap` `dc16d98e fix: align interrupted attention status kind`:

- `pnpm --dir apps lint`.
- `pnpm --dir apps typecheck`.
- `pnpm --dir apps test`.
- `pnpm --dir apps build` (Vite emitted chunk-size warning only).
- `cd apps/desktop/src-tauri && cargo check --locked`.
- `cd apps/desktop/src-tauri && cargo test --locked`.

Code-review-agent gate:

- Initial review found endpoint/config, persistence-root, native executable, Backlog edit, subscription, timeout, and startup error-class blockers.
- Fixes were applied and verification was rerun after each fix round.
- Final two independent code-review-agent runs reported no blocking findings.

## Caveat Decisions

- Desktop Vite chunk-size warning was intentionally silenced with a desktop-only `chunkSizeWarningLimit` of 2048 KiB. Tauri desktop loads assets from local disk; 2 MiB is the review threshold for desktop-local delivery, not current-bundle-size tuning.
- jsdom `window.scrollTo` test noise is silenced in the desktop test setup because TanStack Router calls browser scroll restoration and jsdom does not implement it.
- Desktop endpoint is config-driven: `server_host`/`server_port` from Builder config or `BUILDER_SERVER_HOST`/`BUILDER_SERVER_PORT` may point at local or remote Builder servers. Tauri CSP allows WebSocket endpoints via `ws:` to match runtime configuration.
- Directory picking uses Tauri's cross-platform dialog plugin. Terminal teleport remains a macOS-only shim for MVP and is disabled through native capabilities on non-macOS platforms.
