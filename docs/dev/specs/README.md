# Product Specs

These docs are the authoritative, searchable product-decision source for Kent agents.

Rules:

- Specs contain product and architecture decisions only.
- Implementation plans, progress logs, checklists, audits, and temporary research notes do not belong here.
- Later decisions override older decisions when current code also backs them up.
- Missing code is not evidence that a decision was removed; implementation drift is possible.
- Track unimplemented product work in GitHub issues and implementation cleanup in `docs/dev/techdebt/techdebt.md`, not in temporary migration review files.

Area specs:

- `core-runtime-tools.md`: product scope, architecture boundaries, sessions, auth, config, tools, headless mode, and compaction.
- `tui-transcript.md`: terminal modes, transcript visibility, rendering, input, slash commands, worktrees, notifications.
- `workflow-orchestration.md`: workflow domain, scheduler/runtime behavior, persistence, schema, CLI, and workflow Q/A.
- `desktop-gui.md`: desktop GUI stack, Home/board/task-detail behavior, workflow MVP, native bridge, connection loss, and GUI Q/A.
- `workflow-editor.md`: workflow editor, draft editing, library/linking, sidebar, and save/conflict decisions.
- `release-distribution.md`: release, installer, and distribution decisions.
- `terminology.md`: DDD terms agents should use in specs, code names, APIs, and UI.
