#!/bin/bash
set -e

mkdir -p bin

case "$1" in
    "pico")
        echo "Building ClassicStack for Raspberry Pi Pico (RP2040)..."
        tinygo build -target=pico -o bin/classicstack-pico.uf2 ./hardware/pico
        ;;
    "picow")
        echo "Building ClassicStack for Raspberry Pi Pico W (RP2040 + CYW43439)..."
        tinygo build -target=pico -tags picow -o bin/classicstack-picow.uf2 ./hardware/pico
        ;;
    "pico2")
        echo "Building ClassicStack for Raspberry Pi Pico 2 (RP2350)..."
        tinygo build -target=pico3 -o bin/classicstack-pico2.uf2 ./hardware/pico
        ;;
    "pico2w")
        echo "Building ClassicStack for Raspberry Pi Pico 2 W (RP2350 + CYW43439)..."
        tinygo build -target=pico3 -tags picow -o bin/classicstack-pico2w.uf2 ./hardware/pico
        ;;
    *)
        echo "Usage: $0 {pico|picow|pico2|pico2w}"
        exit 1
        ;;
esac

echo "Build complete."
