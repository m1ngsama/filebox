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

## Deploying to production

Publishing a release also starts the `deploy` workflow, which waits in the `production` environment. The maintainer approves it on the run's page on GitHub; only then does it SSH to the server and name the version. The server downloads that release from GitHub itself, checks the archive against the release's `SHA256SUMS`, checks the binary reports the same version, swaps it in, restarts the service and waits for it to answer. If it does not, it goes back to the previous binary. The workflow then checks that https://cloud.m1ng.space answers, and asks the server to go back if it does not. A prerelease is never deployed.

The deploy key can run one root-owned script, `deploy/filebox-deploy`, and the script accepts one line: a plain version, or `rollback`. The key and its host pin exist only in the `production` environment, which accepts version tags only. A pull request, a branch and the CI workflow never see them.

### One-time setup

On the server, as root:

```sh
install -m 755 -o root -g root deploy/filebox-deploy /usr/local/sbin/filebox-deploy
echo 'm1ng ALL=(root) NOPASSWD: /usr/local/sbin/filebox-deploy' > /etc/sudoers.d/filebox-deploy
chmod 440 /etc/sudoers.d/filebox-deploy && visudo -c
```

Generate a key used for nothing else (`ssh-keygen -t ed25519 -N '' -f deploy-key -C filebox-deploy`) and add the public half to the service account's `~/.ssh/authorized_keys` as

```
restrict,command="sudo /usr/local/sbin/filebox-deploy" ssh-ed25519 AAAA... filebox-deploy
```

In the repository's Settings → Environments → `production`:

| Kind | Name | Value |
| --- | --- | --- |
| secret | `DEPLOY_HOST` | the address the server's SSH is reachable on from the internet |
| secret | `DEPLOY_USER` | the service account |
| secret | `DEPLOY_SSH_KEY` | the private half of the key above |
| secret | `DEPLOY_KNOWN_HOSTS` | the server's host key line, e.g. `[host]:port ssh-ed25519 AAAA...`, taken from the server itself so the host is pinned |
| variable | `DEPLOY_PORT` | the SSH port |

Updating a server by hand, or going back to an older release, still works: run `echo v0.0.1 | sudo /usr/local/sbin/filebox-deploy` on it.
