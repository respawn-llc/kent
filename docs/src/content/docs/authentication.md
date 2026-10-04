---
title: Authentication and connections
description: Use subscriptions, API keys, and local models with Kent.
---

## Choose how to access models

A connection gives Kent access to a model provider. Keep separate connections for your ChatGPT subscription, an API account, or a local model server, then choose which one each agent uses.

- **ChatGPT subscription:** sign in with your ChatGPT account.
- **API key:** use OpenAI or another provider that supports the OpenAI Responses API.
- **Local models:** connect to a Responses-compatible server such as omlx, with auth-less access when the server permits it.

Run `kent` for first-time setup. Use `/login` to add a connection or update an existing sign-in or API-key reference. `/logout` opens the same connection manager and keeps saved credentials.

## Subscription usage

Connections let you decide which work uses your subscription allowance. For example, use a subscription connection for interactive work and a local or API connection for frequently used subagents or the supervisor.

Choose another connection when you want to save subscription usage for later. Changing the global default affects new sessions. Existing sessions keep their connection, and role or workspace choices take precedence over the global default. To change an existing unlocked session's connection, select an agent configured for that connection in Chat settings.

Switching the default leaves requests already in progress on their original connection. Re-authenticating or editing an API-key reference takes effect for subsequent requests.

## API keys and background services

During setup, enter the **name** of an environment variable, such as `MY_PROVIDER_KEY`, instead of pasting the key. Set its value in the Kent server's environment or in `~/.kent/.env`:

```dotenv
MY_PROVIDER_KEY=your-provider-key
```

Protect that file with `chmod 600 ~/.kent/.env`. When using a custom persistence root, place `.env` in that root. For a remote server, configure the key on the server machine.

A background service reads this file independently of your terminal shell. A variable already present in the server's launch environment takes precedence, including an empty value. Restart the server after changing either source.

Kent can start without an `.env` file. If you create one, it must be readable by its owner, private to that owner, and valid dotenv. Referenced provider-key variables are excluded from agent shell processes.

## Configure connections and roles

Declare connections in the global `config.toml`. Workspace settings, agent roles, and the supervisor can select them by name:

```toml
connection = "subscription"

[connections.subscription]
protocol = "chatgpt-codex"

[connections.api]
protocol = "responses"
endpoint = "https://api.openai.com/v1"
environment_variable = "MY_PROVIDER_KEY"

[connections.local]
protocol = "responses"
endpoint = "http://127.0.0.1:8000/v1"

[subagents.worker]
connection = "local"
model = "your-local-model"

[reviewer]
connection = "api"
```

The API-key variable is optional for `responses` connections. Omit it for an auth-less endpoint. Choose a model that the selected provider offers. See [Configuration](../config/) for model settings and precedence.

## Updating an older configuration

Kent converts global `provider_override`, `openai_base_url`, and `provider_capabilities` settings into connections, including choices for roles and the supervisor. Other setting values are unchanged, but TOML comments and formatting may change. Sign in again for converted ChatGPT connections.

If startup identifies an API connection without an environment-variable reference, add a connection with `environment_variable` as shown above. Replace the old access settings with its `connection` name. Apply the same edits manually to shared workspace and private `config.local.toml` files when startup identifies them. Keep model, thinking, tools, and context settings in their existing scopes.
