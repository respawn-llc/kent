## Recon

### Existing ownership and boundaries

- Provider streams are consumed in [`server/llm/openai_http.go`](../../server/llm/openai_http.go) by `consumeResponsesStream`, which iterates provider events and passes them to `responseStreamAccumulator.Consume` in [`server/llm/openai_http_stream.go`](../../server/llm/openai_http_stream.go). `response.reasoning_summary_text.delta` invokes the reasoning callback; assistant output events invoke the assistant callback. The Grok CLI-proxy uses the shared Responses stream path, selected in [`server/llm/provider_factory.go`](../../server/llm/provider_factory.go).
- Runtime callbacks are wired in `server/runtime/engine.go` and steer assistant and reasoning deltas through the runtime output queue. The transcript subscription projection in [`server/runtimeview/transcript_subscription.go`](../../server/runtimeview/transcript_subscription.go) maps runtime assistant and reasoning events to `AssistantDelta` and `ReasoningTraceUpdate` messages.
- Terminal ongoing presentation handles assistant deltas in [`cli/tui/ongoing/surface.go`](../../cli/tui/ongoing/surface.go); transcript detail rows are rendered by [`cli/tui/transcriptrender/render.go`](../../cli/tui/transcriptrender/render.go). The desktop client also has a separate transcript presentation, documented in [`docs/dev/specs/desktop-chat.md`](../../docs/dev/specs/desktop-chat.md).

### Relevant behavior and evidence

- Provider assistant and reasoning callbacks are distinct streams. The accumulator dispatches each when its corresponding provider event is consumed; it does not combine them into one ordered text stream. `server/llm/grok_test.go` has safe synthetic SSE fixtures for Grok reasoning deltas, completed summaries, and unchanged text preservation. `server/llm/openai_http_stream_test.go` has broader synthetic Responses-stream coverage, including reasoning callback behavior and assistant delta handling.
- Runtime retains progressive reasoning as provisional trace state and reconciles it with completed-response reasoning in [`server/runtime/reasoning_reconciliation.go`](../../server/runtime/reasoning_reconciliation.go). `server/runtime/step_executor.go` resolves the completed response and reasoning disposition before continuing execution. Durable transcript history is appended through runtime steering/persistence; it is not a terminal-only buffer.
- Existing ordering tests are important context, not proof of this reported case: `TestStepLoopPublishesCommentaryAssistantWithToolCallsBeforeReasoningAndToolResults` in `server/runtime/committed_transcript_events_test.go` asserts an assistant commentary/tool-call row precedes its reasoning row. `TestCompletedResponseActiveStreamFinalizesOnce` in `server/runtime/completed_response_stream_test.go` checks live assistant delta, stream terminal, and committed assistant ordering. These fixtures do not establish the provider's actual event order for the reported session.
- Product docs require preserving provider reasoning text as Reasoning Traces (`core-runtime-tools.md`, Grok Model Behavior); the desktop contract says it represents supplied traces in authoritative order (`desktop-chat.md`). Ongoing TUI assistant Markdown is append-only once promoted, and committed assistant finalization matches the active stream identity (`ongoing-scrollback-buffer.md`). The terminology entry defines a Streaming Message as temporary presentation, distinct from committed transcript history.
- The ticket's linked human-QA session is an investigation reference only. No private transcript content or credentials were inspected or copied into this plan. Existing test fixtures are synthetic and credential-free. The ticket's follow-up context includes other KENT-688 findings; the comment asking to file this ordering issue is at 2026-10-09T22:38:20Z. The KENT-723 comment also notes the issue may not be Grok-specific.

### Open evidence gap

- Repository fixtures can reproduce controlled provider event sequences and verify callback, runtime projection, committed-history, and terminal rendering order. They do not show whether the report came from provider event ordering, the runtime event-to-transcript projection/persistence boundary, or terminal rendering in the referenced human-QA session. Resolving that distinction requires safe stream evidence at an authorized boundary; avoid collecting or retaining credentials or private conversation text.

## Design

### Explicit user decisions

- “i think this isn't limited to grok.”
- The reproducer was in persisted order and terminal Detail Mode, using Grok subscription. The user no longer has the Session ID; the Session might still be in the local QA harness setup.
- “IMPORTANT: NO BUFFERING OR REORDER IN TUI ONGOING MODE. Keep it as is.”
- “there, reasoning always precedes `final` text for the same turn, but may go after `commentary` text. that's the final spec.”
- Completed history must place reasoning before the final answer while answer text still streams immediately (selected option 2).
- The user has a working auth token at `~/Downloads/auth.json`. If it is used, the user requested refreshing it and putting it at `~/Desktop/auth.json`. Do not include credentials in artifacts.
- Apply the corrected order to newly recorded responses only, including new responses in existing Sessions. Leave already persisted history untouched. Do not rewrite old history or sort it differently in Detail Mode.
- Verification: focused persisted-order automated regression tests plus manual QA, including a fresh Grok subscription reproduction. Reuse existing provider reasoning callback/text-preservation, commentary/tool-call-before-reasoning, and Streaming Message finalization coverage.
- The automated regression batch must check the shared runtime/history boundary with synthetic early and late reasoning provider sequences. Do not save private content or add a broad provider test suite.
- The later QA agent must check Detail Mode live and after reopening, confirm commentary may precede reasoning, and confirm Ongoing Mode still streams without buffering or reordering. No unrelated GUI changes are part of the ticket.
- On treating phase-absent completed answers as automatically subject to the ordering rule: “no. it depends on how kent will treat it. we already have infra for that.”

## Architecture

## Planning

### Responsible boundary

The shared runtime currently commits the assistant message inside `prepareCompletedResponse` using `steerAssistantCommitIntent`. Only after preparation returns does the step loop call `resolveReasoningDisposition`. That assistant steering operation both appends the durable message and publishes/finalizes its committed transcript row. Consequently, even reasoning received before assistant deltas can be persisted after the final answer. This is a concrete code-path explanation, not evidence of what happened in the missing QA Session.

Correct completed-response recording at this existing runtime boundary. Keep provider callbacks and immediate assistant streaming intact. Reuse reasoning reconciliation and runtime steering for persistence and delivery. Preserve commentary-first ordering and rejected-response trace cleanup. Do not repair the result by sorting client rows, buffering Ongoing Mode, or rewriting history. No provider-specific fix, new transport contract, synchronization mechanism, or new subsystem is needed.

### Ordered implementation checklist

1. [x] Inspect current worktree changes and reconcile them with the settled design.
   - Retain the plan's Recon and explicit Design decisions and the two approved runtime-spec requirements. The current worktree also contains their implementation and focused regressions; preserve and remediate those changes in place without restarting the implementation or disturbing the separate manual-QA items.
   - For the supplied phase-absence finding, retain all unfinished implementation and QA steps. The Grok continuation fixture returns an absent ProviderPhase, the Responses projection preserves nil assistant Phase, and Grok does not enable OpenAI first-party phase enforcement. This supported answer shape must not be omitted from the existing regression batch.
   - The user declined a blanket new rule for missing provider phases and directed reuse of how Kent already treats them. Existing `shared/transcript.ClassifyAssistantPhase` classifies an absent transcript phase as final. The existing runtime phase protocol can instead turn a missing phase into commentary when mandatory phase handling applies. Use the normalized assistant's existing Kent classification, not raw provider-label presence. No new phase policy or phase-metadata spec change is needed.
   - The supplied compliance finding is valid: the existing Reasoning Trace contract in `desktop-chat.md` still prohibits gating any assistant operation on trace persistence. That blanket statement conflicts with the user's explicitly superseding final-message recording decision. Retain the core runtime ordering contract, phase-classification approach, and all unfinished regressions and manual QA. Add only the contextual spec alignment below. No implementation restart is needed.
2. [x] Align the existing Desktop Reasoning Trace contract with the approved final-message recording requirement.
   - Narrow `desktop-chat.md`'s blanket assistant-operation prohibition with a final-message recording exception that references `core-runtime-tools.md`'s Model Requests And Cache Continuity section. Keep the ordering requirement owned in the core runtime spec rather than repeating it in Desktop.
   - Preserve immediate assistant streaming and the existing prohibition on gating other assistant operations, tools, edits, and Agent Step lifecycle operations on trace persistence. Preserve trace durability beginning at reconciliation and both existing failure-cleanup sentences. Do not add failure, recovery, synchronization, or cross-resource atomicity guarantees.
   - Completion: the Desktop contract explicitly distinguishes final-message recording under the owning runtime contract from all other unaffected operations, retains its existing trace durability and cleanup behavior, and contains only a short contextual reference to the ordering rule.
3. [x] Use a red/green tracer regression to correct final-answer recording order at the shared runtime boundary.
   - Start with one accepted response containing a completed Reasoning Trace and assistant `final` text. Exercise the normal engine entry point and inspect committed transcript events and persisted history through existing bounded readers/hydration, not private helper calls or source inspection. Establish failure on the current code before changing it.
   - Arrange existing completed-response processing so accepted final-response reasoning is reconciled before the final assistant message is persisted/published. Determine final versus commentary through Kent's existing transcript phase classification after the existing runtime phase protocol has normalized the assistant. A direct `MessagePhaseFinal` equality gate alone is insufficient: the supported non-first-party phase-absent answer is already classified as final by Kent and requires the same recording order.
   - Preserve absent ProviderPhase and nil assistant Phase on that supported path. Do not invent a provider phase, change phase enforcement, or introduce another phase classifier. Keep the existing commentary-before-reasoning path, including responses already treated as commentary by Kent's phase protocol. Apply disposition once per response, retaining existing rejection/discard behavior rather than adding a second reconciliation authority.
   - Keep final-answer-with-tools preparation and its existing tool effects intact. Correct the final text's recording position without gating tool execution on reasoning or extending this work into Workflow completion changes.
   - Completion: the regression passes with reasoning before final in both committed delivery and bounded persisted history, unchanged synthetic reasoning text, and one final assistant row.
4. [x] Extend the approved persisted-order regression batch incrementally for early and late reasoning sequences, reusing existing fixtures.
   - Include an accepted non-first-party response with an explicit absent ProviderPhase fact and nil assistant Phase alongside the explicit-final cases. Set non-first-party capabilities explicitly in the existing scripted fixture instead of relying on `scriptedllm.FinalAnswer` and its OpenAI first-party defaults. Exercise the normal engine entry point and verify reasoning-before-answer committed/persisted order while provider and persisted assistant phase metadata remain absent.
   - For early reasoning, reuse the existing reasoning-before-assistant fake or safe SSE fixtures. For late reasoning, the scripted client already emits assistant deltas before reasoning deltas. Also exercise reasoning available only in the completed response within this same focused ordering batch.
   - Verify immediate assistant delta delivery remains independent of reasoning arrival and that committed reasoning still precedes final text. Use typed roles/phases and identities, not literal UI wording, source-shape assertions, or new production hooks.
   - Reuse existing commentary/tool-call-before-reasoning coverage and existing final-answer-with-tools coverage. Strengthen the latter's existing reasoning fixture with the approved persisted-order assertion if needed. Reuse stream finalization and text-preservation coverage instead of duplicating it.
   - Completion: every approved early/late/completed-only ordering case passes, the supported non-first-party phase-absent answer receives Kent's final-answer recording order without phase metadata changes, commentary remains allowed before reasoning, and existing finalization/tool coverage passes.
5. [x] Run affected checks and verify the patch remains limited to the ordering correction and approved tests.
   - Run `just test server -- ./server/runtime/... ./server/runtimeview/... ./server/llm/...` for the focused runtime/projection/provider coverage, then `just lint server --dry-run` and `just build go` for the project's server checks and required Go rebuild. The User approved omitting `just check server --dry-run` because it also runs the full server test suite.
   - Inspect the resulting diff for duplicate reasoning reconciliation, provider text changes, client sorting/buffering, old-history rewrites, and unrelated changes. Retain the approved core runtime spec update and the narrowly aligned Desktop contextual reference from step 2. Do not add GUI features or new infrastructure.
   - Completion: the affected suites and server lint/build pass, or concrete tooling blockers are reported without claiming verification. Leave manual verification to the separate QA agent below.
6. [x] Resolve valid implementation-review findings without changing the approved behavior or architecture.
   - Remove the added reasoning lifecycle flag. The step loop derives whether final-answer reasoning was already reconciled from the prepared response action and Kent's normalized assistant-phase classification; commentary, rejection, and terminal dispositions remain on the existing caller path.
   - Represent newly added optional test positions with nil pointers. Leave unrelated pre-existing test sentinels unchanged.
   - No concrete correctness defect or architecture finding required a change. Live Grok and terminal-mode behavior remain for the separate manual-QA stage.

## Testing

### Manual QA checklist — separate QA agent

- [ ] Prepare a safe isolated reproduction.
  - Setup: use the local QA harness, beginning with its `doctor` command, and a fresh task-isolated instance. Use the explicitly approved Grok subscription connection for this reproduction, not the production server. If the supplied auth file is used, refresh it and put the refreshed file at the user-requested destination recorded in Design. Use existing supported auth paths. If refreshing/importing credentials is blocked, ask rather than changing authentication code or claiming the scenario passed.
  - Action: use a new, non-private prompt that yields reasoning and a final answer. Observe typed/bounded evidence sufficient to distinguish provider arrival from committed transcript order. Do not walk a transcript from byte zero to EOF or retain credentials/private conversation content.
  - Expected: reproduction and verification evidence identifies the response's supplied phase or its absence, Kent's existing final/commentary treatment, and reasoning/answer positions without relying on the missing original Session. Do not claim the original report's event sequence was recovered.
- [ ] Verify terminal Detail Mode during completion and after reopening.
  - Setup: use the fresh Grok response with a Reasoning Trace and an answer Kent treats as final. A missing provider phase is supported and does not exclude this response from verification. Do not force Grok to emit a phase or relabel its response.
  - Action: observe the completed rows in Detail Mode, then close/reopen the Session and inspect the same newly recorded response.
  - Expected: reasoning precedes the answer Kent treats as final for that turn in completed live display and reopened persisted history, including when its provider phase is absent. Temporary answer streaming is not required to wait for reasoning. Trace text remains unchanged apart from the already specified presentation projection.
- [ ] Verify commentary is allowed before reasoning.
  - Setup: use a harmless prompt that produces commentary/tool activity followed by reasoning and a final answer in the isolated workspace.
  - Action: inspect the completed turn in Detail Mode and after reopening.
  - Expected: commentary may precede reasoning. Reasoning still precedes the final answer. No client sorting is used to achieve this order.
- [ ] Verify Ongoing Mode remains unchanged.
  - Setup: repeat a safe reasoning/final response in terminal Ongoing Mode.
  - Action: watch answer text arrive and finish.
  - Expected: answer text streams immediately without waiting for late reasoning. Already emitted output is not reordered, cleared/replayed, or buffered to enforce reasoning-before-final history. Reasoning remains in its existing transient Thinking Status presentation, not new scrollback rows.
- [ ] Verify existing history remains untouched.
  - Setup: use an existing isolated Session with completed history and record its bounded committed row identities/order before issuing a fresh turn.
  - Action: issue a fresh reasoning/final turn and reopen Detail Mode.
  - Expected: prior rows retain their existing order and content. Only the newly recorded response follows the corrected reasoning-before-final order.
