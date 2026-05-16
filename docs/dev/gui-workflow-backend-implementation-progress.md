# GUI Workflow Backend Implementation Progress

Status: active implementation tracker for `docs/dev/gui-workflow-backend-slice-plan.md` slices 0-5.

Goal: implement backend slices 0-5 with server-authoritative APIs/read models/actions for the GUI workflow MVP.

Current focus:

- [x] Slice 0: connectivity/readiness/capabilities.
- [ ] Slice 1: Home/project admin/project key/workspaces.
- [ ] Slice 2: workflow picker, selected board, groups, live updates.
- [ ] Slice 3: task source workspace and Backlog editing.
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

- Current next step: start Slice 1 recon and first TDD tracer for explicit `project_key` create.
