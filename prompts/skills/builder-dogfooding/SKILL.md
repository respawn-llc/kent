---
name: builder-dogfooding
description: How to use `builder` cli or change your behavior/config. Read to learn `builder` commands; to debug project/workspace errors; when user asks to change builder config/settings/behavior.
---

Builder is the harness you are running inside, but it's also a server that runs agentic loops, a TUI, and a CLI interface that humans see.

Source-of-truth for commands and public docs:

- Run `builder --help` and `builder <command> --help` for exact current CLI flags.
- Full docs index: `https://opensource.respawn.pro/builder/llms.txt`.

You can directly `curl -S` each of the docs pages' `.md` file to get its content. Avoid using web fetch tools on those.

## Projects And Workspace Bindings
Builder tracks projects and workspace roots so sessions can move across checkouts and remote/local server boundaries. If your subagent commands fail with errors about workspace binding or projects, simply attach a workspace folder where you want to run the subagent to the project where you are running:

```bash
$ builder attach <path/to/subagent/workspace>
```

More info in `--help`.

## Config Locations
Global config (applies to all projects) `~/.builder/config.toml` (`%USERPROFILE%\.builder\` on Windows), local config is at `<workspace-root>/.builder/config.toml`. Workspace root is usually your cwd. Config schema and full notes at `https://opensource.respawn.pro/builder/config.md`.

Most behavior changes affect only **new sessions** and only **after server restart**. Existing sessions will keep captured conversation logs and settings. After changing config, ask the user to restart the service `builder service restart`, restart the Builder GUI, and then start a new session, for changes to apply.

Important: do not make changes to your configuration that were not authorized or directly asked for by the user. If your environment is buggy/broken, ask the user for help instead of messing with your internals.

## Change Agent Behavior
Use prompt files for broad behavior changes, skills for reusable on-demand workflows, and subagent roles for specialized headless agents. Start by reading docs at `https://opensource.respawn.pro/builder/prompts.md`

Note that you shouldn't be rewriting main agent's system prompt: the output can be biased and low-quality. System prompts need to be crafted carefully and vary strongly per LLM model family and use-case. Either the user should supply an existing prompt they want to use, or use `{{.DefaultSystemPrompt}}` for sane defaults, and add additional instructions to it.

## Subagent roles
User may ask you to define new "subagent roles". Subagents are `builder run` commands you call. You can also use them for scripting of user's personal builder-based workflows. More info at `builder run --help` and `https://opensource.respawn.pro/builder/headless.md`.

## Worktrees
Builder manages worktrees you work in. You can customize the process of worktree creation by providing a setup script, use it to prepare a newly created worktree with files that a worktree checkout did not bring over, like `.env`, private credentials, encryption keys, symlinks to local docs or other files, install dependencies, etc. It's designed to go from "just did a git checkout" to "fully ready for development".
Read how to create the hook at `https://opensource.respawn.pro/builder/worktrees.md`.

## Shell Postprocess Hooks
Builder can post-process shell command output before you see it.
Hook shape, output, and config are at `https://opensource.respawn.pro/builder/command-postprocessing.md`

You can disable this feature with `raw=true` in your `exec_command` tool. This hook is intended to optimize, shrink, or log the commands that you run. For example, a user may want you to use a tool that makes outputs smaller. Builder also ships embedded optimizers (`builtin` mode toggle) out of the box.
