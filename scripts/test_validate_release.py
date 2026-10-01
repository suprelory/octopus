import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("validate_release.py").resolve()


class ReleaseIdentityTests(unittest.TestCase):
    def test_tagged_untagged_and_manual_references(self):
        with tempfile.TemporaryDirectory() as directory:
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=directory, text=True, stderr=subprocess.PIPE).strip()

            git("init")
            git("-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "--allow-empty", "-m", "first")
            first = git("rev-parse", "HEAD")
            git("tag", "v1.2.3")
            git("-c", "user.name=Test", "-c", "user.email=test@example.test", "tag", "-a", "v1.2.4", "-m", "annotated")

            def validate(ref, sha):
                return subprocess.run([sys.executable, str(SCRIPT), "--ref", ref, "--sha", sha], cwd=directory, text=True, capture_output=True)

            for tag in ["v1.2.3", "v1.2.4"]:
                result = validate("refs/tags/" + tag, first)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout.strip(), "tag=" + tag)
            self.assertNotEqual(validate("refs/heads/master", first).returncode, 0)
            git("-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "--allow-empty", "-m", "after tag")
            second = git("rev-parse", "HEAD")
            self.assertNotEqual(validate("refs/tags/v1.2.3", second).returncode, 0)
            self.assertNotEqual(validate("refs/tags/v1.2.3", first).returncode, 0)
            self.assertNotEqual(validate("refs/tags/v9.9.9", second).returncode, 0)
            git("checkout", "--detach", "v1.2.3")
            self.assertEqual(validate("refs/tags/v1.2.3", first).returncode, 0)

            # A fixed workflow may recover an older tag. Its own branch must
            # never become the application revision checked or built.
            git("checkout", "--detach", second)

            def recover_tag(tag, sha=second, resolve_only=True):
                command = [sys.executable, str(SCRIPT), "--ref", "refs/heads/dev", "--sha", sha, "--tag", tag]
                if resolve_only:
                    command.append("--resolve-only")
                return subprocess.run(command, cwd=directory, text=True, capture_output=True)

            result = recover_tag("v1.2.4")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.strip(), f"tag=v1.2.4\nsha={first}")
            self.assertNotEqual(recover_tag("v1.2.4", sha=first).returncode, 0)
            self.assertNotEqual(recover_tag("v1.2.4", resolve_only=False).returncode, 0)
            for invalid in ["master", "refs/tags/v1.2.4", "v9.9.9", "v1.2.4\nsha=other", "v1.2.4/../master"]:
                self.assertNotEqual(recover_tag(invalid).returncode, 0)

            git("checkout", "--detach", first)
            self.assertEqual(validate("refs/tags/v1.2.4", first).returncode, 0)
            # Moving a tag between resolution and the final checkout fails.
            git("tag", "--force", "v1.2.4", second)
            self.assertNotEqual(validate("refs/tags/v1.2.4", first).returncode, 0)


if __name__ == "__main__":
    unittest.main()
