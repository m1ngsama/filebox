# Releasing

Releases are rare and deliberate. A release is a promise that the build was looked at, so there are no release candidates, nightly tags or releases made to fix the last one in a hurry; a problem found afterwards is fixed on `main` and shipped in the next version.

## Before tagging

1. `main` is what you mean to ship, and the `ci` workflow passed on that exact commit.
2. `make check` passes locally, including the browser tests.
3. Anything that changes the database says so in the commit message, because database changes only go one way.
4. Versions are `vMAJOR.MINOR.PATCH`, nothing else. Below 1.0.0 the first number promises nothing.

## Tagging

```sh
git tag -a v0.0.1 -m "v0.0.1"
git push origin v0.0.1
```

The `release` workflow refuses a tag that is not a plain version, not on `main`, or whose commit has no passing CI run. Otherwise it builds Linux and macOS binaries for amd64 and arm64, adds checksums and build attestations, and opens a **draft** release. Nothing is public yet.

## Reviewing the draft

Download the archives from the draft and check them like a user would:

```sh
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify filebox_v0.0.1_linux_amd64.tar.gz --repo m1ngsama/filebox
tar xzf filebox_v0.0.1_linux_amd64.tar.gz && ./filebox_v0.0.1_linux_amd64/filebox version
```

Read the generated notes and rewrite them so they say what changed for someone running filebox, and what needs doing before upgrading.

## Publishing

Publish the draft by hand on GitHub. Publishing triggers the `image` workflow, which builds the multi-architecture Docker image and pushes `ghcr.io/m1ngsama/filebox:vX.Y.Z`, plus `latest`.

A published release is never edited or replaced. If it is wrong, mark it as such in a new release's notes, or delete it if it is dangerous.

Updating a server is always done by hand; no workflow reaches one.
