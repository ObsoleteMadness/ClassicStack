#!/bin/bash
set -e

mkdir -p bin

GIT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date +"%Y-%m-%d" || echo "unknown")

case "$1" in
    "pico")
        echo "Building ClassicStack for Raspberry Pi Pico (RP2040)..."
        tinygo build -target=pico -ldflags="-X main.BuildCommit=${GIT_SHA} -X main.BuildDate=${BUILD_DATE}" -o bin/classicstack-pico.uf2 ./hardware/pico
        ;;
    "picow")
        echo "Building ClassicStack for Raspberry Pi Pico W (RP2040 + CYW43439)..."
        tinygo build -target=pico -tags picow -ldflags="-X main.BuildCommit=${GIT_SHA} -X main.BuildDate=${BUILD_DATE}" -o bin/classicstack-picow.uf2 ./hardware/pico
        ;;
    "pico2")
        echo "Building ClassicStack for Raspberry Pi Pico 2 (RP2350)..."
        tinygo build -target=pico3 -ldflags="-X main.BuildCommit=${GIT_SHA} -X main.BuildDate=${BUILD_DATE}" -o bin/classicstack-pico2.uf2 ./hardware/pico
        ;;
    "pico2w")
        echo "Building ClassicStack for Raspberry Pi Pico 2 W (RP2350 + CYW43439)..."
        tinygo build -target=pico3 -tags picow -ldflags="-X main.BuildCommit=${GIT_SHA} -X main.BuildDate=${BUILD_DATE}" -o bin/classicstack-pico2w.uf2 ./hardware/pico
        ;;
    *)
        echo "Usage: $0 {pico|picow|pico2|pico2w}"
        exit 1
        ;;
esac

echo "Build complete."
