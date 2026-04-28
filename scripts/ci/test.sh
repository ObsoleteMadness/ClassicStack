#!/usr/bin/env bash
set -euo pipefail

# Run unit tests across the relevant build-tag combinations so optional
# subsystems (afp, macgarden, macip) actually get exercised — `go test
# ./...` without tags would skip the bulk of the codebase.
tag_sets=(
  ""
  "afp"
  "afp macgarden"
  "afp macip"
  "afp macgarden macip"
)

for tags in "${tag_sets[@]}"; do
  echo "=== go test -tags \"${tags}\" ==="
  mapfile -t packages < <(go list -tags "${tags}" ./... | grep -Ev '(^|/)(dist|icon|icons)($|/)' || true)
  if [[ ${#packages[@]} -eq 0 ]]; then
    echo "No packages found to test for tags=\"${tags}\"" >&2
    exit 1
  fi
  go test -tags "${tags}" "${packages[@]}"
done
