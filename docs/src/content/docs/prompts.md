---
title: Prompts
description: Prompt customization files, precedence, placeholders, and session snapshot behavior.
---

Kent supports customizing system prompts, supervisor instructions, subagent system prompts, and repo guidance.

## Instruction Files

- `~/.kent/AGENTS.md` is a global instructions file injected into every session automatically.
- `<workspace>/AGENTS.md` adds developer instructions that are specific to the current project.

## System Prompt

System prompt files replace Kent's built-in default "product engineer" / SWE-focused system prompt. Priority, lowest to highest:

- Built-in system prompt
- `~/.kent/SYSTEM.md`
- `~/.kent/config.toml` `system_prompt_file`
- `<workspace-root>/.kent/SYSTEM.md`
- `<workspace-root>/.kent/config.toml` `system_prompt_file`
- Selected `[subagents.<role>]` `system_prompt_file`

`system_prompt_file` paths are resolved relative to the containing `config.toml` directory unless absolute.

The system prompt is snapshot and cannot be changed after the session starts to prevent cache invalidation and misbehaving models. Start a new session for changes to take effect.

## Placeholders

You can assemble your own system prompt from building blocks provided by Kent. It's highly recommended to leave the  instructions about the harness (`HarnessWorkflowAutonomy`) intact.

System prompt files use Go template syntax with these fields:

- `{{.DefaultSystemPromptHarnessWorkflowAutonomy}}` - important guidelines on harness behavior, environment constraints, available tools.
- `{{.DefaultSystemPromptPersonality}}` - Kent agent identity, communication style, and engineering posture.
- `{{.DefaultSystemPromptAmbiguityAndOutputQuality}}` - opinionated product ambiguity handling and implementation quality rules.
- `{{.DefaultSystemPromptFinalAnswerAndFormatting}}` - final response, Markdown, and formatting rules suitable for TUI.
- `{{.DefaultSystemPromptDelegation}}` - subagent delegation guidance and examples.
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

The workspace config value takes priority. Editing the file affects only sessions that have not run the supervisor with that override yet.
