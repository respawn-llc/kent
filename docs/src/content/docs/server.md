---
title: Builder Server
description: Builder's local client-server architecture and background service management.
---

Builder runs work through a local server process.
Frontends are clients: the terminal UI, headless runs, future apps, and other local integrations connect to the same server API instead of each owning separate orchestration state.

The server owns long-running work: sessions, project bindings, runtime orchestration, background shells, tool execution, and server-side storage under Builder's persistence root.
Keeping one shared server running lets frontends stay lightweight and reconnect without taking ownership of in-flight work.

The same boundary can isolate work on another machine, VM, or container. See [Sandboxing](../sandboxing/) for remote/container setup.

## Background Service

`builder service` installs and manages a supervised background `builder serve` process.
The service starts at login and keeps the local server available before any frontend opens.

```bash
builder service install
```

The background server uses about 50 MB of RAM while idle.
That cost buys one shared orchestrator for all Builder frontends and makes long-running background shells less dependent on the lifetime of a single terminal frontend.

## Commands

```bash
builder service status
builder service status --json
builder service install
builder service install --no-start
builder service install --force
builder service restart
builder service restart --if-installed
builder service stop
builder service start
builder service uninstall
builder service uninstall --keep-running
```

`install` starts the service after registration. `--no-start` only writes the service registration.
`uninstall` stops the service before removing registration. `--keep-running` removes registration without stopping an already-running process.
`restart` fails inside Builder shell commands, because stopping the service can halt active agent work. Ask the operator to restart it outside the session.

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

Service lifecycle commands refuse to change the service when Builder's configured server endpoint is already owned by a manual `builder serve` process or by a non-Builder listener.
If you started `builder serve` manually, stop that process before installing, starting, or restarting the background service.

Running another server on a different configured port is fine. Builder only checks the endpoint resolved from `server_host` and `server_port`.
