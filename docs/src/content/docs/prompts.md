---
title: Prompts
description: Prompt customization files, precedence, placeholders, and session snapshot behavior.
---

Learn how to customize system prompts, supervisor instructions, subagent system prompts, workflow prompts, and repo guidance.

Models will follow the instructions by role: `system -> developer -> user`, from most authoritative to least authoritative. With kent, you can customize all three levels:

## Instruction Files

- `~/.kent/AGENTS.md` is a global instructions file injected into every session automatically.
- `<workspace>/AGENTS.md` adds developer instructions that are specific to the current project.

These files are `developer`-level instructions. Kent includes their full Markdown content beneath an H1 identifying the source file, without wrapping the content in a code block.

Instruction and skill reference paths use CWD-relative paths inside the Working Directory, `~/` paths elsewhere under home, and absolute paths otherwise. CWD itself remains absolute. Skill paths refer to the Working Directory when the Skills list was injected; worktree moves provide a separate CWD-change reminder.

## System Prompt

Kent selects one configured custom prompt through [configuration precedence](../config/#precedence): global, shared workspace, then Main Workspace private settings. A selected role's `system_prompt_file` replaces the inherited agent selection. Omission inherits; an explicitly empty path is invalid.

The selected file keeps its scope in this priority order, lowest to highest. Only one of the configured-file entries participates:

- Built-in system prompt
- `~/.kent/SYSTEM.md`
- `~/.kent/config.toml` `system_prompt_file`
- `<workspace-root>/.kent/SYSTEM.md`
- Shared workspace or Main Workspace private `system_prompt_file`
- Selected `[subagents.<role>]` `system_prompt_file`

Paths are resolved relative to the configuration file that supplies them unless absolute. Automatic global and workspace `SYSTEM.md` discovery is independent of the configured selection.

Kent snapshots the rendered system prompt when it creates the session contract. Edits take effect after successful compaction and the next model request, without rewriting locked history.

## Goal Continuation

After successful automatic, manual, or handoff compaction, a non-workflow session with an active goal resumes with the exact goal text and Kent's goal work and completion guidance. Paused, completed, cleared, and absent goals add no continuation guidance, and reopening a session without compaction does not add it.

## Placeholders

You can assemble your own system prompt from building blocks provided by Kent. It's highly recommended to leave the instructions about the harness (`HarnessWorkflowAutonomy`) intact.

System prompt files use Go template syntax with these fields:

- `{{.DefaultSystemPromptHarnessWorkflowAutonomy}}` - important guidelines on harness behavior, environment constraints, available tools.
- `{{.DefaultSystemPromptPersonality}}` - Kent agent identity, communication style, and engineering posture.
- `{{.DefaultSystemPromptAmbiguityAndOutputQuality}}` - opinionated product ambiguity handling and implementation quality rules.
- `{{.DefaultSystemPromptFinalAnswerAndFormatting}}` - final response, Markdown, and formatting rules suitable for TUI.
- `{{.DefaultSystemPrompt}}` - full text of the built-in Kent system prompt.
- `{{.LaunchCommand}}` - Kent executable command, e.g. `path/to/kent.exe`.
- `{{.EstimatedToolCallsForContext}}` - estimated function/tool-call budget before compaction/handoff, exact number that varies with model context window, like `185`.
- `{{.EditingToolName}}` - name of the tool the agent uses to modify files, like `edit` or `patch`. Varies per model.

Example:

```md
{{.DefaultSystemPromptPersonality}}

{{.DefaultSystemPromptHarnessWorkflowAutonomy}}

# Team Rules

Prefer small, reviewable commits.
```

Additionally, if `tool_preambles = true` in the [config](../config/), another block of text is appended instructing the model to talk to you while working.

## Supervisor System Prompt

`reviewer.system_prompt_file` replaces Kent's built-in supervisor system prompt:

- `~/.kent/config.toml`
- `<workspace-root>/.kent/config.toml`
- `<main-workspace-root>/.kent/config.local.toml`

The same property precedence selects one file, including any explicit nested Supervisor selection in the active role. Paths are relative to their supplying configuration file; omission inherits and an explicitly empty path is invalid. Kent snapshots the rendered Supervisor prompt independently when a Supervisor request is built; edits take effect for Supervisor requests after successful compaction.
