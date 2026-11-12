# release

## NamiLinkLabs fork releases

Fork releases are tagged `v<next upstream minor>-nl.<N>`, for example `v0.10.0-nl.1`:
the fork's work on top of what will become upstream `v0.10.0`. `d2 --version` reports
the tag. The MSI's ProductVersion drops the suffix (`0.10.0`), as for any prerelease.
GitHub does not mark `-nl.N` drafts as prereleases.

1. On a branch from `master`, move `ci/release/changelogs/next.md` to
   `ci/release/changelogs/<tag>.md` and reset `next.md` from `template.md`. Set
   `d2js/js/package.json` and `package-lock.json` to a D2.js version with the same
   `-nl.N` suffix, for example `0.1.35-nl.1`. Merge the branch.
2. Tag the merge commit with an annotated tag and push it:
   `git tag -a v0.10.0-nl.1 -m v0.10.0-nl.1 <commit> && git push origin v0.10.0-nl.1`.
3. `release-archives.yml` runs the full CI, builds, smoke-tests and attests the archives
   and D2.js, and creates a draft release with the archives, `SHA256SUMS`, the SBOM and
   `d2js-<tag>.tgz`. The MSI is built and attached only when the repository variable
   `WIX7_EULA_ACCEPTED` is `true`, which the repository owner sets after accepting the
   WiX 7 EULA.
4. Review the draft and publish it.

`d2js-<tag>.tgz` is the `@namilinklabs/d2` npm package built from the tag. Install it
by URL to use a release before it is on npm:

```sh
npm install https://github.com/NamiLinkLabs/d2/releases/download/v0.10.0-nl.1/d2js-v0.10.0-nl.1.tgz
```

The URL works once the release is published. The package's `version()` returns the
tag and `jsVersion()` its package version.

## npm publishing

The fork publishes D2.js as `@namilinklabs/d2`. Upstream's `@d2lang/d2` and
`@terrastruct/d2` belong to the upstream project and are not published from here.

### First publish (once)

npm does not allow a brand-new package to use staged publishing, so the first version
is published directly from a machine with Go, Bun, Node and
[binaryen](https://github.com/WebAssembly/binaryen) installed:

1. On npmjs.com, create the `namilinklabs` organization and a short-lived granular
   access token that can publish to it.
2. From a clean checkout of `master`, with `d2js/js/package.json` at the version to
   publish:

   ```sh
   cd d2js/js
   bun install --frozen-lockfile
   NPM_VERSION="$(node -p "require('./package.json').version")" \
     NPM_PUBLISH_MODE=publish NPM_TOKEN=<token> ./ci/build.sh
   ```

   The script builds and packs the package, publishes it with the `latest` tag and
   checks the registry tarball matches. A retry skips a version only when the registry
   tarball matches the local one exactly. The token is kept in a temporary npm config
   and never written to the checkout.
3. On the package's settings page on npmjs.com, add a trusted publisher: GitHub
   Actions, repository `NamiLinkLabs/d2`, workflow `npm-stage.yml`, environment
   `npm-release`. Then revoke the token.

### Later versions

1. Bump `d2js/js/package.json` and `package-lock.json` to the new version and merge it.
   Nightly stages derive their prerelease version from the checked-in version, so they
   need no bump.
2. Run the **npm stage** workflow from `master` with that version, or `nightly`. To
   package a specific D2 release, set `d2_ref` to its exact tag, for example
   `v0.10.0-nl.1`. The workflow checks the tag is an ancestor of the dispatched commit
   and builds that tag's source. Without `d2_ref`, it builds the dispatched commit.
   The WASM reports its exact D2 tag or source commit independently of the npm version.
3. The workflow stages the package on npm with trusted publishing. It only runs from
   `refs/heads/master`, and the `npm-release` environment should only accept protected
   branches.
4. Check the pending stage with `npm stage list` and `npm stage view <stage-id>`, and
   compare `npm stage download <stage-id>` with the workflow's `npm-packages` artifact.
   Approve it with `npm stage approve <stage-id>`, or reject an abandoned release with
   `npm stage reject <stage-id>`. A pending stage reserves its version, so list pending
   stages before retrying.

## _install.sh

The template for the install script in the root of the d2 repository.

### gen_install.sh

Generates the install.sh script in the root of the repository by prepending the libraries
it depends on from ../sub/lib.

## release.sh

- ./release.sh is the top level script to generate a new release.
  Run with --help for usage.

Run from a clean checkout with Python 3, Git, and an authenticated GitHub CLI. The script
prepares the release branch, changelog, PR, and tag, then returns. It leaves existing PR
descriptions unchanged and uses the Human/AI template for new PRs. Prereleases must have
a semantic-version suffix, for example `--version=v0.9.0-rc.1`; `--prerelease` with a
stable-looking version is rejected.

A version is assigned once. The script never amends a release commit, force-pushes a branch,
or replaces a tag, including tags for draft releases. It checks local and remote release
refs before making changes. For an unfinished release, a matching remote tag makes
preparation a no-op with guidance to follow the existing Actions run. Published releases
and drafts with an MSI reject preparation. Conflicting tags or release branches require a
new version. A local tag left by a failed push can be pushed only when it points to the
unchanged prepared commit, without recreating the tag object. An untagged partial preparation
reuses its existing changelog commit and pushes the branch normally; it does not create another
empty commit.
If a remote preparation commit is unavailable locally, fetch origin before retrying.

Enable **Release immutability** in the repository's release settings to enforce the lock
on GitHub. Draft assets remain editable until publication; the uploader still accepts only
identical existing assets. Publishing freezes the release assets and associated tag, so
attach and verify every asset before publishing. Release notes/title and prerelease/latest
status remain editable. Enabling this setting applies to future published releases; it does
not retroactively lock already published releases. See [GitHub's release immutability
settings](https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/establish-provenance-and-integrity/prevent-release-changes).

Pushing the tag starts one `Release archives` dependency graph:

1. The existing CI workflow tests the exact tag commit. In parallel, the archive job builds
   all six archives twice using the pinned Go toolchain, compares SHA-256 digests, verifies
   stripped binaries and normalized archives, enforces size budgets, and generates an SBOM.
2. Each archive runs natively on its supported platform, checking its version, SVG, and PNG.
3. The reusable `Windows MSI` workflow consumes that run's Windows amd64 archive, verifies
   its checksum, and builds and verifies the installer on `windows-2022`. It always packages
   notices from the new archive, so an old published release is no longer a PR test fixture.
4. After tests, native smoke, and MSI verification succeed, the workflow attests the archive
   provenance, SBOM, and checksum manifest. A separate write-scoped job creates or resumes a
   draft and uploads the same archives, `SHA256SUMS`, MSI, and `d2.spdx.json`.

The uploader rechecks the tag commit and draft identity/state before uploads and verifies
all final asset digests. Existing assets are accepted only when their bytes match; nothing
is overwritten. There is no workstation download/upload handoff or independent MSI poller.

After the workflow succeeds, review and merge the release PR, then publish the complete
draft manually on GitHub. `release.sh` rejects `--publish`, `--skip-build`, and `--rebuild`,
and never repurposes an existing version. To recover a failed run without changing code or
artifacts, use **Re-run failed jobs** on that run. Code or build changes require a new version,
even if the old version is still a draft. Individual Actions artifacts are immutable, but
reruns can create newer artifacts with the same names; downloads by name select the newest.
Retry failed jobs to reuse completed build outputs. If draft upload was interrupted, the
failed job accepts identical existing assets and attaches only missing ones. A GitHub upload
failure can leave an empty `starter` asset; after rechecking the tag, draft, and asset identity,
the uploader deletes only that zero-byte placeholder and retries its upload. Changed or
completed assets are never deleted. Artifacts are retained for 30 days; recover failures
within that window. Run recovery without another uploader modifying the same draft.

Release-related PRs exercise the same archive and native smoke jobs and, when the WiX gate
below is enabled, the MSI build. PRs never attest or write release assets. CI is called
directly by the tag workflow, so it does not depend on another token-generated event.

Every release produced by this workflow includes `SHA256SUMS`. To verify archive
provenance and its checksum:

```sh
gh attestation verify d2-v0.0.0-linux-amd64.tar.gz --repo d2lang/d2
sha256sum --check --ignore-missing SHA256SUMS
```

The MSI remains unsigned. Authenticode signing is tracked separately in
[issue #1078](https://github.com/d2lang/d2/issues/1078).

The installer uses WiX 7. Before the workflow can install or run WiX 7, a repository owner
must review the [WiX Open Source Maintenance Fee and EULA terms](https://docs.firegiant.com/wix/osmf/),
confirm that any required sponsorship is in place, and set the `WIX7_EULA_ACCEPTED`
repository variable to exactly `true`. That explicit owner-controlled gate enables the
workflow to pass `-acceptEula wix7`. Pull requests still build and smoke-test their release
archives when the variable is absent, but skip MSI packaging. Tag-triggered builds fail
closed when the variable is absent; no draft is uploaded.

## build.sh

Release verification limits each binary to 45,000,000 bytes and each archive to
20,000,000 bytes. The archive limit accommodates native isometric rendering on
macOS as well as Linux and Windows. All six release targets retain size limits
and metadata/checksum verification.

- ./build.sh builds the release archives for each platform into ./build/<VERSION>/*.tar.gz
  Run with --help for usage.

Use `--host-only` to build only the release for the host's `$OS-$ARCH` pair. Local
development invocations still build locally. Production builds and artifact uploads belong
to the tag-triggered `Release archives` workflow; `RELEASE=1` local builds are rejected.

### Docker image helper

`./docker/build.sh` is retained as a load-only local development helper. It rejects
`--push`, `--latest`, and inherited `RELEASE` publishing. The release script no longer
calls it or publishes Docker tags from a legacy AWS SSH builder.

### Production Docker publishing

The manually dispatched `Publish Docker release` GitHub workflow is the production path
for Docker Hub. It replaces the Docker portion of the legacy AWS release path; the Windows
MSI is built separately by the `Windows MSI` workflow described above.

Run it from the protected `master` branch after the GitHub release is published. Enter the
exact v-prefixed release version and leave `publish_latest` off unless this stable release
should become the default Docker image. The workflow rejects `publish_latest` for a GitHub
prerelease or a semver prerelease.

The `docker-release` GitHub environment must provide a `DOCKERHUB_USERNAME` variable set
to `d2lang` and a `DOCKERHUB_TOKEN` secret containing a personal access token for that
Docker ID. The Docker ID must have write access to both the canonical `d2lang/d2`
repository and the maintained `terrastruct/d2` compatibility mirror. The environment's
deployment branch policy must
allow only protected branches. Docker Hub tag immutability must remain enabled on both
repositories for version tags with
`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`; `latest` remains outside
that rule and mutable. This repository-side rule is the final overwrite guard.

The workflow uses a native amd64/arm64 matrix and passes per-platform digest artifacts to
its publication job. PR image smoke tests call the same build and smoke script. Release
orchestration scripts come from the protected dispatch commit; a separate checkout supplies
the Dockerfile and entrypoint from the exact release commit.

Before publishing a production tag, the workflow:

1. verifies the protected-master dispatch, published non-draft semver GitHub release, exact
   Linux amd64 and arm64 asset IDs, and their GitHub SHA-256 digests;
2. explicitly peels the Git tag to a commit, requires that commit to be an ancestor of the
   dispatched `master` commit, and uses that release commit's Dockerfile without modification;
3. builds on native GitHub-hosted amd64 and arm64 runners and pushes only untagged digests
   with provenance;
4. runs native version, SVG, and PNG smoke tests for both digests; and
5. dry-runs and validates the exact two-platform manifest, including provenance
   attestations; then
6. immediately re-fetches the release and re-peels its tag, requiring the release ID,
   publication/prerelease state, tag commit, asset IDs, and asset digests to be unchanged.

Only then does it create the requested version tag in the canonical repository and its
compatibility mirror. Existing version tags are immutable, so the workflow refuses to
overwrite one. If `publish_latest` was explicitly selected, it updates `latest` in both
repositories from the immutable version-manifest digest only after the version manifests
are published and verified. If verification or `latest` promotion fails after a version
tag was created, use GitHub's **Re-run failed jobs** action: the same run accepts an
existing version tag only when its descriptors exactly match that run's verified candidate,
and the separate `latest` job can retry without touching the version tags. A new dispatch
still refuses any existing version tag during preflight.

### _build.sh

Called by build.sh to create a release archive.

Do not invoke directly. If you want to produce a build for a single platform run build.sh
as so:

```sh
 # To only build the linux-amd64 release.
./build.sh --run=linux-amd64
```

```sh
 # To only build the linux-amd64 release locally.
./build.sh --local --run=linux-amd64
```
