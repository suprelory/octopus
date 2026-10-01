"""Validate a release's tag/commit identity without publishing anything."""

import argparse
import re
import subprocess
import sys


def resolve_release(ref, sha, requested_tag=""):
    release_ref = "refs/tags/" + requested_tag if requested_tag else ref
    if not re.fullmatch(r"refs/tags/v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", release_ref):
        raise ValueError("Releases require a version tag; specify the tag for a manual run")
    if not re.fullmatch(r"[0-9a-fA-F]{40,64}", sha):
        raise ValueError("Expected a full triggering commit SHA")

    def resolve(revision):
        return subprocess.check_output(
            ["git", "rev-parse", "--verify", revision], text=True, stderr=subprocess.PIPE
        ).strip()

    tagged = resolve(release_ref + "^{commit}")
    if resolve("HEAD") != sha or (not requested_tag and tagged != sha):
        raise ValueError("The version tag, triggering commit and checkout must match")
    return release_ref.removeprefix("refs/tags/"), tagged


def validate_release(ref, sha):
    tag, _ = resolve_release(ref, sha)
    return tag


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref", required=True)
    parser.add_argument("--sha", required=True)
    parser.add_argument("--tag", default="", help="Existing version tag for a manual recovery run")
    parser.add_argument("--resolve-only", action="store_true", help="Resolve the exact commit before checking and building it")
    args = parser.parse_args()
    if args.tag and not args.resolve_only:
        parser.error("--tag requires --resolve-only; validate the final checkout without an override")
    try:
        tag, sha = resolve_release(args.ref, args.sha, args.tag)
    except (ValueError, subprocess.CalledProcessError) as error:
        print(f"Release validation failed: {error}", file=sys.stderr)
        return 1
    print(f"tag={tag}")
    if args.resolve_only:
        print(f"sha={sha}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
