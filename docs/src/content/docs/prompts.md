---
title: Prompts
description: Prompt customization files, precedence, placeholders, and session snapshot behavior.
---

Builder reads prompt context from global and workspace files.

## Instruction Files

- `~/.builder/AGENTS.md` is a global instructions file injected into every session.
- `<workspace>/AGENTS.md` adds developer instructions that are specific to the current project.

Builder injects these files into the conversation as developer context once per session, not as the main system prompt.

## System Prompt

System prompt files replace Builder's built-in main system prompt for new sessions.

Priority, lowest to highest:

- Built-in system prompt
- `~/.builder/SYSTEM.md`
- `~/.builder/config.toml` `system_prompt_file`
- `<workspace-root>/.builder/SYSTEM.md`
- `<workspace-root>/.builder/config.toml` `system_prompt_file`
- Selected `[subagents.<role>]` `system_prompt_file`

`system_prompt_file` paths are resolved relative to the containing `config.toml` directory unless absolute.

Builder reads and renders the selected system prompt file once when the session sends its first model request, stores the fully rendered result in the `system_prompt` session metadata, and reuses that snapshot for later requests. After a session has stored that snapshot, editing prompt files affects new sessions only. Sessions without `system_prompt` capture the current prompt file on their next model request.

## Placeholders

System prompt files use Go template syntax with these fields:

- `{{.BuilderRunCommand}}` - command prefix for launching Builder subagents from shell, e.g. `path/to/builder.exe`
- `{{.EstimatedToolCallsForContext}}` - estimated function/tool-call budget before compaction/handoff, exact number that varies with model context window, like `185`.
- `{{.EditingToolName}}` - name of the tool the agent uses to modify files, like `edit` or `patch`. Varies per model.
- `{{.DefaultSystemPrompt}}` - full text of the built-in Builder system prompt.
- `{{.DefaultSystemPromptPersonality}}` - Builder agent identity, communication style, and engineering posture.
- `{{.DefaultSystemPromptHarnessWorkflowAutonomy}}` - harness behavior, environment constraints, workflow guidance, and autonomy rules.
- `{{.DefaultSystemPromptAmbiguityAndOutputQuality}}` - product ambiguity handling and implementation quality rules.
- `{{.DefaultSystemPromptFinalAnswerAndFormatting}}` - final response, Markdown, and formatting rules.
- `{{.DefaultSystemPromptDelegation}}` - subagent delegation guidance and examples.

`{{.DefaultSystemPrompt}}` is available to custom prompt files only. Builder's built-in prompt assembly uses section placeholders and rejects `{{.DefaultSystemPrompt}}` self-reference.

Example:

```md
{{.DefaultSystemPromptPersonality}}

{{.DefaultSystemPromptHarnessWorkflowAutonomy}}

# Team Rules

Prefer small, reviewable commits.
```

Tool preambles are appended after the rendered `SYSTEM.md` when `tool_preambles = true` for the locked session.

## Supervisor System Prompt

`reviewer.system_prompt_file` replaces Builder's built-in supervisor system prompt:

- `~/.builder/config.toml`
- `<workspace-root>/.builder/config.toml`

The workspace config value takes priority. Editing the file affects only sessions that have not run the supervisor with that override.
