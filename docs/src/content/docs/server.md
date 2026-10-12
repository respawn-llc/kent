---
title: Kent server
description: Kent's local client-server architecture and background service management.
---

Kent runs all its work through a local server process. Frontends are clients: TUI, desktop app, headless runs, and other local integrations all need the server to be running. To start the server, run `kent serve`.

See [connection configuration](../config/#connecting-to-a-server) for address selection and session resume behavior.

The server owns all long-running work: sessions, projects, runtime orchestration, background shells, tool execution, tasks, workflows, and storage.

Why? Because this:

- Gives ability to fully isolate work on another machine, VM, or container. See [Sandboxing](../sandboxing/) for remote/container setup.
- Drastically reduces resource consumption
- Allows agents to work asynchronously during workflows.
- Allows spawning agents on schedule and periodically.
- Uses only about 25 MB of RAM while idle.

## Background service

The easiest way to use Kent locally without manually running the server is via the **system service**:

```bash
kent service status
kent service install
kent service restart
kent service stop
kent service start
kent service uninstall
```

Install the service once, then restart it after updates.

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

:::tip

You can run multiple manually launched servers with different storage paths. Managed service installation uses one registration regardless of storage path. All service commands accept `--persistence-root` and honor `KENT_PERSISTENCE_ROOT`. The root you install with is remembered, so pass the same root on `status`/`start`/`stop`/`restart`/`uninstall`.

:::
