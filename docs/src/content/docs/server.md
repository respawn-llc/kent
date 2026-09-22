---
title: Kent server
description: Kent's local client-server architecture and background service management.
---

Kent runs all its work through a local server process. Frontends are clients: TUI, desktop app, headless runs, and other local integrations all need the server to be running. To start the server, run `kent serve`.

The server owns all long-running work: sessions, projects, runtime orchestration, background shells, tool execution, tasks, workflows, and storage.

While annoying at times, this:

- Gives ability to fully isolate work on another machine, VM, or container. See [Sandboxing](../sandboxing/) for remote/container setup.
- Drastically reduces resource consumption
- Allows agents to work asynchronously during workflows.
- Allows spawning agents on schedule and periodically.
- Uses only about 25 MB of RAM while idle.

## Background service

Install a system service to run `kent serve` at login:

```bash
kent service status
kent service install
kent service restart
kent service stop
kent service start
kent service uninstall
```

All service commands accept `--persistence-root` and honor `KENT_PERSISTENCE_ROOT`. The root you install with is remembered, so pass the same root on `status`/`start`/`stop`/`restart`/`uninstall` to target that instance.

## Provider environment

API-key [connections](../config/#provider-connections) reference variables in the server's launch environment or its persistence-root `.env` file. The default file is `~/.kent/.env`. A service uses that same file, independently of your shell's environment.

```dotenv
MY_PROVIDER_KEY=your-provider-key
```

Restrict the file to its owner with `chmod 600 ~/.kent/.env`. Kent accepts a missing or empty file. Unreadable files, malformed dotenv content, and permissions allowing other users to read the file prevent startup.

A variable present in the process environment overrides the file, even when its value is empty. Restart the server after changing either environment source. Keep secrets in the server environment and enter only the variable name during connection setup. Agent shell processes exclude referenced provider-key variables.

## Backends

| OS           | Service          |
| ------------ | ---------------- |
| macOS        | LaunchAgent      |
| Linux / WSL2 | `systemd --user` |
| Windows      | Windows Service  |

- On Windows, uninstalling the service stops the server.
- Linux headless machines may need lingering enabled so the server survives logout `loginctl enable-linger "$USER"`.

## Port conflicts

Service install/start commands refuse to change the service when Kent's configured server endpoint is already owned by a manual `kent serve` process or by a non-Kent listener.
If you started `kent serve` manually, stop that process before installing or starting the background service.

Running another server on a different configured port is fine. Kent only checks the endpoint resolved from `server_host` and `server_port`.
