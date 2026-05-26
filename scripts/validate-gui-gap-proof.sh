#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
proof_dir="${1:-$repo_root/.builder/proofs/gui-gap-closure}"
manifest="$proof_dir/manifest.md"

required_files=(
	"$manifest"
	"$proof_dir/screenshots/home-dark.png"
	"$proof_dir/screenshots/home-light.png"
	"$proof_dir/screenshots/project-edit.png"
	"$proof_dir/screenshots/board-valid.png"
	"$proof_dir/screenshots/board-invalid.png"
	"$proof_dir/screenshots/task-detail.png"
	"$proof_dir/screenshots/task-detail-inbox-run.png"
)

required_manifest_terms=(
	"Home/project list"
	"Project edit/workspace management"
	"Valid workflow board"
	"Invalid workflow board"
	"Task detail layout"
	"Inbox blocker/runtime controls"
	"Forced theme override"
	"CLI command copy"
	"Question and approval Inbox controls"
	"Reconnect/draft behavior"
	"No direct DB writes"
)

for path in "${required_files[@]}"; do
	if [[ ! -s "$path" ]]; then
		printf 'missing proof artifact: %s\n' "$path" >&2
		exit 1
	fi
done

for term in "${required_manifest_terms[@]}"; do
	if ! grep -Fq "$term" "$manifest"; then
		printf 'manifest missing acceptance mapping: %s\n' "$term" >&2
		exit 1
	fi
done

if grep -Fq "Board bottom-edge shadow bleed" "$manifest" && [[ ! -s "$proof_dir/screenshots/board-valid-compact.png" ]]; then
	printf 'manifest maps board bottom-edge shadow bleed, but compact board screenshot is missing\n' >&2
	exit 1
fi

if find "$proof_dir" -maxdepth 1 -type f \( -name '*.pid' -o -name '*tmp*' -o -name '*marker*' \) | grep -q .; then
	printf 'proof directory contains runtime marker files:\n' >&2
	find "$proof_dir" -maxdepth 1 -type f \( -name '*.pid' -o -name '*tmp*' -o -name '*marker*' \) >&2
	exit 1
fi

printf 'gui gap proof manifest valid: %s\n' "$proof_dir"
