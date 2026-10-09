# Provider Connections

## Ownership And Selection

- Kent must own Provider Connection definitions in global server configuration. Workspace configuration must not define connections.
- A connection must own provider implementation, endpoint, authentication selection, and connection/protocol capabilities. Model, thinking, model-specific capabilities, and context/compaction policy must remain agent or role settings.
- The currently resolved Provider Connection's provider must own local context-token estimation. Saved Session Contracts and capability overrides must not select the estimator.
- Providers must use shared default content and image estimates unless their implementation overrides them. Default reasoning estimates must treat reasoning as plain text. First-party OpenAI API and ChatGPT connections must use provider-specific encrypted-reasoning and encrypted-compaction estimates.
- Local context-token estimation must not make remote token-count calls or fetch remote image content.
- Each connection must have one user-chosen configuration ID. IDs must begin with a lowercase ASCII letter and contain only lowercase ASCII letters, digits, hyphens, and underscores. Kent must reject duplicate IDs.
- Creation must suggest an editable unused numbered provider-based ID. Connections must not require a separate display name or hidden identity.
- Top-level `connection` must select the default for new unroled interactive Sessions. Roles must inherit that selection unless overridden. Main Workspace private configuration may assign developer-specific connection IDs to shared roles.
- Main agents, child roles, Supervisor, and Workflow must use the same connection-selection rules.
- Workflow preparation must use completed connection and default edits without requiring a server restart. Saved Session bindings must retain their defined precedence.
- Credentials must remain server-owned. Requests must use only the selected connection's credentials.
- Setup completion must remain server-wide. Credential readiness must be checked for the actual Session/role connection, not as a default-connection gate before Session selection. A broken default must not block working connections. If credentials are missing when an interactive Session opens, Kent must open the affected connection's authentication flow. Headless credential failures must return actionable errors.

## Connection Sets

- The `connection` setting must accept either one connection ID or an array of connection IDs. The default and role overrides must support both forms with the existing inheritance rules.
- Array selections must reference the existing global connection definitions. Kent must not require separately named groups or duplicate connection definitions.
- Kent must treat each array as a set. Kent must collapse duplicate IDs, reject empty arrays and invalid IDs, and ignore unknown IDs. If no defined member remains, selection must fail with an actionable error rather than inherit or select another connection.
- Kent must accept valid member definitions without requiring equal protocols, endpoints, or capabilities. Inclusion in a set must express the user's assertion that its members can serve the configured Agent.
- Selection must preserve the Agent's configured model, prompts, and settings. A fresh Session must use the selected connection's declared provider capabilities for supported request features. Selecting a connection that cannot serve the configured model must be treated as a user configuration error.
- Kent must assign fresh Agent Sessions by round-robin over the effective member set in global connection declaration order. Array order must not affect rotation.
- Identical effective member sets must share one server-wide rotation across roles and workspaces. Each distinct effective set must begin at its first globally declared member. Rotation need not survive server restart or preserve position across membership changes.
- Concurrent selections must use the same shared rotation.
- Listing Agents, reading settings, and previewing selections must not advance rotation.
- Selection must not check credentials, refresh tokens, or probe providers to choose a member. The selected connection must use the existing authentication flow or actionable error. Credential or provider failures must not automatically select another member.
- Ordinary turns, resume, and Workflow Continue must retain the saved connection without advancing rotation. Copied Sessions and rollback forks must inherit the saved connection rather than rotate merely because context was copied.
- Explicit Chat Settings Agent changes, Workflow Compact-and-Continue after successful compaction, and replacement of a deleted saved connection must select the next member of the applicable set under the existing Session Binding rules. These must remain the only forceful replacement cases. Existing valid bindings must remain usable when the current configured set cannot select a member.
- Connection sets must be configured through configuration files. Terminal connection management must continue to manage individual connections. Make default must select one connection without changing existing Session bindings.
- Explicit Supervisor `reviewer.connection` must accept one connection ID. When the Supervisor inherits connection selection, it must use the main Session's selected connection rather than allocate separately from the configured set.

## Authentication

- Kent must offer ChatGPT subscription, API-key Responses-compatible, and auth-less Responses-compatible setup choices.
- The two Responses-compatible choices must share one connection model with an optional environment-variable reference. Absence must select auth-less access. A configured reference whose value is missing or empty must fail with an actionable error, not select anonymous access.
- API-key setup must accept an endpoint prefilled with the OpenAI endpoint and an environment-variable name. Auth-less setup must require an endpoint.
- First-run setup, Add, and reference edits must require a nonempty environment-variable name for API-key access but must not check the variable's value or probe the provider before saving. Credential failures must be reported when the connection is used.
- Kent must not persist API keys in configuration or the OAuth credential store. API-key authentication must read the explicitly referenced server environment value.
- Kent must not automatically adopt `OPENAI_API_KEY` or borrow another connection's credentials. Auth-less requests must send no authentication credentials.
- ChatGPT must retain browser and device sign-in, including browser callback or pasted callback URL/code. OAuth failure must not fall back to an API key. Refresh failures must remain observable and actionable.
- If a terminal Session has saved OAuth credentials, opening it must not attempt refresh. An expired refresh token must not close the terminal, block chat or `/login`, or require deleting saved credentials.
- If refreshing a terminal Session's saved OAuth credentials fails during a request, Kent must persist a transcript error identifying the Provider Connection, preserving readable original diagnostics, and directing the user to sign in. The error must read: "Failed to authenticate the provider connection: <readable Go error>. Run /login to authenticate connection <conn-id>, used for this session."
- Provider authentication failure must not automatically retry or replay the failed request or select another connection.
- Re-authentication must be allowed during execution. Already-sent requests must continue. Subsequent requests must use the newly saved credentials without reopening the terminal Session. Re-authentication must not cancel, replay, or reroute work or change unrelated connections.

## Server Environment

- Kent must load `.env` from its server persistence root at startup, including when installed as a service. The default path must be `~/.kent/.env`. Kent must not load repository `.env` files for this purpose.
- A present process environment value must take precedence over the file, including an empty value. Kent must require restart to observe file edits or changes to its launch environment.
- The file must be operator-managed and owner-only. Onboarding and `/login` must not write its secrets.
- The file must supply server environment values without injecting them into agent shell environments.
- Agent subprocess environments must exclude explicitly referenced provider-key variables regardless of which server environment source supplied their values.
- Missing or empty files must be allowed. Unreadable, malformed, or insecure-permission files must fail server startup with safe diagnostics. Kent must use standard dotenv syntax; malformed-line reporting is optional.
- Missing-variable guidance must identify the server-side file path and restart requirement without revealing secrets.

## Session Binding

- Kent must persist a Session's resolved connection ID and preserve an existing binding across ordinary resume and direct continuation.
- Existing-Session settings reads and non-Agent changes must use that binding even when current Agent/default connection references are unavailable. An explicit Agent change must validate the target Agent's current connection and leave the Session unchanged when it is unavailable.
- When the user explicitly changes the Agent in an unlocked Session through Chat Settings, Kent must select and persist the chosen Agent's current connection. Locked Sessions must retain their existing Agent and connection.
- Workflow Compact-and-Continue must use the outgoing connection for compaction and apply the target role's current connection only after successful compaction, following the fresh-contract and failure/interruption boundaries in [Workflow orchestration](workflow-orchestration.md).
- If the saved ID no longer exists, Kent must resolve the replacement from the Session's current role or the unroled default, persist it, and show a notice naming the old and new IDs before the next request. Confirmation must not be required.
- Sessions without a saved ID must bind from their current role/default on first resume. Kent must preserve their established model/request contract.
- Invalid or missing replacement references must block dispatch. Expired credentials and network/provider failures must not cause automatic replacement.
- Manual ID renaming may require re-authentication. Kent need not coordinate renaming.
- Existing Sessions that change connections must follow [Retained Context Compatibility](#retained-context-compatibility).

## Retained Context Compatibility

- Kent must attribute newly retained reasoning and native compaction checkpoints to their producing Reasoning Type. Connection IDs and model names must not determine Reasoning Type.
- When an existing Session changes connection, Kent must omit retained encrypted reasoning that has a Reasoning Type incompatible with the destination or that the destination explicitly cannot consume. This is a narrow exception to the prohibition on filtering already-sent history.
- Omission must apply to the entire incompatible reasoning item, including its readable summary. Kent must preserve its historical Reasoning Trace and must not create substitute summary messages.
- Kent must retain unencrypted reasoning in outgoing context when the destination can represent it. Kent must preserve other active messages and complete tool-call/result relationships unchanged.
- Kent must preserve persisted historical entries, Session identity, and Workflow associations. Reasoning omission must not create a history replacement or require a recovery command or confirmation.
- When Kent omits incompatible reasoning, Kent must append a transcript warning with the exact text: "Conversation was switched to another provider/model - reasoning was lost, degrading output quality". Kent must continue the original pending work automatically.
- Kent must never omit the active native compaction checkpoint. Known incompatibility with its encrypted format must block dispatch with an explanation that the Session cannot continue with that encrypted compaction summary and instructions to restore a compatible connection.
- Local compaction summaries must remain unaffected. Encrypted checkpoints outside the latest active context must not affect compatibility checks.
- For retained items without attribution, Kent may infer Reasoning Type only from retained provider evidence that identifies the item's producing Agent Step and a known format. Missing or conflicting evidence must leave attribution unknown. A replacement destination or backfilled Session Contract must not establish an item's origin. Kent must not rewrite old entries to add attribution.
- When a newly retained item's producing format is unknown, Kent must record unknown attribution and preserve it across resume. Kent must not relabel that item from an older Session Contract.
- Unknown compatibility, including cross-account compatibility or unavailable historical attribution, must not itself block dispatch. Kent must send such context unchanged when the destination protocol permits it and surface provider rejection. A known unsupported native checkpoint must still block dispatch.
- Kent must not automatically call a provider to recover hidden context, select substitute credentials, or retry a request rejected by the provider as incompatible.
- Compatibility checks must use only the bounded active context. Kent must not read the complete historical transcript or copy the entire retained collection to omit reasoning.
- Outside this reasoning-omission exception, Kent must not strip retained context or rewrite already-sent historical items.

## Terminal Setup And Login

- When no Provider Connections are defined, every interactive terminal open must enter connection setup. This includes terminal startup and subsequent Session opens. Headless agent launches must return an actionable setup error.
- First sign-in must be part of onboarding before provider-dependent choices. Kent must defer saving the connection, credentials, and settings until Finish. Slash commands must be unavailable before onboarding.
- Explicitly canceling setup or restarting the server before Finish must discard the unsaved sign-in. Finish must write configuration last. A failed configuration write must leave onboarding incomplete and report the failure; unused saved credentials are acceptable.
- Kent must own one shared pending first-run connection per server. Opening another TUI must not reset it. Explicit setup changes or discard, Finish, and server restart must control its lifetime. An OAuth result arriving after explicit discard must not persist the discarded sign-in.
- First-run onboarding must implicitly select the first connection as default without confirmation.
- After onboarding, `/login` must list Add connection first and existing connections afterward. With zero connections it must enter Add directly. Config-authored connections must appear in the same flow.
- Add must save the new connection in global configuration. When no connections or global default are configured, Add must select the first added connection as the global default without confirmation. Otherwise, Add must offer an explicit Make default choice. Make default must change only the global default and explain any overriding workspace default. Existing role assignments and Session bindings must stay unchanged.
- Add for a ChatGPT connection must save its definition only after successful sign-in. Failed sign-in or cancellation before the operation is accepted must leave no definition. Observer disconnect must not cancel accepted work.
- Selecting an existing ChatGPT connection must re-authenticate without changing its ID or creating a duplicate. Selecting an API-key connection must show and allow replacement of its environment-variable reference, never request the secret. Selecting an auth-less connection must explain that sign-in is unnecessary without editing its endpoint.
- The API-key field must show: "Don't paste your API key here. This is the name of the **environment variable** Kent will read **at the server's location** to get the api key from. Alternatively, place it in a ~/.kent/.env file."
- Connection setup and management operations must show a full-screen animated spinner without loading labels while waiting for a response.
- The server must validate connection definitions and references. Terminal forms must not define separate validation rules.
- `/logout` must open the same picker without deleting credentials.

## Configuration Cutover

- Kent must automatically convert provider-access settings in global configuration, including globally declared roles and Supervisor, into connection definitions and references. Identical access settings must share a connection.
- Conversion must preserve model, role, tool, endpoint, capability, and unrelated user-authored settings. Workspace/shared/private provider-access settings must require manual edits with actionable diagnostics.
- Automatic conversion and connection edits must leave untouched configuration source exactly unchanged, including comments, spacing, ordering, quoting, and line endings. Kent may change edited settings and the syntax required to insert or remove settings. Kent may format inserted content.
- When Kent removes a setting, comments attached to that setting may disappear. When Kent removes a table, comments within that table may disappear. When a deletion reaches the end of the file, empty separator lines immediately before the deleted content may disappear. Kent must leave all other comments unchanged, including comments surrounding a changed setting.
- Converted ChatGPT connections must require re-authentication. Obsolete saved API keys need not be preserved.
- Kent must report changed and failed files, write individual files atomically, and stop affected work on ambiguous mappings or write failures. Cross-file rollback is not required.
- Kent must remove obsolete provider-selection settings and readers after conversion. It must not retain permanent old/new authorities or fallback readers.
