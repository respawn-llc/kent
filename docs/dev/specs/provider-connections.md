## Ownership And Selection

- Kent must own Provider Connection definitions in global server configuration. Workspace configuration must not define connections.
- A connection must own provider implementation, endpoint, authentication selection, and connection/protocol capabilities. Agents or roles must own Agent model, thinking, model-specific capabilities, and context/compaction policy. Speech-model configuration follows [Speech Input](speech-input.md).
- The resolved Provider Connection's provider must own local context-token estimation. Saved Session Contracts and capability overrides must not select the estimator.
- Providers must use shared default content and image estimates unless their implementation overrides them. Default reasoning estimates must treat reasoning as plain text. First-party OpenAI API and ChatGPT connections must use provider-specific encrypted-reasoning and encrypted-compaction estimates.
- Local context-token estimation must not make remote token-count calls or fetch remote image content.
- For first-party OpenAI API and ChatGPT connections, local estimation of original-detail inline images must count 32-pixel patches from locally available dimensions. It must round each dimension up to a whole patch and cap each image contribution at 10,000 tokens.
- For first-party OpenAI API and ChatGPT connections, if local dimensions cannot be read from an original-detail inline image or remote image URL, local estimation must use the ordinary image estimate. This image rule must not change file attachment estimates.
- Each connection must have one user-chosen configuration ID. IDs must begin with a lowercase ASCII letter and contain only lowercase ASCII letters, digits, hyphens, and underscores. Kent must reject duplicate IDs.
- Creation must suggest an editable unused numbered provider-based ID. Connections must not require a separate display name or hidden identity.
- Top-level `connection` must select the default for new unroled interactive Sessions. Roles must inherit that selection unless overridden. Main Workspace private configuration may assign developer-specific connection IDs to shared roles.
- When effective `connection` is absent and exactly one Provider Connection is defined, Kent must select that connection without rewriting configuration. With zero or multiple definitions, selection must fail. Explicit selections and saved Session bindings take precedence.
- When the top-level `connection` selector contains one ID that points to a removed definition and other Provider Connections remain, Kent must select from the remaining definitions for new Sessions and replacement of a deleted saved connection, regardless of whether the selector was written as a scalar or one-element array. This fallback must not rewrite the selector or expand a role-owned selector or a multi-ID array selector.
- Main agents, child roles, Supervisor, and Workflow must use the same connection-selection rules.
- Workflow preparation must use completed connection and default edits without requiring a server restart. Saved Session bindings take precedence as defined by Session Binding.
- The server must own credentials. Requests must use only the selected connection's credentials.
- The server must own provider sign-in initiation, device polling, token exchange, and refresh. Clients must receive sign-in instructions without access or refresh credentials. Clients must own browser launch and callback or pasted-input collection.
- Setup completion must apply server-wide. Credential readiness must be checked for the actual Session/role connection, not as a default-connection gate before Session selection. A broken default must not block working connections. Interactive terminal authentication and connection-setup gating must follow [TUI Startup](tui-startup.md#auth-gate). Headless credential failures must return actionable errors.

## Responses WebSockets

- Kent must use Responses WebSockets for generation through first-party OpenAI API-key, ChatGPT Codex OAuth, and public xAI API-key and OAuth connections. Grok CLI proxy and generic Responses-compatible connections must use HTTP generation.
- A Session's socket must belong to its Active Session Runtime and follow that Runtime's existing idle retirement. Client observation must not own the socket or provider work. Ordinary completed turns must reuse the socket.
- A WebSocket must use the credentials selected when it opens until that socket ends. Model requests on an open socket must not refresh credentials. Opening a new socket must resolve credentials through the selected Provider Connection's authentication owner.
- Kent must send the complete prepared working context on a fresh socket. When subsequent prepared context extends the previous input and completed output unchanged, Kent must send only appended items with the prior response ID. Changed request settings or a non-prefix context must cause a complete current-context request without continuing the old response chain.
- Public xAI continuation requests must omit `instructions` when sending `previous_response_id`. The response chain must supply the instructions for those requests. First and full-context requests must send the current instructions. Changed instructions must start a full-context request without the prior response ID.
- Compaction must keep the socket open and send the complete replacement context on the next generation request.
- Explicit Stop during provider generation must close that socket so generation cannot continue unobserved. A later invocation must start with the complete prepared context on a fresh socket.
- Model-request retries, authentication handling, and allowance switching must follow their owning contracts. Kent must add no SDK recovery, automatic command replay, or WebSocket-to-HTTP fallback.
- A socket must allow messages up to 256 MiB and bound queued incoming data to 64 MiB and 1,600 events. Exceeding a bound must use existing model-error/retry behavior without dropping or truncating events.

## Connection Sets

- The `connection` setting must accept either one connection ID or an array of connection IDs. The default and role overrides must support both forms with the existing inheritance rules.
- Array selections must reference the existing global connection definitions. Kent must not require separately named groups or duplicate connection definitions.
- Kent must treat array selections as sets. Kent must collapse duplicate IDs, reject empty arrays and invalid IDs, and ignore unknown IDs. A role-owned array or a multi-ID top-level array with no defined members must fail with an actionable error. A one-ID top-level array follows the removed-default rule above.
- Kent must accept valid member definitions without requiring equal protocols, endpoints, or capabilities. Inclusion in a set must express the user's assertion that its members can serve the configured Agent.
- Selection must not change the Agent's configured model, prompts, or settings. A fresh Session must use the selected connection's declared provider capabilities for supported request features. Selecting a connection that cannot serve the configured model must be treated as a user configuration error.
- Every fresh Session must select the connection with the lowest estimated consumption over roughly the preceding seven days from its effective member set. The selected role's connection setting must define that set when supplied. Otherwise, the Session must use the inherited connection selector. Array order must not affect selection.
- All Session types, roles, workspaces, and overlapping member sets must share one server-wide consumption history and connection assignment order. This includes interactive, headless, subagent, Workflow, and forked Sessions. Usage through a singleton selection must contribute to the same consumption history. A singleton selection must update the same assignment order.
- Equal consumption scores must prefer the connection with the oldest last assignment. Connections without an assignment in the current server process must precede assigned connections with equal scores. Remaining ties must follow global connection declaration order. Assignment order must exist only in memory and reset on server restart.
- Concurrent selections must update the shared assignment order under one owner.
- Listing Agents, reading settings, and previewing selections must not change assignment order.
- Selection must not check credentials, refresh tokens, or probe providers to choose a member. The selected connection must use the existing authentication flow or actionable error. Credential or provider failures must not automatically select another member except under Automatic usage fallback.
- The selected connection must be sticky for that Session except under Automatic usage fallback. Ordinary turns, resume, and Workflow Continue reusing the same Session ID must otherwise use the saved connection without changing assignment order. A child with a new Session ID must select a fresh connection even when it copies conversation or continuation context from another Session. Workflow-internal copies follow the [Workflow context-preservation contract](workflow-orchestration.md#context-preservation-and-bindings). User-driven Edit/Fork follows the interactive Session creation contract in [Core Runtime Tools](core-runtime-tools.md).
- Explicit Chat Settings Agent changes and replacement of a deleted saved connection must use the same assignment order for the applicable set under the Session Binding rules. Automatic usage fallback must also permit replacement as defined below. Existing valid bindings must be usable when the current configured set cannot select a member.
- Connection sets must be configured through configuration files. Terminal connection management must continue to manage individual connections. Make default must select one connection without changing existing Session bindings.
- Explicit Supervisor `reviewer.connection` must accept one connection ID. When the Supervisor inherits connection selection, it must use the main Session's selected connection rather than allocate separately from the configured set.
- Nano `nano.connection` must accept one connection ID or an array. Each nano invocation must select once through the same shared server-wide rotation for its effective member set and use that connection throughout the invocation.

## Consumption estimates

- Kent must attribute reported usage to the Provider Connection that served the operation. Copied conversation context and restored Session usage totals must not count as new consumption.
- Kent must calculate consumption from uncached input, cached input, and output tokens. Cached input must count once as part of input, not as additional input.
- For models with known pricing, Kent must use standard short-context API prices from official provider sources. Subscription connections must use the same API-price estimate. The estimate must exclude speed-tier premiums, long-context premiums, cache-write charges, and other surcharges.
- Kent must price the provider-reported served model when available. Otherwise, Kent must price the requested model.
- For models without known pricing, Kent must use estimated prices of $1 per million uncached input tokens, $0.10 per million cached input tokens, and $10 per million output tokens.
- Kent must treat missing usage fields as zero for this estimate. Missing cached input must leave reported input priced as uncached input. Kent must not estimate entirely missing usage or retry provider operations to obtain it.
- Kent must persist recent consumption history across server restarts and expire consumption older than roughly seven days. Collection must begin without historical backfill. Existing Sessions must contribute only new usage.
- Selection must use observed consumption without reserving or predicting the cost of unfinished work. Several new Sessions may select the same lower-consumption connection before usage arrives.
- A failure to persist consumption history must not interrupt chat. Kent must log the failure. Unsaved estimates may be lost on restart. Kent must not replay requests or reconstruct history to recover those estimates.
- Separate consumption-history writes must run asynchronously without delaying the next agent turn. They must use the available server-owned Session, Workflow, or Nano parent lifetime rather than the individual model request lifetime.
- If consumption history cannot be read for an allocation, Kent must log the error and select using assignment order alone for that allocation. This must not replace ordinary validation, authentication, or provider error handling.
- Consumption estimates must guide routing only. They must not represent provider invoices or promise equal subscription-allowance drain.

## Automatic usage fallback

- Global server configuration must expose `connection_usage_fallback_seconds` as a nonnegative integer with a default of `300`. Zero must disable automatic usage fallback. A positive value must enable fallback and define the exhausted connection's cooldown in seconds.
- Confirmed usage allowance or credit exhaustion must make the affected connection unavailable during its server-wide cooldown. New Session allocations and subsequent model requests from already-bound Sessions must skip connections in cooldown.
- Kent must identify exhaustion from provider-specific structured error codes or unambiguous provider-specific HTTP status semantics. A bare HTTP 429, temporary rate limit, authentication failure, overload, or unknown error must not trigger fallback.
- Grok's exact WKE machine-tag format must be accepted as a provider error-code source. The `personal-team-blocked:spending-limit` code must identify exhaustion. English error wording must not determine exhaustion.
- Kent must select replacements only from the effective connection set configured for the affected Agent or role. Kent must not use the global list of connection definitions as an alternative set. Including incompatible members must be treated as user configuration error.
- Fallback must use the same consumption-based selection as fresh Session allocation, excluding cooling and already-attempted members. Each replacement assignment must update the shared assignment order used to break consumption ties.
- Kent must retry the failed Agent Step automatically on the replacement connection without confirmation. Fallback must not require keeping incomplete output from the failed attempt.
- Successfully completed steps, historical conversation, Session identity, Workflow associations, and the Agent's model, prompts, and settings must be unchanged. Connection changes must follow [Context compatibility](#context-compatibility).
- After local setup and context checks succeed, Kent must save the replacement as the Session's connection before sending its request and append a transcript notice naming the previous and replacement connections. A failure from that request must not restore the exhausted connection.
- Each fallback sequence must attempt each eligible member at most once. If a replacement fails for a reason other than confirmed exhaustion, Kent must use the existing failure and retry behavior without selecting further members.
- If every eligible member is exhausted or cooling down, Kent must stop with an actionable error identifying the exhausted connections and cooldown availability. Kent must not schedule automatic resumption.
- Fallback must apply to generation and compaction for interactive Agents, subagents, and Workflow execution. An inheriting Supervisor must follow the main Session's replacement. An explicitly configured single Supervisor connection must not select an alternative.

## Authentication

- When more than one Provider Connection is configured, Session connection displays must show the Session's saved connection ID and authentication mode as `Session Connection: <id> (<mode>)`. The mode must be `Subscription`, `API Key`, or `No Auth`. With one configured connection, this row must be hidden. Reading this information must not allocate a connection or advance rotation.

- Kent must offer OpenAI Subscription and API Key, Grok Subscription and API Key, and Generic Responses-compatible api key and No auth setup choices.
- The two Responses-compatible choices must share one connection model with an optional environment-variable reference. Absence must select auth-less access. A configured reference whose value is missing or empty must fail with an actionable error. It must not select anonymous access.
- OpenAI and Grok API-key setup must use their official endpoints without an endpoint field and must request an environment-variable name. Generic API-key setup must request an endpoint and environment-variable name. Auth-less setup must require an endpoint.
- Grok subscription setup must recommend public API OAuth and offer the CLI subscription proxy as experimental.
- Global connection configuration must allow explicit `protocol` values `grok-cli-proxy` and `grok-oauth-api`. The first must select the official Grok Build subscription proxy. The second must select the official public xAI API with subscription OAuth.
- The terminal provider picker must offer “Grok Subscription via the API (recommended)”, “Grok Subscription via the CLI proxy (experimental)”, and “Grok API key” in that order within the Grok group.
- The respective hints must be “Official integration path for Kent, but no free plan access”, “Emulate the official CLI. May be removed or break due to compatibility.”, and “Regular API Key billing”.
- Provider picker options must use bullets. They must not use numbers. Each hint must be faint and indented beneath its title. This rule must not change other onboarding single-choice pages.
- Setup must save the selected protocol. Different subscription routes must form distinct Provider Connections even when they use the same account.
- Manual changes between the two Grok subscription protocols must apply when the runtime opens or resumes. Such changes must not reroute an in-flight request.
- Grok API-key access must use the official public xAI API and the connection's referenced server environment variable.
- Grok must not automatically switch between subscription routes or fall back to API-key access except between explicitly configured members under Automatic usage fallback.
- First-run setup, Add, and reference edits must require a nonempty environment-variable name for API-key access but must not check the variable's value or probe the provider before saving. Credential failures must be reported when the connection is used.
- Kent must not persist API keys in configuration or the OAuth credential store. API-key authentication must read the explicitly referenced server environment value.
- Kent must not automatically adopt `OPENAI_API_KEY` or borrow another connection's credentials. Auth-less requests must send no authentication credentials.
- ChatGPT must support browser and device sign-in, including browser callback or pasted callback URL/code. OAuth failure must not fall back to an API key. Refresh failures must be observable and actionable.
- OAuth credential expiry must be optional for both ChatGPT and Grok. If the provider omits expiry, Kent must store no expiry and must not schedule expiry-based refresh or invent an expiration time.
- Grok subscription connections must use device authorization as their sole sign-in method. Kent must open the verification page automatically and keep the issued code visible while the server waits for authorization. The user must be able to compare that code with the browser before approving access. Successful authorization must complete the sign-in step.
- Grok must identify Kent as the client while sending the selected service's required protocol headers. Kent must send proxy-specific authentication and version headers only to the CLI subscription proxy.
- If Grok rejects the client's protocol version, Kent must report the update requirement without switching routes or replaying the request.
- Grok entitlement rejection must include the provider's original readable diagnostic and identify the affected connection. Kent must distinguish subscription or credit requirements from invalid sign-in and must not claim that re-authentication will fix entitlement.
- HTTP 403 alone must not classify a response as an authentication failure for any provider. Permission or safety refusals without authentication evidence must not direct the user to sign in again.
- Subscription usage requests must use only the selected Provider Connection's credentials. ChatGPT subscription usage requests must use the ChatGPT usage service.
- Both Grok subscription protocols must request quota from the official CLI proxy billing service with that connection's OAuth credentials and the service's required headers. Kent has no established subscription quota request path through the public xAI API. This quota-only use of the CLI proxy must not change the connection's chat route.
- Grok API-key connections must not request subscription quota.
- The terminal `/status` Auth section must be hidden for other authentication methods until provider-specific quota support exists.
- A quota failure must not interrupt chat, automatically refresh sign-in, retry the quota request, or switch connections or routes. Kent must show available Auth details and readable connection-specific failure diagnostics. If Grok billing returns HTTP 404, 403, or 401, Kent must explain that quota is unavailable for this sign-in method. This presentation must not introduce a request fallback. Entitlement failures must not advise re-authentication.
- If a terminal Session has saved OAuth credentials, opening it must not attempt refresh. An expired refresh token must not close the terminal, block chat or `/login`, or require deleting saved credentials.
- If refreshing a terminal Session's saved OAuth credentials fails during a request, Kent must persist a transcript error identifying the Provider Connection, preserving readable original diagnostics, and directing the user to sign in. The error must read: "Failed to authenticate the provider connection: <readable Go error>. Run /login to authenticate connection <conn-id>, used for this session."
- Provider authentication failure must not automatically retry or replay the failed request or select another connection.
- Re-authentication must be allowed during execution. Already-sent requests must continue. Subsequent HTTP requests and new WebSocket handshakes must use the credentials saved by re-authentication without reopening the terminal Session. Re-authentication must not replace credentials on an open WebSocket, cancel, replay, or reroute work, or change unrelated connections.

## Desktop subscription usage

- Desktop must present subscription usage as read-only Provider Connection information. It must omit API-key, no-auth, and other connections without subscription usage support.
- Desktop may load the complete configured connection catalog for subscription-usage inspection. The popup must render its connection rows lazily.
- Each connection's usage display must show its name and supplied plan, all supplied quota windows, and each supplied reset countdown. Ordinary subscription windows must show allowance remaining. Grok must show its supplied Build and Chat consumption breakdown.
- Desktop must fetch quota when a subscription-usage popup opens and refresh it on reopening. Opening the navigation Chrome popup must fetch all supported subscription connections with a small fixed limit on simultaneous requests. Chat and navigation Chrome must share cached usage results for the same connection. Desktop must not poll quota periodically.
- During refresh, Desktop must show cached values and only a small header activity indicator. Successful connection results must appear progressively.
- Desktop must omit unavailable Session bindings and quotas that the provider identifies as unsupported for the sign-in method. Desktop must show intact configured connections with credential errors or temporary quota failures and readable connection-specific diagnostics.
- A subscription-usage popup must offer one Retry icon in its header without a label or tooltip when lookups fail. Retry must request only failed lookups, and its icon must spin during retry. Loading must use an indicator without a label. A connection-catalog failure must use the popup's full Error state with Retry.
- Subscription lookup failure must not block Chat context inspection or compaction.
- Loading and retry must not close a subscription-usage popup.

## Server Environment

- Kent must load `.env` from its server persistence root at startup, including when installed as a service. The default path must be `~/.kent/.env`. Kent must not load repository `.env` files for this purpose.
- A present process environment value must take precedence over the file, including an empty value. Kent must require restart to observe file edits or changes to its launch environment.
- The file must be operator-managed. On Unix, the file must be owner-only. On Windows, Kent must use inherited filesystem permissions. Onboarding and `/login` must not write its secrets.
- The file must supply server environment values without injecting them into agent shell environments.
- Agent subprocess environments must exclude explicitly referenced provider-key variables regardless of which server environment source supplied their values.
- If the file is missing, server startup must create an empty file with owner-only permissions on Unix and inherited filesystem permissions on Windows. Kent must leave existing files, contents, and permissions unchanged. Empty files must be allowed. Non-regular, unreadable, or malformed files must fail server startup with safe diagnostics. On Unix, insecure-permission files must also fail server startup with safe diagnostics. Kent must use standard dotenv syntax. Malformed-line reporting is optional.
- Missing-variable guidance must identify the server-side file path and restart requirement without revealing secrets.

## Session Binding

- Kent must persist a Session's resolved connection ID. Ordinary resume and direct continuation must use that binding unchanged except under Automatic usage fallback.
- Existing-Session settings reads and non-Agent changes must use that binding even when current Agent/default connection references are unavailable. An explicit Agent change must validate the target Agent's current connection and leave the Session unchanged when it is unavailable.
- When the user explicitly changes the Agent in an unlocked Session through Chat Settings, Kent must select and persist the chosen Agent's current connection. Locked Sessions must not change their Agent or connection.
- Workflow Compact-and-Continue reusing the same Session ID must use its saved connection for compaction and target-role activation without changing assignment order except under Automatic usage fallback. A child with a new Session ID must select a fresh connection, following the context-preservation and failure/interruption boundaries in [Workflow orchestration](workflow-orchestration.md).
- If the saved ID is absent from connection definitions, Kent must resolve the replacement from the Session's current role or the unroled default, persist it, and show a notice naming the old and new IDs before the next request. Confirmation must not be required.
- Sessions without a saved ID must bind from their current role/default on first resume. Binding must not change their established model/request contract.
- Invalid or missing replacement references must block dispatch. Expired credentials and network/provider failures must not cause automatic replacement except for confirmed exhaustion under Automatic usage fallback.
- Manual ID renaming may require re-authentication. Kent need not coordinate renaming.
- Existing Sessions that change connections must follow [Context compatibility](#context-compatibility).

## Context compatibility

- When Kent adds reasoning or a native compaction checkpoint to context, it must attribute the item to its producing Reasoning Type. Connection IDs and model names must not determine Reasoning Type.
- The selected Provider Connection's transport must determine the destination Reasoning Type. Capability overrides and saved Session Contracts must not change that type.
- When an existing Session changes connection, Kent must omit encrypted reasoning in context that has a Reasoning Type incompatible with the destination or that the destination explicitly cannot consume. This is a narrow exception to the prohibition on filtering already-sent history.
- Omission must apply to the entire incompatible reasoning item, including its readable summary. Its historical Reasoning Trace must be unchanged. Kent must not create substitute summary messages.
- Outgoing context must include unencrypted reasoning when the destination can represent it. Other active messages and complete tool-call/result relationships must be unchanged.
- Persisted historical entries, Session identity, and Workflow associations must be unchanged. Reasoning omission must not create a history replacement or require a recovery command or confirmation.
- When Kent omits incompatible reasoning, Kent must append a transcript warning with the exact text: "Conversation was switched to another provider/model - reasoning was lost, degrading output quality". Kent must continue the original pending work automatically.
- Kent must never omit the active native compaction checkpoint. Known incompatibility with its encrypted format must block dispatch with an explanation that the Session cannot continue with that encrypted compaction summary and instructions to restore a compatible connection.
- Compatibility checks must not affect local compaction summaries. Encrypted checkpoints outside the latest active context must not affect compatibility checks.
- For context items without attribution, Kent may infer Reasoning Type only from saved provider evidence that identifies the item's producing Agent Step and a known format. Missing or conflicting evidence must leave attribution unknown. A replacement destination or backfilled Session Contract must not establish an item's origin. Kent must not rewrite old entries to add attribution.
- When Kent adds an item whose producing format is unknown, it must record unknown attribution. Resume must not change that attribution. Kent must not relabel that item from an older Session Contract.
- Unknown compatibility, including cross-account compatibility or unavailable historical attribution, must not itself block dispatch. Kent must send such context unchanged when the destination protocol permits it and surface provider rejection. A known unsupported native checkpoint must block dispatch.
- Kent must not automatically call a provider to recover hidden context, select substitute credentials, or retry a request rejected by the provider as incompatible.
- Compatibility checks must use only the bounded active context. Kent must not read the complete historical transcript or copy the entire saved collection to omit reasoning.
- Outside this reasoning-omission exception, Kent must not strip saved context or rewrite already-sent historical items.

## Terminal Setup And Login

- When no Provider Connections are defined, headless agent launches must return an actionable setup error.
- First sign-in must be part of onboarding before provider-dependent choices. Kent must defer saving the connection, credentials, and settings until Finish. Slash commands must be unavailable before onboarding.
- Explicitly canceling setup or restarting the server before Finish must discard the unsaved sign-in. Finish must write configuration last. A failed configuration write must leave onboarding incomplete and report the failure. Unused saved credentials are acceptable.
- Kent must own one shared pending first-run connection per server. Opening another TUI must not reset it. Explicit setup changes or discard, Finish, and server restart must control its lifetime. An OAuth result arriving after explicit discard must not persist the discarded sign-in.
- First-run onboarding must implicitly select the first connection as default without confirmation.
- After onboarding, `/login` must list Add connection first and existing connections afterward. With zero connections it must enter Add directly. Config-authored connections must appear in the same flow.
- Add must save the new connection in global configuration. When no connections or global default are configured, Add must select the first added connection as the global default without confirmation. Otherwise, Add must offer an explicit Make default choice. Make default must change only the global default and explain any overriding workspace default. Existing role assignments and Session bindings must stay unchanged.
- Connection setup must group choices under OpenAI, Grok, and Generic headers, in that order. Each header must use the existing bold, full-foreground header style. Each section must use the choice order specified under Authentication.
- Connection setup must combine provider and authentication-method selection in one scrollable list. OpenAI subscription browser and device options must appear in that list without a second method picker. Grok subscription must lead directly to device authorization. Authentication must use the shared onboarding presentation and navigation.
- Suggested connection IDs must use the selected provider's name and the next available numbered ID. Grok subscription and API-key connections must share the `grok` prefix.
- Add for a subscription connection must save its definition only after successful sign-in. Failed sign-in or cancellation before the operation is accepted must leave no definition. Observer disconnect must not cancel accepted work.
- Selecting an existing subscription connection must re-authenticate without changing its ID or creating a duplicate. Selecting an API-key connection must show and allow replacement of its environment-variable reference, never request the secret. Selecting an auth-less connection must explain that sign-in is unnecessary without editing its endpoint.
- The API-key field must show: "Don't paste your API key here. This is the name of the **environment variable** Kent will read **at the server's location** to get the api key from. Alternatively, place it in a ~/.kent/.env file."
- Connection setup and management requests must show a full-screen animated spinner without loading labels while waiting for a response, except during device authorization. Device authorization must keep its verification instructions and code visible with a waiting indicator until authorization completes or the user navigates back.
- The server must validate connection definitions and references. Terminal forms must not define separate validation rules.
- `/login` and `/logout` must both appear in terminal slash-command suggestions for every authentication type.
- `/logout` must list existing Provider Connections without an Add choice. Opening the list or selecting a connection must not delete anything.
- After selection, `/logout` must ask for confirmation naming the connection and explain that Sessions using it lose access. When a Session reopens, Kent must switch it to another available connection when its configuration permits. Otherwise, its next request is interrupted.
- The choices must be Yes and No, with No selected by default. Choosing No or canceling confirmation must return to the connection list. Canceling the list must return to chat.
- While a removal-page read or removal write is pending, navigation must not close the page. Ctrl+C must keep its global interrupt-or-exit behavior without returning to chat during a pending request.
- Confirmed removal must delete the selected connection's global definition and saved OAuth credentials, including saved account identity. Removal must leave other connections, configuration references, environment secrets, and Session history unchanged. Removal must not revoke access at the provider.
- Removal must be allowed during execution. It must not cancel or replay already-sent requests. Subsequent requests must follow the existing deleted-connection rules under Session Binding.
- Removal must clear saved credentials before removing the definition. If either write fails, Kent must report the failure and whether credentials were already cleared in a status notice, without reporting success or replacing the confirmation page. The user must be able to retry by repeating the confirmation action. Removal must not use cross-file rollback or automatic retry. If the connection is already removed and its credentials are absent, removal must succeed.
- When a connection-list read fails, Kent must show a full-page error with an explicit retry that reloads all reads for the page. Retrying a read must not repeat a successful removal.
- If the refreshed list is nonempty, successful removal must return to that list. After removing the last connection, Kent must immediately close the removal page and enter the existing login/setup flow without stopping server-owned in-flight work.

## Configuration Cutover

- Kent must automatically convert provider-access settings in global configuration, including globally declared roles and Supervisor, into connection definitions and references. Identical access settings must share a connection.
- Conversion must not change model, role, tool, endpoint, capability, or unrelated user-authored settings. Workspace/shared/private provider-access settings must require manual edits with actionable diagnostics.
- Automatic conversion and connection edits must leave untouched configuration source exactly unchanged, including comments, spacing, ordering, quoting, and line endings. Kent may change edited settings and the syntax required to insert or remove settings. Kent may format inserted content.
- When Kent removes a setting, comments attached to that setting may disappear. When Kent removes a table, comments within that table may disappear. When a deletion reaches the end of the file, empty separator lines immediately before the deleted content may disappear. Kent must leave all other comments unchanged, including comments surrounding a changed setting.
- Converted ChatGPT connections must require re-authentication. Conversion may discard obsolete saved API keys.
- Kent must report changed and failed files, write individual files atomically, and stop affected work on ambiguous mappings or write failures. Cross-file rollback is not required.
- Kent must remove obsolete provider-selection settings and readers after conversion. Permanent parallel authorities and fallback readers are prohibited.
