#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$repo_root"

if [ "${KENT_SKIP_FRONTEND:-0}" = "1" ] || [ ! -f apps/package.json ]; then
	exit 0
fi
if ! command -v bun >/dev/null 2>&1; then
	echo "Bun is required to install frontend dependencies." >&2
	exit 2
fi

bun install --cwd apps --frozen-lockfile
