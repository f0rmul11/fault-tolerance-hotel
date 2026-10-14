#!/usr/bin/env bash
set -euo pipefail
# xo v0.0.1 treats expression-index attribute 0 as a table column.
# HotelFlow generates row models only: skip such indexes during introspection.
cd "$(dirname "$0")/.."
export GOWORK=off
module='github.com/not-for-prod/xo@v0.0.1'
go mod download "$module"
source_dir="$(go env GOMODCACHE)/$module"
build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT
cp -R "$source_dir/." "$build_dir/"
chmod -R u+w "$build_dir"
patch -d "$build_dir" -p1 < tools/xo-expression-index.patch
(cd "$build_dir" && go install .)
