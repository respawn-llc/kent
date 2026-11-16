set working-directory := ".."
set default-list

# Build CLI release artifacts.
cli *args:
    bash scripts/release-artifacts.sh build {{ args }}

# Build Desktop release artifacts.
desktop *args:
    bash scripts/desktop-release.sh build --skip-install {{ args }}

# Update the Homebrew tap; pass --help for publishing options.
brew *args:
    bash scripts/update-brew-tap.sh {{ args }}

[private]
_cli-build *args:
    bash scripts/release-artifacts.sh build {{ args }}

[private]
_cli-verify-manifest *args:
    bash scripts/release-artifacts.sh verify-manifest {{ args }}

[private]
_cli-verify-linux-static *args:
    bash scripts/release-artifacts.sh verify-linux-static {{ args }}

[private]
_cli-smoke *args:
    bash scripts/release-artifacts.sh smoke-test {{ args }}

[private]
_windows-installer-smoke *args:
    bash scripts/release-artifacts.sh smoke-windows-installer {{ args }}

[private]
_desktop-build *args:
    bash scripts/desktop-release.sh build {{ args }}

[private]
_desktop-assemble *args:
    bash scripts/desktop-release.sh assemble {{ args }}
