import hashlib
import io
import os
from pathlib import Path
import pty
import select
import subprocess
import tarfile
import tempfile
import time
import unittest


ROOT = Path(__file__).resolve().parents[2]


class UnixInstallerTests(unittest.TestCase):
    OS = "darwin"

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.tools = self.root / "tools"
        self.tools.mkdir()
        self.calls = self.root / "calls"
        self.env = dict(os.environ, HOME=str(self.root), KENT_VERSION="9.9.9",
                        KENT_PREFIX=str(self.root / "prefix"),
                        PATH=f"{self.tools}:{os.environ['PATH']}",
                        FIXTURE_ROOT=str(self.root), FIXTURE_OS=self.OS)
        # Every Kent invocation is a fixture, never the developer's Kent binary.
        binary = b"""#!/bin/sh
echo "$*" >> "$FIXTURE_ROOT/calls"
case "$*" in
'service install')
  echo 'fixture install diagnostics' >&2
  exit "${FIXTURE_INSTALL_EXIT:-0}" ;;
*) exit 91 ;;
esac
"""
        asset = f"kent_9.9.9_{self.OS}_arm64.tar.gz"
        self.archive = self.root / asset
        with tarfile.open(self.archive, "w:gz") as archive:
            entry = tarfile.TarInfo(f"kent_9.9.9_{self.OS}_arm64")
            entry.size = len(binary)
            entry.mode = 0o755
            archive.addfile(entry, io.BytesIO(binary))
        checksum = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        (self.root / "checksums.txt").write_text(f"{checksum}  ./{asset}\n")
        self.tool("uname", '#!/bin/sh\ncase "$1" in -s) echo "$FIXTURE_OS";; -m) echo arm64;; esac\n')
        self.tool("curl", """#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in -o) output="$2"; shift 2;; *) url="$1"; shift;; esac
done
cp "$FIXTURE_ROOT/${url##*/}" "$output"
echo 200
""")

    def tool(self, name, content):
        path = self.tools / name
        path.write_text(content)
        path.chmod(0o755)

    def run_install(self, *args):
        return subprocess.run(["sh", str(ROOT / "scripts/install.sh"), *args],
                              env=self.env, stdin=subprocess.DEVNULL,
                              capture_output=True, text=True, start_new_session=True)

    def test_explicit_service_installs(self):
        result = self.run_install("--service")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.calls.read_text().splitlines(), ["service install"])

    def test_unattended_skips_service(self):
        result = self.run_install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.calls.exists())

    def test_explicit_decline_skips_service(self):
        result = self.run_install("--no-service")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.calls.exists())

    def test_service_failure_preserves_binary_and_exit(self):
        self.env["FIXTURE_INSTALL_EXIT"] = "37"
        result = self.run_install("--service")
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((self.root / "prefix/bin/kent").is_file())
        self.assertIn("fixture install diagnostics", result.stderr)

    def test_conflicting_flags_do_not_install(self):
        result = self.run_install("--service", "--no-service")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "prefix/bin/kent").exists())

    def run_terminal(self, answer, piped=False):
        pid, terminal = pty.fork()
        if pid == 0:
            if piped:
                # Same stdin topology as curl | sh, using a local script.
                command = ['sh', '-c', 'cat "$1" | sh', 'fixture',
                           str(ROOT / "scripts/install.sh")]
            else:
                command = ['sh', str(ROOT / "scripts/install.sh")]
            os.execvpe(command[0], command, self.env)
        output = bytearray()
        answered = False
        deadline = time.monotonic() + 15
        try:
            while time.monotonic() < deadline:
                ready, _, _ = select.select([terminal], [], [], 0.1)
                if ready:
                    try:
                        chunk = os.read(terminal, 65536)
                    except OSError:
                        break
                    if not chunk:
                        break
                    output.extend(chunk)
                # Synchronize on completed binary installation, not prompt copy.
                if not answered and (self.root / "prefix/bin/kent").exists():
                    os.write(terminal, answer)
                    answered = True
            else:
                os.kill(pid, 15)
                self.fail("Installer did not finish within 15 seconds")
        finally:
            os.close(terminal)
            _, status = os.waitpid(pid, 0)
        self.assertEqual(os.waitstatus_to_exitcode(status), 0, output.decode())

    def test_terminal_empty_answer_accepts(self):
        self.run_terminal(b"\n")
        self.assertEqual(self.calls.read_text().splitlines(), ["service install"])

    def test_terminal_decline(self):
        self.run_terminal(b"n\n")
        self.assertFalse(self.calls.exists())

    def test_piped_script_uses_terminal_for_answer(self):
        self.run_terminal(b"\n", piped=True)
        self.assertEqual(self.calls.read_text().splitlines(), ["service install"])

    def test_upgrade_still_offers_service(self):
        self.run_install("--no-service")
        self.run_terminal(b"\n")
        self.assertEqual(self.calls.read_text().splitlines(), ["service install"])

    def test_existing_service_refusal_is_not_forced(self):
        self.run_install("--no-service")
        self.env["FIXTURE_INSTALL_EXIT"] = "12"
        result = self.run_install("--service")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls.read_text().splitlines(), ["service install"])
        self.assertTrue((self.root / "prefix/bin/kent").exists())


class LinuxInstallerTests(UnixInstallerTests):
    OS = "linux"


if __name__ == "__main__":
    unittest.main()
