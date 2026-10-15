## Startup surfaces

- Startup surfaces use the Alternate Screen and leave it before Ongoing Mode begins. Ongoing transcript history therefore enters the Normal Buffer and terminal Scrollback.
- Startup surfaces enable Alternate Scroll while active and disable it on every exit path.
- Startup surfaces never enable Mouse Capture.
- Text fields use the native terminal cursor.
- Every asynchronous gate appears immediately with a loading state. Startup never leaves the terminal blank or frozen while it waits.
- The authentication picker and Session picker use the same status-line treatment for operation errors and notices. This also applies when `/resume` reopens the Session picker.
- Startup remembers the previous visible surface. `Esc` returns to it unless the active surface defines another key behavior.
- Bind Workspace must align its heading, notice, and choices at the top-left of the surface.

## Startup sequence

Kent must process the following gates in order. It must skip a gate only when its condition is inapplicable. Flags must not bypass gates.

1. **Server attach.** The TUI is a client and contains no server. It attaches to the configured endpoint. Explicit `server_host` and `server_port` values are authoritative. A connection failure exits with the endpoint and reason. Kent provides no embedded-server fallback or retry screen.
2. **First-time setup.** A missing `config.toml` must open setup, including first sign-in, before Session selection.
3. **Workspace resolution.** Startup begins from the current workspace. An unregistered current directory opens Project binding and is not registered automatically.
4. **Session selection.** If no Sessions exist, startup opens new-Session setup directly.
5. **Connection readiness.** Session listing must open authentication or connection setup automatically only when no configured connection is usable. Auth-less connections are usable. A broken default connection must not prevent Session selection or use of another working connection.
6. **Workspace-change prompt.** This prompt appears when the selected Session's available workspace root differs from the current root.
7. **Lazy open and handoff.** Kent presents New Session before creating a Session. A nonblank message, Queue, recognized prompt command, Goal Set, manual compaction, confirmed Worktree creation, or user-shell submission requires ordinary Session creation.

- Opening or abandoning an empty New Session, repeated `/new`, and picker navigation must not assign a Provider Connection or change the global connection assignment order. The first Agent-Turn-starting action must claim exactly one connection through ordinary Session creation.
- `Esc` on a startup surface navigates back one gate where a previous gate exists. On the first visible surface it exits the TUI cleanly. The session picker is an explicit exception: its `Esc` key is a no-op.

## New Session

- Opening, typing in, changing settings in, abandoning, or reopening New Session must create neither a durable Session nor a Session catalog row.
- The open New Session must retain its selected workspace, configuration inputs, settings, optional name, and optional Worktree target only in client memory until creation.
- New Session must read settings defaults and choices from the server. Thinking, Fast, Supervisor, Questions, and Auto-compaction edits must remain temporary before creation. Creation must receive the selected settings. After creation, edits must use ordinary server-owned Session settings mutations.
- New Session must show its editable composer immediately and resolve selected-role settings and connection readiness in the background. If the selected connection requires authentication, Kent must show a notice directing the user to `/login` without automatically opening authentication. This applies both before first sign-in on that connection and after authentication is lost. Returning from login must not resend an earlier action.
- For connection sets, New Session readiness must be advisory. Settings reads must not allocate or reserve a connection. The explicit first action must allocate and validate its actual connection through ordinary server selection without requiring every member to be ready. A delivered Session identity and its ordinary draft must remain available after failure.
- While settings load or their read has failed, editing and navigation must remain usable. Initial loading must keep creation-triggering actions unavailable until settings are ready. Loading must appear immediately. Read failure must show an error. Enter after a read failure must retry the read and send the explicitly requested input when that read succeeds. Kent must not guess defaults or retry an already-issued creation or action without another user action.
- Each successful explicit creation-triggering request must create one ordinary Session and invoke its ordinary action once. Concurrent requests issued before a Session result arrives may create separate Sessions.
- Each delivered Session result must adopt that Session in the same composer. Visible unsent text must follow the delivered Session. A result from a New Session the user has left must not navigate the current view.
- If a delivered result identifies a created Session but later activation or action fails, the TUI must keep that Session and its ordinary server-owned draft. If no Session identity was delivered, the TUI must keep New Session without searching for or inferring the result.
- Submission failure must use the restoration behavior in [TUI Chat Core](tui-chat-core.md#queueing-and-steering). Goal Set must leave composer text unchanged. Kent must not automatically retry, replay, or reconcile failed or disconnected creation or actions.
- Navigation or exit must discard uncreated New Session text, name, settings, and Worktree selection without confirmation. `/new` must navigate to a fresh Chat presentation, dispose the prior presentation's in-memory state, and recreate it with ordinary startup defaults.
- Ordinary TUI `/new` must retain only the originating Session's navigation relationship. It must use the chosen Project and workspace's fresh defaults and explicit temporary selections without inheriting the originating Session's model, provider setup, ancestry, or Worktree. Rollback and agent-child inheritance must remain unchanged.
- Existing Session drafts must remain server-owned and retain their ordinary save and restore behavior.
- Pre-Session controls must follow [TUI Slash Overlays](tui-slash-overlays.md#pre-session-controls).

## Auth gate

- The picker and credential selection must follow [Provider Connections](provider-connections.md). Onboarding must own first-run sign-in, and slash commands must be unavailable before onboarding completes.
- Successful auth shows a brief confirmation state on the auth surface before advancing to the next gate.
- While the authentication surface is open, authentication failures and 401 responses must appear there with retry or method-selection actions. Outside that surface, missing or lost authentication must show a notice directing the user to `/login` without an automatic redirect or silent exit.
- Automatic authentication must remain limited to first-time onboarding and Session listing with no usable configured connections. Usability must come from server readiness without refreshing credentials or contacting providers.

## Session picker

- If exactly one Provider Connection is configured, the picker must show its provider and authentication-status presentation.
- If multiple Provider Connections are configured, the picker's connection indicator must show only `N connections` with the configured count.
- Provider metadata display must not refresh credentials or contact a provider.
- The picker must show an animated spinner while it loads its header settings.
- If a header settings or provider metadata read fails, the picker must show the error without blocking navigation or opening the selected Session or New Session. A failed active tab's list load must take priority for `Enter` and retry that list.
- The picker shows recent Sessions and a new-Session action. Infinite Scroll provides older Sessions. An empty state opens new-Session setup.
- The picker has `Sessions` and `Subagents` tabs and opens on `Sessions`. Ordinary interactive sessions and interactive forks start in `Sessions`. Sessions created for headless or workflow-agent execution start in `Subagents`. Every interactive open of a Subagent session, including opening by explicit session ID, permanently promotes it to `Sessions`. Picker selection alone must not promote: workspace lookup failure returns to the picker with a generic retry error in the shared status line, and declining a workspace change returns to the picker. Neither changes the session artifact, recency, or category. An accepted retarget completes before open and promotion. Automated headless/workflow resumes and renames never change category. Sessions without a recorded category appear in `Sessions`. Category is never guessed from a session's name, parent, or current activity.
- The tabs appear directly below the status header using the horizontal bracketed button-row treatment. The selected tab uses the primary bold treatment, the other tab is muted, and incremental lists do not show total counts.
- When both full labels do not fit horizontally, the same buttons stack on adjacent lines rather than clipping or changing labels.
- The picker opens immediately on `Sessions` and loads both tabs' first windows concurrently. A tab shows its own loading spinner until its first window arrives. If both first windows are empty, startup advances to new-session setup.
- The picker requests update status without delaying Session loading.
- Checking and up-to-date states add no row.
- An available update adds a persistent Success-colored `Update available: v<latest>` row below the current Kent version.
- A failed check adds a persistent Error-colored `Update check failed: <cause>` row in the same location.
- The row is visible while the picker is open and reappears on reopening while the update result is fresh.
- Only the Session picker shows update status.
- The `Kent v...` title shows the local client version. Update discovery evaluates the attached server version.
- After a tab data-load failure, that tab shows an error notice without any list rows, including stale rows. `Enter` retries the selected failed tab, and switching into a failed tab also retries. The shared startup status line separately surfaces the operation error: the active tab's outstanding failure wins, otherwise the newest outstanding tab failure is shown. Retry keeps the previous failure visible until that tab succeeds. Recovery clears only that tab, reveals another outstanding failure when present, and clears the status after both recover.
- Retry performs a fresh load from that tab's newest window and resets its old pagination position, selection, and scroll.
- Each tab must store its selected row and scroll position for the duration of the open picker. Reopening the picker starts each tab at its newest session.
- `Create a new session` appears only in `Sessions`. `n` starts a new ordinary session from either tab.
- An empty `Subagents` tab shows `No subagent sessions yet` without changing the selected tab. Tab navigation and `n` are available.
- When both tabs are empty, startup skips the picker and goes directly to new-session setup.
- Each tab requests 50 Sessions per page in recency order with infinite scroll and keeps at most two bounded pages resident. Traversal loads older pages and reloads evicted newer pages when navigating back. Neither client nor server holds or requests the full session set.
- Initial load and fresh retry replace the affected tab body. Loading an older or newer offset page keeps resident rows, selection, and viewport visible, shows a loading affordance at the requested edge, and blocks only crossing that pending edge.
- Each row shows session title and relative age.
- Keys: `Up`/`Down` and `j`/`k` move selection. `PgUp`/`PgDn` page. `Tab`/`Shift+Tab`, `Left`/`Right`, and `h`/`l` switch tabs without opening a session. `Enter` opens. `n` starts a new session. `Esc` is a no-op, `q` has no binding, and `Ctrl+C` is the sole picker exit key.
- Opening a session seeds the next main input from the session's persisted draft verbatim (byte-for-byte, including whitespace and newlines).

## Project binding flow

- An unregistered current directory offers new-Project creation first and an existing-Project picker below.
- Creating a Project attaches the current workspace as its first workspace and main worktree.
- Selecting an existing Project attaches the workspace to it.
- The existing-Project Workspace picker uses the canonical Project Workspace catalog with infinite scroll. It follows server order, shows Workspace name and path, and shows no activity timestamp.
- The Workspace picker requests 50 Workspaces per page and keeps at most two pages resident.
- When the selected Project has exactly one attached Workspace, startup advances without showing the Workspace picker.
- The Workspace picker appears immediately with a loading state.
- After a first-page load failure, the picker shows a retry notice without list rows. `Enter` retries the failed request.
- Loading or failure at an older or newer edge keeps resident rows, selection, and viewport visible. The picker shows a spinner while loading or retry text after failure at the affected edge and blocks only movement across that edge.
- The picker prefetches an adjacent page when selection enters the first or last visible screenful of the resident window.
- Up/Down and `j`/`k` move by one row. PgUp/PgDn move by one visible screenful. Row movement across a loaded boundary selects the first row of the next page or the last row of the previous page. PgUp/PgDn movement across a loaded boundary lands one visible screenful into the loaded page.
- Adjacent-page loading or failure must not move selection. Loading adds no copy beyond the spinner. The shared startup status line shows the operation failure, and the affected list edge shows retry text.
- `Enter` retries a failed first-page request. When valid resident Workspaces are visible after an adjacent-page failure, `Enter` selects the current Workspace and moving toward the failed edge retries that page automatically.
- A successful adjacent-page request with no rows leaves the resident information visible without an additional read or special status.
- A successful retry immediately clears that operation's failure cause and retry affordance, then shows the loaded rows or the normal catalog boundary.
- `Esc` returns to Project selection. Ctrl+C exits startup. The Workspace picker has no `q` binding.
- Workspace catalog loads use the shared client request lifecycle without a picker-specific timeout or automatic retry.
- An empty Workspace catalog shows `no workspace is attached to this project.` followed by `Please attach workspace before continuing.` Both lines use the normal foreground treatment without warning or error color. `Enter` reloads the catalog. `Esc` returns to Project selection.
- Returning from the Workspace picker restores the Project picker's selection and scroll. Entering a Project's Workspace picker again starts a fresh first-page load.
- Server-browsing mode can open existing Projects and workspaces but cannot create or attach them.
- Kent supplies the Project-name suggestion from the workspace. The TUI must not inspect the filesystem to derive it.
- An empty Project name shows an inline error.

## Workspace-change prompt

- If a selected Session has an available attached workspace root different from the current root, startup shows `Workspace changed`.
- `Yes` retargets the Session. `No` returns to the picker.
- A detached workspace-location record cannot trigger or supply the retarget.
- Retargeting always requires explicit user action.

## Multi-client attach

- Opening a Session that another client uses gives this client Equal Full-Control Attach. Startup has no read-only or reduced-control mode.
- When an open Session moves to another Workspace, the TUI closes its current view and automatically reopens and rehydrates the same Session in its new location.

## Errors

- Preflight server-connection failure at TUI startup must exit with the attempted endpoint and underlying reason. An unreachable-server error must explain how to run `kent serve` in a separate terminal or install the background service with `kent service install`, and link to the server setup guide. Instructions must apply to the server machine when the configured endpoint is remote. No retry affordance, no per-surface handling.
- After startup, connection loss shows one persistent `server connection lost` status-line notice across all surfaces.
- A successful request after reconnection clears the notice.
- If the TUI cannot present a coherent connection-loss state, it exits with a clear error.
- Any startup exit path (success, cancel, failure) restores the terminal best-effort: alt-screen exited, cursor restored, no residual control state. Ongoing-mode output already written to the normal buffer is permanent by design and is not restored.
- Debug mode fails fast with diagnostics on startup invariant violations. Normal operation surfaces the error and recovers or exits clearly.
