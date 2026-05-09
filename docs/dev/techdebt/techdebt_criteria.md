# Tech Debt Research Criteria

Purpose: exhaustive checklist for auditing Builder tech debt. This is not the debt catalog itself; it is the backlog of research tasks and smell criteria used to find catalog entries. Each checked item should produce concrete findings with repo-relative paths, examples, implementation impact, remediation task requirements, and regression-prevention requirements.

Use this file as a research backlog, not as the final inventory. When a checklist item produces verified evidence, add or update an entry in `docs/dev/techdebt/techdebt.md`. Some criteria intentionally overlap across audit families; when recording findings, de-duplicate into one family-wide catalog entry unless ownership, implementation impact, or remediation task differs.

## Research Output Requirements

- [x] Record every finding with exact repo-relative file paths and representative symbols/functions.
- [x] Distinguish verified facts from hypotheses.
- [x] Include user-visible impact where applicable.
- [x] Include engineer-visible impact where applicable.
- [x] Classify severity as `P0` correctness/reliability/security, `P1` architecture/source-of-truth/scalability, `P2` maintainability/cleanup, or `P3` polish.
- [x] Link related existing decisions in `docs/dev/decisions.md`.
- [x] Link related scratch debt from `docs/tmp/tech_debt.md` when expanding existing items.
- [x] Prefer family-wide findings over one-off symptom lists.
- [x] Note whether the remediation task is breaking, migration-requiring, or protocol-affecting.
- [x] Note whether a finding needs tests, docs, migration, release notes, or operator communication.
- [x] Note whether the debt is caused by missing abstraction, wrong abstraction, over-abstraction, unclear ownership, historical compatibility, or product ambiguity.
- [x] Note whether debt blocks future features or only slows maintenance.
- [x] Note whether debt increases model-token usage, context drift, or agent confusion.

## Architecture And Boundaries

- [x] Find god packages that import too many internal packages.
- [x] Find god structs that aggregate unrelated services, state, caches, lifecycle, and dependencies.
- [x] Find god files over 600 LoC, especially production files.
- [x] Find god functions with multiple phases, nested branching, or hidden workflows.
- [x] Find packages whose names do not clearly describe one responsibility.
- [x] Find packages that mix composition/root wiring with business logic.
- [x] Find packages that mix UI, persistence, networking, and runtime state.
- [x] Find packages that expose service-locator surfaces.
- [x] Find packages that are only thin pass-through wrappers with little architectural value.
- [x] Find circular conceptual dependencies even when Go import cycles are absent.
- [x] Find dependency direction violations between `cli`, `server`, and `shared`.
- [x] Find `shared` packages that contain server-owned or CLI-owned logic.
- [x] Find frontend code that depends on runtime-native/server-native details instead of client DTOs.
- [x] Find server code that depends on UI presentation types unnecessarily.
- [x] Find transport or persistence code that knows domain policy it should not own.
- [x] Find domain concepts represented differently in multiple layers.
- [x] Find unclear ownership for lifecycle, session state, project state, worktree state, auth state, or process state.
- [x] Find areas where embedded/local and remote/server modes diverge semantically.
- [x] Find compatibility shims that preserve old architecture without explicit migration plan.
- [x] Find abstractions with only one implementation and no clear extension pressure.
- [x] Find abstractions that leak implementation details through method names, DTO fields, or errors.
- [x] Find places where new features require edits across many unrelated packages.
- [x] Find places where policy decisions are encoded by caller choice instead of central authority.
- [x] Find places where one package owns both command/query behavior and projection/read-model behavior.
- [x] Find places where mutable runtime state and durable state are both modified in same function.
- [x] Find places where domain invariants are documented but not encoded in type/API boundaries.

## Source Of Truth And Duplication

- [x] Find protocol methods mirrored across constants, DTOs, clients, gateway switches, tests, and docs.
- [x] Find request/response shape duplication between server APIs, client APIs, UI DTOs, and persistence rows.
- [x] Find validation duplicated across CLI flags, server handlers, transport decoders, and service methods.
- [x] Find auth, scope, or permission rules duplicated across gateway, services, CLI, and runtime.
- [x] Find default values duplicated across config registry, docs, prompts, tests, and generated config payloads.
- [x] Find enum/string constants duplicated without one authoritative package.
- [x] Find status labels, user-facing messages, or warnings duplicated across UI and server.
- [x] Find command names, slash commands, key bindings, hotkeys, and usage text duplicated.
- [x] Find model/tool capability checks duplicated across runtime, config, prompts, and transport.
- [x] Find transcript formatting duplicated between runtime projections, TUI rendering, logs, and tools.
- [x] Find session/workspace/project resolution duplicated across startup, headless, server, and CLI commands.
- [x] Find client/server loopback implementations that drift from remote implementations.
- [x] Find tests that replicate production routing/policy tables manually.
- [x] Find code comments that restate logic because source of truth is not obvious.
- [x] Find multiple caches representing same underlying state.
- [x] Find multiple persisted representations for same fact.
- [x] Find generated and hand-written code covering same contract without drift checks.
- [x] Find config docs generated from one source but behavior controlled elsewhere.
- [x] Find magic strings that should be typed IDs, enums, or registered contracts.
- [x] Find string-based feature detection where typed capability metadata should exist.

## Correctness And Invariants

- [x] Find state machines represented as booleans instead of explicit states.
- [x] Find invalid state combinations possible in structs.
- [x] Find transitions that do not validate current state.
- [x] Find functions that accept zero values where zero is invalid.
- [x] Find missing validation at public boundaries: CLI, RPC, tools, config, persistence, env, filesystem, model responses.
- [x] Find validation performed only in UI/client code but not server/service code.
- [x] Find validation performed after side effects.
- [x] Find code that ignores or discards errors.
- [x] Find error wrapping that loses actionable context.
- [x] Find generic `errors.New` messages where typed errors are needed for caller policy.
- [x] Find `panic` or package-init fatal behavior in runtime paths.
- [x] Find nil-tolerant methods that hide bugs instead of failing at composition boundaries.
- [x] Find defensive fallbacks that mask corrupted state.
- [x] Find partial failure handling that can leave durable state inconsistent.
- [x] Find write paths without atomicity or rollback when multiple resources are mutated.
- [x] Find idempotency gaps for repeated CLI/RPC/runtime requests.
- [x] Find request deduplication assumptions not backed by durable authority.
- [x] Find ordering assumptions not encoded in sequence numbers or clocks.
- [x] Find recovery paths that infer state from text rather than structured facts.
- [x] Find crash recovery cases with undefined or under-tested behavior.
- [x] Find resume paths that behave differently from fresh startup.
- [x] Find interrupted-turn behavior that can lose or duplicate messages/events/tool results.
- [x] Find compaction/fork/history mutation paths that can violate transcript immutability or cache assumptions.
- [x] Find places where cache continuity assumptions are not enforced by tests.
- [x] Find paths that assume a workspace/project/session exists without rechecking.
- [x] Find time/date handling using wall-clock comparisons where monotonic time or persisted timestamps matter.
- [x] Find path handling that does not canonicalize before authorization or comparison.
- [x] Find symlink, `..`, case-sensitivity, or volume-boundary edge cases.
- [x] Find filesystem writes that do not ensure parent directories, permissions, fsync policy, or atomic rename as needed.
- [x] Find code that loads unbounded files into memory.
- [x] Find unbounded collections in long-running runtime state.

## Concurrency, Lifecycle, And Resource Management

- [x] Find goroutines without clear owner, cancellation, or wait path.
- [x] Find background work tied to `context.Background()` instead of a lifecycle context.
- [x] Find channels that can block forever under backpressure.
- [x] Find best-effort sends that drop important events silently.
- [x] Find shared maps/slices accessed without synchronization.
- [x] Find locks held across I/O, RPC, model calls, subprocess waits, or callbacks.
- [x] Find lock-ordering risks across runtime, registry, core, process manager, and session store.
- [x] Find race-prone package globals used by tests or runtime seams.
- [x] Find `sync.Once` usage that prevents recovery after transient initialization failure.
- [x] Find lifecycle `Close` methods that do not join owned goroutines.
- [x] Find cleanup paths that close only some owned resources.
- [x] Find subscriptions/streams without explicit backpressure and completion semantics.
- [x] Find notification fan-out that can leak subscribers.
- [x] Find process management where owner session is advisory but used like authorization.
- [x] Find long-running model/tool calls without cancellation propagation.
- [x] Find shell/process output polling that can lose tail output at completion.
- [x] Find reconnect/release/lease races for active runtime controllers.
- [x] Find request multiplexing assumptions over connections that do not support them.
- [x] Find transport code that opens too many short-lived connections.
- [x] Find TUI updates that can reorder runtime events.
- [x] Find flaky timing tests using sleep instead of deterministic clocks/signals.

## Persistence, Storage, And Migration

- [x] Find mixed SQLite/file-backed authority for same domain fact.
- [x] Find migration code without idempotency tests.
- [x] Find migration code without interrupted-run/resume tests.
- [x] Find old persistence formats still supported through hidden branches instead of explicit migration.
- [x] Find corrupted session/event-log handling that silently skips data.
- [x] Find event schemas without versioning where evolution is expected.
- [x] Find append-only log rewrites that can break crash consistency.
- [x] Find persistence reads that scan entire unbounded histories.
- [x] Find indexes or query patterns that do not scale with many sessions/projects/workspaces.
- [x] Find session listing sorted by filesystem behavior rather than structured metadata.
- [x] Find metadata caches that can drift from database/file state.
- [x] Find missing foreign-key-like invariants in SQLite/domain services.
- [x] Find durable state written before associated file artifacts are committed.
- [x] Find backup/recovered state created without operator-visible diagnostics.
- [x] Find generated state stored near user-owned state with unclear ownership.
- [x] Find retention/cleanup policies missing for logs, generated assets, recovered assets, temp images, and background output.

## Transport, API, And Protocol

- [x] Find one endpoint requiring edits in many files.
- [x] Find gateway switch branches with repeated decode/validate/auth/scope/dispatch logic.
- [x] Find protocol constants not covered by dispatch tests.
- [x] Find dispatch routes without explicit auth policy.
- [x] Find dispatch routes without explicit scope policy.
- [x] Find routes whose scope is inferred from request fields inconsistently.
- [x] Find attach-state semantics that apply to subscriptions but not unary calls.
- [x] Find request validation that is optional by convention rather than enforced centrally.
- [x] Find response error mapping based on text instead of typed errors.
- [x] Find JSON-RPC error code handling missing client-side typed mapping.
- [x] Find client wrappers that use wrong connection strategy for long-running calls.
- [x] Find per-request connection setup where a shared connection manager would reduce complexity.
- [x] Find subscriptions implemented differently from unary calls without common stream contract.
- [x] Find transport tests that assert behavior via ad hoc fake servers rather than reusable route fixtures.
- [x] Find unversioned wire contracts that cannot support compatibility/migration.
- [x] Find public API names that expose internal historical concepts.
- [x] Find protocol/serverapi/clientui layering ambiguities.
- [x] Find request structs missing `json` tags or inconsistent field naming.
- [x] Find enum values without validation and unknown-value strategy.

## Runtime, Model, And Transcript

- [x] Find runtime engine state that mixes lifecycle, request construction, tools, compaction, reviewer, goals, and persistence.
- [x] Find model request assembly spread across multiple files without one typed pipeline.
- [x] Find prompt/system/developer/tool messages assembled via string conventions.
- [x] Find transcript roles/types inferred from content prefixes.
- [x] Find tool-call metadata inferred from result text instead of structured payloads.
- [x] Find persisted transcript entries whose UI projection requires legacy parsing.
- [x] Find compaction logic that mixes policy, token counting, model execution, persistence, and UI status.
- [x] Find reviewer logic that duplicates normal runtime request/tool/persistence flow.
- [x] Find goal logic that duplicates queue/runtime state instead of using shared lifecycle primitives.
- [x] Find fast-mode behavior that bypasses normal invariants.
- [x] Find tool availability/gating represented in prompts but not runtime contracts.
- [x] Find model capability checks that are provider-specific branches instead of typed capability policy.
- [x] Find native web search/multimodal/tool exposure decisions with inconsistent gating.
- [x] Find API cache-key handling that can mutate prior message shape.
- [x] Find retry behavior that can duplicate side effects or persisted entries.
- [x] Find tool execution paths that do not persist start/completion/error consistently.
- [x] Find transcript projection paths that diverge between live, dormant, detail, ongoing, and logs.
- [x] Find cache warning logic not backed by provider metadata.
- [x] Find local diagnostics persisted or replayed inconsistently.

## Tools And Filesystem Safety

- [x] Find tool definitions duplicated between schema, runtime handler, transcript formatter, and docs.
- [x] Find tools whose validation rules differ from tool schema descriptions.
- [x] Find tool result types that are stringly typed instead of structured.
- [x] Find tool errors that give model unclear retry/remediation guidance.
- [x] Find tool calls that can escape workspace boundaries without centralized guard.
- [x] Find path checks done before symlink resolution.
- [x] Find patch/apply logic with non-atomic edge cases.
- [x] Find shell command post-processing that relies on fragile string matching.
- [x] Find command-output truncation that can hide critical error context.
- [x] Find background shell sessions without durable owner/visibility model.
- [x] Find approval flows that mix model-visible and internal approval concepts.
- [x] Find ask-question flows with action payloads lacking versioning.
- [x] Find image/file tools that lack MIME/content validation where needed.
- [x] Find generated tool schemas not tested against runtime handlers.
- [x] Find transcript rendering of tools coupled to specific command text patterns.

## TUI, UX State, And Rendering

- [x] Find TUI models with too many independent mutable fields.
- [x] Find parallel render caches that must be manually invalidated together.
- [x] Find dirty flags that can become inconsistent with source state.
- [x] Find view logic that mutates state during rendering.
- [x] Find ongoing/detail views that project same transcript differently without shared pipeline.
- [x] Find scrollback behavior that risks rewriting immutable normal-buffer history.
- [x] Find layout calculations duplicated across overlays, pickers, status, transcript, onboarding, and worktrees.
- [x] Find picker/list UI duplicated instead of generic row-presenter components.
- [x] Find key handling duplicated across modes with inconsistent behavior.
- [x] Find alt-screen and normal-buffer behavior mixed in one model.
- [x] Find status overlays that fetch data, run subprocesses, parse files, and render in same file.
- [x] Find UI code that directly performs filesystem/network/auth/git operations.
- [x] Find user-facing copy duplicated or inconsistent.
- [x] Find accessibility/terminal compatibility gaps: width, ANSI, Unicode glyphs, no-color, resize, paste, bracketed paste, alternate screen.
- [x] Find flicker-causing full rerenders or unnecessary snapshot invalidation.
- [x] Find UI tests that assert brittle string snapshots without semantic helpers.
- [x] Find interactive flows lacking non-interactive test coverage.

## Configuration And Environment

- [x] Find settings declared in one monolithic registry instead of domain modules.
- [x] Find config default duplicated in code/docs/generated example.
- [x] Find env var parsing inconsistent with TOML/CLI parsing.
- [x] Find CLI flag behavior inconsistent with config file behavior.
- [x] Find settings whose source reporting can lie or become ambiguous.
- [x] Find settings where zero value is both valid and "unset".
- [x] Find settings where inheritance/override rules are implicit or order-dependent.
- [x] Find reviewer/model/provider config inheritance duplicated or under-specified.
- [x] Find capability override config that can create impossible provider states.
- [x] Find file paths in config resolved relative to inconsistent roots.
- [x] Find config migration/breaking changes lacking migration notes.
- [x] Find generated default config with comments that drift from validation.
- [x] Find package globals controlled by config but read before config load.
- [x] Find environment-dependent behavior that makes tests or startup nondeterministic.

## CLI, Commands, And Startup

- [x] Find root command files combining dispatch, parsing, usage text, IO, signals, and app startup.
- [x] Find package-level command runner seams mutated in tests.
- [x] Find command-specific behavior hidden behind generic string parsing.
- [x] Find headless and interactive startup paths resolving server/session/workspace differently.
- [x] Find daemon discovery/launch/fallback policy duplicated across commands.
- [x] Find auth gating duplicated between CLI and server.
- [x] Find local/remote path semantics under-specified or inconsistent.
- [x] Find commands that mutate persistence directly instead of using server RPC where required.
- [x] Find commands that should be admin/server-scoped but use workspace-scoped defaults.
- [x] Find usage/help docs that drift from actual flags.
- [x] Find JSON/headless output paths that share interactive-only assumptions.
- [x] Find signal handling that can leave child processes or runtime leases orphaned.

## Auth, Security, And Privacy

- [x] Find auth readiness checks duplicated across startup, gateway, core, and services.
- [x] Find OAuth/browser/device flow decisions based on raw string fragments.
- [x] Find token/API-key handling paths that risk logging secrets.
- [x] Find errors that may expose credentials, tokens, env vars, or filesystem private paths unnecessarily.
- [x] Find local daemon exposure risks: host binding, port defaults, no auth boundary, origin checks, local socket permissions.
- [x] Find project/session authorization based only on client-supplied IDs.
- [x] Find process access controls that trust advisory owner metadata.
- [x] Find approval bypass paths through alternate tools or CLI commands.
- [x] Find filesystem guard bypass via symlinks, hardlinks, relative paths, temp paths, or generated dirs.
- [x] Find insecure defaults in config or generated assets.
- [x] Find missing audit entries for sensitive actions: approvals, outside-workspace access, auth changes, process kill, destructive worktree operations.
- [x] Find tests with real credentials, hostnames, or private paths.

## Testing And Verification

- [x] Find packages with no tests where business logic exists.
- [x] Find tests that only compile packages with no behavioral assertions.
- [x] Find test files over 800 LoC that hide distinct scenarios.
- [x] Find brittle tests depending on wall-clock sleeps, ordering from maps, terminal width, filesystem timing, or external network.
- [x] Find tests that mutate package globals without cleanup.
- [x] Find tests that use broad integration setup when a unit boundary would be clearer.
- [x] Find missing regression tests for known bug families.
- [x] Find missing race tests for concurrent registries, streams, runtime lifecycle, process output, and transport.
- [x] Find missing crash/restart tests for session persistence, migration, compaction, and tool result persistence.
- [x] Find missing remote-vs-loopback parity tests.
- [x] Find missing authorization tests for every RPC scope policy.
- [x] Find missing validation tests for every public request/CLI/config/tool schema.
- [x] Find tests with large fixture setup duplicated across packages.
- [x] Find golden/snapshot tests whose update process is unclear.
- [x] Find test helpers in production packages that should move to `*_test.go` or shared test packages.
- [x] Find slow tests that block regular development feedback.

## Performance And Scalability

- [x] Find O(n) or worse operations on unbounded transcripts/events per UI frame or request.
- [x] Find repeated full transcript projection where incremental/tail projection is required.
- [x] Find repeated token counting of unchanged history.
- [x] Find repeated config loads or filesystem scans on hot paths.
- [x] Find repeated RPC handshakes/connections for ordinary interactive calls.
- [x] Find subprocess spawning in status/UI refresh loops.
- [x] Find database queries lacking pagination or indexes.
- [x] Find memory retained by long sessions, background output, render caches, or transcript snapshots.
- [x] Find large strings copied repeatedly in render/model/runtime paths.
- [x] Find logs/tool outputs kept in memory after persisted.
- [x] Find unbounded goroutine/channel growth with many sessions/processes/subscribers.
- [x] Find startup paths scanning too many files synchronously.
- [x] Find generated asset sync that does more work than necessary every startup.
- [x] Find model/request payload construction that defeats provider cache hits.
- [x] Find UI rendering that recomputes static style/layout every frame.

## Maintainability And Code Quality

- [x] Find functions with high branch count or deeply nested control flow.
- [x] Find files with multiple unrelated type clusters.
- [x] Find large switch statements over domain actions where registry/table dispatch fits.
- [x] Find magic numbers for sizes, timeouts, debounce, ports, limits, and thresholds.
- [x] Find bool parameters that obscure call-site intent.
- [x] Find stringly typed IDs, modes, states, severities, and roles.
- [x] Find maps of `string` to `any` or generic JSON payloads where typed structs should exist.
- [x] Find manual parsing of command output, error text, URLs, JSON, TOML, or protocol frames when typed APIs exist.
- [x] Find regex-based logic used for semantic decisions.
- [x] Find comments explaining what code does instead of encoding clearer structure.
- [x] Find comments that contradict behavior.
- [x] Find dead code, unused seams, obsolete compatibility branches, or outdated docs.
- [x] Find packages with inconsistent naming conventions.
- [x] Find import aliases that hide ownership or domain intent.
- [x] Find repeated helper functions that should be shared.
- [x] Find shared helper packages that became dumping grounds.
- [x] Find side effects in constructors beyond validation/wiring.
- [x] Find mutation-heavy code where immutable projections/results would reduce bugs.
- [x] Find poor error messages from internal packages surfacing directly to users.
- [x] Find cases where code quality depends on tribal knowledge rather than tests/types/docs.

## Documentation And Product Consistency

- [x] Find docs that describe old persistence, startup, config, tool, or auth behavior.
- [x] Find user-visible features lacking docs.
- [x] Find breaking behavior lacking migration notes.
- [x] Find internal architecture decisions not captured in `docs/dev/decisions.md`.
- [x] Find prompts that encode product decisions absent from docs/decisions.
- [x] Find README/docs mismatch with CLI help.
- [x] Find config docs mismatch with generated defaults.
- [x] Find release scripts/plumbing undocumented for maintainers.
- [x] Find tech debt items already fixed but still documented as open.
- [x] Find TODO-like knowledge living only in tests or comments.

## Build, Release, And Repo Hygiene

- [x] Find generated files committed without clear generation command.
- [x] Find generated code not checked by CI for freshness.
- [x] Find scripts with duplicated version/platform/signing/archive logic.
- [x] Find release workflow assumptions not represented in local scripts.
- [x] Find install scripts that diverge by platform.
- [x] Find build flags/version injection duplicated across scripts and workflows.
- [x] Find CI checks that miss local required checks.
- [x] Find repo files that should be ignored or cleaned from version control.
- [x] Find dependency updates without compatibility tests.
- [x] Find vendored/generated/test fixtures that bloat repo or confuse search.
- [x] Find docs/tmp scratch artifacts that should be promoted, archived, or deleted.

## Prioritized Initial Search Backlog

- [x] Audit RPC endpoint duplication from protocol constants to gateway dispatch to remote clients.
- [x] Audit all production files over 600 LoC and classify why they are large.
- [x] Audit package import fan-in/fan-out and identify god packages.
- [x] Audit `cli/app` responsibilities and split candidates.
- [x] Audit `server/core` composition/service-locator surface.
- [x] Audit `server/runtime` engine state ownership and collaborator boundaries.
- [x] Audit `cli/tui` render/state cache graph.
- [x] Audit `shared/config` setting registry source-of-truth and modularization options.
- [x] Audit persistence authority between SQLite, files, and in-memory runtime state.
- [x] Audit live-vs-dormant session read models.
- [x] Audit transport attach/scope/auth semantics.
- [x] Audit package-level mutable globals and test seams.
- [x] Audit stringly typed IDs/states/modes and semantic regex/string parsing.
- [x] Audit unbounded history reads and memory retention.
- [x] Audit goroutine/channel ownership across runtime, registry, process output, and transport.
- [x] Audit all public request/config/tool validation paths.
- [x] Audit startup/headless/interactive server discovery divergence.
- [x] Audit generated assets and skills ownership/recovery behavior.
- [x] Audit auth/OAuth/device/browser flow typedness and secret handling.
- [x] Audit docs drift against current behavior.
