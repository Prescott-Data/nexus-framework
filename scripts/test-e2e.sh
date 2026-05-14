#!/usr/bin/env bash
# scripts/test-e2e.sh — Full stack E2E dependency test
# Spins up the Nexus stack, runs SDK tests, and tears down.
#
# Usage:
#   bash scripts/test-e2e.sh
#
# Requires: docker-compose, go, node/npx, python3

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

FAILED=0

log()  { echo -e "${GREEN}[E2E]${NC} $*"; }
warn() { echo -e "${YELLOW}[E2E]${NC} $*"; }
fail() { echo -e "${RED}[E2E]${NC} $*"; FAILED=1; }

cleanup() {
  log "Tearing down stack..."
  docker-compose down --remove-orphans -v 2>/dev/null || true
}
trap cleanup EXIT

# ──────────────────────────────────────────────
# Phase 1: Unit + Contract Tests (no Docker needed)
# ──────────────────────────────────────────────
log "━━━ Phase 1: Unit & Contract Tests ━━━"

log "Running Go SDK tests..."
(cd nexus-sdk && go test ./... -count=1) || fail "Go SDK tests failed"

log "Running Gateway tests (incl. broker contract tests)..."
(cd nexus-gateway && go test ./... -count=1) || fail "Gateway tests failed"

log "Running Bridge tests (incl. fault injection)..."
(cd nexus-bridge && go test ./... -count=1 -timeout=30s) || fail "Bridge tests failed"

log "Running Broker tests..."
(cd nexus-broker && go test ./... -count=1) || fail "Broker tests failed"

log "Running Python SDK unit tests..."
(cd nexus-sdk-python && python3 -m unittest discover -s tests -q) || fail "Python SDK tests failed"

log "Running TypeScript SDK build..."
(cd nexus-sdk-ts && npm run build) || fail "TypeScript SDK build failed"

# ──────────────────────────────────────────────
# Phase 2: Full Stack E2E (requires Docker)
# ──────────────────────────────────────────────
if ! command -v docker-compose &>/dev/null && ! docker compose version &>/dev/null 2>&1; then
  warn "docker-compose not found — skipping Phase 2 (full stack E2E)"
  warn "Install Docker to run the full dependency verification."
else
  log "━━━ Phase 2: Full Stack E2E ━━━"

  log "Starting Nexus stack..."
  docker-compose up -d --build 2>&1 | tail -5

  # Wait for Gateway health
  GATEWAY_URL="http://localhost:${PORT_GATEWAY:-8090}"
  log "Waiting for Gateway at ${GATEWAY_URL}..."
  RETRIES=30
  until curl -sf "${GATEWAY_URL}/health" > /dev/null 2>&1 || [ $RETRIES -le 0 ]; do
    sleep 2
    RETRIES=$((RETRIES - 1))
  done

  if [ $RETRIES -le 0 ]; then
    fail "Gateway did not become healthy within 60s"
  else
    log "Gateway is healthy"

    # Run Go SDK smoke test (if NEXUS_GATEWAY_URL is set)
    log "Running Go SDK against live Gateway..."
    NEXUS_GATEWAY_URL="${GATEWAY_URL}" go run nexus-sdk/cmd/smoke-mcp/main.go 2>&1 || fail "Go SDK smoke test failed"

    # Run TypeScript SDK smoke test
    log "Running TypeScript SDK against live Gateway..."
    (cd nexus-sdk-ts && NEXUS_GATEWAY_URL="${GATEWAY_URL}" npx tsx tests/smoke-gateway-api.ts 2>&1) || fail "TS SDK smoke test failed"

    # Run Python SDK smoke test
    log "Running Python SDK against live Gateway..."
    (cd nexus-sdk-python && NEXUS_GATEWAY_URL="${GATEWAY_URL}" python3 tests/smoke_test.py 2>&1) || fail "Python SDK smoke test failed"
  fi
fi

# ──────────────────────────────────────────────
# Results
# ──────────────────────────────────────────────
echo ""
if [ $FAILED -eq 0 ]; then
  log "━━━ All E2E tests passed! ━━━"
  exit 0
else
  fail "━━━ Some E2E tests failed. See output above. ━━━"
  exit 1
fi
