# Running filebox

```sh
echo 'your-password' | filebox passwd -data /srv/data/filebox
filebox serve -data /srv/data/filebox -listen :5280 -ffmpeg /usr/bin/ffmpeg -origin https://files.example.com \
  -vol downloads=/mnt/disk/downloads -vol archive=/mnt/disk/archive
filebox token -data /srv/data/filebox new "laptop"         # app password for WebDAV and the command line
```

Each `-vol name=path` is a volume: a folder on a local disk that filebox serves. `-ffmpeg` is optional and turns on WebP thumbnails and video streaming. `-origin` is the public address, needed for passkeys and share links.

## Behind a reverse proxy

Turn off request and response buffering (nginx: `proxy_request_buffering off; proxy_buffering off;`) and forward `X-Real-IP` and `X-Forwarded-Proto` from loopback.

Open tabs hear about changes over a long-lived `GET /api/events` stream (Server-Sent Events) with a keep-alive every 25 seconds. With buffering off, as above, it passes through nginx unchanged; a proxy that buffers responses or closes idle ones sooner than that delays or drops the updates, and the tabs fall back to reloading when they reconnect.

filebox trusts `X-Real-IP`, then the last `X-Forwarded-For` entry, only when the direct peer is loopback, and uses the address for rate limits and the activity log. The proxy must set these headers itself (nginx: `proxy_set_header X-Real-IP $remote_addr;`) and must not pass a client-supplied value through. Caddy and Traefik pass them through by default.

## Passkeys

Passkeys ([WebAuthn](https://www.w3.org/TR/webauthn-3/)) work only on origins passed with `-origin https://files.example.com` (repeatable). The relying party ID is that host, so a reverse proxy must pass the original `Host` header. Browsers require HTTPS, so plain-HTTP LAN access shows no passkey UI; `http://localhost` is allowed for testing.

Add passkeys under Settings. The password always keeps working, and `filebox passkey -data DIR ls | rm ID` removes a lost one.

## Trash and versions

Deletes from the web UI and WebDAV go to a per-volume trash that keeps items for 30 days, or less when the disk falls below 10% free. So does a folder replaced by a WebDAV MOVE or COPY. Junk that macOS and Windows write and delete on their own (`._*`, `.DS_Store`, `.Trashes`, `Thumbs.db`, `desktop.ini`) is deleted for good and never versioned.

When a file is overwritten by an upload, WebDAV or a restore, filebox keeps the previous content in `<volume>/.filebox/versions` and lists it in the file's details. A file that has other hard links shares its bytes with its versions, so a program that edits it in place through another link also changes those versions.
