"""Private supervisor for the complete Just test invocation."""

import os
import signal
import subprocess
import sys
import time


def main():
    capped, seconds, script, *arguments = sys.argv[1:]
    deadline = time.monotonic() + int(seconds) if capped == "1" else None
    requested_signal = None

    def interrupted(signum, _frame):
        nonlocal requested_signal
        requested_signal = signum

    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    # Bash supplies its own PID so the marker cannot accidentally propagate to
    # another invocation; the execution branch removes it before running tools.
    process = subprocess.Popen(
        ["bash", "-c", 'export _KENT_TEST_SUPERVISED=$$; exec bash "$@"', "test", script, *arguments],
        start_new_session=True,
    )

    while requested_signal is None:
        remaining = None if deadline is None else deadline - time.monotonic()
        if remaining is not None and remaining <= 0:
            break
        try:
            status = process.wait(timeout=0.1 if remaining is None else min(0.1, remaining))
            return status if status >= 0 else 128 - status
        except subprocess.TimeoutExpired:
            pass

    if requested_signal is None:
        print(f"\n✗ test command timed out after {seconds}s; stopping owned processes", file=sys.stderr)
        status = 124
    else:
        status = 128 + requested_signal

    def signal_group(signum):
        try:
            os.killpg(process.pid, signum)
            return True
        except ProcessLookupError:
            return False

    signal_group(signal.SIGTERM)
    # The deadline stops work; RAM-volume teardown and diagnostics get a bounded
    # grace period before remaining members of this invocation are killed.
    cleanup_deadline = time.monotonic() + 10
    while time.monotonic() < cleanup_deadline:
        process.poll()
        if not signal_group(0):
            break
        time.sleep(0.1)
    else:
        signal_group(signal.SIGKILL)
    process.wait()
    return status


if __name__ == "__main__":
    raise SystemExit(main())
