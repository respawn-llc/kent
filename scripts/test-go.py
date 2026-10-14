"""Just's full-suite Go runner; Go owns existing executable validation."""

import argparse
import concurrent.futures
from contextlib import contextmanager
import fcntl
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import threading

ARTIFACT_ROOT = Path(".generated/test-binaries")


def test_output(root, import_path):
    return root / ARTIFACT_ROOT / import_path / "tests.test"


@contextmanager
def artifact_lock(output):
    output.parent.mkdir(parents=True, exist_ok=True)
    with output.with_suffix(".lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        yield


def test_command(output, import_path, prepare_only):
    return ["go", "test", "-c" if prepare_only else "-count=1", "-ldflags=-s -w", "-o", str(output), import_path]


def packages(root):
    output = subprocess.check_output(["go", "list", "-json", "./..."], cwd=root, text=True)
    decoder = json.JSONDecoder()
    position = 0
    while position < len(output):
        while position < len(output) and output[position].isspace():
            position += 1
        if position == len(output):
            break
        package, position = decoder.raw_decode(output, position)
        yield package


def prepare_fixtures(root):
    targets = root / ARTIFACT_ROOT
    app = test_output(root, "core/cli/app")
    builds = [("KENT_PTY_FIXTURE_BINARY", app, test_command(app, "core/cli/app", True))]
    kent = targets / "pty/kent"
    builds.append(("KENT_PTY_KENT_BINARY", kent, ["just", "--quiet", "build", "_go", "--output", str(kent)]))
    for name, variable in (
        ("ansi-writer", "KENT_PTY_ANSI_WRITER_BINARY"),
        ("phase-input-writer", "KENT_PTY_PHASE_INPUT_WRITER_BINARY"),
        ("phase-writer", "KENT_PTY_PHASE_WRITER_BINARY"),
    ):
        output = targets / "pty" / name
        builds.append((variable, output, ["go", "build", "-o", str(output), f"core/internal/testharness/pty/testdata/cmd/{name}"]))
    for variable, output, command in builds:
        with artifact_lock(output):
            subprocess.run(command, cwd=root, check=True)
        os.environ[variable] = str(output)


def run_package(root, package, timeout, prepare_only, output_lock):
    output = test_output(root, package["ImportPath"])
    with artifact_lock(output):
        command = test_command(output, package["ImportPath"], prepare_only)
        if timeout is not None and not prepare_only:
            command.extend(["-timeout", timeout])
        if not prepare_only:
            command.extend(["-exec", f'bash "{Path(__file__).with_name("test-exec.sh").resolve()}"'])
        with tempfile.TemporaryFile() as log:
            result = subprocess.run(command, cwd=root, stdout=log, stderr=log)
            log.seek(0)
            with output_lock:
                shutil.copyfileobj(log, sys.stdout.buffer)
                sys.stdout.buffer.flush()
            return result.returncode


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--workers", type=int, required=True)
    parser.add_argument("--timeout")
    parser.add_argument("--prepare-only", action="store_true")
    options = parser.parse_args()
    if options.workers <= 0:
        parser.error("workers must be positive")
    root = Path.cwd()
    prepare_fixtures(root)
    failed = False
    output_lock = threading.Lock()
    with concurrent.futures.ThreadPoolExecutor(max_workers=options.workers) as pool:
        futures = [pool.submit(run_package, root, package, options.timeout, options.prepare_only, output_lock) for package in packages(root)]
        for future in concurrent.futures.as_completed(futures):
            failed |= future.result() != 0
    return 1 if failed else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, subprocess.CalledProcessError) as error:
        print(f"Go test runner failed: {error}", file=sys.stderr)
        raise SystemExit(1)
