---
title: Configuration
description: Settings locations, precedence, CLI and environment overrides, and the full Kent config reference.
---

## Precedence

Kent resolves settings in this order (ascending priority):

1. Built-in defaults
2. `~/.kent/config.toml`
3. `<workspace-root>/.kent/config.toml`
4. `<main-workspace-root>/.kent/config.local.toml`
5. Environment variables
6. CLI overrides

Each explicitly supplied property overrides the same property from earlier layers. Omitted properties inherit, including within named roles and nested tables. An explicit `false` overrides `true`.

The private `config.local.toml` is read from the main workspace when using worktrees. Shared `config.toml`, on the other hand, follows the operation's workspace or working-directory context.

Define provider connections in global configuration. Workspace and role settings select connections by ID.

Kent loads prompts, tools, and model IDs at session start and reloads them at compaction.

## Locations

### Persistence root

- Workspace settings live at: `<workspace-root>/.kent/config.toml`. Note that workspace root is not necessarily the same as where you might have started the TUI - it's where the agent will actually do the work.
- Private settings live at `<main-workspace-root>/.kent/config.local.toml`. Keep personal overrides uncommitted using your Git ignore configuration. Worktrees read this file from the main workspace.
- Global settings live at: `~/.kent/config.toml`, and this location (along with all other data storage) is overridable via `--persistence-root`. The flag also relocates the root's model-visible global context — global `AGENTS.md`, the global system-prompt file, global skills, and generated assets.
  Service installation records the selected root. The OS supports one Kent service, so install it with the root you want to manage.

## Provider connections

Each entry in `[connections.<id>]` defines provider access. IDs start with a lowercase ASCII letter and contain lowercase letters, digits, hyphens, or underscores. `connection` selects the default for new unroled sessions. Roles and the supervisor inherit it or select another ID through their own `connection` setting.

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

[reviewer]
connection = "api"
```

`chatgpt-codex` uses ChatGPT subscription sign-in. `responses` requires an HTTP or HTTPS endpoint. Its optional `environment_variable` names the API-key variable on the server machine. Omitting that setting selects auth-less access. A missing or empty referenced value produces an error when the connection is used. Setup saves the reference without checking credentials. See [Server environment](../server/#provider-environment) for service configuration.

Use `/login` or `/logout` to add connections, sign in again, edit API-key references, or change the global default. Both commands leave other connections' credentials intact. Re-authentication is available during execution. Already-sent requests finish with their original credentials.

With zero defined connections, terminal startup and session opens enter connection setup. Headless agent launches require a configured connection.

Changing the global default applies to new unroled sessions. Explicit role assignments and saved session connections keep their selected IDs. A workspace `connection` setting overrides the global default. If a saved connection is removed, resuming its session selects and records the current role or default connection.

### Existing provider configuration

Server startup converts global `provider_override`, `openai_base_url`, and `provider_capabilities` settings, including global roles and supervisor settings, into named connections. Conversion changes provider access settings. Other setting values are unchanged. Formatting and comments may be discarded. ChatGPT connections require sign-in again.

If API-backed settings lack an explicit environment reference, define a Responses connection with `environment_variable` and replace the old access settings with its `connection` reference. Startup identifies files that require edits. Workspace `config.toml` and private `config.local.toml` require these edits manually. Keep model, thinking, tools, and context settings in their existing scopes.

## Example

```toml
model = "gpt-6-astra"
connection = "subscription"
provider_identifier = "kent"
thinking_level = "medium" # low, medium, high, xhigh, max, ultra
model_verbosity = "low" # or "medium" / "high"
max_subagent_depth = 2 # 0 through 30; 0 blocks subagent creation completely
# system_prompt_file = "SYSTEM.md" # relative to this config.toml directory
theme = "auto" # or light / dark
web_search = "native"
compaction_mode = "local" # or "native" (if supported)
cache_warning_mode = "default" # cache invalidation warning visibility; or "verbose" / "off"
server_host = "127.0.0.1"
server_port = 53082

[timeouts]
model_request_seconds = 400

[tools]
shell = true
# Leave both patch/edit commented to use Kent's model-based default.
# patch = true
# edit = false
view_image = true
web_search = true
trigger_handoff = true # proactive compaction by the model

[shell]
postprocessing_mode = "all" # shell output token optimizations by Kent: none | builtin | user | all
# postprocess_hook = "~/.kent/shell_postprocess_hook" # custom processor, see docs

[hooks.client]
# see respective docs page
# lifecycle = ["python3", "/absolute/path/lifecycle_hook.py"]

[workflow]
completion_mode = "auto"
concurrency = 5 # max agents to run concurrently for workflows; Script Nodes / Chats do not use it
max_invalid_completion_attempts = 5
pre_compaction_tokens = 247380 # defaults to 70% of context_compaction_threshold_tokens
use_required_tool_calls = true # whether to force models to never stop until workflow is complete on the API level
subagents = false # disables all workflow-agent delegation

[skills]
"skill name" = true

[reviewer] # aka supervisor
frequency = "edits"
# model = "gpt-6-astra"
# model_verbosity = "low"
# connection = "local"
# model_context_window = 64000
timeout_seconds = 120
verbose_output = false # set true to show complete supervisor suggestions in ongoing transcript
# system_prompt_file = "~/.kent/reviewer_system_prompt.md"

# Headless default role; new interactive TUI and Desktop Sessions use the top-level settings.
[subagents.default]
model = "gpt-6-astra"
thinking_level = "low"
description = "Low-cost role for headless runs."
agent_callable = false # model agents cannot delegate to this role
workflow_subagent = false # Workflow agents cannot delegate to this role

[connections.subscription]
protocol = "chatgpt-codex"
```

### Workflow subagent delegation

`[workflow] subagents` defaults to `false`. Set it to `true` to let Workflow agents delegate to roles whose effective `agent_callable` and `workflow_subagent` values allow it. Direct workflow node assignment, including assignment to `default`, is independent of this setting.

`agent_callable` is optional role metadata and defaults to `true`. It controls whether model-originated child delegation may target the role, including the `default` role. Humans can launch the role with `kent run` regardless of this value.

`workflow_subagent` is optional role metadata and defaults to `true`. Every role, including `default` and `fast`, is callable by a Workflow agent only when its effective `agent_callable` and `workflow_subagent` values and `[workflow] subagents = true` all permit it.

## Thinking

Thinking selects the model's reasoning effort. Change it in Chat settings or with [`/thinking <level>`](/slash-commands/). Available levels depend on the model and provider.

A Session's Thinking override takes precedence over its Agent configuration and global `thinking_level`. Use terminal detail mode to inspect recorded Thinking updates on supported models.

## CLI overrides

| Flag                               | Overrides                        | Notes                        |
| ---------------------------------- | -------------------------------- | ---------------------------- |
| `kent run --model`                 | `model`                          |                              |
| `kent run --thinking-level`        | `thinking_level`                 |                              |
| `kent run --theme`                 | `theme`                          |                              |
| `kent run --model-timeout-seconds` | `timeouts.model_request_seconds` |                              |
| `kent run --tools`                 | entire tool set                  | CSV replacement, not a merge |

## Reference

### Core settings

| Key                                   | Type            | Default       | Env                                        | CLI                                | Description                                                                                                                                                                                                                                                                                                                     |
| ------------------------------------- | --------------- | ------------- | ------------------------------------------ | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `model`                               | string          | `gpt-6-astra` | `KENT_MODEL`                               | `kent run --model`                 | Model name. If provider inference from the model name is not enough, set `provider_override` too.                                                                                                                                                                                                                               |
| `max_subagent_depth`                  | int             | `2`           |                                            |                                    | Maximum depth for model-originated creation of new child agents. A root is depth `0`. Values must be from `0` through `30`, and `0` blocks all model-originated child creation. Kent uses the active global-then-workspace value for every launch attempt.                                                                      |
| `thinking_level`                      | string          | `medium`      | `KENT_THINKING_LEVEL`                      | `kent run --thinking-level`        | Provider-specific reasoning effort string.                                                                                                                                                                                                                                                                                      |
| `model_verbosity`                     | string          | `low`         |                                            |                                    | Text verbosity hint for supported models. Allowed: `""`, `low`, `medium`, `high`. Unsupported models ignore it.                                                                                                                                                                                                                 |
| `system_prompt_file`                  | optional string | unset         |                                            |                                    | Selected custom main prompt file. Relative paths resolve from the supplying configuration file. Higher layers replace the selection. Empty paths are invalid.                                                                                                                                                                   |
| `theme`                               | string          | `auto`        | `KENT_THEME`                               | `kent run --theme`                 | TUI theme. Allowed: `auto`, `light`, `dark`. `light` and `dark` force Kent's fixed palettes. `auto` or an omitted value falls back to terminal background detection.                                                                                                                                                            |
| `notification_method`                 | string          | `auto`        | `KENT_NOTIFICATION_METHOD`                 |                                    | Terminal notification backend. Allowed: `auto`, `osc9`, `bel`. `auto` chooses `osc9` on supported terminals and falls back to `bel`.                                                                                                                                                                                            |
| `tui_native_progress_bar`             | bool            | `true`        |                                            |                                    | Emits terminal-native indeterminate progress for eligible interactive TUI operations.                                                                                                                                                                                                                                           |
| `hooks.client.lifecycle`              | string array    | unset         |                                            |                                    | Global-only command and fixed arguments for [interactive TUI lifecycle hooks](../lifecycle-hooks/).                                                                                                                                                                                                                             |
| `tool_preambles`                      | bool            | `true`        | `KENT_TOOL_PREAMBLES`                      |                                    | Includes tool-usage preambles in the main system prompt for interactive runs. Headless `kent run` suppresses them.                                                                                                                                                                                                              |
| `priority_request_mode`               | bool            | `false`       |                                            |                                    | Enables fast-mode requests where the provider supports them.                                                                                                                                                                                                                                                                    |
| `debug`                               | bool            | `false`       | `KENT_DEBUG`                               |                                    | Enables global developer-oriented strictness and logging. Only use for development/debugging                                                                                                                                                                                                                                    |
| `server_host`                         | string          | `127.0.0.1`   | `KENT_SERVER_HOST`                         |                                    | TCP host for the server and its clients. A port conflict requires an explicit endpoint change.                                                                                                                                                                                                                                  |
| `server_port`                         | int             | `53082`       | `KENT_SERVER_PORT`                         |                                    | TCP port for the server and its clients. Clients attached to one persistence root must use the same port. An explicit TCP target takes precedence over a Unix socket.                                                                                                                                                           |
| `web_search`                          | string          | `native`      | `KENT_WEB_SEARCH`                          |                                    | Web search backend. Allowed: `off`, `native`.                                                                                                                                                                                                                                                                                   |
| `provider_identifier`                 | string          | `kent`        | `KENT_PROVIDER_IDENTIFIER`                 |                                    | Sets the `originator` header and the `<provider_identifier>/<Kent version>` User-Agent on OpenAI, ChatGPT Codex, and OpenAI-compatible model-provider requests. The value must be a non-empty HTTP product token, such as `kent`, `my-agent`, or `acme_codex`. A restarted server applies the active value to resumed sessions. |
| `store`                               | bool            | `false`       | `KENT_STORE`                               |                                    | Sets OpenAI Responses `store=true` for main model requests.                                                                                                                                                                                                                                                                     |
| `allow_non_cwd_edits`                 | bool            | `false`       | `KENT_ALLOW_NON_CWD_EDITS`                 |                                    | Allows native file edits outside the working directory. Operating-system temporary directories are always allowed. For tool isolation, use a sandbox.                                                                                                                                                                           |
| `model_context_window`                | int             | `372000`      | `KENT_MODEL_CONTEXT_WINDOW`                |                                    | Explicit context-window size used for compaction and token accounting. The minimum is `40000`.                                                                                                                                                                                                                                  |
| `context_compaction_threshold_tokens` | int             | `353400`      | `KENT_CONTEXT_COMPACTION_THRESHOLD_TOKENS` |                                    | Auto-compaction threshold. Must be `> 0`, `< model_context_window`, and at least `50%` of `model_context_window`. The default is derived from the default context window.                                                                                                                                                       |
| `pre_submit_compaction_lead_tokens`   | int             | `35000`       | `KENT_PRE_SUBMIT_COMPACTION_LEAD_TOKENS`   |                                    | Fixed pre-submit runway reserve before auto-compaction. Kent compacts before sending the next user prompt once (`context_compaction_threshold_tokens` - this threshold) is reached.                                                                                                                                             |
| `minimum_exec_to_bg_seconds`          | int             | `15`          | `KENT_MINIMUM_EXEC_TO_BG_SECONDS`          |                                    | Default floor for `exec_command` yield time before it moves to background and lets Kent manage it asynchronously. Must be `> 0`. Use if model frequently expects your commands to complete fast, they background, and force model to poll for them.                                                                             |
| `compaction_mode`                     | string          | `local`       | `KENT_COMPACTION_MODE`                     |                                    | Allowed: `native`, `local`, `none`. `native` prefers provider-native compaction and falls back to local compaction. `local` always uses local summary compaction. `none` disables auto-compaction and makes manual compaction fail.                                                                                             |
| `cache_warning_mode`                  | string          | `default`     | `KENT_CACHE_WARNING_MODE`                  |                                    | Prompt-cache warning policy. Allowed: `off`, `default`, `verbose`. `default` records confirmed prefix invalidations and reuse disappearance in detail mode. `verbose` surfaces the same warnings in ongoing mode. `off` disables them.                                                                                          |
| `shell_output_max_chars`              | int             | `16000`       | `KENT_SHELL_OUTPUT_MAX_CHARS`              |                                    | Output budget for shell tools and background-shell notices before they are truncated.                                                                                                                                                                                                                                           |
| `bg_shells_output`                    | string          | `default`     | `KENT_BG_SHELLS_OUTPUT`                    |                                    | Background-shell output mode (injection of shell outputs into model context). Allowed: `default`, `verbose`, `concise`. Verbose dumps all output into the main agent's model. Concise forces it to read output files. Default outputs truncated previews + gives a file path.                                                   |
| `shell.postprocessing_mode`           | string          | `builtin`     | `KENT_SHELL_POSTPROCESSING_MODE`           |                                    | Semantic post-processing mode for `exec_command`. Allowed: `none`, `builtin`, `user`, `all`. `builtin` enables Kent processors only.                                                                                                                                                                                            |
| `shell.postprocess_hook`              | optional string | unset         | `KENT_SHELL_POSTPROCESS_HOOK`              |                                    | Executable/script path for a single local command post-processing hook.                                                                                                                                                                                                                                                         |
| `prevent_sleep`                       | string          | `active`      | `KENT_PREVENT_SLEEP`                       |                                    | Prevent system sleep while Kent is running. Allowed: `always` (while the server process is live), `active` (while any agent is working, plus up to one minute of idle-confirmation grace), `never` (disabled). Affects system sleep only. Screensaver and display sleep follow OS settings.                                     |
| `timeouts.model_request_seconds`      | int             | `400`         | `KENT_TIMEOUTS_MODEL_REQUEST_SECONDS`      | `kent run --model-timeout-seconds` | Model request timeout. Most requests are streaming, this only binds stalled requests, so healthy streaming can run for longer than this value.                                                                                                                                                                                  |

### Workflow

| Key                                        | Type   | Default                                                      | Env                                             | Description                                                                                                                                                                                            |
| ------------------------------------------ | ------ | ------------------------------------------------------------ | ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `workflow.completion_mode`                 | string | `auto`                                                       | `KENT_WORKFLOW_COMPLETION_MODE`                 | Default completion mode for workflow agent nodes that inherit the global default. Allowed: `auto`, `structured_output`, `tool`, `shell_command`, `unstructured_output`.                                |
| `workflow.concurrency`                     | int    | `5`                                                          | `KENT_WORKFLOW_CONCURRENCY`                     | Agent Node scheduling capacity. Explicit workflow actions may exceed it. Script Nodes do not use it. Must be `> 0`.                                                                                    |
| `workflow.max_invalid_completion_attempts` | int    | `5`                                                          | `KENT_WORKFLOW_MAX_INVALID_COMPLETION_ATTEMPTS` | Number of invalid workflow completion attempts allowed before Kent interrupts the run. Must be `> 0`.                                                                                                  |
| `workflow.pre_compaction_tokens`           | int    | `70%` of `context_compaction_threshold_tokens`, rounded down |                                                 | Workflow Session pre-compaction threshold. Must be positive and no greater than `context_compaction_threshold_tokens`. See workflow documentation for more info.                                       |
| `workflow.use_required_tool_calls`         | bool   | `true`                                                       |                                                 | Uses provider-required tool selection for `tool` and `shell_command` workflow completion modes. Set to `false` to use automatic tool selection while preserving Kent's workflow completion validation. |
| `workflow.subagents`                       | bool   | `false`                                                      |                                                 | Allows workflow agents to delegate to eligible roles, including `default` and `fast`.                                                                                                                  |

### Supervisor

Configure the supervisor agent that oversees model changes ("reviewer" is the legacy name of the feature).

Supervisor reviews run asynchronously, so you can continue working after the main answer.

| Key                             | Type            | Default                         | Env                                  | Description                                                                                                                                                 |
| ------------------------------- | --------------- | ------------------------------- | ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `reviewer.frequency`            | string          | `edits`                         | `KENT_REVIEWER_FREQUENCY`            | Allowed: `off`, `all`, `edits`. `all` runs the reviewer after every completed assistant turn. `edits` runs it only after successful first-class file edits. |
| `reviewer.model`                | string          | inherits `model`                | `KENT_REVIEWER_MODEL`                | Separate model for the reviewer pass. If unset, Kent uses main `model`.                                                                                     |
| `reviewer.thinking_level`       | string          | inherits `thinking_level`       | `KENT_REVIEWER_THINKING_LEVEL`       |                                                                                                                                                             | Thinking level override for supervisor, provider/model-dependent. |
| `reviewer.model_verbosity`      | string          | inherits `model_verbosity`      | `KENT_REVIEWER_MODEL_VERBOSITY`      | Text verbosity hint for supported reviewer models. Allowed: `""`, `low`, `medium`, `high`.                                                                  |
| `reviewer.model_context_window` | int             | inherits `model_context_window` | `KENT_REVIEWER_MODEL_CONTEXT_WINDOW` | Explicit reviewer context-window size sent to the reviewer provider. The effective value must be at least `40000`.                                          |
| `reviewer.system_prompt_file`   | optional string | unset                           |                                      | Selected custom Supervisor prompt file, resolved relative to its supplying configuration file. Omission inherits the main setting. Empty paths are invalid. |
| `reviewer.timeout_seconds`      | int             | `120`                           | `KENT_REVIEWER_TIMEOUT_SECONDS`      | Reviewer HTTP timeout. Must be `> 0`.                                                                                                                       |
| `reviewer.verbose_output`       | bool            | `false`                         | `KENT_REVIEWER_VERBOSE_OUTPUT`       | Controls only whether the TUI shows you full Reviewer feedback.                                                                                             |

### Supervisor model capabilities

Use `reviewer.model_capabilities` for custom supervisor models. Provider capability overrides belong to the supervisor's selected connection.

| Key (inside `reviewer.model_capabilities.*`) | Type | Default                                                 | Env                                                          | Description                                                                         |
| -------------------------------------------- | ---- | ------------------------------------------------------- | ------------------------------------------------------------ | ----------------------------------------------------------------------------------- |
| `supports_reasoning_effort`                  | bool | inherits `model_capabilities.supports_reasoning_effort` | `KENT_REVIEWER_MODEL_CAPABILITIES_SUPPORTS_REASONING_EFFORT` | Override-marks the reviewer model as supporting reasoning effort / thinking levels. |
| `supports_vision_inputs`                     | bool | inherits `model_capabilities.supports_vision_inputs`    | `KENT_REVIEWER_MODEL_CAPABILITIES_SUPPORTS_VISION_INPUTS`    | Marks the reviewer model as supporting multimodal image and PDF inputs.             |

### Model capability overrides

For models outside Kent's catalog, declare reasoning and vision support through these settings. Native compaction depends on the selected connection.

| Key                                            | Type | Default                | Env                                                 | Description                                                                           |
| ---------------------------------------------- | ---- | ---------------------- | --------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `model_capabilities.supports_reasoning_effort` | bool | `false`                | `KENT_MODEL_CAPABILITIES_SUPPORTS_REASONING_EFFORT` | Override-marks the configured model as supporting reasoning effort / thinking levels. |
| `model_capabilities.supports_vision_inputs`    | bool | model/provider default | `KENT_MODEL_CAPABILITIES_SUPPORTS_VISION_INPUTS`    | Overrides support for multimodal image and PDF inputs.                                |

### Provider capability overrides

Set these fields inside `[connections.<id>.provider_capabilities]`. `provider_id` is required when declaring overrides.

| Key                                 | Type   | Default  | Description                                                                                                                                     |
| ----------------------------------- | ------ | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `provider_id`                       | string | required | Required whenever you set provider capability overrides.                                                                                        |
| `supports_responses_api`            | bool   | `false`  | Marks the provider as supporting the Responses API.                                                                                             |
| `supports_responses_compact`        | bool   | `false`  | Marks the provider as supporting server-side compaction.                                                                                        |
| `supports_native_web_search`        | bool   | `false`  | Marks the provider as supporting native web search.                                                                                             |
| `supports_reasoning_encrypted`      | bool   | `false`  | Marks the provider as supporting encrypted reasoning items.                                                                                     |
| `supports_server_side_context_edit` | bool   | `false`  | Marks the provider as supporting server-side context editing.                                                                                   |
| `supports_provider_verbosity`       | bool   | `false`  | Controls Responses `text.verbosity` for unknown models. Known models use catalog facts.                                                         |
| `is_openai_first_party`             | bool   | `false`  | Marks the provider as first-party OpenAI semantics, which gates some Responses-specific behavior such as fast mode and phase protocol features. |

### Tools

`[tools]` is a per-tool boolean table in `config.toml`.
File-based tool toggles merge with defaults. `KENT_TOOLS` and `kent run --tools` behave differently: they replace the entire tool set with the CSV you provide.

| Key                     | Default            | What enabling it exposes                                                                                     |
| ----------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------ |
| `tools.ask_question`    | task-dependent     | Tool to ask humans interactive questions. Mandatory for workflows.                                           |
| `tools.shell`           | `true`             | The primary shell tool.                                                                                      |
| `tools.patch`           | model-dependent    | Freeform patch grammar edit tool for models trained on it (like the GPT family)                              |
| `tools.edit`            | model-dependent    | JSON text replacement/create/delete edit tool. Intended for models that are not trained to use patch syntax. |
| `tools.trigger_handoff` | `true`             | Tool agents can use to proactively compact their own context.                                                |
| `tools.view_image`      | model-dependent    | Ability to view PNG, JPEG, single-frame WebP and GIF, and PDF files (if supported)                           |
| `tools.web_search`      | provider-dependent | Tool to search the web                                                                                       |
| `tools.write_stdin`     | `true`             | Interaction with background shells.                                                                          |

Notes:

- Native web search requires `tools.web_search = true`, `web_search = "native"`, and provider support.
- `tools.patch` and `tools.edit` are mutually exclusive. If both are left at their defaults, Kent chooses `patch` for models that are trained on freeform patch syntax, otherwise `edit`. To force `edit`, set `edit = true` and `patch = false`.

## Concurrent shells

Set `shell.max_concurrent` in the global configuration to limit running agent-tool shells across the server to prevent overload. The default is `100`.

## Ripgrep config

Kent also installs an optimized, editable ripgrep config at:

```text
~/.kent/rg.conf
```

Kent creates `rg.conf` in the config+data root when missing and exports it to shell tools via `RIPGREP_CONFIG_PATH` only when you have not already set `RIPGREP_CONFIG_PATH` yourself.

### Subagents

`[subagents.<role>]` is a table for headless role settings. `default` is always available and is selected for a new headless `kent run` when no other role is selected and `--agent` is omitted or set to `default`. `fast` is also built in, and other roles are user-defined.
Role tables inherit the base settings and override only keys set in that role.
Definitions of the same role merge across global, shared and private files.
Ordinary environment and CLI agent settings override the selected role's declarations.

More info on the [Subagents page](../headless/).

### Skills

`[skills]` is a file-only per-skill boolean table in `config.toml`. Disabled skills are hidden from models. Keys are matched case-insensitively.

```toml
[skills]
"<skill name>" = false

[subagents.worker.skills]
"<skill name>" = false
```

Notes:

- `[subagents.<role>.skills]` overlays per-skill toggles for that role.
- Use `"quoted names"` to refer to skill keys containing spaces.
