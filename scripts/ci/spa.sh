#!/usr/bin/env bash
# Build the Vite SPA into adapter/control/http/spa for go:embed.
# ClassicStack-web is the Finder UI source: git submodule, sibling checkout, or clone.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
ui="$root/adapter/control/http/ui"
sub="$root/third_party/classicstack-web"
sib="$(cd "$root/.." && pwd)/ClassicStack-web"
repo="${WEB_REPO:-https://github.com/ObsoleteMadness/ClassicStack-web.git}"
ref="${WEB_REF:-feature/shared-finder-host}"

if [[ -f "$sub/src/ui/finder-window.ts" ]]; then
  echo "spa: using submodule $sub"
elif [[ -f "$sib/src/ui/finder-window.ts" ]]; then
  echo "spa: using sibling $sib"
else
  echo "spa: cloning $repo ($ref) into $sub"
  mkdir -p "$(dirname "$sub")"
  if ! git clone --depth 1 --branch "$ref" "$repo" "$sub"; then
    echo "spa: branch $ref missing; cloning default branch" >&2
    git clone --depth 1 "$repo" "$sub"
  fi
fi

cd "$ui"
if [[ -f package-lock.json ]]; then
  npm ci
else
  npm install
fi
npm run build
