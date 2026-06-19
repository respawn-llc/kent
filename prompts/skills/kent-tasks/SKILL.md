---
name: kent-tasks
description: How to use the Kent CLI to manage tasks and task comments — create, inspect, and list tasks, and add/list/replace comments. Use when the user asks to create Kent task records, inspect task state and runs, or manage task comments.
---

A task is the durable user-facing unit of work moving through one workflow. To author or edit the workflow a task runs on, use the `kent-workflows` skill.

Authoritative command details are the live CLI:

```bash
kent task --help
```

## Operate Tasks
Create task records against a linked/default workflow and project, then inspect them:

```bash
kent task create --project . --workflow <workflow> --title "Fix flaky workflow test" --body "Investigate and fix the failure."
kent task list --project .
kent task show <short-id-or-task-id>
```

Task IDs beginning with `task-` are global. Short IDs are project-scoped, so pass `--project <project-id-or-path>` when the current directory is not the target project.

Use `task show` as the main state probe. It prints task metadata followed by placements, runs, transitions, and comments. Use it to find:

- active placement node IDs and states;
- run IDs and interrupted/completed timestamps;
- comment IDs for replacement or deletion.

## Comments
Task comments are durable task-local notes. They are useful for user instructions, review notes, and work logs that should not be committed into a worktree.

```bash
kent task comment add --project . <short-id-or-task-id> --body "Please prioritize the failing scheduler test."
kent task comment list --project . <short-id-or-task-id>
kent task comment replace <comment-id> --body "Updated note."
```

`comment replace` replaces the full body. Use `comment list --include-deleted` when deleted comments matter.

## Human-Only Task Actions
Do not run task commands that start automation, cancel work, move tasks, resume runs, approve transitions, or delete comments. These operations are reserved for humans. If the user asks for one of them, provide the exact command for the user to run themselves. If task work is blocked and you need one of these actions, use `ask_question` to call for help.

Starting task automation can launch model work and consume provider credits. For exploratory validation prefer `task create`, `task show`, and comments over commands that start automation.
