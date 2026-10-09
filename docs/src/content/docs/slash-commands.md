---
title: Slash commands
description: Built-in slash commands and file-backed custom prompts.
---

## Commands

| Command                                                                                 | Input                        | What it does                                                                                                                                                       |
| --------------------------------------------------------------------------------------- | ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `/exit`                                                                                 | none                         | Exit Kent. During active work, Kent detaches while the server runs independently.                                                                                  |
| `/new`                                                                                  | none                         | Start a new session without stopping active work in the current session.                                                                                           |
| `/resume`                                                                               | none                         | Open the session picker without stopping active work in the current session.                                                                                       |
| `/login`, `/logout`                                                                     | none                         | Manage [provider connections](/authentication/), including sign-in and environment references.                                                                     |
| `/compact <instructions>`                                                               | optional free-form text      | Compact the current context. Trailing text is passed through as **additional** compaction instructions. Active non-workflow goals continue in the resumed context. |
| `/name <title>`                                                                         | optional free-form text      | Set an existing session's title. Blank input clears it. Outer whitespace is trimmed.                                                                               |
| <code>/thinking &lt;low&#124;medium&#124;high&#124;xhigh&#124;max&#124;ultra&gt;</code> | optional single value        | Set [Thinking](/config/#thinking). Empty input shows the current level.                                                                                            |
| <code>/fast [on&#124;off&#124;status]</code>                                            | optional single value        | Toggle or inspect fast mode.                                                                                                                                       |
| <code>/supervisor [on&#124;off]</code>                                                  | optional single value        | Toggle supervisor invocation.                                                                                                                                      |
| <code>/autocompaction [on&#124;off]</code>                                              | optional single value        | Toggle auto-compaction.                                                                                                                                            |
| `/status`                                                                               | none                         | Inspect config, Git, runtime and model details. Config lists contributing global, shared and private files in precedence order, plus environment/CLI overrides.    |
| <code>/goal [pause&#124;resume&#124;clear&#124;&lt;objective&gt;]</code>                | optional action or objective | Set or manage the current session goal (ralph-loop). Empty input opens the goal page.                                                                              |
| <code>/ps [kill&#124;inline&#124;logs] &lt;id&gt;</code>                                | optional action + id         | Open the background-process picker, or manage a specific background shell.                                                                                         |
| <code>/wt</code>                                                                        | none                         | Open the Worktrees page.                                                                                                                                           |
| <code>/wt create</code>                                                                 | none                         | Create a worktree. New branches require a non-empty base ref.                                                                                                      |
| <code>/wt switch &lt;target&gt;</code>                                                  | required selector            | Schedule entry into a worktree by id, branch, display name, or path.                                                                                               |
| <code>/wt leave</code>                                                                  | none                         | Schedule a return to the main workspace.                                                                                                                           |
| <code>/wt delete [&lt;target&gt;]</code>                                                | optional selector            | Delete a worktree.                                                                                                                                                 |
| `/copy`                                                                                 | none                         | Copy the latest durable model final answer to the system clipboard.                                                                                                |
| `/back`                                                                                 | none                         | Return to the parent session, if present, with the child’s latest durable final answer prefilled.                                                                  |
| `/review <what to review>`                                                              | optional free-form text      | Trigger Kent's native code review. Uses the empty session or starts a fresh child session.                                                                         |
| `/init <instructions>`                                                                  | optional free-form text      | Run repository initialization. Uses the empty session or starts a fresh child session.                                                                             |
| `/prompt:<name>`                                                                        | optional trailing arguments  | Run a server-owned custom prompt command.                                                                                                                          |

## File-backed prompt commands

Custom prompt commands come from Markdown files on the connected server.

The effective roots, in descending precedence, are:

- `<workspace>/.kent/prompts`
- `<workspace>/.kent/commands`
- `<persistence-root>/prompts`
- `<persistence-root>/commands`
- `<persistence-root>/.generated/prompts`
- `<persistence-root>/.generated/commands`

If the exact `$ARGUMENTS` token appears in the body, Kent replaces every occurrence with trimmed trailing arguments. Otherwise, Kent appends non-empty trailing arguments after one blank line.
