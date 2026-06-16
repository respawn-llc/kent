# BUI-13 QA Summary

QA date: 2026-06-16

## Commands

- `./scripts/test.sh ./server/runtime -run 'Test.*MissingToolOutput|Test.*HTTP400|Test.*Repair' -count=1`
  - Result: `exit_status=0`
  - Evidence: `.builder/qa/BUI-13-runtime-repair-tests.log`
- `go test ./server/runtime -list 'Test.*MissingToolOutput|Test.*HTTP400|Test.*Repair'`
  - Result: `exit_status=0`
  - Evidence: `.builder/qa/BUI-13-runtime-repair-test-list.log`
- `./scripts/test.sh ./shared/llmerrors ./server/llm ./server/session ./server/runtime`
  - Result: `exit_status=0`
  - Evidence: `.builder/qa/BUI-13-targeted-packages.log`
- `./scripts/build.sh --output ./bin/kent`
  - Result: `exit_status=0`
  - Evidence: `.builder/qa/BUI-13-build.log`
- `./scripts/test.sh`
  - Result: `exit_status=1`
  - Failure: frontend `apps/desktop` test `src/features/workflows/WorkflowLibraryRoute.test.tsx > WorkflowLibraryRoute > refreshes the mounted workflow list after saving from the sidebar editor` timed out after 5000ms.
  - Evidence: `.builder/qa/BUI-13-full-test.log`
- `pnpm --dir apps/desktop test -- src/features/workflows/WorkflowLibraryRoute.test.tsx -t "refreshes the mounted workflow list after saving from the sidebar editor"`
  - Result: `exit_status=1`
  - Same timeout reproduced.
  - Evidence: `.builder/qa/BUI-13-frontend-failing-rerun.log`

## Acceptance Coverage Observed

The runtime repair test list includes coverage for normal generation, reviewer generation, compaction, exact token counting, active-tool ineligible token counting, non-400 and unrelated 400 no-op behavior, clearing streaming deltas, missing function/custom calls, multi-call partial repair, reasoning/content preservation, materialized-output precedence, order-insensitive `tool_completed`, outer-call provider item validation, latest valid compaction boundary, legacy boundary handling, malformed boundary abort, runtime projection reload/derived state, and append-only warning event emission.

## Verdict

Ticket-specific Go/runtime acceptance checks passed and the required Go build passed. The repository-wide `./scripts/test.sh` check failed in an unrelated frontend workflow-library test and reproduced on rerun, so this QA result is not a clean repository pass.
