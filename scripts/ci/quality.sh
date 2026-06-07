#!/usr/bin/env bash
set -euo pipefail

# quality.sh runs the static-analysis gates of the CI "Quality" job — vet,
# golangci-lint, govulncheck, and gosec — so the exact same checks (including
# the vulnerability scan) can be run locally before pushing. The race-enabled
# test pass is run separately (scripts/ci/test.sh / `make test-race`) because
# it is slow; CI keeps it as its own step too.
#
# govulncheck and gosec are installed on demand, matching how the CI job
# bootstraps them, so this works on a fresh checkout.

# gosec scans only the packages that handle untrusted external input.
GOSEC_PKGS=(
  ./service/macip/...
  ./service/macgarden/...
  ./service/afpfs/macgarden/...
)

echo "=== go vet ==="
go vet ./...

# CI runs golangci-lint through its dedicated action (for caching and the
# GitHub UI) and sets SKIP_LINT=1 so we don't lint twice; locally the script
# runs it directly when present.
if [[ "${SKIP_LINT:-0}" == "1" ]]; then
  echo "=== golangci-lint (skipped: SKIP_LINT=1) ==="
elif command -v golangci-lint >/dev/null 2>&1; then
  echo "=== golangci-lint ==="
  golangci-lint run --build-tags=all --timeout=5m
else
  echo "=== golangci-lint (not on PATH; install from https://golangci-lint.run) ===" >&2
fi

echo "=== govulncheck ==="
command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck -tags all ./...

echo "=== gosec (untrusted-input paths) ==="
command -v gosec >/dev/null 2>&1 || go install github.com/securego/gosec/v2/cmd/gosec@latest
# G115 (integer overflow on conversions) is excluded: the flagged conversions
# operate on values already bounded by the protocol/wire formats (DHCP option
# lengths, IPv4 octet extraction, AppleTalk node/socket bytes), so the
# "overflow" cannot occur in practice and the rule is pure noise here.
gosec -tags all -exclude=G115 "${GOSEC_PKGS[@]}"

echo "=== quality checks passed ==="
