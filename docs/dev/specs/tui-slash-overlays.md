## Shared overlay conventions

- Overlays are bounded Alternate Screen surfaces. Closing one restores the transcript surface underneath.
- `Esc` and `q` close a read-only overlay. Inside a nested dialog `Esc` steps back one level (dialog → parent overlay) without closing the whole flow.
- `Ctrl+C` inside any overlay routes to the single global runtime Ctrl+C handling (interrupt while busy / exit while idle), closing the overlay surface first — overlays never swallow or reinterpret it.
- List/scroll keys are uniform: `Up`/`Down` move (plus `j`/`k` on list surfaces), `PgUp`/`PgDn` page, `Home`/`End` jump.
- Button groups are uniform: `Tab`/`←`/`→` (plus `h`/`l`) move selection, `Enter` activates, Cancel is the default selection for destructive confirms.
- While an overlay change is pending, the overlay ignores input. A failed action shows its error inline while the overlay is open. If the overlay closed, the error appears as a status notice and transcript entry.
- List selections are identity-stable: refreshes and live updates re-match the selected item by ID, never by index.

## /status

- The Auth section must include the conditional Session Connection row defined in [Provider Connections](provider-connections.md), styled consistently with its other rows. Grok subscription quota access and failures must follow that spec.
- Grok must use the same subscription plan and quota-window presentation as Codex. Kent must show applicable provider-reported plan, percentage left, and window length. Kent must show reset times only when their meaning is established. Missing facts must be omitted rather than displayed as zero. Explicit zero usage must be displayed as a full allowance. Grok-only purchased-credit balances, on-demand caps and spending, and past-period spending must not appear in this quota presentation.
- If Grok reports usage without a usable period length, Kent must show a weekly quota window.
- Grok's quota bar must fill with consumed allowance. If Grok reports Build and Chat contributions that account for total usage, Kent must show one stacked bar with Build first and Chat immediately afterward. Build must use Codex's quota-warning color selected from total shared usage. Chat must always use the secondary color. If the breakdown is missing or incomplete, Kent must show a single total-used bar with Codex's quota colors. The percentage left must come from Grok's reported total usage.

- `/status` is read-only and loads its sections progressively.
- Content is organized in sections — Session, Git, Context, Auth (account + subscription windows with usage bars and reset times), Config (override sources, supervisor, questions), Skills, AGENTS.md inspection, Warnings. Each section loads independently with a loading placeholder. Section failures become warnings listed in the Warnings section, never a blank overlay. The section set is contracted. Copy and layout are outside the contract.
- Keys: scroll only (shared conventions), no actions.
- `/status` must not show server ownership because the TUI is always a client.
- The Config section must show a tree of contributing configuration-file paths in global → shared workspace → Main Workspace private precedence order. It must omit files that did not contribute settings and show the environment and command-line override summary. It must not add a per-property source list.

## /goal

- `/goal` and `/goal show` open the overlay. `set <objective>`, `pause`, `resume`, and `clear` act directly.
- A successful Goal Set applies its committed authoritative Goal result and then shows exactly one transient warning status when the result carries a post-commit diagnostic. The diagnostic creates no transcript entry and must not change the successful Goal result.
- Overlay shows goal status, goal ID, and the objective rendered as Markdown. A no-goal state shows a hint to start one. Load errors render inline in the overlay.
- Setting an objective while a goal is active opens a Replace confirmation (current vs new objective). Clearing an active goal opens a Clear confirmation. Both use a Cancel/Confirm button group (Cancel default) with `y`/`n` shortcuts. Confirming issues the mutation and closes the confirm state. Paused goals clear without confirmation.
- Goal changes for an Active Session Runtime enter Steering as distinct typed intents in acceptance order. When a follow-up Goal mutation is pending locally, the TUI starts its RPC only after the preceding committed Goal result and transient warning settle.

## /ps

- The overlay lists background processes with their state, command, and working directory. It refreshes while open and when process state changes. It has no manual-refresh key.
- Actions on the selected process: `Enter`/`i` paste the process's recent output into the composer (appending below the composer draft) and close the overlay. `k` sends terminate and refreshes the list. `o` opens the log file via the system opener, falling back to `$VISUAL`/`$EDITOR`. Each action is single-flight. Results and failures surface as status notices.
- The paste action is draft-safe: if the composer draft changed between request and response, the stale output is discarded rather than spliced into the newer draft.
- With no processes, actions produce a "nothing selected" notice. The overlay stays open.

## /worktree

- `/worktree` uses the Worktree Management behavior defined in `tui-transcript.md`, including target resolution, deletion safety, aliases, setup, and branch cleanup.
- List phase: first row is the create-new entry (`Enter` on it opens the create dialog, as do `c`/`n`). `Enter` on a worktree row switches to it (`Already current worktree` notice on the current row). `d` opens delete confirmation, `x` opens it with the delete-branch action preselected. `r` refreshes. Main-workspace rows reject deletion with an error notice.
- Create dialog: `Tab`/`Down` and `Shift+Tab`/`Up` cycle fields. `Enter` on a text field advances focus. `Enter` on the action row submits (or cancels). Typed `Branch or ref` input resolves asynchronously with a short debounce, never per-keystroke blocking. `Esc` returns to the list.
- Delete confirmation: shows a preview of what will be removed with warnings. Button group is Cancel / Delete / Delete + Branch (the branch action is present for every branch-backed worktree). `Esc` returns to the list.

## Rollback picker

- Esc-Esc on empty idle input opens the picker. Candidates are user messages with rollback targets. The newest candidate is selected first.
- The picker uses Detail Mode rendering on an Alternate Screen. It must not enable Alternate Scroll and ignores mouse events.
- The selected candidate is highlighted and centered in the transcript. `Up`/`Down` move between candidates. At the window edge they page across transcript windows through bounded requests, re-anchoring the selection.
- Candidate-free pages encountered during edge navigation are traversed automatically through sequential bounded reads without storing them beyond that traversal. Each navigation attempt has a 20-second deadline. Timeout stops pagination, keeps the current candidate selected, and surfaces an error in the status line.
- `Enter` creates and opens a rollback fork at the selected message. The fork contains history before the selected user message and excludes that message.
- The rollback fork inherits the parent Session's complete Goal unchanged, including its objective, state, identity, and original creation and update times.
- The fork opens in Ongoing Mode with the selected user message text restored as unsent composer input.
- `Esc` while selecting closes the picker and restores the prior Transcript Mode.
- Rollback never changes the original Session transcript.
- If the transcript has no rollback candidates, arming does nothing visible.
