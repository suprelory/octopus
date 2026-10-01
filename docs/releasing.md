# Releases

Create a version tag such as `v1.2.3` on the commit to release and push that tag.
The release workflow checks out the triggering commit and verifies that the tag
points to it. It then runs the backend and frontend CI workflows before building
or publishing artifacts.

For a manual run, select the version tag as the workflow reference. Running from
a branch is rejected, even if that branch currently points to a tagged commit.
The build uses that exact tag as its version; untagged local builds use a
`dev-<commit>` version instead of borrowing an older release number.

Release creation refuses an existing GitHub release. A retry cannot overwrite
its assets or continue to push container tags after that refusal. Publish code
changes under a new version tag; do not move existing release tags.

Local checks: `python -m unittest discover -s scripts -p 'test_*.py'` validates
release identity handling without uploading assets. The remote workflow also
runs lint, type checking, unit tests, Playwright Chromium tests and builds.
