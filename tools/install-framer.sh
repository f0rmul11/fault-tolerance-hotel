#!/usr/bin/env bash
set -euo pipefail
# Ozon Tech Framer (Apache-2.0), pinned for reproducible request formats.
revision=ae45a6eb6a20f744f5fcc7d606e7a8c66c591d2c
destination="$1"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git clone --quiet https://github.com/ozontech/framer.git "$work/source"
git -C "$work/source" checkout --quiet "$revision"
# Upstream unconditionally opens a pprof listener on :8081. The course adapter
# removes only that listener; converter, scheduling and load engine are unchanged.
sed '/"net\/http"/d; /_ "net\/http\/pprof"/d; /go func() {/,/}()/d' "$work/source/cmd/framer/main.go" > "$work/main.go"
mv "$work/main.go" "$work/source/cmd/framer/main.go"
(cd "$work/source" && GOWORK=off go build -trimpath -o "$work/framer" ./cmd/framer)
mkdir -p "$(dirname "$destination")"
cp "$work/framer" "$destination.tmp"
chmod 755 "$destination.tmp"
mv "$destination.tmp" "$destination"
