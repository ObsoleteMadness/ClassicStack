#!/usr/bin/env bash
set -euo pipefail

# harness.sh — the Phase 1 (refactor) gates for the new core/adapter/compose
# rings. Kept separate from test.sh/quality.sh (which exercise the legacy
# internal/app stack) so the new architecture's guardrails are auditable on
# their own. See .refactor/01-PHASE-harness.md step A4.

echo "=== build-default: go build ./... ==="
go build ./...

echo "=== build-tags-all: go build -tags all ./... ==="
go build -tags all ./...

echo "=== vet: go vet ./core/... ./adapter/... ./compose/... ==="
go vet ./core/... ./adapter/... ./compose/...

echo "=== archtest: import-graph dependency rule (A2) ==="
go test -count=1 ./core/internal/archtest/...

# As more of Phase 1 lands, add the structure tests here:
#   go test -count=1 ./core/... ./compose/...      # B*/C*/E* unit + conformance
#   (DDP round-trip B7, bus conformance E2, reconfigure-and-notify E4, parity E3)

echo "=== core unit tests (grows with B*/C*/E*) ==="
go test -count=1 ./core/... ./compose/...

echo "harness.sh: OK"
