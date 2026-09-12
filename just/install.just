set working-directory := ".."

desktop:
    @bash scripts/quiet-on-success.sh just install _desktop

[private]
_desktop:
    just _node
    bash scripts/build-desktop.sh --skip-install --install

[private]
_apps-dependencies:
    @bash scripts/quiet-on-success.sh just install _apps-dependencies-implementation

# The repository-wide Effect policy reads embedded sources with the docs parsers.
[private]
_apps-dependencies-implementation: _docs-dependencies
    just _node
    pnpm --dir apps install --frozen-lockfile
    just install _effect-compiler

# Resolve intentional manifest changes under the workspace dependency policy.
apps-lock: _docs-dependencies
    just _node
    node apps/scripts/check-dependency-policy.mjs
    pnpm --dir apps install --no-frozen-lockfile
    just install _effect-compiler

[private]
_effect-compiler:
    pnpm --dir apps/desktop exec effect-tsgo patch --typescript --typescript-package typescript7

[private]
_docs-dependencies:
    @bash scripts/quiet-on-success.sh just install _docs-dependencies-implementation

[private]
_docs-dependencies-implementation:
    just _node
    pnpm --dir docs install --frozen-lockfile

[private]
_go-dependencies:
    @bash scripts/quiet-on-success.sh just install _go-dependencies-implementation

[private]
_go-dependencies-implementation:
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
