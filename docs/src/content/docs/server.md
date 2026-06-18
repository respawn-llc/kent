---
title: Kent Server
description: Kent's local client-server architecture and background service management.
---

Kent runs work through a local server process.
Frontends are clients: the terminal UI, headless runs, future apps, and other local integrations connect to the same server API instead of each owning separate orchestration state.

The server owns long-running work: sessions, project bindings, runtime orchestration, background shells, tool execution, and server-side storage under Kent's persistence root.
Keeping one shared server running lets frontends stay lightweight and reconnect without taking ownership of in-flight work.

The same boundary can isolate work on another machine, VM, or container. See [Sandboxing](../sandboxing/) for remote/container setup.

## Background Service

`kent service` installs and manages a supervised background `kent serve` process.
The service starts at login and keeps the local server available before any frontend opens.

```bash
kent service install
```

The background server uses about 50 MB of RAM while idle.
That cost buys one shared orchestrator for all Kent frontends and makes long-running background shells less dependent on the lifetime of a single terminal frontend.
When supported by the OS, shells that move to the background run at lower process priority so foreground agent work remains responsive under load.

## Commands

```bash
kent service status
kent service status --json
kent service install
kent service install --no-start
kent service install --force
kent service restart
kent service restart --if-installed
kent service stop
kent service start
kent service uninstall
kent service uninstall --keep-running
```

`install` starts the service after registration. `--no-start` only writes the service registration.
`uninstall` stops the service before removing registration. `--keep-running` removes registration without stopping an already-running process.

All service commands accept `--persistence-root` (and honor `KENT_PERSISTENCE_ROOT`). The root you install with is baked into the generated registration as `kent serve --persistence-root <root>`, so the supervised service uses the same config+data root rather than re-resolving `~/.kent` under whatever user the supervisor runs as. Use the same root on `status`/`start`/`stop`/`restart`/`uninstall` to target that instance.
On macOS, `restart` unloads the LaunchAgent, waits for the old server endpoint to stop responding, and bootstraps the LaunchAgent again.
Lifecycle commands that can stop or restart the service fail inside Kent shell commands, because they can halt active agent work. Ask the operator to manage the service outside the session.
`install --no-start` and `uninstall --keep-running` are allowed from Kent shell commands because they do not start or stop the service process.

## Backends

| OS | Supervisor |
| --- | --- |
| macOS | LaunchAgent |
| Linux / WSL2 | `systemd --user` |
| Windows | Scheduled Task at logon, with Startup folder fallback |

Linux headless machines may need lingering enabled so the server survives logout:

```bash
loginctl enable-linger "$USER"
```

## Port Conflicts

Service install/start commands refuse to change the service when Kent's configured server endpoint is already owned by a manual `kent serve` process or by a non-Kent listener.
If the service is installed but unloaded, `restart` can stop a healthy Kent listener on the configured endpoint and attach that endpoint back to the background service.
If you started `kent serve` manually, stop that process before installing or starting the background service.

Running another server on a different configured port is fine. Kent only checks the endpoint resolved from `server_host` and `server_port`.
