#!/usr/bin/env bash
set -euo pipefail

build_version="${BUILD_VERSION:-0.0.0-dev}"
build_commit="${BUILD_COMMIT:-$(git rev-parse --short=12 HEAD)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
output="${OUTPUT:-out/omnitalk}"

mkdir -p "$(dirname "$output")"

# Keep version metadata consistent across all non-Windows builds.
go build -trimpath \
  -ldflags "-s -w -X main.BuildVersion=${build_version} -X main.BuildCommit=${build_commit} -X main.BuildDate=${build_date}" \
  -o "$output" ./cmd/omnitalk
