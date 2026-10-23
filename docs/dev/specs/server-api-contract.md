## Authority And Compatibility

- Protobuf schemas are the sole editable authority for the Kent server API.
- Generated Protobuf messages are the API-boundary contract for official Go and TypeScript clients.
- Kent carries serialized Protobuf API messages in typed binary WebSocket envelopes. gRPC and Connect transport semantics are outside this contract.
- Viewed-image original bodies may use authenticated streaming HTTP delivery with the same Session access checks and protocol-version gating. Normal API messages must use their existing transport.
- Kent's protocol version is the sole compatibility authority for API schema changes.
- Every contract change must increment the protocol version.
- Clients and servers must use the same protocol version.
- Protobuf packages must not introduce an independent `v1` or `v2` compatibility authority.
- Kent promises no source, wire, or backward schema compatibility across protocol versions.
- Protocol capability flags must not negotiate route or schema availability.
- Onboarding model, provider, and import capability facts are ordinary product data, with no protocol compatibility role.
- The Protobuf schema must support standard code generation without changing API ownership.
- The Protobuf schema is platform-neutral. No client or server owns it.
- Applications compile generated contract code into their artifacts and never load `.proto` files at runtime.

### Global Desktop Attention Stream

- The global Desktop attention stream must use the generated Protobuf contract and binary transport.
- Its Session prompt target must include the owning Project identity and Session identity supplied by the server.
- Session-scoped operations, including Session attention, pending Question and Approval reads, batch answers, transcript hydration and events, and attached-Session descriptors, must use only their generated Protobuf contracts.
- The global Desktop attention stream must not introduce a parallel transport or fallback decoding for any operation.

## Operations And Transport

- Protobuf services and typed Kent method options own operation request types, response types, operation kind, and unary connection policy.
- Generated descriptors are the sole operation identity and route-metadata authority.
- Active operation names use strict lower-snake `<package>.<service>.<method>` form with no service elision and no `kent` prefix.
- Each nonempty package segment must start with an ASCII lowercase letter and contain only ASCII lowercase letters, digits, or underscores.
- Service and method identifiers convert from PascalCase to lower snake case by splitting before an uppercase letter when a lowercase letter or digit precedes it, and before the final uppercase letter of an uppercase run when a lowercase letter follows it.
- Digits belong to their surrounding token.
- `APIStatus` becomes `api_status`, `UUID` becomes `uuid`, `HTTP2Server` becomes `http2_server`, and `SubmitUserTurn` becomes `submit_user_turn`.
- Both Go and TypeScript must apply the same operation-name validation and conversion.
- The generated request and response contract must not contain a generic application request ID.
- Multiplexed call and result envelopes may carry opaque connection-local correlation values.
- Connection-local correlation values must not reach service or domain code, persistence, replay, memoization, or reconciliation.
- Notifications and events must not carry connection-local correlation values.
- Every unary method must declare either multiplexed or dedicated connection policy. An unspecified policy is invalid.
- Server Update Status, Workflow Task Search, Runtime Submit User Turn, Chat Submit User Shell Command, Runtime Compact Context, Runtime Live Stop, Runtime Live Wait, and Runtime Live Watch use dedicated unary connections.
- Interactive Session opening must promote a Subagent Session through an explicit Session mutation without loading a Runtime. Session planning is data-only.
- Subscription, progress, and notification connection behavior derives from operation kind.
- A dedicated unary call owns one WebSocket and performs the applicable handshake, authentication, and attachment steps on that connection.
- Caller cancellation closes a dedicated unary connection and stops that caller's waiting and delivery.
- Caller cancellation never terminates a Chat Operation, rolls back a committed Session, changes accepted work, changes the client's multiplexed control connection, or changes another operation.
- The transport must not add a cancel-frame variant for server-owned work.
- Their operation specifications own call, notification, subscription, progress, authentication, attachment, and cancellation semantics.
- Subscription and progress operations use the terminal completion and failure behavior defined by their owning operation specifications. Transport framing adds no generic terminal product outcome.

## Message Design

- Every route-reachable named scalar must be classified as open or validated text, identifier, or closed enum.
- Closed enums must declare every supported value.
- Message-local rules must be declared in the Protobuf contract and enforced through generated validation, except for the known error-code/detail relationships defined below.
- The server-owned domain boundary must own rules that require server state.
- Unclassified scalars and validators are invalid API contract state.
- Kent-owned variant payloads must use typed Protobuf messages or explicit `oneof` branches.
- Transcript events, handshake outcomes, workspace selections, API error details, and Attention variants must not use generic dynamic payloads.
- Session and Prompt Attention and Workflow-owned Attention must each have one typed representation under their owning domain.
- Chat must keep showing assistant answers, streamed text, tool output, and reasoning. The server must send this content in defined API fields. It must not send clients the whole request or response exchanged with a model provider.
- Client-facing operation messages must not wrap provider-owned JSON.
- Operation messages must not contain unclassified `bytes`, `Any`, generic maps, or raw JSON.
- A transport envelope may contain serialized Protobuf bytes only when its operation descriptor declares the exact payload type.
- Optional values represent semantic absence through Protobuf presence and generated optional platform types.
- Sentinel values and explicit JSON-style null entries must not represent absence.
- Missing repeated and map fields represent empty collections.
- Optional wrapper messages are reserved for semantics where absence differs from present-empty.
- Operations with no request content or no success data must use `google.protobuf.Empty`.
- Successful Runtime activation must return an empty success payload. Clients observe Runtime state through server-owned reads and subscriptions.
- Chat actions must share one typed representation for invocation preparation choices. Foreground user shell must be a separate Chat request using the same Session target and server-owned preparation rather than a second public preparation approach.
- API instants must use `google.protobuf.Timestamp`.
- API elapsed amounts must use `google.protobuf.Duration`.
- Raw numeric time representations are permitted only when an external system owns that representation.
- Session identity fields must use canonical lowercase UUID v4 text.
- Session selector inputs and short-alias lookup fields are distinct from Session identity fields.
- Other existing first-party identifiers retain their owning domain format. The Protobuf API does not migrate the persistence or domain identity model.
- First-party UUID v4 identifiers retain canonical textual wire form.
- Session target inputs must accept a full UUID or a Session Short ID. The server must resolve the target to its UUID before target authorization, self-target checks, and execution.
- Clients must not perform a separate preflight identity lookup for a Session selector.
- Session subscriptions must use the server's operation-specific Project authorization after target resolution. Clients must not restrict those subscriptions to the Session attached to their connection.
- Responses that present a Session identifier must include its assigned Session Short ID alongside its UUID when a Short ID exists.
- Returned Session identities and caller Session identities must use canonical UUIDs. Internal Session operations must use UUID identity rather than distinguish selector forms.
- Workspace-scoped Worktree operations must treat the selected Session context as a Session target, not as the invoking Session identity.
- Third-party and provider identifiers use validated string fields unless their owning contract defines another representation.
- Generated TypeScript uses the standard Protobuf `bigint` representation for `int64` and `uint64`.
- JavaScript-facing 64-bit values must be within the JavaScript safe-integer range.

### Task requests

- Task pagination offsets must be between 0 and 2,147,483,647.
- Task continuation offsets must be between 1 and 2,147,483,647.
- Task Search must accept Project and status filter arrays in any order.
- Each Task Search filter array must contain unique values.

## Results And Errors

- Unary results, progress final results, and subscription-start acknowledgements must use an operation-specific top-level `oneof` with exactly one `success` or `error` branch.
- A present error branch is an operation failure.
- Each operation error must contain a nonempty stable code and a typed detail `oneof`.
- Known error codes must declare and validate their required detail variant.
- Known error-code/detail relationships must be checked centrally at the transport boundary from their sole Protobuf declaration.
- A missing outcome, empty error code, or known code with missing or incorrect detail is malformed.
- A client must surface an unknown nonempty error code as a generic error while preserving available unknown fields.
- Subscription-start failure occurs before acknowledgement and is distinct from later events and terminal completion.
- Subscription terminal failures must use generated Protobuf error codes, including Worktree setup completion. Clients must report the owning operation's terminal outcome and typed failure facts unchanged.
- Client-visible wording is client-owned and derived from stable codes and typed details.
- Validation errors in user-provided data must describe what the user must correct. Clients must not present these errors as obscure internal contract failures.
- The shared `internal_failure` code represents otherwise-unclassified operational failures.
- Onboarding rollback errors use one typed primary failure plus structured rollback facts containing operation and cause.
- Session workspace retargeting exposes its background-process blocker through the public `background_process_active` error.
- Clients must not stringify errors or depend on server-authored human-readable messages.

## Validation And Decoding

- External request values must be validated once at the earliest server request boundary before downstream server logic consumes them.
- A shared authority, such as a database constraint used by several writers, must own its validation.
- Clients must run generated Protobuf constraints on server responses and events.
- Platforms must centralize generated validation at their transport boundary.
- Runtime Activity must retain one calculation-time contract check before publishing its read model. Update Status must retain its calculation-time contract check before caching the result, including its existing debug failure and production check-failed outcome.
- Generated clients must carry unknown Protobuf fields unchanged.
- Generated clients must reject unknown enum values.
- Except for the Desktop transcript subscription rule below, malformed Protobuf bytes, unknown operations, wrong frame direction, invalid envelopes, and known-operation validation failures must reject only the affected frame after a connection is established.
- Kent must keep the established connection available for unrelated valid traffic after a rejected frame.
- Kent must return or publish a typed protocol or validation failure when the frame can be correlated safely.
- A Desktop transcript subscription must report the error and close its connection without automatic reconnect when it receives a malformed envelope, an unexpected operation or frame kind, or an invalid completion message. If an expected transcript event is invalid or belongs to a different Session, Desktop must report the error and discard that event without closing the connection. This exception must not change how valid stream completion, network failure, or client callback failures are handled.
- Protocol-version mismatch and invalid handshake or authentication establishment must reject setup and close the connection.
- Kent must not add strike counters, rate limits, or repeated-invalid-frame disconnect state solely for contract validation.
- Debug builds fail fast when Kent attempts to emit a generated message that violates its declared contract.
- Delivered Transcript and Worktree Setup notifications must fail fast on invalid local output in debug and production. Their contract check must run at final notification encoding. Unobserved notifications must not receive a separate generated validation check.
- Debug logs for invalid wire messages must identify the validation failure so developers can understand and fix it.
- Malformed peer input is external data and must be rejected without crashing in debug and production.
- Production must surface an internal contract error and must not send an invalid frame.

## Startup Availability

- Handshake, authentication, readiness, onboarding capability facts, and onboarding finalization are available before server activation completes.
- Operations that require activation must fail with a typed onboarding-required or activation-failed result while activation is unavailable.
- Handshake messages must not contain route capability flags.
