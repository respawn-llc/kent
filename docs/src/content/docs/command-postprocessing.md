---
title: Bash hooks
description: Configure Kent's shell command post-processing and ship your own hook.
---

Kent post-processes shell command output before it is shown to the model to normalize output, reduce command noise, and add useful execution context. Post-processing implements hooks for tools like `rtk` while also giving the model control over when they are used.

## Config

Configure command post-processing under `[shell]` in `~/.kent/config.toml`:

```toml
[shell]
postprocessing_mode = "all" # none | builtin | user | all
postprocess_hook = "~/.kent/shell_postprocess_hook"
```

### `postprocessing_mode`

Allowed values:

- `none`: disable command post-processing.
- `builtin`: run Kent's output cleanup and built-in processing. This is the default.
- `user`: only run user-configured hooks when present.
- `all`: run Kent's output cleanup and built-in processing, then run the configured hook.

Kent's built-in hooks add useful context info about file size, line counts, git commands, compress escape symbols, limit maximum tool output and max line width to prevent overload, and clean up some test output.

## Custom hook config

For custom hooks, make the script follow this protocol:

### Input

Kent sends JSON like:

```json
{
  "tool_name": "exec_command",
  "command": "go test ./...",
  "parsed_args": ["go", "test", "./..."],
  "command_name": "go",
  "workdir": "/abs/workdir",
  "original_output": "...raw command output...",
  "current_output": "...sanitized and built-in processed output...",
  "exit_code": 0,
  "backgrounded": false,
  "max_display_chars": 16000
}
```

Your hook receives both:

- `original_output`: raw command output before sanitization or built-in processing
- `current_output`: command output after sanitization and enabled built-in processing

Hook **must** return JSON like:

```json
{
  "processed": true,
  "replaced_output": "...new output..."
}
```

Return `{"processed": false}` for no-op passthrough. If a configured hook executable is missing, times out, exits nonzero, or returns invalid JSON, Kent falls back to the current output and reports a warning.
