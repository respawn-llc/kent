## Configuration

- Nano must be a specially configured inference role, not an Agent or subagent.
- Nano settings must use the `[nano]` configuration section.
- An absent nano section must leave nano unavailable.
- A configured nano role must require explicit `connection` and `model` settings. Nano must not inherit the main Agent's model or fall back to it.
- Nano must use existing Provider Connections for provider implementation, endpoint, and authentication. Connection selection and rotation follow [Provider Connections](provider-connections.md#connection-sets).
- Nano must support the applicable existing thinking-level, model-verbosity, and model-capability overrides.
- When nano thinking or verbosity is omitted, nano must use provider/model defaults. Nano must derive capabilities from existing provider/model facts and apply explicit nano overrides without inheriting main-Agent controls or capability overrides.
- GPT nano models must default to `medium` thinking when no override is configured. Provider dispatch must supply reasoning effort.
- When the model catalog has no thinking default, nano must use `medium`.
- Nano must derive its model context window when possible and allow an explicit `model_context_window` override. When neither derivation nor an override supplies the window, configuration validation must fail.
- The minimum nano model context window must be 2,500 tokens.
- Nano must expose `timeout_seconds`, defaulting to 120 seconds, and `concurrency`, defaulting to five simultaneous invocations.
- Nano connection, model, context, timeout, and feature settings must use the normal configuration layers. `nano.concurrency` must be global server configuration only and bound nano invocations across all workspaces.
- Explicit incomplete or invalid nano settings must fail configuration validation.
- Nano feature switches must use `[nano.features]`. `session_naming` must default to true when nano is configured. Setting it false must disable automatic Session naming.

## Invocation

- Each nano invocation must perform one model Step. Nano must not execute returned tool calls.
- Consumers must supply their task instructions and input and interpret the result.
- Nano must support the existing provider request and result features allowed by the selected connection's capabilities, including text, structured output, and tool selection.
- Consumers must receive the completed result without incremental UI streaming. Streaming provider transport is permitted.
- Nano must reject oversized input without truncating it. A consumer may prepare a bounded input for its own task.
- Nano must reuse the main Session retry policy for retryable provider errors, including overload and rate limits.
- Invocations must wait for an available concurrency slot. The configured timeout must start when inference begins and include inference retries, not queue waiting.
- The server must allow at most 100 waiting nano invocations. Excess invocations must return a typed busy failure. This limit must not add a configuration setting.
- Consumers must own their server-work lifetimes and failure presentation. Client connection lifecycle must not control server-owned nano work.
- Automatic Session naming follows [Core Runtime Tools](core-runtime-tools.md#automatic-session-naming).
