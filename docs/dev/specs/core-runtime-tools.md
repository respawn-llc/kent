## Authority And Client Boundaries

- Worktree entry, exit, startup, and post-compaction reminders must shorten their CWD relative to the formatting Working Directory when contained within it, relative to home otherwise, and use an absolute path outside both. General environment CWD and Session rebind target-directory context must use absolute paths. Existing conversation content is never rewritten to apply path presentation.
- Patch presentation uses an absolute path supplied by the model exactly as its display label while resolving its canonical filesystem target separately. Image-tool clients collapse structured path labels without changing tool arguments, persisted results, or model-visible arbitrary text. Errors and diagnostics may use absolute paths.

- Kent executes commands without virtualization or sandboxing. Isolation requires running Kent on a remote machine or in Docker.
- The server is the single authority for tool calls, session and other durable data, agent execution, and provider communication. CLI and GUI clients control and observe that authority. They do not own parallel local state or execution.
- Client presence and connection lifecycle are transport-only and are never server-work authority. Connecting, disconnecting, canceling or closing a client request, reconnecting, changing subscriber count, navigating away, or closing a UI may stop that client's observation or delivery only. It never starts, stops, pauses, cancels, retries, replays, duplicates, authorizes, or otherwise changes server-owned work. The detached Session archive lifetime defined by the [CLI Commands](cli-commands.md) specification is the sole exception. Server event publication never depends on subscriber count.
- Core owns each Chat Operation from target resolution through attachment finalization. A Chat Operation runs independently after caller timeout, request cancellation, transport loss, navigation, or client shutdown.
- Core shutdown atomically stops accepting new Chat Operations, cancels every in-flight Chat Operation, and joins them before closing Runtime Authority, metadata, or another dependency used by those operations.
- During Core shutdown, each Chat Operation must finish any acquired Runtime attachment through a bounded finalization attempt before the operation joins. A finalization failure must produce an operator-visible shutdown diagnostic. It must not create replay, reconciliation, or another owner.
- Explicit Runtime activation may hold the selected Session Runtime for the activating client connection. Disconnecting that connection detaches its hold without closing the Runtime or changing its work. An explicit Runtime Release is the separate operation that may drop the owner and close an idle Runtime.
- A completed live execution has one server-authoritative terminal result: status, result kind, reason that no final answer exists, final assistant message, runtime error, timestamps, and whether work was performed. Work was performed when a completed step observed at least two tool-start events, including events emitted during recovery.
- Terminal-result delivery is best-effort and never delays execution completion, waiting callers, queued work, interruption, or successor scheduling.
- Each controlling TUI may run its configured read-only lifecycle command after it accepts a lifecycle event. Desktop, headless, subagent, and server-only use never run it. The server neither supplies nor overrides the command.
- Each client transcript subscription is ordered and sequence-numbered. Hydration is the first message and carries the global newest bounded transcript segment, authoritative `has more above`, an opaque older cursor exactly when older history exists, and no newer cursor. Every later update has the next per-subscription sequence number. Each update contains the required content. Tool completions appear in committed order. Committed assistant entries use the identity of their streamed output. Clients apply every received message once and in order. Clients receive neither total transcript counts nor absolute offsets or revisions.
- Opening or reopening Chat never activates or holds an Active Session Runtime. It independently loads bounded transcript history and the latest completed server-owned projections for RuntimeActivity, Session identity and status, execution target, active execution, reasoning, Reviewer, compaction, tool, Queue, prompt, background-process, context-usage, and Goal state.
- A dormant Session Main View projects complete status from persisted Session facts and current read-only settings and capability policy and reports unavailable RuntimeActivity without creating an Active Session Runtime.
- A read-only transcript subscription may wait for an Active Session Runtime without creating or retaining one.
- The non-transcript projections may be stale and may represent different completed moments. Server authority identifies the owner of each fact. It promises no read freshness or cross-owner snapshot.
- A live Runtime may also report current Reviewer activity as best-effort state without reconstructing an earlier review from transcript history or promising that reconnect observes it.
- Feed delivery state must never replace authoritative Session facts.
- Transcript delivery promises no lossless continuity across subscription establishment, disconnection, process failure, or recovery. Desktop must handle discontinuity through [Desktop Chat's failure contract](desktop-chat.md#failure-and-recovery). Other clients reopen the ordered transcript subscription and reissue the independent owner reads. They must not rely on cached feed state.
- A Session with no execution target hydrates without one. Failure to resolve an execution target fails Chat opening or reopening with a clear error. Kent must not fabricate a target.
- Clients receive transcript, session-activity, and prompt-activity updates in one ordered subscription.
- Active Session Runtime mutation and model-loop ownership follow the [Runtime Steering And Model Loop](runtime-steering-loop.md) specification. Concrete domain owners own dormant Session mutations and long-running operations.
- Transport may correlate one request with one response, sequence connection setup, bound concurrent handling on one connection, apply socket backpressure, and stop connection-bound waiting and delivery when that connection closes. It never cancels a Chat Operation, rolls back a committed Session, or changes accepted work. Transport correlation is not a product identity, replay key, domain order, or server-work owner.

## Skills And Generated Assets

- Kent must inject each `AGENTS.md` as Markdown with the H1 header `# Authoritative instructions, rules, and important context from the <path> file:`, a blank line, and the full unmodified file content. Kent must not wrap that content in a code fence or add a separate `source:` line.
- Model-facing reference paths must be relative to the current Working Directory when contained within it, home-relative with `~/` when elsewhere under the home directory, and absolute otherwise. Kent must not construct parent-relative reference paths. Working Directory values must be absolute.
- Path shortening must affect only context rendered after shortening is applied. It must not affect file contents, source identity, or already-sent conversation history. Working Directory changes must not change rendered Skill paths. The Working Directory reminder supplies the changed location without rebuilding the Skills list.
- Skills are discovered from `<persistence-root>/skills` (default `~/.kent/skills`), workspace `.kent/skills`, and `<persistence-root>/.generated/skills`. Global `AGENTS.md` and the global system-prompt file use the selected persistence root. An empty root means `~/.kent`. Symlinks are followed when these files are discovered or read.
- File-only `[skills]` boolean settings enable or disable a skill for new-session model context. Clients show disabled skills.
- The generated `prompting` skill owns on-demand prompt-writing and agent-coordination guidance. Its enabled or disabled state must affect only ordinary skill visibility. It must not select system-prompt content, role guidance, reminders, or other model context.
- System-prompt templates must accept `DefaultSystemPromptDelegation` and render it as empty text, independent of skill visibility.
- Kent seeds preinstalled skills into `<persistence-root>/.generated/skills`. This generated directory is overwritten whenever the server starts, is not user-owned, and must not be edited. A user skill with the same normalized name shadows its generated skill.
- Model file-edit tools refuse writes in `<persistence-root>/.generated` without requesting approval. The refusal explains that generated files are overwritten and that a skill must be shadowed from an active user skill path using the same normalized name, ID, and directory structure. Command execution and file reads are outside this restriction.
- Generated-file protection can identify paths by exact path, glob pattern, or regular-expression pattern. Regular-expression matching in this protection policy is an explicit exception to the repository's general ban on regex parsing and replacement.
- If generated content is edited, deleted, renamed, linked, or invalid, Kent moves the generated tree to `<persistence-root>/recovered/<UTC timestamp>/.generated` and recreates it. A non-empty recovered directory causes each new session to show a user-only warning to clean recovered files and not edit `.generated`.
- Integrity checking must cover the whole `.generated` directory, including its generated README. Unauthorized additional files and directories must cause recovery. Recognized OS metadata files and permission changes must not cause recovery.
- A Kent upgrade must replace valid, unchanged generated content with the upgrade's content without causing recovery or a new warning.
- Generated-folder recovery notices follow the [full-warning wrapping policy](ongoing-scrollback-buffer.md#grouping-and-separators).
- Generated skills are always seeded. `[skills]` settings only control their model visibility. Empty files, invalid frontmatter, duplicate names, symlinks, and non-regular generated entries are rejected.

## Core Tools And Runtime Integration

- The core model tools are `exec_command`, `write_stdin`, `view_image`, `patch`, `ask_question`, and `trigger_handoff`.
- Outside Workflow-controlled Sessions, model-callable Goal, worktree, Task, and Workflow tools are unavailable. Adding a tool requires explicit human approval and a spec update.
- Model/transcript-visible Session mutations follow the Active Session Runtime's accepted mutation order unless the Runtime Steering specification assigns the operation to an exact or direct owner.
- Tool definitions advertise one canonical tool name and canonical parameter names. Their schemas allow only recognized parameters. Kent accepts the hidden aliases below only for incoming model tool calls, and they do not expand accepted configuration names.
- Kent normalizes every accepted alias to its canonical tool and parameter names before execution, persistence, presentation, or later model context.
- Kent checks exact canonical spelling, listed semantic aliases in order, canonical camelCase forms, semantic-alias camelCase forms, and then case-insensitive forms. Every listed multiword canonical name and semantic alias also accepts its explicit kebab-case companion. Generic punctuation and fuzzy normalization are prohibited.
- Canonical parameters and their accepted casing or separator forms take priority over semantic aliases. Precedence among multiple semantic aliases is unspecified. Unrecognized parameters are ignored when a call reaches Kent.
- An active tool whose published name exactly matches the incoming spelling takes priority over a hidden alias. `patch` and `edit` are mutually exclusive: `edit` aliases `patch` when `patch` is active and is canonical when `edit` is active.
- Kent panics during startup in development and production when hidden aliases resolve one spelling to different tools, or parameter aliases within one tool resolve one spelling to different canonical parameters.
- `exec_command` tool aliases are `shell`, `bash`, `exec`, `run_command`, `shell_command`, `run_shell`, and `bash_command`.
- `write_stdin` has no tool aliases.
- `view_image` tool aliases are `read_image`, `open_image`, `inspect_image`, `vision`, `read_pdf`, `open_pdf`, and `inspect_pdf`.
- `patch` tool aliases are `apply_patch` and `edit`.
- `ask_question` tool aliases are `question`, `ask_user_question`, `request_user_input`, `ask`, `ask_user`, `ask_human`, `help`, and `say`.
- `trigger_handoff` tool aliases are `handoff`, `compact`, and `request_handoff`.
- `edit` tool aliases are `edit_file`, `str_replace_editor`, `replace`, `string_replace`, `replace_text`, and `write`.

| Tool | Canonical parameter | Semantic aliases |
| --- | --- | --- |
| `exec_command` | `cmd` | `command`, `script` |
| `exec_command` | `workdir` | `cwd`, `working_directory`, `working_dir` |
| `exec_command` | `shell` | `shell_path`, `interpreter` |
| `exec_command` | `login` | `login_shell` |
| `exec_command` | `tty` | `pty`, `use_tty` |
| `exec_command` | `raw` | `raw_output` |
| `exec_command` | `yield_time_ms` | `yield_ms`, `wait_ms` |
| `exec_command` | `max_output_tokens` | `max_tokens`, `output_token_limit` |
| `write_stdin` | `session_id` | `process_id`, `shell_id` |
| `write_stdin` | `chars` | `input`, `stdin`, `text` |
| `write_stdin` | `yield_time_ms` | `yield_ms`, `wait_ms` |
| `write_stdin` | `max_output_tokens` | `max_tokens`, `output_token_limit` |
| `view_image` | `path` | `file_path`, `image_path`, `file`, `pdf_path`, `filename` |
| `view_image` | `raw` | `raw_output`, `unoptimized`, `disable_optimization`, `original_quality` |
| `patch` | `patch` | `diff`, `patch_text`, `content`, `patch_content`, `input` |
| `edit` | `path` | `file_path`, `file` |
| `edit` | `old_string` | `old_text`, `find`, `search` |
| `edit` | `new_string` | `new_text`, `replacement`, `replace` |
| `edit` | `replace_all` | `all`, `global` |
| `ask_question` | `question` | `prompt`, `message`, `text` |
| `ask_question` | `suggestions` | `options`, `choices`, `answers` |
| `ask_question` | `recommended_option_index` | `recommended_index`, `suggested_option_index`, `default_index` |
| `trigger_handoff` | `summarizer_prompt` | `summary_prompt`, `handoff_prompt`, `compaction_prompt` |
| `trigger_handoff` | `future_agent_message` | `next_agent_message`, `handoff_message`, `continuation_message` |

- The canonical parameter `yield_time_ms` also accepts the mixed separator form `yield-time_ms`, its full-kebab form `yield-time-ms`, and its generated camelCase form `yieldTimeMs`.
- `steer` applies submitted input through that accepted mutation order.
- `queue` is the separate post-turn user Queue.
- A human steer is one user message and applies as one first-in, first-out Session mutation.
- A `kent run steer` invoked from another Session must emit each accepted submission as a separate developer-role `agent_steer` message in submission order.
- A `kent run` invoked from another Session must submit its assignment as the same developer-role `agent_steer` message, whether it creates a Session or submits to an existing Session.
- Human-started Run prompts must be user messages.
- Kent must not rewrite existing conversation history to change sender attribution.
- An agent-issued Run assignment or steer must contain exactly:

```text
Agent from another session said:
> <submitted text>

You can use `kent run steer <source-session-id> "message"` to respond.
```

- Kent must insert one literal `>` followed by one space immediately before the submitted text. It must not add quote markers to later lines.
- The message includes the source Session selector using the [CLI Session presentation rules](cli-commands.md#session-selectors-and-presentation) and omits its name.
- When rendering model-facing Session selectors in Kent prompts, tool output, and runtime notices, Kent must follow the same presentation rules.
- Kent must not rewrite Session identifiers in existing persisted messages or already-sent conversation history.
- When invoked by an agent, `kent run steer` must reject malformed invoking-Session identity before submission. An absent or blank invoking-Session identity uses human-steer behavior.
- Prompt history stores the complete wrapped message.
- Tool lifecycle effects, Runtime notices, additions after history replacement, and other model/transcript-visible results enter through the owning Agent Step or accepted Session mutation order.
- Line-by-line Markdown streaming is transient presentation, with no committed Runtime mutation.
- A Kent-executed tool call is durable before Kent begins its execution. Provider-hosted work may already be complete when Kent receives its outcome. That outcome follows the same compatible Result Group durability as Kent-executed results. Kent commits compatible complete tool results from one Agent Step in one or more ordered Result Groups. An Agent Turn is never a Result Group. Each result keeps its completion, model-visible output, and attached operator diagnostic together, whether it reports success or error.
- Result Groups follow provider-required order and otherwise the Agent Step's stable result order. Clients and later model requests see no part of a Result Group before the complete Result Group is durable.
- A dormant Goal command is the sole persisted-transcript exception outside an Active Session Runtime: the server atomically confirms that no current agent resource owns the Session, then persists the durable Goal transition and one Goal notice without creating a runtime, live event, transaction, rollback, repair, retry, or extra admission lock. A concurrent live release surfaces its error rather than re-admitting.
- The transcript includes Runtime notices that are not model-visible. A client never replays a Runtime Command after it loses the result or reconnects. Desktop must follow [Desktop Chat's failure contract](desktop-chat.md#failure-and-recovery). Other clients reissue Session reads and subscriptions after reconnect. A later explicit user submission creates a new Runtime Command.
- Input accepted before workflow completion becomes final supersedes that pending completion within the same Exact Execution Scope. Input submitted after that point, or while the scope is closing, receives a retryable rejection so the client restores its draft. It never reaches a successor Node or Session.
- History replacement is atomic from the model's perspective: replacement content and its new context become the next ordered conversation state together. Persisted transcript order never changes.
- Instant Stop, pending-input disposition, Runtime retirement, and loss behavior follow the Runtime Steering specification.

## Command Execution

- A user-run shell command must execute without invoking the model and must enter model context as a User Message, not as an assistant Tool Call or tool output.
- The User Message must contain `User ran a shell command:`, a newline, `<command>` followed by the command and `</command>`, a newline, and the shell tool's plaintext result. Failures must include their plaintext diagnostics.
- A user-run shell command must appear as a compact shell command in ongoing and collapsed transcript views. Its full User Message text and output must appear only in expanded Desktop or expanded detail views. It must not become a prompt-history entry or a Rollback Target.
- `exec_command` is the only model-facing command-execution tool. It uses the user's login shell without a TTY, inherits the parent environment, adds non-interactive technical environment values, and combines stdout and stderr into one unlabelled stream.
- Before launching a command, Kent must resolve a non-empty selected Working Directory to a normalized absolute path and verify that the path exists and is a directory.
- If a non-empty selected Working Directory is absent, Kent must not launch the command and must return `<normalized absolute path> does not exist, so the shell command was not executed. Please select an existing working directory`.
- If a non-empty selected Working Directory exists but is not a directory, Kent must not launch the command and must return `<normalized absolute path> is not a directory, so the shell command was not executed. Please select an existing working directory`.
- The global-only `shell.max_concurrent` setting must cap concurrently running agent-tool shells across the server, including foreground and background commands. It must default to 100 and accept only positive integers. This cap must exclude Workflow Scripts, hooks, and internal Git commands.
- A shell start at capacity must fail with a non-terminal, model-visible error: `Concurrent shell limit of %n reached. You can: a) help the user investigate why so many shells are running; b) wait for system load to decrease; c) clean up only shells you own; or d) raise the limit with the user's approval.`, where `%n` is the configured limit. Starts in progress must reserve capacity. Failed starts and process exits must release capacity. Completed-shell retention must not consume capacity.
- An empty selected Working Directory is validated by the shell manager and never falls back to Kent's server process working directory.
- An `exec_command` failure never adds the `exec_command failed:` prefix to its model-visible error.
- Commands have no lifetime limit. For `exec_command`, omitted `yield_time_ms` uses the default wait, explicit `null` waits until completion, `0` backgrounds immediately, and a positive duration waits up to that many milliseconds before backgrounding an unfinished command.
- This wait policy applies only to new command launches. An output-only `write_stdin` check with an omitted or `null` wait must use a 250 ms output collection window.
- Kent imposes no concurrent-command-process limit, including on background processes visible through `/ps`.
- A foreground command may run while later short Session settings, human input, and model-visible notices are accepted and applied.
- Kent applies a foreground command's terminal output or typed failure before another provider request starts.
- A backgrounded command cannot block unrelated provider or tool work.
- The process owner later records and delivers the background terminal result when the Session can receive it.
- For output-only `write_stdin` checks, Kent must clamp a negative requested duration to zero. A check requesting 0–1,100 ms must use a 250 ms output collection window. A check requesting 1,101–14,999 ms must fail with `Avoid polling repeatedly for short intervals, prefer 3-15min polls depending on task. Pick a better interval and retry`. A check requesting 15 seconds through 24 hours must wait normally for its requested duration or command completion. A check requesting more than 24 hours must fail with `This poll is too long. Consider using system cron jobs and \`kent run\` headless runs for tasks that require such long wait periods`. Sending non-empty input, including whitespace, must be exempt from these output-check limits and collection-window rules.
- A non-zero command exit is recoverable without aborting the model turn. Launch failures are not retried automatically. Interruption sends `SIGINT`, then `SIGKILL` after 10 seconds.
- If Kent's shell-log temporary directory has been deleted, Kent must recreate the same directory before launching the next command and run that command within the same tool call without requiring a server restart.
- If directory preparation or log creation fails, Kent must return an explicit infrastructure error without launching the command. Kent must not replay commands or continue retrying storage preparation.
- `[shell].postprocessing_mode` accepts only `none`, `builtin`, `user`, or `all`. Omission selects the built-in default. An empty or unknown value is an error. `[shell].postprocess_hook` is optional. Absence is `null`. A present empty or whitespace-only value is invalid.
- Each command captures its effective post-processing settings when it starts. That captured policy applies consistently to foreground output, background output, later `write_stdin` polls, completion notices, and shutdown processing, even if settings, role, or workspace change. `raw=true` bypasses that policy in every one of those paths.
- Except in `none` and `raw` modes, generic command-output sanitization runs before built-ins and the optional hook. Built-ins run before the hook. A built-in halt stops only later built-ins. In `user` and `all` modes, Kent runs the user-hook stage only when `[shell].postprocess_hook` is configured. An omitted hook silently skips that stage. A user hook receives JSON on stdin containing the original raw output before sanitization and the current output after sanitization and enabled built-in processing, and returns JSON on stdout. If a configured hook executable is missing, times out, exits nonzero, or returns invalid JSON, the current model-facing output is unchanged and Kent reports a warning.
- The post-processing architecture must support adding processors without replacing the processing pipeline.
- `/ps` shows background processes whose owning Session belongs to the selected Project and operates on processes selected from that list.
- Process recent-output previews are UTF-8 text. A preview leaves valid UTF-8 unchanged and converts each contiguous run of invalid bytes to one Unicode replacement character. This preview projection must not change raw shell output, saved logs, Inline Output, or raw byte offsets.
- Background process IDs are unique for one server lifetime. Their Session association controls Project list membership, notices, and history. Process termination uses the process ID without revalidating Project membership.
- Kent exposes at most 1,000 completed background processes per server lifetime. Completing another process removes the completed process with the oldest direct-access time and its saved log and completion output from `/ps` and process controls.
- Kent performs terminal event creation, delivery/finalization, and only then retention eviction and artifact cleanup.
- Direct process access refreshes completed-process recency. Listing `/ps` has no effect on recency.
- The completed-process retention limit never removes a running process.
- Accessing an unknown or removed process returns the same shell-result-unavailable error.
- The shell owner reports artifact-cleanup failures as operator diagnostics. Runtime Steering must not own or retry that cleanup.

## Patch And Image Tools

- For `gpt-*` models on first-party OpenAI providers, Kent must assume native vision input support unless the model catalog or explicit model capability configuration disables it. Other providers must not gain vision support from this model-name default. The `view_image` tool must follow the resolved model capability for both availability and execution.

- `patch` applies atomically: malformed or conflicting patches leave files unchanged. It supports add, update, move, and delete. It resolves real paths before validating targets, has no timeout, and is not retried automatically.
- Native file operations within the Session's Execution Target Root, a previous Execution Target Root trusted by its Runtime, or a Workspace learned by its Runtime must not require outside-Workspace approval. Other targets must follow the attached-Workspace lookup and ordinary approval rules below. Outside those trusted roots, `patch` and `edit` require approval unless `allow_non_cwd_edits=true`, while `view_image` follows the same rule for reads. All three tools also allow targets under the operating system's temporary roots and their canonical platform aliases without approval. Kent recognizes aliases such as `/tmp` and `/private/tmp` and `/var/tmp` and `/private/var/tmp` where those paths exist. Trusted roots and temporary-root allowances must not override path-deny rules or permit direct edits to another Kent-managed Worktree. The default for `allow_non_cwd_edits` is `false`. A denied edit returns an explicit error telling the model not to circumvent the decision and to request manual user edits when essential.
- An edit that targets another Kent-managed Worktree is forbidden even when that Worktree belongs to a Workspace attached to the same Project.
- `patch` and `edit` deny every target under the Worktree Base Dir loaded at server startup unless the target is inside the Session's current Worktree. This denial applies to a sibling Worktree created after the Session starts, and it occurs before any outside-Workspace approval.
- `view_image` resolves absolute canonical paths before checking access. Workspace checks must happen after symlink resolution, so a symlink escape must be checked against its resolved target. Image reads must use the same learned permissions and approval policy as edits.
- A Session Runtime must initially trust its Execution Target Root without loading the Project Workspace catalog.
- Before requesting outside-Workspace approval for an unrecognized target, native file operations must honor an existing `Allow for this session` decision, then consult authoritative metadata for the broadest attached Workspace root containing the target in the Session's current Project.
- A successful attached-Workspace lookup must permit the operation without outside-Workspace approval. The learned root must be trusted until the Runtime ends. Failed membership matches must not be cached. Detaching a learned Workspace must not revoke that Runtime's permission.
- A Runtime must trust successfully learned Workspace roots and its previous Execution Target Roots across ordinary Session moves, including cross-Project moves. Returning to those roots must not require another outside-Workspace approval. New membership lookups must use the destination Project after a successful move. Learned permissions must not survive Runtime retirement.
- Attached-Workspace permission lookups must use the existing 15-second metadata contention wait. If contention persists after that wait, the file operation must visibly fail without an approval request or a fatal-database panic. Metadata authority failures must not become outside-Workspace approval requests. For a genuinely unavailable metadata authority, debug builds must fail fast with diagnostics and production must surface the fatal server condition to the operator.
- Approved outside-workspace image reads appear in run logs with the requested and resolved paths.
- `view_image` opens and reads each local file in an isolated worker. Kent terminates the worker and returns a recoverable tool error when opening or reading takes longer than 10 seconds or the Agent Step is interrupted.
- When a successful patch cannot accurately describe its whole-file deletion count, Kent never invents a count or reverses the filesystem change. Debug mode fails fast with diagnostics. Production returns the successful path-only result and records an operator diagnostic excluded from model context without stopping execution.
- `view_image` must support non-animated WebP input. Optimization to WebP must not change transparency.
- `view_image` must use automatic image detail by default. When `raw: true` is used with a first-party OpenAI API or ChatGPT connection, it must skip optimization and request original image detail. Other connections must use automatic image detail when `raw: true` is used and must also skip optimization. These image rules must not change PDF or file attachment behavior.
- For supported non-raw raster images of at least 100 KiB, `view_image` must attempt WebP re-encoding after validation. Kent must validate the generated image with an independent decoder before accepting it as provider-bound output. Kent keeps the validated original when optimization fails or is not smaller, and always enforces the attachment-size limit.

## Tool Output And Failure Behavior

- `exec_command` and `write_stdin` must return model-visible plaintext for execution, polling, and error results. Polling must use the same shell-output formatting as command execution. Process lifecycle metadata must not wrap that text in a model-visible JSON object.
- Kent truncates large tool output for model context with standardized head/tail content and truncation metadata. The threshold is configurable and applies after command post-processing.
- Foreground shell output is evaluated after sanitization, post-processing, warnings, truncation, and presentation trimming. Whitespace-only content is no output.
- Each `shell` and `write_stdin` call independently applies the oversized-output guard after output processing and ordinary truncation. The guard carries no state between calls.
- The oversized-output threshold is half of the active Session's established context window.
- The oversized-output guard is eligible only when the current call explicitly requests `max_output_tokens` above the threshold.
- For an eligible call, Kent applies its standard token estimate to the final model-visible result. When that estimate exceeds the threshold, Kent omits all command output and returns a failed tool result.
- An oversized-output failure must say `Command was executed but the output you requested exceeded 0.5 of your memory size. It was forcibly truncated still to prevent your memory overload, and the output written to ${command.output_path} . Next time be more careful with larger outputs.`, replacing `${command.output_path}` with the saved shell-log path.
- An oversized-output failure never stops a running command or changes command execution, the complete shell log, or later independent polls.
- When `max_output_tokens` is omitted, the requested cap is at or below the threshold, or the estimated final result is at or below the threshold, the oversized-output guard has no effect on the call.
- A successful foreground command with output returns only that plaintext. Any completed foreground command without output returns `Exit code N, no output.`. An unsuccessful foreground command with output returns `Exit code N, output:` followed by the output.
- Windows shell results must report the original numeric process exit status, including values above the signed 32-bit maximum. Such statuses must work in live output, background completion, and reopened transcript history without truncation, negative reinterpretation, or rejection.
- Background completion and polling always include the exit code. Completion with no output says `Exit code N, no output.`. Lifecycle facts are separate from output summaries.
- Completed background polling with output must list wall time, the output-file path when available, and `Exit code N, output:` in that order, followed by the command output.
- Concise background output may hide an inline preview, but completion must expose the exit code and output-file location when output exists and must not claim there was no output. Recoverable warnings are visible and count as output, but do not imply command-log content.
- An invalid background-completion state fails fast with diagnostics in debug mode. Production reports the terminal process facts unchanged, records the diagnostic, and reports an explicit Kent error. It must not invent successful or empty output.
- Transient model-step failures retry after `1s`, `2s`, `4s`, `8s`, and `16s`. Ongoing mode shows concise model/API errors. Detail and logs include full details.
- After a provider HTTP 400, Kent may append error results for tool calls that were interrupted without output only when no matching live operation exists, rebuild the request, and retry. Each synthetic error uses the original call's output kind and states that the call was interrupted with no output. Kent never rewrites or removes history, never fabricates success, and leaves matching live operations alone. A 400 without missing outputs surfaces unchanged. Each live repair records a user-only warning with the number of closed calls.
- Speculative compaction must use this bounded missing-tool-output repair. Each corrected request fails immediately on ordinary provider failures.
- Before another provider request, Agent Step completion, history replacement, compaction, Question, Approval, or Workflow effect, every compatible complete tool result already owned by the current Agent Step is durable. A delayed background result becomes durable through its own Steering delivery rather than as part of an unrelated Agent Step.
- Interruption, user cancellation, provider failure, validation failure, and other recoverable errors finish the Agent Step. Kent makes their honest interruption or error results, notices, and warnings durable before it exposes committed transcript entries or terminal presentation for that outcome.
- A persistence failure before a Result Group becomes durable leaves that Result Group absent from the committed transcript and from committed client presentation. Kent surfaces the persistence error and ends the Exact Execution Scope without retrying that persistence operation or creating pending-persistence state. It must not invent a semantic terminal transcript outcome for the absent group.
- Once a Result Group is durable, later persistence observation or live-delivery failure never rolls it back or causes Kent to append it again. Reconnect and reopen hydration restore the authoritative committed Result Group.
- A failure while making an effect barrier durable blocks its pending Question, Approval, or Workflow effect and ends the Exact Execution Scope. Any already-durable Result Group is authoritative and is never appended again.
- A failure that prevents a model turn from starting is persisted as a user-visible developer diagnostic. Local command and validation failures that do not block a turn are ordinary errors.

## Questions And Approvals

- `ask_question` presents one shared question experience for model and runtime requests and pauses the active work until answered. Questions have no timeout or default cancellation.
- The model can ask ordinary freeform or suggested questions only. Internal approvals are not model-callable.
- Headless Sessions must use the ordinary Question and Approval mechanism. Agents must be able to answer pending access Approvals through the existing Question answer commands.
- Suggested questions support freeform commentary and use one-based `recommended_option_index`. In the TUI, `Tab` switches between suggestions and freeform editing. A recommendation uses a Success-colored marker and a faint recommended note. Selecting that row also applies the ordinary selected-row style. Choosing empty freeform opens editing, and a freeform answer cannot be empty. Returning to suggestions must not change an unsent freeform draft.
- Internal approvals belong to the tool call they approve and use that Tool Call ID throughout presentation and answer submission. Each tool call may present at most one internal Approval during its lifetime. Kent fails before presentation when an internal Approval has no owning Tool Call ID. Ordinary Questions use the Tool Call ID of their own `ask_question` call. Internal approvals carry ordered typed decisions. Clients must resolve their display labels locally and must not use server-supplied UI labels. Outside-workspace access ordinarily offers `Allow once`, `Allow for this session`, and `Deny`. Desktop must localize those labels on the client.
- Before an outside-workspace tool call presents its Approval, Kent must discover every attempted path that requires approval. One Approval covers the complete path set. The disclosure follows first-attempt order and includes each distinct path string supplied by the model once. Different aliases are separate even when they resolve to one target. A repeated identical supplied path appears once. Each disclosed path includes its different real resolved path when applicable. `Allow once` authorizes the separately deduplicated canonical target set. Kent revalidates each disclosed path-to-target mapping before access. A changed target or a later undisclosed target fails the tool call visibly and never opens another Approval.
- A Question or Approval is not presented when its preceding durability barrier fails. Kent surfaces the failure and ends the affected exact execution without replaying the blocked interaction.
- A live Agent execution waiting for a Question or Approval is interruptible even when no model or tool step is active.
- Ordinary Runtime Interrupt, Runtime Live Stop, Workflow Task Interrupt, Workflow Manual Move, an Approval answer, and a declined Approval race at the exact live tool call owner. An Approval action is accepted only after the waiting tool accepts its decision or cancellation and any selected session-wide grant takes effect. If the Approval action is accepted first, those effects occur once before Interrupt or Manual Move may stop any continuing tool work. If Interrupt or Manual Move closes the tool call first, the Approval submission is Skipped without applying commentary.
- The UI must not separately identify the Question's asking agent. Stored answers explicitly include the selected option number and commentary.
- An agent's Question answer must identify the answering agent's Session in both suggested-answer and freeform results delivered to the asking agent. Agent attribution must not change human-answer result wording or contents.
- When an answer request includes an invoking agent Session identity, Kent must validate that identity against persisted Session data and must not infer it from answer text or a display label. A request without that identity uses sessionless human-answer behavior and has no agent attribution.
- When Kent receives and validates an invoking agent Session identity, it must reject a sub-agent's answer to a Question asked by its direct parent Session and leave the Question pending. The error must identify the parent Session, explain that the question is likely intended for someone else, and state that the sub-agent must not answer questions it is not authorized to answer. It must not assume whether the intended recipient is a human or another agent.
- Direct-parent rejection is a cooperative agent policy. It provides no authentication boundary against a modified client or direct RPC caller. The answer request uses an unauthenticated invoking Session identity, so a caller may omit it or claim a different existing Session to avoid this restriction.
- Other agent answers must be allowed and must identify the answering Session. This restriction must not extend to earlier ancestors or change Approval behavior.
- Kent must save the answering agent's Session identity in Question answers and expose it unchanged after reload. This applies to new answers in both existing and new Sessions.
- Answers without recorded answering-agent identity must be treated as human answers. Kent must not rewrite their existing answer text.
- A Question answer goes directly to its exact pending Question owner and is not a Steering Intent. An Approval answer or decline goes directly to its exact live tool call owner and is not a Steering Intent. Single-prompt answers follow prompt order and do not survive restart. A submitted answer payload is immutable. An editable draft after failed delivery affects only a later explicit submission. Supported post-answer actions validate their inputs.
- A successful exact claim of a waiting Approval transfers that immutable action from its caller to the exact execution. If Kent observes caller cancellation before claim, the Approval is pending without effect. Caller cancellation after claim stops only that caller's wait. Kent completes optional Engine Intent Queue submission or terminal admission-failure handling, tool delivery, decision effects, and prompt finalization for Allow, Deny, and decline.
- An Allow decision with nonblank commentary first submits that commentary as one ordinary Steering Intent to the Engine Intent Queue. Engine Intent Queue acceptance is the process-local commentary commit boundary. After acceptance, Kent directly delivers Allow and waits for the waiting tool to accept the decision and any selected session-wide grant without waiting for the Steering Intent to execute. The Steering Intent applies in ordinary acceptance order at that Agent Step's normal Step Boundary and becomes model-visible before another provider request. Commentary need not become model-visible before the tool receives Allow.
- Allow commentary creates no Queue Item or separate Pending Work item. A retryable request or transport failure observed before exact Approval claim leaves the Approval pending with no commentary effect. After claim, failure to admit commentary because the Engine has closed ends the tool call with a visible error and closes the Approval without restoring it to pending. If Kent accepts commentary but cannot deliver Allow to the approved tool, Kent also ends that tool call with a visible error and closes the Approval. Kent adds no guarantee that accepted commentary survives a server restart before durable transcript history.
- Parent execution cancellation, sibling-tool failure, execution cleanup, and retirement follow ordinary execution ordering, with no Interrupt race guarantee. A claimed Approval has one terminal owner that arbitrates consumer acceptance against execution closure. Closure before consumer acceptance ends the tool call with one visible error and closes the Approval. Consumer acceptance first applies the decision once. Kent publishes the Approval's resolved state once only after either terminal outcome. Any accepted commentary has a single copy.
- If an accepted Allow-commentary Steering Intent fails during its Step-Boundary application, Kent ends that Agent Step with a visible error before another provider request. Kent must not retry the commentary, restore the Approval, or report the failed message as model-visible.
- Denial commentary travels only with the Approval answer and creates no separate model-visible user message. Patch, edit, and `view_image` denied tool errors use the literal `User also said:` commentary marker. This marker must not change each tool's surrounding error prose, quoting, serialization, typed payload where present, or client presentation.
- When one model response prepares several valid `ask_question` calls, Kent must make them pending concurrently in original model-call order under the same Step. Pending prompt projections must identify the original prepared Questions and whether any await materialization. A single-prompt answer is in flight until a later prepared Question becomes pending or the same Exact Execution Scope closes. Clients keep the accepted Question visible but disabled until authoritative prompt state changes or removes it. Desktop collects drafts and submits answers through its [Chat batch contract](desktop-chat.md#questions-and-approvals).
- A typed prompt-answer batch uses the Step identity as its only batch identity. Each entry uses its Tool Call ID. The batch and its entries have no generated prompt, request, operation, or Approval identity and no replay memo. Each submitted entry is an answered Question, an answered Approval, or a declined prompt.
- Kent validates the complete typed batch before resolving any prompt. A malformed entry rejects the batch without resolving valid siblings.
- The order of submitted batch entries has no meaning. Kent resolves submitted prompts pending for that Step in server prompt order. Ordinary Questions follow model tool-call order. Approval order across tool calls follows server materialization order and has no product guarantee.
- A Question or Approval omitted from the batch is pending. A live tool call waiting for its sole Approval may accept the first complete submitted action. A live tool call not waiting for Approval, a finished tool call, and a stale tool call are Skipped without applying commentary. A batch in which every submitted entry is Skipped succeeds without changing runtime state.
- A batch answer never waits for a later prepared Question to become pending and never pre-answers a future prompt. It completes after processing the submitted prompts pending for that Step or after an operational failure.
- The first complete action to reach each Question or Approval owner wins. Concurrent losing submissions are Skipped and apply no commentary. If a batch fails after resolving an earlier entry, that resolution is committed and every unprocessed entry is pending. Kent must not roll back, continue, replay, automatically retry, or save a batch result for recovery. After connection loss, Desktop must follow [Desktop's prompt-answer failure contract](desktop-gui.md#inbox-questions-approvals-and-notifications). Other clients read the latest completed pending-prompt projection. A later explicit submission for the same Approval uses the same Tool Call ID and may change its immutable payload only while that tool call waits for Approval.
- A successful batch response contains every submitted Tool Call ID exactly once and no other Tool Call ID. Each result is typed Resolved or Skipped. Result order has no meaning.
- A declined ordinary Question produces the same canceled/error Ask Question outcome as terminal cancellation, with no completed answer or synthetic user message. A declined Approval creates no separate decision row.
- Every canceled ordinary Question wait must return the model-facing explanation: "You were interrupted while waiting for an answer to this question after {{elapsed period}}. The interruption grants no authorization to make your own decision or treat any of the proposed options as chosen. Consider re-asking this question when appropriate." Kent must replace `{{elapsed period}}` with a short elapsed waiting period, such as `11m39s`, without adding persistence. This explanation must not change Question or answer state, cancellation behavior, or normal answered-Question behavior.
- Resolved-prompt updates carry Tool Call ID only. They never expose answer or decline content.
- Answered-Question history comes only from the Session event log. Kent must not store a database copy of Question text or answers.
- The Question-history command may read backward through the event log across its requested number of history-replacement windows. This command is an explicit exception to the normal prohibition on full-history transcript reads.
- Question-history reading must not block Session work and provides no consistency guarantee while the event log changes. It may return stale or duplicate Questions.
- A read or decode failure stops Question-history reading. Human output includes Questions already emitted. Streaming JSON may be partial and invalid.
- Question-history delivery reuses the generic subscription transport. The server emits start metadata, zero or more Questions, and final omission metadata in that order. Clients consume those typed events without a separate lifecycle-validation state machine. Generic transport completion is operation success. A transport failure is an operational failure even after final omission metadata.
- Question-history reading ignores provider-history items carried by history replacements. It reads self-contained Question completion events only.
- Event-log schema v2 stores structured Question answers and their answer commit time.
- Event-log schema v2 limits event-envelope field names, top-level event-payload field names, and tool names to 4,096 UTF-8 bytes. Writing or decoding an oversized discriminator fails visibly. Question text, answers, Commentary, provider history, and other payload content are exempt from this limit.
- Event-log schema v1 Sessions are openable, resumable, and writable. Question history uses their normalized presented Question text and verbatim flattened completion output without inferring selected options, Commentary, or answer time.
- Event-log schema v1 discriminator lengths are not constrained by the v2 limit. If bounded Question-history inspection encounters a v1 discriminator that exceeds the v2 limit, the read stops with a visible decode failure. It must not silently omit the record.
- A forked or cloned Session inherits its source Session's event-log schema version.

## Sessions, Location, And Transcript Bounds

- Sessions can stop and resume. The persistence root is configurable and defaults to `~/.kent`. Their durable location model is Project, Workspace, then Worktree.
- A user-driven Edit or Fork must create a new interactive main Session with a new Session ID and no Task ownership, Workflow execution, inherited Agent-role continuation, or parent-agent ancestry. It must never continue the source Workflow.
- A user fork must resolve fresh interactive settings, including model, provider, prompt, and completion settings captured in the source snapshot. It must inherit the source execution location, Worktree reminder, previous-Session provenance, complete Goal, and current desired Thinking selection unchanged. Goal and Thinking inheritance do not confer Workflow ownership.
- A user fork must leave the parent unchanged. The copied conversation prefix must follow the fork history rules without changes. Historical Workflow instructions may appear in that history. They must not confer Workflow ownership or authorize Workflow execution in the child.
- Workflow-internal clones are separate from user-driven forks and must follow the Workflow context-preservation contract.
- Session rebinds must follow the execution-target transition rules below for every caller, including cross-Project moves.
- A Workflow Session must not move to another Project. A cross-Project request must fail without moving the Session. Tool and CLI failures must explain: "Moving across projects during workflow makes no sense: the task would stay stuck in the old project".
- Full transcript history can reach dozens of gigabytes. Production must never load the full session log into memory or walk it from start to end, except when forking or cloning through the selected fork point because copying that history is the operation itself, when the Question-history, Session log, or Session search commands perform their explicitly requested backward history reads, when restoring a Workflow assignment through the bounded-memory backward read defined below, or when streaming a complete Session into an archive.
- Session history commands must stream backward with bounded memory. The CLI Commands specification owns their scope, output, progress, and cancellation behavior.
- When Workflow assignment restoration needs transcript history, Kent may read backward from the end with bounded memory until the latest history replacement boundary. The read must not cross into replaced history. If no history replacement exists, the read may reach the start of the Session. When existing assignment information suffices, a transcript read is optional. This permission must not allow modifying prior history.
- Other transcript access for active and dormant Sessions is limited to the requested bounded page or recent tail plus live streaming output. Model context contains only the bounded active segment established by compaction, never the full transcript.
- `server_host` and `server_port` explicitly select the daemon address. Kent binds exactly that address and fails startup if it is occupied. Local same-machine optimization is additive and cannot override either explicit setting.
- Session activation and release identify the exact session resource generation. A stale release is a successful no-op and cannot close or detach a replacement generation.
- Clients and servers using incompatible protocol generations refuse the connection. Operators must upgrade and restart both before making requests.
- Published custom protocol error codes are stable wire contracts.
- Absent session lineage is `null`. Empty or whitespace-only lineage values are invalid. A Session has separate optional previous-session provenance for human navigation and derived sessions, and parent-agent provenance for model-spawned subagent ancestry. Provenance can cross Projects. Derived Sessions inherit the source parent-agent ancestry and record the immediate source as previous-session provenance. Generic parent provenance means previous-session provenance, never model-spawned ancestry.
- Interactive startup is workspace-first. An unregistered workspace enters an explicit binding flow. Server browsing opens existing Projects and Workspaces only. Headless startup in an unregistered workspace fails without creating hidden state.
- Rebinding is explicit. Kent never infers it.
- A session selected for an interactive workspace prompts to rebind only when its attached workspace differs from the open workspace. Detached workspace-location records neither trigger nor supply a rebind. Attached location has one authority. A detached location record is not an execution fallback.
- Interactive Sessions are created lazily, at the first trigger that needs model work.
- A Session stores one unsent input draft for recovery. Clients restore the draft verbatim when opening the Session.
- Session listings expose the first-prompt preview when available. Clients decide whether to display it.
- Immediately before a Session's first model request, and after every compaction before subsequent model work, Kent locks or refreshes the model/provider setup, generation parameters, effective tool declarations and enabled tool set, system and developer context, skills, workspace information, current model-facing date/time, and conversation context for the current Session Contract. The main prompt-cache key is the Kent Session ID throughout.
- Transcript order is immutable for prompt-cache stability. A pre-commit Result Group persistence failure ends the Exact Execution Scope and makes no committed transcript change. Kent must not replay or reconcile a lost Session operation. A later explicit invocation is a new operation. Process death may lose the current Agent Step and process-local pending Steering as defined by the Runtime Steering specification.
- An explicit confirmed Agent replacement is an exception to the frozen Session Contract. The server must reject unavailable or known incompatible targets before stopping where possible, apply ordinary Stop semantics including input restoration, wait for execution shutdown, and revalidate before replacing the Agent with its complete baseline. New execution must not start during replacement. The server must rebuild the runtime configuration without rewriting historical content, changing the Session ID, forcing compaction, or automatically resuming. Workflow-controlled Agents must be locked. Accepted replacement must survive client disconnection.

## Authentication And Configuration

- TUI and headless startup, and goal, binding, question and worktree CLI commands, must use global/shared configuration and environment/CLI overrides for connection discovery. Initial targeting must use the current or explicitly selected workspace. Clients must not open the server metadata database locally. The connected server must resolve Session and caller-context lookups, including the Session's workspace. Main Workspace private ownership and operational settings must resolve in the running server after connection. A private-only endpoint override must not control pre-connection discovery.
- Provider access and authentication must follow [Provider Connections](provider-connections.md), including connection-local credentials, auth-less access, concurrent re-authentication, and terminal connection selection. Authentication failures and provider 401 errors must be actionable. Successful refresh is silent.
- A missing settings file must start onboarding, including first sign-in, before Session selection. Headless execution must refuse to start until onboarding has completed.
- The global `model` setting defaults to GPT-6.1 Sol. An omitted Supervisor model setting inherits the effective main model.
- `theme=light` and `theme=dark` select fixed palettes. `theme=auto` or omission detects terminal or system appearance.
- `debug=true` or `KENT_DEBUG=1` enables fail-fast diagnostic behavior for developer errors. Without debug mode, Kent must recover when possible or exit with a clear error without crashing for developer errors.
- Thinking levels pass through unchanged. Kent provides recognized choices such as `low`, `medium`, `high`, `xhigh`, and `max` only when the selected model/provider supports them. Otherwise, the user value passes through unchanged.
- `model_context_window` is model-specific and user-overridable. Reviewer and subagent context windows must be at least `40000`. `context_compaction_threshold_tokens` must be lower than `model_context_window`. Invalid values fail configuration validation.
- Each Session activation must resolve its context window, automatic-compaction threshold, and Compaction Mode from the current persisted-Agent-role configuration. These Context-policy facts must not be snapshotted in the Session Contract, rotate prompt-cache lineage, or otherwise invalidate provider caches. A resumed Session must use its provider-capability contract unchanged across runs.
- When no Session provider contract is established, Kent must resolve effective provider capabilities from the selected Provider Connection. The stateless New Chat settings read, dormant Context, and every Session plan must use that same projection without refreshing credentials, making a network request, or saving auth state. Connection configuration changes affect subsequent reads or plans without reconfiguring an already-live Session runtime. Request-time authentication owns credential refresh.
- `max_subagent_depth` is a root-level TOML setting with the precedence defined in [Configuration Resolution](#configuration-resolution). It defaults to `2`, accepts `0` through `30`, and is checked when a child is launched. `0` disables model-originated child launches. Other invalid values fail configuration validation. Values above `30` explain that recursion chains that deep are unsupported.
- OpenAI Responses `store` is configurable and defaults to `false`.
- `provider_identifier` defaults to `kent`, must be a non-empty HTTP product token, and supplies the OpenAI-family `originator` header and `<provider_identifier>/<Kent version>` user agent for main, reviewer, Workflow, and subagent model requests. It takes effect after restart for resumed Sessions. OAuth bootstrap, subscription status, and update checks do not use it.
- Backend HTTP clients advertise and decode zstd and gzip responses from third-party services. A supported compressed response is decoded even when the request supplied its own `Accept-Encoding` value. Local server-to-client communication uses no HTTP content compression.
- A model-provider request body is compressed only when the selected provider protocol requires a content coding, its uncompressed length is known and at least 1,024 bytes, and the body is replayable. Nil, empty, unknown-length, and non-replayable bodies are not compressed. ChatGPT Codex requests through OpenAI OAuth use zstd level 3. OpenAI API-key, OpenAI-compatible, and Anthropic requests do not use request compression.
- Explicitly supplied `Accept-Encoding` and `Content-Encoding` headers must be unchanged. Kent must not stack another content coding or retry a rejected request without compression.
- `tools.web_search` defaults to enabled. `web_search` is `native` or `off`. `tools.view_image` defaults to enabled and is advertised only to multimodal-capable models.

## Configuration Resolution

- Global settings default to `~/.kent/config.toml` unless the persistence root changes. Shared workspace settings use `.kent/config.toml`. Private Main Workspace settings use `.kent/config.local.toml`.
- Sources must apply in ascending precedence: built-in defaults, global settings, shared workspace settings, private Main Workspace settings, environment, and command-line overrides.
- Kent must merge explicitly supplied settings by full property key. A later value must replace only the same property. Omitted properties must inherit. This rule must apply to default-agent and nested settings, same-named subagent roles, tool and skill toggles, and Supervisor settings. An explicit `false` must override an earlier value.
- Ordinary environment and command-line agent overrides must take precedence over explicitly configured properties of the selected subagent role. These overrides must not replace explicitly configured Supervisor properties. Supervisor configuration must be independent and inherit main-agent values only where the Supervisor property is omitted.
- The environment and command-line tools lists must replace the entire available toolset, including disabling unlisted tools. The selected list must take precedence over configuration-file tool settings. A Session must save its first explicitly supplied list and use it through compaction and reopening without requiring the original option again. Subsequent lists must not change it. Command-line selection must win when both sources supply the first list. A Session without a saved list may adopt one on a later launch, but Kent must not infer one from locked tools. If a Session Contract is already locked, the adopted list must take effect after compaction without rewriting locked history. Session Contract refresh must use the saved override.
- A rollback fork must use tools from its new launch configuration for its first model request and must not inherit the source Session's saved tools-list override.
- The private configuration file must accept all workspace-allowed settings and reject global-only settings. A missing private file must contribute no overrides. It must be outside shared repository configuration. Managed Worktrees must read the Main Workspace private file directly, without copying it or reading a private file inside the managed Worktree. Changing the Working Directory or selected Worktree must not change the Main Workspace private layer.
- A higher configuration layer's `system_prompt_file` must replace the lower layer's configured selection. A role-specific selection must override the inherited default-agent selection. Kent must not accumulate competing configured custom prompt files. This selection must not change Automatic `SYSTEM.md` discovery and priority, built-in prompts, AGENTS.md, Skills, or locked Session history behavior.
- Kent must reject an explicitly empty configured `system_prompt_file`. Omission must inherit the lower-layer selection.
- Settings with file-relative paths must resolve against the configuration file that supplied the winning value. `worktrees.setup_script` must resolve relative to its source Workspace. Effective-setting reports must identify the winning layer, full property key, and originating file path or environment/command-line option. Inherited properties must report their originating source unchanged.
- Clients must perform only the minimum validation supported by the configuration library and needed to read and try the values they need. Clients must take on no other configuration-validation responsibility. If the library cannot parse a configuration file, the client command must fail.
- Client configuration reads must be limited to connection discovery and genuinely local preferences, including client lifecycle hooks. TUI Session startup and settings must use the existing server-owned Session and Chat settings APIs rather than reading operational configuration locally.
- The server must validate operational configuration at startup. Clients must not initialize server persistence, Worktree directories, or managed ripgrep configuration. The server must own Session preparation.
- Operational configuration errors must identify the offending source. The server must validate the effective operational configuration and must not replace invalid values with defaults.
- Server operational configuration loading and worktree setup must validate merged explicitly configured context constraints. Context budgets derived from the selected model must be validated at launch.
- Main-agent, subagent, Supervisor, Workflow, worktree-setup, and read-only settings operations must use the same configuration resolution rules. Worktree setup must validate the full configuration, including agent settings.
- If global and workspace settings resolve to the same physical file, Kent must apply it once as global settings, disable that shared workspace layer, and provide diagnostics for both resolved locations.

## Model Requests And Cache Continuity

- Kent must group captured Session properties by Snapshot Policy. Refresh eligibility must be separate from permission to apply a change to the active context.
- Compaction must expire the generation-scoped Session Contract without expiring the independent Workflow Snapshot. Workflow completion-mode refresh boundaries are owned by the [Workflow Orchestration specification](workflow-orchestration.md).
- Provider cache impact must be independent of snapshot refresh eligibility and application permission. A permitted refresh gives no assurance of provider cache reuse for a changed request prefix.

- Every model generation request uses the provider's streaming response protocol. Kent has no non-streaming generation or fallback path.
- Callers that expose only a completed response consume the generation stream internally and return the assembled response only after the stream completes successfully.
- One generation-stream contract carries assistant output, reasoning output, provider-hosted output, and stream activity.
- When Kent records a response, its Reasoning Traces must precede assistant `final` text from the same Agent Turn in persisted transcript order. Reasoning Traces may follow assistant `commentary` text from that Agent Turn.
- This persisted-order requirement must not buffer or reorder TUI Ongoing Mode output. Assistant text must continue streaming immediately.
- Every generation request has a required tool-choice mode: automatic or required. Missing or unknown modes are invalid.
- Required tool choice validates against the complete advertised tool set, including local, custom, and enabled provider-hosted tools. An empty set is invalid. A provider that cannot represent required choice returns a policy error before dispatch. Automatic and required requests use the same bounded provider- and transport-failure retry policy. A retry must use the request's tool-choice mode and advertised tools unchanged. Kent never falls back from required to automatic choice.
- Tool-choice mode changes only tool selection. It never changes the advertised tools or their order, parallel-tool behavior, or prompt-cache identity. Exact counting of a built request must use its tool mode and complete tool set unchanged. Standalone estimation uses automatic choice.
- On supported models and request modes, existing human and Workflow Thinking controls must use native configuration updates while keeping the active context's original request-level reasoning effort unchanged.
- The Session must own its desired Thinking effort independently of Session Contract snapshots. Effort resolution must use the Session override, then the Agent configuration, then global top-level configuration, then Kent's default.
- On each Agent Step, Kent must compare desired Thinking with the last applied effort and issue only the final needed configuration update. Idle and between-Step changes must not produce intermediate updates.
- Dispatched configuration updates in the active conversation must determine the last applied Thinking effort. If no update exists, the original provider effort applies, subject to the post-compaction re-establishment requirement.
- The fixed original provider effort must belong to the Session's active context independently of Session Contract snapshots. Resume must not change it. A successful compaction must end its applicability when the context changes. Forks with compatible copied history must inherit it. Unsupported requests must ignore it and use the current desired Thinking effort directly.
- Dispatched configuration updates must occupy their original conversation positions without creating an ordinary visible Chat message.
- Each committed Thinking configuration-update item must project a persistent transcript entry showing its selected Thinking level at its committed conversation position, including post-compaction reestablishment. Live delivery and restored history must show the same entry and order. Changing the desired selection without committing a configuration-update item must not create a transcript entry.
- Changes made before dispatch must use the final effective Thinking value without producing adjacent configuration updates.
- For all models, Kent must keep queued input pending through request preparation and commit it at the final preparation boundary. When a native Thinking update is needed, Kent must commit it before the new user input. Preparation failure may leave input pending rather than submitted. Until commitment, input is subject to the process-local Pending Work lifetime.
- If applying desired Thinking would require adjacent configuration updates or changing dispatched history, Kent must use the last legally applied effort and reevaluate on the next Agent Step. The Thinking display must show the desired selection.
- A compaction request must use the outgoing context's Thinking configuration. A committed compaction replacement must clear that context's original provider effort without changing the desired Thinking selection. The first subsequent supported request must establish its original provider effort from the then-current desired Thinking selection. An uncommitted compaction replacement must leave the original provider effort unchanged. Kent may also re-establish Thinking through a configuration-update item after compaction.
- An existing Session must initialize its fixed reasoning baseline from its current effective Thinking level when it first uses configuration updates. During adoption, Session-owned state must hold that effective selection unchanged. This initialization may cause a cache miss and must not reconstruct or rewrite earlier requests.
- Unsupported models and request modes must use ordinary Thinking behavior.
- Native Thinking updates must apply to supported GPT-6 Astra, Sol, and Luna requests on the official OpenAI and ChatGPT/Codex endpoints. Custom OpenAI-compatible endpoints must use ordinary Thinking behavior.
- For models whose catalog supports `none`, disabled Thinking must resolve to the explicit `none` effort in setup, Chat settings, model requests, and compaction. Session resume must use that selection unchanged without changing dispatched history.
- If an enabled endpoint rejects a native Thinking update, Kent must surface the request failure without falling back to request-level Thinking changes.
- When constructing the separate Reviewer input, Kent must exclude the main agent's configuration updates without changing the Reviewer's independent Thinking selection. This exclusion must not alter the main Session's recorded history or replay.
- When a new fork targets a model or provider without native Thinking update support, Kent must exclude those updates from the child's initial input without failing because of them. Parent history and the order of the remaining child input must be unchanged. This exception must not change input already dispatched by the child.
- A fork must inherit the parent's current desired Thinking selection subject to the target model's existing settings rules. Its next Agent Step must reconcile that selection with the copied history.

## Grok Model Behavior

- Kent's built-in Grok catalog must include `grok-4.6` and `grok-4.7` for public API access. Kent must not include Grok 4.5 in its built-in catalog or defaults or claim CLI subscription-proxy availability for 4.6. Unknown manual model entry must follow the existing provider-dispatch rules.
- Kent must treat manually entered Grok 4.5 as an uncatalogued model.
- Public API-key and public OAuth connections must use a 500,000-token default context window for these models without a separate larger-window choice.
- On CLI subscription-proxy connections, Grok 4.7 must use a 256,000-token default context window and offer a 500,000-token larger window.
- The catalog must report Grok 4.7's knowledge cutoff as May 2026 and Grok 4.6's as February 2026.
- Grok 4.6 and 4.7 must support low, medium, high, and xhigh Thinking effort. Grok Thinking must default to high.
- Across providers, Kent must offer disabled Thinking only when the model's supported efforts include `none`. Kent must always offer custom Thinking entry. At backend request time, Kent must reject efforts outside a catalogued model's supported list. For uncatalogued models, Kent must send custom efforts unchanged for provider validation.
- Grok must apply between-request Thinking changes through the next request's effort setting. Grok must not use cache-preserving Thinking configuration-update appends or apply Thinking changes during generation.
- Grok must support image input, tool calls, and Kent's existing native Web Search for supported models. Grok Reasoning Summaries must appear unchanged as Reasoning Traces without requiring OpenAI's bold Thinking Status format.
- Kent must ignore unsupported Grok configuration options such as verbosity.
- Grok local context estimates must use its provider's text, image, and encrypted-content accounting without OpenAI's encrypted-envelope deduction. Provider-reported context usage must take precedence over estimates. Compaction policy must not change with estimator selection.
- Grok public-API OAuth and API-key access must support native compaction through the selected service's `/responses/compact` endpoint. The Grok CLI subscription proxy must not advertise or dispatch native compaction. Kent must report rejected compaction without automatic fallback.
- When current capabilities govern, a configured native mode unsupported by the Grok CLI proxy must resolve to local compaction. A Session whose locked contract selects native compaction must receive an unsupported-compaction error without a proxy-native request. Kent must not migrate that contract or retry a rejected provider request through another compaction mode.

## Fast Mode And Context Usage

- Fast Mode is a persisted Session Chat setting available when current capability metadata for the selected Provider Connection declares support for Kent's priority service behavior. Built-in OpenAI and ChatGPT/Codex connections declare support by default. All other connections default to unsupported unless their current metadata explicitly declares support.
- Grok connections must support Fast Mode through `service_tier=priority`. Fast Mode must not change the selected Grok model.
- Fast Mode availability is resolved from current Provider Connection metadata without refreshing authentication or making provider network requests. Locked Session provider facts do not determine availability, and availability is not a Session Contract fact or prompt-cache identity input.
- Changing Fast Mode during an Agent Step persists and publishes immediately, affects the next provider or compaction request, and never changes the request already running.
- A Fast Mode change creates no transcript row.
- A supported request uses the provider's priority service tier when Fast Mode is enabled.
- Disabled Fast Mode omits the provider's priority service tier.
- Enabling Fast Mode for an unsupported provider fails without changing the Session setting.
- Reopening a Session restores its effective Fast Mode setting.
- Changing Fast Mode creates no additional Session Contract generation.
- Reviewer and compaction requests inherit the Session's effective Fast Mode when their provider supports it.
- Context usage uses current provider-reported usage when available and Kent's established current-context estimate otherwise.
- Kent's current-context estimate must use the local estimation behavior owned by the resolved [Provider Connection](provider-connections.md#ownership-and-selection). Provider-specific estimates must not change compaction policy or the precedence of provider-reported usage.
- Kent must distinguish provider-reported context occupancy from billable token categories. Missing provider counts and context occupancy must be nullable. Reported zero context occupancy must take precedence over estimates.
- Compaction selection compares current usage with the configured thresholds.
- Kent must not predict future token growth from earlier turns or maintain a separate adaptive compaction policy.

## Historical Provider Usage

- Kent must record provider usage for successful model operations in Session history, including ordinary turns, compaction, Reviewer work, and Supervisor work.
- Kent must record the operation's provider, model, time, reported token categories, and available billing-relevant details, including hosted-tool usage, so external consumers can look up prices independently.
- Session resume and compaction must not change historical usage. Historical usage is independent of current context usage.
- Missing usage information must be nullable and must not mean zero consumption.
- During the data cutover, saved usage projections must interpret zero usage values as absent. After the cutover, recorded provider-reported zero usage must be explicit and must not be interpreted as absent.
- Recording subsequent operations must not fabricate missing historical usage. Saved observations must not certify complete Session billing history.
- When Session history is copied, saved usage must carry its original Session and operation identity unchanged. Copying history must not represent another provider operation.
- Usage retention must not calculate monetary costs or fetch or maintain model prices.

## Compaction

- Compaction starts a new bounded active conversation while retaining the full durable Session history. Every successful compaction must commit its immutable output independently of later model work. Kent must show the compaction summary when compaction finishes. The first live request using that output must prepare and commit only its fresh generation context without modifying or saving another copy of the output.
- All compaction triggers must share one context-replacement operation. Preparing a saved summary for its next operation must not run compaction again, increment the completed-compaction count, or repeat compaction completion feedback.
- When configuration selects no mode, `compaction_mode=dynamic` must be the default. Explicit `local`, `native`, and `none` selections must use their defined behavior.
- Dynamic compaction must ask the model which carryover-summary sections are relevant, assemble the handoff prompt from the selected section templates, and generate the resulting sectioned carryover.
- Dynamic sections must be sections of the handoff summary, not separate persistent note files.
- Dynamic compaction must use the existing compaction and handoff lifecycle. Selecting this mode must not change compaction trigger timing or add a separate context-reset tool.
- Dynamic compaction must start the fresh active context with its carryover and guidance explaining the handoff and the Session history commands for recovering older information.
- Dynamic compaction must not rewrite or roll back already-sent section-selection history.
- Fresh main hydration, Reviewer request construction, and post-compaction hydration share one canonical stable prefix: applicable Headless context, Subagents, Skills, Worktree context, Agents.md instructions, then active Goal continuation or Workflow context.
- Workflow Sessions assigned to a managed Worktree must include that Worktree's context in their first model request. Worktree context must use distinct wording for conversation startup, post-compaction continuation, and an execution-target switch. Post-compaction wording must describe the inherited execution context without implying that compaction moved files.
- Fresh main and Reviewer requests append Environment after that stable prefix.
- Post-compaction hydration must place compacted or handoff output before Environment and manual user carryover after Environment, except for the provider-native continuation ordering below.
- Successful OpenAI V2 compaction must validate and save one encrypted checkpoint. It must place its continuation reminder as the first ordinary developer message after the system prompt, before other developer context, the unchanged checkpoint, and Environment.
- Successful Grok native compaction must keep the complete returned output array, including every item, its order, and opaque provider content, contiguous and unchanged through saved output, hydration, resume, subsequent generation, and re-compaction. Grok must use the same generation-context layout as other providers, with the continuation reminder and stable context before the output array and Environment after it.
- Each native continuation reminder must belong to the saved generation and use the ordinary developer-notice contract. Local compaction must use its summary wrapper without that native reminder.
- Compaction continuation guidance must carry the original user objective, subsequent corrections, decisions, constraints, and remaining work unchanged. It must direct reuse of completed work and verification unless new evidence makes them stale.
- A single editable Markdown document must own compaction continuation guidance. Native compaction inserts that guidance directly. The local handoff-summary wrapper includes it through a template.
- After successful compaction, Kent must defer base meta context and running-shell capture until the first live model request using the saved summary starts. This behavior must apply to local, dynamic, provider-native, Workflow, and handoff compaction. An idle interval, Session reopening, or offline inspection must not prepare or persist that context early.
- Kent must capture the running shells invoked by that Session at that request boundary. Kent must not persist or transfer a pre-request shell snapshot. Once the request starts, its ordinary developer reminder must become part of the persisted model history.
- Before committing generation context, Kent must establish the Session Contract used by the request. The model named in Environment must match that contract. If later request preparation fails, Kent must keep that contract together with the committed context.
- Once the live request operation commits its generation context, Kent must keep that context even if request preparation or provider dispatch fails. Later retries must reuse that committed history.
- Messages accepted between compaction and request preparation must appear once with their original identity. The prepared model context must include those messages without saving another copy of them.
- When that snapshot contains running shells, Kent must include one ordinary developer reminder in the committed generation context immediately before Environment, after the compacted summary or checkpoint. The provider-native continuation reminder must keep its first position.
- The reminder must include every captured running shell's ID and command preview without a shell-count limit. The preview must collapse whitespace to single spaces and use at most the first 120 Unicode code points of that result, without an ellipsis or other truncation marker.
- The reminder must describe the shells as running when captured. Kent must omit finished shells and omit the reminder when no owned shells are running. Capturing the reminder must not wait for shells to finish or change their execution.
- An editable Markdown document must own the reminder heading, with the shell list inserted through an `ActiveShells` template value.
- Handoff output includes the user-authored future-agent message in the atomic history replacement.
- Goal and Workflow are alternative Session-mode context in their respective slots.
- Outside Workflow execution, Kent includes an active Goal continuation only for an active Goal.
- Paused, completed, cleared, or absent Goals omit Goal continuation from model context.
- Opening or resuming a Session without replacement adds no Goal continuation.
- A resumed non-Workflow Session carries its active Goal verbatim into new model context with the ordinary Goal work and completion guidance, without referring to compaction.
- If a later notification fails, compaction is complete and Kent reports the failure without repeating compaction.
- Kent must accept user input without a separate pre-submit compaction check. Ordinary automatic compaction at model-request preparation must be available for long turns and oversized prompts.
- For non-Workflow speculative compaction, an explicitly configured `pre_submit_compaction_lead_tokens` must set the trigger to `model_context_window - pre_submit_compaction_lead_tokens`. When the lead is not configured, the trigger must default to 88% of `model_context_window`, rounded down to whole tokens. The ordinary automatic threshold must not determine this speculative trigger.
- An explicitly configured lead must be positive. Normal automatic and speculative compaction thresholds must be at least 50% of `model_context_window`. Invalid settings must fail configuration validation.
- When automatic compaction is enabled, Kent must eagerly compact an eligible non-Workflow Session after any successfully completed agent/model turn with a nonblank final answer when authoritative consumed context usage is at least the speculative trigger. This comparison must exclude reserved output tokens. Eligible paths include direct and queued user-message turns, Agent Steer turns, Goal Loops, and background continuations. Workflow turns are excluded. Main and subagent Sessions are eligible in both interactive and Headless modes. Interrupted turns, failed turns, turns waiting for a user answer, and no-final or silent-final outcomes are ineligible.
- Before speculative compaction starts, Kent must revalidate the Session's current automatic-compaction enablement and consumed-context threshold.
- If either automatic-compaction condition is false, Kent skips compaction without a provider call or history replacement.
- A user message submitted while eager compaction is running waits behind that compaction and is then processed against the compacted context.
- Eager compaction is speculative. Its failure must leave the preceding successful turn and uncompacted context unchanged, use ordinary diagnostic reporting, and receive no automatic retry.
- When Supervisor review and speculative compaction follow the same completed turn, they must run in parallel. The review must use the completed turn's uncompacted context. Supervisor feedback may be saved, displayed, and queued before compaction finishes through ordinary steering. The main agent must process that feedback after compaction finishes. If compaction fails, queued input and Supervisor feedback must continue against the uncompacted context.
- A failed compaction must use ordinary run-failure reporting for its durable error message. Compaction progress must report the failed outcome without appending a second error message for the same failure.
- `compaction_mode=none` disables manual and automatic compaction and lets provider context-overflow errors surface.
- A manual compact request is a typed Pending Work item and follows the Session's accepted mutation order.
- Compaction is an Agent Step selected after earlier accepted short mutations apply according to the Runtime Steering specification.
- A manual compact request is never model-visible user text.
- Kent must revalidate manual-compaction policy and eligibility when the pending request starts unless the conversation is already compacted and awaits its next model request.
- Manual compaction has canonical presentation `/compact` followed by normalized guidance when present.
- Each repeated manual compact request is distinct.
- Clients do not coalesce manual compact requests.
- Each manual compact request receives its own typed outcome.
- Agent Steps do not overlap, so manual, automatic, and handoff compaction cannot overlap another compaction Agent Step.
- Repeated compaction of a conversation that is already compacted and awaits its next model request must succeed without another provider call, history replacement, or preparation of the next request's context.
- Otherwise, manual compaction must require at least one Agent Step boundary since Session creation or the latest successful compaction. The active Agent Step satisfies this requirement when its boundary is reached. A rejected request starts no provider call and commits no history replacement.
- Disabled-policy and too-soon manual-compaction failures are typed server-owned outcomes shared by every client.
- Human-facing text calls this operation `compact`. Model-facing context calls it `handoff`. Manual compaction carries the last visible user prompt. Agent handoff may carry a future-agent message visible only in detail.
- The main-agent prompt-cache key is always the Kent Session ID. Compaction and Session Contract changes add no cache-key suffix, namespace, or generation. Reviewer requests use the Session's reviewer key.
- Every production model dispatch must record the exact dispatched cache lineage before sending and the provider-reported cache reuse after a successful response. Generation, Reviewer, local compaction, and provider-native compaction provide no unobserved dispatch path.
- Native and local compaction requests must use saved history unchanged within their protocol's request shape. OpenAI V2 compaction must use the maximal unchanged prefix of the preceding model request, using the ordinary Session Contract, system prompt, tools, and active history before its compaction trigger. Local compaction must use the maximal unchanged prefix and include the complete current active history, including the prior compaction replacement.
- Every provider's compaction request must use the original tool declarations, system prompt, and other cache-snapshotted content unchanged. Kent must not rebuild the system prompt or insert a substitute system message during compaction.
- Grok native compaction must use the actual Session cache key, effective priority tier, instructions, and configured controls. It must omit the OpenAI compaction trigger.
- Grok compaction must not probe field support, strip fields and retry, or switch routes after rejection. Any additional field omission must require demonstrated service rejection and an explicitly approved contract change.
- Compaction cache observations must describe the request actually dispatched, including its actual Session cache key, tools, and input.
- The compaction request must use the pre-compaction Session Contract. When replacement history commits, Kent must clear the full Session Contract snapshot, including model and provider capabilities, generation parameters, enabled tools, and system/reviewer prompts. Every compaction trigger must use this same boundary behavior, including manual, automatic, handoff, and Workflow compaction. The next model request must populate and lock a fresh snapshot. An uncommitted compaction failure must leave the outgoing snapshot unchanged. A later notification failure must not restore a snapshot cleared by committed compaction. Changed prompt-facing content naturally invalidates the changed request prefix. The prompt-cache key is the Session ID.
- Compaction alone emits no cache warning. `cache_warning_mode=default` records confirmed non-postfix invalidation and observed cache-reuse disappearance in Detail. `verbose` exposes the same warnings in Ongoing. `off` disables cache warnings.
- Compaction must not suppress a separately confirmed change to saved request metadata, including the system prompt, tools, or structured-output schema. A confirmed non-postfix change must be reportable when aggregate cache reuse increases or provider cache usage is unavailable. Kent must omit a token-loss amount when the observations cannot quantify it. Discarded pre-compaction history must not be counted as loss caused by a metadata change.
- Local compaction permits no tool calls. Each attempt that receives one records a model-visible and user-visible instruction not to call tools and to retry. Kent retries up to three times, then fails compaction and stops the model loop. Local summaries use automatic tool choice. Post-compaction Workflow work uses the Node's ordinary generation policy.
- If compaction both exceeds provider context length and receives the corresponding provider error, Kent retries the compaction request with cumulative supported earlier tool-payload collapse targets of 10%, 20%, and 40% of the model context window. Shell output and patch input become exactly `<collapsed>`. Calls and their output relationships must be unchanged. Reasoning and unsupported payloads must also be unchanged. A successful repair records the collapse count and estimated omitted tokens for the operator.
- Completing compaction alone adds no UI-only transcript entry. Transcript-visible summaries are ordinary transcript content.
- Persisting the Context read's completed-compaction count or manual-Compact eligibility is best-effort. A failed metadata write must not fail an otherwise successful compaction or Agent Step. Kent reports the failure through its operational diagnostics and adds no retry or recovery flow for these facts.

## Goals

- Goal inspection reads the durable Session Goal without requiring an Active Session Runtime. Running and dormant Sessions return the same authoritative result with required Goal availability and an optional durable Goal. A valid Session with no Goal returns authoritative Goal absence. Unknown or inaccessible Sessions return errors.
- A Goal observation is an exact-Session read-only subscription that is independent of transcript observation and Active Session Runtime lifetime. Its sequence-1 hydration independently loads a latest-completed durable Goal or authoritative Goal absence with Goal availability. Hydration may be stale relative to a concurrent mutation, and the observation may miss a change between that read and subscription registration. Each change observed after registration has the next sequence number. Goal observation never waits for or coordinates with Goal mutation authority to establish freshness.
- Opening, holding, reconnecting, or closing a Goal observation never creates, holds, releases, interrupts, or otherwise changes Runtime work. Closing stops only that client's Goal delivery.
- Goal inspection excludes runtime-local goal-loop suspension. Live clients derive suspension from runtime status.
- Successful Goal mutations through an Active Session Runtime emit typed Runtime goal-status updates carrying the projected Goal status state. Frontends must use these updates and must not infer status from transcript feedback or run lifecycle. Set, pause, resume, complete, and clear emit Runtime updates. Show and read-only operations emit none. A dormant mutation emits no Runtime goal-status update. Its command response reports the mutation result, and an already-registered Goal observation may independently receive the committed change.
- A Goal mutation response is exactly one of authoritative Goal or authoritative Clear. Each result carries Goal availability independently and may omit it.
- Set, Pause, Resume, and Complete return the authoritative durable Goal. Clear returns authoritative durable Goal absence.
- Goal Set requests must carry an explicit execution policy. `start_or_continue` resolves and prepares the target Session when required before invoking ordinary Goal Set behavior, while `preserve_runtime_state` invokes the live or dormant Goal mutation owner without preparing a Runtime.
- New Chat Goal Set uses `start_or_continue`, and exact-Session Desktop Goal Set uses `start_or_continue`. Exact-Session CLI and model-shell Goal Set uses `preserve_runtime_state`. An agent request additionally carries the exact Run and Agent Step identity required by the Goal admission authority.
- A target-aware Goal Set operation returns the exact resolved Session whenever Session creation succeeded, including when later Goal validation or admission rejects the mutation. A failure after committed Goal work must be a diagnostic on the successful Session-bearing result and must not be reported as an uncreated Session or replayed.
- `kent goal clear` prints `Goal cleared` after a successful authoritative Clear response.
- The public Goal mutation protocol uses only that closed result. It never reuses the read-only Goal response, never combines variants, and has no response-shape negotiation or fallback.
- `/goal <objective>` (TUI slash command or GUI path) submits a typed Session mutation that sets the Session Goal, replacing any existing Goal.
- Goal behavior owns the start and continuation of model work caused by a Goal mutation.
- `/goal resume` on a completed goal reopens it as active.
- `/goal resume` on an already-active goal is a no-op with no model-facing messages.
- Goal mode requires `ask_question` in the locked tool surface for active model loops. Validate parity at model-work startup and surface a normal runtime error if violated. This parity is enforced inside workflow-controlled Sessions too, so a Goal set there requires `ask_question` visibility as well.
- Lock: `ask_question` visibility and `/questions` state are separate contracts. Missing `ask_question` from the locked tool surface blocks goal model loops. `/questions off` only makes `ask_question` calls return the questions-disabled tool result and must not stop, suspend, or block active goal execution.
- During live Workflow exact execution, user and agent Goal mutations follow the ordinary Goal rules. Goal continuation is passive.
- A Workflow control-only Runtime held for later work accepts a user Goal mutation only when it is a no-op, requires no new ordinary Goal execution, or is already owned by the current Exact Execution Scope.
- A Workflow control-only Runtime held for later work rejects a user Goal mutation that requires starting new ordinary Goal execution before Steering acceptance through ordinary Goal mutation error feedback.
- While an Exact Execution Scope drives the Session for Workflow Execution, the Goal is a passive objective: no separate Goal Continuation Loop operates, and the active Goal's reminder is folded into the Workflow's invalid-completion nudge.
- Outside workflow-controlled execution, active-Goal continuation after history replacement is a Markdown-backed developer initialization message. It begins `Heads up: this session has an active goal. Goal text:`, inserts the exact Goal verbatim between the ordinary nudge's `<goal>` markers, says `Continue working towards the goal from the resumed state.`, and then reuses the ordinary Goal nudge's shared `Work mode` and `Completion discipline` guidance. Agent-facing continuation text never mentions compaction.
- A valid terminal workflow completion soft-cascades an active goal to complete (actor=system) in the same step, across structured-output, unstructured-output, tool, and shell-command completion intents. A valid completion is never blocked by an active goal. Paused goals are left intact. The cascade applies only if the same goal is active when it commits. For tool-mode completion it is emitted after the terminal tool result is persisted so it never interleaves a non-tool item between a tool call and its result.
- That terminal cascade must observe only the Goal already durable when completion commits. A Goal mutation must persist during its admission, including within the same Agent Step.
- A same-Step Goal mutation that leaves an active Goal after terminal Workflow completion must not start Goal continuation from that completion boundary. The Goal is active and idle until later explicit human input starts ordinary work.
- With an Active Session Runtime, Goal mutations must persist immediately and atomically in Goal admission order. Their model-facing notices must enter the Engine Intent Queue in the same order and wait for the next Step Boundary.
- Without an Active Session Runtime, the dormant Goal owner persists the durable Goal update and corresponding model-facing notice without starting a Runtime.
- Goal mutation requests from every client must return the persisted Goal result without waiting for a Step Boundary or model-visible reminder delivery. An allowed agent `goal set` or confirmed `goal complete` must validate the current Exact Execution Scope, Run, and Agent Step and persist the mutation within that admission.
- If the originating Agent Step ends first, the agent Goal mutation must reject without changing the Goal. If admission completes first, it must return the committed Goal, including its identity and timestamps. Terminal Workflow completion earlier in that open Step must not by itself reject or revoke the Goal admission.
- After terminal Workflow completion commits, exact Goal admission is available only for that same open Scope, Run, and Agent Step. Stop, prompt answers, exact steer, and duplicate Workflow completion are unavailable while caused tool/results finish.
- Goal validation must use the durable Goal, including every earlier committed Goal mutation, rather than a scheduled projection.
- A stale agent execution must reject before persistence. While the current Runtime is available, a later notice-delivery failure must appear as Runtime/model-visible failure feedback. Kent must not undo the committed Goal, rewrite the completed shell result, or replay the mutation.
- Graceful Core shutdown gives pending Goal notices and Goal failure feedback no Goal-specific handling or delivery guarantee. Shutdown must not wait for a final attempt or add Goal-specific validation, error handling, logging, atomicity, persistence, retry, replay, or fallback for that work.
- An effective set/replace, pause, resume/reopen, complete, or clear mutation persists one model-facing goal notice in both live and dormant sessions. A no-op mutation persists no notice.
- On the dormant path, the durable goal update is authoritative and is persisted before its model-facing notice. If the later notice persistence fails, the goal update is committed and the error is surfaced. No cross-resource rollback, automatic repair, or retry is required.
- Each Goal mutation invocation is a new operation. A repeated invocation follows the current Goal state and the ordinary validation and no-op rules. Kent must not replay an earlier response or error.
- Goal CLI never mutates Session storage directly. It submits the Goal operation to the server, which selects the live Steering path or dormant Store path.
- Any `kent service` commands that affect the server state (restart, stop, start it) detect invocation by kent itself and refuse to run, being human-only.
- Ctrl+C during active goal work keeps persisted status `active` and creates runtime-local suspension only. The next user message auto-resumes the suspended goal loop after its turn completes, without requiring `/goal resume`. An explicit `/goal pause` is a hard pause. A user turn that is itself interrupted leaves the loop suspended.
- The goal status-line indicator in TUI shows the animated spinner only while a goal run is executing. When the goal is `active` but idle (e.g. after Ctrl+C), it shows the idle status dot.

## Reviewer

- Post-turn Reviewer exists behind config and defaults to `reviewer.frequency = "edits"`.
- Reviewer runs only after an eligible completed assistant answer.
- Kent captures Reviewer input from that answer boundary before later Session changes can alter it.
- The main answer commits and becomes visible without waiting for Reviewer.
- User input and ordinary main-model and tool work may continue while Reviewer runs.
- Only one Reviewer request may be active for a Session.
- Another eligible answer is skipped rather than queued while review is active.
- The Reviewer receives shorter tool output than the main agent and returns minimal JSON `{"suggestions":["..."]}`.
- Invalid Reviewer payloads are ignored non-fatally.
- A Reviewer generation failure creates one later Reviewer error row when the Runtime can receive it.
- Reviewer phase changes and completion create no transcript row.
- Nonempty suggestions create one later Reviewer feedback row and request one ordinary main-agent follow-up at the next available model boundary.
- A follow-up that returns a nonblank final answer reports the suggestions as applied.
- An explicitly blank silent follow-up reports that no changes were applied.
- `reviewer.verbose_output` controls only the TUI's initial feedback presentation.
- `reviewer.verbose_output` never controls whether feedback exists.
- If Kent cannot apply issued nonempty feedback, the feedback must be visible and the Session follows its ordinary Runtime failure behavior.
- Reviewer runs once without reviewing its own follow-up.
- Live Reviewer activity follows the phase contract in the Runtime Steering specification.
- Live Reviewer activity is not persisted or reconstructed from transcript history.
- Persistent Reviewer activity across later lifecycle boundaries, Runtime replacement, reconnect, or restart is outside this specification.

## Headless Mode And Shared Control

- Model-originated delegation is limited by parent-agent ancestry. A root is depth `0`. With the default limit `2`, root to subagent to sub-subagent is permitted and the next launch is rejected before creating a Session. The limit applies across roles and Workflow-agent delegation. Workflow Node Sessions start at depth `0`. Opening or continuing a Session is not a child launch.
- Ancestry checks inspect at most 30 parent-agent links. Missing older lineage means root. Permission, I/O, corruption, or unavailable storage rejects the launch. Repeated lineage is rejected in production and fails fast with diagnostics in debug mode. A stale or missing immediate caller Session is invalid caller context and reports guidance to use a live Kent shell.
- A blocked launch returns attempted depth and configured limit.
- The role catalog is visible regardless of depth policy. A role with `agent_callable=false` is hidden from model role context and cannot be launched as a Kent-session subagent, but humans can use it headlessly and Workflows can assign it. `fast` is outside the custom-role catalog and can use a faster first-party provider profile or user configuration.
- `[workflow] subagents` is TOML-only and defaults to `false`. Model-originated delegation to every role, including `default` and `fast`, must require effective `agent_callable=true`. Workflow-agent delegation must additionally require `[workflow] subagents = true` and effective `workflow_subagent=true`, without built-in role exceptions. `workflow_subagent=false` excludes a role only from Workflow-agent delegation, not human headless use or direct Workflow-node assignment.
- Subagent callability restrictions must apply only when creating a new Session. Continuing an existing Session through `kent run --session` or `kent run --continue` must be allowed regardless of `agent_callable`, `workflow_subagent`, or `[workflow] subagents`, including when the caller selects an Agent for that Session. Session locks and Agent selection validation must apply.
- For a new headless `kent run` with no explicit role selection, Kent must use the always-available `default` role when `--agent` is omitted. Kent must use that role when `--agent default` is explicit. `[subagents.default]` may override the role's permitted model, Thinking, tools, Skills, and description settings. Omitted role settings inherit the base configuration.
- New interactive TUI and Desktop Sessions without another role selection must use the base configuration. Interactive `--agent default` must select the base configuration. A direct Workflow Node assignment of `default` must use that role's settings regardless of delegation flags.
- A headless Session must persist its selected role identity. Resuming without a role selector must reapply the recorded role. Opening such a Session in TUI or Desktop must use that identity unchanged. If the recorded role is unavailable and the Session Contract is not locked, resuming must select the interactive base settings and clear the recorded role. When no role was recorded, a headless continuation must use the Session's existing settings unchanged. After the Session Contract is locked, resuming must not change the recorded role identity even if the role is unavailable. A later role selection must not replace it.
- A live internal access Approval, including an outside-workspace patch request, is an access-request Question for `kent question` and wait/watch presentation. Each authoritative ordered option carries its display label and typed allow-once, allow-session, or deny decision. A durable Workflow Transition Approval is a separate concept and is never a Question or wait/watch outcome.
- The [CLI Commands](cli-commands.md) specification owns the Question and Run wait/watch command contracts, including presentation, accepted flags, machine-readable output, and exit codes.
- All attached clients have equal full control of one active Session across transcript and status, steering and queued input, Question and Approval answers, and every other control.
- Valid human input is accepted while internal Runtime work is active and follows the Runtime Steering ordering contract.
- Clients report server-authoritative live activity and do not infer busy state locally.
- A client must consider a question or approval answered only after the server accepts the answer and returns or publishes the resolved shared state.
- A running Workflow Task is steerable from every attached client, including chat, queued input, Goal control, settings, compaction, worktree, and process controls. The model may not submit a structured final answer invalid for the current Node. Inability to reach active execution is a runtime-unavailable error.
- Worktree controls are available from every client. List and status are reads. Creation and deletion that do not switch the calling Session execute immediately.
- Entering or leaving a Worktree for an Active Session Runtime accepts one typed Pending Work item carrying the domain Worktree Operation identity.
- Acceptance returns the established acknowledgement without waiting for the next Step Boundary or transition completion.
- Entering or leaving a Worktree for a dormant Session is a direct Worktree operation.
- The Worktree owner later applies the target, Working Directory, tool environment, and reminder or failure.
- Each explicit Worktree transition is an independent domain operation. Kent must not return an earlier result for a matching retry, reject a different transition merely because another is pending, replay an ambiguous operation, or resume process-local pending transitions after restart.
- Worktree deletion follows the concrete multi-Session and process blockers in the Runtime Steering and Workflow specifications.
- Worktree deletion never joins Session mutation ordering.
- For a Session with an Active Session Runtime, Kent must acknowledge a scheduled rebind and apply it between Agent Steps before queued user work, regardless of the caller. A successful move must not disrupt the running agent or change queued input across Projects.
- Before acknowledging a Session rebind, Kent must validate the selected Workspace destination and reject an invalid move, including a Workflow Session move to another Project. Kent must validate the move again when it executes. Entering a Worktree in another Workspace must use the same admission rules. Worktree selector resolution must be deferred to the Step Boundary.
- When the active agent invokes rebind for its own Session, Kent must accept the move only for that Exact Execution Scope.
- Dormant Session rebinds must complete synchronously and must reject a Session with a running Session-owned background command.
- A live Session rebind must allow Session-owned background commands. Existing commands must continue in the directories where they started, and commands started after the move must use the new Working Directory.
- A Session rebind sets the Working Directory to the target Workspace root.
- A Session rebind either applies its Project, Workspace, artifact location, Working Directory, and successful reminder together or leaves the previous location unchanged.
- A successful Session rebind or its authoritative failure notice must be included in the next naturally occurring model step. Rebind must not start a model step.

## Provider Stream Completion

- OpenAI-compatible Responses streams accept `data: [DONE]` or EOF after a valid `response.completed` event. `response.failed`, `response.incomplete`, `error`, and caller cancellation are failures. EOF before a valid terminal event, including malformed framing that prevents reading one, is a provider-contract error and never a partial completion.
