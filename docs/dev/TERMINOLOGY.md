# Terminology

This document defines Builder product and domain language. Use these terms consistently in specs, code names, CLI/API contracts, and implementation plans.

## Workflow Orchestration

### Task

A durable user-facing unit of work. A task owns workflow state, task metadata, node history, edge-transition history, questions, and execution artifacts. Builder sessions are artifacts under a task, not the task itself.

### Workflow

A durable directed graph that describes how tasks move through work. A workflow contains nodes and edges.

### Node

A visible workflow state and Kanban column/status. Node identity is execution identity: when a task is in a node, that node determines which run behavior applies.

Executable nodes configure agent-run behavior:

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

### Transition Payload

Structured data produced by a node run for a selected edge. A transition payload includes the selected transition ID and node output fields carried into edge validation, task metadata updates, and the next node's input.

### Payload Requirements

Edge-owned requirements for transition payload fields. Payload requirements define which source-node output fields must be present before a node transition can continue.

### Transition ID

A stable identifier for an edge leaving a node. Agent nodes choose a transition ID when their output can follow more than one edge. The transition ID selects the edge; the edge selects the target node and transition behavior.

### Context-Preservation Mode

Per-edge transition policy that decides how the next node receives execution context:

- `new_session`: start a blank Builder session and inject previous node output plus task metadata.
- `continue_session`: continue the previous Builder session with a new prompt/goal and bound metadata.
- `compact_and_continue_session`: compact the previous session first, then continue with a new prompt/goal and bound metadata.

### Run

One durable execution attempt for a node on a task. A run may create or continue a Builder session, call tools, ask questions, produce structured node output, and terminate with a structured outcome.

### Interrupted Run

A run whose execution stopped before producing a valid transition payload. Its session and worktree state remain available so execution can continue from the interruption point.

### Node Output Schema

A node-owned schema for the structured output fields available when a run completes. Workflow orchestration uses these fields for edge decisions, task metadata updates, UI display, transition payloads, and the next node's input bindings.

### Session

Builder transcript/runtime artifact used by a run. A task may have many sessions due to loops, branches, retries, or context-preservation choices.

### Node Transition

A task movement from one node to another through an edge. A node transition evaluates edge conditions, applies edge input/output bindings, applies the edge context-preservation mode, and schedules or blocks the next run.

### Join

An edge or node transition point that waits for required inbound branch outputs before continuing.

### Question

A user-blocking ask emitted by a run. Questions pause the affected run or task path until answered.

### Orchestrator

An agent node whose prompt asks it to coordinate work. Orchestration may use subagent/session infrastructure inside an agent run or route work through workflow graph branches.

### Terminal State

A workflow/task state where auto-execution stops because the task is done, canceled, failed, blocked, or awaiting manual/user action.

### Execution Queue

Durable scheduling state for runnable workflow work. The execution queue decides when runs may start or resume; runtime leases remain separate execution-control state.

### Task Comment

A durable note attached to a task. Task comments capture user or agent observations, review notes, worklogs, and other task-local information that should not be committed into a worktree.
