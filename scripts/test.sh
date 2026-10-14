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
Usage: just test [server|desktop|docs|tui|rust] [--timeout SECONDS] [--workers COUNT] [--no-wall-clock-cap] [--inherit-env] [-- TOOL-ARGS...]

The timeout covers the complete command, including every selected test process.
Bare `just test` runs server and desktop tests under one timeout.
USAGE
}

target="default"
timeout_seconds=180
workers=""
wall_clock_cap=1
inherit_env=0
tool_args=()
invocation_args=("$@")

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
	--no-wall-clock-cap)
		wall_clock_cap=0
		shift
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

if [ "${_KENT_TEST_SUPERVISED:-}" != "$$" ]; then
	exec python3 "$repo_root/scripts/test-supervisor.py" "$wall_clock_cap" "$timeout_seconds" "$repo_root/scripts/test.sh" "${invocation_args[@]}"
fi
unset _KENT_TEST_SUPERVISED

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
pty_fixture_root=""
ram_device=""
test_temp_root=""
go_test_timeout_args=()
if [ "$wall_clock_cap" -eq 1 ]; then
	go_test_timeout_args=(-timeout "${timeout_seconds}s")
fi

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
	if [ -n "$pty_fixture_root" ]; then
		find "$pty_fixture_root" -depth -delete 2>/dev/null || true
	fi
	if [ -n "$test_temp_root" ]; then
		if ! find "$test_temp_root" -mindepth 1 -depth -delete; then
			status=1
		fi
		if [ -z "$ram_device" ]; then
			rmdir "$test_temp_root" || status=1
		fi
	fi
	if [ -n "$ram_device" ]; then
		diskutil unmount force "$ram_device" >/dev/null || status=1
		diskutil eject "$ram_device" >/dev/null || {
			echo "Could not detach test RAM disk $ram_device" >&2
			status=1
		}
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
		ram_device="$(diskutil image attach --noMount --plist ram://4194304 |
			python3 -c 'import plistlib,sys; print("/dev/" + plistlib.loads(sys.stdin.buffer.read())["system-entities"][0]["dev-entry"])')"
		diskutil eraseVolume HFS+ "kent-tests-$$" "$ram_device" >/dev/null
		test_temp_root="$(diskutil info -plist "$ram_device" |
			python3 -c 'import plistlib,sys; print(plistlib.loads(sys.stdin.buffer.read())["MountPoint"])')"
		;;
	Linux)
		[ "$(stat -f -c %T /dev/shm)" = "tmpfs" ] || {
			echo "Tests require a memory-backed /dev/shm for filesystem fixtures." >&2
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
prepare_test_storage

run_suite() {
	local label="$1"
	shift
	local log
	log="$(mktemp "$TMPDIR/kent-${label}-tests.XXXXXX")"
	logs+=("$log")
	suite_labels+=("$label")
	"$@" >"$log" 2>&1 &
	suite_pids+=("$!")
}

prepare_pty_fixtures() {
	pty_fixture_root="$(mktemp -d "$TMPDIR/kent-pty-fixtures.XXXXXX")"
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
		run_suite developer-tools just --quiet _test-developer-tools
		prepare_pty_fixtures
		args=(./...)
	fi
	run_suite server go test -exec "bash \"$repo_root/scripts/test-exec.sh\"" -p "$workers" "${go_test_timeout_args[@]}" "${args[@]}"
}

run_desktop() {
	just --quiet _gen-typescript
	run_suite desktop bun run --cwd apps/desktop vitest --config tooling/vite.config.ts run "${tool_args[@]}"
}

run_tui() {
	run_suite tui go test -exec "bash \"$repo_root/scripts/test-exec.sh\"" -p "$workers" "${go_test_timeout_args[@]}" ./cli/tui/... "${tool_args[@]}"
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
