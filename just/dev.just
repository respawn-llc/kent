set working-directory := ".."
set positional-arguments

desktop *args:
    just _node
    just _gen-typescript
    pnpm --dir apps/desktop exec vite --config tooling/vite.config.ts --host 127.0.0.1 --open / {{args}}

desktop-native *args:
    just _node
    pnpm --dir apps/desktop exec tauri dev "$@"

docs *args:
    just _node
    pnpm --dir docs exec astro dev {{args}}

[private]
_desktop-web *args:
    just _node
    just _gen-typescript
    pnpm --dir apps/desktop exec vite --config tooling/vite.config.ts --host 127.0.0.1 {{args}}
