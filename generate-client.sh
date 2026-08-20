#!/usr/bin/env bash
set -euo pipefail

SCHEMA="${1:-schema.yaml}"
OUTPUT="fk-client/generated"
CODEGEN_VERSION="v2.8.0"

# Wipe the tree first: generation overwrites files it emits, but leaves
# behind anything from an earlier run that the current schema no longer
# produces. A stale type that only exists locally hides breakage until it
# reaches CI.
rm -rf "$OUTPUT"
mkdir -p "$OUTPUT"

# go run pulls its own build of the codegen tool without adding it (or its
# dependencies) to our go.mod -- only the small runtime package the
# generated code itself imports ends up as a real dependency.
go run "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@${CODEGEN_VERSION}" \
  -config oapi-codegen-config.yaml \
  -o "$OUTPUT/client.gen.go" \
  "$SCHEMA"
