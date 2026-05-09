# Tech Debt Research Progress

This file tracks slice completion for `docs/dev/techdebt/research_prd.md` so future handoffs can resume without re-auditing finished slices.

## Working Rules Snapshot

- Research only; do not edit production code unless user changes scope.
- Add only verified findings to `docs/dev/techdebt/techdebt.md`.
- Keep entries in PRD block shape: checklist title, evidence paragraph, impact paragraph, remediation task paragraph, regression-prevention paragraph.
- Use repo-relative paths only.
- Check `docs/dev/decisions.md` before finalizing each remediation task.

## Slice Status

### Phase 1: Highest-Signal Architecture Debt

- [x] RPC endpoint source-of-truth and route policy drift. Cataloged as `TD-001` and `TD-002`.
- [x] God packages and god files. Cataloged as `TD-003`, `TD-004`, `TD-006`, `TD-007`, and `TD-008`.
- [x] `cli/app` responsibility boundaries and server leakage. Cataloged as `TD-003`.
- [x] `server/core` service-locator composition surface. Cataloged as `TD-004`.
- [x] `server/runtime` state ownership and runtime subdomain boundaries. Cataloged as `TD-006`.
- [x] `cli/tui` transcript/render/cache graph. Cataloged as `TD-005`.
- [x] `shared/config` registry source-of-truth and modularity. Cataloged as `TD-007`.

### Phase 2: Boundary And Ownership Debt

- [x] `cli`, `server`, and `shared` dependency direction violations. Cataloged as `TD-009`.
- [x] `shared/*` packages that contain server-owned or CLI-owned behavior. Cataloged as `TD-010` and `TD-011`.
- [x] Client-facing DTO, server API, protocol, and runtime-native model overlap. Cataloged as `TD-010`, `TD-011`, and existing `TD-001`.
- [x] Embedded/local versus remote/server semantic divergence. Cataloged as `TD-013`.
- [x] Command/query/read-model ownership confusion. Cataloged as `TD-012`.
- [x] Compatibility shims without explicit migration path. Cataloged as `TD-014`.

## Current Slice

Current slice: complete.

Running subagents for Phase 3:

- Session `1868`: state machines, validation, typed errors.
- Session `1870`: partial failure, idempotency, crash/resume/interrupt, transcript/cache invariants.
- Session `1869`: path canonicalization, workspace authorization, unbounded reads. Failed with provider 429 before reporting; local tool path audit covered the Phase 3 path criterion.

Main-agent Phase 3 findings already added:

- `TD-015`: parallel boolean lifecycle states.
- `TD-016`: process-local temporary `client_request_id` idempotency.
- `TD-017`: unbounded session event-log materialization.
- `TD-018`: optional RPC validation and incomplete typed error taxonomy.
- `TD-019`: session event append and metadata persistence are not one crash-safe transaction.
- `TD-020`: background shell output streaming lacks a bounded chunk/backpressure contract.
- `TD-021`: async and remote tests rely on sleep/poll deadlines.
- `TD-022`: `server/session.Store` holds its global mutex across filesystem I/O.
- `TD-023`: test seams and startup collaborators are mutable package globals.
- `TD-024`: client and UI async helpers use ad hoc goroutine lifecycles without owned cancellation and join contracts.
- `TD-025`: storage migration cutover is not resumable after interruption.
- `TD-026`: session repair and discovery paths hide corrupted persistence from operators.
- `TD-027`: metadata list APIs return unbounded result sets without cursor contracts.
- `TD-028`: tool contracts and transcript formatting still require parallel per-tool edits.
- `TD-029`: status overlay mixes UI presentation with network, auth, git, filesystem, and token collection.
- `TD-030`: project and workspace picker flows duplicate picker mechanics behind mutable globals.
- `TD-031`: root CLI command handling combines parsing, usage text, runners, IO, and signal policy.
- `TD-032`: daemon RPC transport has no client-authenticated control boundary.
- `TD-033`: auth readiness and bootstrap policy is split across startup, health, gateway, bootstrap service, and clients.
- `TD-034`: outside-workspace approval audit trails are inconsistent across file tools.
- `TD-035`: large scenario test files hide behavioral boundaries and slow focused feedback.
- `TD-036`: public docs and README drift from runtime tool, command, config, and security contracts.
- `TD-037`: release, generated-code, and artifact freshness contracts are duplicated or unenforced across scripts, CI, installers, generated outputs, and Homebrew plumbing.

Phase 4 subagents:

- Session `3020`: goroutine ownership, cancellation, close/wait semantics, channels/subscriptions/streams/fan-out/backpressure. Failed with provider 429 before reporting; local audit covered this slice.
- Session `3022`: lock scope/order, package globals/test seams, timing-based tests. Still running at last checkpoint; local audit already cataloged `TD-021`, `TD-022`, and `TD-023`.
- Session `3021`: background process lifecycle, owner-session semantics, runtime lease/reconnect/release races. Failed with provider 429 before reporting; local audit covered background output/log lifecycle and owner-session decision constraints.
- Restarted failed Phase 3/4 subagents as sessions `1004`, `1005`, `1006`, and `1007` after provider model switch. All completed read-only. `1005` strengthened `TD-021`/`TD-022`/`TD-023`; `1006` confirmed runtime lease/reconnect/release coverage and no new background-owner finding; `1004` confirmed path canonicalization looked guarded and remote/rpcwire lifecycle fit `TD-024`; `1007` confirmed stream/close models and tests, with no new finding beyond `TD-020`/`TD-024`.

Next concrete action: complete the active goal.

### Phase 3: Correctness And State Invariants

- [x] State machines encoded as booleans or invalid struct combinations. Cataloged as `TD-015`.
- [x] Missing validation at CLI, RPC, tool, config, persistence, env, filesystem, model-response boundaries. Cataloged as `TD-018`; tool filesystem path validation was audited and no new Phase 3 finding was added because current patch/edit/view-image guards canonicalize real paths and approval-gate outside-workspace access.
- [x] Error handling that loses typed policy or actionable context. Cataloged as `TD-018`.
- [x] Partial failure and atomicity risks across multi-resource writes. Cataloged as `TD-019`.
- [x] Idempotency and request-deduplication gaps. Cataloged as `TD-016`.
- [x] Crash/restart/resume/interrupt behavior divergence. Covered by `TD-015`, `TD-016`, `TD-019`, and existing runtime/session recovery tests; no separate higher-signal Phase 3 finding verified.
- [x] Transcript immutability, compaction, fork, and cache-continuity invariants. Covered by `TD-014`, `TD-015`, `TD-017`, and existing cache/compaction/fork tests; no separate higher-signal Phase 3 finding verified.
- [x] Path canonicalization, symlink, `..`, case-sensitivity, and workspace authorization edge cases. Tool file guards were audited in `server/tools/fsguard`, `server/tools/patch`, `server/tools/edit`, and `server/tools/readimage`; no separate Phase 3 finding verified beyond future Phase 7/9 tool-safety review.
- [x] Unbounded file/history/memory reads. Cataloged as `TD-017`.

### Phase 4: Concurrency And Lifecycle Debt

- [x] Goroutine ownership, cancellation, and close/wait semantics. Cataloged as `TD-024`; `server/serve.Server` and `server/tools/shell.Manager` have explicit wait paths for their primary server/process goroutines, but client/UI helper goroutines still lack consistent owner cancellation and join contracts.
- [x] Channels, subscriptions, streams, fan-out, and backpressure. Cataloged as `TD-020`; runtime event bridge drops are currently paired with gap/hydration behavior and were not split into a separate finding.
- [x] Lock scope and lock-ordering risks. Cataloged as `TD-022` for session store lock scope; no broader cross-package lock-order cycle verified yet.
- [x] Package globals and test seams. Cataloged as `TD-023`.
- [x] Background process lifecycle and owner-session semantics. Cataloged as `TD-020` for output/log lifecycle; owner-session semantics are constrained by the explicit v1 decision that background process ids are app-global and owner session metadata is advisory.
- [x] Runtime lease/reconnect/release races. Broad review found substantial takeover/release/reconnect coverage in `server/sessionruntime/service_test.go`, `shared/client/remote_transport_test.go`, and `cli/app/controller_lease_test.go`; no separate higher-signal finding verified beyond lifecycle test debt in `TD-021` and async ownership debt in `TD-024`.
- [x] Timing-based tests and sleep-driven behavior. Cataloged as `TD-021`.

### Phase 5: Persistence And Migration Debt

- [x] SQLite/file-backed authority overlap. Covered by `TD-019`, which now explicitly includes live runtime state, file-backed `events.jsonl`/session artifacts, and SQLite metadata snapshot authority; existing `TD-012` covers live/dormant read authority.
- [x] Migration idempotency and interrupted-run recovery. Cataloged as `TD-025`.
- [x] Corrupt session/event-log handling and observability. Cataloged as `TD-026`.
- [x] Event schema versioning and append-only rewrite safety. Covered by `TD-014` for schema/version ownership and `TD-019` for event-log rewrite crash safety.
- [x] Database queries, indexes, pagination, and scaling. Cataloged as `TD-027`; existing schema has key indexes, but public list contracts are unbounded.
- [x] Metadata cache drift. Covered by `TD-019` and `TD-012`; no separate cache-drift family verified beyond split live/durable commit/read authorities.
- [x] Generated/recovered/temp state ownership and cleanup. Covered by `TD-020` for background temp logs and by product decisions for generated/recovered assets; no separate Phase 5 finding verified.

### Phase 6: Transport, API, And Protocol Debt

- [x] JSON-RPC error mapping and typed client behavior. Cataloged as `TD-018`; client/server mappings are sentinel-based and incomplete.
- [x] Attach-state semantics across unary and subscription calls. Covered by `TD-001` and `TD-002`; `server/transport/gateway.go` still applies attach/session/project policy through branch-specific helpers despite route metadata.
- [x] Request validation enforcement by route contract. Cataloged as `TD-018`.
- [x] Per-request connection setup versus long-lived multiplexed transport. Re-audit found `shared/client.Remote` now keeps one persistent control connection and route metadata declares dedicated/subscription strategies; remaining lifecycle concerns are covered by `TD-024` and route-source concerns by `TD-001`.
- [x] Subscription contract consistency. Covered by `TD-001`, `TD-002`, `TD-020`, and `TD-024`; session/prompt/process subscriptions still have hand-written gateway/client loops and process output lacks bounded stream semantics.
- [x] Wire contract versioning and enum/field validation. Covered by `TD-014` for versioned compatibility ownership and `TD-018` for field/enum validation and unknown error mapping.

### Phase 7: Runtime, Model, Tools, And Transcript Debt

- [x] Model request assembly and prompt/tool message structure. Covered by `TD-006`; `server/runtime/engine_request.go` still coordinates locked contract, prompt construction, transcript items, tool exposure, native web search gating, cache key, and provider capability checks through the engine.
- [x] Transcript roles, tool metadata, and legacy parsing/fallbacks. Covered by `TD-014` and `TD-028`.
- [x] Compaction policy versus execution separation. Covered by `TD-006`; `server/runtime/compaction.go` still mixes policy thresholds, token counting, provider capability, handoff, model execution, and persistence orchestration.
- [x] Reviewer pipeline duplication. Covered by `TD-006`; `server/runtime/reviewer_pipeline.go` still builds reviewer requests and invokes a follow-up step loop with reviewer-specific recursion suppression.
- [x] Goal loop and queue/runtime lifecycle integration. Covered by `TD-015` for lifecycle booleans and `TD-006` for runtime state ownership.
- [x] Tool definitions, schemas, handlers, transcript renderers, docs, and validation source-of-truth. Cataloged as `TD-028`.
- [x] Tool filesystem safety and approval consistency. Local and subagent audits found patch/edit/view-image/fsguard path canonicalization guarded; no separate Phase 7 finding verified beyond `TD-018` validation taxonomy.
- [x] Shell output truncation/post-processing and background session ownership. Covered by `TD-020`; shell command semantic post-processing remains a specialized tool-formatting concern also touched by `TD-028`.

### Phase 8: TUI, UX State, And CLI Startup Debt

- [x] TUI submodels, render mutation, dirty flags, scrollback immutability, and flicker. Covered by `TD-005`, with timing/native-scrollback test debt also covered by `TD-021`.
- [x] Overlay/picker/list/key handling duplication. Cataloged as `TD-030`.
- [x] Status overlay data acquisition versus presentation. Cataloged as `TD-029`.
- [x] UI side effects: filesystem, network, auth, git, process execution. Cataloged as `TD-029` and broadly covered by `TD-003` for `cli/app` server/side-effect leakage.
- [x] Root command parsing, usage text, runners, signal handling, and package globals. Cataloged as `TD-031`, with mutable test seams also covered by `TD-023`.
- [x] Headless versus interactive startup target resolution. Covered by `TD-013`.
- [x] Daemon discovery/launch/fallback duplication. Covered by `TD-013`.

### Phase 9: Config, Auth, Security, And Privacy Debt

- [x] Config defaults, env/TOML/CLI precedence, source reporting, inheritance, and generated docs. Covered by `TD-007` for registry/source-of-truth debt and `TD-018` for source-aware validation. Re-audit found current precedence and source reporting covered by `shared/config` tests; no separate Phase 9 finding verified.
- [x] Auth readiness duplication. Cataloged as `TD-033`.
- [x] OAuth/browser/device string heuristics and typed error handling. Covered by `TD-018`; `server/auth/openai_oauth_browser.go` still parses pasted callback input by string shape and auth/OAuth flows still surface generic errors, but ownership/remediation is validation/error taxonomy rather than a separate auth-only task.
- [x] Secret logging and privacy risks. Re-audit found auth state file permissions and token exchange errors avoid response-body logging; status quota auth is already covered by `TD-029`. No separate secret-logging finding verified beyond `TD-032` daemon control exposure and `TD-034` sensitive-action audit gaps.
- [x] Local daemon exposure and process/session authorization. Cataloged as `TD-032`; process stream/output lifecycle remains covered by `TD-020`, and advisory owner-session metadata remains intentional per decision.
- [x] Filesystem guard bypasses and audit trails for sensitive actions. Guard bypass surfaces were already audited in Phases 3 and 7; audit-trail inconsistency is cataloged as `TD-034`.

### Phase 10: Tests, Performance, Docs, Build, And Release Debt

- [x] Packages with business logic but weak/no tests. `go list` found no-test packages mostly small/generated/support packages; no separate high-signal package-coverage finding verified. Test maintainability and harness shape are cataloged as `TD-035`.
- [x] Test brittleness, package global mutation, broad fixtures, missing race/crash/remote parity coverage. Covered by `TD-021`, `TD-023`, `TD-035`, and earlier persistence/transport findings where regression tests are explicitly required.
- [x] Hot-path unbounded work: transcript projection, token counting, config loads, filesystem scans, RPC handshakes, subprocess status refresh. Covered by `TD-005`, `TD-017`, `TD-020`, `TD-027`, `TD-029`, and `TD-033`; no separate Phase 10-only hot path family verified.
- [x] Memory retention from transcripts, logs, background output, render caches. Covered by `TD-005`, `TD-017`, `TD-020`, and `TD-027`.
- [x] Docs drift against CLI help, generated config, prompts, and decisions. Cataloged as `TD-036`, with tool docs/source-of-truth overlap also covered by `TD-028` and config registry/docs overlap by `TD-007`.
- [x] Generated code freshness, scripts, release workflow, install scripts, CI coverage, and repo hygiene. Cataloged as expanded `TD-037`; generated SQLite/code/docs freshness is now explicitly part of that finding, while generated asset ownership/recovery behavior remains covered by decisions and earlier audits.

### Scratch Note Reconciliation

- [x] `cli/tui` render cache graph from `docs/tmp/tech_debt.md` is cataloged as `TD-005`.
- [x] RPC route/client/gateway duplication is cataloged as `TD-001` and `TD-002`; the old per-call WebSocket note is no longer current because `shared/client.Remote` now maintains a persistent control connection.
- [x] Embedded/startup attachment surface is cataloged as `TD-013` and broadly supported by `TD-003`.
- [x] Runtime registry responsibility mixing is folded into `TD-006`, including runtime lookup, primary-run leasing, session/prompt activity fan-out, pending prompt storage, and prompt response routing in `server/registry/runtime_registry.go`.
- [x] Live-versus-dormant session view branching is cataloged as `TD-012`.
- [x] Config registry modularity is cataloged as `TD-007`.
- [x] Mutable package-level seams are cataloged as `TD-023`, with status, picker, and root-command specializations cataloged as `TD-029`, `TD-030`, and `TD-031`.
- [x] Status overlay data acquisition is cataloged as `TD-029`.
- [x] Onboarding import monolith is covered by the broader `cli/app` responsibility finding `TD-003`; no separate finding was added because the remediation belongs to the same frontend subdomain extraction.
- [x] Project/workspace picker duplication is cataloged as `TD-030`.
- [x] Root CLI command dispatch, help, IO, signal, and runner seams are cataloged as `TD-031`.
- [x] Tool transcript formatting and parallel per-tool edits are cataloged as `TD-028`.
- [x] Compaction policy/execution mixing is folded into runtime ownership `TD-006`.
- [x] Legacy transcript and reviewer compatibility shims are cataloged as `TD-014` and tool transcript compatibility is also covered by `TD-028`.
- [x] OAuth/browser string heuristics are covered by `TD-018`; no separate auth-only finding was added because the verified root issue is boundary validation and typed error taxonomy.
- [x] Session repair and damaged persistence observability is cataloged as `TD-026`.
- [x] Package-init and constructor panics in command, tool, and provider registries are folded into `TD-018` as extensibility-boundary validation that should surface through startup/composition diagnostics.
- [x] Remote per-call WebSocket dialing was rejected as stale after Phase 6 re-audit; persistent control connection behavior is current, and remaining lifecycle concerns are covered by `TD-024`.

### Decisions Conflict Pass

- [x] `TD-016` keeps durable `client_request_id` dedup framed as deferred route-specific semantics, matching the hard-cut decision.
- [x] `TD-020` preserves app-global background process IDs and advisory owner metadata, and limits remediation to bounded output/backpressure and temp-log lifetime.
- [x] `TD-025` asks for an idempotent/resumable cutover or typed recovery diagnostic, not a hidden compatibility shim.
- [x] `TD-032` preserves explicit TCP/remote/container support and adds client authentication/capabilities instead of banning remote mode.
- [x] `TD-034` keeps sensitive-action audit operator-visible and does not make approval payloads model-visible.
- [x] `TD-036` ties security-doc remediation to the future `TD-032` client-auth boundary.

### Reviewer Weak-Criterion Reconciliation Pass

- [x] `C018`, `C022`, `C023`, `C027`, `C033`, `C034`, and `C037` were reconciled against direct architecture reads. Evidence anchors: `go list -json ./cli/... ./server/... ./shared/...` showed `builder/cli/app` with 119 Go files and 38 internal imports and `builder/server/core` with one file and 38 internal imports; direct reads included `server/core/core.go` `Core`, `server/runtime/engine.go` `Engine`, `shared/serverapi/run_prompt.go` `PromptService`, `shared/clientui/runtime_events.go` `ReduceRuntimeEvent`, and `server/sessionview/service.go` `GetSessionMainView`.
- [x] `C046`, `C048`, `C050`, `C053`, and `C059` were reconciled against source-of-truth duplication reads. Evidence anchors: `shared/protocol/handshake.go` `MethodRuntimeSubmitUserMessage`, `shared/rpccontract/routes.go` `routeContracts`, `shared/serverapi/runtime_control.go` `RuntimeSubmitUserMessageRequest.Validate`, `shared/client/remote.go` `SubmitUserMessage`, `server/transport/gateway.go` `dispatch`, `cli/app/session_server_target.go` `startSessionServer`, and `cli/app/run_prompt_target.go` `startRunPromptClient`.
- [x] `C067`, `C068`, `C071`, `C077`, `C078`, `C080`, `C084`, `C085`, `C086`, and `C087` were reconciled against correctness, recovery, and guard reads. Evidence anchors: `server/transport/gateway.go` `protocolError`, `shared/client/remote_rpc.go` `protocolError`, `server/runtime/engine_message_ops.go` `appendMessage`, `server/session/store.go` `AppendEvent`, `server/session/event_log.go` `bootstrapEventLogStateLocked`, `server/tools/fsguard/guard.go` `Authorize`, and `server/tools/patch/outside_workspace_guard.go` `outsideWorkspaceApprovalRequired`.
- [x] `C095`, `C097`, `C099`, `C103`, `C105`, `C108`, `C109`, and `C110` were reconciled against concurrency and transport lifecycle reads. Evidence anchors: `server/session/store.go` `Store.mu`, `server/tools/shell/manager.go` `Manager`, `server/tools/shell/manager_output.go` `outputSubscription.Next`, `shared/client/remote_rpc.go` `readLoop`, `shared/client/remote_stream.go` `processOutputSubscription.Next`, `server/registry/runtime_registry.go` `RuntimeRegistry`, and `shared/rpccontract/routes.go` `ConnectionStrategy`.
- [x] `C121`, `C123`, and `C126` were reconciled against persistence authority reads. Evidence anchors: `server/metadata/queries.sql` `ListSessionsByProject`, `server/metadata/store.go` `upsertSessionSnapshot` and `sessionMetaFromRecordRow`, `server/session/store.go` `persistMetaLocked`, `server/session/snapshot.go` `SnapshotFromStore`, `server/storagemigration/projectv1.go` `EnsureProjectV1`, and `server/tools/shell/manager.go` background log temp-dir handling.
- [x] `C139`, `C141`, `C142`, `C143`, and `C145` were reconciled against transport and protocol reads. Evidence anchors: `shared/client/remote.go` `Remote`, `shared/client/remote_rpc.go` `callRPC`, `shared/client/remote_stream.go` subscription types, `shared/client/remote_contract_test.go` `TestRemoteClientRoutesMatchRPCContractConnectionStrategy`, `shared/rpccontract/routes.go` `Route`, and `server/transport/gateway.go` `serveSubscription`.
- [x] `C149`, `C150`, `C151`, `C156`, `C157`, `C158`, `C159`, `C160`, and `C165` were reconciled against runtime/model/transcript reads. Evidence anchors: `server/runtime/engine_request.go` `buildRequestPlan`, `server/runtime/compaction.go` `runCompaction`, `server/runtime/reviewer_pipeline.go` reviewer pipeline functions, `server/runtime/goal.go` goal loop functions, `server/runtime/transcript_projector.go` transcript projection, `server/tools/transcript_contracts.go` tool transcript formatting, and `shared/cachewarn/cachewarn.go` `ReasonCompaction`.
- [x] `C170`, `C171`, `C172`, `C176`, `C177`, and `C178` were reconciled against tool and filesystem reads. Evidence anchors: `server/tools/definitions.go` `catalogEntries`, `shared/toolspec/toolspec.go` tool aliases, `server/tools/types.go` `Registry`, `server/tools/fsguard/guard.go` `Authorize`, `server/tools/patch/tool_apply.go` patch application, `server/tools/askquestion/tool.go` action payload flow, `server/tools/readimage/tool.go` image handling, and `server/runtimewire/tool_registry.go` tool registry wiring.
- [x] `C190`, `C192`, `C193`, `C194`, and `C197` were reconciled against UI/TUI reads. Evidence anchors: `cli/tui/model.go` `Model`, `cli/tui/model_reducer.go` `Update`, `cli/tui/model_rendering.go` render paths, `cli/tui/transcript_projection.go` projection cache, `cli/app/ui_status.go` status collection, `cli/app/ui_worktrees.go` worktree UI, `cli/app/ui_path_reference_search.go` path reference worker, and native scrollback tests under `cli/app`.
- [x] `C210` and `C211` were reconciled against config and environment reads. Evidence anchors: `shared/config/config_registry.go` `SettingsRegistry`, `shared/config/config_load.go` `Load`, `shared/config/config_validation.go` `Validate`, `cli/builder/main.go` root flags, and `docs/src/content/docs/config.md`.
- [x] `C218`, `C219`, `C220`, and `C223` were reconciled against CLI command and startup reads. Evidence anchors: `cli/builder/main.go` root command and signal handling, `cli/builder/binding_commands.go` binding mutations, `cli/builder/service_types.go` command seams, `cli/app/session_server_target.go` `startSessionServer`, `cli/app/run_prompt_target.go` `startRunPromptClient`, `cli/app/headless_prompt_server.go`, and `cli/app/embedded_server.go`.
- [x] `C225`, `C226`, `C227`, `C231`, `C233`, and `C235` were reconciled against auth/security reads and negative searches. Evidence anchors: `server/auth/openai_oauth.go` OAuth flow, `server/authflow/authflow.go` auth readiness, `server/authbootstrap/service.go` bootstrap service, `server/serve/serve.go` daemon serving, gateway pre-auth routes in `server/transport/gateway.go`, and auth/OAuth tests using synthetic credentials rather than real token fixtures.
- [x] `C236`, `C237`, `C242`, `C243`, `C249`, and `C250` were reconciled against test harness reads. Evidence anchors: `go list -json ./...` test-file coverage, test size scan via `find ... -name '*_test.go' | xargs wc -l`, `rg` searches for `time.Sleep` and `time.After`, `cli/app/session_server_target_part3_test.go` wait helpers, `shared/client/remote_transport_test.go` reconnect tests, and `server/transport/gateway_test.go` route/auth tests.
- [x] `C254`, `C256`, `C260`, `C264`, `C265`, and `C266` were reconciled against performance/scalability reads. Evidence anchors: `server/runtime/compaction.go` token/compaction policy, `server/runtime/engine_request.go` request assembly, `server/session/store.go` `ReadEvents`, `server/tools/shell/manager_output.go` `io.ReadAll`, `cli/tui/model.go` render caches, `cli/app/ui_status.go` side-effectful status refresh, and `server/generated/generated.go` generated asset sync.
- [x] `C270`, `C271`, `C273`, `C275`, `C276`, `C277`, `C279`, `C280`, `C282`, `C283`, and `C286` were reconciled against maintainability reads. Evidence anchors: large-file scan showing `server/transport/gateway.go`, `shared/config/config_registry.go`, `cli/app/ui_worktrees.go`, and `cli/builder/main.go`; direct reads of `server/tools/transcript_contracts.go`, `server/llm/provider_factory.go`, `cli/app/commands/commands.go`, and `server/tools/shellcmd/parser.go`.
- [x] `C290`, `C291`, `C295`, and `C296` were reconciled against docs/product reads. Evidence anchors: `README.md`, `docs/src/content/docs/config.md`, `docs/src/content/docs/slash-commands.md`, `docs/src/content/docs/server.md`, `docs/src/content/docs/sandboxing.md`, `prompts/system_prompt.md`, `docs/dev/decisions.md`, and `docs/tmp/tech_debt.md`.
- [x] `C297`, `C298`, `C303`, `C304`, `C305`, `C306`, and `C307` were reconciled against build/release/repo reads. Evidence anchors: `scripts/build.sh`, `scripts/ci-check.sh`, `scripts/release-artifacts.sh` `release_targets`, `scripts/install.sh`, `scripts/install.ps1`, `scripts/update-brew-tap.sh`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `docs/dev/release.md`, and `docs/tmp/tech_debt.md` scratch reconciliation.
- [x] `C325` and `C326` were reconciled against backlog-specific reads. Evidence anchors: generated asset ownership in `server/generated/generated.go` plus decisions in `docs/dev/decisions.md`; auth/OAuth/browser/device typedness and secret handling in `server/auth/*`, `server/authflow/authflow.go`, `server/authbootstrap/service.go`, `server/authpolicy/policy.go`, `cli/app/auth_gate.go`, `cli/app/remote_auth_bootstrap.go`, `server/serve/serve.go`, and gateway pre-auth route handling.

### Reviewer Objection Replay Table

| Criterion | Status | Exact command/output ref | Resolution |
| --- | --- | --- | --- |
| `C016` | evidenced/catalog-mapped finding | `INV-LARGE-FILES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C309` | evidenced/catalog-mapped finding | `INV-LARGE-FILES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C018` | evidenced/catalog-mapped finding | `INV-PACKAGE-FANOUT` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C022` | evidenced negative/no-separate finding | `INV-PACKAGE-FANOUT` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C027` | evidenced/catalog-mapped finding | `INV-PACKAGE-FANOUT`, `INV-SERVER-CLIENTUI-IMPORTS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C033` | evidenced/catalog-mapped finding | `INV-PACKAGE-FANOUT`, `INV-INTERFACES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C034` | evidenced/catalog-mapped finding | `INV-PACKAGE-FANOUT`, `INV-INTERFACES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C046` | evidenced/catalog-mapped finding | `techdebt_research_proof.md` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C053` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C067` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C068` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C071` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C095` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C103` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C105` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C108` | evidenced negative/no-separate finding | `INV-TRANSPORT-FAKES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C109` | evidenced negative/no-separate finding | `INV-TRANSPORT-FAKES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C110` | evidenced negative/no-separate finding | `INV-TIMING-TESTS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C121` | evidenced negative/no-separate finding | `techdebt_research_proof.md` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C123` | evidenced/catalog-mapped finding | `techdebt_research_proof.md` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C126` | evidenced negative/no-separate finding | `techdebt_research_proof.md` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C141` | evidenced/catalog-mapped finding | `INV-TRANSPORT-FAKES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C142` | evidenced/catalog-mapped finding | `INV-JSON-TAGS`, `INV-TRANSPORT-FAKES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C145` | evidenced/catalog-mapped finding | `INV-JSON-TAGS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C149` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C150` | evidenced/catalog-mapped finding | `INV-JSON-TAGS`, `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C151` | evidenced/catalog-mapped finding | `INV-JSON-TAGS`, `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C156` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C157` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C160` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C165` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C170` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C171` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C172` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C176` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C177` | evidenced/catalog-mapped finding | `INV-JSON-TAGS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C178` | evidenced/catalog-mapped finding | `INV-JSON-TAGS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C193` | evidenced/catalog-mapped finding | `techdebt_research_proof.md` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C194` | evidenced/catalog-mapped finding | `techdebt_research_proof.md` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C197` | evidenced/catalog-mapped finding | `INV-TEST-COVERAGE`, `INV-LARGE-TESTS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C203` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C209` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C210` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C211` | evidenced/catalog-mapped finding | `INV-TIMING-TESTS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C218` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C219` | evidenced/catalog-mapped finding | `INV-INTERFACES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C220` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C223` | evidenced/catalog-mapped finding | `INV-TIMING-TESTS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C225` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL`, `INV-AUTH-SECURITY-NEGATIVE` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C226` | evidenced negative/no-separate finding | `INV-AUTH-SECURITY-NEGATIVE` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C227` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL`, `INV-AUTH-SECURITY-NEGATIVE` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C231` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL`, `INV-AUTH-SECURITY-NEGATIVE` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C233` | evidenced/catalog-mapped finding | `INV-AUTH-SECURITY-NEGATIVE` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C235` | evidenced negative/no-separate finding | `INV-AUTH-SECURITY-NEGATIVE` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C236` | evidenced/catalog-mapped finding | `INV-TEST-COVERAGE` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C237` | evidenced negative/no-separate finding | `INV-TEST-COVERAGE`, `INV-LARGE-TESTS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C242` | evidenced/catalog-mapped finding | `INV-TEST-COVERAGE`, `INV-LARGE-TESTS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C243` | evidenced/catalog-mapped finding | `INV-TEST-COVERAGE`, `INV-TIMING-TESTS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C249` | evidenced negative/no-separate finding | `INV-TEST-COVERAGE`, `INV-LARGE-TESTS` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C250` | evidenced/catalog-mapped finding | `INV-INTERFACES`, `INV-TEST-COVERAGE` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C254` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C260` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C264` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C265` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C266` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C270` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C271` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C273` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C275` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C276` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C277` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C279` | evidenced/catalog-mapped finding | `INV-PACKAGE-FANOUT` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C280` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C282` | evidenced/catalog-mapped finding | `INV-PACKAGE-FANOUT` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C283` | evidenced/catalog-mapped finding | `INV-INTERFACES` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C286` | evidenced/catalog-mapped finding | `INV-PACKAGE-FANOUT` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C290` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C291` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C295` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C296` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C297` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C303` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C304` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C305` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C306` | evidenced negative/no-separate finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C325` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |
| `C326` | evidenced/catalog-mapped finding | `INV-STRING-REGEX-MAP-BOOL`, `INV-AUTH-SECURITY-NEGATIVE` | See matching paragraph in `docs/dev/techdebt/techdebt_research_proof.md`; replayed after user objection and backed by listed inventory output. |

### Final Documentation Verification

- [x] `docs/dev/techdebt/techdebt.md` uses checklist entry titles plus evidence, impact, remediation, and regression-prevention paragraphs; no pseudo-YAML fields were found.
- [x] `docs/dev/techdebt/research_prd.md` research-sequence checklists are fully checked.
- [x] `docs/dev/techdebt/techdebt_criteria.md` has 327 checked criteria, and `docs/dev/techdebt/techdebt_research_proof.md` has 327 matching proof paragraphs in the same order.
- [x] `docs/dev/techdebt/techdebt_research_proof.md` has no duplicate proof paragraphs, no duplicate Method/Evidence fields, no missing repo-relative source refs, no generic fallback phrases, no missing criterion-specific evidence tokens, and no negative/no-finding claims without recorded negative-search support; every proof paragraph has explicit `Method:`, `Evidence:`, and `Outcome:` sections.
- [x] Targeted reviewer-ID replay scan covered all 87 rows in the objection table: every named `C###` had a proof block, `Method:`/`Evidence:`/`Outcome:` markers, and live repo-relative path anchors after replacing stale symbol/glob tokens such as `cli/actions.Registry.Execute`, `server/core.NewWithContext`, `shared/client.Remote`, `prompts/skills/*/SKILL.md`, and `server/auth/*` with concrete files.
- [x] `docs/dev/techdebt/research_inventories.md` stores durable output excerpts for large-file, package fan-out, server/clientui import, interface, JSON-tag, test coverage, timing-test, transport fake, string/regex/map/bool, and auth/security negative-search inventories.
- [x] `docs/dev/techdebt/verify_research.py` now reruns live drift checks for production files over 600 LoC, every listed package fan-out count, server `shared/clientui` imports, the full `shared/serverapi` request/response JSON-tag scan, and transport fake-server file/line anchors; the latest live large-file scan corrected `server/transport/gateway.go` to 1293 LoC. Exact freshness anchor: `wc -l server/transport/gateway.go` returned `1293 server/transport/gateway.go`, and `find cli server shared -name '*.go' -not -name '*_test.go' -print | xargs wc -l | awk '$2 != "total" && $1 > 600 {print}' | sort -nr` now records `1293 server/transport/gateway.go` in `INV-LARGE-FILES`.
- [x] `docs/dev/techdebt` contains no absolute local path leaks.
- [x] `docs/dev/techdebt/research_progress.md` contains no incomplete checklist items.
- [x] Scratch notes in `docs/tmp/tech_debt.md` were promoted, merged into existing findings, or explicitly rejected as stale/no-separate-finding.
- [x] Product/architecture ambiguities needing user decisions are collected in `docs/dev/techdebt/open_questions.md`.

## Useful Commands

- `go list -json ./...` for package import/dependency evidence.
- `find . -path ./.git -prune -o -name '*.go' -print | xargs wc -l` for large-file evidence.
- `sg` for syntax-aware searches where possible; use `rg` only for repo-wide text patterns.
