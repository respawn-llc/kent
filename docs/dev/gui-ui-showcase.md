# GUI UI Showcase

The desktop app has a dev-only browser page for visual review of hard-to-reach UI states.

Run it from the repo root:

```sh
pnpm --dir apps/desktop dev:showcase
```

Open:

```text
http://127.0.0.1:1420/dev-showcase.html
```

The showcase uses static mock data and a fake API transport. It does not require a Builder server, Tauri window, model run, or persisted workflow fixture.

Proof capture:

```sh
./scripts/capture-gui-ui-showcase-proof.sh
```

The proof script starts Vite and connects to an existing browser CDP endpoint. It does not launch a browser. Start Comet with remote debugging on the configured CDP port before running it.

Default output:

```text
.builder/proofs/gui-ui-showcase/
```

The page covers UI primitives, home and project edit panes, kanban columns and cards, board hover menu states, Sonner toast trigger buttons, floating notices, dialogs, task detail, questions, approvals, comments, activity, runs, teleport states, and expandable controls.
