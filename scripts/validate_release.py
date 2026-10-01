"""Validate a release's tag/commit identity without publishing anything."""

import argparse
import re
import subprocess
import sys


def validate_release(ref, sha):
    if not re.fullmatch(r"refs/tags/v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", ref):
        raise ValueError("Releases must run from a version tag (including manual runs)")
    if not re.fullmatch(r"[0-9a-fA-F]{40,64}", sha):
        raise ValueError("Expected a full triggering commit SHA")

    def resolve(revision):
        return subprocess.check_output(
            ["git", "rev-parse", "--verify", revision], text=True, stderr=subprocess.PIPE
        ).strip()

    tagged = resolve(ref + "^{commit}")
    if tagged != sha or resolve("HEAD") != sha:
        raise ValueError("The version tag, triggering commit and checkout must match")
    return ref.removeprefix("refs/tags/")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref", required=True)
    parser.add_argument("--sha", required=True)
    args = parser.parse_args()
    try:
        tag = validate_release(args.ref, args.sha)
    except (ValueError, subprocess.CalledProcessError) as error:
        print(f"Release validation failed: {error}", file=sys.stderr)
        return 1
    print(f"tag={tag}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
