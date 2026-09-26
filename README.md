# filebox

A self-hosted file server in one binary. Browse, preview, upload and share files on local disks from a browser, a WebDAV client or the command line.

- Resumable uploads via [tus 1.0](https://tus.io/protocols/resumable-upload), served by [tusd](https://github.com/tus/tusd); downloads support HTTP Range.
- WebDAV at `/dav/` with per-device app passwords, optionally read-only.
- Share links with password, expiry, and upload or drop-box modes.
- WebP thumbnails via ffmpeg (optional).
- Deletes from the web UI go to a per-volume trash.

## Build

Requires Go 1.26+ and [bun](https://bun.sh).

```sh
make build        # bin/filebox
make test e2e
```

## Run

```sh
echo 'your-password' | filebox passwd -data /srv/data/filebox
filebox serve -data /srv/data/filebox -listen :5280 -ffmpeg /usr/bin/ffmpeg \
  -vol downloads=/mnt/disk/downloads -vol archive=/mnt/disk/archive
filebox token -data /srv/data/filebox new "laptop"         # app password for WebDAV and CLI
```

Behind a reverse proxy, turn off request and response buffering (nginx: `proxy_request_buffering off; proxy_buffering off;`) and forward `X-Real-IP` and `X-Forwarded-Proto` from loopback.

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
