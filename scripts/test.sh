#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$repo_root"

if [ "${BUILDER_TEST_INHERIT_ENV:-}" != "1" ]; then
    while IFS= read -r name; do
        case "$name" in
            BUILDER_SKIP_FRONTEND|BUILDER_TEST_DISABLE_WALL_CLOCK_CAP|BUILDER_TEST_FRONTEND|BUILDER_TEST_INHERIT_ENV|BUILDER_TEST_TIMEOUT_SECONDS)
                ;;
            BUILDER_*)
                unset "$name"
                ;;
        esac
    done < <(compgen -e BUILDER_ || true)
fi

go_log_file="$(mktemp -t builder-go-test.XXXXXX.log)"
frontend_log_file="$(mktemp -t builder-frontend-test.XXXXXX.log)"
test_pid=""
cleanup() {
    rm -f "$go_log_file" "$frontend_log_file"
}
trap cleanup EXIT

terminate_test_process_group() {
    if [ -z "${test_pid:-}" ] || ! kill -0 "$test_pid" 2>/dev/null; then
        return
    fi
    kill -TERM "-$test_pid" 2>/dev/null || kill -TERM "$test_pid" 2>/dev/null || true
    sleep 2
    kill -KILL "-$test_pid" 2>/dev/null || kill -KILL "$test_pid" 2>/dev/null || true
}

handle_interrupt() {
    terminate_test_process_group
    exit 130
}

handle_term() {
    terminate_test_process_group
    exit 143
}

trap handle_interrupt INT
trap handle_term TERM

disable_wall_clock_cap="${BUILDER_TEST_DISABLE_WALL_CLOCK_CAP:-0}"
case "$disable_wall_clock_cap" in
    0|1)
        ;;
    *)
        printf 'BUILDER_TEST_DISABLE_WALL_CLOCK_CAP must be 0 or 1\n' >&2
        exit 2
        ;;
esac

timeout_seconds="${BUILDER_TEST_TIMEOUT_SECONDS:-120}"
if [ "$disable_wall_clock_cap" != "1" ]; then
    case "$timeout_seconds" in
        ''|*[!0-9]*)
            printf 'BUILDER_TEST_TIMEOUT_SECONDS must be a positive integer <= 120\n' >&2
            exit 2
            ;;
    esac
    if [ "$timeout_seconds" -le 0 ] || [ "$timeout_seconds" -gt 120 ]; then
        printf 'BUILDER_TEST_TIMEOUT_SECONDS must be a positive integer <= 120\n' >&2
        exit 2
    fi
fi
args=("$@")
run_frontend="${BUILDER_TEST_FRONTEND:-auto}"
if [ ${#args[@]} -eq 0 ]; then
    args=(./...)
    if [ "$run_frontend" = "auto" ]; then
        run_frontend=1
    fi
elif [ "$run_frontend" = "auto" ]; then
    run_frontend=0
fi

run_frontend_tests() {
    if [ "${BUILDER_SKIP_FRONTEND:-0}" = "1" ]; then
        return
    fi
    if [ "$run_frontend" != "1" ]; then
        return
    fi
    if [ ! -f apps/package.json ]; then
        return
    fi
    if ! command -v pnpm >/dev/null 2>&1; then
        printf 'pnpm is required to run frontend tests. Install pnpm or set BUILDER_SKIP_FRONTEND=1.\n' >&2
        exit 2
    fi
    if pnpm --dir apps install --frozen-lockfile >"$frontend_log_file" 2>&1 &&
        pnpm --dir apps test >>"$frontend_log_file" 2>&1; then
        return
    fi
    cat "$frontend_log_file"
    exit 1
}

if [ "$disable_wall_clock_cap" = "1" ]; then
    set +e
    go test "${args[@]}" >"$go_log_file" 2>&1
    status=$?
    set -e
    if [ "$status" -eq 0 ]; then
        run_frontend_tests
        exit 0
    fi
    cat "$go_log_file"
    exit "$status"
fi

if ! command -v python3 >/dev/null 2>&1; then
    printf 'python3 is required to run tests with a wall-clock timeout\n' >&2
    exit 2
fi

python3 - "$go_log_file" "${args[@]}" <<'PY' &
import os
import sys

log_file = sys.argv[1]
args = sys.argv[2:]
os.setsid()
fd = os.open(log_file, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
try:
    os.dup2(fd, 1)
    os.dup2(fd, 2)
finally:
    os.close(fd)
os.execvp("go", ["go", "test", *args])
PY
test_pid=$!
timed_out=0
deadline=$((SECONDS + timeout_seconds))

while kill -0 "$test_pid" 2>/dev/null; do
    if [ "$SECONDS" -ge "$deadline" ]; then
        timed_out=1
        terminate_test_process_group
        break
    fi
    sleep 1
done

set +e
wait "$test_pid"
status=$?
set -e
if [ "$status" -eq 0 ]; then
    run_frontend_tests
    exit 0
fi

if [ "$timed_out" -eq 1 ]; then
    printf 'test suite exceeded %ds wall-clock cap; simplify or speed up tests before continuing\n' "$timeout_seconds"
elif [ "$status" -eq 143 ] || [ "$status" -eq 137 ]; then
    printf 'test process was terminated by a signal (exit status %d)\n' "$status"
fi
cat "$go_log_file"
exit 1
