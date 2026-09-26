#!/bin/sh
set -e
D=${E2E_DIR:?}
rm -rf "$D"
mkdir -p "$D/vol/docs" "$D/data"
echo hello > "$D/vol/docs/readme.txt"
echo 'pw-pw-pw-pw' | ../bin/filebox passwd -data "$D/data"
exec ../bin/filebox serve -data "$D/data" -listen 127.0.0.1:5298 -vol v="$D/vol"
