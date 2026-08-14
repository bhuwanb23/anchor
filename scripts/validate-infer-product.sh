#!/usr/bin/env bash
# Anchor Infer — product-path validation helper (Step B).
# Checks that the control plane Infer API surface is healthy and that
# demo-prep prerequisites are documented when a live agent is not available.
#
# Usage:
#   ./scripts/validate-infer-product.sh \
#     --control-plane https://anchor-api.example.com \
#     --token <jwt> \
#     [--server <server-id>]
set -euo pipefail

CP_URL=""
TOKEN=""
SERVER_ID=""

for arg in "$@"; do
  case $arg in
    --control-plane=*) CP_URL="${arg#*=}" ;;
    --token=*)         TOKEN="${arg#*=}" ;;
    --server=*)        SERVER_ID="${arg#*=}" ;;
    *) echo "Unknown arg: $arg"; exit 2 ;;
  esac
done

if [[ -z "$CP_URL" || -z "$TOKEN" ]]; then
  echo "Usage: $0 --control-plane=URL --token=JWT [--server=ID]"
  exit 2
fi

auth=(-H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json")
fail=0

echo "== GET /api/v1/infer/templates =="
code=$(curl -sS -o /tmp/infer-templates.json -w "%{http_code}" "${auth[@]}" "$CP_URL/api/v1/infer/templates" || true)
if [[ "$code" != "200" ]]; then
  echo "FAIL templates HTTP $code"; fail=1
else
  echo "OK templates"
  head -c 200 /tmp/infer-templates.json; echo
fi

if [[ -n "$SERVER_ID" ]]; then
  echo "== GET /api/v1/servers/$SERVER_ID/platform =="
  code=$(curl -sS -o /tmp/infer-platform.json -w "%{http_code}" "${auth[@]}" \
    "$CP_URL/api/v1/servers/$SERVER_ID/platform" || true)
  if [[ "$code" == "200" || "$code" == "404" ]]; then
    echo "OK platform HTTP $code (404 = agent has not reported yet)"
  else
    echo "FAIL platform HTTP $code"; fail=1
  fi

  echo "== GET /api/v1/servers/$SERVER_ID/infer/status =="
  code=$(curl -sS -o /tmp/infer-status.json -w "%{http_code}" "${auth[@]}" \
    "$CP_URL/api/v1/servers/$SERVER_ID/infer/status" || true)
  if [[ "$code" != "200" ]]; then
    echo "FAIL status HTTP $code"; fail=1
  else
    echo "OK status"
    cat /tmp/infer-status.json; echo
  fi

  echo "== GET /api/v1/servers/$SERVER_ID/infer/benchmark =="
  code=$(curl -sS -o /tmp/infer-bench.json -w "%{http_code}" "${auth[@]}" \
    "$CP_URL/api/v1/servers/$SERVER_ID/infer/benchmark" || true)
  if [[ "$code" != "200" ]]; then
    echo "FAIL benchmark HTTP $code"; fail=1
  else
    echo "OK benchmark"
    cat /tmp/infer-bench.json; echo
  fi

  echo ""
  echo "Next (live deploy):"
  echo "  1. Build/push images: ./infer/docker/build.sh ghcr.io/OWNER/anchor-infer --push"
  echo "  2. On the agent host: export ANCHOR_INFER_IMAGE_BASE=ghcr.io/OWNER/anchor-infer"
  echo "  3. Open dashboard /infer → Detect hardware → Deploy"
  echo "  4. ./scripts/demo-prep.sh --control-plane=$CP_URL --token=... --server=$SERVER_ID"
else
  echo "(skip server-scoped checks — pass --server=ID)"
fi

exit $fail
