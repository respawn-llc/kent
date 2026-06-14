---
title: Quickstart
description: Install Kent, authenticate on first launch, tune the most useful settings, and learn the main session workflows.
---

## Install

### Homebrew (macOS/Linux)

```bash
brew tap respawn-llc/tap
brew install respawn-llc/tap/kent
```

### Standalone binaries via GitHub Releases

These versions are **not auto-updated**. Please keep them updated manually by re-running install scripts.

Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/respawn-llc/kent/main/scripts/install.sh | sh
```

Windows:

```powershell
irm https://raw.githubusercontent.com/respawn-llc/kent/main/scripts/install.ps1 | iex
```

Check the installed version with: `kent --version`

Kent is unsandboxed by default.
For container, VM, and remote-server isolation, see [Sandboxing](../sandboxing/).

## Optional: Install the Background Service

Run this if you want one shared Kent server to start at login:

```bash
kent service install
```

It uses about 50 MB of RAM, lets unlimited frontends stay lightweight by connecting to one local orchestrator, and makes background shells reliable when a terminal frontend exits.
See [Kent Server](../server/) for details and service management commands.

## First Authentication

Start Kent CLI with: `kent`

Supported auth options:

- OpenAI/Codex subscription OAuth via the startup sign-in picker.
- OpenAI API-key auth via `OPENAI_API_KEY`. If you prefer API-key auth, export `OPENAI_API_KEY` before launch and kent will use it with your permission.

You can switch later with `/login`.

:::note
Anthropic or Gemini subscriptions/models will not be supported until they allow third-party harnesses in their ToS.
:::

## Main Workflows

- Press `F1` to invoke help with hotkeys.
- Use `Enter` to steer the model, `Tab` to queue messages.
- Use `Shift+Tab` to toggle between detailed transcript mode and lean ongoing mode.
- Type `$ <command>` to execute a shell command and show its output to the model.
- Press `Esc` twice to enter Edit mode, which lets you go back in time, edit a previous message, and fork the session into a new one. Use `Up`/`Down` to walk through user messages; the picker loads older transcript pages at the edges, including messages before compaction boundaries. File edits are not rolled back.
- Use the `Up`/`Down` arrow keys to select and resend previous prompts.
- Press `Ctrl+V` or `Ctrl+D` to paste a clipboard screenshot into the prompt as an image file path.
- Use `/review` to start a code review. In a non-empty session, Kent opens that review in a fresh child session. After the review finishes, you can use `/back` to teleport to the original session.
- `/name <new-name>` will set your session name in the picker and terminal title.
- `/autocompaction` will toggle compaction, and `/compact` will trigger one. If autocompact is off, you can go above 100% context usage if model allows it. **Going above 100% will cost more and degrade model performance**.
- Run `/status` to get detailed info about the session.

For the full command reference, see [Slash Commands](../slash-commands/).

## Configuration

Kent reads settings from `~/.kent/config.toml` and will auto-create it through a UI flow on first start. The full reference is on the [Configuration](../config/) page.

## Skills and Slash Commands

On first launch, the setup wizard can optionally symlink existing skills and slash-command directories from `~/.claude`, `~/.codex`, or `~/.agents` into Kent's `~/.kent` layout.

Kent discovers skills from:

- `<workspace>/.kent/skills`
- `~/.kent/skills`

Kent also seeds preinstalled skills into `~/.kent/.generated/skills`. Do not edit `~/.kent/.generated`; copy a generated skill into a workspace or global skill root to customize it.

You can disable individual skills for new sessions in `~/.kent/config.toml`:

```toml
[skills]
apiresult = false
```
Changes take effect when you start a new session.

Kent discovers custom slash commands from Markdown files in:

- `<workspace>/.kent/prompts`
- `<workspace>/.kent/commands`
- `~/.kent/prompts`
- `~/.kent/commands`

## Supervisor

- Use `/supervisor` to toggle its invocation for the current session. Initial value is config's `reviewer.frequency`, and default is after code edits. Supervisor is a feature that will automatically review the edits made by the model. It increases costs by ~15% (if using the main model) but improves results.

By default supervisor uses the same model as the main one. That may be too much / too slow for you. [Configuration](../config/) page contains instructions on how to change supervisor model.
Running OSS models or smaller models like `gpt-5.4-mini` seems to give almost the same results while keeping costs low.
