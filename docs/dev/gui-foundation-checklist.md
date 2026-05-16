# GUI Foundation Checklist

Status: working checklist for Builder's Tauri/React desktop foundation.

Date: 2026-05-15.

This checklist is for decisions and setup needed before implementing product features in the desktop app. It assumes one monorepo, a GUI codebase that can grow to 200-500k LoC, and eventual feature parity with the TUI.

## Locked Baseline

- [x] Keep GUI in this repository, not a separate GitHub repo.
- [x] Use Tauri desktop shell.
- [x] Use React and TypeScript for UI.
- [x] Keep Builder's Go server authoritative for runtime, worktrees, DB, orchestration, validation, approvals, asks, workflow state, and persistence.
- [x] Treat GUI as a remote-control client over Builder API/read-model contracts.
- [x] Use top-level `apps/` pnpm workspace.
- [x] Put desktop app in `apps/desktop`.
- [x] Put desktop-only shared packages in `apps/desktop/packages/*`.
- [x] Reserve `apps/shared/*` for packages with more than one real GUI-app consumer.
- [x] Use pnpm rather than Bun for the foundation.
- [x] Use final Tauri identifier `sh.kent`.
- [x] First workflow MVP attaches to an existing Builder server and does not start the server automatically.
- [x] If the server is not running, first workflow MVP shows instructions to run `builder service install`.
- [x] First workflow MVP uses island-style floating UI over native glass/acrylic-style window material.
- [x] Shared UI/theme source of truth starts in `apps/shared/ui/theme`.

## Project Skeleton

- [x] Add pnpm workspace metadata under `apps/`.
- [x] Add Vite/React/TypeScript desktop app skeleton.
- [x] Add Tauri v2 `src-tauri` skeleton.
- [x] Add `NativeBridge` package boundary before feature components need native APIs.
- [ ] Add final app icons, bundle metadata, copyright, category, and product naming after rebrand direction is clear.
- [x] Decide whether `Builder`, `Kent`, or another name appears in app chrome for MVP: use `Kent` while rebrand is underway.
- [x] Decide design-system/theme package start point: shared theme starts in `apps/shared/ui/theme`.
- [x] Decide component library start point: app-local `apps/desktop/src/ui` for MVP, with clean imports and extraction path when a second GUI app exists.

## Phase 0 Product Shell

- [ ] Add splash/loading screen for app startup, config loading, and initial server connectivity.
- [ ] Add startup safety surface that explains onboarding incomplete, missing auth, server unavailable, missing/invalid config, readiness failure, protocol mismatch, capability mismatch, and unknown startup failures as plain text.
- [x] Decide startup failure detail level: MVP UI shows human-readable summary and next action only; deeper diagnostics go to the local GUI log file.
- [ ] Add server-not-running instructions that tell the user to run `builder service install`.
- [ ] Add global reusable notification/status infrastructure for startup failures, API errors, validation blockers, connection issues, warnings, update notifications, and background action results.
- [ ] Add project-first navigation shell with stable top-level route slots for Home/project list, project board, task detail, contextual resume modal, settings, and diagnostics.
- [x] Decide initial landing/navigation model: project-first. The app opens to project picker/recent projects, and board/task routes are scoped under selected project.
- [x] Decide app startup destination: Home/project list/inbox full-screen destination.
- [x] Decide Home layout: header with runtime/setup info, left paginated project list subpane, right paginated global attention inbox subpane.
- [x] Decide relaunch behavior: restore last project board when possible and allow navigation back to Home/project list.
- [x] Decide initial task detail surface: dialog first, with right-side pane as later/testable variant.
- [x] Decide initial board sidebar model: no persistent sidebar; preserve Kanban width.
- [x] Decide workflow picker home: hover-expandable non-modal board control/popup, to design later.
- [ ] Add navigation stack and typed destination lifecycle for screens/dialogs so view-models, server subscriptions, native processes, and cleanup are tied to destination open/close.
- [ ] Add dialog infrastructure for task details and future modal flows.
- [ ] Add shared rendering primitives for empty, loading, success, warning, and error states.
- [ ] Add local app config loader for GUI settings such as server endpoint and feature flags.
- [ ] Add server connection state model for connected, disconnected, retrying, auth/config error, migration required, and incompatible version.
- [ ] Add local GUI log file under the Builder persistence root for startup, connection, API, and native bridge failures; cap it at 10 MB for MVP and redact auth headers, tokens, env values, and request bodies by default. No external reporting in MVP.
- [ ] Add capability registry model so unavailable backend features disable UI actions with explanations.
- [ ] Add mock/fake transport fixtures so shell and early screens can be developed before workflow APIs land.
- [ ] Add connection-loss state that disables mutating actions, shows last cached state at most, and keeps an indefinite snackbar/strip/notice visible until reconnect.
- [ ] Add draft persistence across disconnect/reconnect for active form/comment edits; after reconnect refresh, user save overwrites remote state regardless of remote freshness.
- [x] Decide global status messaging direction: reusable global notification/status surface is mandatory; exact strip/snackbar/banner treatment is not locked.
- [x] Decide desktop server endpoint selection for MVP: use Builder config/default host and port only.
- [x] If configured/default endpoint is unreachable, show startup error and `builder service install` instructions; endpoint editing is deferred.

## TypeScript And Frontend Standards

- [x] Enable strict TypeScript.
- [x] Add ESLint config for app code.
- [x] Add Prettier config.
- [x] Add Vitest and Testing Library.
- [x] Decide import boundary rules: feature code cannot import Tauri APIs, `react-markdown`, raw transport, or raw server DTOs; use native bridge, `MarkdownText`, API adapters, and UI kit.
- [ ] Enforce import boundary rules with ESLint as package graph grows.
- [x] Decide state-management stack for server state and local UI state: `@tanstack/react-query` plus React local state for MVP.
- [x] Defer `zustand` and Redux/RTK Query from MVP.
- [x] Decide app routing approach: `@tanstack/react-router`, boxed behind Builder destination helpers.
- [x] Validate route/search params with Zod at the boundary.
- [x] Decide form and validation libraries: React Hook Form plus `@hookform/resolvers` and Zod.
- [x] Defer TanStack Form until workflow editor forms become genuinely complex.
- [x] Complete stack-advisor review for state management, routing, forms, and WebSocket transport before locking frontend library decisions Nikita does not want to choose by taste.
- [x] Decide date/time rendering: use native `Intl`, not Temporal, for MVP.
- [x] Decide command-palette direction: defer from MVP; use `cmdk` when command palette becomes product-relevant.
- [x] Decide keyboard-shortcut and hotkey conventions: exact shortcut map is deferred; use platform-native modifiers, do not capture global shortcuts while typing, expose shortcuts through visible menu/tooltip discovery, and avoid keyboard-only hidden paths.
- [x] Decide design-token strategy and theming model: CSS custom properties for semantic tokens plus CSS Modules/plain CSS in components; no Tailwind or vanilla-extract for MVP.
- [x] Choose Markdown stack: `react-markdown`, `remark-gfm`, and `rehype-sanitize` behind one shared `MarkdownText` component.
- [x] Do not render raw HTML in Markdown for MVP; use `skipHtml` or equivalent, do not add `rehype-raw`, and sanitize output anyway.
- [x] Feature components must not import `react-markdown` directly.
- [x] Custom-render Markdown links through a safe-protocol allowlist and external-link bridge helper with `rel="noreferrer"`.
- [x] Custom-render Markdown `code`/`pre` with theme-token styling; syntax highlighting is deferred.
- [x] Markdown component internals do not own translated copy except i18n-provided aria labels.
- [ ] Lock MVP brand direction, palette tokens, font tokens, and base design-system widgets in code.
- [x] Decide MVP fonts: Montserrat for main UI and Monaspace Neon for monospace values.
- [x] Decide theme modes: light and dark themes with config override when set and auto/system otherwise.
- [x] Decide palette source: derive initial colors from current TUI and docs-site palette; keep token architecture easy to change.
- [x] Decide motion principle: animate every user-visible transition unless reduced motion is enabled; use shared element transitions where practical.
- [x] Decide first workflow MVP accessibility baseline: no accessibility/keyboard-complete pass required.
- [x] Decide first workflow MVP localization baseline: i18n-ready UI strings, no hardcoded user-facing strings in components.
- [x] Decide first workflow MVP i18n stack direction: `i18next`/`react-i18next`, static English locale files, no runtime language switch, no non-English locales initially.
- [x] Decide first workflow MVP icon pack: Lucide.
- [x] Decide later accessibility baseline and test tooling: keep accessibility best-effort until after v1; no stronger post-MVP release bar is locked now.

## Builder API Client

- [x] Decide hand-written vs generated TypeScript JSON-RPC client: start with hand-written typed JSON-RPC/WebSocket client plus DTO adapter layer and contract tests; generated client can come later if contracts stabilize.
- [x] Do not add tRPC for MVP protocol typing; Go server plus Builder JSON-RPC contracts fit typed adapters, Zod boundary validation, and contract tests better.
- [ ] Define DTO adapter layer so `shared/serverapi` churn does not spread across UI.
- [x] Decide WebSocket transport implementation/library: native browser `WebSocket` plus an in-repo JSON-RPC transport/reconnect layer.
- [ ] Define native WebSocket reconnect/backoff, auth-readiness behavior, pending-request rejection, bounded buffering policy, and typed protocol errors.
- [x] Do not replay mutations after reconnect; refetch, resubscribe, and let the user issue a new command.
- [ ] Define error model for server validation, transport loss, auth required, and runtime failure.
- [ ] Treat real backend workflow endpoints/read models as a prerequisite for workflow UI integration beyond shell/prototype.
- [ ] Plan backend/GUI work as vertical slices: backend plumbing/read models first, then GUI integration for that slice.
- [x] Lock MVP vertical slice order: connectivity/capabilities plus Home/project admin/key/workspaces; workflow picker plus project-wide board/groups/live updates; task create/backlog/workspace default; drag-to-start/interrupt/cancel/resume/inbox; detail feed/comments/teleport.
- [ ] Define Home data adapter for paginated project list plus global attention inbox sorted by newest activity first.
- [ ] Define project/workspace path-resolution adapter for New Project and Add Workspace flows.
- [ ] Define project workspace-list adapter for task main workspace dropdown, including current/opened workspace context and project default/main workspace fallback.
- [ ] Define project create/update adapter for editable project name and project key; backend MVP must expose project-key mutation in create flow.
- [ ] Define task create/edit adapter for selected workspace before start; current task schema needs backend support because task workspace is only inferred from managed worktree after start.
- [ ] Define project-wide board read-model adapter for selected project/workflow, including workspace metadata per task card when multiple workspaces exist.
- [ ] Define group-aware board rendering adapter contract so workflow groups can render without flattening or blocking grouped workflows; first UX pass is implementation-led and should remain flexible for Nikita QA changes.
- [ ] Define board action adapter for drag/drop backlog-to-first-active-node start.
- [ ] Define board action adapter for dragging tasks to expandable Done drop target when backend permits manual transition.
- [ ] Define task action adapter for interrupt, cancel, and resume.
- [ ] Define task resume adapter capable of surfacing contextual question-answer and approval flows.
- [ ] Define task comment adapter for create, edit, delete, and list.
- [ ] Define board live-update adapter: initial snapshot plus WebSocket updates, with full refresh on reconnect.
- [ ] Define temporary teleport adapter that opens user's default terminal on the client machine and runs local Builder TUI attach flow. This is a placeholder until built-in GUI chat lands after MVP.
- [ ] Add contract drift checks against Go DTOs/service contracts.
- [ ] Add fake server or mock transport for deterministic GUI tests.

## Native Bridge

- [x] Define initial `NativeBridge` package boundary.
- [ ] Add Tauri implementation for clipboard.
- [ ] Add Tauri implementation for notifications.
- [ ] Add Tauri implementation for opening OS-native terminal/session teleport commands.
- [ ] Add tray/menu-bar abstraction.
- [ ] Add app/window menu abstraction.
- [ ] Add updater abstraction.
- [ ] Add window controls abstraction.
- [ ] Add macOS vibrancy/blur abstraction.
- [ ] Make macOS vibrancy/blur mandatory for first workflow MVP.
- [x] Decide window chrome direction: native border, native shape, and native traffic-light/window controls integrated into app chrome.
- [x] Decide browser/no-op fallback behavior for every capability: use capability registry gates; browser implementation uses real browser APIs when available, no-ops only cosmetic shell features, and disables terminal teleport, updater, and window controls with explicit explanations.

## Tauri Runtime And Server Attachment

- [x] Decide whether desktop only attaches to an existing Builder server, starts a server process, or supports both: attach only for first workflow MVP.
- [x] Define server discovery and endpoint selection for MVP: Builder config/default host and port only.
- [x] Decide auth readiness UX: auth missing/expired/not ready uses the same generic startup failure surface as other readiness failures, with diagnostics in the local GUI log rather than a dedicated auth UX.
- [ ] Define failure states for server unavailable, incompatible version, auth missing, and migration required.
- [x] Decide whether server binary is bundled as Tauri sidecar for any release channel: never bundle Builder server binary in desktop app.

## Static Web UI Surface

- [x] Decide whether production web assets are served by Builder's Go server: defer implementation, but target Go server serving built SPA assets later.
- [x] Decide route prefix direction: future web UI uses an explicit route prefix and does not take over server root.
- [x] Decide exact route prefix, cache policy, and SPA fallback behavior timing: defer exact details until web UI implementation; keep only the constraints that UI must live under an explicit route prefix and never conflict with `/rpc`, `/healthz`, or `/readyz`.
- [x] Decide `builder webui`, `builder serve --ui`, or other command semantics: defer all command naming until web UI implementation after inspecting `server/serve` and the CLI command shape.
- [ ] Ensure `/rpc`, `/healthz`, and `/readyz` remain unambiguous server routes.

## CI/CD

- [x] Add GUI lint/typecheck/test jobs to CI.
- [x] Add GUI web build job.
- [x] Add Tauri native check job after Rust/toolchain prerequisites are available.
- [x] Decide whether native Tauri builds run on every PR or only release/nightly: PR CI runs checks/tests/lint/web build/native check, while release bundles ship through `release.yml`.
- [x] Add dependency update config for `apps/`.
- [x] Decide Node/pnpm pinning policy across docs and GUI: do not downgrade to Node 22; use current Node 25+ where available, keep pnpm pinned through package manager/tooling policy.
- [x] Decide Rust toolchain pinning via `rust-toolchain.toml`.
- [x] Add frontend dependency policy for direct-dependency allowlisting, 7-day package maturity, and install-script blocking.
- [x] Decide artifact retention for GUI builds: PR/native-check artifacts use short/default retention; release artifacts are kept by GitHub Releases.
- [x] Decide release-gated full Tauri bundle smoke for first MVP release: manual QA only; CI still runs build/checks, but no automated packaged-app smoke is required for the first MVP release.

## Release, Signing, And Updates

- [x] First workflow MVP platform is macOS only.
- [x] Decide broader v1 platform and QA matrix after first workflow MVP: macOS first; Windows/Linux later after MVP QA, no broader v1 matrix locked now.
- [x] Decide signing/notarization flow for broader v1 release platform: defer exact signing/notarization until after MVP and release scope start.
- [x] Decide update channel strategy: defer exact updater/channel strategy until after MVP and release scope start.
- [x] Decide whether app identifier `sh.kent` is final before signed release: `sh.kent` is final now.
- [x] Decide version source of truth between root `VERSION`, Tauri config, and package metadata: root `VERSION` remains source of truth and release/build scripts sync desktop metadata from it.
- [x] Decide crash-reporting/telemetry policy before adding any external service: no external crash reporting or telemetry in MVP; local logs/errors only.
- [x] Decide local GUI log policy: store under the Builder persistence root, cap at 10 MB for MVP, and redact secrets by default.

## Testing Practices

- [ ] Unit-test pure UI/domain adapters with Vitest.
- [ ] Component-test important rendering/interaction paths with Testing Library.
- [ ] Integration-test API adapters with fake transport/server fixtures.
- [ ] Add E2E strategy for desktop flows after first real screens exist.
- [ ] Add accessibility checks for core screens.
- [ ] Add visual-regression strategy only when UI stabilizes enough to avoid churn.

## Performance And Scale

- [x] Decide virtualization stack for long chats/lists: TanStack Virtual later, triggered by list size or profiling.
- [ ] Define large-transcript and event-stream constraints. GUI must not require full `events.jsonl` loading.
- [ ] Define render budget and profiling workflow.
- [ ] Define package boundaries for future workflow editor, task/chat, boards, questions, approvals, settings, and shell.
- [x] Decide virtualization stack direction: use TanStack Virtual later for long chats/lists.
- [x] Virtualization trigger: add virtualization when a single visible surface regularly renders more than roughly 300 mixed-height rows or profiling shows list render/scroll jank; until then, paginate.
- [x] Decide when to split feature packages out of `apps/desktop/src`: keep MVP feature code in `apps/desktop/src`; split only when there is a second real consumer, a package has a stable independent boundary, or a module becomes too large and cohesive enough to test independently.

## Dependency Application

- [x] Add MVP frontend dependencies: `@tanstack/react-query`, `@tanstack/react-router`, `react-hook-form`, `@hookform/resolvers`, `zod`, `react-markdown`, `remark-gfm`, `rehype-sanitize`, `i18next`, and `react-i18next`.
- [x] Update `apps/dependency-policy.json` allowlist and `apps/pnpm-lock.yaml` together with dependency additions.
- [x] Respect `minimumReleaseAge: 10080`; current dependency add succeeded without bypassing the 7-day cooldown.
- [x] Do not bypass the 7-day dependency cooldown for TanStack or other packages without explicit security review.

## Documentation

- [x] Add GUI workspace guidance in `apps/AGENTS.md`.
- [x] Update root `AGENTS.md` with GUI workspace summary.
- [x] Add contributor setup docs for GUI prerequisites.
- [x] Add GUI workflow use-case inventory in `docs/dev/gui-workflow-use-cases.md`.
- [x] Add GUI workflow MVP PRD-writing checklist in `docs/dev/gui-workflow-mvp-prd-checklist.md`.
- [x] Add CLI/TUI feature-parity checklist in `docs/dev/gui-cli-parity-checklist.md`.
- [x] Add CLI/TUI parity evidence and command inventories in `docs/dev/gui-cli-parity-evidence.md`.
- [ ] Add troubleshooting docs for Tauri native prerequisites.
- [ ] Keep `docs/dev/gui-client-stack.md` as stack/source-of-truth during bootstrap.
