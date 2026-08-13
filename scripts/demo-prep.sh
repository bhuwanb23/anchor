#!/usr/bin/env bash
set -euo pipefail

# ================================================
# Anchor Infer — Phase 4 Demo Prep Verification
# Run this the morning of the demo (or the night before).
# It automates the checks in docs/layers/phase4-demo-prep.md.
#
# Usage:
#   ./scripts/demo-prep.sh \
#       --control-plane https://anchor-api.example.com \
#       --token <dashboard access token> \
#       --server <server id> \
#       [--template llm-chat-kleidiai] \
#       [--endpoint https://infer-llm-chat-kleidiai.<server>.anchor.app]
#
# Checks:
#   1. The inference endpoint answers on /health.
#   2. Saved benchmark rows exist for the server (both generic + optimized
#      runs), with plausible improvement numbers.
#   3. The status endpoint restores the deployment AND returns the API key,
#      so Section 3 of the dashboard is fully populated on page load.
#
# Exit code 0 = demo-ready. Any failed check prints a fix hint.
# ================================================

CP_URL=""
TOKEN=""
SERVER_ID=""
TEMPLATE_ID="llm-chat-kleidiai"
ENDPOINT=""

for arg in "$@"; do
  case $arg in
    --control-plane=*) CP_URL="${arg#*=}" ;;
    --token=*)         TOKEN="${arg#*=}" ;;
    --server=*)        SERVER_ID="${arg#*=}" ;;
    --template=*)      TEMPLATE_ID="${arg#*=}" ;;
    --endpoint=*)      ENDPOINT="${arg#*=}" ;;
    *)
      echo "Unknown argument: $arg"
      exit 1
      ;;
  esac
done

if [ -z "$CP_URL" ] || [ -z "$TOKEN" ] || [ -z "$SERVER_ID" ]; then
  echo "Usage: $0 --control-plane <url> --token <token> --server <server_id> [--template <id>] [--endpoint <url>]"
  echo ""
  echo "  --control-plane  Control plane base URL (e.g. https://anchor-api.example.com)"
  echo "  --token          Your dashboard access token (localStorage access_token)"
  echo "  --server         The server id the model is deployed on"
  echo "  --template       Template id (default: llm-chat-kleidiai)"
  echo "  --endpoint       Full endpoint URL. Defaults to"
  echo "                   https://infer-<template>.<server>.anchor.app"
  exit 1
fi

if ! command -v curl &>/dev/null; then
  echo "Error: curl is required"
  exit 1
fi

PASS=0
FAIL=0

ok()   { echo "  ✅ $1"; PASS=$((PASS + 1)); }
bad()  { echo "  ❌ $1"; FAIL=$((FAIL + 1)); }

echo "================================================"
echo " Anchor Infer — Demo Prep Verification"
echo "================================================"
echo ""

# ─────────────────────────────────────────────
# Check 1 — Endpoint answers
# ─────────────────────────────────────────────

[ -z "$ENDPOINT" ] && ENDPOINT="https://infer-${TEMPLATE_ID}.${SERVER_ID}.anchor.app"
HEALTH_URL="${ENDPOINT}/health"

echo "[1/3] Endpoint health: ${HEALTH_URL}"
HTTP_CODE=$(curl -s -o /tmp/anchor-demo-health.out -w "%{http_code}" --max-time 20 "$HEALTH_URL" || true)
if [ "$HTTP_CODE" = "200" ]; then
  ok "Endpoint responds 200 OK"
  echo "      body: $(head -c 200 /tmp/anchor-demo-health.out | tr '\n' ' ')"
else
  bad "Endpoint returned HTTP ${HTTP_CODE} (expected 200)."
  echo "      The model may still be loading, or Caddy/container may be down."
  echo "      Fix: check the container on the server: docker ps | grep infer-"
fi

# ─────────────────────────────────────────────
# Check 2 — Benchmark rows exist (generic + optimized)
# ─────────────────────────────────────────────

BENCH_URL="${CP_URL}/api/v1/servers/${SERVER_ID}/infer/benchmark"
echo ""
echo "[2/3] Saved benchmark: ${BENCH_URL}"

if command -v jq &>/dev/null; then
  BENCH=$(curl -s --max-time 20 -H "Authorization: Bearer ${TOKEN}" "$BENCH_URL")
  GENERIC_TPS=$(echo "$BENCH" | jq -r '.generic.median_tokens_per_second // "missing"' 2>/dev/null || echo "missing")
  OPT_TPS=$(echo "$BENCH" | jq -r '.optimized.median_tokens_per_second // "missing"' 2>/dev/null || echo "missing")
  IMPROV=$(echo "$BENCH" | jq -r '.tokens_per_second_improvement_pct // "missing"' 2>/dev/null || echo "missing")
  TTFT_IMPROV=$(echo "$BENCH" | jq -r '.ttft_improvement_pct // "missing"' 2>/dev/null || echo "missing")

  if [ "$GENERIC_TPS" != "missing" ] && [ "$OPT_TPS" != "missing" ]; then
    ok "Both benchmark runs persisted (generic ${GENERIC_TPS} tok/s, optimized ${OPT_TPS} tok/s)"
    echo "      throughput improvement: ${IMPROV}%  ·  TTFT improvement: ${TTFT_IMPROV}%"
    if [ "${IMPROV}" != "missing" ] && [ "$(echo "$IMPROV" <= 0 | bc 2>/dev/null || echo 0)" = "1" ]; then
      echo "      ⚠️  Optimized is not faster than generic — check the numbers look representative."
    fi
  else
    bad "Benchmark comparison incomplete (generic=${GENERIC_TPS}, optimized=${OPT_TPS})."
    echo "      Fix: re-run the benchmark from the dashboard (\"Run benchmark again\") and"
    echo "      wait for it to finish before the demo."
  fi
else
  echo "      (jq not installed — raw response below)"
  curl -s --max-time 20 -H "Authorization: Bearer ${TOKEN}" "$BENCH_URL"
  echo ""
  echo "      Verify both .generic.median_tokens_per_second and .optimized.median_tokens_per_second are present."
fi

# ─────────────────────────────────────────────
# Check 3 — Status restore includes the API key
# ─────────────────────────────────────────────

STATUS_URL="${CP_URL}/api/v1/servers/${SERVER_ID}/infer/status"
echo ""
echo "[3/3] Status restore: ${STATUS_URL}"

if command -v jq &>/dev/null; then
  STATUS=$(curl -s --max-time 20 -H "Authorization: Bearer ${TOKEN}" "$STATUS_URL")
  DEPLOYED=$(echo "$STATUS" | jq -r '.deployed // false' 2>/dev/null || echo "false")
  HAS_KEY=$(echo "$STATUS" | jq -r '.api_key // empty' 2>/dev/null || echo "")
  DETAILS=$(echo "$STATUS" | jq -r '.details.endpoint_url // empty' 2>/dev/null || echo "")

  if [ "$DEPLOYED" = "true" ]; then
    ok "Deployment status restored (deployed=true)"
  else
    bad "Status says deployed=false — no completed deploy stored."
    echo "      Fix: complete a deploy (and benchmark) before the demo."
  fi

  if [ -n "$HAS_KEY" ]; then
    ok "API key available on restore — Section 3 test prompt will work on page load"
  else
    bad "No api_key in status response. Section 3 will load without a usable key."
    echo "      The dashboard needs the deploy result to include api_key for the restore path."
  fi

  if [ -n "$DETAILS" ]; then
    echo "      restored endpoint: ${DETAILS}"
  fi
else
  echo "      (jq not installed — raw response below)"
  curl -s --max-time 20 -H "Authorization: Bearer ${TOKEN}" "$STATUS_URL"
  echo ""
  echo "      Verify .deployed == true and .api_key is present."
fi

# ─────────────────────────────────────────────
# Summary
# ─────────────────────────────────────────────

echo ""
echo "================================================"
echo " Result: ${PASS} passed, ${FAIL} failed"
echo "================================================"

if [ "$FAIL" -gt 0 ]; then
  echo ""
  echo " Not demo-ready yet — fix the ❌ items above, then re-run."
  echo " Optional bonus (Option B): trigger a second deploy from the UI —"
  echo " it should skip the model download and finish in 3–5 minutes."
  exit 1
fi

echo ""
echo " Demo-ready ✅  (Option A: open the Infer page — sections 3 + 4 are"
echo " pre-populated, and the live test prompt will work immediately.)"
exit 0
