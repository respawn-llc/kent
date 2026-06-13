# GUI UI Showcase

The static UI showcase is a secondary path for hard-to-reach UI states. It uses mock data and does not replace browser-client QA against a real Kent server.

Run it from the repo root:

```sh
pnpm --dir apps/desktop dev:showcase
```

Open:

```text
http://127.0.0.1:1420/dev-showcase.html
```

The showcase uses static mock data and a fake API transport. It does not require a Kent server, Tauri window, model run, or persisted workflow fixture.

The page covers UI primitives, home and project edit panes, kanban columns and cards, board hover menu states, Sonner toast trigger buttons, floating notices, dialogs, task detail, questions, approvals, comments, activity, runs, clipboard-copy states, and expandable controls.
