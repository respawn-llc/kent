GUI workspace for Kent desktop/web client surfaces.

## Layout

- `desktop/` contains the Tauri desktop app.
- `desktop/packages/*` contains packages only used by the desktop app.
- `shared/*` is reserved for packages shared by multiple GUI apps. Do not add packages here until there is a second real consumer.

## Stack

- Use React, TypeScript, Vite, Vitest, Bun workspaces, and Tauri v2.
- Keep Kent.s Go server authoritative for runtime, worktrees, DB, orchestration, validation, approvals, asks, and workflow state.
- Treat GUI as a remote-control client over Kent API/read-model contracts.

## Desktop TypeScript Ownership

- Wide Task Detail layouts must give Description and Metadata exactly one shared height: the maximum of their two intrinsic heights. Measure both intrinsic heights before applying that shared height to either outer island. Keep the behavioral regression test that covers both the Description-taller and Metadata-taller cases.
- `desktop/src/app/**` owns shell composition, including startup under `app/startup/**`, routes, sidebars, providers, native-window controllers, and the developer showcase.
- `desktop/src/app-facade/**` is the feature-facing shell seam for app services, navigation, query keys, status, sidebar contracts, and native helpers. It may depend on public API/UI/native seams, but never on shell or features.
- Each `desktop/src/features/<feature>/**` directory is an isolated feature module. Features may depend on `@/app-facade`, `@/api`, `@/ui`, `@/i18n`, and public `@/shared/<capability>` entrypoints, but never on another feature, API internals/composition, shell, or the native package.
- Each `desktop/src/shared/<capability>/**` directory is an app-local reusable capability with its own entrypoint. Shared capabilities may use public app-facade, API, UI, vendor, and shared-capability seams. They must not depend on features or shell.
- `desktop/src/ui/**` owns generic visual primitives and presentation utilities. It depends on no other desktop production owner.
- `desktop/src/api/**` owns adapted models, raw schemas, transport, sockets, and client implementation. Features may import `@/api`. Only shell startup and test support may import `@/api/composition`.
- `desktop/src/test-support/**` owns reusable test-only harnesses through capability entrypoints. Only categorized tests may import it. Feature-internal fixtures live with their feature.
- `desktop/packages/native-bridge/**`, `desktop/src/i18n/**`, `desktop/src/vendor/**`, `desktop/src/types/**`, `desktop/test/**`, and `desktop/tooling/**` are explicit leaf owners.

Cross-owner imports use the target owner's public `@/…` entrypoint. Relative imports stay within one owner. The native package uses `@app/native-bridge`. Vendor aliases must match their declared adapter paths exactly. Entrypoints stay narrow, declarative, and side-effect free.

Architecture lint applies to production, tests, type imports, re-exports, dynamic imports, CommonJS calls, and literal Vitest module APIs. Tests receive no general ownership exemption. Categorized tests may use relative imports only for `src-tauri/tauri.conf.json` and `src-tauri/capabilities/default.json`.

Boundary enforcement is fail-closed: every desktop TypeScript file and local dependency must be classified. Do not add grandfather lists, inline suppressions, deep-import shortcuts, or compatibility barrels. Any exception requires explicit approval, an adjacent configuration rationale, and focused policy-fixture coverage.

## Effect reference and ownership

- The main Project Edit destination is the Effect reference. Its immutable ViewModel defines readonly observations and explicit `Atom.fn` actions. `AppProviders` owns the standard Atom React `RegistryProvider` for the window. React uses standard value-mode action bindings. Use concurrent actions with synchronous admission from the actual Query mutation observers. Do not add pending flags, custom runners, retention, or disposal timers.
- Query owns server content, request outcomes, mutation completion callbacks, invalidation, and bounded pages. `app-facade/queryAtom` publishes the library result directly and finalizes its subscription with Atom. Keep mutation observation mounted with feature action bindings, even without visual readers. Query's MutationObserver cannot restore a detached mutation on resubscribe.
- Local name/key drafts use nullable uninitialized atoms. Derived state is computed from those inputs and Query. Keep visual dialog state in React. Query mutation callbacks own feedback and invalidation after navigation. Atom-local work follows library disposal. Completion uses the original sidebar navigator's accepted/stale outcome.
- Native event observations use cold Streams. Scope owns asynchronous registration and release. Filter to the destination before bounded buffering. Combine only change notifications. Registration failure explicitly fails the supplied Stream queue and produces the ordinary temporary status notification without restarting.
- Native integration may use private platform callback storage. Application-facing observation contracts expose Streams or readonly Atoms. The Effect policy checks those exported contracts even inside native integration.
- Production adoption is limited to Effect, Scope, Fiber, Stream, Atom, and Atom React. Permit `Queue.offerUnsafe` and `Queue.fail` only for the queue supplied to this native `Stream.callback` integration. Do not create queues, an event bus, an application subscription service, or an Effect DI/HTTP/Schema stack.
- The Project subscription API permits one cold `Stream.callback` over the existing transport. Its library-owned buffer holds 1,000 observations. `Queue.offerUnsafe` detects incoming overflow, and `Queue.failCauseUnsafe` with `Cause.die` surfaces fatal debug diagnostics. Composition supplies the logging/debug policy. This narrow transport adapter permission allows typed events and scope cleanup without application queues or a second callback-facing Project API. The explicit policy-fixture waiver applies only to this written permission. Existing lint enforcement and fixtures are unchanged. Adapter runtime coverage is required.
- First-party root/detached execution, manual root scopes/registries, global registry access, and opaque namespace forwarding are forbidden across active JavaScript/TypeScript owners. The existing ESLint plugin supplies the semantic rules through the shared desktop compiler prerequisite for lint/build/check. Subscription enforcement follows Effect-facing export declarations and types. Pre-Effect facade re-exports do not count as migrated. Native and Query callbacks are low-level inputs. Application observations expose readonly Atoms or Streams.
- KENT-365 owns React/Promise native Delete/Unlink confirmation internals. Desktop requests and observations follow the independent-operation contract in `docs/dev/specs/desktop-gui.md`. Connection lifecycle never coordinates feature work.

### Installed Effect guidance

Use `.kent/skills/effect-ts/SKILL.md` when building or changing Effect-backed desktop screens. It maps the installed maintainers' usage guidance to the Project Edit reference. Read `apps/desktop/node_modules/effect/AGENTS.md` completely before writing Effect code, follow its relevant links, and consult the installed `effect/src` for version-specific APIs. Repository Just, dependency policy, and the narrow adoption boundary override generic upstream architecture recommendations. `just setup --apply` installs and prepares the supported Effect-aware TypeScript 7 compiler without changing TypeScript versions.

## Checks

- Run `just setup --apply` before GUI work, then use `just lint desktop`, `just test desktop`, `just build desktop`, or `just check desktop --dry-run`.
- `just lint desktop` enforces the architecture and dependency policies.
- Use browser-client QA as the primary manual GUI QA path. Run `just dev desktop` for interactive QA against an existing Kent server.
- Tauri native builds require Rust toolchain plus platform-specific WebView/build dependencies.
- Commit `apps/desktop/src-tauri/gen/schemas/*.json` when Tauri regenerates them. They are generated. Keeping them in the repo avoids dirty editor/schema state on clean clones.
- Frontend dependency policy is enforced by `apps/dependency-policy.json` and `apps/scripts/check-dependency-policy.mjs`. New direct dependencies are blocked until they are added to the allowlist intentionally.
- TypeScript policy is enforced by `apps/scripts/check-typescript-policy.mjs`. Explicit `any`, including `as any`, is forbidden across the whole `apps/` workspace.
- `apps/bunfig.toml` enforces a seven-day `minimumReleaseAge` of `604800` seconds. `apps/package.json` sets `trustedDependencies: []`. Explicit maintainer approval is required to change either setting.
- Add a package-name entry to `minimumReleaseAgeExcludes` in both `apps/dependency-policy.json` and `apps/bunfig.toml` only after explicit maintainer approval. Remove the entry after the package reaches the seven-day maturity threshold.
