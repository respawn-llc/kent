# Release And Distribution Spec

## Release Source Of Truth

- Root `VERSION` is the source of truth for release version and tag normalization.
- Release/build scripts sync Tauri and package metadata from `VERSION`.
- Official release binaries are built through `scripts/build.sh`.
- The release profile is `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and `-ldflags "-s -w -X builder/shared/buildinfo.Version=..."`.
- Release archive packaging and verification live in `scripts/release-artifacts.sh`; workflow YAML should stay orchestration-focused.

## Targets

- Supported release targets are `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, and `windows/arm64`.
- macOS Intel is unsupported and must not be reintroduced.
- Linux release binaries stay statically linked; do not enable PIE or other dynamic-linking release modes.
- Workflow runner labels use `*-latest` aliases where GitHub provides them. ARM smoke-test jobs stay on `ubuntu-24.04-arm` and `windows-11-arm` while GitHub does not publish `-latest` aliases for those hosted runners.

## GitHub Releases

- GitHub releases publish `checksums.txt`.
- `scripts/install.sh` verifies archive checksums when the manifest is present.
- The release workflow verifies the checksum manifest and smoke-tests packaged binaries on Linux, macOS, and Windows before publishing.
- The release workflow smoke-tests `scripts/install.ps1` against staged Windows release assets before publishing.
- GitHub artifact attestations are intentionally not part of the release pipeline.

## Installers

- Windows one-command installs are served by `scripts/install.ps1`.
- The default Windows user install path is `~/.builder/bin/builder.exe`, matching the user-scoped Builder persistence root.
- Windows installer uninstalls remove only installer-owned binary, PATH, registry, and marker files.
- Windows uninstalls never remove Builder config, sessions, auth, worktrees, skills, or winget-installed dependencies.

## Homebrew

- Homebrew tap automation is part of a release, not an optional follow-up.
- The release workflow updates `respawn-app/homebrew-tap` through `scripts/update-brew-tap.sh` for formula `builder-cli`.
- The tap PR must run `brew test-bot`; on success, `brew pr-pull` publishes bottle metadata to tap `master`.
- If app release publication succeeds but tap update fails, fix release plumbing first if needed, then create a tap-only change for the same published version. Do not cut a second app release.
