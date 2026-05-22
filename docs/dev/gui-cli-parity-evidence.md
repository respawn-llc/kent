# GUI CLI/TUI Parity Evidence

Status: manual evidence log for `docs/dev/gui-cli-parity-checklist.md`.

Date: 2026-05-16.

## Scope

This document records sources and inventories used to build the CLI/TUI parity checklist. It is not a proof of exhaustiveness. It is a manual audit trail for current docs and primary user-facing code paths.

## Dynamic/Filesystem-Driven Surfaces

Some Builder features are not exhaustively enumerable from repository code because users define them through local files or config. This evidence doc records discovery rules and representative behavior only.

- [x] Custom prompt commands are dynamic. Snapshot evidence covers discovery roots, normalization, precedence, and `$ARGUMENTS` behavior, not every possible command name.
- [x] Skills are dynamic. Snapshot evidence covers workspace/global/generated roots and config enablement, not every possible installed skill.
- [x] Subagent roles are dynamic. Snapshot evidence covers role config shape and command selection behavior, not every user-defined role.
- [x] Prompt files are dynamic. Snapshot evidence covers precedence and session snapshot behavior, not every user prompt file.

## Code Evidence Anchors

Line anchors are current as of this audit and intended to make verification faster. If source shifts, use the named functions/tests as stable search keys.

### Command Families

- [x] Service commands
  - Source anchors: `cli/builder/service_command.go:20-37` actions/options; `cli/builder/service_command.go:39-77` dispatcher; `cli/builder/service_command.go:79-167` subcommand flags; `cli/builder/service_command.go:169-243` action execution; `cli/builder/service_command.go:315-333` restart guard.
  - Help anchors: `cli/builder/help.go:447-535`.
  - Key tests: `TestServiceInstallNoStartAndForce`, `TestServiceUninstallKeepRunning`, `TestServiceRestartIfInstalledSkipsMissingService`, `TestServiceRestartIfInstalledRefreshesRegistrationBeforeRestart`, `TestServiceStatusJSON`, `TestServiceInstallRejectsUnmanagedRunningServer`, `TestServiceRestartRejectsBuilderShellSession`, `TestServiceRestartIfInstalledRejectsBuilderShellSession`, `TestServiceRestartHelpMentionsBuilderShellGuard`.
- [x] Goal commands
  - Source anchors: `cli/builder/goal_command.go:36-74` dispatcher; `cli/builder/goal_command.go:76-119` `show`; `cli/builder/goal_command.go:121-160` `set`; `cli/builder/goal_command.go:163-208` `pause/resume`; `cli/builder/goal_command.go:210-265` `complete`; `cli/builder/goal_command.go:271-308` `clear`; `cli/builder/goal_command.go:311-319` session/agent resolution.
  - Help anchors: `cli/builder/help.go:129-148`.
  - Key tests: `TestGoalShowUsesBuilderSessionID`, `TestGoalAgentEnvAllowsSetWithAgentActor`, `TestGoalAgentEnvSetOverwritePrintsDeniedPrompt`, `TestGoalAgentEnvDeniesNonSetMutationWithoutDialing`, `TestGoalAgentCompleteRequiresConfirmTripwire`, `TestGoalCompleteHelpExposesConfirmFlag`, `TestGoalCommandSubprocessTargetsLiveSessionFromUnboundWorktree`, `TestGoalCommandSubprocessSetPersistsWhilePrimaryRunActive`.
- [x] Workflow commands
  - Source anchors: `cli/builder/workflow_command.go:31-74` dispatcher; `cli/builder/workflow_command.go:76-108` create; `cli/builder/workflow_command.go:110-141` list; `cli/builder/workflow_command.go:162-211` node add; `cli/builder/workflow_command.go:232-307` edge add; `cli/builder/workflow_command.go:309-351` link; `cli/builder/workflow_command.go:353-388` unlink; `cli/builder/workflow_command.go:390-431` default; `cli/builder/workflow_command.go:433-476` validate; `cli/builder/workflow_command.go:479-526` inspect; `cli/builder/workflow_command.go:536-617` ID/name/project resolution.
  - Help anchors: `cli/builder/help.go:150-187`.
  - Key tests: `TestWorkflowAndTaskCommandsUseWorkflowAPI`, `TestWriteTaskDetailIncludesParallelBranchIDs`, `TestWorkflowCommandValidationErrorsAreActionable`, `TestWorkflowTaskCommandsDoNotAdvertiseJSONContract`, `TestWorkflowProjectPathResolutionRejectsUnboundPath`.
- [x] Task commands
  - Source anchors: `cli/builder/task_command.go:15-58` dispatcher; `cli/builder/task_command.go:60-106` create; `cli/builder/task_command.go:108-145` start; `cli/builder/task_command.go:147-177` list; `cli/builder/task_command.go:179-215` show; `cli/builder/task_command.go:218-255` cancel; `cli/builder/task_command.go:257-297` resume; `cli/builder/task_command.go:299-336` approve; `cli/builder/task_command.go:338-384` move; `cli/builder/task_command.go:416-590` comments; `cli/builder/task_command.go:592-621` board/task resolution; `cli/builder/task_command.go:623-642` detail output.
  - Help anchors: `cli/builder/help.go:189-267`.
  - Key tests: `TestWorkflowAndTaskCommandsUseWorkflowAPI`, `TestWriteTaskDetailIncludesParallelBranchIDs`, `TestWorkflowTaskCommandsDoNotAdvertiseJSONContract`.
- [x] Slash commands and custom prompts
  - Source anchors: `cli/app/commands/commands.go:83-171` built-in registry; `cli/app/commands/commands.go:147-150` `/worktree` and `/wt` alias; `cli/app/commands/file_prompts.go:26-42` registry with file prompts; `cli/app/commands/file_prompts.go:44-97` prompt discovery; `cli/app/commands/file_prompts.go:99-122` command ID normalization; `cli/app/commands/file_prompts.go:124-149` search dirs; `cli/app/commands/prompt_commands.go:14-33` prompt command registration; `cli/app/commands/prompt_commands.go:35-48` `$ARGUMENTS` replacement/append behavior.
  - Key tests: `TestExecuteBuiltins`, `TestHiddenAliasesDoNotAppearInVisibleCommandLists`, `TestLoadFilePromptCommandsPrecedence`, `TestLoadFilePromptCommandsPrecedenceAfterNormalizationCollision`, `TestLoadFilePromptCommandsSkipsEmptyHigherPriorityDuplicate`, `TestLoadFilePromptCommandsFiltersByExtensionAndDepth`, `TestLoadFilePromptCommandsFallsBackToGeneratedAfterGlobal`, `TestNewDefaultRegistryWithFilePromptsReplacesArgumentsPlaceholder`, `TestNormalizeFilePromptCommandID`, `TestBuildPromptSubmissionReplacesArgumentsPlaceholder`.
- [x] Status/process/worktree TUI surfaces
  - Source anchors: `cli/app/ui_status.go`, `cli/app/internal/status`, `cli/app/internal/statuscollect`, `cli/app/ui_processes.go`, `cli/app/ui_worktree_commands.go`, `cli/app/ui_worktree_*`.
  - Key tests: `TestStatusCommandOpensStatusSurfaceInNativeMode`, `TestStatusCommandProgressivelyLoadsSections`, `TestStatusCommandPersistsPromptHistoryWithoutBlockingOpen`, `TestProcessListEntryRenderTruncatesToWidth`, `TestProcessListOutputPreviewUsesLastNonEmptyLine`, `TestWorktreeCommandOpensOverlayAndRendersPage`, `TestWorktreeUsageIncludesAcceptedAliases`, `TestResolveWorktreeTokenFromEntriesUsesMatcherPrecedence`.

## Public Docs Mapping

- [x] `docs/src/content/docs/quickstart.md`
  - Covered install/update habits, background service recommendation, first auth, main TUI workflows, config entrypoint, skill/command import, and supervisor setup.
  - Mapped to parity sections: launch/help/first-run UX; main chat/input; transcript/rendering/navigation; rollback; slash commands; configuration/customization; subagents/review/prompt commands.
- [x] `docs/src/content/docs/headless.md`
  - Covered `builder run`, continuation, subagent roles, workspace binding, output modes, timeout/progress flags, active-goal restriction, and read-only watcher behavior.
  - Mapped to parity sections: headless runs and automation; subagents/review/prompt commands; project/workspace binding; session lifecycle.
- [x] `docs/src/content/docs/slash-commands.md`
  - Covered built-in commands, custom `/prompt:<name>`, autocomplete, command queueing, goal restrictions, and ask-question dependency notes.
  - Mapped to parity sections: slash commands; goals; background process management; worktree management; main chat/input.
- [x] `docs/src/content/docs/worktrees.md`
  - Covered worktree switch resolution, create/delete behavior, delete blockers, active-delete fallback, base dir, setup script, and setup payload/env.
  - Mapped to parity sections: worktree management; project/workspace binding; configuration/customization.
- [x] `docs/src/content/docs/config.md`
  - Covered config precedence, settings, env/CLI overrides, timeouts, supervisor, model/provider capabilities, tools, subagents, and skills.
  - Mapped to parity sections: configuration/customization; headless runs; subagents; status/diagnostics.
- [x] `docs/src/content/docs/prompts.md`
  - Covered instruction files, system prompt precedence, placeholders, session snapshot behavior, and supervisor prompt override.
  - Mapped to parity sections: prompt/skill/file customization; configuration/customization.
- [x] `docs/src/content/docs/server.md`
  - Covered server boundary, background service, commands/options, OS backends, and port conflicts.
  - Mapped to parity sections: server connection/lifecycle; security/sandboxing/remote operation.
- [x] `docs/src/content/docs/sandboxing.md`
  - Covered unsandboxed default, outside-workspace edit prompts, server trust boundary, remote/container setup, server-visible paths, and sandbox image shape.
  - Mapped to parity sections: security/sandboxing/remote operation; project/workspace binding.
- [x] `docs/src/content/docs/command-postprocessing.md`
  - Covered post-processing modes, raw output bypass, built-ins, hook input/output protocol, and hook failure fallback.
  - Mapped to parity sections: command output post-processing; transcript/rendering/navigation; configuration/customization.
- [x] `README.md`
  - Covered product-level inventory: TUI modes, tools, auth, prompt customization, sessions/projects/server, compaction/cache, worktrees, and shell post-processing.
  - Mapped to parity sections across both top-level buckets.

## CLI Command Inventory

Checked against `cli/builder/main.go` `rootCommand`, `runSubcommand`, `registerCommonFlags`, `registerSessionFlags`; `cli/builder/help.go`; and command-specific files.

### Root

- [x] `builder [flags]`
  - Use case: launch interactive TUI.
  - Flags: `--version`, `--force-interactive`, `--session`, `--continue`.
  - Evidence: `cli/builder/main.go`, `cli/builder/help.go`.
- [x] `builder --version`
  - Use case: print installed version.
  - Evidence: `cli/builder/main.go`.
- [x] `builder --force-interactive`
  - Use case: run interactive UI even when terminal checks fail; mostly migration/debug parity.
  - Evidence: `cli/builder/main.go`.

### Headless Run

- [x] `builder run [flags] <prompt>`
  - Use case: one unattended prompt run with final result.
  - Flags: `--workspace`, `--session`, `--continue`, `--model`, `--provider-override`, `--thinking-level`, `--theme`, `--model-timeout-seconds`, `--tools`, `--openai-base-url`, `--agent`, `--fast`, `--timeout`, `--output-mode`, `--progress-mode`.
  - Output modes: `final-text`, `json`.
  - Progress modes: `quiet`, `stderr`.
  - Error codes in JSON mode: `usage`, `timeout`, `interrupted`, `runtime`.
  - Evidence: `cli/builder/main.go`, `docs/src/content/docs/headless.md`.

### Server And Service

- [x] `builder serve [flags]`
  - Use case: run server in foreground until interrupted.
  - Evidence: `cli/builder/serve.go`, `cli/builder/help.go`.
- [x] `builder service status [--json]`
  - Use case: inspect background service state.
  - Evidence: `cli/builder/service_command.go`, `cli/builder/help.go`, `docs/src/content/docs/server.md`.
- [x] `builder service install [--force] [--no-start]`
  - Use case: install service registration and optionally start service.
  - Evidence: `cli/builder/service_command.go`, `cli/builder/help.go`, `docs/src/content/docs/server.md`.
- [x] `builder service uninstall [--keep-running]`
  - Use case: remove service registration and optionally leave running server alive.
  - Evidence: `cli/builder/service_command.go`, `cli/builder/help.go`, `docs/src/content/docs/server.md`.
- [x] `builder service start`
  - Use case: start installed service.
  - Evidence: `cli/builder/service_command.go`, `cli/builder/help.go`.
- [x] `builder service stop`
  - Use case: stop installed service.
  - Evidence: `cli/builder/service_command.go`, `cli/builder/help.go`.
- [x] `builder service restart [--if-installed]`
  - Use case: restart service, optionally no-op when not installed.
  - Edge behavior: restart is rejected from Builder-managed shell commands because it can kill current work.
  - Evidence: `cli/builder/service_command.go`, `cli/builder/help.go`, `docs/src/content/docs/server.md`.

### Session And Goals

- [x] `builder session-id`
  - Use case: print `BUILDER_SESSION_ID` from Builder-managed shell commands; fails outside them.
  - Evidence: `cli/builder/main.go`, `cli/builder/help.go`.
- [x] `builder goal show [--json] [--session <id>]`
  - Use case: inspect active session goal.
  - Evidence: `cli/builder/goal_command.go`, `cli/builder/help.go`.
- [x] `builder goal set [--session <id>] <objective>`
  - Use case: set goal. Builder shell commands may set a goal when no active or paused goal exists but cannot overwrite an active or paused goal.
  - Evidence: `cli/builder/goal_command.go`, `cli/builder/help.go`.
- [x] `builder goal pause [--session <id>]`
  - Use case: pause goal. Agent-origin command is denied.
  - Evidence: `cli/builder/goal_command.go`, `docs/src/content/docs/slash-commands.md`.
- [x] `builder goal resume [--session <id>]`
  - Use case: resume goal. Agent-origin command is denied.
  - Evidence: `cli/builder/goal_command.go`, `docs/src/content/docs/slash-commands.md`.
- [x] `builder goal clear [--session <id>]`
  - Use case: clear goal. Agent-origin command is denied.
  - Evidence: `cli/builder/goal_command.go`, `docs/src/content/docs/slash-commands.md`.
- [x] `builder goal complete [--session <id>] [--confirm]`
  - Use case: complete goal. Agent-origin completion requires explicit confirmation.
  - Evidence: `cli/builder/goal_command.go`, `cli/builder/help.go`.

### Project And Workspace Binding

- [x] `builder project [path]`
  - Use case: print project ID bound to path or current directory.
  - Evidence: `cli/builder/binding_commands.go`, `cli/builder/help.go`, `docs/src/content/docs/headless.md`.
- [x] `builder project list`
  - Use case: list project ID, display name, and root path.
  - Evidence: `cli/builder/binding_commands.go`, `cli/builder/help.go`.
- [x] `builder project create --path <server-path> --name <project-name>`
  - Use case: create project and first workspace binding.
  - Evidence: `cli/builder/binding_commands.go`, `cli/builder/help.go`.
- [x] `builder attach [path]`
  - Use case: attach another workspace to project from current directory.
  - Evidence: `cli/builder/binding_commands.go`, `cli/builder/help.go`, `docs/src/content/docs/headless.md`.
- [x] `builder attach --project <project-id> [path]`
  - Use case: attach workspace using explicit project ID.
  - Evidence: `cli/builder/binding_commands.go`, `cli/builder/help.go`.
- [x] `builder rebind <session-id> <new-path>`
  - Use case: retarget session to moved or alternate workspace root.
  - Evidence: `cli/builder/binding_commands.go`, `cli/builder/help.go`.

### Workflow

- [x] `builder workflow create [--description <text>] <name>`
- [x] `builder workflow list`
- [x] `builder workflow node add <workflow> --key <node-key> --kind start|agent|join|terminal [--display-name <name>] [--prompt <text>] [--agent <role>]`
- [x] `builder workflow edge add <workflow> --from <source-node-key> --transition <transition-id> --edge-key <edge-key> --to <target-node-key> --context <mode> [--requires-approval]`
- [x] `builder workflow link <project> <workflow> [--default]`
- [x] `builder workflow unlink <project> <workflow>`
- [x] `builder workflow default <project> <workflow>`
- [x] `builder workflow validate <workflow> [--mode draft|task_creation|execution] [--project <reserved>]`
- [x] `builder workflow inspect <workflow>`
- [x] Edge behavior checked: workflow references may be IDs or exact names; ambiguous names require IDs; node refs use node keys in CLI add path.
- [x] Evidence: `cli/builder/workflow_command.go`, `cli/builder/help.go`, `docs/dev/gui-workflow-use-cases.md`.

### Task

- [x] `builder task create --title <title> --body <body> [--workflow <workflow>] [--project <project>]`
- [x] `builder task start <short-id-or-task-id> [--project <project>]`
- [x] `builder task resume <short-id-or-task-id> [--project <project>]`
- [x] `builder task approve <transition-id>`
- [x] `builder task move <short-id-or-task-id> <target-node-id> [--project <project>] [--commentary <text>] [--output name=value]`
- [x] `builder task list [--project <project>]`
- [x] `builder task show <short-id-or-task-id> [--project <project>]`
- [x] `builder task cancel <short-id-or-task-id> [--project <project>] [--reason <text>]`
- [x] `builder task comment add <short-id-or-task-id> --body <text> [--author <kind>] [--project <project>]`
- [x] `builder task comment list <short-id-or-task-id> [--project <project>] [--include-deleted]`
- [x] `builder task comment replace <comment-id> --body <text>`
- [x] `builder task comment delete <comment-id>`
- [x] Edge behavior checked: short IDs resolve within selected/current project; task IDs starting `task-` resolve directly; ambiguous short IDs require task ID.
- [x] Evidence: `cli/builder/task_command.go`, `cli/builder/help.go`, `docs/dev/gui-workflow-use-cases.md`.

## Slash Command Inventory

Checked against `cli/app/commands/commands.go`, `cli/app/commands/file_prompts.go`, `cli/app/commands/prompt_commands.go`, and `docs/src/content/docs/slash-commands.md`.

- [x] `/exit`
  - Action: exit Builder.
- [x] `/new`
  - Action: create new session.
- [x] `/resume`
  - Action: return to startup/session picker.
- [x] `/login`
  - Action: open auth options.
- [x] `/logout`
  - Action: alias-like behavior for opening auth options.
- [x] `/compact <instructions>`
  - Action: compact current context with optional instructions.
- [x] `/name <title>`
  - Action: set title; empty title resets.
- [x] `/thinking <low|medium|high|xhigh>`
  - Action: set or show thinking level.
- [x] `/fast [on|off|status]`
  - Action: toggle/show fast mode; blocked when unavailable for provider.
- [x] `/supervisor [on|off]`
  - Action: toggle supervisor invocation.
- [x] `/autocompaction [on|off]`
  - Action: toggle auto-compaction.
- [x] `/status`
  - Action: open status overlay; allowed while busy and preserves prompt draft.
- [x] `/goal [show|pause|resume|clear|<objective>]`
  - Action: manage current session goal; restrictions captured in parity checklist.
- [x] `/ps [kill|inline|logs] <id>`
  - Action: list/manage background processes; blocked when process client unavailable.
- [x] `/worktree ...`
  - Action: canonical worktree command in code.
- [x] `/wt ...`
  - Action: alias for `/worktree`.
- [x] `/wt status`
- [x] `/wt create`
- [x] `/wt new`
- [x] `/wt switch <target>`
- [x] `/wt delete [target]`
- [x] `/wt remove [target]`
- [x] `/wt rm [target]`
- [x] `/copy`
  - Action: copy latest committed final answer.
- [x] `/back`
  - Action: jump to parent session; blocked when no parent.
- [x] `/review <what to review>`
  - Action: submit built-in review prompt in fresh child session.
- [x] `/init <instructions>`
  - Action: submit built-in init prompt in fresh child session.
- [x] `/prompt:<name> <args>`
  - Action: dynamic file-backed prompt command.
  - Sources: `<workspace>/.builder/prompts`, `<workspace>/.builder/commands`, `~/.builder/prompts`, `~/.builder/commands`, `~/.builder/.generated/prompts`, `~/.builder/.generated/commands`.
  - Edge behavior checked: only `.md`; blank files skipped; command ID normalized from filename; first match wins; `$ARGUMENTS` replaced otherwise trailing args appended.

## TUI Key/Input Behavior Inventory

Checked against `cli/app/ui_help.go`, `cli/app/ui_keymap.go`, `cli/app/ui_input_*`, and docs.

- [x] `F1`, `?` on empty input, and platform help shortcut toggle help.
- [x] `Ctrl+C` interrupts current run or exits.
- [x] `Shift+Tab` / `Ctrl+T` toggles transcript mode.
- [x] `Enter` submits input, selected answer, selected command, or next queued item depending on mode.
- [x] `Tab` / `Ctrl+Enter` autocompletes selected slash command or queues/sends input.
- [x] `Shift+Enter` / `Ctrl+J` inserts newline.
- [x] `Up` / `Down` navigates prompt history at input boundaries and moves selection in overlays.
- [x] `Ctrl+V` / `Ctrl+D` pastes clipboard screenshot as file path.
- [x] Shell-style editing keys are supported: delete current line, delete/yank text, word movement, home/end.
- [x] `Esc Esc` opens rollback selection from idle empty prompt.
- [x] `Esc` cancels/goes back in overlays.

## Status/Diagnostics Inventory

Checked against `cli/app/ui_status.go`, `cli/app/internal/status`, and `cli/app/internal/statuscollect`.

- [x] Base context: session ID/name, workspace root, persistence root, execution target, configured/effective model, thinking level, fast mode, reviewer, auto-compaction, owns-server.
- [x] Auth context: auth state, subscription/window info where available, auth state path.
- [x] Git context: repository/worktree status and errors.
- [x] Environment context: config source, settings, skills, update/version details where available.
- [x] Progressive refresh behavior: base data first, slower auth/git/environment stages later, partial errors surfaced.

## Background Process Inventory

Checked against `cli/app/ui_processes.go` and `docs/src/content/docs/slash-commands.md`.

- [x] Open/list process overlay.
- [x] Refresh process list.
- [x] Select first/last/previous/next/page.
- [x] Kill process.
- [x] Inline output preview into prompt input.
- [x] Open logs through default opener or editor fallback.
- [x] Show unavailable client state.

## Worktree UI Inventory

Checked against `cli/app/ui_worktree_commands.go`, worktree controllers, `cli/app/internal/worktreeview`, and `docs/src/content/docs/worktrees.md`.

- [x] Open worktree page/status.
- [x] Create/new worktree flow.
- [x] Direct switch by selector.
- [x] Delete/remove/rm flow with optional target.
- [x] Target resolution: worktree ID, canonical path, display name, branch name, `main`.
- [x] Delete blockers: main workspace, another session, active background process.
- [x] Active deleted worktree returns session to main workspace.
- [x] Setup script warning/progress payload surfaced.

## Cross-Checks Added To Parity Checklist

- [x] `builder --version`.
- [x] Root `--force-interactive`.
- [x] `/prompt:<name>` dynamic prompts.
- [x] `/worktree` canonical command and `/wt` alias.
- [x] `/wt new`, `/wt remove`, `/wt rm` aliases.
- [x] `/status` busy/draft behavior.
- [x] Goal agent restrictions and `--confirm` behavior.
- [x] `builder service restart --if-installed`.
- [x] `builder run --output-mode=json`.
- [x] `builder run --progress-mode=stderr`.
- [x] Read-only watcher behavior while headless run owns runtime.
