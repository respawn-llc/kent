GUI workspace for Builder desktop/web client surfaces.

## Layout

- `desktop/` contains the Tauri desktop app.
- `desktop/packages/*` contains packages only used by the desktop app.
- `shared/*` is reserved for packages shared by multiple GUI apps. Do not add packages here until there is a second real consumer.

## Stack

- Use React, TypeScript, Vite, Vitest, pnpm workspaces, and Tauri v2.
- Keep Builder's Go server authoritative for runtime, worktrees, DB, orchestration, validation, approvals, asks, and workflow state.
- Treat GUI as a remote-control client over Builder API/read-model contracts.
- Do not import Tauri APIs directly from feature components. Route native capabilities through a `NativeBridge` package.
- Keep API DTO churn behind GUI-side adapters; do not spread raw server DTO assumptions across components.

## Checks

- From `apps/`, run `pnpm install --frozen-lockfile`, `pnpm lint`, `pnpm typecheck`, `pnpm test`, and relevant build scripts after GUI code changes.
- Use browser-client QA as the primary manual GUI QA path. Run `pnpm --dir apps/desktop dev:browser` for interactive QA against an existing Builder server.
- Tauri native builds require Rust toolchain plus platform-specific WebView/build dependencies.
- Commit `apps/desktop/src-tauri/gen/schemas/*.json` when Tauri regenerates them; they are generated, but keeping them in the repo avoids dirty editor/schema state on clean clones.
- Frontend dependency policy is enforced by `apps/dependency-policy.json` and `apps/scripts/check-dependency-policy.mjs`. New direct dependencies are blocked until they are added to the allowlist intentionally.
- TypeScript policy is enforced by `apps/scripts/check-typescript-policy.mjs`. Explicit `any`, including `as any`, is forbidden across the whole `apps/` workspace.
- `apps/pnpm-workspace.yaml` enforces `minimumReleaseAge: 10080` and `onlyBuiltDependencies: []`; do not bypass these without Nikita approval.
- If `pnpm install` fails only because a toolchain transitive package is younger than 7 days, keep direct app deps strict and add a narrow `minimumReleaseAgeExclude` only after explicit review. Do not use age exemptions for direct app/runtime dependencies.
