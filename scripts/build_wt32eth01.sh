#!/bin/bash
set -e

# hardware/esp32/wt32eth01/{emac,wifi}.go cgo directly against ESP-IDF's C
# headers (esp_eth.h, esp_wifi.h, esp_netif.h, driver/gpio.h, ...) and link
# against its component static libraries (-lesp_eth -lesp_wifi -lesp_netif
# -lesp_event). CI installs the SDK via espressif/install-esp-idf-action,
# which exports IDF_PATH; when it's set, point cgo at the component include
# directories those files touch.
#
# KNOWN GAP (not fixed by this script): this still is not expected to fully
# build. ESP-IDF's headers #include a project-generated sdkconfig.h (the
# CONFIG_* macros idf.py derives from a project's sdkconfig), and the
# -lesp_eth/-lesp_wifi/... libraries only exist after a real `idf.py build`
# of a matching component project -- ESP-IDF does not ship them as generic
# prebuilt/linkable artifacts. Neither exists here, so the build is expected
# to fail past the header stage (or at link time) until that's addressed,
# most likely by generating both from a companion ESP-IDF component project
# (or by moving this driver onto TinyGo's own supported ESP32 networking
# path -- the "espradio" package -- instead of raw cgo against ESP-IDF).
# CI treats this build as continue-on-error for exactly this reason.
if [[ -n "${IDF_PATH:-}" ]]; then
  idf_includes=(
    "$IDF_PATH/components/esp_common/include"
    "$IDF_PATH/components/esp_eth/include"
    "$IDF_PATH/components/esp_wifi/include"
    "$IDF_PATH/components/esp_netif/include"
    "$IDF_PATH/components/esp_event/include"
    "$IDF_PATH/components/esp_hw_support/include"
    "$IDF_PATH/components/esp_system/include"
    "$IDF_PATH/components/esp_timer/include"
    "$IDF_PATH/components/driver/include"
    "$IDF_PATH/components/hal/include"
    "$IDF_PATH/components/hal/esp32/include"
    "$IDF_PATH/components/soc/include"
    "$IDF_PATH/components/soc/esp32/include"
    "$IDF_PATH/components/esp_rom/include"
    "$IDF_PATH/components/esp_rom/esp32/include"
    "$IDF_PATH/components/freertos/FreeRTOS-Kernel/include"
    "$IDF_PATH/components/freertos/esp_additions/include"
    "$IDF_PATH/components/newlib/platform_include"
    "$IDF_PATH/components/lwip/include"
    "$IDF_PATH/components/lwip/lwip/src/include"
    "$IDF_PATH/components/xtensa/include"
    "$IDF_PATH/components/xtensa/esp32/include"
  )
  cgo_cflags=""
  for dir in "${idf_includes[@]}"; do
    [[ -d "$dir" ]] && cgo_cflags="$cgo_cflags -I$dir"
  done
  export CGO_CFLAGS="$cgo_cflags"
fi

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
