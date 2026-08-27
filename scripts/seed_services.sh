#!/usr/bin/env bash
# Registers every pipeline service (core itself, split_pdf, detect_boxes,
# crop_boxes, read_qr_ocr, gemini_ocr) with core's service registry
# (POST /services), so the Services screen in core_ui starts populated
# instead of empty. Idempotent — skips any name already registered, so it's
# safe to re-run.
#
# Uses the same *_URL env vars (and defaults) as scripts/run_pipeline.sh, so
# it works unmodified against a pipeline started by that script.
set -euo pipefail

CORE_URL="${CORE_URL:-http://127.0.0.1:8824}"

wait_for_health() {
  for _ in $(seq 1 30); do
    if curl -s -o /dev/null -f "$CORE_URL/health"; then
      return 0
    fi
    sleep 1
  done
  echo "error: core did not become healthy at $CORE_URL/health" >&2
  exit 1
}

echo "==> waiting for core at $CORE_URL"
wait_for_health

existing="$(curl -sf "$CORE_URL/services" | jq -r '.services[].name')"

register() {
  local name="$1" url="$2" web_url="${3:-}"
  if grep -qxF "$name" <<<"$existing"; then
    echo "==> $name already registered, skipping"
    return
  fi
  echo "==> registering $name ($url)${web_url:+, web app ($web_url)}"
  local body
  body="$(jq -n --arg name "$name" --arg url "$url" --arg web_url "$web_url" \
    '{name: $name, url: $url} + (if $web_url == "" then {} else {web_url: $web_url} end)')"
  curl -sf -X POST "$CORE_URL/services" \
    -H "Content-Type: application/json" \
    -d "$body" -o /dev/null -w "    -> %{http_code}\n"
}

register "core" "$CORE_URL"
register "split_pdf" "${SPLIT_PDF_URL:-http://127.0.0.1:8823}"
register "detect_boxes" "${DETECT_BOXES_URL:-http://127.0.0.1:8821}" "${DETECT_BOXES_WEB_URL:-http://127.0.0.1:8825}"
register "crop_boxes" "${CROP_BOXES_URL:-http://127.0.0.1:8822}"
register "read_qr_ocr" "${READ_QR_OCR_URL:-http://127.0.0.1:8819}"
register "gemini_ocr" "${GEMINI_OCR_URL:-http://127.0.0.1:8820}" "${GEMINI_OCR_WEB_URL:-http://127.0.0.1:8820/web}"

echo "==> done"
