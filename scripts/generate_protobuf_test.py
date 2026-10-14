import importlib.util
import subprocess
import tempfile
import threading
import unittest
from pathlib import Path
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "generate_protobuf", Path(__file__).with_name("generate-protobuf.py")
)
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)


class GenerationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        for name in [
            "buf.yaml",
            "buf.gen.go.yaml",
            "go.mod",
            "tools.mod",
            "tools.sum",
            "scripts/generate-protobuf.py",
            "api/proto/test.proto",
            "third_party/protovalidate/proto/validation.proto",
        ]:
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(name)
        self.output = self.root / "shared/protoapi/gen/test.pb.go"
        self.generations = 0

    def emit(self, *_args, **_kwargs):
        self.generations += 1
        self.output.parent.mkdir(parents=True, exist_ok=True)
        self.output.write_text(f"generation {self.generations}")

    def test_unchanged_inputs_preserve_outputs_and_input_changes_regenerate(self):
        with patch.object(generator.subprocess, "run", self.emit):
            generator.generate(self.root, "go")
            original = self.output.stat().st_mtime_ns
            generator.generate(self.root, "go")
            self.assertEqual(self.generations, 1)
            self.assertEqual(self.output.stat().st_mtime_ns, original)
            (self.root / "api/proto/test.proto").write_text("changed")
            generator.generate(self.root, "go")
            self.assertEqual(self.generations, 2)

    def test_deleted_or_modified_outputs_regenerate(self):
        with patch.object(generator.subprocess, "run", self.emit):
            generator.generate(self.root, "go")
            self.output.unlink()
            generator.generate(self.root, "go")
            self.assertEqual(self.generations, 2)
            self.output.write_text("modified")
            generator.generate(self.root, "go")
            self.assertEqual(self.generations, 3)

    def test_failed_generation_does_not_commit_cache(self):
        with patch.object(
            generator.subprocess, "run",
            side_effect=subprocess.CalledProcessError(7, ["just"]),
        ):
            with self.assertRaises(subprocess.CalledProcessError):
                generator.generate(self.root, "go")
        with patch.object(generator.subprocess, "run", self.emit):
            generator.generate(self.root, "go")
            self.assertEqual(self.generations, 1)

    def test_concurrent_generation_only_publishes_once(self):
        entered = threading.Event()
        release = threading.Event()
        errors = []

        def emit(*args, **kwargs):
            entered.set()
            release.wait()
            self.emit(*args, **kwargs)

        def generate():
            try:
                generator.generate(self.root, "go")
            except Exception as error:
                errors.append(error)

        with patch.object(generator.subprocess, "run", emit):
            first = threading.Thread(target=generate)
            second = threading.Thread(target=generate)
            first.start()
            entered.wait()
            second.start()
            release.set()
            first.join()
            second.join()
        self.assertEqual(errors, [])
        self.assertEqual(self.generations, 1)


if __name__ == "__main__":
    unittest.main()
