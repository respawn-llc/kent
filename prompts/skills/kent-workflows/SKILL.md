---
name: kent-workflows
description: How to use the Kent CLI to author and edit Kent workflow graphs — nodes, edges, transitions, parameters, context modes, and project links. Use when the user asks to define, build, inspect, or modify Kent workflows.
---

- A workflow is a graph of node states connected by transition branches.
- A task is a durable user-facing unit of work moving through one workflow. It directly owns its Current Nodes: normally one, or several during parallel fan-out.
- A node is a visible workflow state. `start` is where tasks are created, `agent` node runs Kent sessions, `join` waits for parallel branches of a group, and `terminal` is a sink where automation stops.
- An edge connects a transition group to a target node. A transition group is selected by a `transition_id`; if the group has multiple edges, it fans out to parallel target nodes.
- Model-facing keys such as node keys, edge keys, transition IDs, and parameter keys must be stable and use lowercase Latin characters. Prefer `implementation_finished`, `review_verdict`, `to_done`, `needs_changes` over display labels with spaces.
- Humans perceive workflows as Kanban boards where each node is a column.

Authoritative command details are the live CLI: `kent workflow --help`

## Workflow prompting rules
In addition to general `prompting` rules, you MUST pay attention to these important workflow considerations:

- Workflow agents run in a fresh session. **They do not receive ANY information from the current session, including what the user said, what you answered, tool results, file contents, command outputs, ideas, context, or historical info**; they are different from you. It's easy for you to fall into the trap of assuming that the workflow agents will know what you know. When you write a prompt, run a fresh, cheap `kent run` subagent and give it the target prompt to review; do it at least once, correct the prompt, then repeat until no ambiguity exists. Ask the subagent what is unclear or underspecified and what else it needs to complete the provided task, from **its** perspective. This will help you step into the shoes of a workflow agent. Do this for every prompt you write, without exception.

- Workflow prompts are not piles of task-specific requirements. They run as reusable instructions across hundreds of varying tickets. The more text there is in the prompt, the higher its cumulative cost and the higher the chance the agent gets confused. **Prompts must be punchy, to the point, practical, and focused.** Never add things like "for this specific task do this, otherwise that"; never mention task IDs or add any information or instructions that do not apply universally across all possible tickets and projects the target workflow will be used for; do not add historical context like "Documentation remains consistent" or useless comparisons like "Write real tests, not change detectors".

- **Avoid piling on more and more "rules" and conditionals; changes should be removal-first.** Every rule distracts the agent and increases cognitive load. Ask: "Would I be able to handle all of these requirements in my head?" The more specific the agent's task is, the better. Discuss with the user how smart the model they plan to use is, and define the desired level of node granularity for the model to be able to handle the work. Prefer removing rules, reducing their number, or enforcing them, rather than adding more and more exclusions and conditions.
  - Caveat: Assume every past instruction has a historical precedent. If the user has an instruction, don't remove it just because it breaks the rules above or just because it became inconvenient. Instead, ask them why the instruction exists and what it accomplished. They have insight and memories you do not have.

- Be careful with your words: agents are a monkey's paw. **Assume every instruction will be taken literally.** Examples:
  - The word "only" in "Read the plan file only once" will cause the agent to never re-read the file even if it changed and needs updates.
  - The wording "Address review findings" will cause the agent to keep addressing even invalid, wrong, or conflicting review results infinitely.
  - The wording "Open the PR to main" will force the agent to open a PR even if one already exists, even if no work was done, and even if main is not the correct branch for the repo the workflow is used in.
So proactively run review agents on your prompts, eliminate ambiguity, and **never add instructions or restrictions speculatively** only to discover that they backfire during other tasks. If you're not confident, propose edits via ask_question before making them.

- For messy real-world work, **give the agents an escape hatch instruction** - what to do when a problem happens, e.g. to ask the user, to execute a transition backwards, or to record evidence and proceed without changes.

- **Make your prompts idempotent where the graph topology involves loops**. Consider this scenario: an `implementer` builds a feature, but then a `reviewer` returns it to the `planner` to adjust the requirements. When the `planner` finishes, it sends the task back to the `implementer` **using the same prompt as before**, but the situation has changed: the worktree already contains a previously finished implementation. Because of that, the 2nd implementer will see the existing code and an adjusted plan (without knowing what edits were made and why), get confused, and throw away good code or produce a broken merged implementation. Your prompts in cycles must consider every path the task might take at every point. Carefully use reasoning and review subagents to make sure the workflow topology does not create confusing situations due to prompt reuse.

These rules are a lot to take in; don't try to hold them all in your head or you'll forget some of them. Use subagents and write verification checklists for yourself before making edits to stay on track.

## Verification checklist
This isn't optional for significant changes. Ask subagents to check this:

- [ ] Each workflow prompt makes sense on its own without any other context besides the Kent task and default environment info
- [ ] There are no instructions in any of the prompts that another agent can misinterpret in malicious or undesirable ways
- [ ] Each prompt inside cycles makes sense even on the 2nd+ iteration and the workflow prefers narrow loops to prevent the issue
- [ ] There is guidance for every agent in a loop on how to break the cycle, like asking the human a question, rejecting a finding by talking to the reviewer via parameters, or taking another route
- [ ] There is no point where caches are invalidated in the entire workflow
- [ ] If the workflow is reusable, every prompt and script in it works even across repos, projects, tech stacks and task types
- [ ] No prompt violates any other `prompting` rules

## Build A Workflow
To create a workflow, inspect its generated start/terminal graph, add nodes and edges, link it to a project, then validate it. Use graph inspect/apply when deletion, rewiring, or several related changes must be saved together.

```bash
kent workflow create --description "Implement and review changes" "Implementation Review"
kent workflow list
wid="<uuid-from-workflow-list>"
kent workflow inspect "$wid"

kent workflow node add "$wid" --key implement --kind agent --agent <implementer-role>
kent workflow node add "$wid" --key review --kind agent --agent <reviewer-role>

kent workflow edge add "$wid" --from backlog --transition start --edge-key start --to implement --context new_session --prompt "Implement the task."
kent workflow edge add "$wid" --from implement --transition review --edge-key review --to review --context new_session --prompt "Review the implementation."
kent workflow edge add "$wid" --from review --transition done --edge-key done --to done --context new_session

kent workflow link . "$wid" --default
kent workflow validate "$wid" --mode execution
```

For validation modes, use `draft` while authoring, `task_creation` before creating tasks, and `execution` for final handoff to the user. Draft workflows can be saved and linked while semantic validation fails. Validate before task creation as a best practice.

To understand what `--agent` roles are available (the reminder in your memory might not list all of them), inspect the repo-local and user global `config.toml` files. More info in the `kent-dogfooding` skill.

Important CLI behavior:
- Workflow commands support `--json` for scripting, but JSON is **very verbose**, so avoid defaulting to it.
- `workflow create` auto-creates the initial backlog/start and done/terminal shape; inspect it after creation to avoid adding duplicate start or terminal nodes.
- `workflow list --project <path-or-id>` discovers linked workflows for a project.
- Agent-targeting edges require `--prompt <text>`. Omit `--prompt` for edges targeting `start`, `join`, or `terminal` nodes, as there's nobody to prompt.
- Script nodes use `--kind script --script-path <path>` and can appear anywhere an agent node can appear. Absolute paths resolve on the Kent server; relative paths resolve against the task managed worktree when the script runs.
- Link, unlink, and set project defaults with `kent workflow link`, `kent workflow unlink`, and `kent workflow default`. This sets up bindings between a project (repo, workspace) and a workflow, and enables reuse of workflows.
- `--transition-description <text>` sets the model-facing transition description on the edge's transition group. The agent will see it when it completes the task. Use it to tell the agent when to pick this transition over its siblings.
- `--param <key>=<description>` (repeatable on `edge add` and `edge update`) declares a transition parameter the source agent must produce when taking the transition. On `edge update`, passing `--param` replaces the full parameter set; `--clear-params` removes all parameters. Omitting both preserves existing parameters.
- Treat workflow deletion as high impact because it cascades through the workflow's links, tasks, and graph. Review the deletion preview before confirming.
- Workflows have no version control or recovery, so be careful with changes. By default, ask the user for every change you're about to make to an existing workflow, especially prompting changes. Take backups of topology and prompts before changing the workflow a lot.

## Edit Existing Workflows
Use node and edge commands for focused changes:

```sh
kent workflow node update "$wid" implement --agent <implementer-role>
kent workflow edge update "$wid" edge-abc123 --transition needs_review --transition-display-name "Needs Review" --edge-key review --to review --context compact_and_continue_session --prompt "Review the implementation."
```

Use the public graph commands for complete-graph edits:

```sh
kent workflow graph inspect "$wid" > workflow-graph.json
# Edit workflow-graph.json.
kent workflow graph apply workflow-graph.json --json
kent workflow graph apply - --json < workflow-graph.json
```

Validate the workflow with `kent workflow validate` after each meaningful graph change. Non-atomic edits may result in broken states that fail validation and won't let you proceed, so for batched edits prefer the apply syntax.

## Context And Approval
Each agent-facing edge requires a context mode:

- `new_session`: start a fresh Kent session and inject task metadata plus outputs from the previous node. This is a double-edged sword: The new session starts with the lowest token count, giving the agent the most memory space for their task; keeps it free from bias; does not invalidate caches, keeping costs low; and is most flexible (can change agent roles etc.), but the agent must receive **all** the necessary context to effectively complete their task in the node prompt and input parameters, and they will have to gather context from the workspace **from scratch** to orient themselves (possibly negating token and cost benefits gained). This means `new_session` is a good fit for isolated tasks, verification, self-contained units of work, e.g., code review, QA runs, requirement verification; and a poor fit for continuation of existing work, next-phase of a plan, or highly contextual tasks that need carry-over of information.

- `compact_and_continue_session`: ask the previous agent for a handoff, then start a new session with the next node prompt, their handoff, and task metadata. This is the middle ground - it frees context, but keeps only the important details about the previous agent's work state and actions in context, then starts a new session. It does not invalidate caches, allows changing the subagent role, and still keeps a lot of free memory space available. However, this incurs additional costs to **hand off the previous session**, leaves the previous session cache abandoned, and the context preservation is imperfect due to summarization. Use this when the source node is a large chunk of work (such as feature implementation or a research task) and the next one continues their work in another direction (does not benefit much from the full context). Using this in cyclic subgraphs with small tasks (e.g., re-reviews) can be detrimental because the handoffs will be issued repeatedly; prefer continue_session in that case (it will still hand off on demand).

- `continue_session`: directly continue one of the previous Kent sessions, keeping the **same subagent role**, conversation history, state, and cache. This mode directly gives the agent the next task as a message and runs the loop. This is best when the task that the source node completed was relatively small (fitting in roughly one agent memory window) and the next node directly continues it or some other previous node. For example, an `investigation` phase that continues into `planning` phase, or `implementation` node that teleports context into another `implementation` node as a loop after code review findings were posted.

Note that to prevent high costs due to cache invalidation, only compact and new_session context modes allow changing the subagent role of the target node when transitioning. For example, you cannot continue from `coding` to `code_review` roles unless you compact or start a new session.

Continuation modes also have a context source. Use `--context-source <source>` on `workflow edge add` or `workflow edge update`.

- `immediate_source`: continue or compact the session from the node that selected the transition. This is the default, but not always useful when the next node works on a different task.
- `node:<node-key>`: continue the latest retained Session associated with a specific guaranteed-prior agent node. Use this when the target needs context from an earlier node rather than the immediate source, but the workflow topology must guarantee that the node has already run.
- `previous_target`: continue or compact the latest retained Session associated with this edge's target node. Can only be used in guaranteed loops where the target always ran before the source. If no retained target Session exists, the transition fails.
- `previous_target_or_new`: continue or compact the latest retained Session associated with this edge's target node when one exists; otherwise start the target as an effective `new_session`. Use this for optional re-review loops where the first pass should start fresh and later passes should continue the prior review Session. This is good for looping re-checks, re-reviews, or returning to implementation.

Examples:

```bash
kent workflow edge update "$wid" edge-review \
  --context continue_session \
  --context-source previous_target_or_new

kent workflow edge update "$wid" edge-rework \
  --context compact_and_continue_session \
  --context-source previous_target
```

Context source constraints:
- `new_session` can only use `immediate_source`; source choices are meaningful only for continuation modes.
- Start/backlog transitions must use `immediate_source`.

Use `--requires-approval` when a transition must stop for explicit review before the target node starts. Approval can happen asynchronously with a delay, thus causing cache invalidation, so approvals across `continue_session` are suboptimal. Usually, the user will tell you at which point continuation requires approval.

## Completion modes
The completion mode controls the technicality of how an agent node signals task completion.

Set it per node with `--completion-mode <mode>` on `node add` or `node update`. Omit the flag to default to user-configured default or resolve automatically based on surrounding configuration.

- `auto`: resolve the effective mode based on configuration. This is the default and the right choice for most nodes, because it dynamically applies the rules below.
- `structured_output`: provider-native structured output. Lowest-friction on capable providers, but prevents the Current Node from starting when the provider does not support it, and **causes full cache invalidation** when applied to continued sessions.
- `tool`: completion via the dynamic `complete_node` tool. Use it for tool-driven completion on providers without structured-output support. Also causes **full cache invalidation** for continued sessions.
- `shell_command`: the agent runs `kent task complete` from its shell. Requires a runtime `shell` tool to be enabled in the subagent role config, but the shell tool gives the agent full access to the host system. Prefer this when the workflow contains `continue_session` transitions, because this mode does not cause cache invalidation.
- `unstructured_output`: the agent tries a best-effort JSON submission as its answer. This is the most fragile configuration; use it only if you must use `continue_session` for the node chain and the agents there do not have the `shell` tool that would have enabled the `shell_command` mode.

## Transition Prompts And Parameters
The transition prompt is the task the target agent runs; parameters are the typed outputs the source agent must produce to carry context across the edge.

```bash
kent workflow edge update "$wid" edge-abc123 \
  --transition-description "Implementation is complete; send for review." \
  --param "plan_file_path=Path to the plan document the implementer followed." \
  --prompt "Review the changes against the plan at {{.Params.plan_file_path}}."
```

Prompt placeholders are Go template fields:

- Built-in task and node fields: `{{.TaskId}}`, `{{.TaskShortId}}`, `{{.TaskTitle}}`, `{{.TaskBody}}`, `{{.NodeId}}`, `{{.NodeKey}}`, `{{.NodeDisplayName}}`, `{{.SessionId}}`
- This transition's own parameters: `{{.Params.<parameter_key>}}`. Join-to-agent prompts read aggregated branch parameters the same way.
- A guaranteed-prior transition's parameter: `{{.Params.<transition_id>.<parameter_key>}}`. A transition is guaranteed-prior only when every path from `start` to the prompt-owning edge's source passes through it. Description and session ID params are also supported with this syntax. Use this to reference an earlier output instead of forcing the agents to provide the same parameter repeatedly.

Use parameters together with prompt placeholders to build dynamic task prompts, supplying the context for the next agent node to effectively complete its job. More guidance in the `prompting` skill.

- Parameters are string-only and required once declared.
- Every parameter the source agent provides forms its provision contract: the same parameter key declared on more than one transition out of one source node, or on more than one branch of one fan-out transition, must use an identical description, otherwise the provision fields conflict.
- Declare a parameter on the transition whose source agent can produce it. Prefer referencing a guaranteed-prior transition over re-threading the same value, but re-declare locally where the graph converges or loops and no single upstream transition dominates.

## Script Nodes
Use a script node when a workflow step can run as a deterministic executable on the Kent server. This saves tokens and eliminates the non-deterministic aspect of LLMs. For inspiration, script nodes can run test gates and linters, watch pull request reviews and CI runs, send messages to other agents, record evidence, and more.

```bash
kent workflow node add "$wid" --key release_notes --kind script --script-path scripts/release-notes
kent workflow edge add "$wid" --from implement --transition release_notes --edge-key release_notes --to release_notes --context new_session
```

Kent executes the script directly **in the server shell**, not the user shell; double-check the script's PATH. Stdin is one JSON object: incoming workflow parameter values are top-level properties, and `_kent` contains metadata about the execution. Prefer placing scripts in the repository's main workspace (whether they are checked into VCS depends on the user's choice) for workflows that are tailored to a project, and at `~/.kent/scripts` or a similar generic directory for reusable workflows, unless the user gives guidance.

```json
{
  "release_notes_path": "docs/release-notes.md",
  "_kent": {
    "task_id": "task_123",
    "node_id": "node_456",
    "transition_branch_key": "release_notes"
  }
}
```

Stdout of your scripts must be workflow completion JSON. Stderr is diagnostics only. A typical stdout payload is:

```json
{
  "transition": "to_done",
  "commentary": "Generated release notes.",
  "release_notes_path": "docs/release-notes.md"
}
```

If there is only one outgoing transition, `transition` can be omitted. Include declared transition parameters as top-level string fields. For multiple outgoing transitions, fields for unselected transitions should be `null`.

Relative script paths cannot be fully checked until the task-managed worktree exists, so run manual QA on them to ensure the user doesn't discover bugs when they try to execute real work.
