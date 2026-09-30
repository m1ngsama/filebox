# filebox

A self-hosted file server in one binary. Browse, preview, upload and share files on local disks from a browser, a WebDAV client or the command line.

- Resumable uploads via [tus 1.0](https://tus.io/protocols/resumable-upload), served by [tusd](https://github.com/tus/tusd); downloads support HTTP Range.
- WebDAV at `/dav/` with per-device app passwords, optionally read-only.
- Share links with password, expiry, and upload or drop-box modes.
- WebP thumbnails via ffmpeg (optional).
- Search by name, path and the text inside documents.
- Deletes from the web UI and WebDAV go to a per-volume trash that keeps items for 30 days, or less when the disk falls below 10% free, and so does a folder replaced by a WebDAV MOVE or COPY. Junk that macOS and Windows write and delete on their own (`._*`, `.DS_Store`, `.Trashes`, `Thumbs.db`, `desktop.ini`) is deleted for good and never versioned.
- Passkey login next to the password.

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
