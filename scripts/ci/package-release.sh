#!/usr/bin/env bash
set -euo pipefail

target_os="${TARGET_OS:-}"
release_tag="${RELEASE_TAG:-dev-local}"
build_version="${BUILD_VERSION:-0.0.0-dev}"

if [[ -z "$target_os" ]]; then
  echo "TARGET_OS is required (linux|macos)" >&2
  exit 1
fi

if [[ "$target_os" == "linux" ]]; then
  stage="release/omnitalk-${release_tag}-linux-amd64"
  archive_name="omnitalk-${release_tag}-linux-amd64.tar.gz"

  mkdir -p "$stage"
  cp out/omnitalk "$stage/"
  cp README.md server.toml.example extmap.conf "$stage/"
  cp -a dist/. "$stage/"
  tar -C release -czf "$archive_name" "$(basename "$stage")"
  echo "$archive_name"
  exit 0
fi

if [[ "$target_os" == "macos" ]]; then
  stage="release/omnitalk-${release_tag}-macos-amd64"
  archive_name="omnitalk-${release_tag}-macos-amd64.zip"
  app_root="$stage/OmniTalk.app/Contents"

  mkdir -p "$app_root/MacOS" "$app_root/Resources"
  cp out/omnitalk "$app_root/MacOS/omnitalk"
  chmod +x "$app_root/MacOS/omnitalk"
  cp icons/omnitalk.icns "$app_root/Resources/omnitalk.icns"

  cat > "$app_root/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDisplayName</key><string>OmniTalk</string>
  <key>CFBundleExecutable</key><string>omnitalk</string>
  <key>CFBundleIconFile</key><string>omnitalk.icns</string>
  <key>CFBundleIdentifier</key><string>com.obsoletemadness.omnitalk</string>
  <key>CFBundleName</key><string>OmniTalk</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${build_version}</string>
  <key>CFBundleVersion</key><string>${build_version}</string>
  <key>LSMinimumSystemVersion</key><string>10.13</string>
</dict>
</plist>
EOF

  cp README.md server.toml.example extmap.conf "$stage/"
  cp -a dist/. "$stage/"
  (cd release && zip -r "../$archive_name" "$(basename "$stage")")
  echo "$archive_name"
  exit 0
fi

echo "Unsupported TARGET_OS: $target_os" >&2
exit 1
