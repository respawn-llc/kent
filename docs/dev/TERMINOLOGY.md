# Terminology

This document defines Builder product/domain language. Use these terms consistently in specs, code names, CLI/API contracts, and implementation plans.

## Async Workflow Orchestration

### Task

A durable user-facing unit of work. In the future frontend, a task is the Kanban card. A task owns workflow state, task metadata, node history, edge-transition history, questions, and execution artifacts. Existing Builder sessions are artifacts under a task, not the task itself.

### Workflow

A durable directed graph that describes how tasks move through work. A workflow contains nodes and edges. V1 workflow definitions are SQLite-authoritative and created/edited through backend API and a minimal CLI; frontend workflow editing comes later.

### Node

A visible workflow state and Kanban column/status. V1 intentionally keeps node identity equal to visible column identity to avoid ambiguity when a human moves a task. A task is in exactly one current node during normal linear execution, with join/parallel execution represented by child run state rather than dynamically created columns.

A node configures agent-run behavior:

- subagent role reference and validation policy;
- node prompt/template;
- model/provider/auth/settings overrides where supported;
- enabled tools/tool policy;
- timeout, turn limit, retry policy, and stop conditions;
- worktree/session execution policy that applies while this node runs.

Completing a node means its configured run reached a structured terminal outcome, not that an assistant wrote a natural-language final answer.

### Edge

A directed transition from one completed node to a next node. Edges configure transition behavior, not the agent run itself.

An edge configures:

- target node;
- whether transition needs human approval or another manual interaction;
- context-preservation mode for the next node;
- input/output bindings between prior node output, task metadata, and next node prompt/context;
- routing condition or decision mapping;
- join/aggregation requirements when multiple inbound branches must complete.

### Context-Preservation Mode

Per-edge handoff policy that decides how the next node receives execution context:

- `new_session`: start a blank Builder session and inject previous node output plus task metadata.
- `continue_session`: continue the previous Builder session with a new prompt/goal and bound metadata.
- `compact_and_continue_session`: compact the previous session first, then continue with a new prompt/goal and bound metadata.

### Run

One durable execution attempt for a node on a task. A run may create or continue a Builder session, call tools, ask questions, produce structured node output, and terminate with a structured outcome.

### Session

Builder transcript/runtime artifact used by a run. A task may have many sessions due to loops, branches, retries, or context-preservation choices.

### Handoff

Construction of the next run's execution context from prior run output, task metadata, edge input/output bindings, and edge context-preservation mode.

### Join

An edge or node transition point that waits for all required inbound branch outputs before continuing. V1 does not support racing; aggregation waits for all required inputs.

### Question

A user-blocking ask emitted by a run. Questions pause the affected run/task path until answered through shared prompt-control infrastructure. The future frontend should show questions grouped by task.

### Orchestrator

An ordinary agent node whose prompt asks it to coordinate work. V1 does not dynamically create workflow nodes or Kanban columns. Orchestration uses existing subagent/session infrastructure inside an agent run or through statically defined graph branches.

### Terminal State

A workflow/task state where auto-execution stops because the task is done, canceled, failed, blocked, or awaiting manual/user action.
