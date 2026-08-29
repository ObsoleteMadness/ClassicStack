#!/usr/bin/env bash
set -euo pipefail

# tinygo-gate.sh — the TinyGo amd64 build GATES (not informational). These
# verify the TinyGo-safe core subset actually COMPILES + LINKS for an embedded
# toolchain, WITHOUT ESP32 hardware. A package that pulls in cgo or a runtime
# feature TinyGo doesn't support fails here.
#
# NOTE (errata): on modern TinyGo (0.34+), the stdlib coverage is broad enough
# that importing net/http or reflect alone does NOT fail the build. So the
# forbidden-import / no-reflection allowlist is enforced by the archtest gate
# (core/internal/archtest, step A2), NOT by this build. The two gates are
# COMPLEMENTARY: archtest enforces the import allowlist; this gate enforces real
# embedded-compilability. Do not assume one substitutes for the other. See
# .refactor/00-DESIGN.md errata note for A4.
#
# The amd64 build is cmd/cs-tinygo, a minimal main that imports only the
# TinyGo-safe core subset. Its import surface grows as more of core becomes
# TinyGo-clean. See .refactor/01-PHASE-harness.md step A4.
#
# cmd/cs-tinygo does NOT import client/link (LToUDP/TashTalk openers), so a
# signature drift between localtalk.go and its localtalk_tinygo.go stub can
# pass this gate and only fail the separate "Build Embedded (TinyGo)" CI job
# (hardware/pico, which does import client/link). To catch that class of break
# locally, this gate also does the real -target=pico firmware build below —
# the same one PR CI runs — rather than relying on the amd64 shim alone.

TARGET_PKG="./cmd/cs-tinygo"

if ! command -v tinygo >/dev/null 2>&1; then
  echo "tinygo-gate.sh: tinygo not found on PATH." >&2
  echo "  Install: https://tinygo.org/getting-started/install/" >&2
  echo "  CI installs it via the acifprima/setup-tinygo (or equivalent) action." >&2
  exit 127
fi

echo "tinygo version: $(tinygo version)"

echo "=== build-tinygo-linux-amd64 ==="
GOOS=linux GOARCH=amd64 tinygo build -o /dev/null "${TARGET_PKG}"

echo "=== build-tinygo-windows-amd64 ==="
GOOS=windows GOARCH=amd64 tinygo build -o cs-tinygo.exe "${TARGET_PKG}"
rm -f cs-tinygo.exe

echo "=== build-pico: hardware/pico full firmware (RP2040) ==="
bash scripts/build_pico.sh pico
rm -f bin/classicstack-pico.uf2

echo "tinygo-gate.sh: OK (amd64 gates + Pico firmware build green)"
