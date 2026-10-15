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

The timeout controls Go test execution. Go's emergency process deadline also covers TestMain preparation.
Other test runners use their native timeout settings.
Bare `just test` runs server and desktop tests concurrently.
USAGE
}

target="default"
timeout_seconds=180
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

started="$SECONDS"
failure_labels=()
failure_logs=()
failure_statuses=()
logs=()
suite_labels=()
suite_pids=()
next_suite=0
test_temp_root=""
go_test_timeout_args=(-timeout "${timeout_seconds}s")

finish_suites() {
	local status label log
	while [ "$next_suite" -lt "${#suite_pids[@]}" ]; do
		label="${suite_labels[$next_suite]}"
		log="${logs[$next_suite]}"
		if wait "${suite_pids[$next_suite]}"; then
			status=0
		else
			status=$?
		fi
		next_suite=$((next_suite + 1))
		if [ "$status" -eq 0 ]; then
			if [ -n "${JUST_VERBOSE:-}" ]; then
				cat "$log"
			fi
			printf '✓ %s tests passed\n' "$label"
		else
			failure_labels+=("$label")
			failure_logs+=("$log")
			failure_statuses+=("$status")
		fi
	done
}

cleanup() {
	local status=$?
	local log index label
	trap - EXIT
	finish_suites
	for index in "${!failure_labels[@]}"; do
		label="${failure_labels[$index]}"
		printf '\n✗ %s tests failed (exit %s)\n' "$label" "${failure_statuses[$index]}" >&2
		printf '──── %s test output ────\n' "$label" >&2
		cat "${failure_logs[$index]}" >&2
		printf '──── end %s test output ────\n' "$label" >&2
	done
	for log in "${logs[@]}"; do
		unlink "$log" 2>/dev/null || true
	done
	if [ -n "$test_temp_root" ]; then
		if ! find "$test_temp_root" -mindepth 1 -depth -delete; then
			status=1
		fi
		rmdir "$test_temp_root" || status=1
	fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

case "$target" in
default | desktop | docs) just _node ;;
esac

prepare_test_storage() {
	# Keep compiler workspaces separate from test fixtures, including on noexec tmpfs.
	export GOTMPDIR="${GOTMPDIR:-${TMPDIR:-/tmp}}"
	case "$(uname -s)" in
	Darwin)
		test_temp_root="$(python3 "$repo_root/scripts/test-storage.py")"
		;;
	Linux)
		[ "$(stat -f -c %T /dev/shm)" = "tmpfs" ] || {
			echo "Tests require a memory-backed /dev/shm for filesystem fixtures." >&2
			exit 2
		}
		python3 -c 'import os,sys; sys.exit(bool(os.statvfs("/dev/shm").f_flag & os.ST_NOEXEC))' || {
			echo "Tests require executable RAM storage: /dev/shm is mounted noexec. Configure its mount with exec enabled." >&2
			exit 2
		}
		test_temp_root="$(mktemp -d /dev/shm/kent-tests.XXXXXX)"
		;;
	*)
		echo "Memory-backed test storage is not configured for this platform." >&2
		exit 2
		;;
	esac
	mkdir "$test_temp_root/tmp" "$test_temp_root/outside"
	export TMPDIR="$test_temp_root/tmp"
	export TMP="$TMPDIR"
	export TEMP="$TMPDIR"
	export KENT_TEST_NON_TEMPORARY_ROOT="$test_temp_root/outside"
}
case "$target" in
default | server | tui) prepare_test_storage ;;
esac

run_suite() {
	local label="$1"
	shift
	local log
	log="$(mktemp "${TMPDIR:-/tmp}/kent-${label}-tests.XXXXXX")"
	logs+=("$log")
	suite_labels+=("$label")
	"$@" >"$log" 2>&1 &
	suite_pids+=("$!")
}

run_server() {
	local args=("${tool_args[@]}")
	just --quiet _gen-go
	if [ "${#args[@]}" -eq 0 ]; then
		args=(./...)
	fi
	mkdir -p "$repo_root/.generated/test-binaries"
	run_suite server go test -count=1 -ldflags="-s -w" -o "$repo_root/.generated/test-binaries/" -exec "bash \"$repo_root/scripts/test-exec.sh\"" -p "$workers" "${go_test_timeout_args[@]}" "${args[@]}"
}

run_desktop() {
	just --quiet _gen-typescript
	run_suite desktop bun run --cwd apps/desktop vitest --config tooling/vite.config.ts run "${tool_args[@]}"
}

run_tui() {
	mkdir -p "$repo_root/.generated/test-binaries"
	run_suite tui go test -count=1 -ldflags="-s -w" -o "$repo_root/.generated/test-binaries/" -exec "bash \"$repo_root/scripts/test-exec.sh\"" -p "$workers" "${go_test_timeout_args[@]}" ./cli/tui/... "${tool_args[@]}"
}

run_docs() {
	run_suite docs bun test docs/scripts docs/src "${tool_args[@]}"
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
finish_suites

if [ "${#failure_labels[@]}" -eq 0 ]; then
	printf '✓ test command passed in %ds\n' "$((SECONDS - started))"
	exit 0
fi

printf '\n✗ %d test runner(s) failed in %ds\n' "${#failure_labels[@]}" "$((SECONDS - started))" >&2
if [ "$target" != "default" ] && [ "${#failure_statuses[@]}" -eq 1 ] && [ "${failure_statuses[0]}" -ne 124 ]; then
	exit "${failure_statuses[0]}"
fi
exit 1
