#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
service="${1:?usage: generate-xo.sh booking|hotels|review|check-in}"
case "$service" in booking|hotels|review|check-in) ;; *) echo 'Unknown service' >&2; exit 1;; esac
: "${POSTGRES_DSN:?Point POSTGRES_DSN at the migrated codegen database of this service}"
# Read-only introspection: migrations are a separate, explicit command.
xo "$POSTGRES_DSN" --schema public \
  --methods-config-file "services/$service/tools/xo.yaml" --only-config-table \
  --out "services/$service/internal/pkg/xo" --template-path "services/$service/tools/xo_templates"
