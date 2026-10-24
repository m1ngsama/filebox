# Install and update

## A release build

Linux and macOS, amd64 and arm64. Download the archive for your system from the [Releases](https://github.com/m1ngsama/filebox/releases) page, check it, and unpack it.

```sh
sha256sum -c SHA256SUMS --ignore-missing    # on macOS: shasum -a 256 -c SHA256SUMS --ignore-missing
gh attestation verify filebox_*_linux_amd64.tar.gz --repo m1ngsama/filebox
tar xzf filebox_*_linux_amd64.tar.gz && sudo install filebox_*/filebox /usr/local/bin/
filebox version
```

The attestation proves that GitHub Actions built the archive from this repository.

## Docker

`ghcr.io/m1ngsama/filebox` for amd64 and arm64. [deploy/docker-compose.yml](../deploy/docker-compose.yml) is a starting point. On macOS with colima, which shares only your home folder with its virtual machine, keep the data and volume folders under it; a folder outside it looks empty and unwritable to the container. Make the first account with:

```sh
docker compose run --rm -T filebox passwd -data /data < password.txt
```

## A service

[deploy/filebox.service](../deploy/filebox.service) is a hardened systemd unit, and [deploy/nginx.conf.example](../deploy/nginx.conf.example) a reverse proxy that streams large uploads. Both use placeholder names and paths.

## From source

Requires Go 1.26.10+ and [bun](https://bun.sh).

```sh
make build        # bin/filebox
make test e2e
```

`go install` cannot build filebox, because the web interface is built with bun and embedded in the binary.

## Update

Updating is manual on purpose: nothing in this repository, and no workflow, ever connects to your server.

1. Stop filebox.
2. Copy `filebox.db` from the data directory somewhere safe.
3. Put the new binary in place, or pull the new image.
4. Start filebox.

Database changes apply on start and only go one way. Keep the copy until you trust the new version, and roll back with the old binary and the old copy together. `filebox version` shows which build is running.
