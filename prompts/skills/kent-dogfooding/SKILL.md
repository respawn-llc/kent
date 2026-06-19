---
name: kent-dogfooding
description: How to use `kent` cli or change your behavior/config. Read to learn `kent` commands; to debug project/workspace errors; when user asks to change kent config/settings/behavior.
---

Kent is the harness you are running inside, but it's also a server that runs agentic loops, a TUI, and a CLI interface that humans see.

Source-of-truth for commands and public docs:

- Run `kent --help` and `kent <command> --help` for exact current CLI flags.
- Full docs index: `https://kent.sh/llms.txt`.

You can directly `curl -S` each of the docs pages with an `.md` postfix to get its content. Avoid using web fetch tools on those.

## Projects And Workspace Bindings
Kent tracks projects and workspace roots so sessions can move across checkouts and remote/local server boundaries. If your subagent commands fail with errors about workspace binding or projects, simply attach a workspace folder where you want to run the subagent to the project where you are running:

```bash
$ kent attach <path/to/subagent/workspace>
```

More info in `--help`.

## Config Locations
Global config (applies to all projects) `~/.kent/config.toml` (`%USERPROFILE%\.kent\` on Windows), local config is at `<workspace-root>/.kent/config.toml`. Workspace root is usually your cwd. Config schema and full notes at `https://kent.sh/config.md`. The database and session logs that kent uses are colocated with the config file. Session logs are `.json` files with a full history of events, split per-project. Careful: session logs are very long and can span gigabytes.

Most behavior changes you make affect only **new sessions** and only **after server restart**. Existing sessions will keep captured conversation logs and settings. After changing config, ask the user to restart the service `kent service restart`, restart the Kent GUI, and then start a new session, for changes to apply.

Important: do not make changes to your configuration that were not authorized or directly asked for by the user. If your environment is buggy/broken, ask the user for help instead of messing with your internals.

## Change Agent Behavior
Use prompt files for broad behavior changes, skills for reusable on-demand workflows, and subagent roles for specialized headless agents. Start by reading docs at `https://kent.sh/prompts.md`

Note that you shouldn't be rewriting main agent's system prompt: the output can be biased and low-quality. System prompts need to be crafted carefully and vary strongly per LLM model family and use-case. Either the user should supply an existing prompt they want to use, or use `{{.DefaultSystemPrompt}}` for sane defaults, and add additional instructions to it.

## Subagent roles
User may ask you to define new "subagents" or "agent roles". Subagents are `kent run` commands you call. You can also use them for scripting of user's kent-based workflows. More info at `kent run --help` and `https://kent.sh/headless.md`.

## Worktrees
Kent manages worktrees you work in. You can customize the process of worktree creation by providing a setup script, use it to prepare a newly created worktree with files that a worktree checkout did not bring over, like `.env`, private credentials, encryption keys, symlinks to local docs or other files, install dependencies, etc. It's designed to go from "just did a git checkout" to "fully ready for development".
Read how to create the hook at `https://kent.sh/worktrees.md`.

## Shell Postprocess Hooks
Kent can post-process shell command output before you see it.
Hook shape, output, and config are at `https://kent.sh/command-postprocessing.md`

You can disable this feature with `raw=true` in your `exec_command` tool. This hook is intended to optimize, shrink, or log the commands that you run. For example, a user may want you to use a tool that makes outputs smaller. Kent also ships embedded optimizers (`builtin` mode toggle) out of the box.
