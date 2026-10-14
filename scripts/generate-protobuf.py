#!/usr/bin/env python3
"""Just's change-driven protobuf generation cache."""

import fcntl
import hashlib
import json
import subprocess
import sys
from pathlib import Path


def digest(root, paths):
    result = hashlib.sha256()
    for path in sorted(paths):
        relative = path.relative_to(root).as_posix().encode()
        contents = path.read_bytes()
        result.update(len(relative).to_bytes(8, "big"))
        result.update(relative)
        result.update(len(contents).to_bytes(8, "big"))
        result.update(contents)
    return result.hexdigest()


def generate(root, language):
    if language == "go":
        output = root / "shared/protoapi/gen"
        tool_inputs = ["tools.mod", "tools.sum", "go.mod"]
    elif language == "typescript":
        output = root / "apps/desktop/packages/server-api-contract/src/gen"
        tool_inputs = ["apps/package.json", "apps/bun.lock"]
    else:
        raise ValueError(f"unknown protobuf language: {language}")
    template = "buf.gen.go.yaml" if language == "go" else "buf.gen.ts.yaml"
    inputs = [
        root / name
        for name in ["buf.yaml", template, "scripts/generate-protobuf.py", *tool_inputs]
    ]
    for directory in ["api/proto", "third_party/protovalidate/proto"]:
        inputs.extend((root / directory).rglob("*.proto"))

    cache = root / ".generated/protobuf"
    cache.mkdir(parents=True, exist_ok=True)
    manifest = cache / f"{language}.json"
    with (cache / f"{language}.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        fingerprint = digest(root, inputs)
        previous = None
        if manifest.exists():
            try:
                previous = json.loads(manifest.read_text())
            except json.JSONDecodeError:
                pass  # Interrupted or damaged disposable caches are regenerated.
        outputs = list(output.rglob("*")) if output.exists() else []
        outputs = [path for path in outputs if path.is_file()]
        if outputs and previous == {
            "inputs": fingerprint,
            "outputs": digest(root, outputs),
        }:
            return
        subprocess.run(
            ["just", "--quiet", f"_generate-{language}-source"],
            cwd=root,
            check=True,
        )
        if digest(root, inputs) != fingerprint:
            raise RuntimeError("protobuf inputs changed during generation; rerun the command")
        outputs = [path for path in output.rglob("*") if path.is_file()]
        if not outputs:
            raise RuntimeError(f"protobuf generation produced no {language} outputs")
        temporary = manifest.with_suffix(".tmp")
        temporary.write_text(json.dumps({
            "inputs": fingerprint,
            "outputs": digest(root, outputs),
        }))
        temporary.replace(manifest)


if __name__ == "__main__":
    try:
        generate(Path.cwd(), sys.argv[1])
    except (IndexError, ValueError, OSError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"protobuf generation failed: {error}", file=sys.stderr)
        sys.exit(1)
