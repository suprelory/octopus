"""Exercise the actual publication step without calling GitHub or uploading files."""

import contextlib
import json
import os
import pathlib
import subprocess
import tempfile
import textwrap
import unittest
from unittest import mock


WORKFLOW = pathlib.Path(__file__).resolve().parents[1] / ".github/workflows/release.yaml"
PUBLISH_STEP = WORKFLOW.read_text(encoding="utf-8").split("      - name: Publish Release\n", 1)[1].split("\n      - name:", 1)[0]
PUBLISH_CODE = compile(textwrap.dedent(PUBLISH_STEP.split("        run: |\n", 1)[1]), str(WORKFLOW), "exec")


class PublishReleaseTests(unittest.TestCase):
    def run_step(self, release=None, artifacts=True, failure=None):
        with tempfile.TemporaryDirectory() as directory, contextlib.chdir(directory):
            archive = pathlib.Path("build/archives/octopus-linux-amd64.zip")
            if artifacts:
                archive.parent.mkdir(parents=True)
                archive.write_bytes(b"test archive")
            lookup = subprocess.CompletedProcess(
                [], 1 if release is None else 0,
                stdout="" if release is None else json.dumps(release), stderr="",
            )
            results = [lookup, failure or subprocess.CompletedProcess([], 0)]
            with mock.patch.dict(os.environ, {"RELEASE_TAG": "v1.0.1"}), mock.patch("subprocess.run", side_effect=results) as run:
                try:
                    exec(PUBLISH_CODE, {})
                    error = None
                except (SystemExit, subprocess.CalledProcessError) as caught:
                    error = caught
                return run.call_args_list, error, str(archive)

    def test_creates_new_release_with_artifacts_and_notes(self):
        calls, error, archive = self.run_step()
        self.assertIsNone(error)
        self.assertEqual(calls[-1], mock.call([
            "gh", "release", "create", "v1.0.1", archive,
            "--verify-tag", "--title", "v1.0.1", "--generate-notes",
        ], check=True))

    def test_completes_notes_only_release_without_recreating_it(self):
        calls, error, archive = self.run_step({"tagName": "v1.0.1", "assets": []})
        self.assertIsNone(error)
        self.assertEqual(calls[-1], mock.call(["gh", "release", "upload", "v1.0.1", archive], check=True))

    def test_refuses_existing_assets_or_another_tag(self):
        for release in [
            {"tagName": "v1.0.1", "assets": [{"name": "checksums.sha256"}]},
            {"tagName": "v1.0.0", "assets": []},
        ]:
            with self.subTest(release=release):
                calls, error, _ = self.run_step(release)
                self.assertIsInstance(error, SystemExit)
                self.assertEqual(len(calls), 1)

    def test_missing_artifacts_never_creates_a_release(self):
        calls, error, _ = self.run_step(artifacts=False)
        self.assertIsInstance(error, SystemExit)
        self.assertEqual(calls, [])

    def test_upload_errors_stop_publication(self):
        failure = subprocess.CalledProcessError(1, ["gh", "release", "upload"])
        calls, error, _ = self.run_step({"tagName": "v1.0.1", "assets": []}, failure=failure)
        self.assertIs(error, failure)
        self.assertEqual(len(calls), 2)


if __name__ == "__main__":
    unittest.main()
