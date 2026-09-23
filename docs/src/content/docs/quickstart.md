---
title: Quickstart
description: Install Kent, authenticate on first launch, tune the most useful settings, and learn the main session workflows.
---

## 1. Install Kent server and CLI

#### Homebrew (macOS Apple Silicon/Linux)

```bash
brew tap respawn-llc/tap
brew install respawn-llc/tap/kent
```

#### Arch Linux (AUR)

The [`kent-bin`](https://aur.archlinux.org/packages/kent-bin) AUR package is community-maintained.

```bash
yay -S kent-bin
```

#### Standalone binaries via GitHub Releases

These versions are **not auto-updated**. Please keep them updated manually by re-running install scripts.

Linux:

```bash
curl -fsSL https://kent.sh/install.sh | sh
```

Windows:

```powershell
irm https://kent.sh/install.ps1 | iex
```

Check the installed version with: `kent --version`

## 2. Optional: Install the background service

Run this if you want one shared Kent server to start at login:

```bash
kent service install
```

It uses 20 MB of RAM when idle, lets unlimited frontends stay lightweight by connecting to **one** orchestrator, makes spawning and controlling subagents and background shells reliable, and enables the use of the desktop app. See [Kent Server](../server/) for details and service management commands.

## 3. Install Kent Desktop

The desktop app lets you use Kent's [Workflows and Tasks](../workflows/) feature to build agentic loops and deterministic pipelines to **fully automate** processes and scale to 10s or 100s of agents.

![Kent Desktop showing a project kanban board with tasks grouped by workflow stage](/desktop/desktop-kanban.webp)

### Manual install

Download the installer for macOS Apple Silicon, Linux x86_64, or Windows x64 at [kent.sh/desktop](https://kent.sh/desktop), or install the macOS app via Homebrew:

```bash
brew install --cask respawn-llc/tap/kent-desktop
```

Homebrew installs update through `brew upgrade`, standalone installs self-update.

:::note
The desktop app, due to the asynchronous nature of workflows, needs a [server](../server/) to connect to.

:::

## First use

:::danger[Security Warning]
Kent grants the model **full access** to your computer, with unrestricted tool execution. **Using Kent is equivalent to running `claude --dangerously-skip-permissions` or `codex --yolo`.** Use [Sandboxing](../sandboxing/) to isolate its access.
:::

Start the terminal client with `kent`. First-run setup selects a theme and provider connection before offering default or custom model settings. The connection choices are ChatGPT subscription, API-key Responses-compatible, and auth-less Responses-compatible.

API-key setup asks for a variable name on the server machine. Configure its value using the [server environment](../server/#provider-environment).

Finish saves the first connection as the default. Choosing defaults finishes immediately after connection setup. Canceling setup or restarting the server before Finish discards unsaved settings and sign-in. Use [`/login` or `/logout`](../config/#provider-connections) to manage connections afterward.

:::note
Anthropic or Gemini subscriptions/models will not be supported until these companies allow third-party harnesses in their ToS.
:::

## Main terminal workflows

- Press `F1` to invoke the help menu.
- Use `Enter` to steer the model, `Tab` to queue messages. Slash commands can be queued too!
- Use `Shift+Tab` to toggle between detailed transcript mode and lean ongoing mode.
- Type `$ <command>` to execute a shell command and show its output to the model.
- Press `Esc` twice to enter Edit mode, which lets you go back in time, edit a previous message, and fork the session starting with it. Use `Up`/`Down` to walk through user messages. File edits are **not** rolled back.
- Use the `Up`/`Down` arrow keys to select and resend previous prompts.
- Press `Ctrl+V/D`, `Alt+V/D` to paste clipboard images or text.
- Use `/review` to start a code review. After the review finishes, you can use `/back` to teleport to the original session.
- `/name <new-name>` will set your session name in the picker and terminal title.
- `/autocompaction` will toggle compaction, and `/compact` will trigger one. If autocompact is off, you can go above 100% context usage if model allows it. **Going above 100% will cost more and degrade model performance**.
- Run `/status` to get detailed info about the session.

For the full command reference, see [Slash Commands](../slash-commands/).

## Configuration

Kent reads settings from `~/.kent/config.toml`. The full reference is on the [Configuration](../config/) page.

## Skills and slash commands

On first launch, the setup wizard can optionally import existing skills and slash-command directories from supported providers.

Kent discovers skills from:

- `<workspace>/.kent/skills`
- `~/.kent/skills`
- `<persistence-root>/.generated/skills`

You can disable skills for new sessions in `config.toml`:

```toml
[skills]
creating-skills = false
```

Changes take effect when a session starts or after compaction.

## Supervisor

- Use `/supervisor` to toggle its invocation for the current session. Supervisor is a feature that will automatically review the work done by the model. It increases costs by ~15% (if using the main model) but improves results. By default supervisor uses the same model as the main one. That may be too costly / too slow for you. [Configuration](../config/) page contains instructions on how to change supervisor model.

## Advanced

Once you're comfortable driving the traditional agentic CLI, consider upgrading to [workflows](../workflows/).
