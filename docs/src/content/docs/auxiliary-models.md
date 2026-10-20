---
title: Nano, Supervisor, and Decision models
description: Configure separate models for automatic session naming and code review.
---

## Usage

### Name sessions with nano

Configure nano to generate titles for unnamed sessions without changing the model used for coding. Kent generates a title after the first response and any tool work it starts have completed. Automatic naming leaves existing names unchanged.

Choose an existing [connection](../authentication/) and an explicit nano model:

```toml
[nano]
connection = "subscription"
model = "gpt-6-luna"
```

For a local model whose context window Kent cannot determine, also set `model_context_window` to its actual token limit.

Set `nano.features.session_naming = false` to disable automatic naming. Nano settings follow the normal [configuration precedence](../config/#precedence), so a worktree can select another model or disable naming.

### Review answers with the supervisor

The supervisor checks completed answers and supplies suggestions that the coding agent can apply in a follow-up. The answer is visible without waiting for review.

By default, the supervisor runs after answers that include successful file edits and uses the coding agent's model. Set `reviewer.model` to choose another model, or `reviewer.frequency = "all"` to include answers without file edits. Set the frequency to `"off"` to disable it. Use [`/supervisor`](../slash-commands/) to toggle it during a session.

```toml
[reviewer]
frequency = "edits"
model = "gpt-6-astra"
```

Set `reviewer.verbose_output = true` to show the full supervisor feedback in the terminal transcript.

## Configuration reference

### Nano

| Key                                                 | Type                   | Default                           | Usage                                                                                          |
| --------------------------------------------------- | ---------------------- | --------------------------------- | ---------------------------------------------------------------------------------------------- |
| `nano.connection`                                   | string or string array | required                          | Existing connection name, or equivalent connections to rotate between.                         |
| `nano.model`                                        | string                 | required                          | Model used for automatic naming.                                                               |
| `nano.thinking_level`                               | string                 | model default, otherwise `medium` | Reasoning effort supported by the selected model.                                              |
| `nano.model_verbosity`                              | string                 | provider default                  | `low`, `medium`, or `high`.                                                                    |
| `nano.model_context_window`                         | int                    | model context window              | Required if Kent cannot determine the window. Minimum `2500`.                                  |
| `nano.timeout_seconds`                              | int                    | `120`                             | Inference timeout, including retries, starting when capacity is available. Must be positive.   |
| `nano.concurrency`                                  | int                    | `5`                               | Simultaneous nano calls across workspaces. Set only in global configuration. Must be positive. |
| `nano.features.session_naming`                      | bool                   | `true`                            | Automatic naming when nano is configured.                                                      |
| `nano.model_capabilities.supports_reasoning_effort` | bool                   | model capability                  | Override reasoning-effort support for a custom model.                                          |
| `nano.model_capabilities.supports_vision_inputs`    | bool                   | model capability                  | Override image and PDF input support for a custom model.                                       |

### Supervisor

Supervisor settings use the `[reviewer]` table. Omitted model controls inherit the corresponding coding-agent settings.

| Key                                                     | Type            | Default                         | Environment variable                                         | Usage                                                                                            |
| ------------------------------------------------------- | --------------- | ------------------------------- | ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `reviewer.connection`                                   | string          | session's selected connection   |                                                              | One existing connection name.                                                                    |
| `reviewer.frequency`                                    | string          | `edits`                         | `KENT_REVIEWER_FREQUENCY`                                    | `off`, `all`, or `edits`.                                                                        |
| `reviewer.model`                                        | string          | inherits `model`                | `KENT_REVIEWER_MODEL`                                        | Model used for review.                                                                           |
| `reviewer.thinking_level`                               | string          | inherits `thinking_level`       | `KENT_REVIEWER_THINKING_LEVEL`                               | Reasoning effort supported by the selected model.                                                |
| `reviewer.model_verbosity`                              | string          | inherits `model_verbosity`      | `KENT_REVIEWER_MODEL_VERBOSITY`                              | `""`, `low`, `medium`, or `high`.                                                                |
| `reviewer.model_context_window`                         | int             | inherits `model_context_window` | `KENT_REVIEWER_MODEL_CONTEXT_WINDOW`                         | Explicit context-window override. Minimum `40000`.                                               |
| `reviewer.system_prompt_file`                           | optional string | inherits `system_prompt_file`   |                                                              | Custom prompt path relative to the configuration file that supplies it. Empty paths are invalid. |
| `reviewer.timeout_seconds`                              | int             | `120`                           | `KENT_REVIEWER_TIMEOUT_SECONDS`                              | HTTP timeout. Must be positive.                                                                  |
| `reviewer.verbose_output`                               | bool            | `false`                         | `KENT_REVIEWER_VERBOSE_OUTPUT`                               | Show full supervisor feedback in the terminal transcript.                                        |
| `reviewer.model_capabilities.supports_reasoning_effort` | bool            | inherits main model capability  | `KENT_REVIEWER_MODEL_CAPABILITIES_SUPPORTS_REASONING_EFFORT` | Override reasoning-effort support for a custom model.                                            |
| `reviewer.model_capabilities.supports_vision_inputs`    | bool            | inherits main model capability  | `KENT_REVIEWER_MODEL_CAPABILITIES_SUPPORTS_VISION_INPUTS`    | Override image and PDF input support for a custom model.                                         |
