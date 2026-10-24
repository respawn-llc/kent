set working-directory := ".."

[private]
_prepare-openai:
    GOOS= GOARCH= go run ./scripts/prepare-openai

desktop:
    @bash scripts/quiet-on-success.sh just install _desktop

[private]
_desktop:
    bash scripts/build-desktop.sh --skip-install --install

[private]
_apps-dependencies:
    @bash scripts/quiet-on-success.sh just install _apps-dependencies-implementation

# The repository-wide Effect policy reads embedded sources with the docs parsers.
[private]
_apps-dependencies-implementation: _docs-dependencies
    bun install --cwd apps --frozen-lockfile
    just install _effect-compiler

# Resolve intentional manifest changes under the workspace dependency policy.
apps-lock: _docs-dependencies
    bun apps/scripts/check-dependency-policy.mjs
    bun install --cwd apps
    just install _effect-compiler

# Resolve intentional docs manifest changes without upgrading locked dependencies.
docs-lock:
    bun install --cwd docs

# Resolve intentional native manifest changes without upgrading locked dependencies.
desktop-lock:
    cargo fetch --manifest-path apps/desktop/src-tauri/Cargo.toml

[private]
_effect-compiler:
    bun run --bun --cwd apps/desktop effect-tsgo patch --typescript --typescript-package typescript7

[private]
_docs-dependencies:
    @bash scripts/quiet-on-success.sh just install _docs-dependencies-implementation

[private]
_docs-dependencies-implementation:
    bun install --cwd docs --frozen-lockfile

[private]
_go-dependencies:
    @bash scripts/quiet-on-success.sh just install _go-dependencies-implementation

[private]
_go-dependencies-implementation:
    just install _prepare-openai
    go mod download
    go mod download -modfile=tools.mod

[private]
_desktop-dependencies:
    @bash scripts/quiet-on-success.sh cargo fetch --manifest-path apps/desktop/src-tauri/Cargo.toml --locked

[private]
_dependencies:
    @bash scripts/quiet-on-success.sh just install _dependencies-implementation

[parallel]
[private]
_dependencies-implementation: _go-dependencies _apps-dependencies _desktop-dependencies
