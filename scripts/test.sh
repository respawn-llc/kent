#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

case "$(uname -s)" in
MINGW* | MSYS* | CYGWIN*)
	echo "Local test tooling has not been built for Windows." >&2
	exit 2
	;;
esac

usage() {
	cat <<'USAGE'
Usage: just test [server|desktop|docs|tui|rust] [--timeout SECONDS] [--workers COUNT] [--inherit-env] [-- TOOL-ARGS...]

An explicit timeout sets Go's package timeout and Bun's per-test timeout. Without it, Bun uses its native default.
The timeout does not limit total command runtime.
Bare `just test` runs server and desktop tests.
USAGE
}

target="default"
timeout_seconds=300
timeout_explicit=0
workers=""
inherit_env=0
tool_args=()

while [ "$#" -gt 0 ]; do
	case "$1" in
	server | desktop | docs | tui | rust)
		[ "$target" = "default" ] || { echo "Select one test target" >&2; exit 2; }
		target="$1"
		shift
		;;
	--timeout)
		[ "$#" -ge 2 ] || { echo "--timeout requires seconds" >&2; exit 2; }
		timeout_seconds="$2"
		timeout_explicit=1
		shift 2
		;;
	--workers)
		[ "$#" -ge 2 ] || { echo "--workers requires a count" >&2; exit 2; }
		workers="$2"
		shift 2
		;;
	--inherit-env)
		inherit_env=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	--)
		shift
		tool_args=("$@")
		break
		;;
	*)
		echo "Unknown test argument: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

case "$timeout_seconds" in
'' | *[!0-9]*) echo "--timeout must be a positive integer" >&2; exit 2 ;;
esac
[ "$timeout_seconds" -gt 0 ] || { echo "--timeout must be a positive integer" >&2; exit 2; }

if [ -z "$workers" ]; then
	workers=4
	if command -v nproc >/dev/null 2>&1; then
		workers="$(nproc)"
	elif command -v sysctl >/dev/null 2>&1; then
		workers="$(sysctl -n hw.ncpu 2>/dev/null || echo 4)"
	fi
	[ "$workers" -le 10 ] || workers=10
fi
case "$workers" in
'' | *[!0-9]*) echo "--workers must be a positive integer" >&2; exit 2 ;;
esac
[ "$workers" -gt 0 ] || { echo "--workers must be a positive integer" >&2; exit 2; }

if [ "$inherit_env" -eq 0 ]; then
	while IFS= read -r name; do
		case "$name" in
		KENT_*) unset "$name" ;;
		esac
	done < <(compgen -e KENT_ || true)
fi

[ "$target" != "default" ] || [ "${#tool_args[@]}" -eq 0 ] || {
	echo "Tool arguments require an explicit test target" >&2
	exit 2
}

case "$target" in
default | desktop | docs) just _node ;;
esac

started="$SECONDS"
failure_labels=()
failure_logs=()
failure_statuses=()
logs=()
pty_fixture_root=""
go_test_timeout_args=(-timeout "${timeout_seconds}s")

cleanup() {
	local log
	for log in "${logs[@]}"; do
		unlink "$log" 2>/dev/null || true
	done
	if [ -n "$pty_fixture_root" ]; then
		find "$pty_fixture_root" -depth -delete 2>/dev/null || true
	fi
}
trap cleanup EXIT

run_suite() {
	local label="$1"
	shift
	local suite_started="$SECONDS"
	local log status
	log="$(mktemp -t "kent-${label}-tests.XXXXXX.log")"
	logs+=("$log")

	set +e
	"$@" >"$log" 2>&1
	status=$?
	set -e

	if [ "$status" -eq 0 ]; then
		printf '✓ %s tests passed in %ds\n' "$label" "$((SECONDS - suite_started))"
		return
	fi
	failure_labels+=("$label")
	failure_logs+=("$log")
	failure_statuses+=("$status")
}

prepare_pty_fixtures() {
	pty_fixture_root="$(mktemp -d -t kent-pty-fixtures.XXXXXX)"
	go test -c -o "$pty_fixture_root/kent-pty-fixture.test" core/cli/app
	just --quiet build _go --output "$pty_fixture_root/kent"
	go build -o "$pty_fixture_root/ansi-writer" core/internal/testharness/pty/testdata/cmd/ansi-writer
	go build -o "$pty_fixture_root/phase-input-writer" core/internal/testharness/pty/testdata/cmd/phase-input-writer
	go build -o "$pty_fixture_root/phase-writer" core/internal/testharness/pty/testdata/cmd/phase-writer
	export KENT_PTY_FIXTURE_BINARY="$pty_fixture_root/kent-pty-fixture.test"
	export KENT_PTY_KENT_BINARY="$pty_fixture_root/kent"
	export KENT_PTY_ANSI_WRITER_BINARY="$pty_fixture_root/ansi-writer"
	export KENT_PTY_PHASE_INPUT_WRITER_BINARY="$pty_fixture_root/phase-input-writer"
	export KENT_PTY_PHASE_WRITER_BINARY="$pty_fixture_root/phase-writer"
}

run_server() {
	local args=("${tool_args[@]}")
	just --quiet _gen-go
	if [ "${#args[@]}" -eq 0 ]; then
		prepare_pty_fixtures
		args=(./...)
	fi
	run_suite server go test -p "$workers" "${go_test_timeout_args[@]}" "${args[@]}"
}

run_desktop() {
	local args=("--workers=$workers")
	if [ "$timeout_explicit" -eq 1 ]; then
		args+=("--timeout=${timeout_seconds}000")
	fi
	just --quiet _gen-typescript
	run_suite desktop bun run --cwd apps/desktop tooling/run-tests.ts "${args[@]}" "${tool_args[@]}"
}

run_tui() {
	just --quiet install _prepare-openai
	run_suite tui go test -p "$workers" "${go_test_timeout_args[@]}" ./cli/tui/... "${tool_args[@]}"
}

run_docs() {
	local args=()
	if [ "$timeout_explicit" -eq 1 ]; then
		args+=("--timeout=${timeout_seconds}000")
	fi
	run_suite docs bun test "${args[@]}" docs/scripts docs/src "${tool_args[@]}"
}

run_rust() {
	run_suite rust cargo test --manifest-path tui-rs/Cargo.toml --workspace --locked "${tool_args[@]}"
}

case "$target" in
default)
	run_server
	run_desktop
	;;
server) run_server ;;
desktop) run_desktop ;;
docs) run_docs ;;
tui) run_tui ;;
rust) run_rust ;;
esac

if [ "${#failure_labels[@]}" -eq 0 ]; then
	[ "$target" != "default" ] || printf '✓ all tests passed in %ds\n' "$((SECONDS - started))"
	exit 0
fi

for index in "${!failure_labels[@]}"; do
	label="${failure_labels[$index]}"
	status="${failure_statuses[$index]}"
	printf '\n✗ %s tests %s in %ds\n' \
		"$label" \
		"failed" \
		"$((SECONDS - started))" >&2
	printf '──── %s test output ────\n' "$label" >&2
	cat "${failure_logs[$index]}" >&2
	printf '──── end %s test output ────\n' "$label" >&2
done

printf '\n✗ %d test runner(s) failed in %ds\n' "${#failure_labels[@]}" "$((SECONDS - started))" >&2
if [ "$target" != "default" ] && [ "${#failure_statuses[@]}" -eq 1 ] && [ "${failure_statuses[0]}" -ne 124 ]; then
	exit "${failure_statuses[0]}"
fi
exit 1
