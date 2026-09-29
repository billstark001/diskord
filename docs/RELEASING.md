# Release procedure

`frontend/package.json` is the single source of the application version. Its
value is an npm semantic version without the leading `v`; the build scripts
inject `v<version>` into the Go binary. Ad hoc `go build` output reports `dev`.
Configuration and SQLite schema version numbers are separate compatibility
markers and do not follow the application release number.

1. Update `frontend/package.json` and its pnpm lockfile, then move completed
   entries from `Unreleased` to a dated section in `CHANGELOG.md`. Commit these
   changes before tagging. The tag must be exactly `v<package version>`.
2. Run `./scripts/build.sh`, `./scripts/verify.sh`, and `./scripts/dist.sh` from
   a clean checkout. Check `dist/diskord-<os>-<arch>[.exe]` for both macOS and
   Windows architectures; run the host binary's `version` command and confirm
   the expected tag. These checks build the embedded frontend from the pinned
   pnpm lockfile. They do not test a Discord desktop installation.
3. Push the release commit to `main` and wait for the `build-and-release`
   workflow on that commit to pass. Its validation job runs lint and race tests;
   the platform matrix builds four single-file executables, including a native
   Windows AMD64 test run.
4. Create and push an annotated tag on that exact commit. For example:

   ```sh
   release_tag="v$(node -p "require('./frontend/package.json').version")"
   git tag -a "$release_tag" -m "$release_tag"
   git push origin "$release_tag"
   ```

5. Watch the tag's `build-and-release` run. The validation job rejects tags
   that differ from the package version or have no changelog section. After all
   jobs pass, the release job publishes the four executables and GitHub's
   automatic source archives. Tags with a hyphen are marked prereleases.
   Confirm the release and all four assets in GitHub before announcing it.

If a pre-tag check fails, fix it on `main` and rerun the checks before tagging.
If a tag has already been published, keep it immutable; make a new patch or
prerelease version for a corrected build. Release binaries are unsigned and
not notarized; platform-specific trust and Discord integration still require
manual acceptance testing.
