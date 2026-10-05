"""Implementation of `just dev onboarding [true|false]`."""

import argparse
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


def stop(process):
    if process is not None and process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            print("QA process did not stop within 15 seconds; killing it.", file=sys.stderr)
            process.kill()
            process.wait()


def wait_for_server(server, port):
    # Onboarding deliberately makes /readyz unavailable until the user finishes.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        if server.poll() is not None:
            raise RuntimeError(f"QA server exited with status {server.returncode}")
        try:
            with opener.open(f"http://127.0.0.1:{port}/healthz", timeout=1) as response:
                health = json.load(response)
            if health["pid"] != server.pid or health["status"] != "ok":
                raise RuntimeError("QA port is not serving the server we started")
            return
        except (urllib.error.URLError, TimeoutError):
            time.sleep(0.1)
    raise RuntimeError("QA server did not start within 30 seconds")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("cleanup", nargs="?", choices=("true", "false"), default="true")
    args = parser.parse_args()
    if not sys.stdin.isatty() or not sys.stdout.isatty():
        parser.error("run `just dev onboarding` in an interactive terminal")

    repo = Path(__file__).resolve().parent.parent
    # Keep Unix socket paths below the platform limit, including on macOS.
    root = Path(tempfile.mkdtemp(prefix="kent-onboarding-", dir="/tmp"))
    server = None
    tui = None
    log = None
    result = 1

    def interrupt(signum, _frame):
        # Ctrl+C belongs to the foreground TUI once it is running.
        if signum != signal.SIGINT or tui is None:
            raise SystemExit(128 + signum)

    for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(signum, interrupt)
    try:
        binary = root / "bin" / "kent"
        print("Building Kent for fresh-install QA...", flush=True)
        subprocess.run(
            ["just", "build", "go", "--output", str(binary)], cwd=repo, check=True
        )
        env = {key: value for key, value in os.environ.items() if not key.startswith("KENT_")}
        for variable, directory in {
            "HOME": "home",
            "XDG_CONFIG_HOME": "config",
            "XDG_CACHE_HOME": "cache",
            "XDG_DATA_HOME": "data",
            "XDG_STATE_HOME": "state",
            "XDG_RUNTIME_DIR": "runtime",
            "TMPDIR": "tmp",
        }.items():
            path = root / directory
            path.mkdir(mode=0o700)
            env[variable] = str(path)
        workspace = root / "workspace"
        workspace.mkdir()
        # Reserve an ephemeral candidate, never the production default.
        while True:
            with socket.socket() as candidate:
                candidate.bind(("127.0.0.1", 0))
                port = candidate.getsockname()[1]
            if port != 53082:
                break
        env.update(
            KENT_PERSISTENCE_ROOT=str(root / "home" / ".kent"),
            KENT_SERVER_HOST="127.0.0.1",
            KENT_SERVER_PORT=str(port),
            PATH=str(binary.parent) + os.pathsep + env.get("PATH", os.defpath),
        )
        print(f"QA directory: {root}\nStarting server on 127.0.0.1:{port}...", flush=True)
        log = (root / "server.log").open("w+", encoding="utf-8", errors="replace")
        server = subprocess.Popen(
            [str(binary), "serve"], cwd=workspace, env=env,
            stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        wait_for_server(server, port)
        tui = subprocess.Popen([str(binary)], cwd=workspace, env=env)
        result = tui.wait()
        if server.poll() is not None:
            raise RuntimeError(f"QA server exited with status {server.returncode}")
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Onboarding QA failed: {error}", file=sys.stderr)
        result = 1
    finally:
        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            signal.signal(signum, signal.SIG_IGN)
        stop(tui)
        stop(server)
        if log is not None:
            if result != 0:
                log.seek(0)
                shutil.copyfileobj(log, sys.stderr)
            log.close()
        if args.cleanup == "true":
            shutil.rmtree(root)
        else:
            print(f"Retained QA state (may contain credentials): {root}", flush=True)
    return result if result >= 0 else 128 - result


if __name__ == "__main__":
    sys.exit(main())
