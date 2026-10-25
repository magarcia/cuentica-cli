# Maintainer release guide

GitHub Actions runs tests, builds release archives, and publishes GitHub Releases from version tags. It uses the repository's `GITHUB_TOKEN`; it needs no Cuéntica API key or personal access token.

## Publish the repository

These are maintainer commands. Creating the public repository and pushing tags publish the source and release assets.

1. Review the source, license, dependency notices, and ignored files. Keep credentials, `.env` files, downloaded documents, and local build output out of Git.
2. If this checkout has no Git repository, initialize it and create the first commit:

   ```sh
   git init -b main
   git add .
   git commit -m "Prepare Cuéntica CLI for public release"
   ```

3. Create the public repository with the [GitHub CLI](https://cli.github.com/):

   ```sh
   gh repo create magarcia/cuentica-cli --public --source=. --remote=origin --push
   ```

   If the repository or `origin` already exists, check the remote before pushing instead of recreating it.

4. Enable GitHub Actions and allow the GitHub-owned actions used by the workflows. Public repositories support the artifact attestations used by this release workflow. No repository secrets are required.
5. Enable private vulnerability reporting under the repository's security settings, as described in [SECURITY.md](../SECURITY.md). Review Dependabot alerts and repository access.
6. After the first CI run, protect `main` with a ruleset requiring pull requests and the CI checks you want enforced. GitHub settings control these rules; committing workflow files doesn't enable branch protection.

The workflow files are [ci.yml](../.github/workflows/ci.yml) and [release.yml](../.github/workflows/release.yml). Dependency update settings live in [dependabot.yml](../.github/dependabot.yml).

## Prepare a version

Start from the intended release commit on `main`. Keep the working tree clean and confirm CI passes.

```sh
just check
just test-race
just clean
just release-snapshot
VERSION=snapshot just verify-release
```

The snapshot builds six archives and verifies their checksums, contents, build metadata, and target platforms. `--run` also exercises the archive matching your host without contacting Cuéntica or reading credentials.

Use a SemVer tag with a leading `v`:

- Stable release: `v0.1.0`, `v1.2.3`.
- Prerelease: `v0.1.0-rc.1`, `v1.2.3-beta.2`.

The tag provides the binary version. The build embeds the tagged commit and its commit date. There's no source version file to update. Review changes since the previous tag so GitHub's generated release notes describe the release accurately.

## Publish a release

Create the tag on the release commit and push it explicitly:

```sh
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

Replace `v0.1.0` with the chosen version. Pushing a `v*` tag starts the release workflow; invalid SemVer tags fail validation.

The workflow runs these stages before publication:

| Stage | Checks or output |
| --- | --- |
| CI | Tests and native builds on Linux, macOS, and Windows with Go 1.26 and current stable Go; Linux race checks |
| Package | Cross-compile six binaries with `CGO_ENABLED=0`, archive them, and calculate SHA-256 checksums |
| Verify | Validate archive contents, target metadata, and checksums; smoke-test native archives on all three operating systems |
| Attest | Record GitHub build provenance for the six archives and checksum manifest in a public repository |
| Publish | Upload assets to a draft, then publish a GitHub Release with generated notes |

Stable versions become the latest release. Versions with a prerelease suffix are marked as prereleases and don't replace the latest stable version.

A published release isn't overwritten. If a run fails, inspect its failed job and rerun after fixing the cause. A retry can replace assets in its draft release. Once a release is published, publish corrections under a new version instead of moving its tag.

## Release assets

Each release contains these files, where `<version>` includes the tag's leading `v`:

| Target | Archive |
| --- | --- |
| macOS x86-64 | `cuentica_<version>_darwin_amd64.tar.gz` |
| macOS ARM64 | `cuentica_<version>_darwin_arm64.tar.gz` |
| Linux x86-64 | `cuentica_<version>_linux_amd64.tar.gz` |
| Linux ARM64 | `cuentica_<version>_linux_arm64.tar.gz` |
| Windows x86-64 | `cuentica_<version>_windows_amd64.zip` |
| Windows ARM64 | `cuentica_<version>_windows_arm64.zip` |
| SHA-256 manifest | `checksums.txt` |

Every archive includes `cuentica` or `cuentica.exe`, `README.md`, `LICENSE`, and `THIRD_PARTY_NOTICES`. The release job's archive verification requires these files.

Release binaries aren't code signed or notarized. GitHub attestations identify the repository workflow that built an archive; they don't replace OS code signing. Native smoke tests cover the runner's architecture. ARM64 binaries on other operating systems are cross-compiled and inspected, but don't all run on native CI hardware.

## Update Homebrew

Homebrew is the default installation method on macOS and Linux:

```sh
brew install magarcia/tap/cuentica
```

After publishing each stable release, update [magarcia/homebrew-tap](https://github.com/magarcia/homebrew-tap). Publishing a GitHub Release doesn't update the tap automatically.

1. In `~/dev/homebrew-tap/Formula/cuentica.rb`, replace the four archive URLs with the new release's macOS/Linux ARM64/amd64 URLs. Homebrew infers the version from the URL.
2. Replace the four SHA-256 values with the matching entries from the release's `checksums.txt`.
3. Publish the formula change and wait for the tap's Cuéntica workflow to pass installation, completions, functional tests, and audits on all four targets.
4. Check `brew update`, `brew upgrade magarcia/tap/cuentica`, and `cuentica --version` against the new release.

Keep the tap's README and this repository's installation instructions consistent. Prereleases remain available as GitHub downloads.

## Verify a published release

Download assets from a specific release into an empty directory:

```sh
gh release download v0.1.0 --repo magarcia/cuentica-cli --dir release-v0.1.0
cd release-v0.1.0
shasum -a 256 --check checksums.txt
```

On Linux, `sha256sum --check checksums.txt` works too. With every archive present, all six checksum checks should pass.

Verify an archive's GitHub attestation before extracting it:

```sh
gh attestation verify cuentica_v0.1.0_darwin_arm64.tar.gz --repo magarcia/cuentica-cli
```

Then extract the archive for your system and run `cuentica --version`, `cuentica --help`, and a `--dry-run` request. Verify that the version and commit match the release. These checks need no API key.

## Package locally

Use the same version, commit, and build date for packaging and verification:

```sh
just clean
export VERSION=v0.1.0
export COMMIT="$(git rev-parse HEAD)"
export BUILD_DATE="$(git log -1 --format=%cI)"
just release
just verify-release
```

Release packaging refuses unexpected files or stale release assets in `dist/`. Run `just clean` before packaging another version. Local packaging writes files only; it doesn't create a GitHub Release.
