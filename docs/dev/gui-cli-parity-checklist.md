# GUI CLI/TUI Feature Parity Checklist

Status: draft inventory for eventual desktop GUI replacement of Builder's terminal UI and command surface.

Date: 2026-05-16.

## Purpose

Inventory current user-facing CLI/TUI use cases so future GUI work can track feature parity. This checklist is broader than the workflow MVP. It includes interactive terminal workflows, slash commands, headless command use, service management, project/workspace binding, configuration, and docs-listed product flows.

This is not a screen design. It is a parity backlog.

Exhaustiveness claim: none. This checklist is audited against current docs and primary CLI/TUI user-facing code paths, but it is not a proof that every behavior is captured. Dynamic user-defined surfaces are represented by discovery rules and representative behavior.

Evidence and command inventories for this checklist live in `docs/dev/gui-cli-parity-evidence.md`.

## Canonical Capability Ownership

This checklist has two buckets for sequencing, but each capability should have one behavioral owner. Slash-command rows track command routing and aliases; behavior lives in the canonical feature sections.

- Goal behavior is owned by `Goals`.
- Background process behavior is owned by `Background Process Management`.
- Worktree behavior is owned by `Worktree Management`.
- Status behavior is owned by `Status, Diagnostics, And Observability`.
- Auth behavior is owned by `Launch, Help, And First-Run UX`.
- Server/service behavior is owned by `Server Connection And Lifecycle`.
- Project/workspace behavior is owned by `Project And Workspace Binding`.

## Interactive TUI/Desktop Flows

### Launch, Help, And First-Run UX

- [ ] GUI can replace `builder` interactive startup as the primary human frontend.
- [ ] GUI exposes equivalent help/discovery for major actions currently shown by TUI `F1`, `?`, and platform shortcut labels.
- [ ] GUI supports clear usage/error states for unsupported actions, invalid inputs, and missing prerequisites.
- [ ] GUI keeps power-user escape hatches visible when an operation still requires CLI during migration.
- [ ] GUI supports first-run setup when no usable config/auth exists.
- [ ] GUI supports OAuth/Codex subscription sign-in flow.
- [ ] GUI supports `OPENAI_API_KEY` auth preference and conflict resolution.
- [ ] GUI supports explicit no-auth/local-provider mode where server supports it.
- [ ] GUI can switch auth later, equivalent to `/login` and `/logout`.
- [ ] GUI shows auth success, auth failure, pending OAuth/device-code, and callback states.
- [ ] GUI supports onboarding choices for model, theme, context window, thinking level, verbosity, questions, supervisor, compaction, reviewer model, and reviewer thinking.
- [ ] GUI supports importing existing skills and slash-command directories from supported source tools.
- [ ] GUI supports selecting/enabling/disabling imported or generated skills during setup.
- [ ] GUI shows setup review summary before writing config.
- [ ] GUI persists config choices to same global/workspace config model as CLI.

### Session Selection And Lifecycle

- [ ] GUI can create a new session.
- [ ] GUI can resume existing sessions from a startup/session picker.
- [ ] GUI can show session metadata in picker: title, workspace/project, status, last activity, and availability.
- [ ] GUI can continue a specific session by ID, equivalent to `--session`/`--continue`.
- [ ] GUI can show current session ID, replacing `builder session-id` for human use.
- [ ] GUI can preserve prompt drafts across session picker and app restarts.
- [ ] GUI supports session title editing and reset, equivalent to `/name <title>` and empty `/name`.
- [ ] GUI supports parent/child session navigation, equivalent to `/back`.
- [ ] GUI supports fresh child sessions for built-in review/init flows.
- [ ] GUI handles active headless runs owning runtime while interactive view attaches read-only.
- [ ] GUI can recover/resubscribe to session streams after reconnect.
- [ ] GUI supports workspace-change prompts when opening a session from a different checkout.

### Main Chat And Input

- [ ] GUI supports prompt editing and submission.
- [ ] GUI supports multiline input.
- [ ] GUI supports queuing prompts while model is busy, equivalent to `Tab`/`Ctrl+Enter`.
- [ ] GUI supports prompt history navigation and resend.
- [ ] GUI supports slash-command autocomplete and exact-command queueing semantics.
- [ ] GUI supports ask-question responses, including multiple-choice and freeform answers.
- [ ] GUI supports shell-command submission from prompt input, equivalent to `$ <command>`, and shows output to model.
- [ ] GUI supports path/file reference picking and insertion.
- [ ] GUI supports clipboard screenshot/image/PDF paste into prompt, equivalent to `Ctrl+V`/`Ctrl+D`.
- [ ] GUI supports copy latest final answer to clipboard, equivalent to `/copy`.
- [ ] GUI exposes input busy/locked states and blocked-command reasons.
- [ ] GUI preserves queued input and prompt drafts across compaction where server supports it.

### Transcript, Rendering, And Navigation

- [ ] GUI supports ongoing/lean transcript mode.
- [ ] GUI supports detail transcript mode with full tool/model visibility.
- [ ] GUI can toggle transcript modes, equivalent to `Shift+Tab`/`Ctrl+T`.
- [ ] GUI preserves append-only transcript semantics for ongoing history.
- [ ] GUI supports large transcript paging without loading full `events.jsonl`.
- [ ] GUI renders markdown, code blocks, ANSI/style intent, diffs, patches, shell outputs, tool calls, reasoning/status, reviewer output, and errors.
- [ ] GUI surfaces cache warnings according to configured warning mode.
- [ ] GUI shows compaction/proactive handoff events and resulting continuation state.
- [ ] GUI supports native scrollback-like behavior or equivalent persistent scroll history.
- [ ] GUI supports status line/indicators for runtime, input mode, queue, active process, worktree, model, and blockers.

### Rollback, Editing History, And Forking

- [ ] GUI supports entering rollback/edit mode from idle empty input, equivalent to `Esc Esc`.
- [ ] GUI lets user select older user messages, including older transcript pages and messages before compaction boundaries.
- [ ] GUI lets user edit a previous message and fork session from that point.
- [ ] GUI clearly states file edits are not automatically rolled back.
- [ ] GUI supports cancel/back navigation from rollback mode.
- [ ] GUI can show parent/fork relationships between sessions.

### Runtime Control And Agent Interaction

- [ ] GUI can interrupt active run or exit, equivalent to `Ctrl+C` semantics.
- [ ] GUI can show runtime connection/lease/read-only ownership state.
- [ ] GUI can show tool execution progress and background shell notices.
- [ ] GUI can surface outside-workspace edit approval prompts.
- [ ] GUI can show and answer model questions from `ask_question`.
- [ ] GUI supports terminal bell/system notification equivalents for completed turns or attention-needed states.
- [ ] GUI supports model timeout/runtime error display and recovery actions.

### Slash Command Routing

- [ ] GUI supports `/exit`: exit/close current frontend safely.
- [ ] GUI supports `/new`: create new session.
- [ ] GUI supports `/resume`: return to session picker.
- [ ] GUI supports `/login` and `/logout`: open auth options without silently clearing credentials.
- [ ] GUI supports `/compact <instructions>`: manual context compaction with optional instructions.
- [ ] GUI supports `/name <title>`: set/reset session title.
- [ ] GUI supports `/thinking <low|medium|high|xhigh>`: set/show thinking level.
- [ ] GUI supports `/fast [on|off|status]`: toggle/show fast mode where provider supports it.
- [ ] GUI supports `/supervisor [on|off]`: toggle supervisor invocation.
- [ ] GUI supports `/autocompaction [on|off]`: toggle auto-compaction.
- [ ] GUI routes `/status` to the canonical status/diagnostics surface.
- [ ] GUI routes `/goal [show|pause|resume|clear|<objective>]` to canonical goal actions.
- [ ] GUI routes `/ps [kill|inline|logs] <id>` to canonical background-process actions.
- [ ] GUI routes `/wt` and `/worktree` to canonical worktree management.
- [ ] GUI supports `/wt status`.
- [ ] GUI supports `/wt create` and `/wt new`.
- [ ] GUI supports `/wt switch <target>`.
- [ ] GUI supports `/wt delete [target]`, `/wt remove [target]`, and `/wt rm [target]`.
- [ ] GUI supports `/copy`: copy latest committed final answer.
- [ ] GUI supports `/back`: navigate to parent session.
- [ ] GUI supports `/review <what to review>`: start native code review in fresh child session.
- [ ] GUI supports `/init <instructions>`: run repository initialization prompt.
- [ ] GUI supports `/prompt:<name> <args>` custom file-backed prompt commands.
- [ ] GUI preserves slash-command busy-state gating, run-while-busy allowances, draft preservation, aliases, and queued-command behavior.

### Goals

- [ ] GUI can show active session goal.
- [ ] GUI can set a goal.
- [ ] GUI can pause a goal.
- [ ] GUI can resume a goal.
- [ ] GUI can clear a goal.
- [ ] GUI can complete a goal with confirmation where required.
- [ ] GUI can distinguish user vs agent goal mutations and preserve agent restrictions.
- [ ] GUI rejects agent-driven pause/resume/clear unless backend permits it.
- [ ] GUI requires explicit agent confirmation before agent-driven complete where backend requires it.
- [ ] GUI shows goal errors when live runtime is unavailable.
- [ ] GUI shows ask-question dependency errors for goal set/resume when `ask_question` is disabled.
- [ ] GUI can expose JSON-like/debug goal state if needed for parity with `builder goal show --json`.
- [ ] GUI can target goals by selected session outside active session context.

### Background Process Management

- [ ] GUI can list background shell processes for the current session/workspace.
- [ ] GUI can refresh process list.
- [ ] GUI can show process state, command, started time, running/finished status, and log location.
- [ ] GUI can kill a selected background process.
- [ ] GUI can inline a process output preview into prompt input.
- [ ] GUI can open process logs in system viewer/editor equivalent.
- [ ] GUI can navigate large process lists with selection/paging.
- [ ] GUI handles process-client unavailable states.

### Worktree Management

- [ ] GUI can list worktrees with status and dirty counts.
- [ ] GUI can create a managed worktree.
- [ ] GUI can suggest/sanitize branch names from session title or prompt context.
- [ ] GUI can require or choose non-empty base ref for new branch creation.
- [ ] GUI switches session into newly created worktree after create.
- [ ] GUI can switch directly to a worktree by unique selector: ID, canonical path, display name, branch name, or `main`.
- [ ] GUI can delete a worktree with confirmation.
- [ ] GUI blocks deleting main workspace worktree.
- [ ] GUI blocks deleting worktree used by another session or active background process.
- [ ] GUI moves active session back to main workspace if active worktree is deleted.
- [ ] GUI shows setup-script progress/result/warnings for worktree creation.
- [ ] GUI exposes worktree config: `base_dir` and `setup_script`.
- [ ] GUI can show setup-script payload/env values for debugging.

### Status, Diagnostics, And Observability

- [ ] GUI has detailed status page equivalent to `/status`.
- [ ] GUI shows config source and effective values.
- [ ] GUI shows model, configured model, thinking level, fast mode, supervisor, and auto-compaction state.
- [ ] GUI shows auth state, subscription/window state where available, and auth file path.
- [ ] GUI shows git/worktree/environment status.
- [ ] GUI shows session ID, session title, workspace root, persistence root, execution target, and whether frontend owns server.
- [ ] GUI shows skill inspection status.
- [ ] GUI shows update/version info.
- [ ] GUI refreshes slow status sections progressively and reports partial failures.
- [ ] GUI includes debug/transcript diagnostics mode where current TUI has it.

### Native/Desktop Equivalents

- [ ] GUI supports native clipboard for copy/paste and screenshots/images.
- [ ] GUI supports native notifications or attention indicators for completion/question/error.
- [ ] GUI supports app/window title updates currently represented by terminal title changes.
- [ ] GUI supports keyboard shortcuts for core TUI actions.
- [ ] GUI provides accessible alternatives for keyboard-only TUI flows.
- [ ] GUI supports theme selection equivalent to terminal light/dark/auto mode, adapted to desktop.

## Command, Service, Config, And Automation Flows

### Command Help, Version, And Distribution

- [ ] GUI shows app version equivalent to `builder --version`.
- [ ] GUI exposes command/action help equivalent to root CLI help for migration.
- [ ] GUI documents how desktop install/update flow replaces Homebrew/standalone CLI update habits where relevant.
- [ ] GUI can surface CLI-equivalent usage errors for operations that still map to command-shaped APIs.
- [ ] GUI has no direct product equivalent for root `--force-interactive`, but preserves the underlying intent through debug/migration paths when needed.

### Server Connection And Lifecycle

- [ ] GUI connects to Builder server as a thin client; server remains authoritative for sessions, tools, shells, projects, worktrees, auth, and persistence.
- [ ] GUI shows connected/disconnected/ready/unready server state.
- [ ] GUI can start or attach to manual `builder serve` equivalent when product direction allows it.
- [ ] GUI can manage background service lifecycle equivalent to `builder service status/install/uninstall/start/stop/restart`.
- [ ] GUI can display machine-readable service status fields currently available through `builder service status --json`.
- [ ] GUI supports install options equivalent to `--no-start` and `--force` where native service management remains in scope.
- [ ] GUI supports uninstall option equivalent to `--keep-running`.
- [ ] GUI supports restart option equivalent to `--if-installed`.
- [ ] GUI surfaces port conflicts, unmanaged server conflicts, and non-Builder listener conflicts.
- [ ] GUI blocks or warns on service restart when current session/work could be killed.
- [ ] GUI distinguishes local loopback server from remote/container server endpoints.

### Configuration And Customization

- [ ] GUI shows effective config with precedence: built-in defaults, global config, workspace config, env vars, and headless CLI overrides where relevant.
- [ ] GUI can inspect or edit core settings: model, provider override, OpenAI-compatible base URL, thinking level, verbosity, theme, web search, compaction, cache warning mode, timeouts, persistence root, server host, and server port.
- [ ] GUI can inspect or edit tool toggles: `ask_question`, shell, patch/edit, trigger handoff, view image, web search, write stdin.
- [ ] GUI explains patch/edit mutual exclusion and dynamic default selection.
- [ ] GUI can inspect or edit shell output settings, background shell output mode, command post-processing mode, and post-processing hook.
- [ ] GUI can inspect or edit supervisor/reviewer settings, model/provider overrides, auth policy, context window, timeout, verbose output, and system prompt file.
- [ ] GUI can inspect or edit model/provider capability overrides for custom models/providers.
- [ ] GUI can inspect or edit subagent roles, including description, model/provider/tool/skill overrides, priority mode, callable flag, context window, shell-output settings, and system prompt file.
- [ ] GUI can inspect or edit skills enablement.
- [ ] GUI can inspect prompt customization files and precedence: global/workspace `AGENTS.md`, global/workspace `SYSTEM.md`, config `system_prompt_file`, and subagent prompt overrides.
- [ ] GUI explains session snapshot behavior for prompt files and system prompts.
- [ ] GUI exposes or links to generated/user-editable `rg.conf` behavior.

### Project And Workspace Binding

- [ ] GUI can list known projects, equivalent to `builder project list`.
- [ ] GUI can create a project with server-visible path and display name, equivalent to `builder project create --path --name`.
- [ ] GUI can show project ID for current or selected path, equivalent to `builder project [path]`.
- [ ] GUI can attach another workspace path to an existing project, equivalent to `builder attach [path]`.
- [ ] GUI can attach using explicit project ID, equivalent to `builder attach --project <project-id> [path]`.
- [ ] GUI explains local vs remote path semantics: paths must be visible to the server process.
- [ ] GUI can repair moved workspace/session bindings, equivalent to `builder rebind <session-id> <new-path>`.
- [ ] GUI supports startup project binding flow when current workspace is unregistered.
- [ ] GUI prevents stale project/workspace state from leaking across project switches.

### Headless Runs And Automation

- [ ] GUI can start one-off unattended runs equivalent to `builder run <prompt>` if product keeps this as a GUI action.
- [ ] GUI can continue a previous headless session equivalent to `builder run --continue <session-id>`.
- [ ] GUI can select workspace for headless run equivalent to `--workspace`.
- [ ] GUI can select subagent role equivalent to `--agent <role>` and built-in `--fast`.
- [ ] GUI can override model, provider, thinking level, theme, model timeout, tools CSV, and OpenAI base URL where headless flags support it.
- [ ] GUI can set optional run timeout.
- [ ] GUI can choose output style equivalent to `--output-mode=final-text` or `--output-mode=json`.
- [ ] GUI can show progress equivalent to `--progress-mode=stderr` and quiet mode equivalent to `--progress-mode=quiet`.
- [ ] GUI displays run result, warnings, duration, session ID/name, continue ID, and continue command equivalent data.
- [ ] GUI shows structured error codes equivalent to JSON output: usage, timeout, interrupted, runtime.
- [ ] GUI explains headless limitation: no mid-run human questions and no tool preambles.
- [ ] GUI shows when session cannot continue headlessly because it has an active goal.
- [ ] GUI shows read-only watcher behavior when a headless run owns an active runtime.

### Subagents, Review, And Prompt Commands

- [ ] GUI can browse configured subagent roles and built-in `fast`.
- [ ] GUI can show role description, callable status, inherited/overridden config, tools, and skills.
- [ ] GUI can start explicit subagent/headless runs as a human.
- [ ] GUI can expose agent-callable role availability to the current session where relevant.
- [ ] GUI supports native code review flow equivalent to `/review`.
- [ ] GUI supports workspace initialization prompt equivalent to `/init`.
- [ ] GUI discovers custom prompt commands from workspace and global prompt/command directories.
- [ ] GUI shows custom prompt command name derived from filename and dedupe/precedence behavior.
- [ ] GUI supports `/prompt:<name>` execution with trailing arguments.
- [ ] GUI supports `$ARGUMENTS` replacement and fallback trailing-input append behavior.

### Prompt, Skill, And File Customization

- [ ] GUI can show global and workspace `AGENTS.md` files used as developer context.
- [ ] GUI can show global and workspace `SYSTEM.md` and configured system prompt files.
- [ ] GUI can show selected subagent system prompt file.
- [ ] GUI can explain prompt snapshot timing for sessions.
- [ ] GUI can show generated skill root and user/workspace skill roots.
- [ ] GUI can create/open user-editable prompt/skill directories.
- [ ] GUI can warn users not to edit generated skill files directly.

### Workflow And Task Commands

The workflow/task GUI inventory lives in `docs/dev/gui-workflow-use-cases.md`. This parity checklist tracks the CLI command surface that must remain represented by GUI screens/actions.

- [ ] GUI covers `builder workflow create`.
- [ ] GUI covers `builder workflow list`.
- [ ] GUI covers `builder workflow node add`.
- [ ] GUI covers `builder workflow edge add`.
- [ ] GUI covers `builder workflow link`.
- [ ] GUI covers `builder workflow unlink`.
- [ ] GUI covers `builder workflow default`.
- [ ] GUI covers `builder workflow validate`.
- [ ] GUI covers `builder workflow inspect`.
- [ ] GUI covers `builder task create`.
- [ ] GUI covers `builder task start`.
- [ ] GUI covers `builder task resume`.
- [ ] GUI covers `builder task approve`.
- [ ] GUI covers `builder task move`.
- [ ] GUI covers `builder task list`.
- [ ] GUI covers `builder task show`.
- [ ] GUI covers `builder task cancel`.
- [ ] GUI covers `builder task comment add/list/replace/delete`.
- [ ] GUI covers `builder task comment list --include-deleted`.
- [ ] GUI preserves exact ID/name resolution semantics and ambiguity handling where users need to debug references.

### Security, Sandboxing, And Remote Operation

- [ ] GUI clearly states Builder is unsandboxed by default and server environment is trust boundary.
- [ ] GUI exposes outside-workspace edit approval behavior and `allow_non_cwd_edits`.
- [ ] GUI supports connecting to Builder server running in VM/container/remote trust zone.
- [ ] GUI explains server-visible path semantics for remote/container projects.
- [ ] GUI can help configure or inspect server host/port for sandbox/remote use.
- [ ] GUI can surface sandbox image requirements or diagnostics when working with containerized servers.
- [ ] GUI warns against broad host-home mounts or unsafe credential exposure when assisting sandbox setup.

### Command Output Post-Processing

- [ ] GUI can show when shell output was post-processed.
- [ ] GUI can expose raw output or link to full output/log file when model/operator needs it.
- [ ] GUI can configure `shell.postprocessing_mode`.
- [ ] GUI can configure or test `shell.postprocess_hook`.
- [ ] GUI can surface hook failures, invalid JSON, timeout, or fallback warnings.
- [ ] GUI can show built-in output optimizations such as Go test collapse and file-read line counts.
