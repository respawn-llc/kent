---
title: Authentication and connections
description: Use subscriptions, API keys, and local models with Kent.
---

Kent ships built-in multi-account with load balancing and sticky sessions. Supported providers:

- ChatGPT
- Any API-Key Responses-compatible
- Grok Build
- Local model servers
- GLM Coding plan

Anthropic and Gemini will not be supported as they forbid usage of third party harnesses.

- Each sign-in is called a connection.
- Use `/login` in TUI to add connections (sign in on Desktop is in development). Call it multiple time to populate account credentials.
- Edit the `config.toml` to assign connection rotation and usage of connection by your Agent Roles.
- You can specify a connection for Supervisor requests (rotation is not supported there as supervisor requests are bound to the Session)
- Kent will try to preserve cache by reusing the same connection for a given session. Kent rotates on new sessions only, so the usage will likely not be perfectly even if part of your sessions are token-hungry.

## API keys and background services

To securely authenticate via API keys in public repos and support remote control, your API keys are environment variables, so config knows only their names.

During setup, enter the **name** of an environment variable, such as `MY_PROVIDER_KEY`. Set its value in the Kent server's environment or in `~/.kent/.env`:

```dotenv
MY_PROVIDER_KEY=your-provider-key
```

When using a custom persistence root, place `.env` in that root. For a remote server, configure the key on the server machine.

A background service reads this file independently of your terminal shell. A variable already present in the server's launch environment takes precedence, including an empty value. Restart the server after changing either source.

The OpenAI and Grok API-key choices use their official endpoints. Choose Generic to supply a different Responses endpoint.

## Grok support

There are 2, both semi-official, modes of using your subscription in Grok: CLI Proxy and OAuth API. First emulates the official CLI, used by e.g. OpenClaw. OAuth API is the official path, but does not have a Free Plan.

## Configure connections and roles

Declare connections in the global `config.toml`. Workspace settings, agent roles, and the supervisor can select them by name:

```toml
connection = "codex-1"

[connections.codex-1]
protocol = "chatgpt-codex"

[connections.codex-2]
protocol = "chatgpt-codex"

[connections.api]
protocol = "responses"
endpoint = "https://api.openai.com/v1"
environment_variable = "MY_PROVIDER_KEY"

[connections.grok]
protocol = "grok-oauth-api"

[connections.xai]
protocol = "grok-api-key"
environment_variable = "XAI_API_KEY"

[connections.local]
protocol = "responses"
endpoint = "http://127.0.0.1:8000/v1"

[subagents.worker]
connection = ["codex-1", "codex-2"]
model = "gpt-6-astra"

[reviewer]
connection = "api"
```

:::tip

Kent won't replace your model just because a connector doesn't offer it, so it's your responsibility to declare a model and settings that are supported by all the connections you use when load-balancing.
i

:::

### Share work across equivalent connections

Set the default or a session-producing role's `connection` to an array of connection names:

```toml
connection = ["account-a", "account-b"]

[connections.account-a]
protocol = "chatgpt-codex"

[connections.account-b]
protocol = "chatgpt-codex"
```

New sessions alternate between the defined members in their global declaration order, regardless of array order. The same member set shares one rotation across roles and workspaces.

`reviewer.connection` is an exclusion - it only accepts one connection. When omitted, the supervisor uses the main session's connection.
