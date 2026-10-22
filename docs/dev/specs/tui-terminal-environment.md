## Resize

- In Ongoing Mode, a width change that cannot use terminal-native repaint starts Scratch Rehydration after a one-second debounce.
- Bounded Alternate Screen surfaces relayout immediately for each resize. They do not use Scratch Rehydration.
- Resize never loses state: selections, scroll positions (clamped to new bounds), input drafts, and in-flight operations all survive a resize on every surface.

## Size degradation

- Below 40 columns or 4 rows, Ongoing Mode must leave its owned frame blank without changing Scrollback and hold transcript events until the terminal reaches both dimensions.
- Below 40 columns or 10 rows, other TUI surfaces must leave their owned frame blank without changing Scrollback.
- Rendering resumes when the terminal reaches the minimum size.
- At or above its absolute minimum, Ongoing Mode must apply the current-content live-area minimum defined in [Ongoing Scrollback Buffer](ongoing-scrollback-buffer.md#mutable-band-height). Hiding the live area must not stop committed output.
- At or above the minimum, every surface must be usable. Content that exceeds the height can scroll. Every unbounded list uses Infinite Scroll and never loads the complete list. Horizontal content wraps without overflowing or disappearing off-screen. The status line follows its priority ladder.

## Theme and color

- Theme setting is `dark`, `light`, or auto (default). Auto resolves through terminal background detection at startup and preselects the onboarding theme.
- All colors come from the shared theme palette's role system (primary/secondary/muted/status roles). Surfaces never hardcode colors. Palette colors are adaptive to the resolved theme.
- Theme resolution happens once at startup. There is no live re-detection mid-session.

## Terminal control modes

- Ongoing Mode must not use Alternate Scroll (`?1007`).
- Alternate Screen surfaces enable Alternate Scroll while active and disable it on exit. The rollback picker is the exception.
- No surface enables Mouse Capture.
- Kent must release modified-key reporting while an external editor owns the terminal. When the editor returns, Kent must restore that reporting after the terminal returns to Kent, whether the editor succeeds or fails. Kent must restore ordinary terminal input on exit.

## Native progress

- When `tui_native_progress_bar` is enabled, the interactive TUI emits the OSC progress sequence (OSC 9, subcommand 4) as a best-effort terminal-native progress signal.
- `tui_native_progress_bar` is a global or workspace config-file boolean, defaults to `true`, and has no environment override or command-line flag.
- Kent must not identify terminal brands or probe for native progress support. An unsupported terminal may ignore the signal.
- Disabling native progress suppresses only the OSC progress sequence. It must not change in-TUI spinners, loading text, status labels, or the context meter.
- Compaction, Reviewer activity while `invoking`, Detail Mode transcript page loading, Worktree creation including its setup script, and Worktree deletion activate native progress.
- The main Agent Turn, Reviewer activity while `addressing_feedback`, Worktree list refresh, Worktree target lookup, Worktree switch scheduling, Session listing, Project loading, Session opening, workspace retargeting, startup surfaces, and onboarding do not activate native progress.
- The eligible operations listed above use indeterminate native progress because they expose no trustworthy completion fraction.
- Kent may emit numeric native progress only when the operation supplies an authoritative percentage. Kent must not derive a percentage from elapsed time or arbitrary phase weights.
- Kent waits 500 milliseconds after aggregate eligible activity begins before it emits native progress.
- If all eligible activity ends during the delay, Kent emits no native progress signal.
- Once visible, native progress is continuously active while at least one eligible operation is active.
- While another eligible operation is active, starting or ending an overlapping eligible operation must not restart the delay, flash, or clear native progress.
- Success, failure, interruption, cancellation, Session transition, and TUI exit clear native progress immediately unless another eligible operation is active.
- This feature must not emit the OSC progress error state.
- Headless commands, Workflow commands, JSON output, redirected output, Desktop, and the server do not emit native progress.
- The interactive TUI clears native progress best-effort whenever it exits.

## Markdown hyperlinks

- Kent emits OSC 8 for valid Markdown link destinations on every terminal.
- Exact terminal whitelisting controls only destination visibility. Ghostty and libghostty embedders (`TERM_PROGRAM=ghostty`), kitty (`KITTY_WINDOW_ID` or `TERM=xterm-kitty`), iTerm2, WezTerm, Alacritty, Windows Terminal, VS Code-compatible terminals, and Zed render an explicit Markdown link as its clickable label. Version strings do not participate in detection.
- Terminal.app, terminals outside the whitelist, and sessions inside tmux, Zellij, or GNU screen render an explicit Markdown link as clickable `label destination`. Unsupported terminals ignore OSC 8 and show the visible destination.
- The Markdown library owns GFM table layout. Explicit links inside table cells render clickable `label destination` on every terminal, independent of the terminal whitelist.
- Autolinks render the destination once in both modes.

## Patch file hyperlinks

- Kent must emit OSC 8 on every terminal for a patch path that identifies a syntactically absolute local path. Patch file hyperlinks must work on every operating system supported by the TUI.
- Kent must resolve a relative patch path against the working directory used for that patch before deriving its destination. A compact row must continue to display the workspace-relative label while its destination uses the resolved absolute local-file URI. A Detail header must display the available absolute path and use the same destination.
- Local-file URIs must use forward slashes on every operating system, including for Windows drive paths.
- Only displayed path characters may be part of the hyperlink. Counts, the Detail selection rail, trailing whitespace, and truncation markers must be outside it, including when a path wraps. Every displayed path fragment must be linked across wrapping and truncation.
- Unsupported terminals must ignore OSC 8 and show the visible path without additional destination text. Added, deleted, unsuccessful, and nonexistent files must be linked when their paths identify absolute local paths. Moved files must link to their destination paths. Patch links must use their ordinary styling. When context cannot identify an absolute path or file rows cannot be identified, Kent must show the visible path or content unlinked.
