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
Usage: just test [server|desktop|integration|docs|tui|rust] [--timeout SECONDS] [--workers COUNT] [--inherit-env] [-- TOOL-ARGS...]

The timeout controls Go package and Bun per-test execution, not total command runtime.
Without an explicit timeout, Bun uses its native per-test default.
Bare `just test` runs server, desktop, and production-binary integration tests concurrently.
USAGE
}

target="default"
timeout_seconds=300
workers=""
desktop_workers=8
native_timeout_args=()
inherit_env=0
tool_args=()

while [ "$#" -gt 0 ]; do
	case "$1" in
	server | desktop | integration | docs | tui | rust)
		[ "$target" = "default" ] || { echo "Select one test target" >&2; exit 2; }
		target="$1"
		shift
		;;
	--timeout)
		[ "$#" -ge 2 ] || { echo "--timeout requires seconds" >&2; exit 2; }
		timeout_seconds="$2"
		native_timeout_args=("--timeout=${timeout_seconds}000")
		shift 2
		;;
	--workers)
		[ "$#" -ge 2 ] || { echo "--workers requires a count" >&2; exit 2; }
		workers="$2"
		desktop_workers="$2"
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
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

case "$target" in
default | desktop | docs) just _node ;;
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
	run_suite server go test -count=1 -ldflags="-s -w" -o "$repo_root/.generated/test-binaries/" -p "$workers" "${go_test_timeout_args[@]}" "${args[@]}"
}

run_desktop_tests() {
	local status=0
	bun test "$@" '--path-ignore-patterns=**/*.test.tsx' "${native_timeout_args[@]}" "${tool_args[@]}" || status=$?
	bun test "$@" --preload=./test/setup.ts '--path-ignore-patterns=**/*.test.ts' "${native_timeout_args[@]}" "${tool_args[@]}" || status=$?
	return "$status"
}

run_desktop() {
	local argument
	for argument in "${tool_args[@]}"; do
		case "$argument" in
		--reporter-outfile | --reporter-outfile=*)
			echo "--reporter-outfile requires separate Bun environments; the combined desktop target cannot write both reports to one file." >&2
			exit 2
			;;
		esac
	done
	just --quiet _gen-typescript
	local protocol_version
	protocol_version="$(jq -ce '.version | strings | select(length > 0)' shared/protocol/version.json)"
	local args=(
		--cwd apps/desktop
		"--parallel=$desktop_workers" --isolate
		--tsconfig-override=tsconfig.json
		--preload=./test/commonSetup.ts
		"--define=__KENT_PROTOCOL_VERSION__=$protocol_version"
		"--define=import.meta.env=globalThis.__KENT_TEST_ENV__"
		'--path-ignore-patterns=test/**'
		'--path-ignore-patterns=eslint-fixtures/**'
		'--path-ignore-patterns=**/node_modules/**'
		'--path-ignore-patterns=**/dist/**'
		'--path-ignore-patterns=**/gen/**'
		'--path-ignore-patterns=**/*.spec.*'
		'--path-ignore-patterns=**/*_spec.*'
		'--path-ignore-patterns=**/*_test.*'
		'--path-ignore-patterns=**/*.test.js'
		'--path-ignore-patterns=**/*.test.jsx'
		'--path-ignore-patterns=**/*.test.mjs'
		'--path-ignore-patterns=**/*.test.cjs'
		'--path-ignore-patterns=**/*.test.mts'
		'--path-ignore-patterns=**/*.test.cts'
		--pass-with-no-tests
	)
	run_suite desktop run_desktop_tests "${args[@]}"
}

run_tui() {
	mkdir -p "$repo_root/.generated/test-binaries"
	run_suite tui go test -count=1 -ldflags="-s -w" -o "$repo_root/.generated/test-binaries/" -p "$workers" "${go_test_timeout_args[@]}" ./cli/tui/... "${tool_args[@]}"
}

run_integration() {
	mkdir -p "$repo_root/.generated/test-binaries"
	just --quiet build go --output "$repo_root/.generated/test-binaries/kent"
	run_suite integration go test -count=1 -ldflags="-s -w" -o "$repo_root/.generated/test-binaries/" -p "$workers" "${go_test_timeout_args[@]}" -tags=integration ./internal/testharness/pty "${tool_args[@]}"
}

run_docs() {
	run_suite docs bun test "${native_timeout_args[@]}" docs/scripts docs/src "${tool_args[@]}"
}

run_rust() {
	run_suite rust cargo test --manifest-path tui-rs/Cargo.toml --workspace --locked "${tool_args[@]}"
}

case "$target" in
default)
	run_server
	run_desktop
	run_integration
	;;
server) run_server ;;
desktop) run_desktop ;;
integration) run_integration ;;
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
