# filebox

A self-hosted file server in one binary. Browse, preview, upload and share files on local disks from a browser, a WebDAV client or the command line.

- Resumable uploads via [tus 1.0](https://tus.io/protocols/resumable-upload), served by [tusd](https://github.com/tus/tusd); downloads support HTTP Range.
- WebDAV at `/dav/` with per-device app passwords, optionally read-only.
- Share links with password, expiry, a download limit, and upload or drop-box modes. A drop box can ask uploaders for their name and keep each one's files in a folder of that name.
- WebP thumbnails via ffmpeg (optional).
- Search by name, path and the text inside documents.
- Deletes from the web UI and WebDAV go to a per-volume trash that keeps items for 30 days, or less when the disk falls below 10% free, and so does a folder replaced by a WebDAV MOVE or COPY. Junk that macOS and Windows write and delete on their own (`._*`, `.DS_Store`, `.Trashes`, `Thumbs.db`, `desktop.ini`) is deleted for good and never versioned.
- Passkey login next to the password.
- Command palette on Ctrl/⌘+K, drag-to-select with the mouse, a storage page by type, folder and file, and a duplicate finder that compares content.
- Webhook notifications to ntfy, Gotify, Slack or your own endpoint.

## Build

Requires Go 1.26+ and [bun](https://bun.sh).

```sh
make build        # bin/filebox
make test e2e
```

## Run

```sh
echo 'your-password' | filebox passwd -data /srv/data/filebox
filebox serve -data /srv/data/filebox -listen :5280 -ffmpeg /usr/bin/ffmpeg -origin https://files.example.com \
  -vol downloads=/mnt/disk/downloads -vol archive=/mnt/disk/archive
filebox token -data /srv/data/filebox new "laptop"         # app password for WebDAV and CLI
```

Behind a reverse proxy, turn off request and response buffering (nginx: `proxy_request_buffering off; proxy_buffering off;`) and forward `X-Real-IP` and `X-Forwarded-Proto` from loopback. filebox trusts `X-Real-IP`, then the last `X-Forwarded-For` entry, only when the direct peer is loopback, and uses the address for rate limits and the activity log. The proxy must set these headers itself (nginx: `proxy_set_header X-Real-IP $remote_addr;`) and must not pass a client-supplied value through. Caddy and Traefik pass them through by default.

## Content search

filebox extracts the text inside documents in the background so search also finds words in them. It reads up to 1 MiB of text from each plain-text, Markdown, code, HTML, SVG, XML and EPUB file, and from PDFs when poppler's [`pdftotext`](https://poppler.freedesktop.org/) is on the `PATH`. Content matches need at least 3 characters, except Chinese, Japanese and Korean text, which a separate bigram index lets match from 1 character. On Linux the extraction threads and `pdftotext` run at nice 19; elsewhere only `pdftotext` does. A file that fails to extract is retried once after 24 hours.

The index lives in `content.db` in the data directory and takes 2.5 to 4 times the extracted text, so a library of 500 books needs 1 to 2 GB. content.db can be deleted at any time and is rebuilt in the background. Pass `-content=false` to stop indexing and ignore the file.

## Team

Several people can share one server. An account is an admin, who reaches every volume and manages the others, or a member, who reaches only the volumes granted to them: read, or read and write. A volume nobody granted does not exist for a member: it is missing from lists, search, recent files, photos, storage, duplicates and WebDAV, and its files answer 404. Share links made by a member stop working the moment they lose access to what the link shows. Favorites belong to each person. Tags, the trash and file versions belong to the volume.

The first account is an admin, and so is every account that existed before roles did. Accounts made later start with no access:

```sh
echo 'her-password' | filebox passwd -data /srv/data/filebox -user alice
filebox user -data /srv/data/filebox access alice downloads rw
filebox user -data /srv/data/filebox access alice archive r
filebox user -data /srv/data/filebox ls
filebox user -data /srv/data/filebox role alice admin     # or member
```

A running server notices a change made on the command line within 30 seconds.

Admins manage the team from the Team page instead: invite someone with a one-time link that sets their role and per-volume access (the link works once and expires after 1, 7 or 30 days), change anyone's role or access, make a one-time link for a member who forgot their password (it signs them out everywhere once used), and remove members. There is always at least one admin.

## Duplicates

The duplicate finder (`/duplicates`) hashes only files that share a size with another file, remembers each hash in the database until the file changes, and works through a large library over several rounds of 8 seconds. Selected copies go to the trash, and at least one copy in each group always stays.

## Notifications

Under Settings, add a webhook URL and choose what to hear about: uploads, shared-link downloads, sign-ins, failed sign-ins, new share links, new app passwords and low disk space (under 10% free on a volume). Send either JSON or plain text, which ntfy and Gotify take as the message. A signing secret adds `X-Filebox-Signature: sha256=<hmac of the body>` to JSON messages. Failed deliveries retry twice, a webhook gets at most 30 messages a minute, and repeated downloads of one file by one visitor within an hour count once. The page shows only the host of each URL, since the path often holds a secret.

## Versions

When a file is overwritten by an upload, WebDAV or a restore, filebox keeps the previous content in `<volume>/.filebox/versions` and lists it in the file's details. A file that has other hard links shares its bytes with its versions, so a program that edits it in place through another link also changes those versions.

## Passkeys

Passkeys ([WebAuthn](https://www.w3.org/TR/webauthn-3/)) work only on origins passed with `-origin https://files.example.com` (repeatable). The relying party ID is that host, so a reverse proxy must pass the original `Host` header. Browsers require HTTPS, so plain-HTTP LAN access shows no passkey UI; `http://localhost` is allowed for testing. Add passkeys under Settings. The password always keeps working, and `filebox passkey -data DIR ls | rm ID` removes a lost one.

## Command line

```sh
export FILEBOX_URL=https://file.example.com FILEBOX_TOKEN=fb_...
curl -T big.iso -u ":$FILEBOX_TOKEN" "$FILEBOX_URL/dav/downloads/"                      # upload
curl -C - -o big.iso -H "Authorization: Bearer $FILEBOX_TOKEN" "$FILEBOX_URL/raw/downloads/big.iso"   # resumable download
```

Resumable upload — rerun the same command after an interruption. `fbput` speaks plain [tus](https://tus.io/protocols/resumable-upload) to `/upload/` with a Bearer app token, which [tusd](https://github.com/tus/tusd) accepts:

```sh
fbput() { # fbput FILE VOLUME [DIR]
  local f=$1 vol=$2 dir=${3:-/} size state loc off meta
  size=$(wc -c < "$f" | tr -d ' ')
  state="$HOME/.cache/fbput/$(printf %s "$FILEBOX_URL|$vol|$dir|$(realpath "$f")|$size" | shasum | cut -c1-40)"
  mkdir -p "${state%/*}"
  set -- -H "Authorization: Bearer $FILEBOX_TOKEN" -H 'Tus-Resumable: 1.0.0'
  loc=$(cat "$state" 2>/dev/null)
  off=$([ -n "$loc" ] && curl -sfI "$@" "$FILEBOX_URL$loc" | tr -d '\r' | awk -F': ' 'tolower($1)=="upload-offset"{print $2}')
  if [ -z "$off" ]; then
    meta="vol $(printf %s "$vol" | base64 | tr -d '\n'),dir $(printf %s "$dir" | base64 | tr -d '\n'),filename $(printf %s "${f##*/}" | base64 | tr -d '\n')"
    loc=$(curl -sf -D - -o /dev/null -X POST "$@" -H "Upload-Length: $size" -H "Upload-Metadata: $meta" "$FILEBOX_URL/upload/" \
      | tr -d '\r' | awk -F': ' 'tolower($1)=="location"{print $2}') || return 1
    [ "$size" = 0 ] && return 0
    echo "$loc" > "$state"; off=0
  fi
  tail -c +$((off + 1)) "$f" | curl -f --progress-bar -X PATCH "$@" -H "Upload-Offset: $off" \
    -H 'Content-Type: application/offset+octet-stream' -T - -o /dev/null "$FILEBOX_URL$loc" && rm -f "$state"
}
```

`curl -T -` streams the PATCH body chunked (no `Content-Length`); tusd reads the request body until EOF regardless, so this needs no special handling.

## License

[AGPL-3.0-or-later](LICENSE). The vendored [foliate-js](web/src/vendor/foliate/LICENSE) is MIT.
