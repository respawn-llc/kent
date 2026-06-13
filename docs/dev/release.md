# Release

This is the current release flow for `kent`.

## Recommended Path

Use `workflow_dispatch`. It is the simplest path and does not require the `autorelease` PR label flow.

1. Make sure the release commit is on `main` and pushed.
2. Set `VERSION` to the release version, usually without the `v` prefix, for example:

```text
0.2.0
```

3. Commit and push that change.
4. Trigger the release workflow:

```bash
gh workflow run release.yml --repo respawn-llc/kent
```

5. Wait for the `release` workflow in `respawn-llc/kent` to finish.
6. Wait for the tap automation in `respawn-llc/homebrew-tap` to finish.
7. Verify the GitHub release and Homebrew install.

## What The App Release Workflow Does

The `release` workflow in `/.github/workflows/release.yml`:

1. Reads `VERSION`.
2. Normalizes it to a git tag `vX.Y.Z`.
3. Creates and pushes the tag if it does not already exist.
4. Builds static release binaries through `scripts/build.sh` with the shared release profile.
5. Packages the release archives and writes `checksums.txt` through `scripts/release-artifacts.sh`.
6. Verifies the checksum manifest and smoke-tests packaged binaries on Linux, macOS, and Windows before publishing.
7. Smoke-tests the Windows installer against staged release assets before publishing.
8. Publishes the GitHub release.
9. Checks out `respawn-llc/homebrew-tap`.
10. Runs `scripts/update-brew-tap.sh` for formula `kent`.
11. Opens a PR in the tap repo with label `pr-pull`.

## What The Tap Automation Does

The tap repo automation is part of the release, not an optional follow-up.

1. The tap PR runs `brew test-bot`.
2. On success, `brew pr-pull` runs.
3. `brew pr-pull` pushes bottle metadata to tap `master`.
4. After that, `brew update && brew install kent` should resolve to the new version.

## Recovery If The Tap Step Fails After Publish

If the app release workflow publishes `vX.Y.Z` successfully but fails in `update_brew_tap`, do not cut a second app release.

1. Fix the workflow or tap updater script on `main` first if the failure is in release plumbing.
2. Create the tap change manually from this repo using `scripts/update-brew-tap.sh` against a fresh clone of `respawn-llc/homebrew-tap` on a branch like `chore/kent-vX.Y.Z`.
3. Open the tap PR with label `pr-pull`.
4. Wait for `brew test-bot`, then `brew pr-pull`, and only then consider the release complete.

## Verification

Verify all of these before considering the release done:

1. The GitHub release `vX.Y.Z` exists in `respawn-llc/kent` and contains the expected assets plus `checksums.txt`.
2. The tap PR in `respawn-llc/homebrew-tap` is closed by the automation.
3. The formula on tap `master` has the new tag URL and bottle block.
4. A standalone Unix install works and passes checksum verification when the release publishes `checksums.txt`:

```bash
curl -fsSL https://raw.githubusercontent.com/respawn-llc/kent/main/scripts/install.sh | sh
kent --version
```

5. A standalone Windows install works and passes checksum verification:

```powershell
irm https://raw.githubusercontent.com/respawn-llc/kent/main/scripts/install.ps1 | iex
kent --version
```

6. A fresh Homebrew install works after `brew update`:

```bash
brew update
brew tap respawn-llc/tap
brew install kent
kent --version
```

If short-name resolution is stale on a machine, use the fully qualified formula name:

```bash
brew install respawn-llc/tap/kent
```

7. Clean up the Homebrew and direct installs to restore your local development build:

```bash
brew uninstall kent 2>/dev/null || true
sudo rm -f /usr/local/bin/kent
which kent # should point to your local development bin directory, e.g. ./bin/kent
```

## Notes

- Installed binary name stays `kent`. Formula name is `kent`.
- Official release targets are `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, and `windows/arm64`. macOS Intel is unsupported.
- The smoke-test workflow uses `*-latest` where GitHub provides it; ARM still requires the pinned hosted-runner labels `ubuntu-24.04-arm` and `windows-11-arm` because GitHub does not publish `ubuntu-latest-arm` or `windows-latest-arm` aliases.
- Do not create the git tag manually unless you are intentionally bypassing the workflow behavior.
- Linux release binaries must stay statically linked; do not switch the release pipeline to PIE or other dynamic-linking modes.
- Keep archive packaging and release verification logic in `scripts/release-artifacts.sh`; `release.yml` should stay orchestration-focused.
- When polling workflows, use long poll times (10-20 minutes). Avoid short polls or waits.

## Alternate Path

The workflow can also run automatically when a merged PR carries the `autorelease` label. That path uses the same workflow and the same downstream tap automation.
