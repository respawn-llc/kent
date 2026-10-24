## Scope And Authority

- This specification owns observable Session mutation order and the choice of work between Agent Steps for one Active Session Runtime.
- A durable Session may have no Active Session Runtime or one Active Session Runtime.
- Interactive startup, a Headless Run, Workflow Execution, or an interactive Chat mutation may open an Active Session Runtime when the server has the complete activation context.
- Interactive Chat mutations prepare and open their required Active Session Runtime inside the server before mutation acceptance.
- Desktop and TUI must use one action-attached Runtime preparation approach. When an action supplies invocation preparation choices, the server must validate and apply them. When those choices are absent, the server must use the saved Session choices.
- TUI invocation preparation must apply the selected Agent and explicit Thinking and tool choices supported by Session launch.
- An explicit Thinking choice must apply to an already-loaded Engine during preparation. Applying that choice may wait for the current Agent Step but must not wait for the whole Agent Turn.
- A user-Shell activation in Chat Steer or Queue must use the same server-owned preparation. It must work as the first action on a dormant existing Session without a preceding Send. It must execute without a model invocation and report its result at the command boundary.
- Clients never activate, own, release, or otherwise manage an Active Session Runtime as a prerequisite for a Chat mutation.
- The Active Session Runtime is authoritative for its live model, transcript, Pending Work, and Session-setting state.
- Every model-executing Agent Turn, including Goal-started work, must belong to a live Exact Execution Scope before its first Agent Step and until its model work finishes.
- That execution must be the single authority for its pending Questions and Approvals, answer delivery, and interruption.
- Clients render server state. They must not create another ordering authority.
- Boundary-required Session mutations are applied one at a time in acceptance order.
- Kent promises no order between concurrent requests before one request is accepted.
- Acceptance order governs operations that share the same boundary owner.
- Acceptance order gives no ordering guarantee for later provider, tool, process, Worktree, Reviewer, or Workflow execution.
- Accepted mutations belong to the Session, not to one client connection.
- Caller cancellation or disconnection stops only that caller's wait and delivery. A Chat Operation is independent of both before and after mutation acceptance.
- Pending mutations are process-local and may be lost on server exit.
- A server exit may lose an in-progress Agent Step. Already committed Session state is authoritative after exit.

## Acceptance And Responses

- Human Send/Steer accepts each message as a distinct Pending Work item and returns its server identity without waiting for model execution.
- The intent-level Chat Steer result maps existing admission into accepted or not accepted without changing the ordinary Submit User Turn response. An accepted result carries its Session and Queue Item identities. A concrete failure observed synchronously after acceptance may accompany those identities as a typed diagnostic.
- Provider, model, tool, or Runtime failure after Chat Steer acceptance follows the Runtime or transcript event path. It must not downgrade, replace, or rewrite the delivered Chat result.
- Valid human text is acceptable while provider, tool, Worktree, process, Reviewer, Workflow, or compaction work is active whenever the Session can reach another Step Boundary.
- An Idle Active Session Runtime applies accepted short mutations without waiting for a prior Agent Step.
- Post-turn Queue is a separate server-owned first-in, first-out message collection exposed through one public Session Queue operation.
- Queue accepts the same typed ordinary-text or prompt/Agent-command input as Submit User Turn. The server resolves a prompt or Agent command at admission from the Session's authoritative effective workspace and queues the resolved model input. History and Pending Work presentation use the canonical command text.
- Post-turn Queue accepts each input as a distinct Queue Item and returns its identity without waiting for its later eligible turn. If no Agent Turn is active and the Session can start ordinary work, the accepted Queue Item starts immediately. Otherwise, ordinary after-turn eligibility applies.
- Each new Agent Turn must consume at most one post-turn queued model message, selecting the oldest eligible one. Remaining Queue Items must wait until that Agent Turn ends before starting subsequent turns.
- Session Name persists immediately while an Agent Step is running.
- An unnamed Session must have explicit name absence. Setting a name must require nonblank text. Clearing a name must remove it rather than store an empty name.
- A dormant existing Session's name change must persist without starting an Active Session Runtime. Name changes must be available while an Agent Step runs.
- Thinking, Fast Mode, Supervisor, Questions, and Auto-compaction changes may complete failure-prone preparation, durably commit, and return while an Agent Step is running.
- A committed live-Runtime setting change is accepted in Session mutation order and applies at the next between-Agent-Step boundary.
- Setting changes enter neither user-visible Pending Work nor the post-turn Queue.
- The server publishes each successful setting change and its typed transient feedback to every connected client.
- Clients observing an existing Session's settings must receive one complete authoritative settings snapshot when its name or Agent, Thinking, Fast Mode, Supervisor, Questions, or Auto-compaction settings change. The snapshot must include explicit name absence and must be available without an Active Session Runtime. Snapshot delivery must follow ordinary delivery semantics without an additional ordering or replay guarantee.
- A setting change affects later provider and compaction requests and never alters an Agent Step already running. Native Thinking updates follow the legal-position deferral rule in Model Requests And Cache Continuity.
- Setting changes create no model-visible entries or transcript rows except for the cache-preserving Thinking configuration items defined by Model Requests And Cache Continuity.
- An operator Thinking change and a Workflow-owned Thinking change have no relative ordering or precedence guarantee when they overlap.
- Kent must not delay either change, assign a shared winner order, or reconcile the two owners. Request acceptance and response order do not determine the effective Thinking value.
- The effective live Thinking value is whichever independently owned write applies last.
- If its own later state publication or notification fails, an authoritative Thinking write is applied.
- A definitely-uncommitted Thinking write has no effect on the live value.
- Another independently owned Thinking write may replace an authoritative value.
- Boundary-required model-visible technical entries apply in accepted order.
- A user foreground shell command reports its terminal result at the command boundary.
- Human Shell actions must enter the same server-owned Steer or post-turn Queue collection as human messages, using the caller's selected intent.
- Human Shell actions must use typed activations through the ordinary Chat Steer and Queue operations, not a separate Shell operation or model text. They must target an existing Session.
- Steer Shell actions must execute at the next eligible Step Boundary. Queue Shell actions must wait until the current Agent Turn ends.
- If no Agent Turn is active, eligible Shell actions must execute without starting the model.
- Eligible Shell actions must execute in their lane's acceptance order. Consecutive eligible Queue Shell actions must execute before the next queued model message starts its turn, with their outputs available in its model context.
- The one-model-message-per-turn Queue limit must not prevent consecutive Shell-only actions from executing.
- Accepted Shell actions must appear in Pending Work until execution starts and follow its shared capacity and process-loss rules.
- Rejection before acceptance must restore the Shell invocation for explicit retry. Stop must restore accepted Shell actions that have not started through ordinary human-input restoration.
- A Shell command that ran must record its success or nonzero exit through the user-Shell result contract in [Core Runtime And Tools](core-runtime-tools.md#core-tools-and-runtime-integration). It must not be retried automatically or restored as unexecuted.
- If Shell completes but Stop or terminal request-preparation failure prevents the next model request, Kent must record the completed Shell result in the Session without another model request or resubmission. Uncommitted model messages must keep their ordinary pending/restoration behavior.
- Stopping running Shell work must follow ordinary Shell interruption behavior without rollback or stronger commit-or-restore guarantees.
- A foreground shell process may run while later short Session mutations apply.
- A background process becomes independent after Kent reports it as backgrounded.
- Background completion is delivered when applicable and never blocks unrelated model or tool work.
- Manual compaction requests enter Pending Work and return after scheduling. A request admitted while an Agent Step is active enters the current Active Session Runtime in accepted order. It must not attempt to start another Agent execution.
- Each manual compaction request is distinct and receives its own later success or typed failure.
- Reviewer execution never delays delivery of the main answer.
- Reviewer feedback or failure arrives later if the originating Runtime is available.
- An Active-Runtime Worktree enter or leave enters Pending Work and returns the established Worktree Operation acknowledgement without waiting for the target change to finish.
- Attached clients later observe the authoritative target or typed failure.
- A live Session rebind must return a scheduled acknowledgement without waiting for the target change. A self-agent request must identify the exact originating Agent Step.
- A dormant-Session Worktree enter or leave is a direct Worktree operation.
- Worktree create and delete are direct Worktree operations outside Session mutation ordering.
- A live Workflow assignment applies in accepted Session order.
- Workflow Execution decides whether its Current Node start wins.
- Question answers and Approval decisions use their exact live owners and follow the shared contract in `core-runtime-tools.md`. They enter neither Pending Work nor the post-turn Queue. Nonblank Allow commentary is a separate ordinary Steering Intent accepted before Kent releases the waiting tool and applied at that Agent Step's normal Step Boundary.
- Instant Stop goes directly through Session Runtime Authority and returns after Stop is admitted.
- Instant Stop returns without waiting for cancellation cleanup or retirement.
- Live Workflow completion and exact tool output belong to the matching Agent Step and return their exact result synchronously.
- A validation failure before acceptance creates no Pending Work or Queue Item.
- A typed domain failure after acceptance completes only that operation and leaves unrelated later mutations accepted.
- Interactive submission is accepted against one current Active Session Runtime.
- Accepted Chat Send and Queue must return the input identity and a typed admission disposition: started turn, started Shell, pending Steer, or pending Queue.
- Immediate Shell-only admission must report started Shell rather than started turn. Pending Shell admission must report its Steer or Queue lane.
- The Runtime must determine this disposition at admission, including work already scheduled but not yet executing. The response must not infer it from a later activity read or wait for model output or client transcript hydration.
- A model message accepted through Queue while idle must report a started turn while retaining its explicit Queue intent.
- If an interactive Chat mutation, including submission, Queue, or manual compaction, targets a Session without an Active Session Runtime, the server prepares and opens one before admission.
- The server owns Runtime preparation. Clients never activate or release it.
- Prepared Runtime lifetime follows the server-owned idle policy in [Core Runtime And Tools](core-runtime-tools.md), whether or not Chat admission accepts the requested work.
- If Runtime replacement wins before acceptance, Kent resolves the replacement and retries admission without requiring another user submission.
- Authentication, metadata, execution-target, filesystem, tool, validation, Runtime-opening, or Runtime-publication failure before acceptance is terminal for that submission and creates no Pending Work or Queue Item.
- Core shutdown may cancel a Chat Operation before acceptance. That cancellation creates no Pending Work or Queue Item. A Session already committed during New Chat target resolution is discoverable.

## Turn-End Attention

- The server must publish one ephemeral Session turn-end attention event for each completed assistant-final Agent Turn with at least two tool calls.
- Turns with fewer than two tool calls, interruptions, and outcomes without an assistant final answer must not publish turn-end attention events.
- Turn-end attention events must carry the Session identity and assistant-final content for client-owned notification copy and navigation.
- The existing attention stream must deliver turn-end events globally to Desktop and by Session to Session-scoped observers, including when Desktop has no Chat open for that Session.
- Pending Queue messages must not delay turn-end events or consolidate events from separate completed turns.
- Turn-end events must not be persisted, replayed, or acknowledged. Disconnected clients must not receive missed turn-end events after reconnect.
- Clients must own notification copy, localization, and presentation. Notification delivery must not change Runtime work.

## Pending Work

- Pending Work is the process-local server-authoritative projection of accepted work that has not started.
- Pending Work contains only human messages, human Shell actions, manual compaction, and Active-Runtime Worktree enter or leave.
- Pending Work is separate from the internal Engine Intent Queue and has no persistence, replay, reconciliation, or connection-owned lifecycle.
- Every accepted queued action receives one identity.
- Desktop, TUI, and CLI callers must generate a UUID v4 Queue Item identity before submitting human Steer or Queue input. Kent must validate the supplied identity and accept it unchanged. Server-authored input must receive its identity at its origin.
- Kent must reject a supplied identity that belongs to existing pending or active input without replacing that input. After the input leaves pending and active work, this identity provides no replay or retry deduplication.
- Each user message committed from Queue Items must include their identities. Live delivery and later transcript reads must expose this same source-input provenance with the committed row identity.
- Existing transcript history must be unchanged. Historical messages and messages without originating Queue Items may have no source-input identities.
- The same identity represents that action in Pending Work, removal, compaction status, and Worktree acknowledgement or outcome wherever those surfaces apply.
- Each item carries a concrete typed operation rather than a generic command.
- A human-message item presents its exact submitted text.
- Manual compaction has canonical presentation `/compact` followed by its normalized guidance when guidance is present.
- Worktree enter has canonical presentation `/wt switch <selector>`.
- Worktree leave has canonical presentation `/wt leave`.
- Canonical presentation is independent of command alias, capitalization, spacing, or whether a control or typed command initiated the action.
- The server projects post-turn Queue items first in Queue order and then Steer items in server acceptance order across human messages, manual compaction, and Worktree transitions.
- Projection order promises no order between concurrent requests before server acceptance and no later execution or completion order.
- Human Send/Steer, post-turn Queue, manual compaction, and Active-Runtime Worktree enter or leave share a normal-admission capacity of 100 Pending Work items.
- Normal admission rejects when the server independently observes at least 100 items.
- Concurrent admissions that each observe a lower count may temporarily exceed 100.
- Kent never evicts accepted work to enforce the capacity.
- The Pending Work list route is the only client-facing source of a complete Pending Work collection.
- Transcript hydration and live notifications never carry a complete Pending Work collection.
- Kent broadcasts a payload-free Pending Work Changed notification after each completed membership change.
- A Pending Work Changed notification carries no order, revision, membership, or freshness guarantee.
- Every client that observes a Pending Work Changed notification fetches the latest collection for the exact Session associated with that notification.
- A client that receives a successful Send/Steer, Queue, manual-compaction, Active-Runtime Worktree, or discard response fetches the latest collection for that exact Session.
- Every accepted initial, replacement, reconnect, or Scratch Rehydration hydration clears the client's previous Pending Work collection and fetches the latest collection for the exact hydrated Session.
- An accepted hydration clears the previous collection even when the Session identity is unchanged because Pending Work belongs to the process-local Active Session Runtime.
- A Pending Work list result or failure from an earlier accepted hydration has no effect after a later hydration is accepted.
- After a failed Pending Work list read, the client may display only a collection fetched successfully during the current uninterrupted hydration scope.
- A failed initial read after an accepted hydration leaves the collection empty.
- Every item is discardable until its operation starts.
- Starting and discarding one item are mutually exclusive. Whichever the server accepts first wins.
- A discard after start fails because the item has left Pending Work.
- Successful discard removes only that item and returns its canonical presentation to the discarding client.
- A successful discard produces the removal response and a Pending Work Changed notification, after which clients fetch the latest collection.
- Pending Work removal never cancels an operation that has started.
- Discarding a Worktree transition before start emits no Worktree completion, failure, or cancellation outcome.
- Pending Work excludes running, completed, failed, canceled, and historical items.
- A malformed command or missing required field fails before acceptance.
- A Worktree selector resolves when its transition starts.
- Mutable Worktree safety and manual-compaction eligibility are revalidated when the operation starts.
- A user or validation failure after acceptance removes the item and reports the typed failure without restoring its canonical presentation.
- A definitely unapplied technical failure broadcasts one ephemeral source-agnostic restoration containing the canonical presentation to every connected client.
- Every client that observes the technical restoration restores the canonical presentation through its ordinary composer behavior.
- Kent must not target, persist, replay, or acknowledge the restoration. It must not count connected clients before broadcasting it.
- If no client observes the restoration, the canonical presentation is lost.
- An operation that committed is never restored because later publication or response delivery failed.
- If later Session state or response publication fails, a Worktree transition whose target applied is Completed.
- Kent surfaces the later Worktree publication diagnostic without restoring the command or reporting the applied transition as failed.
- If Worktree rollback fails, Kent reports neither Completed nor Failed and restores no command because the target and Runtime relationship is indeterminate.
- An indeterminate rollback retires only the affected Active Session Runtime before it can accept or execute more work.
- Mandatory Active Session Runtime retirement gives pending Goal notices and Goal failure feedback no special handling or delivery guarantee. Retirement must not wait for a final attempt or add Goal-specific validation, error handling, logging, atomicity, persistence outside the affected Runtime, retry, replay, or fallback for that work.
- An indeterminate dormant-Session transition returns its diagnostic without retiring a Runtime.

## Protected Agent Steps

- An Agent Step begins when Kent starts a provider request and ends after Kent handles the response, every caused tool call, and every committed tool result needed before another provider request.
- Model settings, tools, model context, execution target, Working Directory, and already-applied transcript input stay fixed for the complete Agent Step.
- Provider input stays fixed except for additional human instructions delivered through native steering.
- Ordinary Session mutations do not change those facts while the Agent Step is running.
- An accepted mutation that needs a Step Boundary waits until the running Agent Step ends.
- An already-running Agent Step is never preempted by Worktree work, compaction, or Reviewer work. Human input may use native steering as defined below.
- Exact Question, Approval, Stop, Goal, Workflow-completion, and caused-output rules use the matching live Exact Execution Scope described by their owning specifications.

## Native Human Steering

- On supported models at first-party OpenAI API-key and ChatGPT Codex OAuth endpoints, ordinary human Send/Steer must use native mid-turn steering during model generation.
- Unsupported models and other providers must use ordinary boundary delivery.
- Native steering must not change earlier output or cancel already-started tools.
- Post-turn Queue must use post-turn delivery. Inter-agent developer messages must use ordinary boundary delivery.
- Kent must assume submitted native steers arrived and must use existing error-to-composer restoration behavior on errors.
- Native steering must not add delivery reconciliation, automatic replay, or steering-specific recovery.
- Ordinary native steering must not introduce special ordering, concurrency, race, or atomicity guarantees.
- Active-target headless continuation must use the combined submission and observation contract in [CLI Commands](cli-commands.md#headless-run-and-shared-control). Start/finish-race coordination applies only to that operation.

## Step Boundaries And Next Work

- At each Step Boundary, Kent applies accepted boundary-required Session mutations in order until the next operation starts or all boundary-required mutations have applied.
- Mutations accepted while this processing is underway join the same acceptance-ordered drain.
- Human input without native steering normally applies at the first Step Boundary after acceptance.
- At an eligible Step Boundary, Kent must apply all accepted eligible Steer inputs together. This batch must not include post-turn Queue Items waiting for a later Agent Turn.
- Kent never begins a third provider request with accepted human text unapplied.
- Time spent inside an Agent Step or concrete long-running domain work counts as no additional provider request for that limit.
- Later short mutations may apply while a foreground shell process or Worktree transition is running.
- Starting another provider request is a separate decision from applying accepted mutations.
- Human Send/Steer requests ordinary model work without making the caller wait for model execution.
- Applicable post-turn Queue work keeps its own Queue order and ordinary after-turn eligibility.
- A Worktree transition has priority over accepted human model work waiting to start.
- An accepted live Session rebind must use the same execution-target transition priority for every caller.
- If human model work has already started its Agent Step, the Worktree transition waits for that Step to finish.
- After that Agent Step finishes, the Worktree transition receives the next eligible Step Boundary before another ordinary continuation.
- Later human messages are accepted and apply while the Worktree transition holds model-work eligibility.
- The Worktree transition must not select, merge, transfer, or execute human Pending Work.
- Foreground shell completion alone gives no permission for another provider request.
- Kent applies the foreground command's terminal output or typed failure to the Session before another provider request.
- Background execution reserves no Agent Step.
- A background terminal update follows ordinary Session timing when delivery is applicable.
- A requested manual compaction is selected as the next Agent Step ahead of an already-requested ordinary continuation.
- Kent applies accepted short mutations before selecting manual compaction and again after compaction before resuming ordinary work.
- Manual compaction and Worktree transitions start in server acceptance order relative to one another.
- Neither operational kind has priority over the other.
- Automatic compaction and Workflow Pre-Compaction keep the selection points defined by the Compaction and Workflow specifications.

## Workflow-Controlled Sessions

- A saved Workflow Session with no live Exact Execution Scope supports passive Session controls but cannot start an ordinary non-Workflow Agent execution.
- In that posture, eligible settings and passive Goal changes are available.
- Post-turn Queue and manual compaction fail before acceptance when no Workflow Agent Step can become eligible.
- Human Send/Steer reactivates the interrupted Current Node through the same Workflow Resume authority as the public Resume action.
- Saved-Session reactivation selects the exact Session, Task, Current Node, and parallel branch bound to that Session.
- Saved-Session reactivation must not validate, mutate, or start sibling Current Nodes.
- Kent accepts the original human input only after the selected Current Node publishes a matching fresh Workflow Agent execution.
- If selected preparation, admission, publication, or caller waiting fails first, Kent accepts no original input and starts no ordinary Session execution.
- Public Task Resume may resume every eligible interrupted Current Node on the Task and returns after durable requeueing.
- Public Task Resume returns without waiting for startup.
- While an accepted Workflow Agent assignment is preparing and no exact execution is live, Send/Steer and short settings are accepted.
- Post-turn Queue is unavailable while an accepted Workflow Agent assignment is preparing.
- The preparing posture begins after Workflow Execution accepts the assignment and before matching exact execution becomes live.
- The preparing posture ends when matching execution becomes live or preparation fails or is canceled.
- Preparation alone gives a Current Node no running, interruptible, or exact live-work status.
- No provider request starts until the matching assignment is admitted and every earlier accepted short Session mutation has applied.
- Competing or stale preparation may leave already-accepted Session changes.
- Changes accepted during competing or stale preparation must not be rolled back.
- The first durable Current Node admission wins among competing starts.

## Questions, Approvals, And Stop

- A pending Question or Approval belongs to its exact live Agent execution even when no model or tool step is active. It may be interrupted through the ordinary exact Interrupt behavior.
- Manual Move denies a pending Workflow Approval before applying the requested move.
- Every public Instant Stop route revalidates and admits Stop through Session Runtime Authority before it cancels an Agent execution.
- A stale Stop cannot cancel a successor execution.
- Session Runtime Authority must stop the selected live Agent execution, including a Question or Approval wait, or selected stoppable Runtime-owned work, including compaction and foreground shell execution.
- If no stoppable work exists, Interrupt must succeed with the authoritative Runtime state so clients can discard a stale busy projection.
- Runtime must panic in development when settled execution ownership contradicts running activity. In production, Runtime must interrupt the inconsistent activity and publish a transcript error through ordinary Runtime output handling.
- Valid startup, finalization, and compaction transitions must not trigger the ownership invariant.
- Only matching live Agent or Script execution authorizes Task Interrupt.
- Stop closes the matching execution to new associated human input before cancellation begins.
- Input accepted after Stop admission belongs to later Runtime work and is not removed by that execution's cleanup.
- A finalizing exact execution cannot be stopped.
- Repeating Stop during or after finalizing cleanup must not start another cleanup.
- Stop closes pending Question and Approval calls through the ordinary interrupted execution outcome.
- When the stopped execution reaches its boundary, Kent removes its pending human Send/Steer and post-turn Queue messages.
- Non-message Session mutations are accepted after Stop.
- Worktree operations already running must continue under their own owners after Stop.
- When Stop races a durable human-message append, Kent promises no single commit-or-restore outcome.
- Text involved in that race may commit, be returned for restoration, be duplicated, or be lost.
- Kent broadcasts removed human text once in an ephemeral interruption event with its item identities and text in server acceptance order.
- Each observing live client restores that text in order and then appends any text already in its composer.
- Kent must not persist, replay, acknowledge, or target the interruption event.
- A client or server that misses the interruption event loses the removed text.

## Reviewer

- Kent builds Reviewer input from the completed answer and the Session state at that answer boundary.
- The main answer commits and becomes visible before Reviewer execution finishes.
- The user may submit more input and ordinary model or tool work may continue while Reviewer runs.
- A Session runs at most one Reviewer request at a time.
- Another eligible answer is not queued for later review while one Reviewer is active.
- Reviewer failures must return through ordinary Session mutation order.
- Empty Reviewer feedback must not start or steer an Agent Turn.
- Nonempty Reviewer feedback must use ordinary Steering to join the active Agent Turn or start an Agent Turn through ordinary Session admission when idle.
- Reviewer feedback must not create a separate execution mode or extend the originating Agent Turn while waiting for review.
- An Agent Turn must become Supervisor-triggered when it accepts Reviewer feedback.
- Ordinary input must not clear the Supervisor-triggered flag during that Agent Turn.
- The Supervisor-triggered flag must clear when the Agent Turn ends and the Runtime becomes idle.
- A Supervisor-triggered Agent Turn must not trigger another Reviewer request.
- Reviewer activity is live best-effort state with values `inactive`, `invoking`, and `addressing_feedback`.
- Reviewer activity is `invoking` while the Reviewer model request is active.
- Reviewer activity must derive `addressing_feedback` from the active Agent Turn's Supervisor-triggered flag.
- Reviewer activity returns to `inactive` when neither a Reviewer request nor a Supervisor-triggered Agent Turn is active, or the Runtime closes.
- The active TUI shows Reviewer activity during `invoking` and `addressing_feedback`.
- Reviewer activity creates no transcript lifecycle row. Runtime replacement, reconnect, transcript hydration, and application restart discard it.
- Persistent Reviewer activity across later lifecycle boundaries or reconnects is outside this specification.

## Worktree Ownership And Deletion

- Worktree management completely owns Worktree create. Creation must not change a Session target.
- Active-Runtime Worktree enter and leave use Pending Work and follow the priority and non-preemption rules in this specification.
- Worktree deletion never obtains permission by joining Session mutation order.
- Deletion fails without waiting or stopping work when a targeting Active Session Runtime is executing, applying or holding accepted Session mutations, has selected or begun provider or tool work, or is held for Workflow control.
- A targeting Active Session Runtime that is truly idle is retired before its durable Session is retargeted.
- Kent retargets dormant Sessions before physical deletion.
- A targeting Session without an Active Session Runtime is retargeted directly in durable state before physical deletion.
- A live background process whose Working Directory is inside the target Worktree blocks deletion.
- If human input is accepted first, deletion fails.
- If deletion retires and retargets first, later input uses the new target.
- Worktree operations are independent requests. Each repeated request is distinct.
- Kent must not replay, deduplicate, or reconcile an ambiguous Worktree retry.

## Fast Mode And Context Selection

- Fast Mode is available only when the active provider supports Kent's fast-request behavior.
- A successful Fast Mode change becomes the Session's authoritative Chat setting and affects the next provider or compaction request, including after the Session is reopened.
- Enabling unsupported Fast Mode fails without changing the Session setting.
- Fast Mode changes do not alter an Agent Step already running and do not create a new Session Contract generation or prompt-cache identity.
- Supported provider requests use the provider's priority service tier when Fast Mode is enabled and omit that tier when it is disabled.
- Context usage and compaction selection use the Session's current provider-reported usage or established current-context estimate and the configured thresholds.
- Compaction timing must not use forecasts of future token growth from previous turns.
- Automatic compaction keeps the current consumed-context threshold and rechecks current eligibility before it starts.

## Failure And Loss

- Session durability has no Kent-imposed timeout.
- A slow durable write may delay the operation without becoming a synthetic timeout failure.
- A definitely uncommitted required Session mutation starts no later provider request, fails pending operation results, returns restorable input through its ephemeral restoration event, and closes the Runtime without automatic retry, replay, or recovery.
- If later publication or reply delivery fails, a committed mutation is authoritative.
- Kent reports a failure after a committed mutation without applying the mutation again.
- If Kent cannot determine whether an authoritative Session mutation committed outside the indeterminate Worktree rollback case defined by this specification, it terminates the backend with diagnostic context.
- Process death may lose pending mutations, commands, Questions, Reviewer work, partial Agent Turns, and ephemeral input restoration.
- Kent must not reconstruct or replay work lost on process death.
- Except for the Pending Work normal-admission limit defined here, accepting or retaining the 10,000th pending Session mutation for one Session terminates the backend as an invariant violation.
- Kent adds no other smaller capacity limit, eviction, persistence, retry, or recovery behavior.
