#!/usr/bin/env bash
# Build the Vite SPA into adapter/control/http/spa for go:embed.
# ClassicStack-web is the Finder UI source. Resolution order: WEB_DIR, the
# third_party/classicstack-web submodule (initialising it if the clone skipped
# submodules), a sibling ../ClassicStack-web checkout, then a WEB_REF clone.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
ui="$root/adapter/control/http/ui"
sub="$root/third_party/classicstack-web"
sib="$(cd "$root/.." && pwd)/ClassicStack-web"
repo="${WEB_REPO:-https://github.com/ObsoleteMadness/ClassicStack-web.git}"
ref="${WEB_REF:-main}"

# WEB_DIR pins a checkout explicitly — the escape hatch for working against a
# local ClassicStack-web tree without disturbing the submodule.
if [[ -n "${WEB_DIR:-}" ]]; then
  if [[ ! -f "$WEB_DIR/src/ui/finder-window.ts" ]]; then
    echo "spa: WEB_DIR=$WEB_DIR is not a ClassicStack-web checkout" >&2
    exit 1
  fi
  echo "spa: using WEB_DIR $WEB_DIR"
  web="$WEB_DIR"
elif [[ -f "$sub/src/ui/finder-window.ts" ]]; then
  echo "spa: using submodule $sub"
  web="$sub"
elif [[ -d "$root/.git" || -f "$root/.git" ]] && git -C "$root" config --file .gitmodules --get submodule."third_party/classicstack-web".url >/dev/null 2>&1; then
  # Cloned without --recurse-submodules; populate the pin rather than
  # silently building against whatever the fallbacks happen to find.
  echo "spa: initialising submodule $sub"
  git -C "$root" submodule update --init --depth 1 third_party/classicstack-web
  web="$sub"
elif [[ -f "$sib/src/ui/finder-window.ts" ]]; then
  echo "spa: using sibling $sib"
  web="$sib"
else
  echo "spa: cloning $repo ($ref) into $sub"
  mkdir -p "$(dirname "$sub")"
  if ! git clone --depth 1 --branch "$ref" "$repo" "$sub"; then
    echo "spa: branch $ref missing; cloning default branch" >&2
    git clone --depth 1 "$repo" "$sub"
  fi
  web="$sub"
fi

# tsc resolves bare imports inside the web tree (fflate, lucide) from that tree's
# own node_modules, so a fresh CI checkout needs them installed. Runtime deps are
# enough; skip an existing node_modules so a developer's sibling checkout keeps
# its devDependencies.
if [[ ! -d "$web/node_modules" && -f "$web/package.json" ]]; then
  echo "spa: installing web runtime deps in $web"
  if [[ -f "$web/package-lock.json" ]]; then
    (cd "$web" && npm ci --omit=dev --ignore-scripts)
  else
    (cd "$web" && npm install --omit=dev --ignore-scripts)
  fi
fi

cd "$ui"
if [[ -f package-lock.json ]]; then
  npm ci
else
  npm install
fi
npm run build
