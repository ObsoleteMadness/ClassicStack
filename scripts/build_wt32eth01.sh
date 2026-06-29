#!/bin/bash
set -e

echo "Building ClassicStack for WT32-ETH01 (ESP32)..."
mkdir -p bin
tinygo build -target=esp32 -o bin/classicstack-wt32eth01.bin ./hardware/esp32/wt32eth01
echo "Build complete: bin/classicstack-wt32eth01.bin"
