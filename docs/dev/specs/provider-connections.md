# Provider Connections

## Ownership And Selection

- Kent must own Provider Connection definitions in global server configuration. Workspace configuration must not define connections.
- A connection must own provider implementation, endpoint, authentication selection, and connection/protocol capabilities. Model, thinking, model-specific capabilities, and context/compaction policy must remain agent or role settings.
- Each connection must have one user-chosen configuration ID. IDs must begin with a lowercase ASCII letter and contain only lowercase ASCII letters, digits, hyphens, and underscores. Kent must reject duplicate IDs.
- Creation must suggest an editable unused numbered provider-based ID. Connections must not require a separate display name or hidden identity.
- Top-level `connection` must select the default for new unroled interactive Sessions. Roles must inherit that selection unless overridden. Main Workspace private configuration may assign developer-specific connection IDs to shared roles.
- Main agents, child roles, Supervisor, and Workflow must use the same connection-selection rules.
- Credentials must remain server-owned. Requests must use only the selected connection's credentials.
- Setup completion must remain server-wide. Credential readiness must be checked for the actual Session/role connection, not as a default-connection gate before Session selection. A broken default must not block working connections. Interactive credential failures must open the affected connection's authentication flow; headless failures must return actionable errors.

## Authentication

- Kent must offer ChatGPT subscription, API-key Responses-compatible, and auth-less Responses-compatible setup choices.
- The two Responses-compatible choices must share one connection model with an optional environment-variable reference. Absence must select auth-less access. A configured reference whose value is missing or empty must fail with an actionable error, not select anonymous access.
- API-key setup must accept an endpoint prefilled with the OpenAI endpoint and an environment-variable name. Auth-less setup must require an endpoint.
- Kent must not persist API keys in configuration or the OAuth credential store. API-key authentication must read the explicitly referenced server environment value.
- Kent must not automatically adopt `OPENAI_API_KEY` or borrow another connection's credentials. Auth-less requests must send no authentication credentials.
- ChatGPT must retain browser and device sign-in, including browser callback or pasted callback URL/code. OAuth failure must not fall back to an API key. Refresh failures must remain observable and actionable.
- Re-authentication must be allowed during execution. Already-sent requests must continue; subsequent requests must use the newly saved credentials. Re-authentication must not cancel, replay, or reroute work or change unrelated connections.

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
- When the user explicitly changes the Agent in an unlocked Session through Chat Settings, Kent must select and persist the chosen Agent's current connection. Locked Sessions must retain their existing Agent and connection.
- Workflow Compact-and-Continue must use the outgoing connection for compaction and apply the target role's current connection only after successful compaction, following the fresh-contract and failure/interruption boundaries in [Workflow orchestration](workflow-orchestration.md).
- If the saved ID no longer exists, Kent must resolve the replacement from the Session's current role or the unroled default, persist it, and show a notice naming the old and new IDs before the next request. Confirmation must not be required.
- Sessions without a saved ID must bind from their current role/default on first resume. Kent must preserve their established model/request contract.
- Invalid or missing replacement references must block dispatch. Expired credentials and network/provider failures must not cause automatic replacement.
- Manual ID renaming may require re-authentication. Kent need not coordinate renaming.
- Known incompatibility between the selected connection's protocol and retained encrypted context must fail before dispatch with an explanation and instructions to restore a compatible connection.
- Unknown cross-account compatibility must not itself block dispatch. Kent must send unchanged history and surface provider rejection. Kent must not strip context or rewrite historical items.

## Terminal Setup And Login

- First sign-in must be part of onboarding before provider-dependent choices. Kent must defer saving the connection, credentials, and settings until Finish. Slash commands must be unavailable before onboarding.
- Canceling or restarting before Finish must discard the unsaved sign-in. Finish must write configuration last. A failed configuration write must leave onboarding incomplete and report the failure; unused saved credentials are acceptable.
- First-run onboarding must implicitly select the first connection as default without confirmation.
- After onboarding, `/login` must list Add connection first and existing connections afterward. With zero connections it must enter Add directly. Config-authored connections must appear in the same flow.
- Add must save the new connection in global configuration and offer an explicit Make default choice. Make default must change only the global default, explain any overriding workspace default, and preserve existing role assignments and Session bindings.
- Selecting an existing ChatGPT connection must re-authenticate without changing its ID or creating a duplicate. Selecting an API-key connection must show and allow replacement of its environment-variable reference, never request the secret. Selecting an auth-less connection must explain that sign-in is unnecessary without editing its endpoint.
- The API-key field must show: "Don't paste your API key here. This is the name of the **environment variable** Kent will read **at the server's location** to get the api key from."
- `/logout` must open the same picker without deleting credentials.

## Configuration Cutover

- Kent must automatically convert provider-access settings in global configuration, including globally declared roles and Supervisor, into connection definitions and references. Identical access settings must share a connection.
- Conversion must preserve model, role, tool, endpoint, capability, and unrelated user-authored settings. Workspace/shared/private provider-access settings must require manual edits with actionable diagnostics.
- Automatic conversion and connection edits must preserve unrelated setting values. They may reformat the file and discard comments.
- Converted ChatGPT connections must require re-authentication. Obsolete saved API keys need not be preserved.
- Kent must report changed and failed files, write individual files atomically, and stop affected work on ambiguous mappings or write failures. Cross-file rollback is not required.
- Kent must remove obsolete provider-selection settings and readers after conversion. It must not retain permanent old/new authorities or fallback readers.
