set working-directory := ".."
set positional-arguments

# Fresh-install TUI QA. Pass false to retain temporary state after exit.
onboarding cleanup="true":
    @python3 scripts/dev-onboarding.py "$1"

desktop *args:
    just _gen-typescript
    bun run --bun --cwd apps/desktop vite --config tooling/vite.config.ts --host 127.0.0.1 --open / {{args}}

desktop-native *args:
    bun run --bun --cwd apps/desktop tauri dev "$@"

docs *args:
    bun run --bun --cwd docs astro dev {{args}}

[private]
_desktop-web *args:
    just _gen-typescript
    bun run --bun --cwd apps/desktop vite --config tooling/vite.config.ts --host 127.0.0.1 {{args}}
