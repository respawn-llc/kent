# Async Workflow Orchestration

## Purpose

Design Builder's backend foundation for asynchronous, configurable agent pipelines before frontend implementation. The feature turns Builder from a manually driven terminal coding-agent harness into an orchestrator for project-scoped workflows where tasks move through graph nodes, Kanban statuses, agent workers, review loops, and merge/cleanup stages.

Frontend design is intentionally out of scope for this document except where backend contracts must support later workflow/Kanban UI, question inbox, task views, and status visualization.

## Current Idea

- Users define workflows made of nodes and transitions.
- Nodes can map to Kanban statuses; several nodes may share one status.
- A task entering an auto-runnable node can start an agent or other executor automatically.
- Agent nodes use configured subagent roles, custom prompts, dynamic completion tools, and goal-like autonomous looping.
- Completion tools return structured decisions such as advance, send back, request user input, fail, or hand off to another workflow branch.
- Review nodes can emit findings and move tasks back to implementation; architecture/design nodes can send underspecified work back to design.
- Work should run asynchronously through a queue with global/project/workflow concurrency limits to avoid rate limits.
- Work should use Builder's existing project, workspace, worktree, session, goal, ask_question, background process, and server architecture.

## External Reference

Anthropic's "Building Effective AI Agents" article is useful vocabulary for workflow families Builder should support: prompt chaining, routing, parallelization/sectioning/voting, orchestrator-workers, evaluator-optimizer, and autonomous agents. Builder's design should model these as composable workflow graph/execution primitives rather than separate products. The article also reinforces two constraints that match Builder's direction: keep orchestration simple/transparent and invest heavily in model-facing tool interfaces.

Source: https://www.anthropic.com/engineering/building-effective-agents

## Known Constraints

- Durable domain identity is already `project > workspace > worktree`; async tasks must fit this model instead of reintroducing workspace-scoped identity.
- SQLite is authoritative for structured metadata; large append-only artifacts remain file-backed.
- Full transcript history can be gigabytes; workflow code must not load `events.jsonl` fully into memory.
- Existing goal mode is user/session-oriented and requires `ask_question`; workflow agents likely need a server-owned autonomous task loop with similar nudge semantics but different completion authority.
- Current subagent roles configure model/provider/tool/settings overlays, not workflow-specific node behavior.
- Existing `RunPromptService` is one-shot headless prompt execution and returns a final assistant string; async workflow agents need durable, resumable runs with structured terminal events.
- Workflow definitions must validate referenced subagent roles before execution. If config changes remove or invalidate a role, affected workflow runs should fail fast with actionable validation errors rather than silently substituting another agent.
- Each workflow edge may specify handoff mode for the next node: start a new blank session with previous output/task metadata, continue the prior session with a new prompt/goal, or compact then continue the prior session with metadata.

## Open Questions

- What CLI surface is enough to test v1 without building frontend?
- Which node types exist in v1 beyond agent execution?
- How flexible should per-node inputs/outputs be without exposing arbitrary JSON schema building?
- How should user questions pause/resume tasks across many concurrent agents?
- How should worktrees, branches, commits, PRs, and cleanup attach to tasks?
- How should queueing, retries, cancellations, and resource cleanup behave?
- What is the migration path from current sessions/goals/subagents into workflow runs?

## Product Decisions

Decisions will be recorded here during the planning interview.

- V1's smallest testable vertical slice is backend/API/CLI first: create a task, auto-run at least one agent node in a worktree, capture structured completion, and move task status. The CLI can be clunky and removable; it exists to test backend behavior before GUI investment.
- `Task` is the primary durable work item. Existing Builder sessions are execution artifacts under tasks, not the task itself. One task may accumulate many sessions through loops, branches, retries, and complex chains.
- Moving a task from backlog to to-do should auto-run through auto nodes until the task reaches a terminal state or blocks on a user question, manual gate, error, capacity limit, or other explicit stop condition.
- Workflow definitions may rely on TOML-configured subagent roles. This creates config drift risk; v1 accepts fail-fast validation rather than inventing a full stable workflow file/schema solution immediately.
- Builder should support the major agentic workflow patterns from the Anthropic article in some form: prompt chaining, routing, parallelization with aggregation, orchestrator-workers, evaluator-optimizer loops, and open-ended autonomous agents.
- Per-edge session handoff must be configurable in v1 with at least three modes: `new_session`, `continue_session`, and `compact_and_continue_session`.

## Domain Vocabulary

- `Task`: durable user-facing unit of work shown as a Kanban card and tracked through workflow state.
- `Workflow`: reusable graph definition for how tasks move through nodes and transitions.
- `Workflow Node`: one executable or manual step in a workflow graph; may map to a Kanban status.
- `Workflow Edge`: transition between nodes; carries routing conditions and session handoff mode.
- `Run`: one durable attempt to execute a node for a task.
- `Session`: Builder transcript/runtime artifact created or continued by a run.
- `Handoff`: construction of the next run's execution context from prior task metadata, node outputs, and optional session continuation.
- `Question`: user-blocking ask emitted by an agent/run and answered later through shared prompt-control infrastructure.

## Backend Architecture Draft

To be drafted after PRD decisions are explicit.

## Data Model Draft

To be drafted after PRD decisions are explicit.

## Implementation Plan

To be drafted after architecture review.
