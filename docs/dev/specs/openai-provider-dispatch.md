## Dispatch Identity

- HTTP header requirements apply to HTTP requests and WebSocket opening handshakes. Individual WebSocket create events use the open connection and their current request metadata.
- Every OpenAI-family generation and compaction dispatch sends `originator`, `User-Agent`, and `session-id`, including a dispatch to an explicit custom OpenAI-compatible endpoint without authentication.
- `session-id` is the dispatch's Kent Session identity, except for nano's ephemeral invocation identity defined below. It must be nonempty, must have no leading or trailing space or horizontal tab, and must be valid as an outbound HTTP header value.
- Kent rejects a missing or invalid dispatch Session identity before authentication resolution, credential refresh, or any network request.
- Authenticated dispatches send `Authorization` and ChatGPT account-identity headers. An explicit custom OpenAI-compatible endpoint may send the common identity headers without authentication or `Authorization`.
- Kent sends no `session_id` header.
- Kent must not send separate provider requests to count input tokens.
- Model-context resolution and offline request inspection send no Session identity, Codex dispatch metadata, routing hint, or provider turn state.
- Every nano invocation must use one ephemeral UUID v4 as its provider dispatch identity, whether or not it has a parent Session. Nano must not create a Kent Session or persist this identity. The identity must be unchanged across retries of the invocation.

## Provider attempt diagnostics

- Each OpenAI-family generation and compaction attempt must emit an operator diagnostic when dispatch starts and another when the attempt returns. Each attempt must have its own identity, including retries.
- Diagnostics must identify the Session, Provider Connection, model, prompt-cache key, requested and served service tiers, and authenticated account identity when available.
- Outcomes must distinguish completed, failed, and canceled attempts. Diagnostics must include elapsed time, available HTTP status and provider request identity, and available provider-reported token usage, including usage reported by failed or incomplete responses.
- Diagnostics must represent unreported token usage as unknown. They must distinguish a provider-reported zero from absent usage.
- Attempt diagnostics must not contain credentials, provider turn state, request or response bodies, model output, or encrypted payloads.
- Attempt diagnostics must not change conversation content, connection selection, credentials, retry behavior, or provider request payloads.

## ChatGPT Codex Routing

- Only ChatGPT Codex OAuth dispatches send Codex turn metadata, `x-codex-routing-hint`, or `x-codex-turn-state`.
- Codex turn metadata is one object at `client_metadata["x-codex-turn-metadata"]`.
- Codex turn metadata contains `session_id`, `thread_id`, `turn_id`, and `window_id`. It contains `request_kind` only when the operation has a supported request kind.
- A main-agent or subagent dispatch maps its own Kent Session ID to `session_id` and `thread_id`, its Live Execution's Step ID to `turn_id`, and `<Session ID>:<compaction generation>` to `window_id`.
- Each subagent sends only its own Session and Agent Turn identities.
- A Reviewer dispatch maps `<Kent Session ID>/supervisor` to `session_id` and `thread_id`, copies the enclosing main Live Execution's Step ID to `turn_id`, and maps `<Kent Session ID>/supervisor:<main compaction generation>` to `window_id`.
- A nano dispatch must map its invocation UUID to `session_id`, `thread_id`, and `turn_id`, and `<invocation UUID>:0` to `window_id`, with request kind `turn`. Nano must prepare these facts once and use the same facts through retries without looking up an active parent Turn or compaction state.
- Ordinary main-agent, subagent, and Reviewer generation uses request kind `turn`. Responses compaction uses `compaction`. Local generation-based compaction omits request kind.
- Inline compaction uses the enclosing Live Execution's Step ID. Standalone compaction uses its own Live Execution's Step ID.
- A compaction dispatch uses the pre-compaction window. The compaction generation advances only after successful compaction.
- On an HTTP request or WebSocket opening handshake, `x-codex-routing-hint` is `model=<selected model>` and adds `;tier=<effective service tier>` only when that opening request uses a service tier.
- The routing model must be nonempty, must have no leading or trailing space or horizontal tab, must contain no semicolon, and must produce a valid outbound HTTP header value.
- Kent rejects an invalid routing model before the provider request. The routing tier must equal the service tier of the HTTP request or the request that opens the WebSocket.

### Responses WebSocket Routing

- Kent must follow Codex's WebSocket routing behavior. The routing hint belongs to the opening handshake. A later model or service-tier change must not by itself reopen the socket to update that hint.
- Each ChatGPT Codex create event must carry its current model, service tier and Codex turn metadata.
- A WebSocket opening handshake must omit `x-codex-turn-state`. Kent must not reopen a socket to replay that header.
- Kent must observe WebSocket turn state from the headers in `response.metadata` events. Subsequent create events within the same Agent Turn must propagate the accepted value exactly under `client_metadata["x-codex-turn-state"]`.
- WebSocket credentials follow the sticky-credential contract in [Provider Connections](provider-connections.md#responses-websockets).

## ChatGPT Codex Turn State

- Turn-state lifetime, ownership, validation and diagnostics apply to both transports. HTTP dispatches use initial response headers and request-header replay. Responses WebSockets use the metadata events and create metadata defined in the preceding section.
- ChatGPT Codex provider turn state must be opaque Agent Turn-scoped routing state. Kent must never trim, decode, canonicalize, join, log, persist, or present its value.
- For HTTP dispatches, Kent must observe one `x-codex-turn-state` value from each dispatch's initial HTTP response headers.
- A provider turn-state value must be nonempty, must have no leading or trailing space or horizontal tab, and must be valid as an outbound HTTP header value.
- The first accepted value must be the routing value for the entire routing lifetime. Later response values must not replace it.
- Each Agent Turn must start without provider turn state. Until Kent accepts a valid value, HTTP request headers and WebSocket create metadata must omit `x-codex-turn-state`.
- Subsequent requests within the same Agent Turn must replay the accepted value exactly through their transport's turn-state field, including retries, tool-loop continuations, and changed-payload repairs.
- Each Nano invocation must start without provider turn state and replay its first accepted value exactly on retries of that invocation.
- Nano routing state must not be shared with its parent Agent Turn or another Nano invocation.
- Steered input within an Agent Turn must use that turn's routing lifetime. Queued input must start a new Agent Turn with fresh routing state.
- Provider turn state must not cross unrelated Sessions, ChatGPT accounts, or provider endpoints. An authentication ownership change must clear the routing state.
- If Kent cannot identify the ChatGPT account, it must omit outbound provider turn state and must not accept returned provider turn state for replay. Missing account identity must not introduce a model-request failure.
- Reviewer work on the same ChatGPT account and provider endpoint must share the enclosing main Agent Turn's routing state while retaining its separate Reviewer metadata identity. The first accepted value from either main-agent or Reviewer work must become their shared turn value.
- Reviewer work on a different ChatGPT account or provider endpoint must start with independent routing state.
- Remote compaction within an Agent Turn must share that turn's routing state. Generation after compaction must replay the same first accepted value.
- Standalone compaction must start with fresh routing state.
- Separate local generation-based compaction must use its own routing state across its retries. Returning to main-agent generation must use the main Agent Turn's routing state.
- HTTP dispatches must send the complete prepared conversation. Responses WebSocket dispatches must use the unchanged prepared context with incremental continuation defined in [Provider Connections](provider-connections.md#responses-websockets). Provider turn state must not authorize rewriting history or independently selecting a conversation prefix.
- Provider turn-state absence or invalidity never changes the original model success, provider error, stream error, or cancellation result.
- Invalid state produces one redacted operator warning and one best-effort live `provider_turn_state_invalid` operational diagnostic per logical dispatch.
- The warning and diagnostic must not contain provider state, credentials, account data, request or response bodies, model output, or encrypted payloads.
- The diagnostic is not persisted or replayed and may be missed by an absent, disconnected, or overflowed client.
- Clients own all human-facing provider-state diagnostic copy.
