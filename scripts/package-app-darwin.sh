#!/usr/bin/env bash
# Assembles dist/ClassicStack.app: a menu-bar-only macOS app bundle wrapping
# classicstackd (the background daemon) and classicstack-tray (the status
# item that starts/monitors/controls it). Unsigned, local/manual build — see
# `make app-darwin`. Not part of CI packaging.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

if [[ "$(go env GOOS)" != "darwin" ]]; then
  echo "package-app-darwin.sh: must be built on darwin (systray needs Cocoa)" >&2
  exit 1
fi

tags="${TAGS:-all}"
dist_dir="${DIST_DIR:-$root/dist}"
app_dir="$dist_dir/ClassicStack.app"
contents_dir="$app_dir/Contents"
macos_dir="$contents_dir/MacOS"
resources_dir="$contents_dir/Resources"

build_version="${BUILD_VERSION:-0.0.0-dev}"
build_commit="${BUILD_COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
ldflags="-X main.BuildVersion=${build_version} -X main.BuildCommit=${build_commit} -X main.BuildDate=${build_date}"

# Embed the TN3179 local-network usage string into the Mach-O, same as the
# Makefile's `build`/`build-svc` targets — classicstackd does its own raw
# networking, so it carries the same section classicstack does.
plist="$root/packaging/darwin/Info.plist"
darwin_ldflags="$ldflags -linkmode=external -extldflags=-Wl,-sectcreate,__TEXT,__info_plist,$plist"

echo "package-app-darwin: building classicstackd and classicstack-tray (tags: $tags)"
go build -tags "$tags" -ldflags "$darwin_ldflags" -o "$dist_dir/classicstackd" ./cmd/classicstackd
go build -tags "$tags" -ldflags "$ldflags" -o "$dist_dir/classicstack-tray" ./cmd/classicstack-tray

echo "package-app-darwin: assembling $app_dir"
rm -rf "$app_dir"
mkdir -p "$macos_dir" "$resources_dir"

cp "$dist_dir/classicstackd" "$macos_dir/classicstackd"
cp "$dist_dir/classicstack-tray" "$macos_dir/classicstack-tray"
chmod +x "$macos_dir/classicstackd" "$macos_dir/classicstack-tray"

cp "$root/packaging/darwin/app/Info.plist" "$contents_dir/Info.plist"
cp "$root/icons/classicstack.icns" "$resources_dir/classicstack.icns"
cp "$root/server.toml.example" "$resources_dir/server.toml.example"

# The starter config (with example AFP/SMB/NCP/EtherDFS shares) and the
# sample folders it points at — classicstack-tray provisions both into
# ~/Library/Application Support/ClassicStack on first run. See launcher.go.
cp "$root/packaging/darwin/app/server.toml" "$resources_dir/server.toml"
cp -R "$root/packaging/darwin/app/Volumes" "$resources_dir/Volumes"

echo "package-app-darwin: built $app_dir"
