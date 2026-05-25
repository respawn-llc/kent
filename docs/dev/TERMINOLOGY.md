# Terminology

This document defines Builder's DDD ubiquitous language. Use these terms consistently in specs, code names, CLI/API contracts, and implementation plans.

## Workflow Orchestration

### Task

A durable user-facing unit of work. A task owns workflow state, task metadata, node history, edge-transition history, question associations, and execution artifacts. Builder sessions are artifacts under a task, not the task itself.

### Task Short ID

A human-facing project-scoped task identifier. A task short ID combines a project key with a project-local sequence number.

### Project Key

A short human-facing prefix used in task short IDs. Project keys are uppercase, globally unique, and immutable after tasks exist for that project.

### Graph Revision

A monotonic workflow graph counter incremented by graph-affecting workflow edits. It provides traceability for tasks, runs, transitions, approvals, and stale-behavior warnings without snapshotting the full workflow definition.

### Workflow

A durable directed graph that describes how tasks move through work. A workflow contains nodes and edges.

### Workflow Draft

A workflow definition that can be saved and edited while semantic validation reports graph or project-context errors. Drafts still satisfy hard storage invariants such as valid identifiers, valid references, and valid enum values. Draft status is validation behavior, not a separate copy of the workflow graph.

### Validation Context

The purpose for validating a workflow graph, such as draft editing, task creation, or execution scheduling. Different contexts can report the same errors but choose different blocking behavior.

### Project Workflow Link

A project association with a reusable workflow definition. The link lets a project use a workflow without copying the workflow graph.

### Assignee

The subagent role associated with an executable node. UI surfaces may present the role as the node's assignee.

### Node

A workflow graph state. Agent, start, and terminal nodes can map to user-visible workflow states or Kanban columns/statuses, while join nodes are internal merge plumbing omitted from board columns. Workflow editor visuals may show join nodes as inspectable merge plumbing even though they are not board columns or user-visible task states. Node identity is execution identity: when a task is in a node, that node determines which run behavior applies.

Executable nodes configure agent-run behavior:

- subagent role reference and validation policy;
- node prompt/template;
- output schema;
- run limits and stop conditions;
- worktree/session execution policy that applies while this node runs.

Completing a node means its configured run reached a structured terminal outcome, not that an assistant wrote a natural-language final answer.

### Start Node

The node where new tasks enter a workflow. A start node is non-executable and has no input requirements.

### Task Start

An explicit operation that moves a newly created task from its start-node placement into the workflow's first executable placement by applying the start node's outgoing transition.

### Terminal Node

A sink node where workflow automation stops.

### Edge

A directed graph connection from a source node to a target node. Edges configure transition behavior, not the agent run itself.

An edge configures:

- target node;
- whether transition needs human approval or another manual interaction;
- context-preservation mode for the next node;
- context source for continuation modes;
- input/output bindings between prior node output, task metadata, and next node prompt/context;
- routing condition or decision mapping;
- join/aggregation requirements when multiple inbound branches must complete.

### Transition Output

Structured data produced by a node run for a selected transition group. Transition output includes the selected transition ID, optional commentary, and top-level node output fields carried into edge validation, transition logs, and the next node's input.

### Output Requirements

Edge-owned requirements for transition output fields. Output requirements define which source-node output fields must be present before a node transition can continue.

### Transition ID

A stable identifier for a transition group leaving a node. Agent nodes choose a transition ID when their output can follow more than one transition group.

### Transition Group

One or more outgoing edges selected together by a transition ID. A transition group with one edge performs a normal single-node transition. A transition group with multiple edges fans out into parallel target nodes.

### Context-Preservation Mode

Per-edge transition policy that decides how the next node receives execution context:

- `new_session`: start a blank Builder session and inject previous node output plus task metadata.
- `continue_session`: continue the previous Builder session with a new prompt/goal and bound metadata.
- `compact_and_continue_session`: compact the previous session first, then continue with a new prompt/goal and bound metadata.

### Context Source

Per-edge continuation policy that decides which earlier run supplies the source session for `continue_session` or `compact_and_continue_session`. The default is `immediate_source`, meaning the run that produced the selected transition. `node:<node_key>` selects the latest completed run for a guaranteed-prior agent node while keeping input bindings tied to the immediate transition output.

### Run

One durable execution attempt for a node on a task. A run may create or continue a Builder session, call tools, ask questions, produce structured node output, and terminate with a structured outcome.

### Interrupted Run

A run whose execution stopped before producing valid transition output. Its session and worktree state remain available so execution can continue from the interruption point.

### Session Contract

The immutable execution setup captured by a Builder session after its first model request, including model/provider setup, generation parameters, tool schema snapshot, and system/developer prompt snapshot. Workflow direct continuation reuses this persisted setup.

### Node Output Schema

A node-owned schema for the structured output fields available when a run completes. Workflow orchestration uses these fields for edge decisions, UI display, transition output, and the next node's input bindings.

### Session

Builder transcript/runtime artifact used by a run. A task may have many sessions due to loops, branches, retries, or context-preservation choices. In current code, "session" means the durable transcript from the start of a conversation until its terminal/end state; it can cross compaction and handoff boundaries. A single handoff-to-handoff model range is an execution slice within a session, not the whole session.

### Session Run

One live execution of a session through the runtime loop. A session can have multiple runtime activations over time through resume, queued user submissions, goal turns, compaction, or background continuation.

### Node Transition

A task movement from one node to another through an edge. A node transition evaluates edge conditions, applies edge input/output bindings, applies the edge context-preservation mode, and schedules or blocks the next run.

### Node Placement

An occurrence of a task in a node. A task can have multiple active node placements when a workflow explicitly runs parallel branches.

### Parallel Batch

The set of branch node placements created by one fan-out transition group for one task. A parallel batch gives join nodes a correlation identity for deciding which branch results belong together.

### Join

A non-agent node that waits for required inbound branch outputs before continuing.

### Task Cancellation

A task-level stop operation that prevents further workflow automation for the task and interrupts active runs with cancellation reason metadata. Cancellation archives the task to the workflow's terminal/Done node for board visibility while preserving cancellation metadata as the task status/activity.

### Question

A user-blocking ask emitted by a run through the `ask_question` tool. Questions carry prompt text, optional suggestions/options, optional recommended option index, and schema-backed answer expectations. The frontend presents them as a modal/action surface; answering resumes the blocked runtime path through the normal question resolver.

### Orchestrator

An agent node whose prompt asks it to coordinate work. Orchestration may use subagent/session infrastructure inside an agent run or route work through workflow graph branches.

### Operational Stop State

A workflow/task state where auto-execution stops because the task is done, interrupted, blocked, or awaiting manual/user action.

### Scheduler

Server-owned automation scheduler for runnable workflow work. Runnable work is derived from durable task/run state, while pending-work ordering and active runtime ownership are live scheduler/runtime state.

### Task Comment

A durable note attached to a task. Task comments capture user or agent observations, review notes, worklogs, and other task-local information that should not be committed into a worktree. A task comment can be added, replaced as a whole, or soft-deleted.

## GUI

### Toast

A transient or persistent global notification surfaced by the desktop app. Toast and snackbar are equivalent terms in Builder GUI docs and code.

## TUI And Transcript

### Ongoing Mode

Primary long-running TUI mode backed by normal-buffer terminal scrollback. Ongoing mode appends committed transcript history and live overlays without owning a scrollable viewport or rewriting previously emitted lines.

### Detail Mode

Transcript inspection mode with UI-local selection, expansion, and line-oriented viewport scrolling. Detail content can update while open, but scroll/anchor behavior stays stable unless user navigates.

### Transcript Mode

The rendering posture for transcript entries. Current modes are ongoing and detail; each transcript role declares whether it is visible in ongoing, collapsed in ongoing, detail-only, or hidden.

### Alternate Screen

Terminal screen buffer separate from normal scrollback. Builder avoids alternate screen for ongoing mode so persistent history remains in native terminal scrollback. Some focused pickers may use alternate screen for temporary UI.

### Alternate Scroll

Terminal mode `?1007`, which converts wheel input into cursor-key style events in alternate-screen contexts. Ongoing mode never enables alternate scroll. Detail may enable it only while active, then disable it on exit.

### Mouse Capture

Terminal mode where the app receives mouse events instead of leaving them to native terminal selection. Builder keeps mouse capture disabled in ongoing and detail modes so text selection/copy remains native.

### Normal Buffer

Terminal buffer with native scrollback. Ongoing mode renders committed history here and treats emitted lines as immutable.

### Scrollback

Terminal-owned history of normal-buffer output. Builder does not replay, clear, or restyle committed ongoing scrollback after startup.
