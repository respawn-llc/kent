#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$repo_root"

usage() {
	cat <<'USAGE'
Usage: scripts/build-desktop.sh [--version vX.Y.Z|X.Y.Z] [--skip-install] [-- <tauri build args>]

Builds the Kent desktop (Tauri) app bundle. The bundle version is stamped from
VERSION (or KENT_VERSION / --version) at build time via a `tauri build --config`
merge, so the committed manifests (tauri.conf.json, package.json, Cargo.toml)
stay at their 0.0.0 placeholder and are never hand-edited per release.

Options:
  --version       Override the bundle version. Defaults to KENT_VERSION or VERSION.
  --skip-install  Skip the workspace dependency install step.
  -h, --help      Show this help.

Arguments after `--` are forwarded to `tauri build`, e.g.:
  scripts/build-desktop.sh -- --bundles dmg --target aarch64-apple-darwin
USAGE
}

read_version() {
	local version="${KENT_VERSION:-}"
	if [ -z "$version" ] && [ -f VERSION ]; then
		version="$(tr -d '[:space:]' <VERSION)"
	fi
	printf '%s' "${version#v}"
}

version=""
skip_install=0
tauri_args=()

while [[ $# -gt 0 ]]; do
	case "$1" in
	--version)
		version="${2:-}"
		shift 2
		;;
	--skip-install)
		skip_install=1
		shift
		;;
	--)
		shift
		tauri_args=("$@")
		break
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "Unknown argument: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

if [ -z "$version" ]; then
	version="$(read_version)"
fi
version="${version#v}"

if [ -z "$version" ]; then
	echo "Unable to resolve version. Set --version, KENT_VERSION, or a VERSION file." >&2
	exit 1
fi

if ! command -v pnpm >/dev/null 2>&1; then
	echo "pnpm is required to build the desktop app." >&2
	exit 2
fi

if [ "$skip_install" != "1" ]; then
	pnpm --dir apps install --frozen-lockfile
fi

echo "Building Kent desktop bundle version ${version}" >&2

pnpm --dir apps/desktop exec tauri build \
	--config "{\"version\":\"${version}\"}" \
	${tauri_args[@]+"${tauri_args[@]}"}
