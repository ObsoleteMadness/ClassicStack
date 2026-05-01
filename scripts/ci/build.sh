#!/usr/bin/env bash
set -euo pipefail

build_version="${BUILD_VERSION:-0.0.0-dev}"
build_commit="${BUILD_COMMIT:-$(git rev-parse --short=12 HEAD)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
build_variant="${BUILD_VARIANT:-all}"

case "$build_variant" in
  all)    tags="all" ;;
  router) tags="" ;;
  *)
    echo "Unsupported BUILD_VARIANT: $build_variant (expected: all|router)" >&2
    exit 1
    ;;
esac

if [[ -n "${OUTPUT:-}" ]]; then
  output="$OUTPUT"
elif [[ "$build_variant" == "all" ]]; then
  output="out/classicstack"
else
  output="out/classicstack-${build_variant}"
fi

mkdir -p "$(dirname "$output")"

ldflags="-s -w -X main.BuildVersion=${build_version} -X main.BuildCommit=${build_commit} -X main.BuildDate=${build_date}"

if [[ -n "$tags" ]]; then
  go build -trimpath -tags "$tags" -ldflags "$ldflags" -o "$output" ./cmd/classicstack
else
  go build -trimpath -ldflags "$ldflags" -o "$output" ./cmd/classicstack
fi
