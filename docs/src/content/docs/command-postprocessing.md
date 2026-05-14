---
title: Bash Hooks
description: Configure Builder's shell command post-processing and ship your own hook.
---

## Overview

Builder can post-process shell command output before it is shown to the model.
It normalizes terminal output, reduces command noise, and adds useful execution context.
Builder keeps this separate from command execution itself:

- command runs normally
- Builder runs the configured post-processing chain
- successful commands omit the exit-code header; failed commands include `Exit code N, output:`
- The model can disable that when it needs the full output

## Config

Configure command post-processing under `[shell]` in `~/.builder/config.toml`:

```toml
[shell]
postprocessing_mode = "all" # none | builtin | user | all
postprocess_hook = "~/.builder/shell_postprocess_hook"
```

### `postprocessing_mode`

Allowed values:

- `none`: disable command post-processing
- `builtin`: run Builder's sanitizer and built-in processors
- `user`: run Builder's sanitizer, then your configured hook
- `all`: run Builder's sanitizer, built-ins, then your configured hook

`raw: true` on a shell tool call bypasses command post-processing for that call. `raw: true` and `postprocessing_mode = "none"` preserve ANSI/style sequences in command output, subject to JSON result encoding and output truncation.

## Built-ins

- Successful direct `go test ...` commands collapse to `PASS` when output has no benchmarks, coverage, or JSON data.
- Direct partial file reads add information about file size to the output, like `[Total line count: 186]`
- Built-in processors run as a chain. A processor can pass output to later processors or stop the built-in chain with final output.

## Hook Protocol

### Input

Builder sends JSON like:

```json
{
  "tool_name": "exec_command",
  "command": "go test ./...",
  "parsed_args": ["go", "test", "./..."],
  "command_name": "go",
  "workdir": "/abs/workdir",
  "original_output": "...sanitized command output...",
  "current_output": "...built-in processed output or original output...",
  "exit_code": 0,
  "backgrounded": false,
  "max_display_chars": 16000
}
```

Your hook receives both:

- `original_output`: sanitized command output before Builder semantic shaping
- `current_output`: current Builder output after built-ins, or the same as `original_output` if no built-in handled it

This lets your hook either add on top of Builder defaults or replace them completely.
In `all` mode, the hook runs after built-ins even when a built-in processor stops the built-in chain.

Hook **must** return JSON like:

```json
{
  "processed": true,
  "replaced_output": "...new output..."
}
```

Return `{"processed": false}` for no-op passthrough. If the hook is missing, times out, exits nonzero, or returns invalid JSON, Builder falls back to the current output and reports a warning.
