## Surface and lifecycle

- The active prompt appears in the Ongoing Mode Mutable Band and takes input focus. If the complete rendered prompt would lose any row in the final Mutable Band allocation, Kent must show it on a dedicated Alternate Screen.
- A collapsed transcript entry for a Question shows only the Question.
- The active question is Markdown-rendered and wraps within its surface. Inline answer options take viewport priority. Inline question lines use the available capacity after options and follow the live region's collapse/truncation behavior. Pure freeform prompts use cursor-anchored viewport behavior.
- The dedicated surface must show the complete active prompt as one scrollable document. It must use the inline prompt presentation without a separate title, transcript, tools, or unrelated Mutable Band content.
- Each dedicated prompt must open at the beginning of its Markdown question in reading mode, with no option selected and no editor cursor.
- `Up`/`Down`, including alternate-scroll wheel input, must form one continuous vertical interaction. They move through focused options or editor rows before entering document reading at the boundary. Moving above the first option must clear visible selection and scroll upward.
- In reading mode, downward movement must reach the document bottom before one additional downward step focuses the first option or editor. `Enter` must submit only a focused answer.
- Typing, paste, and freeform editing or cursor movement other than `Up`/`Down` and `PgUp`/`PgDn` must focus the editor, jump to the document bottom, discard the reading position, and apply the operation. `Enter` must do nothing while the editor cursor is hidden.
- While reading an ordinary Question, `Tab` must focus freeform input. While reading an Approval, `Tab` must do nothing until a decision has been chosen. Returning from freeform entered through reading must restore reading with no visible option selection.
- `PgUp`/`PgDn` must enter reading mode and scroll by a viewport page. They must clear visible answer focus. After scrolling reaches the bottom, one additional downward step must focus the first option or editor.
- Approval commentary must stay associated with its chosen decision and draft while reading hides answer focus. Returning to first-option focus after page reading must not reassign that commentary.
- Dedicated rendering must follow the universal size rule in `tui-terminal-environment.md`, including its blank owned frame below minimum size. This rule takes precedence over prompt-specific fallback content.
- The dedicated surface must use the shared status line with scrolling guidance in its help slot. General help must be unavailable there. Where the universal size rule permits rendering, if the terminal cannot fit one framed prompt-content row plus status, the surface must show only a resize instruction, omit status and answer cursor, and leave the prompt pending.
- Promotion must be one-way for the active FIFO group. After promotion, later queued prompts must use the same surface until the group closes, even when resize, refresh, or a later prompt would fit inline. The final close must restore the untouched Ongoing Mode normal-buffer transcript.
- Prompts use FIFO order and show one active prompt at a time. Resolving the active prompt shows the next one.
- A prompt resolved from another attached client disappears from the active view or pending list.
- An update to the active prompt refreshes it in place without replacing the user's selection or draft. On the dedicated surface, reading/answer focus and scroll position must stay unchanged where possible, with scroll clamped to the refreshed content. Refresh must not ring the bell or add a banner.
- A prompt received behind another full-screen destination must stay deferred until the user returns to Ongoing Mode. Admission, queued activation, and resolution must leave that destination's visible state, input mode, and key routing unchanged.
- The terminal bell rings when a new prompt appears.
- Submitting an answer fixes the submitted content for that attempt. The editor accepts edits while Kent delivers it. Further edits affect only a later retry if delivery fails.
- The dedicated surface must stay active during delivery and reconciliation and close only when no active or queued prompt exists.
- Allow commentary is part of the server-owned Approval action. The TUI sends no preceding Queue or Steer request and applies no Approval-specific input freeze.
- Repeated `Enter` during delivery must show a brief nonblocking sending notice without submitting another answer.
- After a terminal delivery failure, the operator can retry using the prompt, selection, and edited retry draft.
- Resolving the prompt discards the retry draft.
- Kent must not assign a client request identity, replay, or automatically retry a failed answer delivery. A later explicit Approval submission uses the same Tool Call ID and is actionable only while that tool call is waiting for Approval.
- Missing answer transport or invalid prompt identity must leave the prompt pending and show an actionable error. Only server-confirmed resolution may advance the FIFO group or restore the composer.
- After the last prompt resolves, focus returns to the main composer and activity returns to running/idle. A focused non-ask destination must keep its input mode and key routing.
- The status-line spinner pauses while a prompt waits.

## Prompt kinds

- **Question with options**: numbered options plus an appended "Freeform answer" option. When an inline Question becomes active, its valid recommended option is selected. Without a recommendation, option 1 is selected. A recommended option is marked (star + recommended suffix) and the selection marker is distinct from the recommendation marker. Exact glyphs are outside the contract. Initial dedicated reading has no selection.
- **Pure freeform question** (no options): goes straight to text entry inline and starts in reading mode on the dedicated surface.
- **Approval prompt**: options carry Approval decisions and have no freeform-answer option. Inline, the one-time allow decision is selected when offered. Otherwise the first offered decision is selected. Initial dedicated reading has no selection. Optional commentary attaches to the chosen decision. Commentary behavior follows the shared Approval contract in `core-runtime-tools.md`.
- An outside-workspace Approval begins with `Agent wants to access a batch of files, but <count> are outside workspace dir:`. It then shows one Markdown bullet for each distinct path string supplied by the model, in first-attempt order, followed by `Allow this access?`. A repeated identical supplied path appears once. Different aliases use separate bullets even when they resolve to one target. Each bullet shows the path supplied by the model. When that path resolves through a symlink or alias to a different real path, the same bullet also shows the real resolved path with a right arrow.

## Keys

- `Up`/`Down` move the option cursor while answer focus is active. Dedicated prompts use the document-boundary interaction above. `Enter` submits the focused selection (option number, approval decision, or freeform text).
- `Tab` toggles between the option picker and freeform text entry. For Approvals, freeform is commentary for the selected decision. Toggling back shows the typed draft dimmed below the options for a non-Approval prompt without changing it.
- Freeform entry uses the same editing and clipboard behavior as the main composer.
- Choosing the "Freeform answer" option with empty text enters freeform mode. Submitting it with empty text is rejected with an error notice + bell.
- `Esc` cancels any active answer delivery, then cancels the prompt so the agent receives a typed cancellation. `Ctrl+C` cancels only the client-side answer delivery and directly interrupts the prompt's exact Agent execution. The server interruption closes the pending prompt through the ordinary interrupted execution outcome. The TUI must not submit a prompt cancellation before Interrupt, wait for canceled answer delivery, or show that delivery's cancellation or deadline as an error.
- There is no digit-jump on prompt options.
