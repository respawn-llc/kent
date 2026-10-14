"""Reuse a worktree-owned macOS RAM volume; isolate each invocation's fixtures."""

import fcntl
import os
from pathlib import Path
import plistlib
import subprocess
import tempfile
from urllib.parse import urlparse


def diskutil(*args):
    return subprocess.check_output(["diskutil", *args])


root = Path(__file__).resolve().parent.parent / ".generated"
root.mkdir(exist_ok=True)
mount = root / "test-ram"
with (root / "test-storage.lock").open("a") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    if not os.path.ismount(mount.resolve()):
        if mount.is_symlink():
            mount.unlink()
        elif mount.exists():
            raise RuntimeError(f"Expected a RAM-volume link at {mount}")
        attached = plistlib.loads(diskutil("image", "attach", "--noMount", "--plist", "ram://4194304"))
        device = "/dev/" + attached["system-entities"][0]["dev-entry"]
        try:
            diskutil("eraseVolume", "HFS+", "KentTestFixtures", device)
            diskutil("unmount", device)
            diskutil("mount", "nobrowse", device)
            info = plistlib.loads(diskutil("info", "-plist", device))
            mount.symlink_to(info["MountPoint"])
        except BaseException:
            subprocess.run(["diskutil", "eject", device], check=True)
            raise
    info = plistlib.loads(diskutil("info", "-plist", str(mount)))
    if info["VolumeName"] != "KentTestFixtures" or info["FilesystemType"] != "hfs":
        raise RuntimeError(f"Unexpected filesystem at {mount}")
    images = plistlib.loads(subprocess.check_output(["hdiutil", "info", "-plist"]))["images"]
    device = info["DeviceNode"]
    ram_backed = any(
        urlparse(image["image-path"]).scheme == "ram"
        and any(entity.get("dev-entry") == device for entity in image["system-entities"])
        for image in images
    )
    if not ram_backed:
        raise RuntimeError(f"Expected RAM-backed fixture storage at {mount}")
    print(tempfile.mkdtemp(prefix="run-", dir=mount.resolve()))
