#!/bin/sh
set -eu

# Imports are mounted read-only by make; no sources are stored in the repository.
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT
mkdir -p "$workdir/api-gateway" "$workdir/booking" "$workdir/check-in" "$workdir/hotels"
cp -R api "$workdir/api-gateway/api"
cp -R /proto-imports "$workdir/api-gateway/imports"
cp -R ../booking/api "$workdir/booking/api"
cp -R ../check-in/api "$workdir/check-in/api"
cp -R ../hotels/api "$workdir/hotels/api"
cp buf.gen.local.yaml buf.gen.external.yaml "$workdir/api-gateway/"
cat > "$workdir/api-gateway/buf.yaml" <<'YAML'
version: v2
modules:
  - path: api
  - path: imports
lint:
  use:
    - STANDARD
  except:
    - PACKAGE_DIRECTORY_MATCH
  ignore:
    - imports
YAML
cat > "$workdir/booking/buf.yaml" <<'YAML'
version: v2
modules:
  - path: api
YAML
cp "$workdir/booking/buf.yaml" "$workdir/check-in/buf.yaml"
cp "$workdir/booking/buf.yaml" "$workdir/hotels/buf.yaml"
service_dir=$PWD
cd "$workdir/api-gateway"
buf lint api
buf build
buf generate --template buf.gen.local.yaml
buf generate --template buf.gen.external.yaml
rm -rf "$service_dir/internal/pkg/pb"
cp -R internal/pkg/pb "$service_dir/internal/pkg/"
