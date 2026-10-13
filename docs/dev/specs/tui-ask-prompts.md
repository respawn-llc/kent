## Surface and lifecycle

- The active prompt appears in the Ongoing Mode Mutable Band and takes input focus. It must not open an Alternate Screen.
- A collapsed transcript entry for a Question shows only the Question.
- The active question is Markdown-rendered and wraps within the live region. Answer options take viewport priority. Question lines use the available capacity after options and follow the live region's collapse/truncation behavior. Pure freeform prompts use cursor-anchored viewport behavior.
- Prompts use FIFO order and show one active prompt at a time. Resolving the active prompt shows the next one.
- A prompt resolved from another attached client disappears from the active view or pending list.
- An update to the active prompt refreshes it in place without replacing the user's selection or draft.
- The terminal bell rings when a new prompt appears.
- Submitting an answer fixes the submitted content for that attempt. The editor accepts edits while Kent delivers it. Further edits affect only a later retry if delivery fails.
- Allow commentary is part of the server-owned Approval action. The TUI sends no preceding Queue or Steer request and applies no Approval-specific input freeze.
- Repeated `Enter` during delivery must show a brief nonblocking sending notice without submitting another answer.
- After a terminal delivery failure, the operator can retry using the prompt, selection, and edited retry draft.
- Resolving the prompt discards the retry draft.
- Kent must not assign a client request identity, replay, or automatically retry a failed answer delivery. A later explicit Approval submission uses the same Tool Call ID and is actionable only while that tool call is waiting for Approval.
- After the last prompt resolves, focus returns to the main composer and activity returns to running/idle.
- The status-line spinner pauses while a prompt waits.

## Prompt kinds

- **Question with options**: numbered options plus an appended "Freeform answer" option. When the Question becomes active, its valid recommended option is selected. Without a recommendation, option 1 is selected. A recommended option is marked (star + recommended suffix) and the selection marker is distinct from the recommendation marker. Exact glyphs are outside the contract.
- **Pure freeform question** (no options): goes straight to text entry.
- **Approval prompt**: options carry Approval decisions and have no freeform-answer option. The one-time allow decision is selected when offered. Otherwise the first offered decision is selected. Optional commentary attaches to the chosen decision. Commentary behavior follows the shared Approval contract in `core-runtime-tools.md`.
- An outside-workspace Approval begins with `Agent wants to access a batch of files, but <count> are outside workspace dir:`. It then shows one Markdown bullet for each distinct path string supplied by the model, in first-attempt order, followed by `Allow this access?`. A repeated identical supplied path appears once. Different aliases use separate bullets even when they resolve to one target. Each bullet shows the path supplied by the model. When that path resolves through a symlink or alias to a different real path, the same bullet also shows the real resolved path with a right arrow.

## Keys

- `Up`/`Down` move the option cursor. `Enter` submits the selection (option number, approval decision, or freeform text).
- `Tab` toggles between the option picker and freeform text entry. For Approvals, freeform is commentary for the selected decision. Toggling back shows the typed draft dimmed below the options for a non-Approval prompt without changing it.
- Freeform entry uses the same editing and clipboard behavior as the main composer.
- Choosing the "Freeform answer" option with empty text enters freeform mode. Submitting it with empty text is rejected with an error notice + bell.
- `Esc` cancels any active answer delivery, then cancels the prompt so the agent receives a typed cancellation. `Ctrl+C` cancels only the client-side answer delivery and directly interrupts the prompt's exact Agent execution. The server interruption closes the pending prompt through the ordinary interrupted execution outcome. The TUI must not submit a prompt cancellation before Interrupt, wait for canceled answer delivery, or show that delivery's cancellation or deadline as an error.
- There is no digit-jump on prompt options.
