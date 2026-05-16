# GUI Workflow Backend Implementation Progress

Status: active implementation tracker for `docs/dev/gui-workflow-backend-slice-plan.md` slices 0-5.

Goal: implement backend slices 0-5 with server-authoritative APIs/read models/actions for the GUI workflow MVP.

Current focus:

- [x] Slice 0: connectivity/readiness/capabilities.
- [x] Slice 1: Home/project admin/project key/workspaces.
- [x] Slice 2: workflow picker, selected board, groups, live updates.
- [x] Slice 3: task source workspace and Backlog editing.
- [ ] Slice 4: actions, attention inbox, questions, approvals.
- [ ] Slice 5: task detail, activity feed, comments, teleport.

Working rules:

- Use `docs/dev/gui-workflow-backend-slice-plan.md` as the source spec.
- Update this progress file after each meaningful implementation milestone.
- Keep GUI/backend additions out of `docs/dev/async-workflow-implementation-checklist.md`.
- Write tests first for each behavior slice.
- Run slice verification commands before marking a slice complete.

Slice 0 notes:

- Started: 2026-05-16.
- Recon target packages: `server/serve`, `server/core`, `server/transport`, `shared/rpccontract`, `shared/client`, `shared/servicecontract`, `shared/protocol`, `shared/serverapi`.
- First TDD tracer: add `server/serve` public RPC test that starts an unauthenticated server, dials configured remote, calls `server.readiness.get`, and expects summary-first not-ready response with server/protocol identity and generic auth blocker cause.
- Implemented `server.readiness.get` and `server.capabilities.get` over public remote, route contract, service contract, transport, core, and serve wiring.
- Verification passed:
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/auth ./server/bootstrap ./server/embedded ./server/core ./server/serve ./cli/app`
  - `./scripts/build.sh --output ./bin/builder`

Slice 1 notes:

- Started: 2026-05-16.
- Completed tracer: explicit `ProjectCreateRequest.ProjectKey` flows through projectview service, metadata store, response binding, list/overview summaries.
- Completed tracer: invalid/duplicate/omitted project-key create behavior in `server/projectview`.
- Completed tracer: `ProjectWorkspaceListRequest` service method returns all workspaces and default primary workspace using GUI DTO shape.
- Completed tracer: `project.workspace.list` route/client/protocol/servicecontract/transport wiring.
- Completed tracer: `project.home.list` paginated Home summaries include project key, primary workspace, default workflow ID/name/validity, task/workflow/attention counts, generated timestamp, and event watermark field.
- Current implementation returns `latest_event_sequence=0` as a foundation watermark until Slice 2 adds durable subscription sequence delivery.
- Verification passed:
  - `./scripts/test.sh ./server/metadata ./server/projectview ./shared/serverapi ./shared/clientui`
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/projectview ./server/metadata ./server/workflowstore ./server/workflowview ./server/core ./server/serve ./cli/app`
  - `./scripts/build.sh --output ./bin/builder`

Slice 2 notes:

- Started: 2026-05-16.
- Completed tracer: selected `workflow.board.get` returns project identity, picker, selected workflow, safe columns, groups, cards, done preview, action facts, generated timestamp, and event watermark.
- Completed tracer: visual workflow node groups have schema, store/service/API routes, graph revision bumps, node group assignment, and board group projection.
- Completed tracer: `workflow.subscribeProject` route/client/transport/service streams durable monotonic invalidation events after `after_sequence`; empty project ID subscribes to global Home invalidations.
- Home `latest_event_sequence` now reads durable workflow event watermark instead of Slice 1 placeholder `0`.
- Current card workspace chip uses primary/default workspace fallback until Slice 3 persists task source workspace.
- Verification passed:
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/workflowsvc ./server/workflowstore ./server/workflowview ./server/workflowscheduler`
  - `./scripts/test.sh ./server/metadata ./server/projectview ./cli/builder ./cli/app ./server/core ./server/serve`
  - `./scripts/build.sh --output ./bin/builder`

Slice 3 notes:

- Started: 2026-05-16.
- Completed tracer: `tasks.source_workspace_id` migration, optional body schema, project-scoped source workspace trigger, and SQLC/query/store support.
- Completed tracer: task create persists selected source workspace, defaults omitted source workspace to primary, allows empty body, and rejects foreign-project source workspace.
- Completed tracer: `workflow.task.update` edits title/body/source workspace while task is still Backlog, rejects edits after cancellation or automation/worktree start, and emits task update invalidations.
- Completed tracer: board cards and task detail expose persisted source workspace, body preview, body/source URL, and created/updated timestamps; legacy missing source workspace falls back to project primary workspace in read/worktree paths.
- Completed tracer: `workflow.task.start` remains the drag-to-start backend operation and emits task start invalidation; worktree creation now uses selected task source workspace before primary fallback.
- Verification passed:
  - `./scripts/test.sh ./shared/serverapi ./shared/servicecontract ./shared/client ./shared/protocol ./shared/rpccontract ./server/transport ./server/metadata ./server/workflowsvc ./server/workflowstore ./server/workflowview ./server/worktree`
  - `./scripts/test.sh ./cli/builder ./server/workflowscheduler ./server/workflowrunner`
  - `./scripts/build.sh --output ./bin/builder`
