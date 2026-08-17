#!/usr/bin/env bash
set -euo pipefail

# harness.sh — the Phase 1 (refactor) gates for the new core/adapter/compose
# rings. Kept separate from test.sh/quality.sh (which exercise the legacy
# internal/app stack) so the new architecture's guardrails are auditable on
# their own. See .refactor/01-PHASE-harness.md step A4.

echo "=== build-default: go build ./... ==="
go build ./...

echo "=== spa: Vite UI for webui embed ==="
make spa

echo "=== build-tags-all: go build -tags all ./... ==="
go build -tags all ./...

echo "=== vet: go vet ./core/... ./adapter/... ./compose/... ==="
go vet ./core/... ./adapter/... ./compose/...

echo "=== archtest: import-graph dependency rule (A2) ==="
go test -count=1 ./core/internal/archtest/...

echo "=== new architecture unit and conformance tests (with tags) ==="
go test -count=1 -tags all ./core/... ./compose/... ./adapter/...

echo "harness.sh: OK"
