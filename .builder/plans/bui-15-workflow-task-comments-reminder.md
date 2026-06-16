## Definition

- Recon focus: GH #361 asks workflow agents to be made aware that task comments exist without injecting all comment bodies into model context.
- Current workflow-mode developer instructions are embedded at `prompts/workflow/workflow_task_instructions.md` and rendered by `prompts.RenderWorkflowTaskInstructions` in `prompts/embed.go`.
- Workflow run prompt context currently flows through `server/workflowrunner.BuildWorkflowTaskInstructions` into `workflowruntime.TaskInstructions`, then through `server/runtime.workflowTaskInstructionsContent` into `prompts.WorkflowNodeContextArgs`.
- Existing task prompt fields are `TaskId`, `TaskShortId`, `TaskTitle`, `TaskBody`, workflow/node metadata, transitions, and node prompt. There is no existing `TaskNumberOfComments` field in `prompts.WorkflowNodeContextArgs`, `workflowruntime.TaskInstructions`, `workflowstore.RunStartContext`, or transition prompt placeholder validation.
- Task comments already exist as durable task-local notes in `server/workflowstore/comments.go`, `server/workflowsvc/service.go`, and CLI commands in `cli/kent/task_command.go`.
- The CLI command found in the current codebase is `kent task comment list <short-id-or-task-id>`, singular `comment`. The imported GH body used ``kent task comments list {{.TaskShortId}}``, but the task's current comment corrects that to `kent task comment list`.
- `kent task show` currently inlines comments for fewer than 10 comments and prints a pull-based pointer for 10 or more comments. That behavior is separate from workflow-mode prompt injection.
- `docs/dev/specs/workflow-orchestration.md` currently says task comments are not automatically injected into agent context and agents read comments through CLI/API when needed. The new behavior preserves that body-not-injected design while adding a count and explicit pull command breadcrumb.

Completion criteria for the eventual implementation:

- Workflow-mode developer instructions include a concise comment-discovery line when rendered for a task with visible comments, using the task short ID and current visible comment count.
- Comment bodies are not injected into the workflow-mode prompt.
- The rendered line points agents at the existing singular `task comment list` command, and the typo-prone plural `task comments` group is also supported as an alias for the existing task comment command group.
- Comment count is sourced from task persistence/read model each time workflow instructions are appended or re-appended, not derived by parsing CLI output or loading comments into prompt text.
- Existing workflow prompt rendering still substitutes all current fields, embeds completion-mode instructions, and leaves no unresolved `{{...}}` placeholders.
- Tests cover rendering of the comment count/command in workflow instructions and propagation into runtime-appended workflow instructions, including the re-append count refresh path.
- `TaskNumberOfComments` remains internal to workflow-mode reminder rendering; no transition prompt placeholder validation or GUI placeholder chip changes are required.
- Relevant workflow documentation/specs are updated if the model-facing contract or command wording changes.

## Design

- Locked decision: render the workflow-mode reminder with the existing singular command shape `kent task comment list`. On 2026-06-16, after reconciling the imported GH body with the task comment `typo: kent task comment list`, the user chose to support both command groups but render the existing singular command in the model-facing reminder.
- Locked decision: show the workflow reminder only when the task has at least one visible comment. Do not add a zero-comment reminder line.
- Locked decision: use an existing returned comment count if one is already available on the relevant path; otherwise count the visible comments returned by the query. Do not count deleted/tombstone comments.
- Locked decision: keep `TaskNumberOfComments` internal to workflow-mode developer reminder rendering. Do not expose it as a user-authored transition prompt placeholder, and do not add it to workflow editor placeholder chips.
- Locked decision: use grammatically correct reminder copy for the count, rendering `1 comment` and `N comments` while preserving the ticket's meaning.
- Locked decision: add `kent task comments` as an alias for the full existing `kent task comment` subcommand group, not only for `list`, while keeping `kent task comment ...` as the canonical reminder command.
- Locked decision: re-query the visible comment count each time the workflow prompt is appended, including re-append/steer paths such as compaction, without causing avoidable prompt-cache invalidations.
- Locked decision: render the runnable reminder command with `{{.LaunchCommand}} task comment list {{.TaskShortId}}`, not a literal `kent`, so the command matches the active Kent binary/path like existing workflow-mode instructions.

## Architecture

- Existing structure and ownership:
  - `workflowstore` remains the persistence boundary for task comments. Comments are hard-deleted task-local notes, so "visible/current comment count" is the count of rows in `task_comments` for the task ID.
  - `workflowrunner` remains the adapter that starts workflow runtimes from durable run/task context. It should wire comment-count access into the runtime, but it should not snapshot the count into the run-start data as an immutable fact.
  - `workflowruntime` owns runtime-facing workflow contracts. It is the right package for a small count-provider interface because `server/runtime` already depends on `workflowruntime`, and `workflowrunner` can satisfy the interface with `workflowstore.Store`.
  - `server/runtime` remains the only place that appends/re-appends model-visible workflow-mode context. It should resolve the current comment count immediately before building a workflow-mode message.
  - `prompts` owns the embedded workflow instruction template and render-only template data. The comment count field belongs in the workflow instruction args, not in user-authored transition prompt data.
  - `cli/kent` owns the command alias. The alias should reuse the existing task comment command implementation and API calls; no new service/RPC method is required for plural `task comments`.

- Data contracts:
  - Add a count-only persistence query, for example `CountTaskComments`, to `server/metadata/queries.sql`, regenerate `server/metadata/sqlitegen`, and expose it as `workflowstore.Store.CountTaskComments(ctx, taskID) (int64, error)`.
  - Define a small runtime contract in `server/workflowruntime`, for example:
    - `type TaskCommentCounter interface { CountTaskComments(context.Context, workflow.TaskID) (int64, error) }`
    - `workflowruntime.Config` gets an optional `TaskCommentCounter TaskCommentCounter` field.
    - Do not add `TaskNumberOfComments` to `workflowruntime.TaskInstructions`; that struct remains stable run-start/task metadata built from `RunStartContext`.
  - `workflowrunner.RuntimeStore` should include `CountTaskComments` so the starter has an explicit compile-time dependency on the count capability. `Starter.run` passes `TaskCommentCounter: s.store` into `workflowruntime.Config`.
  - `prompts.WorkflowNodeContextArgs` gets `TaskNumberOfComments int64`. Runtime rendering passes this fresh count separately from `workflowruntime.TaskInstructions`. If the template needs grammar support, keep derived grammar such as `TaskCommentsNoun` in the render-only template data produced by `RenderWorkflowTaskInstructions`, not in transition prompt rendering or GUI placeholder definitions.

- Runtime count-refresh control flow:
  - `BuildWorkflowTaskInstructions` should keep building stable task/node/transition instructions from `RunStartContext`; it should not query or freeze the comment count.
  - Add an engine helper that queries `TaskCommentCounter.CountTaskComments(ctx, workflow.TaskID(e.cfg.WorkflowRun.Instructions.TaskID))` and returns the current count as a separate render value. It must not mutate `e.cfg.WorkflowRun.Instructions`.
  - `steerWorkflowModeIfNeeded(ctx, stepID)` uses the helper only after it determines that a workflow-mode message for the active run is actually going to be appended. If a current run-scoped workflow message already exists, it returns without querying because no append occurs.
  - `compactionReinjectedMetaMessages(ctx)` uses the same helper before asking `metaContextBuilder` to include workflow meta messages in the replacement active list. This makes manual/remote compaction re-inject the current count without mutating old model-visible items.
  - `workflowModeMetaMessage` and `workflowTaskInstructionsContent` should accept a render-only comment-count argument and pass it into `prompts.WorkflowNodeContextArgs`. If `metaContextBuilder.Build` remains responsible for including workflow messages, add a render-only `WorkflowTaskCommentCount` option rather than storing the count in `workflowruntime.TaskInstructions`.

- Prompt rendering:
  - `prompts/workflow/workflow_task_instructions.md` conditionally renders a short comment-discovery reminder only when `gt .TaskNumberOfComments 0`.
  - The reminder uses the active launch command and the existing singular command shape: `{{.LaunchCommand}} task comment list {{.TaskShortId}}`.
  - The template renders grammatically correct labels for `1 comment` and `N comments`.
  - Comment bodies, comment IDs, authors, timestamps, and previews are not included in workflow-mode prompt args.

- CLI alias flow:
  - `taskSubcommand` dispatches both `comment` and `comments` to the existing comment subcommand group.
  - The alias applies to `add`, `list`, `replace`, and `delete` because routing happens at the group boundary.
  - Help text should document both forms, while keeping one implementation and one API surface. Existing singular commands remain supported.

- Failure behavior:
  - If a workflow run has no `TaskCommentCounter` or no task ID, render as zero comments. This keeps unit-test and non-store runtime construction simple; production workflow runs created by `workflowrunner.Starter` should always provide the counter and task ID.
  - If `CountTaskComments` returns an error, propagate it from the append/re-append path. Do not silently treat a query failure as zero comments because that hides durable task context. Initial workflow prompt append failure should fail the workflow turn/start path; compaction re-injection failure should fail before committing a partial replacement.
  - A count is point-in-time. Comments added or deleted after a prompt append are reflected at the next workflow prompt append/re-append, not retroactively in old transcript items.

- Cache and transcript implications:
  - Do not scan, edit, filter, or replace prior workflow prompt messages to refresh the count. Existing persisted conversation items stay immutable and ordered.
  - Initial prompt append changes only the new run-scoped workflow-mode message. Compaction already creates a new active list at an explicit cache boundary, so including the freshly counted workflow reminder there preserves existing cache-continuity rules.
  - The workflow-mode message `SourcePath` continues to be the active run ID, so duplicate prevention remains run-scoped and existing "new run after old workflow prompt" behavior is preserved.

- Testing seams implied by the architecture:
  - Store tests cover `CountTaskComments` after add and delete to prove hard-deleted comments are not counted.
  - Prompt tests cover conditional rendering, command shape, and singular/plural grammar through rendering invariants rather than asserting the entire prompt text.
  - Runtime tests use a fake `TaskCommentCounter` on `workflowruntime.Config` to prove initial append and compaction re-injection query the count at append time and do not mutate or duplicate old workflow messages.
  - Runtime error tests cover count-provider failures on initial workflow prompt append and compaction re-injection; compaction must not commit a partial history replacement when count resolution fails.
  - Runner tests verify `BuildWorkflowTaskInstructions` leaves the count unset/static and `Starter` wires the store as the runtime count provider.
  - CLI tests verify `task comments list` routes to the same list implementation and at least one mutating subcommand path proves the whole group is aliased.

## Planning

Execute this as vertical TDD slices: for each slice, add the narrow failing behavior test first, implement only the code needed to make that slice green, then refactor before moving to the next slice. Do not bulk-write all tests up front.

- [x] Slice 1: prompt-rendering contract.
  - Files: `prompts/embed_test.go`, `prompts/embed.go`, `prompts/workflow/workflow_task_instructions.md`.
  - RED: add focused prompt-rendering tests for `TaskNumberOfComments` behavior:
    - zero comments do not render a comment-discovery reminder;
    - one comment renders a reminder with `1 comment`;
    - multiple comments render `N comments`;
    - the runnable command uses `selfcmd.LaunchCommand()` plus `task comment list <short-id>`.
  - GREEN: add `TaskNumberOfComments` to `prompts.WorkflowNodeContextArgs`, derive any grammar helper inside `RenderWorkflowTaskInstructions`, and conditionally render the reminder in the embedded workflow prompt.
  - Completion criterion: `./scripts/test.sh ./prompts` passes, and tests assert behavioral fragments/invariants rather than the full prompt body.
  - Progress: implemented and verified with `./scripts/test.sh ./prompts`.

- [x] Slice 2: count-only comment persistence read.
  - Files: `server/metadata/queries.sql`, `server/metadata/sqlitegen/*`, `server/workflowstore/comments.go`, `server/workflowstore/store_test.go`.
  - RED: add store-level tests proving `CountTaskComments` returns `0` for a task without comments, increments after `AddComment`, and decreases after `DeleteComment`.
  - GREEN: add a sqlc `CountTaskComments` query over `task_comments` by `task_id`, regenerate `server/metadata/sqlitegen`, and expose `workflowstore.Store.CountTaskComments(ctx, workflow.TaskID) (int64, error)`.
  - Completion criterion: `./scripts/test.sh ./server/metadata ./server/workflowstore` passes.
  - Progress: implemented and verified with `./scripts/test.sh ./server/metadata ./server/workflowstore`.

- [x] Slice 3: runtime count-provider contract and initial append refresh.
  - Files: `server/workflowruntime/completion.go`, `server/runtime/meta_context_runtime.go`, `server/runtime/meta_context.go`, `server/runtime/workflow_completion_test.go`.
  - RED: add runtime tests with a fake comment counter proving the workflow-mode prompt appended for a workflow turn uses the current counter value, an existing run-scoped workflow prompt prevents both duplicate append and unnecessary counter query, and a counter error fails the initial workflow prompt append without appending a workflow-mode message.
  - GREEN: add `workflowruntime.TaskCommentCounter` and `workflowruntime.Config.TaskCommentCounter`; add an engine helper that resolves the current count only at append time and passes it as a separate render value into the existing workflow prompt renderer without modifying `workflowruntime.TaskInstructions`.
  - Completion criterion: `./scripts/test.sh ./server/runtime` passes for the new initial-append tests.
  - Progress: implemented and verified with `./scripts/test.sh ./server/runtime`.

- [x] Slice 4: compaction/re-append count refresh.
  - Files: `server/runtime/engine_part15_test.go`, `server/runtime/meta_context_runtime.go`.
  - RED: add or extend compaction tests to prove workflow prompt re-injection queries the counter again and uses the updated count after comments change; also assert prior persisted workflow messages are not edited or duplicated.
  - RED: add a counter-error compaction test proving `compactNow` returns the count error and does not commit a history replacement, workflow prompt replacement, or other partial active-list mutation.
  - GREEN: update `compactionReinjectedMetaMessages(ctx)` to use the same current-count helper before including workflow meta messages in the replacement active list.
  - Completion criterion: `./scripts/test.sh ./server/runtime` passes, including the compaction re-injection test.
  - Progress: implemented and verified with `./scripts/test.sh ./server/runtime`.

- [x] Slice 5: workflow runner wiring.
  - Files: `server/workflowrunner/starter.go`, `server/workflowrunner/starter_test.go`.
  - RED: add an integration-style workflow runner test that creates a task with comments, starts the run, and observes that the first model request includes the comment-count reminder from the store-backed count provider.
  - GREEN: add `CountTaskComments` to `workflowrunner.RuntimeStore`, pass `TaskCommentCounter: s.store` into `workflowruntime.Config`, and keep `BuildWorkflowTaskInstructions` count-free so the count is not snapshotted at run-start.
  - Completion criterion: `./scripts/test.sh ./server/workflowrunner` passes, and coverage proves the model-visible count comes from the runtime count provider rather than `BuildWorkflowTaskInstructions` or `RunStartContext`.
  - Progress: implemented and verified with `./scripts/test.sh ./server/workflowrunner`.

- [x] Slice 6: plural CLI alias.
  - Files: `cli/kent/task_command.go`, `cli/kent/help/task.txt`, `cli/kent/workflow_command_test.go`.
  - RED: add CLI tests proving `kent task comments list <task>` routes to the same list behavior and that at least one non-list subcommand, such as `add`, also works through the plural group.
  - GREEN: dispatch both `comment` and `comments` to the same task comment subcommand group and update task help to document both accepted command forms without adding new API/RPC methods.
  - Completion criterion: `./scripts/test.sh ./cli/kent` passes.
  - Progress: implemented and verified with `./scripts/test.sh ./cli/kent`.

- [x] Slice 7: documentation/spec reconciliation.
  - Files: `docs/dev/specs/workflow-orchestration.md`, and only other docs if implementation changes an operator-visible command or behavior beyond the existing spec edits.
  - RED: no new automated test required unless a doc linter exists in the standard verification path.
  - GREEN: confirm the spec still states the shipped behavior: bodies are not injected, count plus singular pull command appears only when comments exist, count is re-queried on append/re-append, and both singular/plural CLI groups are accepted.
  - Completion criterion: `git diff -- docs/dev/specs/workflow-orchestration.md` matches final behavior and contains no implementation-history wording.
  - Progress: reconciled against the implemented behavior; existing spec diff matches shipped behavior.

- [x] Slice 8: end-to-end verification and build.
  - Run targeted packages after each slice as listed above.
  - Before handoff after Go implementation, run the broader relevant test set: `./scripts/test.sh ./prompts ./server/metadata ./server/workflowstore ./server/workflowruntime ./server/runtime ./server/workflowrunner ./cli/kent`.
  - Build the binary with `./scripts/build.sh --output ./bin/kent`.
  - Completion criterion: all targeted tests and the build pass, or failures are documented with exact command output and the remaining blocker.
  - Progress: verified with `./scripts/test.sh ./prompts ./server/metadata ./server/workflowstore ./server/workflowruntime ./server/runtime ./server/workflowrunner ./cli/kent` and `./scripts/build.sh --output ./bin/kent`.

Delegable workstreams if this exceeds one agent's working memory:

- CLI alias workstream: Slice 6 is independent after the product decision is locked and can be implemented without touching runtime/store files.
- Prompt rendering workstream: Slice 1 can proceed independently of store/runtime wiring once the field name `TaskNumberOfComments` is accepted.
- Store/runtime workstream: Slices 2-5 should stay together or be handed off in order because the runtime provider contract depends on the store count method and runner wiring.
