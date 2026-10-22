# BUI-149 implementation checkpoint

## Scope and state

- This is an unfinished implementation checkpoint in Session 442, not a completion report.
- The complete approved plan is `BUI-149.md`. Step 1 is complete. Step 2 is in progress. Steps 3–9 are untouched.
- Initial staged plan/spec changes were committed and the branch rebased onto `origin/main` before implementation. The approved baseline commit is `2a591451e`, based on `5aa01aac2`.
- All subsequent working-tree changes belong to this implementation. Preserve them. No implementation was discarded.
- SDK is still v3.56.0. No SDK upgrade, dependency patch, WebSocket connection, continuation checkpoint, cleanup propagation or non-HTTP error change has been implemented.
- No manual QA, production credential use or production server/configuration changes were performed.

## User decisions recovered during implementation

- The user rejected both proposed policies for a response containing a final answer and tool calls. The actual requirement is to trust the provider's emitted item order throughout streaming, acceptance, persistence, display and subsequent requests. Do not invent phase-based rejection or regrouping.
- This includes hosted calls before, between and after commentary. The user explicitly reported hosted search splitting commentary and authorized fixing that within this ticket.
- The user approved revising the single-string streaming architecture and raised the complete ticket's hard production-line cap from 2,000 to 4,000.
- The user wants a separate release for this expanded change. Publication is outside this implementation assignment.
- The proposed strict provider API requirement for tool-result order was withdrawn as unverified. Do not present it as an OpenAI requirement. Existing Result Group roster ordering and persistence timing stay unchanged.
- Existing Result Groups generally hold completed results until the group closes. In the user's three-one-second/one-45-second example, calls are saved before execution and results generally commit around 45 seconds, unless an existing interaction/completion barrier flushes earlier. No timing change was authorized.
- These decisions are recorded in the plan's Design section and its October 22 Architecture adjustment. Owning core spec now distinguishes native output order from generic saved Message behavior.

## Implemented foundation

- `server/session/event_message_v1.go`: native body alternative contains provider format, byte-preserving `[][]byte` native items and local reasoning-type attribution. Native records exclude parallel Content/ToolCalls/ReasoningItems. Local call presentation is saved separately by call ID without copied arguments.
- `server/llm/native_response.go`: immutable native body and parsed projections using the existing output parser. Native bodies pass through `ItemsFromMessages` with original Raw bytes and ordering.
- Real Responses terminal output is attached to the Message in the existing adapter/accumulator. Generic saved Messages retain their existing reader/input reconstruction.
- Runtime acceptance, cloning through existing struct copies, phase handling and event codec carry the native body. Native final/tool output no longer takes the generic phase-based split path. Ordinary final-with-tools completion still runs accepted tools and then completes through the existing loop.
- `native_response_projection.go`: indexed per-item presentation from the native body. Bounded transcript-page projection and reload now retain commentary/search/commentary/final item order.
- Native opaque-only responses are saved without a visible assistant row and can continue to the next model invocation.
- Streaming callback contract now carries `AssistantDelta.OutputIndex` and `OnOutputItem`. Retry plumbing carries one `llm.StreamCallbacks` value and clears emitted output on retries.
- Runtime's provisional assistant state is now a collection of separately correlated streams. Finalization maps each provider output index to its own stream ID.
- Hosted provider output is projected through ordinary typed steering events while generation is streaming. Live tool ledger creation deduplicates repeated starts, including later hosted materialization.
- Tool calls now carry optional original output indexes. Reasoning already had source coordinates.
- Protocol 217 → 218. Hydration now carries `active_assistants`; live assistant/tool/reasoning messages carry optional output indexes.
- Registry tracks multiple active assistant stream IDs. Desktop models, hydration and live transcript projection carry the collection and order native provisional items by output index.
- `just setup --apply` and `just gen` were run. Generated sources are ignored checkout-local artifacts.

## Current tests and checks

- Real Runtime red/green tests in `server/runtime/native_response_continuation_test.go` cover accepted answer bytes/identity, actual tool-result continuation, reasoning attribution, opaque output, hosted-call interleaving, reload, and streaming identity/event ordering.
- `session_event_adapter_test.go` adds native local-presentation reload without a second saved answer/tool-argument body.
- Existing tests were adapted to typed callback arguments, multiple stream correlations and hydration arrays. No source-inspection or literal-copy tests were added.
- Passed: focused `just test server -- ./server/runtime ./server/runtimeview ./server/registry -run 'Native|Stream|Hydration' -count=1` after the CEL and stream-identity corrections.
- Passed earlier affected selection: llm and session tests matching Native/Responses/Reopen/CacheWarning/CacheContinuity/Retained/Streaming/Stream/final-tool cases. The initial Runtime failure in this selection was an obsolete scalar final-stream assertion, now migrated.
- Passed: `just build _desktop-compiler` after optional-property and target-library fixes.
- Passed: `just test desktop -- src/app-facade/transcript-window/transcriptWindowLive.test.ts src/api/clientChat.test.ts`.
- Passed: owning-spec Markdown validator for both changed spec pages. Independent adversarial review is still required by the later workflow gate.
- `git diff --check` passed at the last checkpoint.
- No full server suite, complete lint run or successful final Go binary build yet.

## Immediate build failure and next work

- Continuation update: active Go consumers now read `ActiveAssistants`. Ongoing keeps the head stream plus an ordered pending queue, renders all provisional tails and advances the head on commit/abort without dropping later streams. This removes the scalar build failure and different-ID delta panic; full hosted-tool ordering remains unfinished, so Step 2 remains open.
- Passed: focused CLI `BlankFinal|Hydration` tests, the complete ongoing Surface package, and alternate Go build (`bin/qa/kent-BUI-149`, verified present).
- The broader CLI app run exposed `TestRunPromptCreatesSessionAndPersistsDurableTranscript`: its test-only record projector ignored native bodies. Adapted that projector to `MessageFromNativeResponse` with error propagation; the focused regression now passes. The complete CLI app package has not been rerun after this correction.
- A compound shell command ran the successful build after the failed CLI tests, masking that failure in its final exit code; recorded the actual failed child output above rather than treating it as a clean run.
- Added a public Surface boundary regression accepting two separate streams before either commits; it passes.
- Real-Runtime native reasoning/tool regression now also checks the emitted local-tool start retains output index 2. Red reproduced a nil position; green carries parsed call positions through `toolCallAccumulator.Merge`/`ToToolCalls`.
- Removed redundant parsing of completed native output in the accumulator: the native body uses the already parsed original items, rather than reparsing them through the reload constructor. Focused Runtime/LLM `Native|Responses` tests pass.
- New User decision: terminal calls append at their original positions and result presentation appends later; Desktop keeps results in the original cards. Never reorder, re-emit, rewrap or hold Ongoing tool-call emissions for more than one Step. Recorded in Design and owning `tui-transcript.md`; this still needs implementation. Owning Markdown validation and alternate build pass; independent review belongs to the later gate.
- Extended the real-Runtime hosted-order test through `TranscriptCommittedRowFactsFromSnapshot`: red demonstrated `[before after final search_native]` despite correctly ordered raw ChatEntries. Green keeps native completion views at the native call source, materializes persisted tool messages through that source, and omits the extra later materialized view. Generic Message ownership is unchanged. Focused Runtime `Native|Reopen|Transcript|Hydration` tests pass. Live events/Registry/Desktop ordering still needs the separate correction; do not treat snapshot success as proof of it.
- Extended the same native fixture with reasoning between commentary and hosted search. Red showed `[reasoning before search_native after final]`. Native reasoning now derives from native items at its original position instead of persisting a duplicate local text entry. Only local duration/Kent trace-correlation metadata is stored in `NativeResponseBody.ReasoningPresentation`; parser/reconciliation remain shared with generic responses. Native reconciliation prepares metadata before assistant commit and clears provisional reasoning after commit. Generic reconciliation still saves its existing local entries.
- Added the native reasoning no-duplicate-local-record assertion in the real tool-loop test. Passed focused Runtime/LLM/Session `Native|Reasoning|Reopen` and Runtime/View/Registry/Session `Native|Reasoning|Stream|Hydration`; alternate Go build passes. A subsequent one-line condition correction keeps rejected native output on the existing provisional-reasoning reset path; rerun its affected workflow/reasoning tests as part of the next focused check.
- Real hydration boundary added to the hosted/reasoning order fixture: red showed `[before reasoning after final search_native]`. Delivery scan now derives native tool results at their source call position and suppresses the separate later materialized result view. This changes projection only, not persistence or tool timing. Added actual hydration stream-correlation assertions to the streamed hosted-call fixture; they pass.
- Background command 15836 completed successfully: focused Runtime/View/Registry/Session `Native|Reasoning|Stream|Hydration|WorkflowPreflight`, alternate Go build and `git diff --check` all pass. No background commands remain. Raw live event/Registry/Desktop display ordering and Ongoing start/result rendering are still unfinished.
- Further User clarification: if a later hosted item starts while an earlier message still receives deltas, retain the complete earlier message before the tool card using provider item order. Waiting for that message to finish within the same Step is authorized, without delaying tool execution or waiting for its result. Recorded in Design. Current accumulator callbacks still emit non-message item callbacks immediately; the interleaved-delta case needs coverage and implementation.

`just build go --output ./bin/qa/kent-BUI-149` currently fails:

```text
cli/tui/ongoing/surface.go:207:67: hydration.ActiveAssistant undefined
```

Do not pick one stream just to make this compile. The active Go CLI/TUI still assumes a single assistant stream and must be migrated to the approved ordered behavior. Rust `tui-rs/` stays frozen.

Known active Go references:

- `cli/tui/ongoing/surface.go`: hydration, `activeAssistantState`, delta/finalization handling. A second stream currently panics because its ID differs from the single active stream.
- `cli/app/session_transcript_controller.go`: `hydrationHasNoTerminalRows`.
- `cli/app/ui_runtime_tuple_hydration_test.go`: scalar hydration fixture.

Read the TUI skill and uphold append-only native scrollback. Never repaint earlier emitted lines.

## Important incomplete boundaries

- Desktop provisional stream ordering is implemented, but **committed tool-card display ordering is not solved yet**. Tool rows are owned by later completion events and `present()` appends provisional items after committed rows. A native response containing text around a hosted call can therefore still be wrong at this boundary despite the bounded legacy transcript-page test passing.
- Registry's committed-locator contract is still strictly append-ordered. Do not silently change that contract or make history mutable merely to fix presentation. Resolve the actual native source position through the approved ordered-output model and add the focused behavior test.
- Output indexes reset for each provider response. Kent's exact Step ID can span several model/tool iterations. Do not assume `(StepID, OutputIndex)` globally identifies a response across iterations or add a redundant persistent ID without justification.
- The existing native Message event sequence plus original item index is available as a source coordinate after acceptance. Whether/how to project that to completed/pending tools was only investigated, not implemented or locked as a separate user decision.
- Reasoning Traces still go through the old local-entry reconciliation/persistence path. Audit native summary duplication and full ordered reasoning presentation against the native-body contract before completing Step 2.
- Per-item stream IDs are now mapped for native assistant rows. Verify multiple accepted rows through real Registry/Desktop subscription boundaries, not only raw Runtime events.
- Local ToolCall output indexes can be lost when the terminal accumulator reconstructs `ToolCalls`; hydration derives them from the native body. Check this live/loaded asymmetry.
- Native body construction currently parses terminal items again for its cached projection. Before integration, align the final output/checkpoint with the same native source, and check for duplicated parsing/raw copies within the approved size envelope.
- No production-size feasibility gate has been completed. Count all production changes plus targeted SDK patch/preparation, not tests or unchanged downloaded SDK source.

## Tooling observations

- Quote test filters twice through Just interpolation, e.g. `-run \"'Native|Stream|Hydration'\"`. A first attempt without the inner quotes executed pipes as shell commands.
- An initial fixture used nonexistent `ConnectionOpenAIAPIKey`; corrected to `ConnectionResponses` with the official OpenAI endpoint.
- The generated hydration CEL rule also needed migration from scalar `active_assistant` to `active_assistants.all(...)`. Fixed and regenerated.
- Desktop uses exact optional properties and a target without `toSorted`. Conditional object spreads and cloning before `.sort` fixed compilation.
- The public-doc prose validator was also attempted against the internal task plan and flagged its historical/worklog wording. Do not rewrite approved Design decisions for stylistic cleanup. Both owning specs validate. No public documentation was edited.
- Skills already read in this conversation: kent-tasks, tdd, spec-writing, docs-writing, ui-design, effect-ts and tui-design. No Effect code was changed.
- Main workflow completion still requires all plan items, final affected/full checks, alternate Go build, reproducible SDK preparation, reviewer handoff and `kent task complete`. Do not execute completion from this partial checkpoint.
