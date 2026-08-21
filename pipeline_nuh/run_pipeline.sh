#!/usr/bin/env bash
# Runs the detect_boxes -> crop_boxes -> read_qr_ocr pipeline over every image
# in input_imgs/, writing results into this directory's subfolders.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WORKSPACE="$SCRIPT_DIR"
INPUT_DIR="$WORKSPACE/input_imgs"

DETECT_BOXES_DIR="$REPO_ROOT/detect_boxes"
CROP_BOXES_DIR="$REPO_ROOT/crop_boxes"
READ_QR_OCR_DIR="$REPO_ROOT/read_qr_ocr"
GEMINI_OCR_DIR="$REPO_ROOT/gemini_ocr"

READ_QR_OCR_PORT="${READ_QR_OCR_PORT:-8819}"
GEMINI_OCR_PORT="${GEMINI_OCR_PORT:-8820}"
DETECT_BOXES_PORT="${DETECT_BOXES_PORT:-8821}"
CROP_BOXES_PORT="${CROP_BOXES_PORT:-8822}"

READ_QR_OCR_URL="http://127.0.0.1:${READ_QR_OCR_PORT}"
GEMINI_OCR_URL="http://127.0.0.1:${GEMINI_OCR_PORT}"
DETECT_BOXES_URL="http://127.0.0.1:${DETECT_BOXES_PORT}"
CROP_BOXES_URL="http://127.0.0.1:${CROP_BOXES_PORT}"

CONFIDENCE="${CONFIDENCE:-0.25}"
PAD_RATIO="${PAD_RATIO:-0.15}"
UPSCALE="${UPSCALE:-3.0}"
RUN_OCR="${RUN_OCR:-true}"
RUN_GEMINI_OCR="${RUN_GEMINI_OCR:-true}"

pids=()
cleanup() {
  for pid in "${pids[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

wait_for_health() {
  local url="$1" name="$2"
  for _ in $(seq 1 60); do
    if curl -s -o /dev/null -f "$url/health"; then
      return 0
    fi
    sleep 1
  done
  echo "error: $name did not become healthy at $url" >&2
  exit 1
}

echo "==> starting detect_boxes server on :$DETECT_BOXES_PORT"
(
  cd "$DETECT_BOXES_DIR"
  MODEL_PATH="$DETECT_BOXES_DIR/weight.pt" \
    exec .venv/bin/uvicorn server:app --host 127.0.0.1 --port "$DETECT_BOXES_PORT" \
    >"$WORKSPACE/detect_boxes.log" 2>&1
) &
pids+=($!)
wait_for_health "$DETECT_BOXES_URL" "detect_boxes"

echo "==> starting crop_boxes server on :$CROP_BOXES_PORT"
(
  cd "$CROP_BOXES_DIR"
  exec .venv/bin/uvicorn server:app --host 127.0.0.1 --port "$CROP_BOXES_PORT" \
    >"$WORKSPACE/crop_boxes.log" 2>&1
) &
pids+=($!)
wait_for_health "$CROP_BOXES_URL" "crop_boxes"

echo "==> starting read_qr_ocr server on :$READ_QR_OCR_PORT"
(
  cd "$READ_QR_OCR_DIR"
  exec .venv/bin/uvicorn server:app --host 127.0.0.1 --port "$READ_QR_OCR_PORT" \
    >"$WORKSPACE/read_qr_ocr.log" 2>&1
) &
pids+=($!)
wait_for_health "$READ_QR_OCR_URL" "read_qr_ocr"

if [[ "$RUN_GEMINI_OCR" == "true" ]]; then
  if [[ -z "${GEMINI_API_KEY:-}" && -z "${GOOGLE_API_KEY:-}" ]]; then
    echo "error: RUN_GEMINI_OCR=true but GEMINI_API_KEY (or GOOGLE_API_KEY) is not set" >&2
    exit 1
  fi
  echo "==> starting gemini_ocr server on :$GEMINI_OCR_PORT"
  (
    cd "$GEMINI_OCR_DIR"
    exec .venv/bin/uvicorn server:app --host 127.0.0.1 --port "$GEMINI_OCR_PORT" \
      >"$WORKSPACE/gemini_ocr.log" 2>&1
  ) &
  pids+=($!)
  wait_for_health "$GEMINI_OCR_URL" "gemini_ocr"
fi

echo "==> servers up: detect_boxes ($DETECT_BOXES_URL), crop_boxes ($CROP_BOXES_URL), read_qr_ocr ($READ_QR_OCR_URL)$([[ "$RUN_GEMINI_OCR" == "true" ]] && echo ", gemini_ocr ($GEMINI_OCR_URL)")"
wait
