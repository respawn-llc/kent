# GUI Client Stack Spec

Status: initial stack lock for GUI bootstrap.

Date: 2026-05-15.

## Purpose

Define the frontend stack for Builder's new GUI client. The first workflow MVP starts with projects, workflow Kanban boards, task details, comments, questions, approvals, and terminal teleport. Workflow editor and built-in chat are later. Over time the GUI should be capable of replacing the current TUI.

Workflow MVP behavior lives in `docs/dev/gui-workflow-mvp-prd.md`; implementation sequence lives in `docs/dev/gui-workflow-mvp-implementation-plan.md`.

## Stack Lock

- UI implementation: React + TypeScript.
- Desktop shell: Tauri.
- Workflow editor: React Flow/xyflow unless early prototyping exposes a blocking issue.
- Virtualized long lists/chat: TanStack Virtual when list size or profiling justifies virtualization.
- Server integration: Builder's Go server remains authoritative for runtime, worktrees, DB, orchestration, validation, and workflow state. The GUI is a remote-control client over Builder's API/read-model boundary.

The exact v1 release platform is a separate release-scope decision. The stack decision is Tauri desktop, not a commitment to all desktop operating systems in v1.

## Distribution Surfaces

Primary surface:

- Tauri desktop app.

Architecture-compatible future/secondary surface:

- Browser-hosted web UI served by Builder's Go server.

Production web UI hosting is deferred from the workflow MVP. Target direction is Go server serving built SPA assets under an explicit route prefix later, without taking over the server root or conflicting with `/rpc`, `/healthz`, and `/readyz`.

The React/TypeScript UI should not depend directly on Tauri APIs. Native functionality must go through a client-side `NativeBridge`, allowing a browser implementation and future shells to define explicit capability differences.

Do not freeze command and route names yet. Current server routes are `/rpc`, `/healthz`, and `/readyz`. Static UI hosting and web UI command naming are deferred until web UI implementation after inspecting `server/serve` and the CLI command shape.

Manual GUI QA uses the browser client path in `docs/dev/gui-browser-qa.md`.

## Native Bridge

Native bridge capabilities must be explicit and capability-checked:

- Clipboard.
- Native notifications.
- Tray/menu bar.
- App/window menus.
- Updater.
- Window controls.
- macOS vibrancy/blur.

Browser implementation may degrade or no-op unsupported capabilities. Tauri implementation owns native behavior through plugins and shell-specific commands.

Fallback policy: every native bridge capability is gated through a capability registry. Browser implementations use real browser APIs when available, no-op only cosmetic shell features, and disable terminal teleport, updater, and window-control actions with explicit explanations.

## Client Logic Boundary

Keep client business logic intentionally thin:

- Server owns authoritative validation, workflow graph semantics, task state, approvals, asks, scheduler state, runtime state, and persistence.
- Client owns rendering, interaction state, optimistic UI where safe, local filters/sorts, and transient editor gestures.
- Workflow DTO/read-model churn before Builder 2.0 must be isolated behind a GUI-side API adapter.
- Do not make Respawn SDK extraction or public SDK maintenance a dependency of the GUI MVP.

## Rejected Shells

Electron is rejected for Builder's GUI client stack.

Compose JVM Desktop is rejected as the GUI shell. This does not rule out Kotlin Multiplatform for future shared non-UI client code. Compose Desktop has adequate ordinary desktop support such as menus, tray, notifications, and basic windows, but does not satisfy Builder's macOS-native shell requirements. Evidence from the stack review:

- Compose transparent windows require `undecorated`.
- Custom title bars require reimplementing native chrome behavior.
- No first-party Compose Desktop macOS vibrancy/blur API was found.

Go-native GUI toolkits are not selected. Wails is credible as a Go/WebView shell but does not materially beat Tauri for Builder. Fyne/Gio do not meet Builder's workflow-editor ecosystem and macOS polish needs.

## Follow-Up Decisions

### Repo Layout

Decision: keep GUI code in this repository under the top-level `apps/` workspace.

- `apps/desktop` contains the Tauri desktop app.
- `apps/desktop/packages/*` contains packages shared within the desktop app, such as `NativeBridge`.
- `apps/shared/*` is reserved for packages shared by more than one GUI app. Do not add packages there until there is a second real consumer.

MVP feature code stays in `apps/desktop/src`. Split feature packages only when there is a second real consumer, a package has a stable independent boundary, or a module becomes too large and cohesive enough to test independently.

Rationale: Builder server contracts, read models, release automation, and GUI integration tests should evolve atomically while the product shape is still mutable. A separate repository would add API-version and release-coordination tax too early.

### Package Manager

Decision: use pnpm for the GUI workspace.

Rationale: pnpm is a dependency/workspace manager, not a replacement runtime. It keeps the new platform surface smaller while Tauri already adds Rust/native build complexity. Bun remains a possible future experiment, but is not part of the GUI foundation.

### Native App Identifier

Decision: use `sh.kent` as the final Tauri identifier for the `kent.sh` product namespace.

Changing it after shipping signed app updates is treated as a migration because native app identity, app data, signing, and updater behavior can be affected.

Visible MVP app chrome uses `Kent` while the rebrand is underway. Final signed-release display naming remains a release decision; the native app identifier is locked above.

### TypeScript API Client

Decision: start with a hand-written typed TypeScript JSON-RPC/WebSocket client plus GUI-side DTO adapters. Add contract tests against Go DTO/schema fixtures so `shared/serverapi` churn does not leak into feature components. A generated client can be introduced later if contracts stabilize enough to justify generation.

Do not add tRPC for MVP protocol typing. tRPC is optimized for TypeScript server/client stacks, while Builder's server is Go and already uses Builder JSON-RPC contracts. Use typed adapters, Zod boundary validation, and contract tests instead. Revisit generated schemas or codegen later if contract churn settles.

Use native browser `WebSocket` with an in-repo JSON-RPC transport/reconnect layer. Do not adopt a third-party reconnect/RPC WebSocket wrapper for MVP. The transport must own Builder-specific behavior: request ids, pending-request rejection on disconnect, typed protocol errors, capped reconnect/backoff, auth-readiness, capability checks, bounded buffering, and full refresh after reconnect.

Do not replay mutations after reconnect. Refetch, resubscribe, and let the user issue a new command.

### Frontend State, Routing, And Forms

Use `@tanstack/react-query` for server read models, request cache, mutations, invalidation, and WebSocket-driven cache updates. Use React local state for local UI state in MVP. Do not add Zustand or Redux Toolkit in MVP.

Use `@tanstack/react-router` for typed routes/destinations, boxed behind Builder destination helpers so feature code does not depend on router details. Validate route/search params with Zod at the boundary. Dialog and modal routes should remain deterministic and restorable; task detail/resume state can live in route search params when it affects back/close/deep-restore behavior.

Desktop modal actions must go through `useNativeDialogFallback` and the native dialog bridge. The hook owns native open, status-toast error handling, and explicit fallback rendering. Feature-owned ad hoc in-page modal fallbacks are not allowed unless that fallback is intentionally registered through this helper.

Native dialog windows inherit the opener's effective app theme through `NativeDialogWindowOptions.theme` and the bridge's internal route serialization. Feature routes should not hand-roll theme query params.

Native dialog startup gates must match dialog responsibilities. API-backed dialogs, such as task creation and project creation, stay behind `StartupGate` because they need server readiness before rendering mutable forms. Event-only dialogs, such as workspace unlink confirmation, bypass `StartupGate` and communicate back to the main window through native events so they do not pay a WebSocket readiness handshake before showing confirmation UI.

Use React Hook Form with `@hookform/resolvers` and Zod for MVP forms. Zod schemas should live at GUI adapter/form boundaries and produce i18n error keys rather than English strings. Defer TanStack Form until the workflow editor has genuinely complex form/editor needs.

### Styling And Import Boundaries

Use CSS custom properties for semantic design tokens plus CSS Modules/plain CSS in components. Do not add Tailwind or vanilla-extract for MVP.

Feature code must not import Tauri APIs, `react-markdown`, raw transport, or raw server DTOs. Use native bridge packages, shared `MarkdownText`, API adapters, and app-local UI kit exports instead.

### Markdown Rendering

Task bodies, task comments, and many future GUI surfaces use Markdown rendering from plain multiline text inputs. Do not add a WYSIWYG editor for MVP.

Use `react-markdown` with `remark-gfm` and `rehype-sanitize`, wrapped in one shared `MarkdownText` component. Feature components must not import `react-markdown` directly.

Do not render raw HTML in Markdown for MVP. Use `skipHtml` or equivalent, do not add `rehype-raw`, and sanitize output anyway.

Custom-render links with a safe-protocol allowlist, open external links through a bridge helper, and add `rel="noreferrer"`. Custom-render `code` and `pre` with theme-token styling now; syntax highlighting is deferred. Do not put translated strings inside Markdown component internals except i18n-provided aria labels.

### Dates, Lists, And Commands

Use native `Intl` for MVP date/time formatting. Do not use Temporal yet because macOS Tauri depends on WebKit compatibility.

Continue paginating lists by default. Add TanStack Virtual when a single visible surface regularly renders more than roughly 300 mixed-height rows or profiling shows list render/scroll jank.

Do not add a command palette for MVP. When it becomes product-relevant, use `cmdk` for the palette UI.

Exact shortcut map is deferred. GUI keyboard conventions use platform-native modifiers, do not capture global shortcuts while typing, expose shortcuts through visible menu/tooltip discovery, and avoid keyboard-only hidden paths.

### Dependency Application

The GUI foundation dependencies are added to `apps/desktop/package.json`: `@tanstack/react-query`, `@tanstack/react-router`, `react-hook-form`, `@hookform/resolvers`, `zod`, `react-markdown`, `remark-gfm`, `rehype-sanitize`, `i18next`, and `react-i18next`.

Keep `apps/dependency-policy.json` and `apps/pnpm-lock.yaml` in sync with package additions. Respect `minimumReleaseAge: 10080`; do not bypass the 7-day dependency cooldown for TanStack or other packages without explicit security review.

Shadcn components are adapted into the local UI kit rather than generated blindly. `pnpm dlx shadcn@latest add item` prompts for `components.json` in `apps/desktop`; use `pnpm dlx shadcn@latest view item` plus local adaptation unless adding shadcn config is an intentional architecture change.

Dropdown controls use the app-local UI kit `SelectField`, adapted in shadcn style as a custom combobox/listbox. Do not use native `<select>` controls in desktop GUI feature code.

Shared empty, loading, and error surfaces live in `apps/desktop/src/ui/StateViews.tsx`. Use `EmptyState` for route-level no-content states instead of hand-rolled cards; it defaults to a full-page island with a centered icon/title/body/actions column, `Inbox` as the empty icon, optional `icon`, and `actions` rendered as a wrapping button row. `LoadingState` delays its visible spinner layout by 500ms the first time a loading key is used and renders blank space first to avoid fast-loading flicker; later remounts for the same key show immediately. Embedded demos/cards should pass `fullPage={false}`. Route-level loading/error states under `RouteTransitionFrame` should pass `reveal={false}` to avoid nested post-navigation reveal flashes.

### CSS Ownership

Keep `apps/desktop/src/styles.css` global: theme tokens, base rules, shared utilities, and shared UI motion only. Feature-specific selectors and CSS variables belong with the owning feature. Board-only styles, including workflow board hover menu variables and workflow issue bullets, live in `apps/desktop/src/features/board/board.css`.

### CI And Runtime Versions

Regular CI runs GUI checks, tests, lint, typecheck, web build, and native Tauri check. Full release bundles are produced through `release.yml`, not every PR.

Do not downgrade the GUI toolchain to Node 22 just because it is an LTS floor. Use the current Node 25+ line where available unless a concrete toolchain issue appears. Keep pnpm pinned through `packageManager`/Corepack policy.

Root `VERSION` remains the source of truth. Release/build scripts sync Tauri/package metadata from it.

### Logs And Telemetry

Do not add external crash reporting or telemetry in MVP. Use local logs/errors only, including a local GUI log file under the Builder persistence root for startup, connection, API, and native bridge failures. Keep the MVP log bounded to 10 MB and redact auth headers, tokens, env values, and request bodies by default.

### Static Hosting

Production web UI hosting is deferred. Future direction is Go server serving built SPA assets under an explicit route prefix. Exact route prefix, cache policy, SPA fallback behavior, and whether static UI hosting is always enabled or opt-in remain implementation-time decisions. Keep only the constraints that the web UI must not take over server root and must not conflict with `/rpc`, `/healthz`, or `/readyz`.

### Web UI Command Surface

Exact semantics for `builder webui`, `builder serve --ui`, or another command are deferred until web UI implementation after inspecting `server/serve` and the CLI command shape.

### Tauri Server Attachment

The Tauri app attaches to an already-running Builder server and never bundles the Builder server binary as a Tauri sidecar. Auth missing, auth expired, and auth not ready use the same generic startup failure path as other readiness failures.

Startup failure UI stays summary-first for MVP: show human-readable failure text and the next action, while deeper diagnostics are written to the local GUI log file.

### V1 Release Platform

MacOS ships first. Windows/Linux are later after MVP QA, with no broader v1 matrix locked now.

Exact signing/notarization and update-channel strategy are deferred until after MVP and release scope start. The app identifier `sh.kent` is final now.

First MVP release bundle uses manual QA only for packaged-app smoke. CI still runs build/checks, but automated packaged-app smoke is not required for the first MVP release.

Accessibility stays best-effort until after v1; no stronger post-MVP release bar is locked now.
