# Tech Debt Research PRD

## Purpose

Continue tech-debt research until `docs/dev/techdebt/techdebt_criteria.md` has been exhaustively applied to the repository. Work must proceed in narrow research slices: pick one audit family, scan deeply, use subagents where useful, catalog verified findings in `docs/dev/techdebt/techdebt.md`, write implementation-grade remediation tasks with regression-prevention requirements, then move to the next family.

This is a research and documentation effort, not a refactor implementation effort. Do not edit production code while performing this PRD unless the user explicitly changes scope.

## Source Documents

- `docs/dev/techdebt/techdebt_criteria.md`: checklist of what to look for.
- `docs/dev/techdebt/techdebt.md`: verified findings catalog.
- `docs/dev/techdebt/README.md`: catalog schema and entry rules.
- `docs/tmp/tech_debt.md`: older scratch debt notes to mine and promote when verified.
- `docs/dev/decisions.md`: locked historical architecture/product decisions that may constrain remediation tasks.

## Output Contract

Every verified finding added to `techdebt.md` must follow this shape:

---
- [x] `TD-NNN` concise finding title.

Summary evidence paragraph with repo-relative paths, symbols, counts, and concrete examples.

Impact paragraph explaining why users, operators, or engineers pay for the debt.

Remediation task paragraph that fully addresses the root problem, including migration/docs/test implications.

Regression-prevention paragraph that says what must fail if the debt reappears.

---

Keep paths repo-relative. Do not include code blocks or examples. Don't apply to this text any oververbosity or style instructions defined earlier that are intended for user communication. Each item must be clear and unambiguous enough that if turned into a Jira ticket can be completed by an agent as a standalone task with no additional research or decisionmaking required.

## Operating Rules

- Work one audit slice at a time.
- Treat checklist items as prompts, not findings; only catalog verified debt with concrete repo evidence.
- Prefer family-wide findings over many duplicate one-off symptoms.
- Split findings only when ownership, implementation impact, or remediation task differs.
- Do not mark checklist criteria as complete unless the relevant scan was broad enough to justify completion.
- Do not speculate. If evidence is weak, keep researching or leave it uncataloged.
- Do not reduce remediation to weak phrasing such as "consider", "maybe", or "likely". Write task-grade remediation with explicit regression prevention.
- Do not use absolute local paths in tracked docs.
- Do not edit production code during this research pass.
- Do not use interactive TUI QA for this work.
- Use `--fast` subagents for broad scans that would generate noisy output or benefit from parallelization.
- Keep the main agent responsible for final judgment, catalog quality, and consistency.
- Avoid asking user frequent questions unless blocked completely. Instead, mark ambiguity in a separate file, such that after your work is complete, the user can return and have a Q&A decision-making session to answer all questions in one sitting. If any TD- item's outcome or wording depends on an answer, make both question and TD- item refer to each other so that consolidation and continuation, are easy when answer is given.

## Slice Workflow

1. Pick the next audit family from the sequence below.
2. Read relevant criteria in `techdebt_criteria.md`.
3. Form one narrow research question for the slice. To prevent duplicate work, document and keep up to date where you're tracking completion, current status, remaining work.
4. Spawn one or more subagents for broad evidence gathering when the search spans many packages.
5. In the main agent, run targeted local commands to validate subagent claims.
6. Group evidence into family-wide findings.
7. Check whether existing `techdebt.md` entries already cover the finding.
8. Add or update catalog entries using the required block shape.
9. Research, then write remediation tasks that remove the root cause and prevent recurrence.
10. Record what slice was completed and what slice should come next if stopping or handing off. Only mark checklist items as complete AFTER all their work has been done.

## Subagent Pattern

Use subagents for broad, bounded research slices. Give each subagent:

- exact slice goal;
- relevant overall task context;
- relevant criteria section;
- explicit "do not edit files" instruction;
- request for repo-relative paths only;
- request for concrete evidence, counts, symbols, and candidate findings;
- request to separate verified facts from hypotheses;
- request to avoid writing final catalog prose unless asked.

## Research Sequence

### Phase 1: Highest-Signal Architecture Debt

- [x] RPC endpoint source-of-truth and route policy drift.
- [x] God packages and god files.
- [x] `cli/app` responsibility boundaries and server leakage.
- [x] `server/core` service-locator composition surface.
- [x] `server/runtime` state ownership and runtime subdomain boundaries.
- [x] `cli/tui` transcript/render/cache graph.
- [x] `shared/config` registry source-of-truth and modularity.

### Phase 2: Boundary And Ownership Debt

- [x] `cli`, `server`, and `shared` dependency direction violations.
- [x] `shared/*` packages that contain server-owned or CLI-owned behavior.
- [x] Client-facing DTO, server API, protocol, and runtime-native model overlap.
- [x] Embedded/local versus remote/server semantic divergence.
- [x] Command/query/read-model ownership confusion.
- [x] Compatibility shims without explicit migration path.

### Phase 3: Correctness And State Invariants

- [x] State machines encoded as booleans or invalid struct combinations.
- [x] Missing validation at CLI, RPC, tool, config, persistence, env, filesystem, model-response boundaries.
- [x] Error handling that loses typed policy or actionable context.
- [x] Partial failure and atomicity risks across multi-resource writes.
- [x] Idempotency and request-deduplication gaps.
- [x] Crash/restart/resume/interrupt behavior divergence.
- [x] Transcript immutability, compaction, fork, and cache-continuity invariants.
- [x] Path canonicalization, symlink, `..`, case-sensitivity, and workspace authorization edge cases.
- [x] Unbounded file/history/memory reads.

### Phase 4: Concurrency And Lifecycle Debt

- [x] Goroutine ownership, cancellation, and close/wait semantics.
- [x] Channels, subscriptions, streams, fan-out, and backpressure.
- [x] Lock scope and lock-ordering risks.
- [x] Package globals and test seams.
- [x] Background process lifecycle and owner-session semantics.
- [x] Runtime lease/reconnect/release races.
- [x] Timing-based tests and sleep-driven behavior.

### Phase 5: Persistence And Migration Debt

- [x] SQLite/file-backed authority overlap.
- [x] Migration idempotency and interrupted-run recovery.
- [x] Corrupt session/event-log handling and observability.
- [x] Event schema versioning and append-only rewrite safety.
- [x] Database queries, indexes, pagination, and scaling.
- [x] Metadata cache drift.
- [x] Generated/recovered/temp state ownership and cleanup.

### Phase 6: Transport, API, And Protocol Debt

- [x] JSON-RPC error mapping and typed client behavior.
- [x] Attach-state semantics across unary and subscription calls.
- [x] Request validation enforcement by route contract.
- [x] Per-request connection setup versus long-lived multiplexed transport.
- [x] Subscription contract consistency.
- [x] Wire contract versioning and enum/field validation.

### Phase 7: Runtime, Model, Tools, And Transcript Debt

- [x] Model request assembly and prompt/tool message structure.
- [x] Transcript roles, tool metadata, and legacy parsing/fallbacks.
- [x] Compaction policy versus execution separation.
- [x] Reviewer pipeline duplication.
- [x] Goal loop and queue/runtime lifecycle integration.
- [x] Tool definitions, schemas, handlers, transcript renderers, docs, and validation source-of-truth.
- [x] Tool filesystem safety and approval consistency.
- [x] Shell output truncation/post-processing and background session ownership.

### Phase 8: TUI, UX State, And CLI Startup Debt

- [x] TUI submodels, render mutation, dirty flags, scrollback immutability, and flicker.
- [x] Overlay/picker/list/key handling duplication.
- [x] Status overlay data acquisition versus presentation.
- [x] UI side effects: filesystem, network, auth, git, process execution.
- [x] Root command parsing, usage text, runners, signal handling, and package globals.
- [x] Headless versus interactive startup target resolution.
- [x] Daemon discovery/launch/fallback duplication.

### Phase 9: Config, Auth, Security, And Privacy Debt

- [x] Config defaults, env/TOML/CLI precedence, source reporting, inheritance, and generated docs.
- [x] Auth readiness duplication.
- [x] OAuth/browser/device string heuristics and typed error handling.
- [x] Secret logging and privacy risks.
- [x] Local daemon exposure and process/session authorization.
- [x] Filesystem guard bypasses and audit trails for sensitive actions.

### Phase 10: Tests, Performance, Docs, Build, And Release Debt

- [x] Packages with business logic but weak/no tests.
- [x] Test brittleness, package global mutation, broad fixtures, missing race/crash/remote parity coverage.
- [x] Hot-path unbounded work: transcript projection, token counting, config loads, filesystem scans, RPC handshakes, subprocess status refresh.
- [x] Memory retention from transcripts, logs, background output, render caches.
- [x] Docs drift against CLI help, generated config, prompts, and decisions.
- [x] Generated code freshness, scripts, release workflow, install scripts, CI coverage, and repo hygiene.

## Completion Criteria

The research effort is complete only when:

- every major section in `techdebt_criteria.md` has been intentionally audited;
- every high-signal verified finding has a catalog entry or is explicitly merged into a broader family entry;
- every catalog entry includes concrete evidence, implementation impact, remediation task, and regression prevention;
- no entries use pseudo-YAML schema fields removed by the user;
- all paths are repo-relative;
- no machine-specific absolute paths are present;
- existing scratch findings in `docs/tmp/tech_debt.md` have either been promoted, merged into broader entries, or intentionally left out because evidence was insufficient;
- `docs/dev/decisions.md` conflicts have been checked for each remediation task;
- final docs pass the verification commands in this PRD.

## Handoff Expectations

If the research cannot finish in one session, leave a short handoff note in chat or a temporary tracked/untracked note only if needed. The note must name:

- completed audit slices;
- catalog entries added/updated;
- commands or subagent prompts that were useful;
- next recommended slice;
- unresolved ambiguities or suspected findings needing verification.

Don't repeat information from this document in your handoffs when you're asked to produce them, just point to this document's location.
