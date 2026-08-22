#!/bin/bash
set -e

echo "Building ClassicStack for WT32-ETH01 (ESP32)..."
mkdir -p bin
GIT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date +"%Y-%m-%d" || echo "unknown")
# -target=esp32 is inheritable-only (a base target board-specific targets extend, not
# buildable directly) — esp32-generic is the concrete "plain ESP32 module" target, the
# right fit for a DIY/breakout board like WT32-ETH01 with no dedicated TinyGo target.
# -tags wt32eth01 is required: every file in ./hardware/esp32/wt32eth01 is gated
# `//go:build esp32 && wt32eth01`, so without it the package has no Go files at all.
tinygo build -target=esp32-generic -tags wt32eth01 -ldflags="-X main.BuildCommit=${GIT_SHA} -X main.BuildDate=${BUILD_DATE}" -o bin/classicstack-wt32eth01.bin ./hardware/esp32/wt32eth01
echo "Build complete: bin/classicstack-wt32eth01.bin"
