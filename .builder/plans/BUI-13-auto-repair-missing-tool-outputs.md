## Definition

GH #359 describes a persisted transcript corruption mode after a Kent server crash or force exit: an assistant tool call can be durably present as a started call, but its corresponding tool output is absent. Providers then reject the next request because provider-visible history contains tool calls without matching outputs. The requested product behavior is to repair only after a model API returns HTTP 400, then walk back no farther than the latest compaction boundary and remove unfinished tool calls from the durable transcript instead of appending synthetic tool outputs. The fix must be provider-generic, not OpenAI-only.

Relevant code found during recon:

- `server/runtime/step_executor.go` sends normal model turns through `Engine.buildRequestPlan` and `Engine.generateWithRetry`, then persists assistant messages and later tool completions.
- `server/runtime/engine.go` has `generateWithRetryClient`, where non-retriable model errors currently return immediately. HTTP 400 is classified as non-retriable in `shared/llmerrors/errors.go`, for both `ProviderAPIError` and legacy `APIStatusError`.
- `server/runtime/chat_store.go` owns runtime provider-history projection. `snapshotProviderItemsLocked` currently materializes tool completion events into provider-visible output items when possible, but leaves a call item without output when no completion exists.
- `server/runtime/message_lifecycle.go` restores persisted `message`, `tool_completed`, `local_entry`, cache observation/warning, and `history_replaced` events by walking `events.jsonl`. `history_replaced` is the current compaction boundary event and resets provider history via `chatStore.replaceHistory`.
- `server/runtime/compaction_persistence.go` appends a `history_replaced` event for compaction. This is not equivalent to the requested repair because the ticket asks to remove unfinished calls directly from the transcript, not to append synthetic completions or add another compaction-style replacement as the only source of truth.
- `server/session/event_log.go` already has private canonical rewrite utilities (`readEventsFile`, `writeEventsFile`) used for log compaction, and `server/session/store.go` exposes read/walk/append APIs. There is not yet an obvious public API for rewriting a filtered event log prefix/tail while updating metadata.
- `server/runtime/transcript_scan.go` and `server/runtime/persisted_transcript_scan.go` can reconstruct visible transcript windows without loading unbounded UI state, but this repair likely needs a bounded tail scan since the ticket explicitly limits repair to the segment after the latest `history_replaced`.
- `server/llm/types.go` defines the provider-neutral item model: tool calls are `function_call` or `custom_tool_call`, outputs are `function_call_output` or `custom_tool_call_output`, and calls become assistant `Message.ToolCalls` while outputs become tool-role messages.
- Existing related tests include `server/runtime/engine_part16_test.go` for non-overflow HTTP 400 behavior, `server/runtime/compaction_*_test.go` for compaction-boundary/history replacement behavior, `server/session/store_part2_test.go` for canonical event-log rewrite behavior, and `server/runtime/persisted_transcript_scan_test.go` for persisted transcript projection.

Open details to settle during design:

- Whether the repair should rewrite `events.jsonl` by removing only the call entries from assistant `message` payloads after the latest `history_replaced`, or also remove associated runtime-only `tool_call_started`/assistant UI event rows if they are ever persisted in the same log. Current persisted restore only consumes `message`, `tool_completed`, `local_entry`, cache events, and `history_replaced`; live `EventToolCallStarted` is not obviously a persisted event kind.
- How to detect "proper outputs" for multi-tool assistant messages: every call ID in an assistant message after the latest compaction should have a matching output by call ID and call/output kind, with materialized tool-role messages taking precedence over `tool_completed` synthesis just like `chatStore` projection. Calls lacking that proper output are candidates for removal. Completed sibling calls must be preserved.
- How to trigger repair after 400-caused request failures without retrying unrelated 400s forever. The settled shape is a progress-bounded repair attempt before returning a non-retriable 400; if repair changed the durable transcript/runtime projection, rebuild the history-consuming provider request and retry while each repair removes additional calls.
- Whether repair should be unavailable for in-memory/non-persisted sessions. Since the ticket asks to remove calls directly from the transcript after crashes, persisted sessions are the meaningful target.
- How to refresh in-memory runtime state after rewriting the persisted transcript. Options include applying the same filtered mutations to `chatStore`, or using an internal restore/reprojection path after durable rewrite.

Completion criteria:

- A provider-generic helper identifies HTTP 400 errors across `ProviderAPIError` and `APIStatusError` without depending on OpenAI-specific error codes or body strings.
- On any runtime provider/API request that consumes session provider-visible history and fails with HTTP 400, Kent attempts transcript repair before surfacing the error; if the repair removes at least one unfinished tool call, Kent rebuilds provider-visible request history/input and retries the provider call. Additional 400s may attempt repair again as long as each repair removes more unfinished calls.
- Repair scans only the tail segment after the latest `history_replaced` event and never removes or edits pre-compaction history.
- Repair removes unfinished local and custom tool calls from persisted assistant messages/provider items rather than appending synthetic tool completions.
- Repair preserves completed tool calls, their output messages/items, unrelated assistant/user/developer messages, local entries, cache observations, and compaction events.
- Multi-tool turns are handled precisely: an assistant message with both completed and unfinished calls is rewritten to keep completed calls; if the assistant message becomes empty, it is removed only when safe.
- If no repairable missing output is found for a 400, behavior remains the current non-retriable failure path and does not loop.
- Each successful repair appends an operator-only warning row with the locked wording and removed-call count.
- Runtime state and persisted `events.jsonl` agree after repair, including after reopening the same session.
- Tests cover at least: single missing output repaired after 400 and retried; no repair before 400; latest compaction boundary respected; multi-call partial repair; custom tool calls/custom outputs, output-kind mismatches, order-insensitive completion, and materialized-output precedence over `tool_completed`; compaction 400 repair/retry and unrelated compaction 400 no-op; eligible exact token-count 400 repair/retry before diagnostic and unrelated/ineligible token-count 400 diagnostic/fallback; active live tool calls not repaired by external/preflight token-count 400s; non-400 errors not repaired; 400 with no repairable tail remains non-retriable; operator-only warning is persisted with the removed-call count.
- Existing Go test suites for touched packages pass, and after Go changes the repo builds with `./scripts/build.sh --output ./bin/kent`.

## Design

- User-facing repair notice is required. When Kent removes unfinished tool calls during this repair flow, it should steer a regular warning/diagnostic row into the committed transcript. The wording should follow: `Transcript history was rolled back <N> calls to repair after interruption`, where `<N>` is the number of removed unfinished calls. This notice is not a synthetic tool completion; it is ordinary operator feedback after the direct transcript repair.
- The repair notice is operator-only transcript feedback, not model-visible context. It should be visible in ongoing/detail transcript through the committed warning/local-entry path, but should not be added to the provider request history.
- Repair should remove only unfinished tool calls when feasible. Preserve assistant text/reasoning and completed sibling tool calls in the same assistant message, and remove the entire assistant message only if removing unfinished tool calls leaves no meaningful message content/items. The user explicitly prefers "ideally only the tool call" while allowing pragmatic simplification if implementation constraints make message-level removal necessary.
- Repair may run after any provider HTTP 400 because provider error codes are not stable enough to distinguish this exact failure. The repair operation must therefore be safe when the 400 is unrelated: if no unfinished tail tool calls exist, it must make no changes and surface the original failure. If a repair changes the transcript, Kent should retry; repeated repair/retry is acceptable while each repair removes additional unfinished calls, and it naturally stops when there are no repairable calls left.
- Repair targets durable session history and refreshed runtime projections. It must not attempt to retroactively rewrite already-emitted ongoing-mode terminal scrollback. After durable/runtime repair, append the warning row as the visible committed indication of what changed.

## Architecture

### Placement

Repair belongs at the runtime/session persistence boundary, not in provider adapters and not in request serialization. Provider adapters should continue to serialize the prepared request they receive and fail invalid history. Runtime provider/API requests that consume provider-visible session history are repair-eligible only when the caller is at a safe pre-model boundary that cannot race with live tool execution. The same corrupted durable tail can be submitted through normal generation, reviewer generation, compaction, or exact token-count requests before the next main model turn, but external/preflight callers must not repair active in-progress tool calls.

The runtime integration point should be a shared wrapper around history-consuming provider/API calls rather than inside provider clients:

- Build the request from the current durable/runtime projection. For normal generation this is `Engine.buildRequestPlan(ctx, stepID, true)`; for reviewer, compaction, and exact token counting this is the existing reviewer/compaction/token-count request builder using `snapshotItems()` or the current compaction window.
- Call the existing generator/counting function and preserve its ordinary transient retry or fallback behavior.
- If the provider/API call fails with HTTP 400, ask the engine to repair missing tail tool outputs.
- If repair removed at least one call, reload projection, rebuild the request from the repaired source of truth, and retry the provider/API call.
- If repair removed nothing, surface the original 400 through the existing non-retriable path.

This keeps repair provider-generic while avoiding request-time filtering. `generateWithRetryClient`, reviewer generation, compaction generation, and precise token counting can keep owning their existing transport retries, prompt-cache observations, and fallback behavior for each concrete request; the repair wrapper sits one level above them and receives a rebuild callback. Requests that do not consume session history can use the existing path without repair.

Compaction needs the same safe repair/retry behavior because `prepareModelTurn` can run auto-compaction before the normal generation request. If local or remote compaction receives HTTP 400 and repair changes the transcript, rebuild the compaction input/window from the repaired provider history and retry compaction. Existing context-overflow payload-collapse repair remains in place: missing-output repair runs first on HTTP 400, and if it makes no change the existing overflow/non-overflow compaction behavior continues unchanged.

Exact token-count calls also need repair when they run inside a safe runtime-owned pre-model boundary because `currentInputTokensPrecisely` and related pre-submit compaction planning paths build requests from `snapshotItems()` and call `CountRequestInputTokens`. On HTTP 400 from eligible token counting, attempt missing-output repair before persisting the exact-token-count failure diagnostic. If repair commits, rebuild the token-count request from the repaired projection and retry; persist the diagnostic and fall back only when repair makes no change or the rebuilt token-count call still fails without further progress. External token-count calls such as runtime-control `ShouldCompactBeforeUserMessage` are not repair-eligible unless they also own the exclusive runtime step/safe boundary; they should keep the existing diagnostic/fallback behavior.

### Repair Eligibility

Add an explicit repair eligibility value passed into the shared wrapper. A call site may request repair only when it satisfies both conditions:

- The caller owns the primary/exclusive runtime turn at a pre-model/pre-compaction boundary, or otherwise proves no live tool execution can exist.
- Runtime live state reports no active tool execution or pending live tool-call bookkeeping; the repair must not rely on durable history alone to distinguish a crash from an in-progress live tool call.

Eligible call sites:

- Normal generation immediately before sending a model request inside `exclusiveStepLifecycle`.
- Reviewer generation when invoked as part of the same exclusive turn and before tool execution.
- Local/remote compaction and precise token counting when invoked from runtime-owned pre-model compaction planning inside the exclusive turn.

Ineligible call sites:

- Runtime-control/preflight calls such as `ShouldCompactBeforeUserMessage` that can run concurrently with a live tool execution.
- Any token-count, compaction, or generation call that cannot prove it owns the safe pre-model boundary.
- Any repair attempt while the runtime has active live tool executions/pending tool-call starts.

When a call is ineligible and receives HTTP 400, preserve existing behavior: exact token counting records its diagnostic/falls back, compaction/generation surfaces or handles the error as before, and no durable transcript rewrite occurs.

### Error Classification

Add a provider-neutral status helper in `shared/llmerrors` and re-export it from `server/llm/errors.go`, for example:

```go
func HasHTTPStatus(err error, statusCode int) bool
```

The helper should use `errors.As` for both `*ProviderAPIError` and `*APIStatusError`. The repair trigger should call this helper with `400`; it must not parse provider response bodies, provider codes, OpenAI-specific fields, or error message strings. `IsNonRetriableModelError` should remain responsible for existing retry policy; the repair wrapper runs before accepting the 400 as final.

### Repair Model

Introduce a runtime-owned repair scanner, likely in `server/runtime/missing_tool_output_repair.go`, with a small result type:

```go
type missingToolOutputRepairResult struct {
	RemovedCalls int
	Changed      bool
}
```

The scan is over persisted session events, not the in-memory request. It walks only the segment after the latest valid, non-legacy `history_replaced` event by decoding with `decodePersistedHistoryReplacementPayload`, resetting state at that boundary, and remembering the boundary event sequence/ordinal for the rewrite pass. Legacy ignored `history_replaced` events should be skipped the same way restore skips them; malformed/non-decodable `history_replaced` events must abort repair before commit rather than letting repair cross a boundary that normal restore would fail to decode. If there is no compaction boundary, the whole log is eligible, but the implementation still streams events and stores only distinct call IDs/output state plus the latest boundary marker, not all event payloads.

The first pass should collect post-boundary tool-call completion state with call kind:

- Assistant `message` events contribute candidate calls from `llm.Message.ToolCalls` and record whether each call is a normal function call or custom tool call.
- Materialized tool-role `message` events with `ToolCallID` are recorded with output kind. They complete a call only when their output kind matches the recorded call kind. `llm.MessageTypeCustomToolCallOutput` completes custom calls; ordinary tool messages complete function calls. A custom call followed by a normal tool-role message is still unfinished/invalid.
- `tool_completed` events mark their non-empty outer `CallID` as completed only when doing so matches `chatStore.snapshotProviderItemsLocked` projection semantics. If there is any materialized tool-role message for that call ID, `chatStore` skips `tool_completed` synthesis, so completion depends solely on the materialized output kind. If there is no materialized output, `ProviderItems` may complete the call when they contain an output item for the same outer call ID whose kind matches the recorded call kind; if `ProviderItems` is empty, `chatStore` will synthesize the output kind from the call item and the call is complete.
- `tool_completed.ProviderItems` should not complete arbitrary inner call IDs; inspect provider items only as validation/shaping for that same outer call unless `chatStore` completion indexing changes.
- Later valid, non-legacy `history_replaced` events clear all previous pending calls.

The scanner must be order-insensitive like restore/projection. Do not use a pending-only algorithm that assumes outputs occur after assistant calls. Instead collect separate maps for assistant call kind, materialized output kind, and `tool_completed` state keyed by call ID, clearing all maps on valid compaction boundaries. At end of pass, classify each assistant call using those maps and `chatStore` precedence rules. This preserves existing behavior where `tool_completed` can appear before the assistant message and later enrich/synthesize the result. Memory remains proportional to distinct tool call IDs/output records in the active post-compaction segment, not to full transcript payload size. If no call is classified unfinished, repair is a no-op and the original 400 is returned.

The rewrite pass edits only assistant `message` payloads strictly after the same latest compaction boundary:

- Remove only `ToolCalls` whose IDs are unfinished.
- Remove materialized tool-role `message` events for removed call IDs when those outputs did not satisfy the call kind; leaving a mismatched or orphan output would keep provider-visible history invalid after the call is removed.
- Preserve assistant content, phase, message type, source path, reasoning items, and completed sibling calls.
- Drop an assistant message event only when removing unfinished calls leaves no content, no reasoning items, and no completed tool calls.
- Preserve all unrelated events, including user/developer messages, local entries, cache observations/warnings, `tool_completed`, matched completed output messages, and `history_replaced`.
- Do not append synthetic tool outputs.

This call-level rewrite satisfies the user preference for preserving surrounding assistant text/reasoning. If implementation discovers persisted response-item payloads beyond `llm.Message` in this path, the same call-ID filtering rules should be applied at the provider-item boundary rather than broadening to message-level deletion.

### Session Store Rewrite API

Add a generic streaming rewrite API to `server/session.Store` instead of teaching `session` about runtime transcript semantics. The runtime supplies analysis/transform callbacks and any post-rewrite events; the store owns locking, atomic file replacement, metadata reconciliation, and observer notification.

Recommended shape:

```go
type EventRewriteDecision struct {
	Event Event
	Drop  bool
}

type EventRewriteResult struct {
	Changed        bool
	LastSequence   int64
	OldLastSequence int64
	EventCount     int
	AppendedEvents []Event
}

func (s *Store) AnalyzeAndRewriteEvents(
	stepID string,
	analyze func(Event) error,
	transform func(Event) (EventRewriteDecision, error),
	extraEvents func() ([]EventInput, error),
) (EventRewriteResult, bool, error)
```

Implementation requirements:

- Hold `Store.mu` for both the analysis pass and the rewrite pass so appends cannot interleave between finding unfinished calls and removing them.
- Treat callbacks as pure, non-reentrant functions. `analyze`, `transform`, and `extraEvents` must not call back into `Store`, runtime output/control mutation, observers, or any API that can acquire `Store.mu`; they should only decode payloads, update closure-local analysis state, and return transformed events.
- Validate/open the current `events.jsonl` as a regular session file using existing file-security helpers for each pass.
- Stream old events during analysis, then reopen/stream old events again while writing transformed events to a temp file line by line; do not call `ReadEvents`, `readEventsFile`, or otherwise materialize the whole log.
- Preserve existing `Seq` and `Timestamp` for unchanged/modified events. Dropped events may leave sequence gaps; do not renumber unrelated history.
- Build caller-provided `extraEvents` after analysis and before replacement while still holding `Store.mu`. For this repair, the extra event is the required `local_entry` warning, assigned `oldLastSequence + 1` so transcript revision remains monotonic even when the repaired tail drops the old last event.
- Set `meta.LastSequence` to the highest written/appended sequence, not to a lower last remaining historical sequence. This preserves monotonic transcript revisions for live UI reducers and future appends.
- Update `eventsFileSizeBytes`, `conversationFreshness`, and `UpdatedAt` from the rewritten stream; reset `writesSinceCompaction` and `pendingFsyncWrites` to `0` after the atomic rewrite because they are counters, not derivable transcript facts.
- Persist metadata and notify the persistence observer exactly like append/compaction paths.
- Return a `committed` flag separately from `err`, matching append-path commit semantics. `committed` becomes true once the `events.jsonl` replacement succeeds, even if metadata persistence/reconciliation or observer notification later fails. Runtime must reload from disk when `committed=true` and surface the post-commit error separately.
- Return `Changed=false` without replacing the file when the transformer leaves every event byte-for-byte equivalent and `extraEvents` returns no events.

The existing private canonical rewrite in `event_log.go` can provide encoding/atomic-rename primitives, but it currently loads all events; the new API should be streaming so it respects the repository invariant that `events.jsonl` is unbounded.

### Runtime Projection Refresh

The repair warning must be part of the same store transaction as the rewrite, not a separate best-effort append. Define one runtime formatter/constant for the exact text:

```go
Transcript history was rolled back <N> calls to repair after interruption
```

After a committed durable rewrite, runtime state must be refreshed from the repaired transcript before the request is rebuilt. Do not rely on an in-memory request filter and do not leave `chatStore` with removed calls.

Add an explicit typed steering repair item under the runtime output-mutation boundary. The repair item should own the committed store rewrite result, the non-persisting live warning event, and projection replacement; do not add ad-hoc runtime emitters or direct raw mutation calls outside the steering boundary. The helper should:

- Run through `steer(...)`/`applySteeringItem` while holding `outputMutationMu` so durable writes, runtime projection changes, and emitted events stay ordered. Maintain lock order as runtime output/control lock first, then `Store.mu`; never call back into runtime output mutation while holding `Store.mu`.
- Rebuild a fresh transcript projection by walking persisted events through side-effect-free persisted-event replay code extracted from `message_lifecycle.go` and `TranscriptProjector`.
- Replace the active `transcriptRuntimeState` chat projection atomically with the rebuilt `chatStore`.
- Restore runtime side state that is derived from persisted transcript events and can affect later requests: local diagnostic dedupe, prompt-cache response lineage from cache response events, compaction count/last workflow compaction marker, compaction-soon reminder state, and `baseMetaInjected`.
- Avoid live side effects that resume-time recovery performs for operator convenience, such as re-queuing handoff requests/future messages or background notices.
- Reset precise token tracking and clear or recompute persisted `UsageState`, because removing provider-visible calls changes the request token baseline.
- Preserve live scrollback semantics by not emitting delete/update events for already-rendered ongoing-mode rows. Live clients may see later snapshots without the removed tool calls; the terminal scrollback already printed remains untouched.

The warning is a normal persisted `local_entry`: visible/operator-only, not included in provider history. Because the warning event is appended inside the rewrite transaction, runtime must not call `appendPersistedLocalEntryRecordRaw` for it. Add a dedicated typed steering item/helper for repair warnings that emits the already-persisted warning exactly once, fed by the warning event returned in `EventRewriteResult.AppendedEvents`.

The typed steering helper should capture the pre-repair committed entry count before rewrite, build the warning `ChatEntry` from the appended `local_entry` payload, and emit an append-only `EventLocalEntryAdded` with explicit transcript metadata instead of relying on `emitRaw` to infer it from the repaired projection. Use `CommittedEntryStart = preRepairCommittedCount`, `CommittedEntryCount = preRepairCommittedCount + warningEntryCount`, and `TranscriptRevision = rewriteResult.LastSequence`; then reload projection from disk so future snapshots/detail/reopen views reflect the repaired transcript and warning. Do not emit deletion events for removed tool-call rows.

The reload helper should clear streaming assistant state before retry if the failed 400 attempt emitted any partial deltas. `generateWithRetryClient` currently returns immediately for non-retriable 400 before `onAttemptReset`; extend the generation wrapper enough to report emitted assistant/reasoning deltas and call the existing clear-streaming steering intent before the external repair retry.

### Retry Control Flow

Each history-consuming provider/API call should be progress-bounded:

```go
for {
	req, err := rebuildRequestFromCurrentProjection()
	if err != nil { return err }

	resp, err := callProviderOrCountTokensWithExistingRetry(req)
	if err == nil { return resp, nil }
	if !llm.HasHTTPStatus(err, 400) { return err }
	if !repairEligibility.AllowsRepair(e) { return err }

	repair, committed, repairErr := e.repairMissingToolOutputsAfterHTTP400(ctx, stepID)
	if committed { reloadProjectionAndEmitWarningAppendOnly() }
	if repairErr != nil { return errors.Join(err, repairErr) }
	if !repair.Changed || repair.RemovedCalls == 0 { return err }
	// Durable history and chat projection changed. Rebuild from source of truth.
}
```

There is no fixed retry count for this repair loop. It stops because each iteration must remove at least one previously persisted unfinished call; once no calls remain, the original 400 is returned. A defensive upper bound equal to the number of removed calls observed so far is unnecessary if the loop requires progress, but tests should include unrelated normal-generation, compaction, and token-count 400s to prove no path spins.

Prompt-cache observations are acceptable per request attempt: the failed pre-repair request may leave a `cache_request_observed` event without a response, and the rebuilt post-repair request records its own observation. Since restore ignores request-only observations for lineage, this does not affect future provider history. Compaction retry wrappers should preserve existing context-overflow collapse behavior after a no-op missing-output repair. Exact token-count wrappers should avoid `reportPreciseTokenCountFailure` while a repair is committed and the count request is being rebuilt/retried.

### Failure Paths And Concurrency

- If the repair scan or rewrite fails before commit, return the original model error joined with the repair error so the operator sees both the provider failure and the local repair failure.
- If durable rewrite commits but metadata persistence, observer notification, projection reload, or live warning emission fails, reload from disk when possible and surface the joined error. The on-disk transcript is already the source of truth and reopening should recover the repaired state.
- If the store is not durable, repair should be a no-op. The ticket is about persisted crash recovery, and non-durable sessions have no durable transcript to rewrite.
- `exclusiveStepLifecycle` prevents overlapping model turns, and `Store.mu` serializes append/rewrite. The repair helper should also use `controlMutationMu` or the existing output mutation boundary so compaction, warning append, and transcript reload cannot interleave with repair.
- Repair must not touch pending live tool execution bookkeeping or calls that might belong to active tool goroutines. `EventToolCallStarted` is a live runtime event, not a persisted restore input; durable repair edits assistant `message` events only after the eligibility gate proves there is no live in-progress tool execution to corrupt.

### Test Plan

- `shared/llmerrors` or `server/llm` unit tests: `HasHTTPStatus(err, 400)` recognizes `ProviderAPIError`, wrapped `ProviderAPIError`, `APIStatusError`, and wrapped `APIStatusError`; it rejects other statuses and nil.
- `server/session` tests: streaming rewrite drops/edits events without loading all events, preserves regular-file safety, appends post-rewrite events in the same transaction, preserves monotonic `LastSequence`, leaves sequence gaps intentionally, recomputes freshness, resets write/fsync counters, reports committed observer failures, and avoids replacing unchanged logs.
- Runtime repair scanner tests: single missing function call, custom tool call, output-kind mismatch, mismatched materialized output plus valid `tool_completed`, `tool_completed` before assistant message, multi-call assistant message with one completed sibling, message dropped when empty, content/reasoning preserved, completion recognized from matching-kind tool-role message and from `tool_completed` only when materialized-output precedence allows it.
- Runtime integration tests: normal generation gets HTTP 400, repairs, appends the warning with removed-call count, rebuilds the request, and retries successfully; compaction gets HTTP 400 from corrupted history, repairs, rebuilds compaction input/window, and retries successfully; eligible exact token-count requests get HTTP 400 from corrupted history, repair before diagnostic persistence, rebuild, and retry successfully; ineligible external/preflight token-count 400s do not repair active in-progress tool calls; no repair happens before a 400; a non-400 does not repair; normal-generation, compaction, and token-count 400s with no unfinished calls return/fall back through the original behavior without looping; latest valid `history_replaced` boundary prevents editing pre-compaction missing calls; malformed `history_replaced` aborts repair; repeated 400s repair while progress is made; live warning emission appends after already-rendered removed rows through typed steering instead of targeting their old indices.
- Reopen test: after repair and warning, reopening the same session reconstructs provider items without unfinished calls and includes the operator-only warning in transcript projections.

## Planning

Implementation should proceed as vertical TDD slices. Each slice starts with the narrowest failing behavior test for that slice, then implements only enough production code to pass before moving on. Keep the feature behind runtime provider-call orchestration for requests that consume session history; do not add request-time/provider-adapter sanitizers.

### Slice 1 - Provider HTTP Status Helper

- [x] RED: Add status-helper tests covering `ProviderAPIError`, wrapped `ProviderAPIError`, `APIStatusError`, wrapped `APIStatusError`, nil, and non-400 statuses.
  - Files: `server/llm/errors_test.go` or package-level tests in `shared/llmerrors`.
  - Completion criterion: the new tests fail because no provider-generic HTTP status helper exists.
- [x] GREEN: Add `HasHTTPStatus(err error, statusCode int) bool` in `shared/llmerrors/errors.go` and re-export from `server/llm/errors.go`.
  - Files: `shared/llmerrors/errors.go`, `server/llm/errors.go`.
  - Completion criterion: `./scripts/test.sh ./server/llm ./shared/llmerrors` passes and existing non-retriable/context-overflow behavior is unchanged.

### Slice 2 - Streaming Session Event Rewrite API

- [x] RED: Add session-store tests for analysis+rewrite under one store lock, event dropping/editing, same-transaction extra event append, monotonic `LastSequence`, intentional sequence gaps, freshness recomputation, write/fsync counter reset, unchanged no-op behavior, and committed-result behavior when post-commit observer notification fails.
  - Files: `server/session/store_part2_test.go` or a new `server/session/event_rewrite_test.go`.
  - Completion criterion: targeted `./scripts/test.sh ./server/session -run 'Test.*Rewrite'` fails on the missing API.
- [x] GREEN: Implement `AnalyzeAndRewriteEvents` or equivalent generic API on `session.Store`.
  - Files: `server/session/store.go`, `server/session/event_log.go`; add small helper functions there rather than loading all events with `ReadEvents`/`readEventsFile`.
  - Required contracts: callbacks are pure/non-reentrant; `Store.mu` covers both passes; rewritten events preserve original `Seq`/`Timestamp`; caller-provided extra events are assigned after the old high-water sequence; `committed=true` once `events.jsonl` replacement succeeds; metadata/observer errors after replacement are returned with `committed=true`.
  - Completion criterion: session rewrite tests pass with `./scripts/test.sh ./server/session -run 'Test.*Rewrite'`, and `rg 'ReadEvents\\(|readEventsFile\\(' server/session` confirms the new rewrite path does not materialize full event logs.

### Slice 3 - Runtime Missing-Output Repair Scanner

- [x] RED: Add runtime repair-core tests for missing function calls, missing custom calls, output-kind matching, custom call plus normal function output remaining repairable, mismatched materialized output plus valid `tool_completed` remaining repairable because materialized output blocks synthesis, `tool_completed` before assistant message completing a later valid call, completed sibling preservation, assistant message dropping only when empty, assistant content/reasoning preservation, completion recognized from matching-kind tool-role `message`, completion recognized from outer `tool_completed.CallID` only when materialized-output precedence allows proper synthesis/provider-item kind, inner `ProviderItems` not completing arbitrary calls, latest valid `history_replaced` boundary, legacy ignored `history_replaced`, and malformed `history_replaced` aborting repair.
  - Files: new `server/runtime/missing_tool_output_repair_test.go`.
  - Completion criterion: targeted runtime tests fail on missing repair scanner/rewrite orchestration.
- [x] GREEN: Implement the repair scanner and event transformer.
  - Files: new `server/runtime/missing_tool_output_repair.go`; reuse `llm.Message`, `storedToolCompletion`, and `decodePersistedHistoryReplacementPayload`.
  - Required contracts: scanner is order-insensitive and keeps call-kind, materialized-output-kind, and `tool_completed` maps keyed by call ID plus latest boundary marker; rewrite edits only post-boundary assistant `message` events and removes mismatched materialized output messages for repaired call IDs; no synthetic tool outputs; warning `local_entry` is created through the store rewrite API's extra-event callback using the exact runtime formatter.
  - Completion criterion: `./scripts/test.sh ./server/runtime -run 'TestMissingToolOutputRepair'` passes and repaired persisted events reopen without unfinished calls.

### Slice 4 - Side-Effect-Free Runtime Projection Reload

- [x] RED: Add tests proving a committed repair refreshes `chatStore` provider items and transcript snapshots from disk without re-queuing handoff/future-message side effects, while restoring derived state needed for later requests.
  - Files: `server/runtime/missing_tool_output_repair_test.go` plus focused replay tests if extraction needs them.
  - Completion criterion: tests fail because runtime projection remains stale after durable rewrite or replay has resume-only side effects.
- [x] GREEN: Extract reusable persisted-event replay for repair projection refresh.
  - Files: `server/runtime/message_lifecycle.go`, `server/runtime/transcript_projector.go`, `server/runtime/transcript_runtime_state.go`, possibly `server/runtime/request_cache_lineage.go`.
  - Required contracts: replace active chat projection atomically; restore local diagnostics, prompt-cache response lineage, compaction count/last workflow compaction marker, compaction-soon reminder state, and `baseMetaInjected`; do not enqueue handoff/background side effects; reset precise token tracking and clear or recompute persisted `UsageState`.
  - Completion criterion: targeted runtime replay tests pass and `snapshotItems()` after repair matches reopened-session provider items.

### Slice 5 - Append-Only Live Warning Semantics

- [x] RED: Add a live-event test where a persisted missing tool call has already produced a committed tool-call row, repair removes that row from durable projection, and the operator warning is emitted through typed steering as an append-only `local_entry_added` after the pre-repair committed count.
  - Files: `server/runtime/missing_tool_output_repair_test.go` or `server/runtime/committed_transcript_events_test.go`.
  - Completion criterion: test fails because the warning is duplicated, persisted outside the rewrite transaction, or targets the removed row's old index.
- [x] GREEN: Add a dedicated typed steering item/helper for repair warning emission and projection reload.
  - Files: `server/runtime/output_steering.go`, `server/runtime/missing_tool_output_repair.go`, and architecture guard tests if the new typed steering boundary needs allowlisting.
  - Required contracts: use the warning event returned from `EventRewriteResult.AppendedEvents`; do not call `appendPersistedLocalEntryRecordRaw`; do not add ad-hoc `emitRaw` callers outside the steering boundary; emit explicit `TranscriptRevision`, `CommittedEntryStart`, and append-style `CommittedEntryCount`; then reload projection from disk.
  - Completion criterion: live-event test passes, ongoing-mode append semantics are preserved, and architecture guard tests still pass with the new typed steering boundary.

### Slice 6 - Provider HTTP 400 Repair Loop For History Consumers

- [x] RED: Add the tracer integration test: normal generation request fails with HTTP 400 due to a persisted unfinished tool call, Kent repairs durable history, appends one operator-only warning, rebuilds the request, retries, and succeeds.
  - Files: new `server/runtime/missing_tool_output_repair_integration_test.go` or `server/runtime/engine_part16_test.go`.
  - Completion criterion: test fails because current `generateWithRetry` returns the non-retriable 400 without repair.
- [x] RED: Add compaction integration tests where a corrupted tail reaches compaction first: local or remote compaction receives HTTP 400, repair removes the unfinished call, compaction rebuilds from repaired provider history, and retry succeeds; unrelated compaction HTTP 400 with no unfinished calls returns the original error without looping.
  - Files: `server/runtime/compaction_overflow_repair_test.go`, `server/runtime/compaction_trim_test.go`, or the new integration test file.
  - Completion criterion: tests fail because compaction currently surfaces the 400 without missing-output repair.
- [x] RED: Add exact token-count tests where an eligible runtime-owned `CountRequestInputTokens` returns HTTP 400 from corrupted current history, repair runs before `precise_token_count_failure` is persisted, the token-count request is rebuilt and retried successfully; unrelated eligible token-count HTTP 400 with no repairable tail preserves the existing diagnostic/fallback behavior without looping.
  - Files: `server/runtime/token_usage_test.go`, `server/runtime/engine_part12_test.go`, or the new integration test file.
  - Completion criterion: tests fail because current token counting persists the diagnostic and falls back without attempting missing-output repair.
- [x] GREEN: Add the progress-bounded repair wrapper around all provider/API calls that consume session provider history.
  - Files: `server/runtime/step_executor.go`, `server/runtime/engine.go`, `server/runtime/reviewer_pipeline.go`, `server/runtime/compaction_executor.go`, `server/runtime/compaction.go`, `server/runtime/missing_tool_output_repair.go`.
  - Required contracts: normal generation, reviewer generation, local/remote compaction, and eligible current-history token counting rebuild from repaired projection after a committed repair; token counting suppresses the exact-token-count diagnostic while repair/retry succeeds; requests that do not consume session history or fail the repair-eligibility gate keep the existing path; repair only triggers on `llm.HasHTTPStatus(err, 400)`; each retry requires `RemovedCalls > 0`; no request-time item filtering is introduced.
  - Completion criterion: normal-generation tracer, compaction repair/no-op tests, and token-count repair/no-op tests pass, existing non-repair retry tests in `server/runtime/engine_part16_test.go` still pass, and context-overflow compaction payload-collapse tests continue to pass.

### Slice 7 - Error, Boundary, And Retry Edge Cases

- [x] RED/GREEN: Add and satisfy active-tool safety tests: while a tool execution is live and its assistant call legitimately lacks output, an external/preflight exact-token-count HTTP 400 must not repair or rewrite the active call; the same corrupted durable tail can still be repaired later from an eligible safe pre-model boundary.
  - Files: `server/runtime/token_usage_test.go`, `server/runtime/missing_tool_output_repair_integration_test.go`, or a focused runtime-control test covering `server/runtimecontrol/service.go`.
  - Completion criterion: active in-progress tool calls remain persisted and runtime bookkeeping is untouched after ineligible token-count 400s.
- [x] RED/GREEN: Add and satisfy one edge-case test at a time for non-400 no repair, unrelated 400 with no unfinished calls surfacing original error, repeated 400s repairing while progress is made, latest valid compaction boundary respected, malformed compaction boundary aborting repair, custom tool call/output kind mismatch repair, and partial multi-call repair.
  - Files: `server/runtime/missing_tool_output_repair_integration_test.go`, `server/runtime/missing_tool_output_repair_test.go`.
  - Completion criterion: `./scripts/test.sh ./server/runtime -run 'Test.*MissingToolOutput|Test.*HTTP400|Test.*Repair'` passes and each edge case has one behavior-focused test.
- [x] RED/GREEN: Add and satisfy a streaming partial-delta test if current fakes can simulate HTTP 400 after deltas; otherwise add a focused generator test proving the external repair retry clears streaming state when the failed attempt emitted deltas.
  - Files: `server/runtime/engine_part16_test.go` or the new integration test file.
  - Completion criterion: no stale `ChatSnapshot().Ongoing` or reasoning delta remains before the repaired retry.

### Slice 8 - Refactor, Guards, And Documentation Check

- [x] Refactor duplicated persisted-event replay/scanning helpers only after all repair tests are green.
  - Files: keep runtime repair in cohesive helpers; avoid growing existing files beyond their current responsibilities where practical.
  - Completion criterion: targeted runtime/session/llm tests still pass after each refactor step.
- [x] Update architecture guard tests if the new typed repair steering item or projection replacement boundary requires explicit allowlisting.
  - Files: `server/runtime/meta_context_architecture_test.go`.
  - Completion criterion: guard tests pass and only intentional repair-boundary files are allowlisted.
- [x] Verify `docs/dev/specs/core-runtime-tools.md` still matches the implemented UX and no public docs need additional changes.
  - Completion criterion: spec states HTTP 400-triggered durable repair, no synthetic completions, and operator-only warning behavior.

### Final Verification

- [x] Run targeted package tests: `./scripts/test.sh ./shared/llmerrors ./server/llm ./server/session ./server/runtime`.
- [x] Run the required Go build before handoff after production code changes: `./scripts/build.sh --output ./bin/kent`.
- [x] Final completion criterion: all targeted tests pass, build succeeds, repaired `events.jsonl` and runtime projections agree after reopen, and no provider adapter/request serialization sanitizer was added.

### Review Remediation

- [x] Fixed exact token-count repair eligibility so preflight/runtime-control current-history token counts use a non-repairing path, while safe pre-model runtime paths opt into repair explicitly.
- [x] Fixed streaming session rewrite high-water sequence preservation when a rewrite drops the highest-sequence event without appending a new event.
- [x] Re-ran targeted package tests, the required Go build, `git diff --check`, and the full `./scripts/test.sh` suite after review fixes.

### Delegable Workstreams

- Session rewrite API can be delegated independently after Slice 2 RED tests are written. Write scope: `server/session/**`.
- Runtime repair scanner can be delegated after the session API contract is green. Write scope: `server/runtime/missing_tool_output_repair.go` and its tests only.
- Runtime integration/loop work should stay with the primary implementer because it crosses `step_executor`, reviewer generation, compaction retry, exact token counting, projection reload, live events, and warning semantics.
