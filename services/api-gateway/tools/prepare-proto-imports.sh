#!/bin/sh
set -eu

# Download only the four imported schemas into an explicitly supplied external cache.
imports_dir=${1:?Usage: prepare-proto-imports.sh /absolute/path/outside/repository}
googleapis_revision=208993fa5be54732413481843b8154f15eefba52
gateway_revision=v2.27.3
mkdir -p "$imports_dir/google/api" "$imports_dir/protoc-gen-openapiv2/options"
for name in annotations http; do
 curl --fail --silent --show-error --location \
  "https://raw.githubusercontent.com/googleapis/googleapis/$googleapis_revision/google/api/$name.proto" \
  --output "$imports_dir/google/api/$name.proto"
done
for name in annotations openapiv2; do
 curl --fail --silent --show-error --location \
  "https://raw.githubusercontent.com/grpc-ecosystem/grpc-gateway/$gateway_revision/protoc-gen-openapiv2/options/$name.proto" \
  --output "$imports_dir/protoc-gen-openapiv2/options/$name.proto"
done
curl --fail --silent --show-error --location \
 "https://raw.githubusercontent.com/googleapis/googleapis/$googleapis_revision/LICENSE" \
 --output "$imports_dir/LICENSE.googleapis"
curl --fail --silent --show-error --location \
 "https://raw.githubusercontent.com/grpc-ecosystem/grpc-gateway/$gateway_revision/LICENSE" \
 --output "$imports_dir/LICENSE.grpc-gateway"
