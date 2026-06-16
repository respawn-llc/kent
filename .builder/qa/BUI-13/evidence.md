# BUI-13 QA Evidence
date: 2026-06-16T17:14:12Z
worktree: /Users/nek/.kent/worktrees/builder-cli-c2f75fc8-68f5-4deb-a23c-21cc5820436d/BUI-13

## git status --short
 M docs/dev/specs/core-runtime-tools.md
 M server/llm/errors.go
 M server/llm/errors_test.go
 M server/runtime/compaction.go
 M server/runtime/compaction_executor.go
 M server/runtime/engine.go
 M server/runtime/meta_context_architecture_test.go
 M server/runtime/output_steering.go
 M server/runtime/reviewer_pipeline.go
 M server/runtime/step_executor.go
 M server/runtime/transcript_runtime_state.go
 M server/session/event_log.go
 M server/session/store.go
 M shared/llmerrors/errors.go
?? .builder/
?? server/runtime/missing_tool_output_repair.go
?? server/runtime/missing_tool_output_repair_integration_test.go
?? server/runtime/missing_tool_output_repair_test.go
?? server/session/event_rewrite_test.go

## QA Commands

- `go test -v ./shared/llmerrors ./server/llm ./server/session ./server/runtime -run 'Test(HasHTTPStatus|AnalyzeAndRewriteEvents|NormalGenerationHTTP400RepairsMissingToolOutputRebuildsAndRetries|ReviewerHTTP400RepairsMissingToolOutputRebuildsAndRetries|CompactionHTTP400RepairsMissingToolOutputRebuildsAndRetries|ExactTokenCountHTTP400RepairsBeforeDiagnosticAndRetries|IneligibleActiveToolTokenCountHTTP400DoesNotRepairButLaterGenerationCan|CurrentTokenCountWithoutRepairEligibilityDoesNotRepairHTTP400|NormalGenerationNon400AndUnrelated400DoNotRepair|MissingToolOutputRepair)'` -> pass (`.builder/qa/BUI-13/relevant-go-test-v.log`)
- `./scripts/test.sh ./shared/llmerrors ./server/llm ./server/session ./server/runtime` -> pass (`.builder/qa/BUI-13/package-script-tests.log`)
- `./scripts/build.sh --output ./bin/kent` -> pass (`.builder/qa/BUI-13/build.log`)

## Acceptance Evidence Mapping

- Provider-generic HTTP 400 detection across current and legacy API error shapes: `TestHasHTTPStatus`.
- Repair attempted only after eligible HTTP 400 and request rebuilt/retried for normal generation, reviewer, compaction, and exact token counting: `TestNormalGenerationHTTP400RepairsMissingToolOutputRebuildsAndRetries`, `TestReviewerHTTP400RepairsMissingToolOutputRebuildsAndRetries`, `TestCompactionHTTP400RepairsMissingToolOutputRebuildsAndRetries`, `TestExactTokenCountHTTP400RepairsBeforeDiagnosticAndRetries`.
- No preflight/ineligible active-tool repair and no repair for non-400 or unrelated 400 failures: `TestIneligibleActiveToolTokenCountHTTP400DoesNotRepairButLaterGenerationCan`, `TestCurrentTokenCountWithoutRepairEligibilityDoesNotRepairHTTP400`, `TestNormalGenerationNon400AndUnrelated400DoNotRepair`.
- Persisted transcript rewrite behavior, atomic post-rewrite warning append, monotonic sequence handling, no-op behavior, and post-commit observer failure handling: `TestAnalyzeAndRewriteEvents*`.
- Missing-output repair scenarios: unfinished function call removal and warning append, partial multi-call preservation, empty assistant message dropping, reasoning/content preservation, matching materialized outputs, custom output kinds, materialized-output precedence, order-insensitive `tool_completed`, outer-call validation, latest compaction boundary behavior, legacy/malformed `history_replaced` handling, runtime projection/derived state reload, and append-only warning emission: `TestMissingToolOutputRepair*`.
