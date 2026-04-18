#!/usr/bin/env bash
set -euo pipefail

mapfile -t packages < <(go list ./... | grep -Ev '(^|/)(dist|icon|icons)($|/)' || true)

if [[ ${#packages[@]} -eq 0 ]]; then
  echo "No packages found to test" >&2
  exit 1
fi

go test "${packages[@]}"
