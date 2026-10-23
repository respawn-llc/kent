set working-directory := ".."

[parallel]
default mode="": (server mode) (desktop mode) (docs mode)

server mode="":
    #!/usr/bin/env bash
    set -euo pipefail
    just _gen-go
    if [ "{{mode}}" = "--dry-run" ]; then
        files="$(gofmt -l .)"
        [ -z "$files" ] || { echo "$files"; exit 1; }
    elif [ -z "{{mode}}" ]; then
        gofmt -w .
    else
        echo "Usage: just lint server [--dry-run]" >&2
        exit 2
    fi
    just _lint-protobuf
    go run ./cmd/checkarchitecture .
    go vet ./...

desktop mode="":
    #!/usr/bin/env bash
    set -euo pipefail
    bun apps/scripts/check-dependency-policy.mjs
    bun apps/scripts/check-typescript-policy.mjs
    bun test --timeout 60000 apps/scripts/check-eslint-architecture.test.mjs
    if [ "{{mode}}" = "--dry-run" ]; then
        [ -d apps/desktop/packages/server-api-contract/src/gen ] || {
            echo "Generated TypeScript sources are required. Run 'just gen' first." >&2
            exit 2
        }
        bun run --bun --cwd apps prettier --check .
        bun run --bun --cwd apps/desktop eslint .
    elif [ -z "{{mode}}" ]; then
        just _gen-typescript
        bun run --bun --cwd apps prettier --write .
        bun run --bun --cwd apps/desktop eslint . --fix
    else
        echo "Usage: just lint desktop [--dry-run]" >&2
        exit 2
    fi
    just build _desktop-compiler

tui mode="":
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "{{mode}}" = "--dry-run" ]; then
        files="$(gofmt -l cli/tui)"
        [ -z "$files" ] || { echo "$files"; exit 1; }
    elif [ -z "{{mode}}" ]; then
        gofmt -w cli/tui
    else
        echo "Usage: just lint tui [--dry-run]" >&2
        exit 2
    fi
    go vet ./cli/tui/...

# Frozen Rust is explicit-only while tui-rs/ remains frozen.
rust mode="":
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "{{mode}}" = "--dry-run" ]; then
        cargo fmt --manifest-path tui-rs/Cargo.toml --all -- --check
    elif [ -z "{{mode}}" ]; then
        cargo fmt --manifest-path tui-rs/Cargo.toml --all
    else
        echo "Usage: just lint rust [--dry-run]" >&2
        exit 2
    fi
    cargo clippy --manifest-path tui-rs/Cargo.toml --workspace --all-targets --locked -- -D warnings
    cargo run --manifest-path tui-rs/Cargo.toml --locked -p manifest-check -- check --repo-root .

docs mode="":
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "{{mode}}" = "--dry-run" ]; then
        bun run --bun --cwd docs prettier --check .
    elif [ -z "{{mode}}" ]; then
        bun run --bun --cwd docs prettier --write .
    else
        echo "Usage: just lint docs [--dry-run]" >&2
        exit 2
    fi
    bun run --bun --cwd docs astro check

[private]
_desktop-native command="check" *args:
    cargo {{command}} --manifest-path apps/desktop/src-tauri/Cargo.toml --locked {{args}}

[private]
_effect-policy *paths:
    bun apps/scripts/check-effect-policy.mjs {{paths}}
