#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$repo_root"

usage() {
	cat <<'USAGE'
Usage: scripts/build.sh --output /path/to/kent [--version vX.Y.Z|X.Y.Z] [--package ./cli/kent] [--skip-frontend]

Builds frontend assets and a release-profile Kent binary using a static Go toolchain configuration.

Options:
  --output   Output path for the compiled binary.
  --version  Override the embedded Kent version. Defaults to KENT_VERSION or VERSION.
  --package  Main package to build. Defaults to ./cli/kent.
  --skip-frontend
            Skip frontend asset build.
USAGE
}

read_version() {
	local version="${KENT_VERSION:-}"
	if [ -z "$version" ] && [ -f VERSION ]; then
		version="$(tr -d '[:space:]' <VERSION)"
	fi
	printf '%s' "${version#v}"
}

run_frontend_build() {
	if [ "${KENT_SKIP_FRONTEND:-0}" = "1" ]; then
		return
	fi
	if [ ! -f apps/package.json ]; then
		return
	fi
	if ! command -v pnpm >/dev/null 2>&1; then
		echo "pnpm is required to build frontend assets. Install pnpm or set KENT_SKIP_FRONTEND=1." >&2
		exit 2
	fi

	local log_file
	log_file="$(mktemp -t kent-frontend-build.XXXXXX.log)"
	if pnpm --dir apps install --frozen-lockfile >"$log_file" 2>&1 &&
		pnpm --dir apps build >>"$log_file" 2>&1; then
		rm -f "$log_file"
		return
	fi
	cat "$log_file"
	rm -f "$log_file"
	exit 1
}

output=""
package_path="./cli/kent"
version=""
skip_frontend="${KENT_SKIP_FRONTEND:-0}"

while [[ $# -gt 0 ]]; do
	case "$1" in
	--output)
		output="${2:-}"
		shift 2
		;;
	--version)
		version="${2:-}"
		shift 2
		;;
	--package)
		package_path="${2:-}"
		shift 2
		;;
	--skip-frontend)
		skip_frontend=1
		shift
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

if [ -z "$output" ]; then
	echo "--output is required" >&2
	usage >&2
	exit 1
fi

if [ -z "$version" ]; then
	version="$(read_version)"
fi
version="${version#v}"
export KENT_SKIP_FRONTEND="$skip_frontend"

mkdir -p "$(dirname -- "$output")"

run_frontend_build

ldflags=(-s -w)
if [ -n "$version" ]; then
	ldflags+=(-X "core/shared/config.Version=${version}")
fi

env CGO_ENABLED="${CGO_ENABLED:-0}" \
	go build \
	-trimpath \
	-buildvcs=false \
	-ldflags "${ldflags[*]}" \
	-o "$output" \
	"$package_path"
