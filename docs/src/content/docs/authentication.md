---
title: Authentication and connections
description: Use subscriptions, API keys, and local models with Kent.
---

## Choose how to access models

A connection gives Kent access to a model provider. Keep separate connections for subscriptions, API accounts, and local model servers, then choose which one each agent uses.

- **OpenAI:** ChatGPT subscription or API key.
- **Grok:** subscription or xAI API key.
- **Generic:** Responses-compatible API key or no auth, including local servers such as omlx.

Run `kent` for first-time setup. Use `/login` to add a connection or update an existing sign-in or API-key reference. `/logout` opens the same connection manager and keeps saved credentials.

OpenAI subscription sign-in offers browser and device authorization. Browser sign-in also accepts pasted callback input. Grok subscription sign-in opens a device-authorization page automatically. Compare the code displayed by Kent with the browser, then approve access in the browser. Kent shows the code until authorization completes. Each named connection keeps its own credentials.

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

The OpenAI and Grok API-key choices use their official endpoints. Choose Generic to supply a different Responses endpoint.

## Grok models and subscription routes

Grok connections default to `grok-4.7` with `high` thinking. The supervisor inherits the primary model unless explicitly configured. Saved sessions and explicit role settings keep their selected models.

Kent's built-in catalog includes `grok-4.6` and `grok-4.7`. The subscription proxy uses a 256,000-token context setting for `grok-4.7`, with 500,000 available as the larger choice. Its `grok-4.6` context limit is unknown. Public API connections use 500,000 tokens for both models.

Both catalogued models offer `low`, `medium`, `high`, and `xhigh` thinking, vision, reasoning summaries, and native web search. Fast mode requests priority processing with the same model. Verbosity settings do not affect Grok requests.

Native compaction uses the selected connection's `/responses/compact` endpoint and keeps the provider's complete returned context for continuation. Compaction sends the session cache key, effective priority tier, and non-tool request settings. Subscription-proxy compaction and acceptance of these extra request fields are experimental pending live verification.

Custom thinking accepts manual values for any model. Kent rejects unsupported efforts for catalogued models when sending a request. For uncatalogued models, the provider validates the custom value. Across providers, Disable is available only for models whose supported efforts include `none`. Native web search is incompatible with manually entered `grok-4.5`.

Subscription setup creates `grok-cli-proxy`. Public API OAuth is an experimental, configuration-only alternative: set the connection's `protocol` to `grok-oauth-api`, then reopen or resume the session. Requests already running keep their original route. Switching routes requires an explicit configuration edit. Public API OAuth inference is not live-verified.

Expired or invalid sign-in requires `/login`. A subscription or spending-limit rejection requires resolving the account's entitlement or credits with xAI, rather than signing in again. A protocol-version rejection requires updating Kent.

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

[connections.grok]
protocol = "grok-cli-proxy"

[connections.xai]
protocol = "grok-api-key"
environment_variable = "XAI_API_KEY"

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

Grok protocols use fixed endpoints: `grok-cli-proxy` uses `https://cli-chat-proxy.grok.com/v1`, while `grok-oauth-api` and `grok-api-key` use `https://api.x.ai/v1`. Omit `endpoint` for these protocols. Only `grok-api-key` accepts and requires `environment_variable`.

## Updating an older configuration

Kent converts global `provider_override`, `openai_base_url`, and `provider_capabilities` settings into connections, including choices for roles and the supervisor. Sign in again for converted ChatGPT connections.

If startup identifies an API connection without an environment-variable reference, add a connection with `environment_variable` as shown above. Replace the old access settings with its `connection` name. Apply the same edits manually to shared workspace and private `config.local.toml` files when startup identifies them. Keep model, thinking, tools, and context settings in their existing scopes.
