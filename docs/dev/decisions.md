# Decisions

## Product Scope

- Build a minimal terminal coding agent focused on output quality, speed, and professional workflows.
- Tech stack: Go + Bubble Tea; no TypeScript.
- Public docs site uses Astro + Starlight from the repository `docs/` directory, deploys as a fully static GitHub Pages site, mirrors the root `README.md` as the initial docs home, and uses Algolia DocSearch for site search.
- Skills are supported via AGENTS-driven `SKILL.md` discovery/injection from `~/.builder/skills` and `<workspace>/.builder/skills`.
- First-run onboarding may optionally symlink skills and slash-command roots from `~/.claude`, `~/.codex`, or `~/.agents` into Builder's `~/.builder` layout; normal runtime discovery still reads only Builder-owned directories.
- `config.toml` supports a file-only `[skills]` boolean table for per-skill new-session enable/disable toggles; disabled skills remain visible in `/status` and only affect future skills-message injection.
- Preinstalled skills are seeded from binary-embedded deterministic assets under `prompts/skills/**` into hardcoded `~/.builder/.generated/skills`; generated assets do not use configured session persistence root.
- `~/.builder/.generated` is deterministic, destructible, overwritten on server startup, and not user-owned. Generated sync runs on server startup (`builder serve` or embedded server), not in clients.
- Generated asset integrity uses `.generated/.builder-generated.json` with schema, Builder version, and tree hash excluding the marker. Clean old generated trees upgrade in place; edited/add/delete/rename/symlink/invalid-marker states move the whole `.generated` entry to `~/.builder/recovered/<UTC timestamp>/.generated`, then regenerate.
- If `~/.builder/recovered` is non-empty, every new session gets a user-facing, non-model-visible warning asking the user to clean recovered files and not edit `~/.builder/.generated`; existing sessions are not warned every turn.
- Generated skills are always seeded regardless of config. Existing `[skills]` toggles only disable injection by normalized skill name.
- Generated skills are shadowed by any user skill with the same normalized name from workspace/global roots. Do not redesign existing non-generated duplicate behavior as part of generated skills.
- Initial preinstalled skill framework ships `skill-creator`, but `prompts/skills/skill-creator/SKILL.md` must be valid before commit: non-empty file, valid frontmatter with non-empty `name` and `description`, non-empty body. Generated skill validation must also reject duplicate generated skill names and symlinks/non-regular entries.
- Full-access execution in v1 (no sandbox).
- Architecture must remain pluggable/composable with low-friction extension points.
- Source layout is a single Go module organized under top-level `cli/`, `server/`, and `shared/` roots: CLI/frontend-owned packages live under `cli/`, authoritative runtime/persistence/tool/auth packages live under `server/`, and boundary-safe shared contracts/helpers live under `shared/`.
- Working name is `builder` and must stay easy to rename.

## Client/Server Lifecycle Boundary

- Server owns durable lifecycle state, lifecycle mutations, and canonical lifecycle event streams.
- CLI/TUI consumes client-facing DTOs and shared service clients; it does not own alternate durable/runtime lifecycle flows for embedded-local mode.
- Embedded-local mode adapts through the same loopback service/client boundary as remote/shared server mode. Direct in-process engine, broker, process, auth, or project objects may exist only behind server-owned adapters.
- User-visible side effects tied to lifecycle events are triggered at one client-facing accepted-event boundary, not inside only one transport or runtime path.
- Incomplete migration paths should be removed rather than preserved with compatibility shims. Breaking API or protocol changes are allowed when documented.

## Core Runtime And Tools

- Core tools: `exec_command`, `write_stdin`, `view_image`, `patch`, `ask_question`.
- Experimental agent-only tool `trigger_handoff` is config-gated under `[tools]`, defaults to `false`, and is always declared to the model for a session when enabled rather than being shown/hidden dynamically by context usage.
- Goal management is CLI/runtime-owned. Builder must not add model-callable goal tools, even dynamically; the Builder CLI is the authoritative control surface for creating, updating, pausing, resuming, clearing, and completing goals.
- Users manage goals primarily through TUI `/goal`; models may use normal shell commands `builder goal show`, `builder goal complete`, and `builder goal set <objective>` for the current session, relying on Builder-injected caller session identity for correct session targeting. Agent `builder goal set` is only allowed when no goal exists; agent attempts to overwrite an existing goal, pause, resume, or clear must fail nonzero with model-facing warning copy from `prompts/goal/agent_command_denied.md`.
- `/goal <objective>` is immediate: it sets/replaces the session goal and starts a model turn toward that goal right away. If a model turn is currently running, setting/replacing is rejected.
- `/goal resume` on a completed goal reopens that same goal as active; this is intentional operator recovery for mistaken completion.
- Goal completion is explicit CLI state mutation, not natural-language inference. The model reports completion by running `builder goal complete`; Builder must not infer completion merely from assistant text. Agent-env completion requires hidden `--confirm` after first tripwire failure; human CLI completion does not require confirm.
- Goal state persists in session metadata for direct resume reads as `{goal_id, objective, status, created_at, updated_at}` with `status` limited to `active|paused|complete` in v1. Goal lifecycle also appends structured audit/projection events: `goal_set`, `goal_status_updated`, and `goal_cleared`, each with actor `user|agent|system`.
- Goal mode requires `ask_question` for active model loops. Validate active-goal/question parity at the boundary that starts model work and surface a normal runtime error if violated; do not add queue-specific or config-specific special cases. Reopening a session with a persisted active goal must still construct the runtime when `ask_question` is disabled so `builder goal show|pause|clear` can recover the session; only autonomous loop startup is suppressed.
- Goal CLI must never mutate session DB directly. It must cross live server/runtime RPC using existing app-server discovery/config; if the discovered/default server does not host the target session, fail live-runtime-unavailable.
- Standalone `builder goal` commands do not acquire controller leases. TUI/user goal mutations stay controller-lease-backed through existing runtime-control paths; model-shell CLI gets narrow lease-free RPC authority for same-session `show`, first-time `set`, and confirm-gated `complete` only.
- While a model turn is currently running, TUI goal lifecycle only accepts pause and clear. Set/replacement, resume, and complete are rejected while a model turn is currently running. Runtime-control `SetGoal` and `ResumeGoal` must acquire the primary-run gate before persisting active goal state, so a busy session cannot be left with an active goal and no running loop.
- Ctrl+C during active goal work keeps persisted status `active` and creates only runtime-local suspension until the next user message/session resume; do not persist an interrupt-specific status or metadata flag.
- Goal prompts and model-facing goal error copy live under `prompts/goal/`; avoid hardcoded multi-line goal prompts in Go. V1 prompt inventory is `set.md`, `nudge.md`, `pause.md`, `resume.md`, `clear.md`, `complete.md`, `already_complete.md`, `agent_command_denied.md`, and `complete_confirm_required.md`; one-line human UI labels may stay in Go.

## Command Execution Tool

- `exec_command` is the sole shell-command execution surface; the legacy `shell` tool is removed from future design decisions.
- Runs in the user login shell.
- Executes in non-TTY mode (pipes, not PTY).
- Uses direct shell invocation only (no runtime command parsing/AST preprocessing).
- Inherits parent environment and adds non-interactive hints.
- Merges stdout/stderr into one stream without origin tags.
- Command execution has no explicit timeout. `yield_time_ms` only controls when Builder returns control and backgrounds the process.
- Command lifetime is unlimited. `yield_time_ms` only controls when Builder returns control and backgrounds the process.
- Non-zero exit is recoverable (does not auto-abort the turn).
- No automatic retry for shell process-launch failures.
- Interrupt escalation is `SIGINT` then `SIGKILL` after 10s grace.
- Command output semantic post-processing is built into Builder, not delegated to shell wrappers. It applies after command execution and base sanitization, not before execution.
- `raw` is a first-class public parameter on `exec_command`; default is processed output, `raw=true` bypasses semantic post-processing while keeping transport hygiene/safety truncation.
- Built-in post-processors run before the optional user-defined hook.
- User post-process hook is configured as a path to an executable/script; Builder sends JSON on stdin and expects JSON on stdout.
- User post-process hook receives both original sanitized output and Builder's current built-in-processed output so it can either add on top or replace.
- Builder does not hard-block the user hook on irreversible commands; hook responsibility stays with the user. Built-in Builder processors still target read-only/reversible command families by policy.
- Command post-processing is configured under a dedicated `[shell]` config table.
- `[shell].postprocessing_mode` is the global mode switch and uses explicit values: `none | builtin | user | all`.
- Per-call `raw=true` still bypasses semantic shaping regardless of global mode.
- User hook has no separate timeout knob; it follows the same unlimited command lifetime and parent tool-call cancellation semantics.
- Built-in processors may run on both success and failure; each processor decides based on exit code.
- `exec_command` result JSON stays minimal in v1; processor metadata is internal and not added to the public tool result schema.
- Built-in processors are implemented as Go code in a composable registry; v1 does not add a declarative filter DSL beyond the single user hook.
- Hook failures must not change the provider-facing command-output envelope in v1. Warning surfacing, if any, stays plain-text-compatible and warning deduplication is optional.
- If an `exec_command` backgrounds, its selected processing mode persists with that process session for later `write_stdin` polls and completion notices.
- Foreground `exec_command` processing does not add a dedicated raw-output artifact in v1; operators can rerun with `raw=true` when needed.
- Background shell processes (`exec_command` / `write_stdin`) are app-global, not session-scoped.
- Background process ids are app-global within one app instance; owner session metadata is advisory for routing notices/history, not an access-control boundary.
- `/ps` may surface and operate on background processes started from other sessions in the same app instance; this is intentional in v1 to preserve operator visibility/control of long-running jobs.

## Patch Tool

- Apply is atomic: malformed/conflicting patch means no file changes.
- Allowed operations in v1: add/update/move/delete.
- `Delete File` participates in the same atomic patch transaction as add/update/move.
- Patch targets are validated with real-path resolution.
- No timeout and no automatic retries.
- Patch success persistence includes patch input + apply-result metadata.
- Outside-workspace edits are approval-gated unless explicitly enabled.
- `allow_non_cwd_edits=false` by default.
- If outside-workspace approval is denied, return an explicit non-circumvention tool error instructing manual user edits when essential.

## View Image Tool

- `view_image` path resolution uses absolute + canonical real paths before access checks.
- Workspace boundary checks apply after symlink resolution; symlink escapes outside workspace are blocked by default.
- Paths containing `..` are evaluated via canonical resolution; they are only allowed when the canonical target remains within the workspace boundary.
- Outside-workspace file reads are approval-gated via the same approver contract as `patch`, with per-call/per-session allow semantics.
- Approved outside-workspace reads are written to run logs with requested/resolved path metadata for auditability.

## Tool Output, Retries, And Failure Handling

- Large tool output is truncated for model consumption using standardized payloads (head+tail + truncation metadata, threshold configurable).
- Model-step transient failures use exponential backoff retries with 5 attempts (`1s, 2s, 4s, 8s, 16s`).
- Model/API errors in `ongoing` are shown as concise single-line errors; full details remain in detail view/logs.
- Transcript error roles are split by operator visibility: plain `error` remains a detail-only diagnostic role, while persisted operator-facing turn-start failures (submit/pre-submit/queued-resume failures that prevent the agent loop from starting) use `developer_error_feedback` so the message is appended into ongoing scrollback.
- Local command/validation failures that do not block a model turn from starting stay on plain `error` by design. Examples: slash validation, `/fast` and `/ps` usage errors, and settings/status/process command failures that are already surfaced via detail mode and/or transient statusline notices.

## Ask Question

- `ask_question` is shared by model and runtime, with unified UI.
- Runtime `ask_question` pauses active pipeline until answered.
- Waits indefinitely (no timeout/default cancel).
- Model-callable `ask_question` is limited to ordinary question/suggestion/freeform asks. Approval prompts are internal automated workflows only and are not exposed to the model tool schema.
- Supports suggestions + freeform override.
- With suggestions: option picker includes a dedicated `Freeform answer` branch, and `Tab` toggles between picker and freeform commentary editing.
- Suggestion asks use a schema-level `recommended_option_index` (1-based) instead of embedding `Recommended:` into suggestion text. The recommendation metadata is optional; missing or inapplicable values are ignored rather than failing the ask flow.
- In the ask picker, the recommended suggestion shows a green `★` marker before the option number and keeps the option text green, plus a faint `• recommended` note; when that row is selected, the marker becomes `✔︎` and uses normal selected-row styling.
- Selecting `Freeform answer` with empty input opens freeform editing; submitting from that path still requires non-empty commentary.
- For suggestion asks, returning to picker keeps any pending freeform draft visible as muted text and submits/restores that draft when the user picks an option or tabs back into editing.
- For internal approval asks, the picker only shows the fixed built-in options `Allow once`, `Allow for this session`, and `Deny`; `Tab` adds commentary for the currently selected option and that commentary is injected through the regular queued user-message steering flow. Allowing continues transparently to the model. Denial fails the original guarded tool call with an authoritative rejection error instead of surfacing a separate approval answer event.
- Freeform ask input uses the same prompt-box editing/cursor behavior as the main input.
- Without suggestions: freeform directly.
- Source origin is not labeled in UI.
- Answers are persisted as explicit summary text (including selected option number and any additional freeform commentary).
- Queue semantics are strict FIFO, in-memory only, and submitted answers are not editable.
- Optional post-answer action binding is supported.
- Action handling uses typed registry (stable id + payload schema + handler).
- v1 ships registry scaffolding only (no built-in actions).
- Action payload schemas are unversioned in v1.
- Unknown action id is fatal (crash in all build modes).

## Sessions, Persistence, And Durability

- Sessions support stop/resume.
- Persistence root is configurable; default `~/.builder`.
- Storage layout is workspace-scoped containers (`<workspace-folder-name>-<random-uuid>`) with UUID session directories.
- The server-driven migration target uses hybrid persistence: SQLite is authoritative for structured metadata and server-owned resources; large append-only session artifacts stay file-backed.
- The durable domain model is `project > workspace > worktree`; legacy workspace-scoped containers are migration input, not future protocol identity.
- Sessions remain project-scoped durable objects and carry a mutable current execution target `(workspace_id, worktree_id?, cwd_relpath)`.
- The app-global daemon listen configuration is explicit and user-configurable via separate `server_host` and `server_port` settings. Builder uses a fixed built-in default port in the private/dynamic range, binds exactly that configured address, and fails startup if the port is occupied; it must not silently rebind, fall back, or use `:0` ephemeral assignment.
- Same-machine transport optimization is local-first and additive. On Unix platforms the daemon also exposes a derived Unix domain socket under runtime-local ephemeral state keyed by the persistence root; this does not add a new config surface and does not replace configured TCP.
- `server_host` and `server_port` remain the durable remote source of truth. Same-machine clients may prefer the derived Unix socket only while the TCP target is still the default local attach target; explicit `server_host` or `server_port` overrides stay authoritative and must continue to dial configured TCP. LAN/remote clients continue to use configured TCP semantics and health/readiness remain bound to configured HTTP/TCP.
- The default WebSocket transport uses `github.com/lxzan/gws` behind `shared/rpcwire`. The legacy `golang.org/x/net/websocket` adapter was removed after the transport boundary landed; higher layers stay bound to Builder-owned `rpcwire` contracts rather than a library-specific API. Remaining `golang.org/x/net/websocket` imports are test fixtures only and do not participate in runtime transport.
- JSON-RPC custom error codes in `shared/protocol` are a wire contract. `ErrCodeRequestCanceled` (`-32023`) represents normal request cancellation and clients must map it to `context.Canceled` instead of plain error text.
- Interactive startup remains workspace-first. When startup cwd is unregistered, Builder enters an explicit post-auth binding flow with a create-new-project action first and a clearly separated existing-project picker below it.
- That bind/create startup flow is valid only when the client has a meaningful local path and the server can resolve that path.
- If the client has no meaningful cwd/path for the server, or the server cannot resolve the client path, startup switches to server-browsing mode instead of trying to bind the client path.
- In server-browsing mode, the client may open existing server projects/workspaces only; it must not offer "bind this workspace" or "create a project for this client path".
- First setup for server-browsing mode is server-admin only for now. Remote filesystem traversal/browsing is out of scope for this slice.
- Headless startup in an unregistered workspace fails fast; it must not auto-create hidden project/workspace state.
- To support agent recovery in that fail-fast model, Builder will expose explicit workspace-binding CLI commands: `builder project [path]` to inspect the project bound to a path, `builder attach [path]` to bind a workspace to the project already bound to `cwd`, and `builder attach --project <project-id> [path]` as the explicit project-id override. All forms default `path` to `cwd`.
- The minimum server-admin setup command surface is `builder project list`, `builder project create --path <server-path> --name <project-name>`, and `builder attach --project <project-id> <server-path>`.
- Those server-admin commands must prefer RPC to the configured running daemon when one exists; they must not require shutting the server down or taking local ownership of the persistence root.
- Explicit relocation recovery is `builder rebind <session-id> <new-path>`, which retargets one session to a different workspace root. Unknown-cwd startup does not infer relocation; it stays on the normal bind/create flow.
- When a session is chosen from the interactive session picker and its stored workspace root differs from Builder's current workspace root, startup must show a `Workspace changed` confirmation. `Yes` retargets that session to the current workspace root before opening it; `No` returns to the session picker.
- For the migration's runtime-residency model, lease identity is explicit and distinct from `client_request_id`; reconnect rehydrates, reattaches, and acquires a fresh lease rather than reclaiming an abandoned one.
- The attempted SQLite-backed `client_request_id` dedup persistence expansion is being hard-cut before ship. Current shipping direction keeps `client_request_id` on the API surface, retains lease-specific semantics for `sessionruntime.activate` / `sessionruntime.release`, and defers any durable/shared dedup authority to later dedicated session-control work.
- Post-migration, `session.json` is removed. Session metadata authority moves to SQLite. `events.jsonl` and `steps.log` remain file-backed for now.
- Interactive session creation remains lazily durable; creating a new interactive session does not immediately force durable metadata writes.
- The one-time storage migration is blocking at startup, stages the new database/layout before cutover, and keeps the old tree as a timestamped backup after success.
- Workspace path rebinding after relocation is always explicit user action; Builder must not auto-rebind inferred matches.
- Database access for the migration architecture is SQL-first and explicit. Prefer typed code generation from hand-written SQL (`sqlc`) plus Goose-managed SQL migrations over ORM-owned schema/runtime state.
- Session persistence format today is split `session.json` + `events.jsonl`.
- `events.jsonl` is append-only on normal writes; periodic compaction rewrites canonical JSONL to control long-session growth.
- Session directory names are UUID-only.
- Session start/setup becomes immutable at first model request dispatch.
- Resumed sessions keep locked setup immutable, except thinking level.
- Lock covers model + core generation params, enabled tools, tool schema/description snapshot, and system prompt snapshot; thinking level is mutable mid-session.
- Transcript message order is immutable for cache stability.
- Canonical model context/history is stored as Responses API input items; message-only chat is UI projection.
- Prompt-cache continuity warnings are computed at the request layer from actual cache-keyed model requests, not from compaction/fork/edit heuristics.
- Exact warning condition: for the same `prompt_cache_key`, warn when the new request prompt shape is not a postfix extension of the previous request prompt shape for that key and provider usage reports a positive cached-input-token decrease. If cached-token metadata is absent or current reuse is equal/higher, do not emit a cache-miss warning.
- Forks or any other operation that switches to a new cache key do not produce cache-continuity warnings; a new key is a new lineage, not an invalidation.
- Retry attempts for one logical model request are treated as one request for cache-warning purposes.
- Timeout/TTL-based cache-warning suppression is forbidden unless authoritative provider metadata is present on the actual transport.
- Prompt-cache warnings are persisted as structured server-side facts and replay identically for live runtimes, restored runtimes, and dormant session transcript views.
- `cache_warning_mode` is a three-state config: `off` disables cache warnings, `default` catches unwanted invalidations, and `verbose` includes everything from `default` plus broader invalidation diagnostics such as provider-reported cache reuse drops for postfix-compatible requests when the provider does not expose the cause.
- Tool-call identity prefers provider-native ids; UUID fallback when missing.
- Retry collisions on tool-call ids overwrite prior-attempt ids.
- Event identity uses monotonic sequence id + wall timestamp.
- No event integrity hash chain.
- Durability strategy: async capture with append-only turn writes and configurable fsync policy.
- Tool results persist at tool-completion boundary.
- History replacement during compaction persists as atomic `history_replaced` events.
- Compaction completion is represented in transcript projections by the persisted `compaction_summary` entry's `CompactLabel`/`OngoingText`; Builder must not persist or synthesize a separate `compaction_notice` transcript row for completed compactions.
- Crash-loss tolerance allows losing up to one in-flight tool call.
- No session event compression.

## Interrupts, Queueing, And In-Turn Messaging

- In-turn user messaging supports both steering queueing and queued post-turn send.
- Queue/send hotkey is `Tab`; compatibility alias: `Ctrl+Enter`.
- Clipboard image paste hotkeys are `Ctrl+V` and `Ctrl+D`; they save clipboard images to temp PNG files and insert the resulting path into the active text input.
- Known `Ctrl+Enter` CSI encodings normalize to the same queue action.
- Mid-run steering is soft-insert only (delivered at safe boundary after current tool completion; no forced interruption).
- Steering submissions never lock the input box; each `Enter` while busy queues another steering message immediately.
- Pending steering order is strict FIFO.
- When multiple steering messages flush at the same boundary, they are coalesced into one user message separated by blank lines before sending to the model.
- Pending user message order is strict FIFO.
- Pending queue is unbounded.
- Queued hotkey messages are in-memory only.
- Injected mid-run messages persist only on delivery boundary.
- `Ctrl+C` interrupt is turn-local (stop current model step + active tool process, keep app/session alive).
- Interrupt injects developer-role control message: `User interrupted you`; it is detail-only and hidden from ongoing transcript.
- Post-interrupt state returns to idle with input ready.
- Resume after interrupt requires explicit user text (no autogenerated resume message).
- Crash recovery is bifurcated:
- Mid-step crash resumes via interrupt flow.
- Otherwise restore normal state directly.

## Prompts, Tool Schemas, And Instruction Sources

- Prompt sources live in repository files.
- System prompt is a markdown file in-repo.
- Tool definitions (names, descriptions, schemas) are centralized in one file.
- Tool definitions are also the single source of truth for tool runtime availability, request exposure/gating (including multimodal and native-web-search opt-in), hosted-output decoding, transcript metadata, and render hints.
- Prompts/tool definitions are build-embedded (runtime-hardcoded from source files; no runtime file loading dependency).
- Instruction precedence follows provider/API role semantics (no custom override layer).
- Modern transcript semantics are typed and persisted end-to-end: tool-call display uses explicit tool-presentation payloads, meta-context classification uses structured fields (`message_type`, `source_path`), and compaction summaries persist as typed transcript items rather than content prefixes or header parsing.

## AGENTS.md Injection

- AGENTS injection happens once per session and is not repeated on resume.
- Injection order on first user turn is deterministic:
- Existing restored messages.
- Global `~/.builder/AGENTS.md` as `developer` message when present.
- Workspace-root `AGENTS.md` as `developer` message when present.
- Current user prompt.
- Injection uses structured fenced formatting including source path.

## Auth And Credential Policy

- OpenAI auth supports API key and subscription OAuth.
- Auth is global app-level (not per-session).
- Startup only blocks on auth when the resolved provider path requires Builder-managed OpenAI auth; explicit OpenAI-compatible base URLs and other non-OpenAI provider paths may continue without auth.
- Startup auth failures and 401s are surfaced as normal UX with actionable messaging rather than raw transport noise.
- Startup auth selection uses the same themed startup picker style as session selection.
- Startup auth picker always exposes the OAuth methods `oauth_browser`, `oauth_browser_paste`, and `oauth_device`, may expose `Use existing OPENAI_API_KEY from now on` when available, and always exposes `Continue without Builder auth`.
- OAuth issuer routing is not configurable in production. Builder hardcodes the official provider issuer per provider, and `BUILDER_OAUTH_ISSUER` is intentionally unsupported to prevent credential routing to overridden domains.
- `/status` subscription quota fetch uses a fixed ChatGPT usage endpoint. Custom `openai_base_url` values suppress quota fetch, but explicitly configured official ChatGPT hosts (`chatgpt.com`, `chat.openai.com`, with optional `/backend-api`) still allow it.
- Startup auth picker uses friendly titles with one-line explanations and does not show raw method ids in the rows.
- Interactive startup treats `OPENAI_API_KEY` as a chooser-backed auth source, not an unconditional override.
- When `OPENAI_API_KEY` is present, the startup auth picker may also show a separate non-OAuth option: `Use existing OPENAI_API_KEY from now on`.
- When saved subscription auth and `OPENAI_API_KEY` are both present with no remembered preference, startup shows a picker to choose which source should win from now on.
- When `OPENAI_API_KEY` is present and no saved subscription auth is configured, startup auth adds `Use existing OPENAI_API_KEY from now on` as a first-class picker option.
- Choosing the env-key path remembers `prefer env api key when available`; choosing OAuth while an env key is available remembers `prefer saved/subscription auth`.
- `/login` reopens auth selection; skipping there behaves like logout by clearing stored auth state when one exists.
- `/logout` is retained as an alias and clears both the active auth method and the remembered env-vs-saved-auth preference so re-auth starts from a clean choice.
- After an interactive auth success or first-time env-key adoption, startup shows a centered success screen before session selection continues. Conflict-only auth-source preference resolution does not. The title is `Auth success for: <email>` when OAuth token claims provide an email; otherwise it is `Auth success`.
- OAuth failure does not auto-fallback to API key.
- OAuth tokens auto-refresh silently; only refresh failures are surfaced.
- Global auth method can be switched only while idle.
- Auth credentials are stored in plain JSON under the persistence root (`auth.json`) with restrictive file permissions; no OS secure-store backend exists in v1.

## Configuration

- User settings are loaded from `~/.builder/config.toml`.
- If `~/.builder/config.toml` does not exist after the first successful auth, interactive startup runs a first-time setup flow before session selection. The first screen is a theme picker with a live preview. That picker preselects `light` or `dark` from terminal background detection, preserves `theme = "auto"` when the user accepts the detected default, and only writes an explicit `light` or `dark` when the user overrides that detected choice. Headless startup writes the default config directly with `theme = "auto"`.
- `theme=light` and `theme=dark` select Builder's own fixed palettes. `theme=auto` or an omitted theme falls back to terminal background detection.
- Unknown `config.toml` keys are rejected as configuration errors.
- Configuration precedence: `CLI overrides > environment > settings file > built-in defaults`.
- Global debug mode is configurable via `debug = true` in `config.toml` or `BUILDER_DEBUG=1` in the environment. Debug mode enables developer-oriented strictness such as hard-failing invariants that production mode recovers from.
- Ongoing native-history recovery must distinguish true same-session divergence from sliding authoritative tail windows. When an authoritative ongoing-tail hydrate advances the page offset but overlaps the already-emitted tail, Builder appends only the new suffix and must not full-replay or re-emit overlapped committed rows.
- Thinking level passes configured values through unchanged and applies only to OpenAI model families.
- Context window is explicit setting: `model_context_window` (default `272000`).
- Validation requires `context_compaction_threshold_tokens < model_context_window`.
- Responses API `store` is configurable via `store` / `BUILDER_STORE`, default `false`.
- Compaction routing is configurable by `compaction_mode` (`native|local|none`, default `local`).
- Terminal notification backend is configurable by `notification_method` (`auto|osc9|bel`, default `auto`).
- `tools.web_search` is enabled by default; `web_search` controls whether provider-native web search is activated (`native`) or disabled (`off`).
- `tools.view_image` is enabled by default; runtime only advertises it to models that support multimodal inputs.

## Context Management And Compaction

- Auto-compaction is enabled near context limits.
- Builder may compact before sending the next user prompt when current context usage is already within a configurable runway reserve of the normal compaction threshold; in that case the prompt is queued, compaction runs first, and the queued prompt is submitted immediately after compaction completes.
- Pre-submit compaction uses `context_compaction_threshold_tokens - pre_submit_compaction_lead_tokens`, with `pre_submit_compaction_lead_tokens` repurposed as fixed runway reserve and defaulting to `35000`.
- Startup rejects compaction settings that would begin either normal compaction or the effective pre-submit compaction band below `50%` of `model_context_window`.
- Auto-compaction failure aborts the current turn.
- `compaction_mode=none` disables manual and automatic compaction.
- Manual compaction is available via `/compact` while idle; optional arguments are appended as compaction guidance.
- Human-facing UX uses `compact` terminology, while agent-facing prompt/tool language uses `handoff`; do not mix these narratives across those surfaces without an explicit product decision.
- Successful manual `/compact` appends a hidden developer carryover message containing the last visible user prompt so the post-compaction model context still knows what the user most recently asked for.
- The compaction-soon reminder is single-shot until the next real compaction replaces history. Restores and forks derive the issued state from replayed transcript/history-replacement events instead of blindly copying stale metadata. When `tools.trigger_handoff=true`, the reminder template injects agent-facing text that `trigger_handoff()` is now allowed; the tool must fail before the reminder fires and must also fail while `/autocompaction` is off.
- Agent-triggered handoff uses its own internal compaction mode and may append a detail-only future-agent developer message; it must not reuse manual `/compact` carryover semantics.
- Main-agent OpenAI `session_id` stays on the persisted Builder session id for the entire conversation lifetime.
- Main-agent prompt cache lineage is keyed separately from `session_id` and rotates by compaction generation: the base key is `<session_id>` before first compaction, then `<session_id>/compact-N` for generation `N`.
- Supervisor/reviewer OpenAI `session_id` stays on `<session_id>/supervisor`; its prompt cache lineage uses the distinct base key `<session_id>/supervisor` before first compaction, then applies `/compact-N` with the same shared compaction generation counter as the main agent.
- Local compaction instructions are injected as final `developer` message.
- Local compaction summary generation reads full provider history from latest compaction checkpoint onward (or from start if none).
- Local compaction summary generation keeps tool declarations for request shape/cache stability but runtime rejects any returned tool calls.
- Local compaction summary generation must reuse the normal main-agent request envelope: same session identity, same assembled main-agent system prompt, same tool declarations, same fast/reasoning flags; only the request items differ by appending compaction instructions.
- Manual compaction failures are surfaced to UI without terminating session.
- Native compaction eligibility is capability-driven and user-configurable.
- `type=compaction` items and encrypted reasoning/compaction payloads are treated as opaque and replayed unchanged.
- Compaction lifecycle emits and persists started/completed/failed events.
- Local compaction instructions are sent as `developer` messages, and local compaction summaries/checkpoints persist internally as `developer` messages with `message_type=compaction_summary`; any model-facing summary prefix is added only at the provider input boundary. Native/remote compaction has no transcript-message prompt equivalent because it uses provider `Instructions` plus opaque provider output, which Builder replays unchanged.
- Compaction-completed runtime status never creates a UI-only transcript row. Transcript-visible compaction notices/summaries come only from server-owned transcript items/local entries.
- Ongoing suppresses detailed summary content; detail shows persisted compaction items in file order, including full local summary when available.

## Model Defaults

- Model seed is unset by default.
- Temperature is fixed to `1`.
- Max output tokens are unlimited by default.

## UI, Modes, And Rendering

- UI has two modes: `ongoing` (default) and `detail`, toggled by `Shift+Tab` or `Ctrl+T`.
- `ongoing` remains minimal:
- Show command start and file hint previews with truncation.
- If collapsing is not possible, show first command line and ellipsize.
- Hide thinking traces, preambles, outputs, and diffs.
- Ongoing preview sizing is fixed: command max `80`, file max `60`, soft-wrap allowed.
- Ongoing line prefix is `> `.
- Shell command previews remain syntax-highlighted in both modes; ongoing renders them with lower-contrast `preview` styling plus terminal `faint`, while detail keeps full syntax colors.
- Detail mode is an expandable transcript inspector. Collapsed detail is the default. User and assistant messages show at most the first 3 rendered lines; tool calls show the same first input line used by ongoing previews; ask-question entries show only the question; known developer/context reminders use typed compact labels. Expanding a message reveals the full detail content for that entry.
- Detail compact labels are metadata-first. Runtime/client projection must preserve source message type, source path, compact content/label, and tool presentation metadata where available. Legacy sessions that lack this metadata degrade by role and text-preview fallback only; Builder must not parse old prompt/reminder text to reclassify AGENTS, skills, environment, worktree, patch, or handoff messages.
- Unknown roles, unknown message types, and invalid/missing presentation metadata must stay visible and expandable. If recoverable text exists, unknown/malformed entries are visible in ongoing and detail; empty unknown/malformed entries are detail-only diagnostics. Production rendering emits diagnostics and uses deterministic fallback labels; debug mode may hard-fail only on internal detail-item invariants, not on old or unknown transcript data.
- Detail tool calls with error results stay collapsed by default but may show compact input plus a structured error summary supplied by runtime/projection; expanding reveals full input/output.
- Detail scrolling is line-oriented. `Up`/`Down` move by one rendered line when the viewport can scroll. After line, page, wheel, or alternate-scroll movement, compact detail selects the visible selectable item nearest the viewport center, so `Enter` toggles the item the user is visually centered on. Tall expanded entries remain selected while their body crosses that center anchor.
- Detail rows do not use dedicated collapsed/expanded glyphs. The first rendered line keeps the normal role/tool symbol; continuation lines in multi-line detail items use faint tree guides (`│` for middle lines, `└` for the last line).
- Patch/edit tools use `⇄` in ongoing, detail, and native replay. The symbol inherits pending/success/error tool color by result state.
- Detail items use the same role-group separator policy as ongoing/native transcript rendering, but with blank lines instead of divider rules. Consecutive tool rows form dense chunks; role-group transitions get one blank line.
- Detail selection uses full-width selected background/fill only. It must not change foreground colors, and selected background has the lowest priority so more specific backgrounds such as patch diff add/remove or syntax backgrounds win.
- Compact detail selection has visual-only highlighted blank spacer rows above and below the selected item when adjacent viewport rows exist. These spacers extend the selected side rail, reduce focus noise, and do not change detail scroll metrics or transcript line counts.
- Compact detail replaces the selected expandable item's role symbol with `▶` when collapsed and `▼` when expanded. The affordance is selected-only; unselected rows keep normal role symbols to avoid chevron noise. Detail status line mirrors the selected action as `Enter to expand` or `Enter to collapse`.
- Transcript rendering stages are explicit and ordered: `content render -> low-level semantic transform -> wrap -> line layout -> final decoration`.
- Style ownership is fixed by layer:
- Formatter config owns syntax backgrounds and formatter base foreground.
- Transcript rendering owns role styling, subdued shell preview styling, and diff semantics.
- Layout owns prefixes, indentation, and wrapping only.
- Semantic color tokens are centralized in `shared/theme`; TUI and app surfaces resolve colors from that palette instead of hardcoding inline hex values in renderers.
- Rendering/style invariants:

## Transcript Visibility

- Transcript visibility is defined by one product matrix, not by ad hoc projector or renderer filters.
- Visibility semantics:
- `O`: visible in ongoing mode with full text and visible in detail mode.
- `OC`: visible in ongoing mode in collapsed/shortened form and visible in detail mode with full text.
- `D`: hidden in ongoing mode and visible in detail mode.
- `X`: hidden in both transcript modes.
- Unknown/malformed visibility extension: entries with recoverable text are treated as `O` for operator visibility; empty unknown/malformed entries are `D` diagnostics. Known roles/message types still use the locked matrix below.
- Locked message-type visibility:
- `agents.md`: `D`
- `skills`: `D`
- `environment`: `D`
- `compaction_summary`: `O` when projected from a completed compaction, using the ordinal compact label in ongoing/collapsed detail and preserving full summary text for expansion.
- `interruption`: `O`
- `error_feedback`: `O`
- `compaction_soon_reminder`: `D`
- `reviewer_feedback`: represented in transcript by reviewer transcript roles, not by rendering the raw developer reviewer prompt directly. Effective visibility is `OC` or `O` depending on reviewer verbosity config.
- `background_notice`: `OC`
- `handoff_future_message`: `D`
- `manual_compaction_carryover`: `D`
- `headless_mode`: `D`
- `headless_mode_exit`: `D`
- Locked non-message transcript role visibility:
- user turns: `O`
- assistant turns: `O`
- tool calls: `OC`
- reviewer suggestions/status: `OC` or `O` depending on reviewer verbosity config.
- Visibility ownership is split by boundary but must follow the same contract:
- runtime projection decides whether a persisted/runtime message becomes a transcript entry and which transcript role it uses.
- TUI rendering decides how that transcript role behaves in ongoing vs detail mode.
- When a concept already has a dedicated transcript role, do not also render its raw developer/request artifact. Example: reviewer feedback is shown through `reviewer_suggestions` / `reviewer_status`, not by duplicating the underlying `reviewer_feedback` developer message.
- CLI transcript frontend ownership is file-scoped. `ui_runtime_adapter.go` is the runtime event coordinator only. `ui_transcript_event_reducer.go`, `ui_transcript_page_reducer.go`, and `ui_transcript_tail_reducer.go` own transcript planning/policy. `ui_transcript_event_apply.go`, `ui_transcript_page_apply.go`, and `ui_transcript_runtime_state.go` own UI-side transcript mutations. `ui_transcript_entries.go` owns app/TUI transcript conversion and equality helpers. `ui_transcript_diag.go` owns transcript diagnostic field formatting. `ui_runtime_events.go` owns runtime event batching/fence policy.
- Detail shell commands are full syntax color.
- Ongoing shell commands are syntax-highlighted but subdued.
- Formatted text uses the app foreground as its base text color.
- Syntax-highlighted output must not emit backgrounds unless explicitly intended, such as final diff added/removed decoration.
- Assistant text streams in ongoing mode.
- Tool output is not streamed live; show running status and reveal on completion.
- `detail` is a live transcript view with UI-local expansion and selection state; transcript changes update content while open, while scroll/anchor stays stable unless the user navigates.
- Mid-step entry shows latest completed snapshot only.
- Detail content is not static while open; only scroll/anchor behavior is stable.
- Snapshot scope is full session transcript up to latest completed step.
- Detail transcript rendering is flat continuous stream (no grouped sections).
- Step-end markers appear in detail only.
- Ongoing does not own a transcript viewport or restore app-managed ongoing scroll. Native terminal scrollback owns committed ongoing history navigation.
- Mode-toggle events are UI-ephemeral and not persisted.
- App interaction/overlay control is modeled as explicit typed states with allowed transitions. `ask`, `rollback`, `process list`, and `status` own isolated controller state; overlapping boolean precedence is forbidden.
- Detail is a fullscreen pager-style transcript overlay (input/queued/picker hidden).
- Ongoing mode uses native terminal scrollback backed by backend/runtime committed transcript SSOT. Runtime events are delivery triggers and live-overlay updates; permanent ongoing rows come from committed suffix/range reads.
- Main UI startup stays in the normal buffer because ongoing-mode replay must remain visible in terminal scrollback. The former `tui_alternate_screen` config is removed; legacy config keys are rejected.
- Main UI startup clears the visible terminal viewport once before rendering (including `native` mode), so each session (including `/new`) starts from a clean visible slate.
- During continuous attachment, ongoing-mode normal-buffer history is append-only. Once a transcript line is emitted into scrollback, it is immutable: no retroactive restyling, no in-place rewrites, no clear-and-replay, and no full-buffer re-emission to paper over same-session logical divergence.
- For frontend transcript-sync semantics, compaction is same-session committed transcript progression, not a same-session transcript rewrite.
- User-visible transcript history is never truncated by compaction or handoff. Compaction may replace model context, but detail/ongoing transcript reads must preserve all pre-compaction committed history across any number of compactions.
- Any latest-compaction boundary or floor is tail/model metadata only. Detail transcript paging and rendering must ignore it and show the full append-only transcript in persisted order.
- Legacy persisted `history_replaced` entries with `engine="reviewer_rollback"` are compatibility no-ops on replay. Builder must tolerate and ignore them rather than treating them as transcript-rewrite semantics.
- Rollback/fork is navigation or attachment to a different session target, not a same-session transcript mutation.
- Assistant streaming is rendered in the ongoing live viewport and is not appended to normal-buffer scrollback until commit.
- Ongoing-mode normal-buffer scrollback is committed-transcript only. Tool-progress, assistant deltas, reasoning deltas, and any other provisional live activity are transient viewport state only and must never become immutable scrollback authority.
- If connectivity or subscription continuity is lost, the transient ongoing live viewport is discarded immediately. Recovery happens by hydrating authoritative committed transcript state and resubscribing.
- Transcript-affecting transport failures must not be swallowed or converted into fake empty/idle UI state. Correctness wins over continuity: the affected live view may stop, but it must not continue from stale transcript data.
- For external continuity-loss recovery only, re-issuing the TUI ongoing buffer from authoritative committed state is acceptable.
- Client-side transcript divergence caused by deduplication, ordering, overlap, or pagination bugs is not an acceptable redraw case; it must be fixed at the root cause. Production reconciles/rebases silently without user-visible same-session divergence warnings; global debug mode may hard-fail instead.
- Pending tool-call activity in ongoing mode lives only in the volatile live region, not in committed normal-buffer scrollback.
- Ongoing-mode glyphs reserve `@` for web search tool calls; reviewer status/suggestion entries use `§`.
- Pending tool-call previews in the live region use the same rendering/layout as normal committed `tool_call` previews, with no pending-only labels, keywords, or extra markers.
- Tool completion in ongoing mode appends exactly one final committed line for that tool, already rendered in its terminal state. Ongoing mode must never recolor or otherwise mutate an earlier emitted tool line.
- Parallel tool calls in ongoing mode commit through a stable frontier: later completed calls remain in the live region until all earlier pending calls are ready, but they render in their final tool state immediately; only still-running calls show the spinner. Newly committable final lines append once in transcript order.
- In ongoing main-input mode, `Up`/`Down` are reserved for prompt-history recall at whole-buffer boundaries and for normal multiline cursor movement otherwise; they do not scroll the ongoing transcript. `PgUp`/`PgDn` also do not scroll ongoing transcript state; terminal/native scrollback owns that behavior.
- Builder input fields use one shared editor implementation with a real terminal cursor by default across ongoing and alt-screen surfaces. The editor owns text semantics and rendering computes exact physical cursor coordinates; the terminal owns cursor shape, color, and blink so emulator configuration is respected. Soft/reverse-video cursor rendering is fallback-only for inactive/locked inputs or verified integration failures, not a parallel primary implementation.
- Real-cursor input rendering must be width-safe inside a closed input component. `InputField.Render(width)` owns both rendered input lines and cursor coordinates; callers must not splice arbitrary unwrapped content into those lines. Any line that can physically hard-wrap in the terminal must be pre-wrapped/truncated and counted by `InputField` before cursor placement; untracked hard wraps are correctness bugs because they shift the terminal cursor and can corrupt Bubble Tea diff rendering. Fallback to soft cursor is allowed only for verified cursor drift, wrap mismatch, or alt-screen corruption that cannot be solved in the renderer adapter.
- Startup/onboarding/project/worktree input fields use `cli/tui/input.Editor` for editing state and `cli/tui/input.Field` for rendering/cursor coordinates, rather than Bubble `textinput.Model`, app-local wrappers, or additional text-input components. `uiSharedTextInput` was removed; future input surfaces must reuse `Editor`/`Field` and shared app key-policy helpers instead of adding separate cursor/rendering behavior.
- Failed prompt-history navigation attempts emit a plain terminal BEL with no transient UI notification.
- Main-input `@` path autocomplete uses a cached repo-relative path corpus built asynchronously from `rg --no-config --files -0 --hidden -g '!.git'`; corpus prewarming starts eagerly in the background when the UI model is created for a workspace, but it must be scheduled through Bubble Tea startup commands (`startupCmds` / `Init`) rather than unmanaged constructor goroutines. Live matching never shells out per keystroke and runs only against the in-memory cache. Query tracking is cursor-local and accepts path-safe runes inside the tracked token: Unicode letters/digits plus `/`, `.`, `_`, and `-`, so nested and hidden path references can be continued after accepting a directory completion. Hidden paths are included, `.git` is explicitly excluded, and normal ignore-file handling remains enabled so `.gitignore` junk such as `node_modules`, `.gradle`, and `build` stays out by default. Non-empty directory candidates are derived from file paths, so empty directories are intentionally excluded in v1. Corpus-build failures are retryable on later queries in the same workspace; they do not permanently disable path autocomplete for the session.
- Rationale: terminal normal-buffer scrollback cannot be safely rewritten portably; committed replay is the single source of truth for persistent formatted history.
- Ongoing mode keeps mouse capture disabled to preserve native text selection behavior.
- Ongoing mode never enables terminal alternate-scroll (`?1007`).
- Detail transcript overlay always uses terminal alt-screen (`?1049`).
- Detail does not enable terminal mouse capture because it blocks native text selection in common terminals. Detail may enable terminal alternate-scroll (`?1007`) while active, and must disable it again on leaving detail.
- Rollback/edit message picker uses detail rendering inside alt-screen but does not enable terminal alternate-scroll (`?1007`) and ignores mouse events; `Up`/`Down` are the only picker navigation path.
- Rationale: ongoing must preserve long-lived normal-buffer scrollback; detail still needs smooth native selection/copy, so selection wins over app-level pointer capture. Wheel navigation is best-effort through terminal alternate-scroll and any mouse events the terminal can deliver without capture.
- Compact detail viewport scrolling (`Up`/`Down`, `PgUp`/`PgDn`, wheel, and alternate-scroll key events) auto-focuses the selectable item nearest the viewport center so `Enter` expands what the user is visually centered on.
- No timestamps are shown in UI.
- Streaming paint cadence is 16ms with token coalescing per flush tick.
- Main status line is compact and fixed: activity indicator, optional git branch, model label, process/server metadata, transient warning; context meter is right-aligned. The activity indicator is followed by one plain space. Later metadata segments use ` · ` separators. Goal mode does not add persistent `goal active`/`goal paused`/`goal complete` text; only an active goal turn changes the progress word to primary-blue `goal`.
- Model label appends thinking level when reasoning effort is supported by the resolved model contract; unknown non-empty model ids default to reasoning-capable.
- Status line includes right-aligned context meter (10-char bar + `% ctx window`, green/yellow/red at `<50%`, `50-<80%`, `>=80%`).

## Startup And Session Selection UX

- Startup shows recent sessions with pick-or-new flow.
- Startup session list is scrollable with no cap.
- If no sessions exist, startup goes directly to new-session setup.
- In the server-driven migration target, when CLI startup cwd does not resolve to a registered project/workspace/worktree, startup enters a project-picker/registration flow rather than auto-registering. That flow may create a new project and attach the current workspace as its first workspace/main worktree, or explicitly attach the current workspace to an existing project. Outside that flow, the CLI remains workspace-first.

## Worktree Management

- Worktree-management planning and implementation use `workspace` terminology only; older `repo` references are stale.
- Planned `/worktree` management keeps session identity stable and changes only the shared session execution target `(workspace_id, worktree_id?, cwd_relpath)`.
- The first `/worktree` slice does not introduce a separate teleport-root abstraction; execution-target switching plus explicit worktree/origin status is sufficient.
- The shipped `/worktree` create UX is flow-only: `/worktree`, `/worktree new`, and `/worktree create` all enter a single smart-target create dialog. The raw `/worktree create <branch> [path]` bypass is intentionally unsupported.
- The create dialog only auto-suggests a target name from the sanitized session name. If that yields nothing, the `Branch or ref` field stays blank; it must not fall back to the current branch, main branch, or a generic placeholder name.
- The create dialog has no explicit `new branch` / `existing ref` selector. Builder resolves the typed `Branch or ref` asynchronously and shows a live badge: `new branch`, `existing branch`, or `detached ref`.
- In the create dialog, `Branch or ref` appears before `Base ref`; `Base ref` defaults to `HEAD`, is required for new-branch creation, and is only relevant when Builder will create a new branch.
- Worktree transitions append an immediate user-visible local note and also maintain a lazy typed developer-context reminder for the next model submission; the latest pending reminder always wins before submit and may reappear after compaction generation changes.
- Worktree transitions do not append synthetic transcript notes. They store the latest pending typed developer-context reminder, then materialize that message through the normal transcript path before the next user/model turn. Ongoing mode renders its compact text, detail mode retains the full developer message, and model request assembly collapses older worktree reminders to the latest one.
- Git remains the source of truth for worktree topology; Builder stores only additive metadata and blocks deleting a worktree that is still targeted by another session.
- Existing non-Builder git worktrees remain manageable from Builder in the first slice, but should be visually marked where feasible.
- Additive `/worktree` aliases are acceptable when they preserve the same safety semantics: `/worktree status`, `/worktree ls`, `/worktree remove`, and `/worktree rm` are supported synonyms over the same server-owned operations.
- Worktree delete is rebind-first cleanup: if the current session targets the worktree, Builder first moves it back to the main workspace, then performs remaining git cleanup even if the worktree directory was already removed manually.
- Worktree delete is also blocked while background shell processes still run under that worktree.
- Automatic branch cleanup after worktree delete is conservative and best-effort; Builder only auto-attempts branch deletion when provenance proves the branch was created for that worktree. Otherwise the branch stays unless the user explicitly confirms branch deletion too. Safe delete is allowed, force delete is not part of the first slice.
- New worktrees default under `worktrees.base_dir`, rooted under Builder persistence state by default; Builder creates missing base directories and auto-picks unique suffixed paths on collisions.
- Live worktree retarget should rebind runtime-local tool handlers to the new effective root rather than leaving tools pinned to the original startup workspace.
- The optional post-create worktree setup script is configured by `worktrees.setup_script`, runs asynchronously after new-worktree creation only, and receives both positional args and stdin JSON plus mirrored env vars; failure or timeout surfaces as transcript-local error info and does not undo the created worktree or session switch.

## Slash Commands

- Leading slash input enters command mode when first non-space char is `/`.
- Picker matches only first token and updates continuously.
- After whitespace, command enters argument mode and picker hides.
- `Enter` runs the currently selected slash command, including the default first match for partial input.
- `Tab` on a partial selected slash command autocompletes it and inserts a trailing space for arguments.
- Unknown slash commands are sent to model as normal user prompts.
- Built-in commands: `/logout`, `/exit`, `/new`, `/resume`, `/compact`, `/name`, `/thinking`, `/fast`, `/review`, `/init`, `/supervisor`, `/autocompaction`, `/status`, `/ps`, `/copy`, `/back`.
- Exact known slash commands use the normal queued-input drain path when queued, including conditionally fresh-session commands like `/review` and `/init`; they are never sent to the model as plain user prompts.
- Run-safe commands execute immediately while busy.
- Non-run-safe known commands while busy are rejected with transient status-line error.
- `/copy` copies the latest committed assistant `final_answer` to the system clipboard and stays hidden from the picker until that value is available.
- `/review` auto-submits the embedded review rubric prompt; it stays in-place for empty sessions and forks a fresh child session once the current session already has a visible user prompt. Optional args are appended as review scope.
- `/back` reopens the parent session when available; the parent draft becomes the child session's last committed assistant `final_answer` only when that message is also the last committed message, unless the parent already has its own saved draft.
- `/supervisor` controls runtime reviewer invocation for the current session only.
- `/supervisor` toggles when called without args; `/supervisor on|off` sets explicitly.
- `/supervisor` emits user-visible confirmation in transcript + status line and does not persist to config.
- `/autocompaction` controls runtime auto-compaction invocation for the current session only.
- `/autocompaction` toggles when called without args; `/autocompaction on|off` sets explicitly.
- `/autocompaction` emits user-visible confirmation in transcript + status line and does not persist to config.
- `/status` opens a read-only detail overlay with account/subscription status, workdir, session ids, compact git summary, context usage, model/config state, skills (including config-disabled markers), `AGENTS.md` paths, compaction count, and a session-section ownership row only when the current CLI instance owns the server.
- `/status` refreshes progressively on open: the base snapshot renders immediately, then account, git, and environment sections fill in asynchronously. It uses the same detail-surface alt-screen policy and native text-selection behavior as other detail overlays.
- Built-in prompt commands use embedded markdown templates.
- Slash commands support file-backed prompts from:
- `./.builder/prompts`, `./.builder/commands`, `~/.builder/prompts`, `~/.builder/commands`.
- Non-recursive `.md` scan, merged namespace, precedence: local > global and `prompts` > `commands`.
- File command id format: `prompt:<filename-without-extension>`.
- Triggering file command submits file content verbatim as `user` message.

## Notifications

- Ring terminal bell when a new `ask_question` is shown.
- Ring on turn end only if the turn executed at least two tool calls.
- UI turn-queue lifecycle exposes a reusable queue-drained hook; terminal bell notifications are one consumer of that hook.
- Turn-end ringing is keyed by runtime step id and projected `tool_call_started`/`assistant_message` events, but the actual notification is deferred until the queued prompt drain is fully idle.
- Turn-end notification text includes assistant response preview when available, else `<session title>: turn complete` with `builder` as the fallback title.
- Ask notifications include the ask text as `<session title>: Question: <question>` or `<session title>: Action required: <question>`.
- `auto` notification method prefers OSC 9 on supported terminals and falls back to BEL.
- OSC 9 notifications still emit a separate BEL so supported terminals get both notification and audible bell.
- OSC 9 is disabled when `WT_SESSION` is set.

## API Headers And Provider Wiring

- OpenAI requests always set `originator` and `User-Agent` headers.
- `session_id` header is sent whenever a session id exists, for both OAuth and API key auth.
- LLM provider wiring uses a provider-factory seam so runtime/app constructs `llm.Client` via provider selection (default OpenAI), enabling provider expansion without runtime refactors.

## Headless Mode

- `builder run "prompt"` is the supported headless subagent interface.
- Headless subagent roles are selected with `builder run --agent <role> "prompt"`; `--fast` is sugar for the built-in `fast` role.
- Subagent roles are configured as file-only `[subagents.<role>]` tables in `~/.builder/config.toml` and inherit the main config unless overridden.
- The built-in `fast` role exists even without config. On exact OpenAI first-party setups it heuristically switches to a smaller/faster profile and enables `priority_request_mode`; if it resolves to the same config as the main agent, Builder returns a warning so the caller can suggest tuning later.
- Executes a single non-interactive prompt with existing runtime/session persistence.
- Creates/resumes normal sessions and auto-names unnamed sessions `<session-id> subagent`.
- Default timeout is infinite; `--timeout` can bound execution.
- Output modes are explicit: default `--output-mode=final-text`, optional `--output-mode=json`.
- JSON mode emits exactly one final object on `stdout`: `status`, `result`/`error`, `session_id`, `session_name`, `duration_ms`, plus continuation metadata and startup `warnings` when available.
- Final-text mode emits startup warnings first, then the final assistant text, and optionally a continue hint.
- Progress is quiet by default and is emitted to `stderr` only when `--progress-mode=stderr`.

## Release Engineering

- Official release binaries are built through `scripts/build.sh`; the release profile is `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and `-ldflags "-s -w -X builder/shared/buildinfo.Version=..."`.
- Release archive packaging and verification live in `scripts/release-artifacts.sh`; workflow YAML should stay orchestration-focused.
- Supported release targets are `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, and `windows/arm64`; macOS Intel is unsupported and must not be added back.
- Workflow runner labels should use `*-latest` aliases where GitHub provides them. ARM smoke-test jobs currently stay on `ubuntu-24.04-arm` and `windows-11-arm` because GitHub does not publish `-latest` aliases for those hosted runners.
- Linux release binaries must stay statically linked; do not enable PIE or other dynamic-linking release modes.
- GitHub releases must publish `checksums.txt`, and `scripts/install.sh` verifies archive checksums when that manifest is present.
- Windows one-command installs are served by `scripts/install.ps1`; the default user install path is `~/.builder/bin/builder.exe`, matching the user-scoped Builder persistence root.
- Windows installer uninstalls must remove only installer-owned binary/PATH/registry/marker files and never remove Builder config, sessions, auth, worktrees, skills, or winget-installed dependencies.
- The release workflow must verify the checksum manifest and smoke-test packaged binaries on Linux, macOS, and Windows before publishing.
- The release workflow must smoke-test `scripts/install.ps1` against staged Windows release assets before publishing.
- GitHub artifact attestations are intentionally not part of the release pipeline.

## Experimental Reviewer

- Post-turn reviewer agent exists behind config and defaults to `reviewer.frequency = "edits"`.
- Reviewer runs only after completed assistant final handoff and only if the completed turn executed at least one tool call.
- Reviewer uses more aggressive tool-output truncation than main-agent path.
- Reviewer contract is minimal JSON: `{"suggestions":["..."]}`; invalid payloads are ignored non-fatally.
- If suggestions exist, runtime appends them as `developer` message and runs one extra main-agent follow-up pass.
- Follow-up noop token is exact `NO_OP`; if emitted, runtime keeps original assistant final answer.
- Reviewer pass is single-shot (no recursive review of review).
