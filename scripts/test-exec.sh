#!/usr/bin/env bash
set -euo pipefail

# Go uses GOTMPDIR for both compiler workspaces and testing.T.TempDir.
# The compiler may use its ordinary cache; fixtures always use the RAM volume.
export GOTMPDIR="$TMPDIR"
exec "$@"
