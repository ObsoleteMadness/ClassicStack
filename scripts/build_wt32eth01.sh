#!/bin/bash
set -e

echo "Building ClassicStack for WT32-ETH01 (ESP32)..."
mkdir -p bin
GIT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date +"%Y-%m-%d" || echo "unknown")
tinygo build -target=esp32 -ldflags="-X main.BuildCommit=${GIT_SHA} -X main.BuildDate=${BUILD_DATE}" -o bin/classicstack-wt32eth01.bin ./hardware/esp32/wt32eth01
echo "Build complete: bin/classicstack-wt32eth01.bin"
