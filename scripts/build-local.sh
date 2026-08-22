#!/usr/bin/env bash
# Local desktop build: every host command into ./bin, with the full desktop tag
# set (all,pcap,netboot,fuse). This is the dev-checkout counterpart to
# scripts/ci/build.sh — same binaries plus the diagnostic tools, collected in
# one directory instead of scattered across the repo root, and unstripped so
# stack traces and delve stay useful.
#
#   ./scripts/build-local.sh                     # everything into ./bin
#   ./scripts/build-local.sh classicstack csmount  # just these
#   BIN_DIR=/tmp/cs ./scripts/build-local.sh     # elsewhere
#   TAGS="all" ./scripts/build-local.sh          # override the tag set
#   SPA=1 ./scripts/build-local.sh               # force a Vite SPA rebuild
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

bin_dir="${BIN_DIR:-$root/bin}"
goos="$(go env GOOS)"

# fuse pulls in cgofuse (macFUSE / libfuse) and exists only on darwin/linux;
# Windows mounts through WinFsp and must not get the tag.
case "$goos" in
  darwin|linux) default_tags="all pcap netboot fuse" ;;
  *)            default_tags="all pcap netboot" ;;
esac
tags="${TAGS:-$default_tags}"
tags="${tags//,/ }"  # `-tags` takes either separator; normalise for matching below

# The service/daemon wrapper is a different command per OS, and csmount only
# exists where there is a filesystem driver to talk to.
case "$goos" in
  windows) svc_pkg="classicstack-svc" ;;
  *)       svc_pkg="classicstackd" ;;
esac

tools=(csclient csecho csgetzones csipxping csnbp csncpinfo csnetsend csnetview)
all_targets=(classicstack "$svc_pkg" csmount "${tools[@]}")

if [[ $# -gt 0 ]]; then
  targets=("$@")
else
  targets=("${all_targets[@]}")
fi

build_version="${BUILD_VERSION:-0.0.0-dev}"
build_commit="${BUILD_COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
ldflags="-X main.BuildVersion=${build_version} -X main.BuildCommit=${build_commit} -X main.BuildDate=${build_date}"

# On macOS, embed Info.plist into the Mach-O so Local Network privacy (TN3179)
# can show a usage string — see the same block in the Makefile.
if [[ "$goos" == "darwin" ]]; then
  plist="$root/packaging/darwin/Info.plist"
  ldflags="$ldflags -linkmode=external -extldflags=-Wl,-sectcreate,__TEXT,__info_plist,$plist"
fi

# The `all`/`webui` tags embed the Vite SPA, which must exist on disk before
# go:embed runs. Build it when it is missing; SPA=1 forces a refresh, SPA=0
# skips even when absent (the embed then serves an empty UI).
embeds_spa=0
case " $tags " in
  *" all "*|*" webui "*) embeds_spa=1 ;;
esac
spa_built=0
[[ -n "$(ls -A adapter/control/http/spa/assets 2>/dev/null || true)" ]] && spa_built=1
if [[ "${SPA:-}" == "1" || ( "${SPA:-}" != "0" && "$embeds_spa" == 1 && "$spa_built" == 0 ) ]]; then
  make spa
fi

mkdir -p "$bin_dir"

ext=""
[[ "$goos" == "windows" ]] && ext=".exe"

echo "building for $goos/$(go env GOARCH) with tags: $tags"
for target in "${targets[@]}"; do
  if [[ ! -d "cmd/$target" ]]; then
    echo "build-local: no such command: cmd/$target" >&2
    exit 1
  fi
  out="$bin_dir/${target}${ext}"
  echo "  -> ${out#$root/}"
  go build -tags "$tags" -ldflags "$ldflags" -o "$out" "./cmd/$target"
done
