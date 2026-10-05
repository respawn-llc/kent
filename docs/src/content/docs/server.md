---
title: Kent server
description: Kent's local client-server architecture and background service management.
---

Kent runs all its work through a local server process. Frontends are clients: TUI, desktop app, headless runs, and other local integrations all need the server to be running. To start the server, run `kent serve`.

See [connection configuration](../config/#connecting-to-a-server) for address selection and session resume behavior.

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

See [API keys and background services](../authentication/#api-keys-and-background-services) for provider credentials and the server's `.env` file.

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
